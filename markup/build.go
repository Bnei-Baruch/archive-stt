// Package markup: the lesson's segmentation (paragraphs, speakers, chapters) from a run's words and diarization
// (ai-trns's paragraphs: who speaks, when, reading or not) and, if the lesson has one, its human transcript; and
// the document served.
//
// Paragraph.I, Chapter.PStart / PEnd: index of the <p> element in the transcript's doc2html HTML -- the
// paragraphs kmedia shows -- or, with no transcript (source "audio"), of the diarize paragraph (the document served
// gives its text, from the words).
// Speaker: a role code -- rav / reader / student -- or nil; the client draws the label. Times in seconds of the media.
//
// Chapter types (a client shows an unknown one as "other"):
//
//	reading     the reader reads the source; title: the reader's announcement as written ("קטע מס' 46 ...",
//	            "אות ט\"ו") -- not parsed: transcripts number items in different ways and many lessons have none
//	commentary  the Rav after a reading; title: the reading's
//	qa          a student's question, the follow-ups and the Rav's answers; title: the question
//	other       opening, announcements
//
// Not marked (hard to get reliably, left out on purpose): the source item as a number, its place in the source
// document, seminars.
//
// With a transcript the chapters come from its marks, as the editors write them: "קריין: ..." starts a reading
// (but the lesson's own title, "קריין: שיעור ..."), "שאלה:" or a "תלמיד:" after the Rav spoke starts a question
// (one right after a student's turn is a follow-up in it), unmarked paragraphs are the Rav's. The paragraphs get
// their times by aligning their words to the heard ones (textdiff). Without one the chapters come from the
// audio: the diarize paragraphs' `reading` flag and speakers.
package markup

import (
	"html"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/Bnei-Baruch/archive-stt/common"
	"github.com/Bnei-Baruch/archive-stt/models"
)

// Roles maps ai-trns's speaker ids and display labels (ai_trns.diarize.labels) to role codes; the codes map to
// themselves. Any other voice is a student.
var Roles = map[string]string{
	"Rav": common.RoleRav, "Reader": common.RoleReader,
	"הרב": common.RoleRav, "קריין": common.RoleReader, "תלמיד": common.RoleStudent,
	common.RoleRav: common.RoleRav, common.RoleReader: common.RoleReader, common.RoleStudent: common.RoleStudent,
}

// Role is the role code of an ai-trns paragraph's speaker, by its label (then its raw id); "" if it has neither
func Role(p models.DiarizeParagraph) string {
	for _, k := range []string{p.Label, p.Speaker} {
		if r, ok := Roles[k]; ok {
			return r
		}
	}
	if p.Label != "" || p.Speaker != "" {
		return common.RoleStudent
	}
	return ""
}

// a non-word character after a Hebrew word: RE2's \b knows only ASCII words
const wordEnd = `(?:$|[^\p{L}\p{N}_])`

var (
	pRE           = regexp.MustCompile(`(?is)<p\b[^>]*>(.*?)</p>`)
	tagRE         = regexp.MustCompile(`<[^>]+>`)
	readerRE      = regexp.MustCompile(`^\s*קריין\s*:`)
	questionRE    = regexp.MustCompile(`^\s*שאלה\s*:`)
	studentRE     = regexp.MustCompile(`^\s*(?:תלמיד|תלמידה)\s*:`)
	markRE        = regexp.MustCompile(`^\s*(?:קריין|שאלה|תלמיד|תלמידה)\s*:\s*`)
	lessonTitleRE = regexp.MustCompile(`^\s*קריין\s*:\s*(?:שיעור|שיחה|כנס)` + wordEnd) // the lesson itself
	// a paragraph of the reading itself: the quoted text and its reference "(הרב"ש. מאמר 3 ...)"
	quoted = []string{`"`, "“", "״", "„", "("}
)

const TitleChars = 120

func isQuoted(p string) bool {
	for _, q := range quoted {
		if strings.HasPrefix(p, q) {
			return true
		}
	}
	return false
}

// TranscriptParagraphs is the text of every <p> of a doc2html document, in order (empty ones kept: the index is
// the <p>'s)
func TranscriptParagraphs(docHTML string) []string {
	ms := pRE.FindAllStringSubmatch(docHTML, -1)
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = strings.Join(fields(html.UnescapeString(tagRE.ReplaceAllString(m[1], ""))), " ")
	}
	return out
}

