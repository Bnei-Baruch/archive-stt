package api

// The run: kmedia files -> ai-trns words job -> ai-trns speakers job -> markup stored and served. ai-trns and
// kmedia are fakes behind the pipeline's interfaces.

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/Bnei-Baruch/archive-stt/aitrns"
	"github.com/Bnei-Baruch/archive-stt/kmedia"
	"github.com/Bnei-Baruch/archive-stt/markup"
	"github.com/Bnei-Baruch/archive-stt/models"
	"github.com/Bnei-Baruch/archive-stt/pipeline"
	"github.com/Bnei-Baruch/archive-stt/store"
)

var paras = []string{
	"",
	"שיעור בוקר - אחרי עריכה",
	"קריין: שיעור בנושא חוק הערבות.",
	"נמשיך אותו כי אנחנו צריכים חיבור בינינו",
	"קריין: קטע מס' 46 מתוך חוק הערבות.",
	`"כל ישראל יש להם חלק לעולם הבא"`,
	`(הרב"ש. מאמר 3)`,
	"תתאר לעצמך שאתה הולך להתחתן עם החברים",
	"שאלה: מה זו אותה מערכת שקושרת בין דברים?",
	"זו המערכת של הבורא שקושרת הכול",
	"תלמיד: ולמה דווקא הבורא?",
	"כי הוא המקור של הכול",
	"קריין: קטע הבא, מס' 47, מאור ושמש.",
	`"העיקר הוא שכל אחד ואחד אל יחשוב לעצמו"`,
	"מי שעושה מעצמו אפס בחברה מקשר את כולם",
}

var markRE = strings.NewReplacer("קריין:", "", "שאלה:", "", "תלמיד:", "")

// diarizeAnswer is what ai-trns answers to ?format=paragraphs -- a paragraph per run of one speaker (the header,
// paragraph 1, is not spoken) -- and to ?format=json: the same words, each with its speaker and cue
func diarizeAnswer(format string) []byte {
	var out, all []map[string]interface{}
	t := 0.0
	for i, p := range paras {
		if i == 1 {
			t += 5
			continue
		}
		label := "הרב"
		if strings.HasPrefix(p, "קריין") || strings.HasPrefix(p, `"`) || strings.HasPrefix(p, "(") {
			label = "קריין"
		} else if strings.HasPrefix(p, "שאלה") || strings.HasPrefix(p, "תלמיד") {
			label = "תלמיד"
		}
		for _, tok := range strings.Fields(markRE.Replace(p)) {
			w := map[string]interface{}{"word": tok, "start": t, "end": t + 0.25}
			all = append(all, map[string]interface{}{"word": tok, "start": t, "end": t + 0.25, "speaker": label,
				"cue": i})
			if n := len(out); n > 0 && out[n-1]["label"] == label {
				out[n-1]["words"] = append(out[n-1]["words"].([]map[string]interface{}), w)
			} else {
				out = append(out, map[string]interface{}{"speaker": label, "label": label, "reading": false,
					"start": t, "words": []map[string]interface{}{w}})
			}
			t += 0.3
		}
	}
	for _, p := range out {
		ws := p["words"].([]map[string]interface{})
		p["end"] = ws[len(ws)-1]["end"]
		var text []string
		for _, w := range ws {
			text = append(text, w["word"].(string))
		}
		p["text"] = strings.Join(text, " ")
	}
	if format == "json" {
		b, _ := json.Marshal(map[string]interface{}{"version": 1, "kind": "diarize", "words": all,
			"stats": map[string]interface{}{"speakers": 3}})
		return b
	}
	b, _ := json.Marshal(map[string]interface{}{"paragraphs": out, "stats": map[string]interface{}{"paragraphs": len(out)}})
	return b
}

type fakeAiTrns struct {
	jobs  map[string]*aitrns.Job
	calls [][]string
	down  bool
}

func (f *fakeAiTrns) CreateWordsJob(url, language string) (string, error) {
	f.calls = append(f.calls, []string{"words", url, language})
	f.jobs["w1"] = &aitrns.Job{ID: "w1", Status: "running"}
	return "w1", nil
}

func (f *fakeAiTrns) CreateSpeakersJob(url, language, srt, uid string) (string, error) {
	f.calls = append(f.calls, []string{"speakers", url, language, srt, uid})
	f.jobs["s1"] = &aitrns.Job{ID: "s1", Status: "running"}
	return "s1", nil
}

