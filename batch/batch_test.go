package batch

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeServer: each lesson has a status; a started one becomes ready after `steps` checks
type fakeServer struct {
	mu      sync.Mutex
	status  map[string]string // uid -> pending | ready | failed; absent: no run
	checks  map[string]int
	starts  []string
	steps   int
	missing map[string]bool   // POST -> 404
	words   map[string]string // uid -> words_source of its ready run
	rebuilt []string
}

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/") // cus/{uid}/...
	uid := parts[1]
	if r.Method == http.MethodPost && parts[2] == "rebuild" {
		if f.missing[uid] {
			w.WriteHeader(404)
			json.NewEncoder(w).Encode(map[string]string{"detail": "no ready run"})
			return
		}
		f.rebuilt = append(f.rebuilt, uid)
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]interface{}{"rev": 2, "unchanged": uid == "Same0001",
			"current": uid != "Human001"})
		return
	}
	if r.Method == http.MethodPost {
		if f.missing[uid] {
			w.WriteHeader(404)
			json.NewEncoder(w).Encode(map[string]string{"detail": "no kmedia unit " + uid})
			return
		}
		f.starts = append(f.starts, uid+"?"+r.URL.RawQuery)
		f.status[uid] = "pending"
		f.checks[uid] = 0
		w.WriteHeader(202)
		json.NewEncoder(w).Encode(map[string]string{"status": "pending", "stage": "words"})
		return
	}
	st, ok := f.status[uid]
	if !ok {
		w.WriteHeader(404)
		json.NewEncoder(w).Encode(map[string]string{"detail": "no such run"})
		return
	}
	if st == "pending" {
		f.checks[uid]++
		if f.checks[uid] > f.steps {
			st = "ready"
			if uid == "Fails001" {
				st = "failed"
			}
			f.status[uid] = st
		}
	}
	out := map[string]interface{}{"status": st, "stage": "speakers", "words_source": f.words[uid]}
	if st == "failed" {
		out["error"] = "speakers: boom\nline two"
	}
	json.NewEncoder(w).Encode(out)
}

func setup(t *testing.T, f *fakeServer) (*Runner, *bytes.Buffer) {
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	out := new(bytes.Buffer)
	return &Runner{Base: srv.URL, StatusBase: srv.URL, Parallel: 2, Poll: time.Millisecond,
		OKPath: filepath.Join(dir, "ok.txt"), FailedPath: filepath.Join(dir, "failed.txt"), Out: out,
		HTTP: srv.Client()}, out
}

func read(t *testing.T, p string) string {
	b, err := os.ReadFile(p)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

func eq(t *testing.T, got, want interface{}) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("\ngot  %#v\nwant %#v", got, want)
	}
}

func TestABatch(t *testing.T) {
	f := &fakeServer{status: map[string]string{"Ready001": "ready", "Pend0001": "pending"}, checks: map[string]int{},
		steps: 2, missing: map[string]bool{"Missing1": true}}
	r, out := setup(t, f)
	os.WriteFile(r.OKPath, []byte("InOk0001\n"), 0o644)
	os.WriteFile(r.FailedPath, []byte("New00001\told error\n"), 0o644)

	todo, err := r.Prepare([]string{"InOk0001", "Ready001", "Pend0001", "New00001", "Missing1", "Fails001"})
	if err != nil {
		t.Fatal(err)
	}
	eq(t, todo, []string{"Ready001", "Pend0001", "New00001", "Missing1", "Fails001"})
	eq(t, r.Skipped, []string{"InOk0001"})
	if err := r.Run(todo); err != nil {
		t.Fatal(err)
	}
	r.Summary()

	// ready on the server: not started; pending: waited for, not started
	eq(t, f.starts, []string{"New00001?force=false", "Fails001?force=false"})
	eq(t, r.Passed, []string{"Ready001", "Pend0001", "New00001"})
	eq(t, r.Failed, []string{"Missing1", "Fails001"})
	eq(t, read(t, r.OKPath), "InOk0001\nReady001\nPend0001\nNew00001\n")
	// New00001 passed: out of the failed list; the errors on one line
	eq(t, read(t, r.FailedPath), "Fails001\tspeakers: boom line two\nMissing1\t404 no kmedia unit Missing1\n")
	if !strings.Contains(out.String(), "done: 3 ok, 2 failed, 1 skipped") {
		t.Fatal(out.String())
	}
}

