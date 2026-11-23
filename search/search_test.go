package search

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Bnei-Baruch/archive-stt/models"
)

func eq(t *testing.T, got, want interface{}) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("\ngot  %#v\nwant %#v", got, want)
	}
}

// alignment of the paragraphs, a word a second
func alignment(paras ...string) *models.Alignment {
	a := &models.Alignment{}
	s := 0.0
	for _, p := range paras {
		ws := []models.AlignedWord{}
		for _, t := range strings.Fields(p) {
			ws = append(ws, models.AlignedWord{Text: t, Start: s, End: s + 0.5})
			s++
		}
		a.Paragraphs = append(a.Paragraphs, ws)
	}
	return a
}

func TestNorm(t *testing.T) {
	eq(t, []string{Norm(`שָׁלוֹם,`), Norm(`"ט"ו"`), Norm("–"), Norm("Ёлка!"), Norm("בית־הכנסת")},
		[]string{"שלום", "טו", "", "елка", "ביתהכנסת"})
	eq(t, Words("  <<שלום>> – עולם. "), []string{"שלום", "עולם"})
}

var ix = NewIndex(alignment(
	"כותרת השיעור – אחרי עריכה",                    // 0: words 0-3 (the dash is skipped), times 0-4
	"כל ישראל יש להם חלק לעולם הבא, אמר החכם.",     // 1: words 4-12, times 5-13
	"ועוד פעם: כל ישראל יש להם חלק בתורה ובמצוות.", // 2: words 13-21, times 14-22
))

func TestTheStartAndTheEndExactly(t *testing.T) {
	from, to, found := ix.Locate(Words("כל ישראל יש"), Words("אמר החכם."), 0)
	eq(t, []interface{}{from, to, found}, []interface{}{4, 12, Exact})
	eq(t, ix.Pos(from), Pos{P: 1, W: 0, Start: 5, End: 5.5})
	eq(t, ix.Pos(to), Pos{P: 1, W: 8, Start: 13, End: 13.5})
	// the dash is a token of the timecode but not a word
	from, _, _ = ix.Locate(Words("אחרי עריכה"), nil, 0)
	eq(t, ix.Pos(from), Pos{P: 0, W: 3, Start: 3, End: 3.5})
}

func TestARepeatedStartIsTheOneWithItsEndAfterIt(t *testing.T) {
	from, to, found := ix.Locate(Words("כל ישראל יש להם"), Words("בתורה ובמצוות"), 0)
	eq(t, []interface{}{ix.Pos(from).P, ix.Pos(to).P, found}, []interface{}{2, 2, Exact})
	// two places of the start and of the end: the pair of about n words
	from, to, _ = ix.Locate(Words("כל ישראל יש"), Words("להם חלק"), 9)
	eq(t, []interface{}{from, to}, []interface{}{4, 8})
	from, to, _ = ix.Locate(Words("כל ישראל יש"), Words("להם חלק"), 16)
	eq(t, []interface{}{from, to}, []interface{}{4, 19})
	// no end: the first
	from, _, found = ix.Locate(Words("כל ישראל יש להם"), nil, 0)
	eq(t, []interface{}{ix.Pos(from).P, found}, []interface{}{1, Exact})
}

func TestNearlyTheSameIsFuzzy(t *testing.T) {
	// a word changed, one added
	from, to, found := ix.Locate(Words("ועוד פעם כל ישראל שיש להם"), nil, 0)
	eq(t, []interface{}{from, to, found}, []interface{}{13, 18, Fuzzy})
	// the end not found: the start, fuzzy
	_, _, found = ix.Locate(Words("אמר החכם"), Words("משהו אחר לגמרי"), 0)
	eq(t, found, Fuzzy)
	// the start not found: the end
	from, _, found = ix.Locate(Words("משהו אחר לגמרי"), Words("בתורה ובמצוות"), 0)
	eq(t, []interface{}{from, found}, []interface{}{20, Fuzzy})
	_, _, found = ix.Locate(Words("משהו אחר לגמרי"), nil, 0)
	eq(t, found, "")
	_, _, found = ix.Locate(nil, nil, 0)
	eq(t, found, "")
}

func TestTheCacheKeepsTheLatest(t *testing.T) {
	c := &Cache{Size: 2}
	builds := 0
	get := func(key string) {
		c.Get(key, func() (*Index, error) { builds++; return ix, nil })
	}
	get("a")
	get("b")
	get("a") // kept
	get("c") // b out
	get("a")
	eq(t, builds, 3)
	get("b")
	eq(t, builds, 4)
	if got, _ := c.Get("none", func() (*Index, error) { return nil, nil }); got != nil {
		t.Fatal("nothing built, nothing kept")
	}
}