func (f *fakeAiTrns) Job(id string) (*aitrns.Job, error) {
	if f.down {
		return nil, &aitrns.Error{}
	}
	j := f.jobs[id]
	if j == nil {
		return nil, nil
	}
	c := *j
	return &c, nil
}

func (f *fakeAiTrns) Result(id, format string) ([]byte, error) {
	switch id + "/" + format {
	case "w1/srt":
		return []byte("1\n00:00:00,000 --> 00:00:01,000\nשלום\n"), nil
	case "s1/paragraphs", "s1/json":
		return diarizeAnswer(format), nil
	}
	return nil, &aitrns.Error{}
}

type fakeKmedia struct {
	unit       *kmedia.Unit
	noDoc      bool              // the transcript cannot be had
	transcript string            // its HTML ("": that of paras)
	files      map[string]string // the CDN
}

func (k *fakeKmedia) FetchUnit(uid string) (*kmedia.Unit, error) {
	if uid != "Unit0001" {
		return nil, kmedia.ErrNotFound
	}
	return k.unit, nil
}

func (k *fakeKmedia) FetchTranscriptHTML(string) (string, error) {
	if k.noDoc {
		return "", errors.New("doc2html: 500")
	}
	if k.transcript != "" {
		return k.transcript, nil
	}
	return "<p>" + strings.Join(paras, "</p><p>") + "</p>", nil
}

func (k *fakeKmedia) FetchFile(id string) ([]byte, error) {
	if b, ok := k.files[id]; ok {
		return []byte(b), nil
	}
	return nil, errors.New("cdn: 404")
}

// fakeMDB: the lessons' checked subtitles by uid/language
type fakeMDB map[string]string

func (m fakeMDB) VTT(uid, language string) (string, error) {
	if uid == "Broken01" {
		return "", errors.New("connection refused")
	}
	return m[uid+"/"+language], nil
}

func (k *fakeKmedia) AudioURL(id string) string { return "https://cdn.kabbalahmedia.info/" + id }

func unit() *kmedia.Unit {
	return &kmedia.Unit{ID: "Unit0001", Duration: 60, Files: []kmedia.File{
		{ID: "AudioTr1", Type: "audio", Language: "he", MimeType: "audio/mpeg", Name: "heb_t_x.mp3"},
		{ID: "AudioOr1", Type: "audio", Language: "he", MimeType: "audio/mpeg", Name: "heb_o_x.mp3"},
		{ID: "TextPdf1", Type: "text", Language: "he", InsertType: "tamlil", Name: "heb_o_x.pdf"},
		{ID: "TextDoc1", Type: "text", Language: "he", InsertType: "tamlil", Name: "heb_o_x.doc"},
	}}
}

type env struct {
	t       *testing.T
	p       *pipeline.Pipeline
	h       *Handlers
	handler http.Handler
	ai      *fakeAiTrns
	km      *fakeKmedia
	mdb     fakeMDB
}

func newEnv(t *testing.T) *env {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ai := &fakeAiTrns{jobs: map[string]*aitrns.Job{}}
	km := &fakeKmedia{unit: unit(), files: map[string]string{}}
	m := fakeMDB{}
	p := &pipeline.Pipeline{Store: s, AiTrns: ai, Kmedia: km, Subtitles: m}
	h := &Handlers{Pipeline: p}
	return &env{t: t, p: p, h: h, handler: Handler(h), ai: ai, km: km, mdb: m}
}

// do sends a request from addr (default: this host) and decodes the answer into out
func (e *env) do(method, path string, out interface{}, opts ...func(*http.Request)) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = "127.0.0.1:50000"
	for _, o := range opts {
		o(req)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			e.t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body.String())
		}
	}
	return rec
}

func (e *env) run(path string) *models.Run {
	e.t.Helper()
	r := new(models.Run)
	e.do("GET", path, r)
	return r
}

func check(t *testing.T, got, want interface{}) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("\ngot  %#v\nwant %#v", got, want)
	}
}

