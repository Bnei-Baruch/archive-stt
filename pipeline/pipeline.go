// Package pipeline: a run -- the lesson's audio -> ai-trns words job -> ai-trns speakers job -> its words
// (?format=json) and diarization (?format=paragraphs without the text) kept, segmentation revision `auto`. Driven by polling (Sync): on every read and by the maintenance loop (SyncAll).
// One run per lesson at a time.
package pipeline

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/Bnei-Baruch/archive-stt/aitrns"
	"github.com/Bnei-Baruch/archive-stt/common"
	"github.com/Bnei-Baruch/archive-stt/kmedia"
	"github.com/Bnei-Baruch/archive-stt/markup"
	"github.com/Bnei-Baruch/archive-stt/models"
	"github.com/Bnei-Baruch/archive-stt/store"
	"github.com/Bnei-Baruch/archive-stt/utils"
)

// AiTrns is what a run needs of ai-trns (aitrns.Client)
type AiTrns interface {
	CreateWordsJob(audioURL, language string) (string, error)
	CreateSpeakersJob(audioURL, language, srt, uid string) (string, error)
	Job(id string) (*aitrns.Job, error)
	Result(id, format string) ([]byte, error)
}

// Kmedia is what a run needs of kmedia (kmedia.Client)
type Kmedia interface {
	FetchUnit(uid string) (*kmedia.Unit, error)
	FetchTranscriptHTML(fileID string) (string, error)
	AudioURL(fileID string) string
}

type Pipeline struct {
	Store  *store.Store
	AiTrns AiTrns
	Kmedia Kmedia
	mu     sync.Mutex
}

// StartError is a run that could not start: Status is the HTTP status to answer with
type StartError struct {
	Status int
	Msg    string
}

func (e *StartError) Error() string { return e.Msg }

func newID() string {
	b := make([]byte, 6)
	utils.Must(func() error { _, err := rand.Read(b); return err }())
	return hex.EncodeToString(b)
}

// jobDone: (the finished job, "") / (nil, "") while it runs / (nil, error) if it failed
func (p *Pipeline) jobDone(id string) (*aitrns.Job, string, error) {
	if id == "" {
		return nil, "job is lost", nil
	}
	j, err := p.AiTrns.Job(id)
	if err != nil {
		return nil, "", err
	}
	if j == nil {
		return nil, "job is lost", nil
	}
	if !aitrns.Final[j.Status] {
		return nil, "", nil
	}
	if j.Status != aitrns.Done {
		if j.Error != "" {
			return nil, j.Error, nil
		}
		return nil, j.Status, nil
	}
	return j, "", nil
}

// BuildSegmentation is the automatic segmentation of a run's words and diarization (and the lesson's transcript,
// if it has one)
func (p *Pipeline) BuildSegmentation(run *models.Run, words *models.Words,
	d *models.Diarization) *models.SegmentationData {
	html := ""
	if t := run.Data.Transcript; t != nil {
		var err error
		if html, err = p.Kmedia.FetchTranscriptHTML(t.FileUID); err != nil {
			slog.Warn("no transcript, built from the audio", "uid", run.CuUID, "err", err)
			html = ""
		}
	}
	return markup.Build(d.Paragraphs, words.Words, html, markup.Duration(run.Data.Media, d.Paragraphs))
}

// diarizeResult is a finished diarize job's words (?format=json) and paragraphs (?format=paragraphs); failure:
// the answers do not hold together. A paragraph's words are found again by time (markup.Split): checked here.
func (p *Pipeline) diarizeResult(jobID string) (*models.Words, *models.DiarizeAnswer, string, error) {
	b, err := p.AiTrns.Result(jobID, "json")
	if err != nil {
		return nil, nil, "", err
	}
	words := new(models.Words)
	if err := json.Unmarshal(b, words); err != nil {
		return nil, nil, "words: " + err.Error(), nil
	}
	if b, err = p.AiTrns.Result(jobID, "paragraphs"); err != nil {
		return nil, nil, "", err
	}
	answer := new(models.DiarizeAnswer)
	if err := json.Unmarshal(b, answer); err != nil {
		return nil, nil, "paragraphs: " + err.Error(), nil
	}
	starts := make([]float64, len(answer.Paragraphs))
	for i, ap := range answer.Paragraphs {
		starts[i] = ap.Start
	}
	for i, ws := range markup.Split(starts, words.Words) {
		if len(ws) != len(answer.Paragraphs[i].Words) {
			return nil, nil, fmt.Sprintf("paragraph %d: %d words, %d by time", i, len(answer.Paragraphs[i].Words),
				len(ws)), nil
		}
	}
	return words, answer, "", nil
}

