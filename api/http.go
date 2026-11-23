package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

const cacheControl = "Cache-Control"

var (
	// the markup is public data of public lessons: any site may read it
	public = "public, max-age=300"
	// a run moves on: no caching
	fresh = "no-store"
)

// writeJSON answers with v as JSON
func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("writing the answer", "err", err)
	}
}

// fail answers with {"detail": msg}, as the clients expect
func fail(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"detail": msg})
}

func internal(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	fail(w, http.StatusInternalServerError, "internal error")
}

// cors lets any site read the API (public data) and start runs with a key
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			h.Set("Access-Control-Allow-Methods", "GET, POST")
			if req := r.Header.Get("Access-Control-Request-Headers"); req != "" {
				h.Set("Access-Control-Allow-Headers", req)
			}
			h.Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// recoverAndLog turns a panic into a 500 and logs the requests that change something or fail
func recoverAndLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			if p := recover(); p != nil {
				slog.Error("panic", "method", r.Method, "path", r.URL.Path, "panic", p)
				fail(sw, http.StatusInternalServerError, "internal error")
			}
			if r.Method != http.MethodGet || sw.status >= 500 {
				slog.Info("request", "method", r.Method, "path", r.URL.Path, "status", sw.status)
			}
		}()
		next.ServeHTTP(sw, r)
	})
}
