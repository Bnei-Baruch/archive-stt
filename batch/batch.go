// Package batch starts markup runs on an archive-stt server for a list of lessons, a few at a time, waits for
// them and keeps two lists: the lessons marked up (ok) and those that failed, with the error.
//
// Before a start the server is asked about the lesson: ready -> listed as ok, not run again; pending -> waited
// for. A lesson in the ok list is not even asked about. Force runs them all again; RedoSTT runs again those whose
// ready markup has its words from the STT (the server takes checked subtitles where the lesson has them).
// Rebuild: no runs -- each lesson's segmentation is rebuilt on the server from its kept words (no ai-trns), one
// request per lesson.
package batch

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

// Runner is one batch
type Runner struct {
	Base       string // where runs are started (the tunnel or the service itself)
	StatusBase string // where they are watched (the same, or the public proxy for a dry run)
	Language   string // "" -- the lesson's original language
	Force      bool
	RedoSTT    bool // a ready lesson whose words are from the STT is run again
	Rebuild    bool // POST /cus/{uid}/rebuild instead of a run
	Parallel   int
	Poll       time.Duration
	DryRun     bool
	OKPath     string
	FailedPath string
	Out        io.Writer
	HTTP       *http.Client
	// Stop: no new starts, the started ones are waited for (the first Ctrl+C); Abort: leave at once (the second)
	Stop, Abort <-chan struct{}

	ok      map[string]bool
	failed  map[string]string
	running map[string]time.Time
	order   []string // running, in start order (map order is random)
	Passed  []string
	Failed  []string
	Skipped []string
	NotDone []string
	total   int
}

// netErr is the network or the tunnel failing for a moment: the batch waits and goes on
type netErr struct{ err error }

func (e *netErr) Error() string { return e.err.Error() }

// run is the server's answer: a run, an error (detail) or a rebuild (rev, unchanged, current)
type run struct {
	Status      string  `json:"status"`
	Stage       string  `json:"stage"`
	WordsSource string  `json:"words_source"`
	Error       *string `json:"error"`
	Detail      string  `json:"detail"`
	Rev         int     `json:"rev"`
	Unchanged   bool    `json:"unchanged"`
	Current     bool    `json:"current"`
}

