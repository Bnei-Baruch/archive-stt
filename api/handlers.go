package api

import (
	"errors"
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