func failed(run models.Run, msg string) *models.Run {
	run.Status = common.StatusFailed
	run.Error = &msg
	return &run
}

// advance moves the run on if its current job has finished; the returned run is a copy
func (p *Pipeline) advance(run models.Run) (*models.Run, error) {
	if run.Stage == common.StageWords {
		j, failure, err := p.jobDone(run.Jobs.Words)
		if err != nil {
			return nil, err
		}
		if failure != "" {
			return failed(run, "transcription: "+failure), nil
		}
		if j == nil {
			return &run, nil
		}
		srt, err := p.AiTrns.Result(j.ID, "srt")
		if err != nil {
			return nil, err
		}
		id, err := p.AiTrns.CreateSpeakersJob(run.Data.AudioURL, run.Language, string(srt), run.CuUID)
		if err != nil {
			return nil, err
		}
		run.Stage, run.Jobs.Speakers = common.StageSpeakers, id
		return &run, nil
	}
	j, failure, err := p.jobDone(run.Jobs.Speakers)
	if err != nil {
		return nil, err
	}
	if failure != "" {
		return failed(run, "speakers: "+failure), nil
	}
	if j == nil {
		return &run, nil
	}
	words, answer, failure, err := p.diarizeResult(j.ID)
	if err != nil {
		return nil, err
	}
	if failure != "" {
		return failed(run, "speakers: bad answer: "+failure), nil
	}
	d := answer.Diarization()
	// first: the segmentation can be rebuilt from them
	if err := p.Store.SaveWords(run.FileUID, run.ID, words); err != nil {
		return failed(run, "saving the words: "+err.Error()), nil
	}
	if err := p.Store.SaveDiarization(run.ID, d); err != nil {
		return failed(run, "saving the diarization: "+err.Error()), nil
	}
	seg := p.BuildSegmentation(&run, words, d)
	if err := p.Store.AddSegmentation(&models.Segmentation{CuUID: run.CuUID, FileUID: run.FileUID,
		WordsRunID: run.ID, Source: common.RevAuto, Author: run.Author, Data: *seg}); err != nil {
		return failed(run, "saving the segmentation: "+err.Error()), nil
	}
	stats := map[string]interface{}{}
	for k, v := range seg.Stats {
		stats[k] = v
	}
	for k, v := range answer.Stats {
		stats[k] = v
	}
	run.Status = common.StatusReady
	run.Data.Finished = utils.Now()
	run.Data.Stats = stats
	return &run, nil
}