func (r *Runner) logf(format string, a ...interface{}) {
	fmt.Fprintf(r.Out, "%s %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, a...))
}

func (r *Runner) call(method, u string) (int, *run, error) {
	req, err := http.NewRequest(method, u, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("User-Agent", "archive-stt-markup")
	res, err := r.HTTP.Do(req)
	if err != nil {
		return 0, nil, &netErr{err}
	}
	defer res.Body.Close()
	body := new(run)
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return 0, nil, &netErr{err}
	}
	_ = json.Unmarshal(b, body) // a non-JSON answer: an empty one
	return res.StatusCode, body, nil
}

func looksLikeUID(s string) bool {
	if len(s) != 8 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// ReadUIDs reads the sources: a uid itself, "-" for stdin, or a file with a uid per line (after a TAB -- anything,
// # -- a comment); in order, without repeats
func ReadUIDs(sources []string, stdin io.Reader) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	add := func(line string) {
		uid := strings.TrimSpace(strings.SplitN(line, "\t", 2)[0])
		if uid != "" && !strings.HasPrefix(uid, "#") && !seen[uid] {
			seen[uid] = true
			out = append(out, uid)
		}
	}
	for _, src := range sources {
		if looksLikeUID(src) {
			add(src)
			continue
		}
		var in io.Reader = stdin
		if src != "-" {
			f, err := os.Open(src)
			if err != nil {
				return nil, err
			}
			defer f.Close()
			in = f
		}
		sc := bufio.NewScanner(in)
		for sc.Scan() {
			add(sc.Text())
		}
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// readList is uid -> the rest of its line (an error), from a list file; empty if there is none
func readList(path string) (map[string]string, []string, error) {
	out := map[string]string{}
	var order []string
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return out, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		uid := strings.TrimSpace(parts[0])
		if _, dup := out[uid]; !dup {
			order = append(order, uid)
		}
		out[uid] = ""
		if len(parts) > 1 {
			out[uid] = parts[1]
		}
	}
	return out, order, sc.Err()
}

func (r *Runner) saveFailed() error {
	uids := make([]string, 0, len(r.failed))
	for uid := range r.failed {
		uids = append(uids, uid)
	}
	sort.Strings(uids)
	var b strings.Builder
	for _, uid := range uids {
		fmt.Fprintf(&b, "%s\t%s\n", uid, r.failed[uid])
	}
	if err := os.WriteFile(r.FailedPath+".tmp", []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(r.FailedPath+".tmp", r.FailedPath)
}

func (r *Runner) progress() string {
	return fmt.Sprintf("[%d/%d]", len(r.Passed)+len(r.Failed), r.total)
}

func (r *Runner) took(uid string) string {
	t0, ok := r.running[uid]
	r.stopWaiting(uid)
	if !ok {
		return ""
	}
	return fmt.Sprintf(" (%ds)", int(time.Since(t0).Seconds()))
}

func (r *Runner) wait(uid string) {
	r.running[uid] = time.Now()
	r.order = append(r.order, uid)
}

func (r *Runner) stopWaiting(uid string) {
	delete(r.running, uid)
	for i, u := range r.order {
		if u == uid {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
}

func (r *Runner) markOK(uid, note string) error {
	secs := r.took(uid)
	r.Passed = append(r.Passed, uid)
	if !r.ok[uid] {
		r.ok[uid] = true
		f, err := os.OpenFile(r.OKPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(f, uid)
		f.Close()
		if err != nil {
			return err
		}
	}
	if _, was := r.failed[uid]; was {
		delete(r.failed, uid)
		if err := r.saveFailed(); err != nil {
			return err
		}
	}
	if note != "" {
		note = " -- " + note
	}
	r.logf("%s: ok%s%s   %s", uid, secs, note, r.progress())
	return nil
}

func (r *Runner) markFailed(uid, msg string) error {
	secs := r.took(uid)
	r.Failed = append(r.Failed, uid)
	msg = strings.Join(strings.Fields(msg), " ")
	if msg == "" {
		msg = "failed"
	}
	r.failed[uid] = msg
	r.logf("%s: FAILED%s -- %s   %s", uid, secs, msg, r.progress())
	return r.saveFailed()
}

// Prepare reads the lists and splits the uids into those to run and those skipped (in the ok list); it returns
// those to run
func (r *Runner) Prepare(uids []string) ([]string, error) {
	var err error
	okMap, _, err := readList(r.OKPath)
	if err != nil {
		return nil, err
	}
	r.ok = map[string]bool{}
	for uid := range okMap {
		r.ok[uid] = true
	}
	if r.failed, _, err = readList(r.FailedPath); err != nil {
		return nil, err
	}
	r.running = map[string]time.Time{}
	var todo []string
	for _, uid := range uids {
		if r.ok[uid] && !r.Force && !r.RedoSTT {
			r.Skipped = append(r.Skipped, uid)
		} else {
			todo = append(todo, uid)
		}
	}
	r.total = len(todo)
	return todo, nil
}

func (r *Runner) startURL(uid string) string {
	q := url.Values{"force": {fmt.Sprint(r.Force)}}
	if r.Language != "" {
		q.Set("language", r.Language)
	}
	return r.Base + "/cus/" + uid + "/markup?" + q.Encode()
}

// rebuildNext rebuilds the next lesson's segmentation; a network error leaves it in todo
func (r *Runner) rebuildNext(todo []string) ([]string, error) {
	uid := todo[0]
	if r.DryRun {
		r.logf("%s: would rebuild", uid)
		return todo[1:], nil
	}
	code, out, err := r.call(http.MethodPost, r.Base+"/cus/"+uid+"/rebuild")
	if err != nil {
		return todo, err
	}
	todo = todo[1:]
	switch {
	case code == http.StatusOK || code == http.StatusCreated:
		note := fmt.Sprintf("rev %d", out.Rev)
		if out.Unchanged {
			note += " unchanged"
		}
		if !out.Current {
			note += ", not current (a human revision is)"
		}
		return todo, r.markOK(uid, note)
	default: // 404 no ready run, 409 its words or diarization not kept, 502 kmedia
		return todo, r.markFailed(uid, fmt.Sprintf("%d %s", code, out.Detail))
	}
}

// startNext checks the next lesson and starts it; a network error leaves it in todo
func (r *Runner) startNext(todo []string) ([]string, error) {
	if r.Rebuild {
		return r.rebuildNext(todo)
	}
	uid := todo[0]
	code, latest, err := r.call(http.MethodGet, r.StatusBase+"/cus/"+uid+"/runs/latest")
	if err != nil {
		return todo, err
	}
	todo = todo[1:]
	redo := r.RedoSTT && latest.WordsSource == "stt"
	if code == http.StatusOK && !r.Force && !(redo && latest.Status == "ready") {
		switch latest.Status {
		case "ready":
			return todo, r.markOK(uid, "ready on the server already ("+latest.WordsSource+")")
		case "pending":
			r.logf("%s: pending already (%s), waiting", uid, latest.Stage)
			r.wait(uid)
			return todo, nil
		}
	}
	if r.DryRun {
		r.logf("%s: would start", uid)
		return todo, nil
	}
	code, started, err := r.call(http.MethodPost, r.startURL(uid))
	if err != nil {
		return append([]string{uid}, todo...), err
	}
	if code == http.StatusAccepted {
		r.logf("%s: started (%s)", uid, started.Stage)
		r.wait(uid)
		return todo, nil
	}
	// 404 no such unit, 422 no audio in that language, ...
	return todo, r.markFailed(uid, fmt.Sprintf("%d %s", code, started.Detail))
}

func (r *Runner) check() error {
	for _, uid := range append([]string(nil), r.order...) {
		code, latest, err := r.call(http.MethodGet, r.StatusBase+"/cus/"+uid+"/runs/latest")
		if err != nil {
			return err
		}
		if code != http.StatusOK {
			continue
		}
		switch latest.Status {
		case "ready":
			if err := r.markOK(uid, latest.WordsSource); err != nil {
				return err
			}
		case "failed":
			msg := "failed"
			if latest.Error != nil {
				msg = *latest.Error
			}
			if err := r.markFailed(uid, msg); err != nil {
				return err
			}
		}
	}
	return nil
}

func closed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// pause waits d; false if aborted meanwhile
func (r *Runner) pause(d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-r.Abort:
		return false
	}
}

// Run works through todo; it returns when all are done, or on Abort (the started runs go on on the server)
func (r *Runner) Run(todo []string) error {
	stopLogged := false
	for {
		if closed(r.Abort) {
			return r.leave()
		}
		if closed(r.Stop) && !stopLogged {
			stopLogged = true
			r.NotDone, todo = append(r.NotDone, todo...), nil
			r.logf("stopping: no new runs; waiting for %d (Ctrl+C again to leave them)", len(r.running))
		}
		var err error
		for len(todo) > 0 && len(r.running) < r.Parallel && !closed(r.Stop) && err == nil {
			todo, err = r.startNext(todo)
		}
		if err == nil {
			if len(r.running) == 0 {
				if len(todo) == 0 {
					return nil
				}
				continue
			}
			select {
			case <-time.After(r.Poll):
			case <-r.Stop:
				if !stopLogged {
					continue
				}
				if !r.pause(r.Poll) {
					return r.leave()
				}
			case <-r.Abort:
				return r.leave()
			}
			err = r.check()
		}
		var ne *netErr
		if errors.As(err, &ne) {
			r.logf("network: %v; again in %s", err, r.Poll)
			if !r.pause(r.Poll) {
				return r.leave()
			}
		} else if err != nil {
			return err // a list file could not be written
		}
	}
}

func (r *Runner) leave() error {
	r.logf("left running on the server: %s", strings.Join(r.order, ", "))
	return nil
}

// Summary prints what this batch did and the lists' sizes
func (r *Runner) Summary() {
	notStarted := ""
	if len(r.NotDone) > 0 {
		notStarted = fmt.Sprintf(", %d not started", len(r.NotDone))
	}
	r.logf("done: %d ok, %d failed, %d skipped (in %s)%s", len(r.Passed), len(r.Failed), len(r.Skipped),
		r.OKPath, notStarted)
	if len(r.Passed) > 0 {
		fmt.Fprintf(r.Out, "\nok (%d): %s\n", len(r.Passed), strings.Join(r.Passed, " "))
	}
	if len(r.Failed) > 0 {
		fmt.Fprintf(r.Out, "\nfailed (%d):\n", len(r.Failed))
		for _, uid := range r.Failed {
			fmt.Fprintf(r.Out, "  %s\t%s\n", uid, r.failed[uid])
		}
	}
	fmt.Fprintf(r.Out, "\nall: %d in %s, %d in %s\n", len(r.ok), r.OKPath, len(r.failed), r.FailedPath)
}