func title(text string) string {
	text = strings.TrimSpace(markRE.ReplaceAllString(text, ""))
	rs := []rune(text)
	if len(rs) <= TitleChars {
		return text
	}
	cut := string(rs[:TitleChars])
	if k := strings.LastIndex(cut, " "); k >= 0 {
		cut = cut[:k]
	}
	return cut + "…"
}

type chapter struct {
	Type, Title  string
	PStart, PEnd int
}

const (
	markReader  = "reader"
	markStudent = "student"
)

// MarkedChapters are the chapters by the transcript's marks; nil if it has no marks
func MarkedChapters(paras []string) []chapter {
	hasMarks := false
	usesQuestion := false
	for _, p := range paras {
		if readerRE.MatchString(p) || questionRE.MatchString(p) || studentRE.MatchString(p) {
			hasMarks = true
		}
		if questionRE.MatchString(p) {
			usesQuestion = true // with "שאלה:" marks a "תלמיד:" is a follow-up in the question
		}
	}
	if !hasMarks {
		return nil
	}
	var out []*chapter
	var cur *chapter
	readingTitle := ""
	start := func(kind string, i int, t string) {
		cur = &chapter{Type: kind, Title: t, PStart: i, PEnd: i}
		out = append(out, cur)
	}

	prev := "" // the previous paragraph's mark: reader | student | "" (the Rav)
	for i, p := range paras {
		if p == "" {
			continue
		}
		mark := ""
		if readerRE.MatchString(p) {
			mark = markReader
		} else if questionRE.MatchString(p) || studentRE.MatchString(p) {
			mark = markStudent
		}
		switch {
		case mark == markReader:
			if lessonTitleRE.MatchString(p) {
				if cur == nil || cur.Type != "other" {
					start("other", i, title(p))
				}
			} else if cur != nil && cur.Type == "reading" && prev == markReader {
				// the book, then the item: one announcement
				readingTitle = title(cur.Title + " · " + title(p))
				cur.Title = readingTitle
			} else {
				readingTitle = title(p)
				start("reading", i, readingTitle)
			}
		case mark == markStudent:
			followUp := cur != nil && cur.Type == "qa" && !questionRE.MatchString(p) &&
				(usesQuestion || prev == markStudent)
			if !followUp {
				start("qa", i, title(p))
			}
		case cur == nil:
			start("other", i, title(p))
		case cur.Type == "reading" && i > cur.PStart+1 && !isQuoted(p):
			start("commentary", i, readingTitle)
		}
		cur.PEnd = i
		prev = mark
	}
	merged := make([]chapter, 0, len(out)) // one opening, not one per paragraph mark before the first reading
	for _, c := range out {
		if len(merged) > 0 && c.Type == "other" && merged[len(merged)-1].Type == "other" {
			merged[len(merged)-1].PEnd = c.PEnd
		} else {
			merged = append(merged, *c)
		}
	}
	return merged
}

// MarkedSpeaker is {paragraph index: speaker} the marks tell: the reader's announcements and reading, the
// students' turns
func MarkedSpeaker(paras []string, chapters []chapter) map[int]string {
	out := map[int]string{}
	for _, c := range chapters {
		for i := c.PStart; i <= c.PEnd; i++ {
			p := paras[i]
			if p == "" {
				continue
			}
			if readerRE.MatchString(p) || (c.Type == "reading" && i > c.PStart && isQuoted(p)) {
				out[i] = common.RoleReader
			} else if questionRE.MatchString(p) || studentRE.MatchString(p) {
				out[i] = common.RoleStudent
			}
		}
	}
	return out
}

// HeardWord is a heard word with its paragraph's role and reading flag
type HeardWord struct {
	Word       string
	Start, End float64
	Label      string
	Reading    bool
}

// Split gives every paragraph (by its start, in order) its words: those whose middle is before the next
// paragraph's start. Paragraphs end and start between words, so it holds for rounded times too.
func Split(starts []float64, words []models.Word) [][]models.Word {
	out := make([][]models.Word, len(starts))
	j := 0
	for k := range starts {
		a := j
		for j < len(words) && (k+1 == len(starts) || (words[j].Start+words[j].End)/2 < starts[k+1]) {
			j++
		}
		out[k] = words[a:j]
	}
	return out
}

func diarizeStarts(ps []models.DiarizeParagraph) []float64 {
	out := make([]float64, len(ps))
	for i, p := range ps {
		out[i] = p.Start
	}
	return out
}

// Text is the words joined as ai-trns joins a paragraph's
func Text(words []models.Word) string {
	var out []string
	for _, w := range words {
		if f := strings.Fields(w.Word); len(f) > 0 {
			out = append(out, strings.Join(f, " "))
		}
	}
	return strings.Join(out, " ")
}

