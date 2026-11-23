// Package pipeline: a run -- the lesson's audio -> ai-trns words job -> ai-trns speakers job -> its words
// (?format=json) and diarization (?format=paragraphs without the text) kept, segmentation revision `auto`.
// A lesson with checked subtitles (MDB, made before mdb.VTTBefore) skips the words job: its cues go to the
// speakers job instead. Driven by polling (Sync): on every read and by the maintenance loop (SyncAll).
// One run per lesson at a time. Rebuild: a new revision `auto` from a run's kept words and diarization.
package pipeline

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"github.com/Bnei-Baruch/archive-stt/aitrns"
	"github.com/Bnei-Baruch/archive-stt/common"
	"github.com/Bnei-Baruch/archive-stt/kmedia"
	"github.com/Bnei-Baruch/archive-stt/markup"
	"github.com/Bnei-Baruch/archive-stt/models"
	"github.com/Bnei-Baruch/archive-stt/search"
	"github.com/Bnei-Baruch/archive-stt/store"
	"github.com/Bnei-Baruch/archive-stt/subtitles"
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
	FetchFile(fileID string) ([]byte, error)
	AudioURL(fileID string) string
}

// Subtitles finds the lesson's checked subtitles (mdb.Client): a file uid or ""
type Subtitles interface {
	VTT(cuUID, language string) (string, error)
}