func TestTheChainWordsThenSpeakersThenMarkup(t *testing.T) {
	e := newEnv(t)
	check(t, e.do("GET", "/cus/Unit0001/markup", nil).Code, 404)
	check(t, e.do("GET", "/cus/Unit0001/runs/latest", nil).Code, 404)
	run := new(models.Run)
	rec := e.do("POST", "/cus/Unit0001/markup", run)
	check(t, rec.Code, 202)
	check(t, []interface{}{run.Status, run.Stage, run.Kind, run.FileUID, run.Language, run.WordsSource, *run.Author},
		[]interface{}{"pending", "words", "first", "AudioOr1", "he", "stt", "local"})
	check(t, e.ai.calls, [][]string{{"words", "https://cdn.kabbalahmedia.info/AudioOr1", "he"}})
	// a second start while it runs is the same run
	again := new(models.Run)
	e.do("POST", "/cus/Unit0001/markup", again)
	check(t, again.ID, run.ID)

	// nothing changes while ai-trns works
	check(t, e.run("/cus/Unit0001/runs/latest").Stage, "words")
	e.ai.jobs["w1"].Status = "done"
	r := e.run("/cus/Unit0001/runs/latest")
	check(t, []string{r.Stage, r.Jobs.Words, r.Jobs.Speakers}, []string{"speakers", "w1", "s1"})
	c := e.ai.calls[1]
	check(t, []string{c[0], c[4]}, []string{"speakers", "Unit0001"})
	if !strings.Contains(c[3], "שלום") {
		t.Fatal("the words job's SRT goes to the speakers job")
	}

	e.ai.jobs["s1"].Status = "done"
	e.p.SyncAll()
	r = e.run("/cus/Unit0001/runs/" + run.ID)
	check(t, r.Status, "ready")
	var runs []models.Run
	e.do("GET", "/cus/Unit0001/runs", &runs)
	check(t, len(runs), 1)

	doc := new(models.Document)
	rec = e.do("GET", "/cus/Unit0001/markup", doc, func(r *http.Request) { r.Header.Set("Origin", "https://kabbalahmedia.info") })
	check(t, rec.Code, 200)
	check(t, rec.Header().Get("Access-Control-Allow-Origin"), "*")
	check(t, rec.Header().Get("Cache-Control"), "public, max-age=300")
	check(t, []interface{}{doc.Version, doc.OriginalLanguage, doc.Language, doc.Media.FileUID},
		[]interface{}{2, "he", "he", "AudioOr1"})
	check(t, doc.Transcript, &models.FileRef{FileUID: "TextDoc1"})
	check(t, doc.Run, models.DocumentRun{ID: run.ID, Created: run.Created, WordsSource: "stt", Source: "transcript"})
	check(t, []interface{}{doc.Segmentation.Rev, doc.Segmentation.Source}, []interface{}{1, "auto"})
	var types []string
	for _, ch := range doc.Chapters {
		types = append(types, ch.Type)
	}
	check(t, types, []string{"other", "reading", "commentary", "qa", "reading", "commentary"})
	for _, p := range doc.Paragraphs {
		if p.Speaker != nil && markup.Roles[*p.Speaker] != *p.Speaker {
			t.Fatalf("speaker %q is not a role code", *p.Speaker)
		}
	}
	check(t, doc.Stats["aligned_paragraphs"], 13.0)
	byRev := new(models.Document)
	e.do("GET", "/cus/Unit0001/markup?rev=1", byRev)
	check(t, byRev, doc)
	check(t, e.do("GET", "/cus/Unit0001/markup?rev=2", nil).Code, 404)
	// the words (by the media file) and the diarization (by the run) are kept, so the markup can be rebuilt
	// without ai-trns
	words, err := e.p.Store.LoadWords("AudioOr1", run.ID)
	if err != nil || len(words.Words) == 0 {
		t.Fatalf("words: %v", err)
	}
	d, err := e.p.Store.Diarization(run.ID)
	if err != nil || d == nil || len(d.Paragraphs) == 0 {
		t.Fatalf("diarization: %v", err)
	}
	seg, _ := e.p.Store.Segmentation("Unit0001", 1)
	check(t, seg.Data.Transcript, &models.FileRef{FileUID: "TextDoc1"})
	for _, p := range doc.Paragraphs {
		if p.Text != nil {
			t.Fatal("a paragraph of the transcript got the text of the words")
		}
	}
}

func (e *env) finish() {
	e.ai.jobs["w1"].Status = "done"
	e.run("/cus/Unit0001/runs/latest")
	e.ai.jobs["s1"].Status = "done"
	e.run("/cus/Unit0001/runs/latest")
}

func TestASecondRunIsANewAutoRevision(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 2; i++ {
		e.do("POST", "/cus/Unit0001/markup?force=true", nil)
		e.finish()
	}
	doc := new(models.Document)
	e.do("GET", "/cus/Unit0001/markup", doc)
	check(t, doc.Segmentation.Rev, 2)
	var runs []models.Run
	e.do("GET", "/cus/Unit0001/runs", &runs)
	check(t, len(runs), 2)
}

