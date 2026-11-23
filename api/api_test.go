package api

// The run: kmedia files -> ai-trns words job -> ai-trns speakers job -> markup stored and served. ai-trns and
// kmedia are fakes behind the pipeline's interfaces.

import (
	"encoding/json"
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

type fakeKmedia struct{ unit *kmedia.Unit }

func (k *fakeKmedia) FetchUnit(uid string) (*kmedia.Unit, error) {
	if uid != "Unit0001" {
		return nil, kmedia.ErrNotFound
	}
	return k.unit, nil
}

func (k *fakeKmedia) FetchTranscriptHTML(string) (string, error) {
	return "<p>" + strings.Join(paras, "</p><p>") + "</p>", nil
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
}

func newEnv(t *testing.T) *env {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ai := &fakeAiTrns{jobs: map[string]*aitrns.Job{}}
	km := &fakeKmedia{unit: unit()}
	p := &pipeline.Pipeline{Store: s, AiTrns: ai, Kmedia: km}
	h := &Handlers{Pipeline: p}
	return &env{t: t, p: p, h: h, handler: Handler(h), ai: ai, km: km}
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
	check(t, []interface{}{run.Status, run.Stage, run.Kind, run.FileUID, run.Language, run.WordsSource,
		*run.Data.Transcript, *run.Author},
		[]interface{}{"pending", "words", "first", "AudioOr1", "he", "stt", models.FileRef{FileUID: "TextDoc1"}, "local"})
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
	rebuilt := e.p.BuildSegmentation(run, words, d)
	seg, _ := e.p.Store.Segmentation("Unit0001", 1)
	a, _ := json.Marshal(rebuilt)
	b, _ := json.Marshal(seg.Data)
	check(t, string(a), string(b))
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
