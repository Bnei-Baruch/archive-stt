// Package aitrns: ai-trns over HTTP. Two jobs per lesson, nothing else is used:
//
//  1. POST /jobs            the audio's words (result as SRT: the cues the next job is aligned to)
//  2. POST /diarize/words   the same audio + those cues + the lesson's uid (ai-trns finds the sources itself) ->
//     GET /jobs/{id}/result?format=paragraphs: [{speaker, label, reading, start, end, text,
//     words: [{word, start, end}]}] -- the time codes and the speakers.
//
// ai-trns deletes a finished job after a few days, so the answer is saved (store) as soon as it is ready.
package aitrns

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const Done = "done"

// Final job statuses
var Final = map[string]bool{"done": true, "error": true, "cancelled": true}

// Error is ai-trns down or not answering: transient for a running run (it is tried again at the next tick)
type Error struct{ msg string }

func (e *Error) Error() string { return e.msg }

func errorf(format string, a ...interface{}) error { return &Error{fmt.Sprintf(format, a...)} }

// Refused is ai-trns refusing the request itself (400, 413, 422, ...): asking again gets the same answer, so the
// run fails
type Refused struct{ msg string }

func (e *Refused) Error() string { return e.msg }

// transientStatus: ai-trns down, overloaded or its key not set up yet -- fixed on its side, the run waits
func transientStatus(status int) bool {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusRequestTimeout, http.StatusTooManyRequests:
		return true
	}
	return status >= 500
}

// Job is an ai-trns job's state
type Job struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

type Client struct {
	URL  string
	Key  string // a service key of ai-trns (X-API-Key)
	HTTP *http.Client
}

func New(baseURL, key string, timeout time.Duration) *Client {
	return &Client{URL: strings.TrimRight(baseURL, "/"), Key: key, HTTP: &http.Client{Timeout: timeout}}
}

// call returns the body and status; 404 is passed through, other errors are *Error
func (c *Client) call(method, path, contentType string, body io.Reader) ([]byte, int, error) {
	req, err := http.NewRequest(method, c.URL+path, body)
	if err != nil {
		return nil, 0, err
	}
	if c.Key != "" {
		req.Header.Set("X-API-Key", c.Key)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, errorf("ai-trns: %v", err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, 0, errorf("ai-trns: %v", err)
	}
	if res.StatusCode >= 400 && res.StatusCode != http.StatusNotFound {
		text := string(b)
		if len(text) > 200 {
			text = text[:200]
		}
		msg := fmt.Sprintf("ai-trns %s %s: %d %s", method, path, res.StatusCode, text)
		if !transientStatus(res.StatusCode) {
			return nil, res.StatusCode, &Refused{msg}
		}
		return nil, res.StatusCode, &Error{msg}
	}
	return b, res.StatusCode, nil
}

func jobID(b []byte) (string, error) {
	var j Job
	if err := json.Unmarshal(b, &j); err != nil || j.ID == "" {
		return "", errorf("ai-trns: no job id in %.200s", string(b))
	}
	return j.ID, nil
}

// CreateWordsJob starts the transcription of the audio at audioURL
func (c *Client) CreateWordsJob(audioURL, language string) (string, error) {
	form := url.Values{"url": {audioURL}, "lang": {language}, "format": {"srt"}, "fix_rtl": {"false"}}
	b, _, err := c.call(http.MethodPost, "/jobs", "application/x-www-form-urlencoded",
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	return jobID(b)
}

// CreateSpeakersJob starts the speakers and paragraphs of the audio, aligned to the words job's SRT
func (c *Client) CreateSpeakersJob(audioURL, language, srt, uid string) (string, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range map[string]string{"url": audioURL, "lang": language, "paragraphs": "true", "uid": uid} {
		if err := w.WriteField(k, v); err != nil {
			return "", err
		}
	}
	part, err := w.CreatePart(map[string][]string{
		"Content-Disposition": {`form-data; name="srt"; filename="cues.srt"`},
		"Content-Type":        {"application/x-subrip"},
	})
	if err != nil {
		return "", err
	}
	if _, err := part.Write([]byte(srt)); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	b, _, err := c.call(http.MethodPost, "/diarize/words", w.FormDataContentType(), &body)
	if err != nil {
		return "", err
	}
	return jobID(b)
}

// Job is the job, or nil if ai-trns no longer has it
func (c *Client) Job(id string) (*Job, error) {
	b, status, err := c.call(http.MethodGet, "/jobs/"+url.PathEscape(id), "", nil)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, nil
	}
	j := new(Job)
	if err := json.Unmarshal(b, j); err != nil {
		return nil, errorf("ai-trns: job %s: %v", id, err)
	}
	return j, nil
}

// Result is a finished job's result in a format (srt, paragraphs)
func (c *Client) Result(id, format string) ([]byte, error) {
	q := url.Values{"format": {format}, "fix_rtl": {"false"}}
	b, status, err := c.call(http.MethodGet, "/jobs/"+url.PathEscape(id)+"/result?"+q.Encode(), "", nil)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, errorf("ai-trns: no result %s of job %s", format, id)
	}
	return b, nil
}