func TestTheOriginalLanguageComesFromTheOriginalRecording(t *testing.T) {
	e := newEnv(t)
	e.km.unit = &kmedia.Unit{ID: "Unit0001", Files: []kmedia.File{
		{ID: "AudioHe1", Type: "audio", Language: "he", MimeType: "audio/mpeg", Name: "heb_t_x.mp3"},
		{ID: "AudioRu1", Type: "audio", Language: "ru", MimeType: "audio/mpeg", Name: "rus_o_x.mp3"}}}
	run := new(models.Run)
	e.do("POST", "/cus/Unit0001/markup", run)
	check(t, []string{run.Language, run.FileUID, run.Data.OriginalLanguage}, []string{"ru", "AudioRu1", "ru"})
	// a translation, asked for by name
	e.do("POST", "/cus/Unit0001/markup?language=he&force=true", run)
	check(t, []string{run.Language, run.FileUID, run.Data.OriginalLanguage}, []string{"he", "AudioHe1", "ru"})
	for i := range e.km.unit.Files {
		e.km.unit.Files[i].Name = "x.mp3"
	}
	check(t, e.do("POST", "/cus/Unit0001/markup?force=true", nil).Code, 422)
}

func TestAFailedJobFailsTheRunAndItCanBeStartedAgain(t *testing.T) {
	e := newEnv(t)
	e.do("POST", "/cus/Unit0001/markup", nil)
	e.ai.jobs["w1"].Status, e.ai.jobs["w1"].Error = "error", "boom"
	r := e.run("/cus/Unit0001/runs/latest")
	if r.Status != "failed" || !strings.Contains(*r.Error, "boom") {
		t.Fatalf("%+v", r)
	}
	e.do("POST", "/cus/Unit0001/markup", r)
	check(t, r.Status, "pending")
}

func TestAiTrnsDownForAMomentDoesNotFailTheRun(t *testing.T) {
	e := newEnv(t)
	e.do("POST", "/cus/Unit0001/markup", nil)
	e.ai.down = true
	check(t, e.run("/cus/Unit0001/runs/latest").Status, "pending")
}

func TestChecks(t *testing.T) {
	e := newEnv(t)
	check(t, e.do("GET", "/cus/bad/markup", nil).Code, 400)
	check(t, e.do("POST", "/cus/Missing1/markup", nil).Code, 404)
	check(t, e.do("POST", "/cus/Unit0001/markup?language=xx", nil).Code, 400)
	check(t, e.do("GET", "/cus/Unit0001/markup?rev=x", nil).Code, 400)
	e.h.APIKeys = map[string]string{"dudi": "secret"}
	check(t, e.do("POST", "/cus/Unit0001/markup", nil).Code, 401)
	run := new(models.Run)
	rec := e.do("POST", "/cus/Unit0001/markup", run, func(r *http.Request) { r.Header.Set("X-API-Key", "secret") })
	check(t, []interface{}{rec.Code, *run.Author}, []interface{}{202, "key:dudi"})
	check(t, e.do("GET", "/cus/Unit0001/runs/latest", nil).Code, 200)  // reading stays public
	check(t, e.do("GET", "/cus/Unit0002/runs/"+run.ID, nil).Code, 404) // not this lesson's run
	detail := map[string]string{}
	e.do("POST", "/cus/Unit0001/markup", &detail)
	check(t, detail["detail"], "X-API-Key required")
}

func TestWithoutKeysARunIsStartedOnlyFromThisHost(t *testing.T) {
	e := newEnv(t)
	remote := func(r *http.Request) {
		r.RemoteAddr = "203.0.113.7:50000"
		r.Header.Set("X-Forwarded-For", "127.0.0.1")
	}
	check(t, e.do("POST", "/cus/Unit0001/markup", nil, remote).Code, 403)
	check(t, e.do("GET", "/health", nil, remote).Code, 200)
	check(t, e.do("POST", "/cus/Unit0001/markup", nil, func(r *http.Request) { r.RemoteAddr = "[::1]:50000" }).Code, 202)
}

