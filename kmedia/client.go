// Package kmedia: a lesson's files by uid and its transcript as kmedia shows it (archive-unzip doc2html).
package kmedia

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// UIDRe is a kmedia (MDB) uid
var UIDRe = regexp.MustCompile(`^[A-Za-z0-9]{8}$`)

// ErrNotFound: kmedia has no such unit
var ErrNotFound = fmt.Errorf("not found")

type File struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	Language   string  `json:"language"`
	MimeType   string  `json:"mimetype"`
	InsertType string  `json:"insert_type"`
	Duration   float64 `json:"duration"`
}

type Unit struct {
	ID       string  `json:"id"`
	Duration float64 `json:"duration"`
	Files    []File  `json:"files"`
}

type Client struct {
	URL    string
	CDNURL string
	HTTP   *http.Client
}

func New(kmediaURL, cdnURL string, timeout time.Duration) *Client {
	return &Client{URL: strings.TrimRight(kmediaURL, "/"), CDNURL: strings.TrimRight(cdnURL, "/"),
		HTTP: &http.Client{Timeout: timeout}}
}

func (c *Client) get(u string, timeout time.Duration) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "archive-stt")
	client := c.HTTP
	if timeout > 0 {
		client = &http.Client{Timeout: timeout}
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	return b, res.StatusCode, err
}

// FetchUnit is the content unit with its files; ErrNotFound if kmedia has no such unit
func (c *Client) FetchUnit(uid string) (*Unit, error) {
	b, status, err := c.get(c.URL+"/backend/content_units/"+url.PathEscape(uid), 0)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if status >= 400 {
		return nil, fmt.Errorf("kmedia: %d", status)
	}
	u := new(Unit)
	return u, json.Unmarshal(b, u)
}

// FetchTranscriptHTML is the transcript file as kmedia shows it
func (c *Client) FetchTranscriptHTML(fileID string) (string, error) {
	b, status, err := c.get(c.URL+"/assets/api/doc2html/"+url.PathEscape(fileID), 60*time.Second)
	if err != nil {
		return "", err
	}
	if status >= 400 {
		return "", fmt.Errorf("doc2html %s: %d", fileID, status)
	}
	return string(b), nil
}

// FetchFile is the file from the CDN (a subtitles file, ...)
func (c *Client) FetchFile(fileID string) ([]byte, error) {
	b, status, err := c.get(c.AudioURL(fileID), 0)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, fmt.Errorf("cdn %s: %d", fileID, status)
	}
	return b, nil
}

// AudioURL is the file on the CDN
func (c *Client) AudioURL(fileID string) string {
	return c.CDNURL + "/" + fileID
}

func isOriginal(f File) bool {
	return strings.Contains(f.Name, "_o_")
}

// LessonFiles are the unit's audio in language (the original recording -- "_o_" in its name -- before a
// translation) and its transcript in that language (a text file, not a PDF, .docx first), or nil
func LessonFiles(u *Unit, language string) (audio, transcript *File) {
	var audios, texts []File
	for _, f := range u.Files {
		if f.Language != language {
			continue
		}
		mime := f.MimeType
		if mime == "" {
			mime = "audio/mpeg"
		}
		if f.Type == "audio" && mime == "audio/mpeg" {
			audios = append(audios, f)
		}
		if f.Type == "text" && (f.InsertType == "" || f.InsertType == "tamlil") &&
			!strings.HasSuffix(strings.ToLower(f.Name), ".pdf") {
			texts = append(texts, f)
		}
	}
	sort.SliceStable(audios, func(i, j int) bool { return isOriginal(audios[i]) && !isOriginal(audios[j]) })
	isDocx := func(f File) bool { return strings.HasSuffix(strings.ToLower(f.Name), ".docx") }
	sort.SliceStable(texts, func(i, j int) bool { return isDocx(texts[i]) && !isDocx(texts[j]) })
	if len(audios) > 0 {
		audio = &audios[0]
	}
	if len(texts) > 0 {
		transcript = &texts[0]
	}
	return
}

// OriginalLanguage is the language the lesson was given in: that of its original recording ("_o_" in the file's
// name), audio before video; "" if no file is marked original
func OriginalLanguage(u *Unit) string {
	lang := ""
	for _, f := range u.Files {
		if f.Language == "" || !isOriginal(f) {
			continue
		}
		if f.Type == "audio" {
			return f.Language
		}
		if f.Type == "video" && lang == "" {
			lang = f.Language
		}
	}
	return lang
}
