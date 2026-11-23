// Package subtitles: a WebVTT file of MDB as the SRT cues ai-trns's /diarize/words aligns the words to.
package subtitles

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	timingRe = regexp.MustCompile(`^((?:\d+:)?\d{2}:\d{2}[.,]\d{3})\s+-->\s+((?:\d+:)?\d{2}:\d{2}[.,]\d{3})`)
	tagRe    = regexp.MustCompile(`<[^>]*>`)
	// a run of sentence punctuation at the start of a line: an RTL line saved in visual order (".שלום" for
	// "שלום."); a dash or a quote may start a line as it is (a speaker change, a quotation), they stay
	leadPunctRe  = regexp.MustCompile(`^([.,!?:;…]+)\s*(.*)$`)
	trailPunctRe = regexp.MustCompile(`[.,!?:;…]$`)
)

// srtTime is a VTT time (mm:ss.ttt or hh:mm:ss.ttt) as SRT's hh:mm:ss,ttt
func srtTime(t string) string {
	t = strings.Replace(t, ".", ",", 1)
	if strings.Count(t, ":") == 1 {
		t = "00:" + t
	}
	if i := strings.Index(t, ":"); i == 1 {
		t = "0" + t
	}
	return t
}

// fixLine is the line without its markup and, if it starts with sentence punctuation and does not end with it,
// with that punctuation moved to its end
func fixLine(line string) string {
	line = strings.Join(strings.Fields(tagRe.ReplaceAllString(line, "")), " ")
	if m := leadPunctRe.FindStringSubmatch(line); m != nil && m[2] != "" && !trailPunctRe.MatchString(m[2]) {
		return m[2] + m[1]
	}
	return line
}

// ToSRT is the VTT's cues as SRT, numbered again, and how many there are. Cue settings, NOTE / STYLE / REGION
// blocks and tags are dropped; a cue without text is left out.
func ToSRT(vtt string) (string, int, error) {
	vtt = strings.TrimPrefix(strings.ReplaceAll(strings.ReplaceAll(vtt, "\r\n", "\n"), "\r", "\n"), "\xef\xbb\xbf")
	if !strings.HasPrefix(vtt, "WEBVTT") {
		return "", 0, fmt.Errorf("not a WebVTT file")
	}
	var b strings.Builder
	n := 0
	for _, block := range strings.Split(vtt, "\n\n") {
		lines := strings.Split(strings.Trim(block, "\n"), "\n")
		at := -1
		for i, l := range lines {
			if strings.Contains(l, "-->") {
				at = i
				break
			}
		}
		if at < 0 {
			continue // the header, NOTE, STYLE, REGION
		}
		m := timingRe.FindStringSubmatch(strings.TrimSpace(lines[at]))
		if m == nil {
			return "", 0, fmt.Errorf("bad cue timing %q", lines[at])
		}
		var text []string
		for _, l := range lines[at+1:] {
			if l = fixLine(l); l != "" {
				text = append(text, l)
			}
		}
		if len(text) == 0 {
			continue
		}
		n++
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n", n, srtTime(m[1]), srtTime(m[2]), strings.Join(text, "\n"))
	}
	return b.String(), n, nil
}