func TestCorsPreflight(t *testing.T) {
	e := newEnv(t)
	rec := e.do("OPTIONS", "/cus/Unit0001/markup", nil, func(r *http.Request) {
		r.Header.Set("Origin", "https://kabbalahmedia.info")
		r.Header.Set("Access-Control-Request-Method", "POST")
		r.Header.Set("Access-Control-Request-Headers", "X-API-Key")
	})
	check(t, rec.Code, 204)
	check(t, rec.Header().Get("Access-Control-Allow-Headers"), "X-API-Key")
}

const vtt = "WEBVTT\n\n1\n00:00:01.000 --> 00:00:02.500\n.שלום לכם\n\n2\n00:00:02.500 --> 00:00:04.000\n<i>חברים</i>\n"

func TestCheckedSubtitlesAreTheWordsNoTranscriptionJob(t *testing.T) {
	e := newEnv(t)
	e.mdb["Unit0001/he"] = "Vtt00001"
	e.km.files["Vtt00001"] = vtt
	run := new(models.Run)
	check(t, e.do("POST", "/cus/Unit0001/markup", run).Code, 202)
	check(t, []interface{}{run.Stage, run.WordsSource, *run.VttFileUID, run.Jobs},
		[]interface{}{"speakers", "vtt", "Vtt00001", models.Jobs{Speakers: "s1"}})
	check(t, len(e.ai.calls), 1)
	c := e.ai.calls[0]
	check(t, []string{c[0], c[1], c[2], c[4]},
		[]string{"speakers", "https://cdn.kabbalahmedia.info/AudioOr1", "he", "Unit0001"})
	check(t, c[3], "1\n00:00:01,000 --> 00:00:02,500\nשלום לכם.\n\n2\n00:00:02,500 --> 00:00:04,000\nחברים\n\n")
	e.ai.jobs["s1"].Status = "done"
	check(t, e.run("/cus/Unit0001/runs/latest").Status, "ready")
	doc := new(models.Document)
	e.do("GET", "/cus/Unit0001/markup", doc)
	check(t, doc.Run.WordsSource, "vtt")
}

func TestSubtitlesInAnotherLanguageOrUnusableGoToTheSTT(t *testing.T) {
	e := newEnv(t)
	e.mdb["Unit0001/ru"] = "Vtt00001"
	run := new(models.Run)
	e.do("POST", "/cus/Unit0001/markup", run)
	check(t, []interface{}{run.Stage, run.WordsSource, run.VttFileUID}, []interface{}{"words", "stt", (*string)(nil)})

	e.mdb["Unit0001/he"] = "Vtt00002"
	e.km.files["Vtt00002"] = "WEBVTT\n\n"
	e.do("POST", "/cus/Unit0001/markup?force=true", run)
	check(t, []interface{}{run.Stage, run.WordsSource, run.VttFileUID}, []interface{}{"words", "stt", (*string)(nil)})

	e.mdb["Unit0001/he"] = "Vtt00003" // not on the CDN
	check(t, e.do("POST", "/cus/Unit0001/markup?force=true", nil).Code, 502)
}

func TestMDBDownFailsTheStart(t *testing.T) {
	e := newEnv(t)
	e.km.unit.ID = "Broken01"
	km := *e.km
	e.p.Kmedia = &brokenKmedia{&km}
	check(t, e.do("POST", "/cus/Broken01/markup", nil).Code, 502)
}

type brokenKmedia struct{ *fakeKmedia }

func (k *brokenKmedia) FetchUnit(uid string) (*kmedia.Unit, error) { return k.unit, nil }

type rebuilt struct {
	Rev        int                    `json:"rev"`
	Unchanged  bool                   `json:"unchanged"`
	Current    bool                   `json:"current"`
	WordsRunID string                 `json:"words_run_id"`
	Source     string                 `json:"source"`
	Transcript *models.FileRef        `json:"transcript"`
	Stats      map[string]interface{} `json:"stats"`
}

