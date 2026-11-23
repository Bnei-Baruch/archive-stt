package markup

import (
	"strings"
	"testing"

	"github.com/Bnei-Baruch/archive-stt/models"
)

func heard(texts ...string) []models.Word {
	out := make([]models.Word, len(texts))
	for i, t := range texts {
		out[i] = models.Word{Word: t, Start: float64(10 + i), End: float64(10+i) + 0.5}
	}
	return out
}

type aw struct {
	T    string
	S, E float64
	M    string
}

func flat(a *models.Alignment) [][]aw {
	out := make([][]aw, len(a.Paragraphs))
	for i, ws := range a.Paragraphs {
		out[i] = []aw{}
		for _, w := range ws {
			out[i] = append(out[i], aw{w.Text, w.Start, w.End, w.Match})
		}
	}
	return out
}

func TestEveryTokenOfTheTranscriptGetsATime(t *testing.T) {
	paras := []string{"כותרת", "", "קריין: שלום חברים – היום", "נלמד מאמר חדש"}
	// "כותרת" not heard; "מאמר" heard as "ספר" (sub); the mark and the dash are not words
	a := AlignWords(paras, heard("שלום", "חברים", "היום", "נלמד", "ספר", "חדש"))
	eq(t, flat(a), [][]aw{
		{{"כותרת", 10, 10, ""}}, // before the first heard word: at its start
		{},
		{{"קריין:", 10, 10, ""}, {"שלום", 10, 10.5, "eq"}, {"חברים", 11, 11.5, "eq"}, {"–", 11.5, 12, ""},
			{"היום", 12, 12.5, "eq"}},
		{{"נלמד", 13, 13.5, "eq"}, {"מאמר", 14, 14.5, "sub"}, {"חדש", 15, 15.5, "eq"}},
	})
	eq(t, a.Stats, map[string]int{"words": 9, "eq": 5, "sub": 1})
}

func TestWordsNotHeardShareTheGapAndTheEnd(t *testing.T) {
	a := AlignWords([]string{"אחד שתיים שלוש ארבע חמש", "סוף לא נשמע"}, heard("אחד", "חמש"))
	eq(t, flat(a), [][]aw{
		{{"אחד", 10, 10.5, "eq"}, {"שתיים", 10.5, 10.67, ""}, {"שלוש", 10.67, 10.83, ""}, {"ארבע", 10.83, 11, ""},
			{"חמש", 11, 11.5, "eq"}},
		{{"סוף", 11.5, 11.5, ""}, {"לא", 11.5, 11.5, ""}, {"נשמע", 11.5, 11.5, ""}}, // after the last: at its end
	})
}

func TestAMarkGluedToTheWordKeepsTheWord(t *testing.T) {
	toks := transcriptTokens([]string{"קריין:שלום עולם", "שאלה : מה"})
	var got [][]interface{}
	for _, tk := range toks {
		got = append(got, []interface{}{tk.P, tk.W, tk.Text, tk.norm})
	}
	eq(t, got, [][]interface{}{{0, 0, "קריין:שלום", "שלום"}, {0, 1, "עולם", "עולם"}, {1, 0, "שאלה", ""},
		{1, 1, ":", ""}, {1, 2, "מה", "מה"}})
}

func TestNothingHeardNoTimes(t *testing.T) {
	a := AlignWords([]string{"שלום"}, nil)
	eq(t, flat(a), [][]aw{{{"שלום", 0, 0, ""}}})
}

func TestAQuoteReadAgainLaterDoesNotDragTheText(t *testing.T) {
	quote := "כל ישראל יש להם חלק לעולם הבא אמן ואמן"
	paras := []string{"פתיחה אחת שתיים", quote, "סיום ארבע חמש"}
	// the first reading has a word wrong; the second, later, is word for word: difflib's longest block
	var said []string
	said = append(said, strings.Fields("פתיחה אחת שתיים כל ישראל יש להם משהו לעולם הבא אמן ואמן סיום ארבע חמש")...)
	said = append(said, strings.Fields("ועכשיו עוד פעם "+quote)...)
	words := heard(said...)
	old, _ := tokenMatchWith(WordOps, transcriptTokens(paras), said)
	if old[len(old)-1] >= 0 {
		t.Fatal("difflib alone was expected to lose the end here")
	}
	a := AlignWords(paras, words)
	eq(t, a.Paragraphs[1][0].Start, words[3].Start)  // the quote at its first reading
	eq(t, a.Paragraphs[2][2].Start, words[14].Start) // and the end after it
	eq(t, a.Paragraphs[2][2].Match, "eq")
}