type Pipeline struct {
	Store     *store.Store
	AiTrns    AiTrns
	Kmedia    Kmedia
	Subtitles Subtitles // nil: no MDB, the words always come from the STT
	mu        sync.Mutex
	indices   search.Cache // the transcripts' words for Locate, by transcript and run
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

// BuildSegmentation is the automatic segmentation of a run's words and diarization and the lesson's transcript
// (html: its doc2html; nil / "": none, built from the audio), and the transcript's words on the run's (nil without
// a transcript). The revision names the transcript even when it has no marks and the segmentation is the audio's:
// its words are aligned all the same.
func BuildSegmentation(run *models.Run, words *models.Words, d *models.Diarization, transcript *models.FileRef,
	html string) (*models.SegmentationData, *models.Alignment) {
	seg := markup.Build(d.Paragraphs, words.Words, html, markup.Duration(run.Data.Media, d.Paragraphs))
	if transcript == nil || html == "" {
		return seg, nil
	}
	seg.Transcript = transcript
	return seg, markup.AlignWords(markup.TranscriptParagraphs(html), words.Words)
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
	// a run is not failed for archive-unzip: built from the audio, a rebuild later takes the transcript
	transcript, html, err := p.transcript(&run)
	if err != nil {
		slog.Warn("no transcript, built from the audio", "uid", run.CuUID, "err", err)
		transcript, html = nil, ""
	}
	seg, aligned := BuildSegmentation(&run, words, d, transcript, html)
	if aligned != nil {
		if err := p.Store.SaveAlignment(transcript.FileUID, run.ID, aligned); err != nil {
			return failed(run, "saving the alignment: "+err.Error()), nil
		}
	}
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
	if aligned != nil {
		stats["alignment"] = aligned.Stats
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
	audio, _ := kmedia.LessonFiles(unit, language)
	if audio == nil {
		return nil, &StartError{422, fmt.Sprintf("unit %s has no %s audio", uid, language)}
	}
	audioURL := p.Kmedia.AudioURL(audio.ID)
	vttUID, srt, err := p.checkedSubtitles(uid, language)
	if err != nil {
		return nil, err
	}
	stage, wordsSource, jobs := common.StageWords, common.WordsSourceSTT, models.Jobs{}
	if srt != "" { // the words are the subtitles': only the speakers job, which times them on the audio
		stage, wordsSource = common.StageSpeakers, common.WordsSourceVTT
		jobs.Speakers, err = p.AiTrns.CreateSpeakersJob(audioURL, language, srt, uid)
	} else {
		jobs.Words, err = p.AiTrns.CreateWordsJob(audioURL, language)
	}
	if err != nil {
		return nil, &StartError{502, err.Error()}
	}
	duration := audio.Duration
	if duration == 0 {
		duration = unit.Duration
	}
	run = &models.Run{
		ID: newID(), CuUID: uid, Kind: common.RunKindFirst, Language: language, FileUID: audio.ID,
		Stage: stage, Status: common.StatusPending, WordsSource: wordsSource, Jobs: jobs, Created: utils.Now(),
		Data: models.RunData{AudioURL: audioURL, OriginalLanguage: original,
			Media: models.Media{FileUID: audio.ID, DurationS: duration}},
	}
	if author != "" {
		run.Author = &author
	}
	if vttUID != "" && srt != "" {
		run.VttFileUID = &vttUID
	}
	if err := p.Store.SaveRun(run); err != nil {
		return nil, err
	}
	slog.Info("run started", "uid", uid, "id", run.ID, "words", wordsSource, "stage", stage)
	return run, nil
}

// checkedSubtitles is the lesson's checked subtitles in language (MDB) and their cues as SRT; ("", "") if it has
// none or there is no MDB. A file that gives no cues is logged and the STT is used.
func (p *Pipeline) checkedSubtitles(uid, language string) (string, string, error) {
	if p.Subtitles == nil {
		return "", "", nil
	}
	vttUID, err := p.Subtitles.VTT(uid, language)
	if err != nil {
		return "", "", &StartError{502, "mdb: " + err.Error()}
	}
	if vttUID == "" {
		return "", "", nil
	}
	b, err := p.Kmedia.FetchFile(vttUID)
	if err != nil {
		return "", "", &StartError{502, "subtitles: " + err.Error()}
	}
	srt, n, err := subtitles.ToSRT(string(b))
	if err != nil || n == 0 {
		slog.Warn("subtitles unusable, the words from the STT", "uid", uid, "vtt", vttUID, "cues", n, "err", err)
		return vttUID, "", nil
	}
	return vttUID, srt, nil
}

// transcript is the lesson's transcript in kmedia now, in the run's language, and its doc2html; (nil, "") if it
// has none. Taken when a segmentation is built (a run's end, a rebuild), not when the run starts: the newest.
func (p *Pipeline) transcript(run *models.Run) (*models.FileRef, string, error) {
	unit, err := p.Kmedia.FetchUnit(run.CuUID)
	if errors.Is(err, kmedia.ErrNotFound) {
		return nil, "", err
	}
	if err != nil {
		return nil, "", fmt.Errorf("kmedia: %w", err)
	}
	_, t := kmedia.LessonFiles(unit, run.Language)
	if t == nil {
		return nil, "", nil
	}
	html, err := p.Kmedia.FetchTranscriptHTML(t.ID)
	if err != nil {
		return nil, "", fmt.Errorf("transcript: %w", err)
	}
	return &models.FileRef{FileUID: t.ID}, html, nil
}

// RebuildError is a rebuild that could not be done: Status is the HTTP status to answer with
type RebuildError = StartError

// Rebuilt is what a rebuild did
type Rebuilt struct {
	Rev        int                    `json:"rev"`
	Unchanged  bool                   `json:"unchanged"` // the same as the last revision auto of that run: none added
	Current    bool                   `json:"current"`   // the lesson's current revision (false: a human one is)
	WordsRunID string                 `json:"words_run_id"`
	Source     string                 `json:"source"` // transcript | audio
	Transcript *models.FileRef        `json:"transcript"`
	Stats      map[string]interface{} `json:"stats"`
	Alignment  map[string]int         `json:"alignment,omitempty"` // the transcript's words on the run's: words, eq, sub
}

// Rebuild writes a new segmentation revision `auto` from a run's kept words and diarization (runID; "": the
// lesson's latest ready run that has them) and the lesson's transcript in kmedia now -- no ai-trns. Human
// revisions stay current. A revision equal to the last `auto` of the same run is not written again; the
// transcript's alignment is written all the same (an older build may have none).
func (p *Pipeline) Rebuild(uid, runID, author string) (*Rebuilt, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	run, err := p.rebuildRun(uid, runID)
	if err != nil {
		return nil, err
	}
	words, err := p.Store.LoadWords(run.FileUID, run.ID)
	if err != nil {
		return nil, &RebuildError{409, fmt.Sprintf("run %s: no words kept: %v", run.ID, err)}
	}
	d, err := p.Store.Diarization(run.ID)
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, &RebuildError{409, fmt.Sprintf("run %s: no diarization kept", run.ID)}
	}
	// not silently from the audio: that would replace a revision built on the transcript with a worse one
	transcript, html, err := p.transcript(run)
	if errors.Is(err, kmedia.ErrNotFound) {
		return nil, &RebuildError{404, "no kmedia unit " + uid}
	}
	if err != nil {
		return nil, &RebuildError{502, err.Error()}
	}
	data, aligned := BuildSegmentation(run, words, d, transcript, html)
	var alignStats map[string]int
	if aligned != nil {
		if err := p.Store.SaveAlignment(transcript.FileUID, run.ID, aligned); err != nil {
			return nil, err
		}
		alignStats = aligned.Stats
	}
	seg, err := p.Store.LastAuto(uid, run.ID)
	if err != nil {
		return nil, err
	}
	unchanged := seg != nil && sameData(&seg.Data, data)
	if !unchanged {
		seg = &models.Segmentation{CuUID: uid, FileUID: run.FileUID, WordsRunID: run.ID, Source: common.RevAuto,
			Data: *data}
		if author != "" {
			seg.Author = &author
		}
		if err := p.Store.AddSegmentation(seg); err != nil {
			return nil, err
		}
		slog.Info("rebuilt", "uid", uid, "run", run.ID, "rev", seg.Rev, "source", data.Source)
	}
	current, err := p.Store.Segmentation(uid, 0)
	if err != nil {
		return nil, err
	}
	return &Rebuilt{Rev: seg.Rev, Unchanged: unchanged, Current: current != nil && current.Rev == seg.Rev,
		WordsRunID: run.ID, Source: data.Source, Transcript: data.Transcript, Stats: data.Stats,
		Alignment: alignStats}, nil
}

// rebuildRun is the run to rebuild from: the given one (of this lesson, ready) or the latest ready one with a
// diarization kept
func (p *Pipeline) rebuildRun(uid, runID string) (*models.Run, error) {
	if runID != "" {
		run, err := p.Store.GetRun(runID)
		if err != nil {
			return nil, err
		}
		if run == nil || run.CuUID != uid {
			return nil, &RebuildError{404, "no such run of this lesson"}
		}
		if run.Status != common.StatusReady {
			return nil, &RebuildError{409, fmt.Sprintf("run %s is %s", run.ID, run.Status)}
		}
		return run, nil
	}
	runs, err := p.Store.Runs(uid)
	if err != nil {
		return nil, err
	}
	for _, run := range runs {
		if run.Status != common.StatusReady {
			continue
		}
		d, err := p.Store.Diarization(run.ID)
		if err != nil {
			return nil, err
		}
		if d != nil {
			return run, nil
		}
	}
	return nil, &RebuildError{404, "no ready run with its words and diarization kept"}
}

func sameData(a, b *models.SegmentationData) bool {
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(x) == string(y)
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

// currentAny is the lesson's current revision and its run, in language ("": the run's); nils if not
func (p *Pipeline) currentAny(uid, language string) (*models.Segmentation, *models.Run, error) {
	seg, err := p.Store.Segmentation(uid, 0)
	if err != nil || seg == nil {
		return nil, nil, err
	}
	run, err := p.Store.GetRun(seg.WordsRunID)
	if err != nil || run == nil || (language != "" && language != run.Language) {
		return nil, nil, err
	}
	return seg, run, nil
}

// current is currentAny if the revision is on a transcript
func (p *Pipeline) current(uid, language string) (*models.Segmentation, *models.Run, error) {
	seg, run, err := p.currentAny(uid, language)
	if err != nil || seg == nil || seg.Data.Transcript == nil {
		return nil, nil, err
	}
	return seg, run, nil
}

// transcriptIndex is the words of the revision's transcript aligned on its run's, for search; nil if none kept
func (p *Pipeline) transcriptIndex(seg *models.Segmentation, run *models.Run) (*search.Index, error) {
	if seg.Data.Transcript == nil {
		return nil, nil
	}
	t := seg.Data.Transcript.FileUID
	return p.indices.Get("t/"+t+"/"+run.ID, func() (*search.Index, error) {
		a, err := p.Store.LoadAlignment(t, run.ID)
		if err != nil || a == nil {
			return nil, err
		}
		return search.NewIndex(a), nil
	})
}

// heardIndex is the run's heard words, for search
func (p *Pipeline) heardIndex(run *models.Run) (*search.Index, error) {
	return p.indices.Get("w/"+run.FileUID+"/"+run.ID, func() (*search.Index, error) {
		w, err := p.Store.LoadWords(run.FileUID, run.ID)
		if err != nil {
			return nil, err
		}
		return search.NewWordsIndex(w.Words), nil
	})
}

// Timecode is the time of every word of the transcript of the lesson's current revision, in language ("": the
// run's); nil if there is no markup, no transcript, no alignment kept or the markup is in another language
func (p *Pipeline) Timecode(uid, language string) (*models.Timecode, error) {
	seg, run, err := p.current(uid, language)
	if err != nil || seg == nil {
		return nil, err
	}
	a, err := p.Store.LoadAlignment(seg.Data.Transcript.FileUID, run.ID)
	if err != nil || a == nil {
		return nil, err
	}
	out := &models.Timecode{UID: uid, Language: run.Language, Transcript: *seg.Data.Transcript, Media: run.Data.Media,
		RunID: run.ID, Rev: seg.Rev, Paragraphs: make([][]float64, len(a.Paragraphs))}
	for i, ws := range a.Paragraphs {
		out.Paragraphs[i] = make([]float64, len(ws))
		for k, w := range ws {
			out.Paragraphs[i][k] = w.Start
		}
	}
	return out, nil
}

// Locate finds fragments of the transcript of the lesson's current revision, in language ("": the run's): each
// query is "<first words>|<last words>|<number of words>", the last two optional (the number: about, it picks
// among several places). nil if there is no markup
// on a transcript (in that language) or no alignment kept.
func (p *Pipeline) Locate(uid, language string, queries []string) (*models.Located, error) {
	seg, run, err := p.current(uid, language)
	if err != nil || seg == nil {
		return nil, err
	}
	ix, err := p.transcriptIndex(seg, run)
	if err != nil || ix == nil {
		return nil, err
	}
	out := &models.Located{UID: uid, Language: run.Language, Transcript: *seg.Data.Transcript, RunID: run.ID,
		Rev: seg.Rev, Results: make([]models.Fragment, len(queries))}
	for k, q := range queries {
		parts := strings.SplitN(q, "|", 3)
		var last string
		n := 0
		if len(parts) > 1 {
			last = parts[1]
		}
		if len(parts) > 2 {
			n, _ = strconv.Atoi(strings.TrimSpace(parts[2])) // not a number: unknown
		}
		r := models.Fragment{Q: q}
		if from, to, found := ix.Locate(search.Words(parts[0]), search.Words(last), n); found != "" {
			a, b := ix.Pos(from), ix.Pos(to)
			r.Found, r.Start, r.End, r.P, r.W = found, &a.Start, &b.End, &a.P, &a.W
			r.Chapter = chapterAt(seg.Data.Chapters, a.Start)
		}
		out.Results[k] = r
	}
	return out, nil
}

// chapterAt is the chapter at t (a chapter lasts until the next starts), or nil
func chapterAt(chs []models.Chapter, t float64) *models.Chapter {
	for k := len(chs) - 1; k >= 0; k-- {
		if chs[k].Start <= t {
			c := chs[k]
			return &c
		}
	}
	return nil
}

// paragraphAt is the revision's paragraph at t (the last started by then), or nil
func paragraphAt(ps []models.Paragraph, t float64) *models.Paragraph {
	for k := len(ps) - 1; k >= 0; k-- {
		if ps[k].Start <= t {
			c := ps[k]
			return &c
		}
	}
	return nil
}

func textWord(pos search.Pos) models.TextWord {
	w := models.TextWord{Text: pos.Text, Start: pos.Start, End: pos.End}
	if pos.P >= 0 {
		pp, ww := pos.P, pos.W
		w.P, w.W = &pp, &ww
	}
	return w
}

// textIndex is the transcript's words if kept, else the heard ones, and which
func (p *Pipeline) textIndex(seg *models.Segmentation, run *models.Run) (*search.Index, string, error) {
	ix, err := p.transcriptIndex(seg, run)
	if err != nil {
		return nil, "", err
	}
	if ix != nil {
		return ix, common.SegFromTranscript, nil
	}
	ix, err = p.heardIndex(run)
	return ix, common.SegFromAudio, err
}

// Text is the lesson's text at the moment from (at: to < 0) or between from and to, in language ("": the run's):
// the words (the transcript's if it has one, else the heard ones), the paragraphs and the chapters of the current
// revision there; nil if there is no markup (in that language)
func (p *Pipeline) Text(uid, language string, from, to float64) (*models.LessonText, error) {
	seg, run, err := p.currentAny(uid, language)
	if err != nil || seg == nil {
		return nil, err
	}
	ix, source, err := p.textIndex(seg, run)
	if err != nil {
		return nil, err
	}
	out := &models.LessonText{UID: uid, Language: run.Language, Rev: seg.Rev, Source: source,
		Words: []models.TextWord{}, Paragraphs: []models.Paragraph{}, Chapters: []models.Chapter{}}
	if to < 0 { // a moment
		if i := ix.At(from); i >= 0 {
			out.Words = append(out.Words, textWord(ix.Pos(i)))
		}
		if pa := paragraphAt(seg.Data.Paragraphs, from); pa != nil {
			out.Paragraphs = append(out.Paragraphs, *pa)
		}
		if c := chapterAt(seg.Data.Chapters, from); c != nil {
			out.Chapters = append(out.Chapters, *c)
		}
		return out, nil
	}
	i, j := ix.Range(from, to)
	for k := i; k < j; k++ {
		out.Words = append(out.Words, textWord(ix.Pos(k)))
	}
	for _, pa := range seg.Data.Paragraphs {
		if pa.End > from && pa.Start < to {
			out.Paragraphs = append(out.Paragraphs, pa)
		}
	}
	for _, c := range seg.Data.Chapters {
		if c.End > from && c.Start < to {
			out.Chapters = append(out.Chapters, c)
		}
	}
	return out, nil
}

// ContextWords: words on each side of a phrase found, for its context
const ContextWords = 8

// Find is where the phrase q is said in the lesson, in language ("": the run's), at most limit places, the best
// first: in the transcript if it has one, else (or not found there) in the heard words -- fuzzy, the STT errs.
// nil if there is no markup (in that language).
func (p *Pipeline) Find(uid, language, q string, limit int) (*models.Found, error) {
	seg, run, err := p.currentAny(uid, language)
	if err != nil || seg == nil {
		return nil, err
	}
	out := &models.Found{UID: uid, Language: run.Language, Rev: seg.Rev, Q: q, Results: []models.Phrase{}}
	words := search.Words(q)
	if len(words) == 0 {
		return out, nil
	}
	tix, err := p.transcriptIndex(seg, run)
	if err != nil {
		return nil, err
	}
	hix, err := p.heardIndex(run)
	if err != nil {
		return nil, err
	}
	for _, src := range []struct {
		ix     *search.Index
		source string
	}{{tix, common.SegFromTranscript}, {hix, common.SegFromAudio}} {
		if src.ix == nil {
			continue
		}
		matches, _ := src.ix.Find(words)
		for _, m := range matches {
			if len(out.Results) == limit {
				break
			}
			a, b := src.ix.Pos(m.From), src.ix.Pos(m.To)
			ph := models.Phrase{Start: a.Start, End: b.End, Source: src.source,
				Score:     float64(int(float64(m.Matched)/float64(len(words))*100)) / 100,
				Context:   src.ix.Text(m.From-ContextWords, m.To+ContextWords),
				Paragraph: paragraphAt(seg.Data.Paragraphs, a.Start), Chapter: chapterAt(seg.Data.Chapters, a.Start)}
			if a.P >= 0 {
				pp, ww := a.P, a.W
				ph.P, ph.W = &pp, &ww
			}
			out.Results = append(out.Results, ph)
		}
		if len(out.Results) > 0 {
			break
		}
	}
	return out, nil
}