func TestRebuildFromTheKeptWordsAndDiarization(t *testing.T) {
	e := newEnv(t)
	check(t, e.do("POST", "/cus/Unit0001/rebuild", nil).Code, 404) // nothing to rebuild from
	first := new(models.Run)
	e.do("POST", "/cus/Unit0001/markup", first)
	e.finish()
	calls := len(e.ai.calls)

	// the same input: no new revision
	out := new(rebuilt)
	check(t, e.do("POST", "/cus/Unit0001/rebuild", out).Code, 200)
	check(t, []interface{}{out.Rev, out.Unchanged, out.Current, out.WordsRunID, out.Source, *out.Transcript},
		[]interface{}{1, true, true, first.ID, "transcript", models.FileRef{FileUID: "TextDoc1"}})

	// the transcript is gone from kmedia: built from the audio, a new revision
	files := e.km.unit.Files
	e.km.unit.Files = files[:2]
	out = new(rebuilt)
	check(t, e.do("POST", "/cus/Unit0001/rebuild", out).Code, 201)
	check(t, []interface{}{out.Rev, out.Unchanged, out.Current, out.Source, out.Transcript},
		[]interface{}{2, false, true, "audio", (*models.FileRef)(nil)})
	doc := new(models.Document)
	e.do("GET", "/cus/Unit0001/markup", doc)
	check(t, []interface{}{doc.Segmentation.Rev, doc.Run.Source, doc.Transcript, doc.Paragraphs[0].Text != nil},
		[]interface{}{2, "audio", (*models.FileRef)(nil), true})

	// a new transcript file in kmedia: the document names it, not the run's
	e.km.unit.Files = append(files[:2:2], kmedia.File{ID: "TextNew1", Type: "text", Language: "he", Name: "heb_o_y.docx"})
	e.do("POST", "/cus/Unit0001/rebuild", out)
	check(t, []interface{}{out.Rev, out.Source}, []interface{}{3, "transcript"})
	e.do("GET", "/cus/Unit0001/markup", doc)
	check(t, *doc.Transcript, models.FileRef{FileUID: "TextNew1"})

	// a transcript that cannot be had is an error, not a rebuild from the audio
	e.km.noDoc = true
	check(t, e.do("POST", "/cus/Unit0001/rebuild", nil).Code, 502)
	e.km.noDoc = false
	check(t, len(e.ai.calls), calls) // no ai-trns
	runs, _ := e.p.Store.Runs("Unit0001")
	check(t, len(runs), 1)

	// a human revision stays current
	if err := e.p.Store.AddSegmentation(&models.Segmentation{CuUID: "Unit0001", FileUID: "AudioOr1",
		WordsRunID: first.ID, Source: "human", Data: models.SegmentationData{Source: "audio"}}); err != nil {
		t.Fatal(err)
	}
	e.km.transcript = "<p>" + strings.Join(paras[:10], "</p><p>") + "</p>"
	e.do("POST", "/cus/Unit0001/rebuild", out)
	check(t, []interface{}{out.Rev, out.Unchanged, out.Current}, []interface{}{5, false, false})
	e.do("GET", "/cus/Unit0001/markup", doc)
	check(t, []interface{}{doc.Segmentation.Rev, doc.Segmentation.Source}, []interface{}{4, "human"})
}

func TestRebuildFromAGivenRun(t *testing.T) {
	e := newEnv(t)
	first := new(models.Run)
	e.do("POST", "/cus/Unit0001/markup", first)
	e.finish()
	second := new(models.Run)
	e.do("POST", "/cus/Unit0001/markup?force=true", second)
	// the latest run is pending: the latest ready one is used
	out := new(rebuilt)
	e.do("POST", "/cus/Unit0001/rebuild", out)
	check(t, out.WordsRunID, first.ID)
	check(t, e.do("POST", "/cus/Unit0001/rebuild?run="+second.ID, nil).Code, 409)
	check(t, e.do("POST", "/cus/Unit0001/rebuild?run=nosuchrun", nil).Code, 404)
	check(t, e.do("POST", "/cus/Unit0002/rebuild?run="+first.ID, nil).Code, 404)
	e.do("POST", "/cus/Unit0001/rebuild?run="+first.ID, out)
	check(t, []interface{}{out.WordsRunID, out.Unchanged}, []interface{}{first.ID, true})
	// only from this host, like a run
	check(t, e.do("POST", "/cus/Unit0001/rebuild", nil, func(r *http.Request) { r.RemoteAddr = "203.0.113.7:1" }).Code, 403)
}

func TestATranscriptThatCannotBeHadAtARunsEndGivesTheAudioNotAFailure(t *testing.T) {
	e := newEnv(t)
	e.do("POST", "/cus/Unit0001/markup", nil)
	e.km.noDoc = true
	e.finish()
	check(t, e.run("/cus/Unit0001/runs/latest").Status, "ready")
	doc := new(models.Document)
	e.do("GET", "/cus/Unit0001/markup", doc)
	check(t, []interface{}{doc.Run.Source, doc.Transcript}, []interface{}{"audio", (*models.FileRef)(nil)})
}
