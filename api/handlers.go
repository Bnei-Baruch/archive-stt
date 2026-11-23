package api

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/Bnei-Baruch/archive-stt/common"
	"github.com/Bnei-Baruch/archive-stt/kmedia"
	"github.com/Bnei-Baruch/archive-stt/pipeline"
)

type Handlers struct {
	Pipeline *pipeline.Pipeline
	APIKeys  map[string]string // name -> key; empty: runs are started from this host only
}

func uidParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	uid := strings.TrimSpace(r.PathValue("uid"))
	if !kmedia.UIDRe.MatchString(uid) {
		fail(w, http.StatusBadRequest, "uid: a kmedia content unit id (8 letters and digits)")
		return "", false
	}
	return uid, true
}

// StartMarkup: POST /cus/{uid}/markup?language=&force=false -> 202 + the run
func (h *Handlers) StartMarkup(w http.ResponseWriter, r *http.Request) {
	author, ok := h.mayStart(w, r)
	if !ok {
		return
	}
	uid, ok := uidParam(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	language := q.Get("language")
	if language != "" && !common.IsLanguage(language) {
		fail(w, http.StatusBadRequest, "language: one of "+strings.Join(common.Languages, ", "))
		return
	}
	force := false
	if s := q.Get("force"); s != "" {
		var err error
		if force, err = strconv.ParseBool(s); err != nil {
			fail(w, http.StatusBadRequest, "force: true or false")
			return
		}
	}
	run, err := h.Pipeline.Start(uid, language, force, author)
	var se *pipeline.StartError
	if errors.As(err, &se) {
		fail(w, se.Status, se.Msg)
		return
	}
	if err != nil {
		internal(w, r, err)
		return
	}
	w.Header().Set(cacheControl, fresh)
	writeJSON(w, http.StatusAccepted, run)
}

// Rebuild: POST /cus/{uid}/rebuild?run= -> 201 + what was built (200 if the same as before: no new revision)
func (h *Handlers) Rebuild(w http.ResponseWriter, r *http.Request) {
	author, ok := h.mayStart(w, r)
	if !ok {
		return
	}
	uid, ok := uidParam(w, r)
	if !ok {
		return
	}
	out, err := h.Pipeline.Rebuild(uid, r.URL.Query().Get("run"), author)
	var se *pipeline.StartError
	if errors.As(err, &se) {
		fail(w, se.Status, se.Msg)
		return
	}
	if err != nil {
		internal(w, r, err)
		return
	}
	w.Header().Set(cacheControl, fresh)
	status := http.StatusCreated
	if out.Unchanged {
		status = http.StatusOK
	}
	writeJSON(w, status, out)
}

// GetMarkup: GET /cus/{uid}/markup?rev= -> the markup document; 404 until a run is ready
func (h *Handlers) GetMarkup(w http.ResponseWriter, r *http.Request) {
	uid, ok := uidParam(w, r)
	if !ok {
		return
	}
	rev := 0
	if s := r.URL.Query().Get("rev"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			fail(w, http.StatusBadRequest, "rev: a revision number")
			return
		}
		rev = n
	}
	if _, err := h.Pipeline.Sync(uid); err != nil {
		internal(w, r, err)
		return
	}
	doc, err := h.Pipeline.Document(uid, rev)
	if err != nil {
		internal(w, r, err)
		return
	}
	w.Header().Set(cacheControl, public)
	if doc == nil {
		msg := "no markup for this lesson"
		if rev > 0 {
			msg = "no revision " + strconv.Itoa(rev)
		}
		fail(w, http.StatusNotFound, msg)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

// GetTimecode: GET /cus/{uid}/timecode?lang= -> the time of every word of the transcript, by its <p> and its place
// there; 404 without markup, a transcript or in another language (translations: not yet)
func (h *Handlers) GetTimecode(w http.ResponseWriter, r *http.Request) {
	uid, ok := uidParam(w, r)
	if !ok {
		return
	}
	lang, ok := langParam(w, r)
	if !ok {
		return
	}
	if _, err := h.Pipeline.Sync(uid); err != nil {
		internal(w, r, err)
		return
	}
	tc, err := h.Pipeline.Timecode(uid, lang)
	if err != nil {
		internal(w, r, err)
		return
	}
	w.Header().Set(cacheControl, public)
	if tc == nil {
		fail(w, http.StatusNotFound, "no markup for this lesson (in this language)")
		return
	}
	writeJSON(w, http.StatusOK, tc)
}

// MaxQueries: fragments asked in one GET /cus/{uid}/locate
const MaxQueries = 20

// GetLocate: GET /cus/{uid}/locate?q=<first words>|<last words>|<number of words>&q=...&lang= -> where and when
// each fragment of the text is (the transcript's, else the heard words; results in the order asked); 404 without
// markup (in this language)
func (h *Handlers) GetLocate(w http.ResponseWriter, r *http.Request) {
	uid, ok := uidParam(w, r)
	if !ok {
		return
	}
	lang, ok := langParam(w, r)
	if !ok {
		return
	}
	queries := r.URL.Query()["q"]
	if len(queries) == 0 || len(queries) > MaxQueries {
		fail(w, http.StatusBadRequest, "q: 1 to "+strconv.Itoa(MaxQueries)+
			" fragments, <first words>|<last words>|<number of words>")
		return
	}
	if _, err := h.Pipeline.Sync(uid); err != nil {
		internal(w, r, err)
		return
	}
	out, err := h.Pipeline.Locate(uid, lang, queries)
	if err != nil {
		internal(w, r, err)
		return
	}
	w.Header().Set(cacheControl, public)
	if out == nil {
		fail(w, http.StatusNotFound, "no markup for this lesson (in this language)")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// langParam is the lang query parameter ("": the markup's); false: answered 400
func langParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	lang := r.URL.Query().Get("lang")
	if lang != "" && !common.IsLanguage(lang) {
		fail(w, http.StatusBadRequest, "lang: one of "+strings.Join(common.Languages, ", "))
		return "", false
	}
	return lang, true
}

func seconds(s string) (float64, bool) {
	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil && f >= 0 && !math.IsInf(f, 0)
}

// MaxTextSpan: seconds of text one GET /cus/{uid}/text gives
const MaxTextSpan = 900

// GetText: GET /cus/{uid}/text?at=754.3 | ?from=750&to=800, &lang= -> the words, paragraphs and chapters there;
// 404 without markup (in this language)
func (h *Handlers) GetText(w http.ResponseWriter, r *http.Request) {
	uid, ok := uidParam(w, r)
	if !ok {
		return
	}
	lang, ok := langParam(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	from, to := 0.0, -1.0
	switch {
	case q.Has("at") && !q.Has("from") && !q.Has("to"):
		if from, ok = seconds(q.Get("at")); !ok {
			fail(w, http.StatusBadRequest, "at: seconds")
			return
		}
	case q.Has("from") && q.Has("to") && !q.Has("at"):
		var okTo bool
		from, ok = seconds(q.Get("from"))
		to, okTo = seconds(q.Get("to"))
		if !ok || !okTo || to <= from || to-from > MaxTextSpan {
			fail(w, http.StatusBadRequest, "from, to: seconds, from < to, at most "+strconv.Itoa(MaxTextSpan)+" apart")
			return
		}
	default:
		fail(w, http.StatusBadRequest, "at, or from and to: seconds of the media")
		return
	}
	if _, err := h.Pipeline.Sync(uid); err != nil {
		internal(w, r, err)
		return
	}
	out, err := h.Pipeline.Text(uid, lang, from, to)
	if err != nil {
		internal(w, r, err)
		return
	}
	w.Header().Set(cacheControl, public)
	if out == nil {
		fail(w, http.StatusNotFound, "no markup for this lesson (in this language)")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

const (
	// MaxPhrase: characters of a phrase looked for in a lesson
	MaxPhrase = 500
	// MaxFound: places of a phrase given at most (default 10)
	MaxFound = 50
)

// GetFind: GET /cus/{uid}/find?q=&lang=&limit=10 -> where in the lesson the phrase is said, the best first; 404
// without markup (in this language)
func (h *Handlers) GetFind(w http.ResponseWriter, r *http.Request) {
	uid, ok := uidParam(w, r)
	if !ok {
		return
	}
	lang, ok := langParam(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	phrase := strings.TrimSpace(q.Get("q"))
	if phrase == "" || len([]rune(phrase)) > MaxPhrase {
		fail(w, http.StatusBadRequest, "q: a phrase, up to "+strconv.Itoa(MaxPhrase)+" characters")
		return
	}
	limit := 10
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > MaxFound {
			fail(w, http.StatusBadRequest, "limit: 1 to "+strconv.Itoa(MaxFound))
			return
		}
		limit = n
	}
	if _, err := h.Pipeline.Sync(uid); err != nil {
		internal(w, r, err)
		return
	}
	out, err := h.Pipeline.Find(uid, lang, phrase, limit)
	if err != nil {
		internal(w, r, err)
		return
	}
	w.Header().Set(cacheControl, public)
	if out == nil {
		fail(w, http.StatusNotFound, "no markup for this lesson (in this language)")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// GetContentUnits: GET /cus?lang= -> the content units whose current revision has no transcript (their text is the
// heard words: GET /cus/{uid}/content), in lang (none: any); public
func (h *Handlers) GetContentUnits(w http.ResponseWriter, r *http.Request) {
	lang, ok := langParam(w, r)
	if !ok {
		return
	}
	out, err := h.Pipeline.ContentUnits(lang)
	if err != nil {
		internal(w, r, err)
		return
	}
	w.Header().Set(cacheControl, public)
	writeJSON(w, http.StatusOK, out)
}

// GetContent: GET /cus/{uid}/content?lang= -> the lesson's heard words as plain text (paragraphs apart by an empty
// line), for a search engine; public; 404 without markup (in this language)
func (h *Handlers) GetContent(w http.ResponseWriter, r *http.Request) {
	uid, ok := uidParam(w, r)
	if !ok {
		return
	}
	lang, ok := langParam(w, r)
	if !ok {
		return
	}
	if _, err := h.Pipeline.Sync(uid); err != nil {
		internal(w, r, err)
		return
	}
	text, found, err := h.Pipeline.Content(uid, lang)
	if err != nil {
		internal(w, r, err)
		return
	}
	w.Header().Set(cacheControl, public)
	if !found {
		fail(w, http.StatusNotFound, "no markup for this lesson (in this language)")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(text))
}

// GetRuns: GET /cus/{uid}/runs -> the lesson's runs, newest first
func (h *Handlers) GetRuns(w http.ResponseWriter, r *http.Request) {
	uid, ok := uidParam(w, r)
	if !ok {
		return
	}
	if _, err := h.Pipeline.Sync(uid); err != nil {
		internal(w, r, err)
		return
	}
	runs, err := h.Pipeline.Store.Runs(uid)
	if err != nil {
		internal(w, r, err)
		return
	}
	w.Header().Set(cacheControl, fresh)
	writeJSON(w, http.StatusOK, runs)
}

// GetRun: GET /cus/{uid}/runs/{id} -> one run ("latest": the newest, moved on)
func (h *Handlers) GetRun(w http.ResponseWriter, r *http.Request) {
	uid, ok := uidParam(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	run, err := h.Pipeline.Sync(uid)
	if err == nil && id != "latest" {
		run, err = h.Pipeline.Store.GetRun(id)
	}
	if err != nil {
		internal(w, r, err)
		return
	}
	w.Header().Set(cacheControl, fresh)
	if run == nil || run.CuUID != uid {
		fail(w, http.StatusNotFound, "no such run of this lesson")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// Health: GET /health
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	n, err := h.Pipeline.Store.CountRuns()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "runs": n})
}
