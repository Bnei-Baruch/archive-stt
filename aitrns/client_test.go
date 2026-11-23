package aitrns

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Down, overloaded or not let in yet -- the run waits (*Error); the request itself refused -- it fails (*Refused)
func TestStatusesTransientOrRefused(t *testing.T) {
	for status, transient := range map[int]bool{400: false, 413: false, 422: false, 401: true, 403: true, 408: true,
		429: true, 500: true, 502: true, 503: true} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))
		_, err := New(srv.URL, "", time.Second).Job("j1")
		srv.Close()
		var e *Error
		var refused *Refused
		if transient && !errors.As(err, &e) || !transient && !errors.As(err, &refused) {
			t.Fatalf("%d: %T %v", status, err, err)
		}
	}
	_, err := New("http://127.0.0.1:1", "", time.Second).Job("j1")
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("no connection: %T %v", err, err)
	}
}