// HeardWords are the run's words, each with its diarize paragraph's role and reading flag
func HeardWords(ps []models.DiarizeParagraph, words []models.Word) []HeardWord {
	var out []HeardWord
	for k, ws := range Split(diarizeStarts(ps), words) {
		role := Role(ps[k])
		for _, w := range ws {
			out = append(out, HeardWord{Word: w.Word, Start: w.Start, End: w.End, Label: role, Reading: ps[k].Reading})
		}
	}
	return out
}

// Span is a paragraph's time and its heard words' indices
type Span struct {
	Start, End float64
	Words      []int
}

// Align is the Span of every paragraph by its words' match to the heard ones; nil where nothing matched or the
// match runs back in time (a repeated phrase matched elsewhere)
func Align(paras []string, words []HeardWord) []*Span {
	toks := transcriptTokens(paras)
	texts := make([]string, len(words))
	for j, w := range words {
		texts[j] = w.Word
	}
	match, kind := tokenMatch(toks, texts)
	matched := map[int][]int{}
	for k, t := range toks {
		if kind[k] == "eq" {
			matched[t.P] = append(matched[t.P], match[k])
		}
	}
	out := make([]*Span, len(paras))
	lastStart := -1.0
	for i := range paras {
		js := matched[i]
		// a paragraph needs a few matched words: one stray common word is not an anchor
		nWords := 0
		for _, t := range fields(paras[i]) {
			if Norm(t) != "" {
				nWords++
			}
		}
		if len(js) == 0 || len(js) < min(3, nWords) {
			continue
		}
		start, end := words[js[0]].Start, words[js[len(js)-1]].End
		if start < lastStart {
			continue
		}
		out[i] = &Span{start, end, js}
		lastStart = start
	}
	return out
}

// fill gives the non-empty paragraphs without a time the gap between their timed neighbours
func fill(spans []*Span, paras []string, duration float64) []*Span {
	out := append([]*Span(nil), spans...)
	for i, p := range paras {
		if p == "" || out[i] != nil {
			continue
		}
		prev, next := 0.0, duration
		for k := i - 1; k >= 0; k-- {
			if out[k] != nil {
				prev = out[k].End
				break
			}
		}
		for k := i + 1; k < len(out); k++ {
			if out[k] != nil {
				next = out[k].Start
				break
			}
		}
		out[i] = &Span{prev, math.Max(prev, next), nil}
	}
	return out
}

// majority is the most frequent non-empty label, the first seen on a tie; "" if none
func majority(labels []string) string {
	counts := map[string]int{}
	var order []string
	for _, x := range labels {
		if x == "" {
			continue
		}
		if counts[x] == 0 {
			order = append(order, x)
		}
		counts[x]++
	}
	best := ""
	for _, x := range order {
		if best == "" || counts[x] > counts[best] {
			best = x
		}
	}
	return best
}