// Sync is the lesson's latest run, moved on if its current job has finished; nil if it has none
func (p *Pipeline) Sync(uid string) (*models.Run, error) {
	run, err := p.Store.LatestRun(uid)
	if err != nil || run == nil || run.Status != common.StatusPending {
		return run, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sync(uid)
}

func (p *Pipeline) sync(uid string) (*models.Run, error) {
	run, err := p.Store.LatestRun(uid)
	if err != nil || run == nil || run.Status != common.StatusPending {
		return run, err
	}
	next, err := p.advance(*run)
	var transient *aitrns.Error
	switch {
	case errors.As(err, &transient): // ai-trns is down for a moment: try again at the next tick
		slog.Warn("ai-trns unavailable, again at the next tick", "uid", uid, "err", err)
		return run, nil
	case err != nil:
		slog.Error("run failed", "uid", uid, "err", err)
		next = failed(*run, err.Error())
	}
	if next.Stage != run.Stage || next.Status != run.Status || next.Jobs != run.Jobs {
		if err := p.Store.SaveRun(next); err != nil {
			return run, err
		}
		slog.Info("run", "uid", uid, "id", next.ID, "status", next.Status, "stage", next.Stage)
	}
	return next, nil
}

// SyncAll moves on every pending run
func (p *Pipeline) SyncAll() {
	uids, err := p.Store.PendingUIDs()
	if err != nil {
		slog.Error("pending runs", "err", err)
		return
	}
	for _, uid := range uids {
		if _, err := p.Sync(uid); err != nil {
			slog.Error("sync failed", "uid", uid, "err", err)
		}
	}
}

// Start begins a first run on the lesson's original recording (or that in language); a pending one is returned
// as is unless force
func (p *Pipeline) Start(uid, language string, force bool, author string) (*models.Run, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	run, err := p.sync(uid)
	if err != nil {
		return nil, err
	}
	if run != nil && run.Status == common.StatusPending && !force {
		return run, nil
	}
	unit, err := p.Kmedia.FetchUnit(uid)
	if errors.Is(err, kmedia.ErrNotFound) {
		return nil, &StartError{404, "no kmedia unit " + uid}
	}
	if err != nil {
		return nil, &StartError{502, "kmedia: " + err.Error()}
	}
	original := kmedia.OriginalLanguage(unit)
	if language == "" {
		language = original
	}
	if language == "" {
		return nil, &StartError{422, fmt.Sprintf("unit %s has no original recording (a file with _o_ in its name)",
			uid)}
	}
	if !common.IsLanguage(language) {
		return nil, &StartError{422, fmt.Sprintf("unit %s: language %s is not transcribed (one of %s)", uid,
			language, strings.Join(common.Languages, ", "))}
	}
	audio, transcript := kmedia.LessonFiles(unit, language)
	if audio == nil {
		return nil, &StartError{422, fmt.Sprintf("unit %s has no %s audio", uid, language)}
	}
	audioURL := p.Kmedia.AudioURL(audio.ID)
	jobID, err := p.AiTrns.CreateWordsJob(audioURL, language)
	if err != nil {
		return nil, &StartError{502, err.Error()}
	}
	duration := audio.Duration
	if duration == 0 {
		duration = unit.Duration
	}
	run = &models.Run{
		ID: newID(), CuUID: uid, Kind: common.RunKindFirst, Language: language, FileUID: audio.ID,
		Stage: common.StageWords, Status: common.StatusPending, WordsSource: common.WordsSourceSTT,
		Jobs: models.Jobs{Words: jobID}, Created: utils.Now(),
		Data: models.RunData{AudioURL: audioURL, OriginalLanguage: original,
			Media: models.Media{FileUID: audio.ID, DurationS: duration}},
	}
	if author != "" {
		run.Author = &author
	}
	if transcript != nil {
		run.Data.Transcript = &models.FileRef{FileUID: transcript.ID}
	}
	if err := p.Store.SaveRun(run); err != nil {
		return nil, err
	}
	slog.Info("run started", "uid", uid, "id", run.ID, "job", jobID)
	return run, nil
}

// Document is the lesson's markup at the current (rev 0) or the given segmentation revision; nil if none
func (p *Pipeline) Document(uid string, rev int) (*models.Document, error) {
	seg, err := p.Store.Segmentation(uid, rev)
	if err != nil || seg == nil {
		return nil, err
	}
	run, err := p.Store.GetRun(seg.WordsRunID)
	if err != nil || run == nil {
		return nil, err
	}
	var words *models.Words // the paragraphs' text, when they are built from the audio
	if seg.Data.Source == common.SegFromAudio {
		if words, err = p.Store.LoadWords(run.FileUID, run.ID); err != nil {
			slog.Warn("no words, the document without the paragraphs' text", "uid", uid, "run", run.ID, "err", err)
			words = nil
		}
	}
	return markup.Document(seg, run, words), nil
}