func TestForceAndLanguage(t *testing.T) {
	f := &fakeServer{status: map[string]string{"Ready001": "ready"}, checks: map[string]int{}}
	r, _ := setup(t, f)
	os.WriteFile(r.OKPath, []byte("Ready001\n"), 0o644)
	r.Force, r.Language = true, "ru"
	todo, _ := r.Prepare([]string{"Ready001"})
	eq(t, todo, []string{"Ready001"})
	if err := r.Run(todo); err != nil {
		t.Fatal(err)
	}
	eq(t, f.starts, []string{"Ready001?force=true&language=ru"})
	eq(t, read(t, r.OKPath), "Ready001\n") // listed once
}

func TestADryRunStartsNothing(t *testing.T) {
	f := &fakeServer{status: map[string]string{}, checks: map[string]int{}}
	r, out := setup(t, f)
	r.DryRun = true
	todo, _ := r.Prepare([]string{"New00001"})
	if err := r.Run(todo); err != nil {
		t.Fatal(err)
	}
	eq(t, len(f.starts), 0)
	eq(t, read(t, r.OKPath), "")
	if !strings.Contains(out.String(), "New00001: would start") {
		t.Fatal(out.String())
	}
}

func TestStopWaitsForTheStartedAndStartsNoMore(t *testing.T) {
	f := &fakeServer{status: map[string]string{}, checks: map[string]int{}, steps: 3}
	r, _ := setup(t, f)
	r.Parallel = 1
	stop := make(chan struct{})
	close(stop)
	r.Stop = stop
	todo, _ := r.Prepare([]string{"New00001", "New00002"})
	if err := r.Run(todo); err != nil {
		t.Fatal(err)
	}
	eq(t, len(f.starts), 0)
	eq(t, r.NotDone, []string{"New00001", "New00002"})
}

func TestANetworkErrorIsWaitedOut(t *testing.T) {
	r := &Runner{Base: "http://127.0.0.1:1", StatusBase: "http://127.0.0.1:1", Parallel: 1, Poll: time.Millisecond,
		OKPath: filepath.Join(t.TempDir(), "ok"), FailedPath: filepath.Join(t.TempDir(), "failed"),
		Out: new(bytes.Buffer), HTTP: &http.Client{Timeout: time.Second}}
	abort := make(chan struct{})
	r.Abort = abort
	todo, _ := r.Prepare([]string{"New00001"})
	done := make(chan error)
	go func() { done <- r.Run(todo) }()
	time.Sleep(30 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("returned on a network error: %v", err)
	default:
	}
	close(abort)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	eq(t, len(r.Failed), 0)
}

func TestReadUIDs(t *testing.T) {
	p := filepath.Join(t.TempDir(), "list.txt")
	os.WriteFile(p, []byte("# a comment\nFile0001\tsome note\n\nFile0002\nUid00001\n"), 0o644)
	got, err := ReadUIDs([]string{"Uid00001", p, "-"}, strings.NewReader("Stdin001\nFile0001\n"))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, got, []string{"Uid00001", "File0001", "File0002", "Stdin001"})
}

func TestRebuild(t *testing.T) {
	f := &fakeServer{status: map[string]string{}, checks: map[string]int{}, missing: map[string]bool{"Missing1": true}}
	r, out := setup(t, f)
	r.Rebuild = true
	os.WriteFile(r.OKPath, []byte("Done0001\n"), 0o644)
	todo, _ := r.Prepare([]string{"Done0001", "New00001", "Same0001", "Human001", "Missing1"})
	if err := r.Run(todo); err != nil {
		t.Fatal(err)
	}
	eq(t, f.rebuilt, []string{"New00001", "Same0001", "Human001"})
	eq(t, f.starts, []string(nil))
	eq(t, r.Passed, []string{"New00001", "Same0001", "Human001"})
	eq(t, read(t, r.FailedPath), "Missing1\t404 no ready run\n")
	for _, s := range []string{"Same0001: ok -- rev 2 unchanged", "Human001: ok -- rev 2, not current"} {
		if !strings.Contains(out.String(), s) {
			t.Fatal(out.String())
		}
	}
}

func TestRedoSTTRunsAgainOnlyTheReadyFromTheSTT(t *testing.T) {
	f := &fakeServer{status: map[string]string{"Stt00001": "ready", "Vtt00001": "ready"}, checks: map[string]int{},
		words: map[string]string{"Stt00001": "stt", "Vtt00001": "vtt"}}
	r, _ := setup(t, f)
	r.RedoSTT = true
	os.WriteFile(r.OKPath, []byte("Stt00001\nVtt00001\n"), 0o644) // the ok list does not skip them
	todo, _ := r.Prepare([]string{"Stt00001", "Vtt00001", "New00001"})
	if err := r.Run(todo); err != nil {
		t.Fatal(err)
	}
	eq(t, f.starts, []string{"Stt00001?force=false", "New00001?force=false"})
	eq(t, len(r.Passed), 3)
}