// round2 rounds the exact value to 2 decimals, as Python's round(x, 2) (math.Round(x*100)/100 is off on .xx5)
func round2(x float64) float64 {
	r, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', 2, 64), 64)
	return r
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func chapterTimes(chapters []chapter, paragraphs []models.Paragraph, duration float64) []models.Chapter {
	at := make(map[int]models.Paragraph, len(paragraphs))
	for _, p := range paragraphs {
		at[p.I] = p
	}
	out := make([]models.Chapter, 0, len(chapters))
	for _, c := range chapters {
		var ps []models.Paragraph
		for i := c.PStart; i <= c.PEnd; i++ {
			if p, ok := at[i]; ok {
				ps = append(ps, p)
			}
		}
		if len(ps) == 0 {
			continue
		}
		out = append(out, models.Chapter{Type: c.Type, Title: c.Title, PStart: c.PStart, PEnd: c.PEnd,
			Start: ps[0].Start, End: ps[len(ps)-1].End})
	}
	for k := 0; k+1 < len(out); k++ { // a chapter lasts until the next one starts
		out[k].End = math.Max(out[k].Start, out[k+1].Start)
	}
	if len(out) > 0 {
		if out[0].Type == "other" {
			out[0].Start = math.Min(out[0].Start, 0)
		}
		out[len(out)-1].End = math.Max(out[len(out)-1].End, duration)
	}
	return out
}

// FromTranscript is the segmentation of a marked transcript on the heard words; nil if it has no marks
func FromTranscript(paras []string, words []HeardWord, duration float64) *models.SegmentationData {
	chapters := MarkedChapters(paras)
	if chapters == nil {
		return nil
	}
	spans := Align(paras, words)
	aligned, nonEmpty := 0, 0
	for i, p := range paras {
		if p != "" {
			nonEmpty++
			if spans[i] != nil {
				aligned++
			}
		}
	}
	spans = fill(spans, paras, duration)
	marked := MarkedSpeaker(paras, chapters)
	paragraphs := make([]models.Paragraph, 0, len(paras))
	for i, p := range paras {
		s := spans[i]
		if p == "" || s == nil {
			continue
		}
		// not heard (a header of the document): no speaker
		speaker := marked[i]
		if speaker == "" {
			labels := make([]string, len(s.Words))
			for k, j := range s.Words {
				labels[k] = words[j].Label
			}
			speaker = majority(labels)
		}
		if speaker == "" && len(s.Words) > 0 {
			speaker = common.RoleRav
		}
		if _, ok := marked[i]; speaker == common.RoleReader && !ok {
			speaker = common.RoleRav // the audio's reader where the text has no mark: the Rav
		}
		paragraphs = append(paragraphs, models.Paragraph{I: i, Start: round2(s.Start), End: round2(s.End),
			Speaker: strPtr(speaker)})
	}
	chs := chapterTimes(chapters, paragraphs, duration)
	return &models.SegmentationData{Source: common.SegFromTranscript, Paragraphs: paragraphs, Chapters: chs,
		Stats: map[string]interface{}{"transcript_paragraphs": nonEmpty, "aligned_paragraphs": aligned,
			"chapters": len(chs)}}
}

// FromAudio is the segmentation from the diarize paragraphs alone: reading by the `reading` flag, the Rav after
// it commentary, a student's turn opens a question; titles from the words
func FromAudio(ps []models.DiarizeParagraph, words []models.Word, duration float64) *models.SegmentationData {
	texts := Split(diarizeStarts(ps), words)
	paragraphs := make([]models.Paragraph, 0, len(ps))
	var chapters []chapter
	cur := -1 // index into chapters
	for i, p := range ps {
		label := Role(p)
		paragraphs = append(paragraphs, models.Paragraph{I: i, Start: round2(p.Start), End: round2(p.End),
			Speaker: strPtr(label)})
		kind := "" // "": the paragraph goes on the current chapter
		switch {
		case p.Reading:
			if cur < 0 || chapters[cur].Type != "reading" {
				kind = "reading"
			}
		case label == common.RoleStudent:
			// a new question unless it is a follow-up in a question the Rav has not answered yet
			if cur < 0 || chapters[cur].Type != "qa" || ravAnswered(ps, chapters[cur].PStart, i) {
				kind = "qa"
			}
		case cur >= 0 && chapters[cur].Type == "reading":
			kind = "commentary"
		case cur < 0:
			kind = "other"
		}
		if kind != "" {
			t := ""
			if kind == "qa" || kind == "reading" {
				t = title(Text(texts[i]))
			}
			chapters = append(chapters, chapter{Type: kind, Title: t, PStart: i, PEnd: i})
			cur = len(chapters) - 1
		}
		chapters[cur].PEnd = i
	}
	chs := chapterTimes(chapters, paragraphs, duration)
	return &models.SegmentationData{Source: common.SegFromAudio, Paragraphs: paragraphs, Chapters: chs,
		Stats: map[string]interface{}{"diarize_paragraphs": len(paragraphs), "chapters": len(chs)}}
}

func ravAnswered(ps []models.DiarizeParagraph, a, b int) bool {
	for k := a; k < b; k++ {
		if Role(ps[k]) == common.RoleRav {
			return true
		}
	}
	return false
}

// Build is the automatic segmentation: from the transcript if it has marks, else from the audio
func Build(ps []models.DiarizeParagraph, words []models.Word, transcriptHTML string,
	duration float64) *models.SegmentationData {
	if transcriptHTML != "" {
		if seg := FromTranscript(TranscriptParagraphs(transcriptHTML), HeardWords(ps, words), duration); seg != nil {
			return seg
		}
	}
	return FromAudio(ps, words, duration)
}

// Duration is the media's duration, or the end of the last heard paragraph
func Duration(media models.Media, ps []models.DiarizeParagraph) float64 {
	if media.DurationS > 0 {
		return media.DurationS
	}
	if len(ps) > 0 {
		return ps[len(ps)-1].End
	}
	return 0
}
