package markup

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Bnei-Baruch/archive-stt/common"
	"github.com/Bnei-Baruch/archive-stt/models"
)

var Paras = []string{
	"",
	"שיעור בוקר - אחרי עריכה",
	"קריין: שיעור בנושא חוק הערבות.",
	"נמשיך אותו כי אנחנו צריכים חיבור בינינו",
	"קריין: קטע מס' 46 מתוך חוק הערבות.",
	`"כל ישראל יש להם חלק לעולם הבא"`,
	`(הרב"ש. מאמר 3)`,
	"תתאר לעצמך שאתה הולך להתחתן עם החברים",
	"שאלה: מה זו אותה מערכת שקושרת בין דברים?",
	"זו המערכת של הבורא שקושרת הכול",
	"תלמיד: ולמה דווקא הבורא?",
	"כי הוא המקור של הכול",
	"קריין: קטע הבא, מס' 47, מאור ושמש.",
	`"העיקר הוא שכל אחד ואחד אל יחשוב לעצמו"`,
	"מי שעושה מעצמו אפס בחברה מקשר את כולם",
}

type span struct {
	Type         string
	PStart, PEnd int
}

func spans(cs []chapter) []span {
	out := make([]span, len(cs))
	for i, c := range cs {
		out[i] = span{c.Type, c.PStart, c.PEnd}
	}
	return out
}

func eq(t *testing.T, got, want interface{}) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("\ngot  %#v\nwant %#v", got, want)
	}
}

func TestChaptersByTheTranscriptMarks(t *testing.T) {
	chapters := MarkedChapters(Paras)
	eq(t, spans(chapters), []span{{"other", 1, 3}, {"reading", 4, 6}, {"commentary", 7, 7}, {"qa", 8, 11},
		{"reading", 12, 13}, {"commentary", 14, 14}})
	// the reader's announcement as written, not a parsed number; the commentary carries its reading's
	eq(t, chapters[1].Title, "קטע מס' 46 מתוך חוק הערבות.")
	eq(t, chapters[2].Title, chapters[1].Title)
	eq(t, chapters[3].Title, "מה זו אותה מערכת שקושרת בין דברים?")
	if MarkedChapters([]string{"רק טקסט", "בלי סימנים"}) != nil {
		t.Fatal("no marks: no chapters")
	}
}

func TestItemsNumberedAnyWayAndStudentsWithoutQuestionMarks(t *testing.T) {
	paras := []string{`קריין: "תלמוד עשר הספירות", חלק שני.`, `קריין: אות ט"ו.`, `"טקסט האות"`, "הסבר של הרב",
		"תלמיד: שאלה ראשונה?", "תשובה", "תלמיד: שאלה שנייה?", "תלמיד: והמשך שלה?", "תשובה"}
	chapters := MarkedChapters(paras)
	eq(t, spans(chapters), []span{{"reading", 0, 2}, {"commentary", 3, 3}, {"qa", 4, 5}, {"qa", 6, 8}})
	eq(t, chapters[0].Title, `"תלמוד עשר הספירות", חלק שני. · אות ט"ו.`)
	eq(t, chapters[1].Title, chapters[0].Title)
}

func TestTheLessonTitleIsNotAReading(t *testing.T) {
	// a Hebrew word boundary after "שיעור" (RE2's \b is ASCII only)
	chapters := MarkedChapters([]string{"קריין: שיעור בנושא.", "קריין: שיעורים קודמים.", "טקסט"})
	eq(t, spans(chapters)[0], span{"other", 0, 0})
	eq(t, chapters[1].Type, "reading")
}

// Spoken are the heard words of the paragraphs, in order, without the marks and the paragraphs in skip
func Spoken(paras []string, skip map[int]bool) []HeardWord {
	var words []HeardWord
	t := 0.0
	for i, p := range paras {
		if skip[i] {
			t += 5
			continue
		}
		label := common.RoleRav
		if strings.HasPrefix(p, "קריין") || strings.HasPrefix(p, `"`) || strings.HasPrefix(p, "(") {
			label = common.RoleReader
		} else if strings.HasPrefix(p, "שאלה") || strings.HasPrefix(p, "תלמיד") {
			label = common.RoleStudent
		}
		for _, tok := range strings.Fields(markRE.ReplaceAllString(p, "")) {
			words = append(words, HeardWord{Word: tok, Start: t, End: t + 0.25, Label: label})
			t += 0.3
		}
	}
	return words
}

func speaker(p models.Paragraph) string {
	if p.Speaker == nil {
		return ""
	}
	return *p.Speaker
}

func TestParagraphsGetTheirTimesFromTheHeardWords(t *testing.T) {
	words := Spoken(Paras, map[int]bool{1: true}) // the header is not spoken
	seg := FromTranscript(Paras, words, 60)
	at := map[int]models.Paragraph{}
	for _, p := range seg.Paragraphs {
		at[p.I] = p
	}
	eq(t, seg.Stats["transcript_paragraphs"], 14)
	eq(t, seg.Stats["aligned_paragraphs"], 13)
	if !(at[4].Start < at[5].Start && at[5].Start < at[7].Start && at[7].Start < at[8].Start) {
		t.Fatal("paragraphs out of order")
	}
	if at[1].Start != 0 || at[1].End > at[2].Start {
		t.Fatal("a paragraph not heard gets the gap before the next one")
	}
	var got []string
	for _, i := range []int{2, 5, 7, 8, 9, 10} {
		got = append(got, speaker(at[i]))
	}
	eq(t, got, []string{"reader", "reader", "rav", "student", "rav", "student"})
	// a chapter runs until the next one; the first from 0, the last to the end of the media
	chs := seg.Chapters
	eq(t, chs[0].Start, 0.0)
	eq(t, chs[len(chs)-1].End, 60.0)
	for k := 0; k+1 < len(chs); k++ {
		eq(t, chs[k].End, chs[k+1].Start)
	}
	eq(t, chs[1].Start, at[4].Start)
}

func TestARepeatedPhraseDoesNotMoveAParagraphBack(t *testing.T) {
	paras := []string{"קריין: קטע מס' 1.", `"אחד שתיים שלוש ארבע"`, "הסבר ארוך על הקטע הזה",
		"שאלה: אחד שתיים שלוש ארבע?"}
	got := Align(paras, Spoken(paras[:3], nil))
	if got[3] != nil && got[3].Start < got[2].Start {
		t.Fatal("moved back")
	}
}

// dps are diarize paragraphs and their words, one word a second: {label, reading, start, end, text}
func dps(ps ...[]interface{}) ([]models.DiarizeParagraph, []models.Word) {
	var out []models.DiarizeParagraph
	var words []models.Word
	for _, p := range ps {
		start, end := p[2].(float64), p[3].(float64)
		out = append(out, models.DiarizeParagraph{Label: p[0].(string), Reading: p[1].(bool), Start: start, End: end})
		for k, w := range strings.Fields(p[4].(string)) {
			words = append(words, models.Word{Word: w, Start: start + float64(k), End: start + float64(k) + 0.5})
		}
	}
	return out, words
}

func dp(label string, reading bool, start, end float64, text string) []interface{} {
	return []interface{}{label, reading, start, end, text}
}

func TestChaptersFromTheAudioWithoutATranscript(t *testing.T) {
	ps, words := dps(dp("קריין", false, 0, 5, "קטע מס 3"), dp("קריין", true, 5, 30, "טקסט המקור"),
		dp("הרב", false, 30, 90, "הסבר"), dp("תלמיד", false, 90, 95, "למה?"), dp("הרב", false, 95, 120, "כי"),
		dp("תלמיד", false, 120, 125, "ועוד?"))
	seg := FromAudio(ps, words, 130)
	var got []span
	for _, c := range seg.Chapters {
		got = append(got, span{c.Type, c.PStart, c.PEnd})
	}
	eq(t, got, []span{{"other", 0, 0}, {"reading", 1, 1}, {"commentary", 2, 2}, {"qa", 3, 4}, {"qa", 5, 5}})
	eq(t, []string{seg.Chapters[1].Title, seg.Chapters[3].Title}, []string{"טקסט המקור", "למה?"})
	p := seg.Paragraphs[3]
	eq(t, []interface{}{p.I, p.Start, p.End, speaker(p), p.Text}, []interface{}{3, 90.0, 95.0, "student", (*string)(nil)})
	eq(t, []string{speaker(seg.Paragraphs[0]), speaker(seg.Paragraphs[1]), speaker(seg.Paragraphs[2])},
		[]string{"reader", "reader", "rav"})
}

func TestBuildFallsBackToTheAudioWithoutMarks(t *testing.T) {
	ps, words := dps(dp("הרב", false, 0, 5, "שלום"))
	seg := Build(ps, words, "<p>בלי סימנים</p>", 10)
	eq(t, seg.Source, "audio")
	eq(t, speaker(seg.Paragraphs[0]), "rav")
	eq(t, len(seg.Chapters), 1)
	eq(t, seg.Chapters[0].Type, "other")
	eq(t, seg.Stats["chapters"], 1)
}

func TestSplitGivesEveryParagraphItsWords(t *testing.T) {
	w := func(word string, start, end float64) models.Word { return models.Word{Word: word, Start: start, End: end} }
	words := []models.Word{w("א", 0.462, 0.824), w("ב", 0.884, 1.447), w("ג", 1.5, 1.6), w("ד", 2.003, 2.4),
		w("ה", 2.5, 2.5)}
	// the times as stored (rounded to 0.01) split as the exact ones
	for _, starts := range [][]float64{{0.462, 1.5, 2.003}, {0.46, 1.5, 2.0}} {
		var got []string
		for _, ws := range Split(starts, words) {
			got = append(got, Text(ws))
		}
		eq(t, got, []string{"א ב", "ג", "ד ה"})
	}
	eq(t, len(Split(nil, words)), 0)
	eq(t, Text([]models.Word{w(" שלום ", 0, 1), w("  ", 1, 2), w("עולם", 2, 3)}), "שלום עולם")
}

func TestTheDocumentGivesTextFromTheWordsWithoutATranscript(t *testing.T) {
	ps, words := dps(dp("הרב", false, 0, 5, "שלום חברים"), dp("תלמיד", false, 10, 15, "שאלה קצרה"))
	seg := &models.Segmentation{CuUID: "Unit0001", Rev: 1, Data: *FromAudio(ps, words, 20)}
	run := &models.Run{ID: "r1", Language: "he"}
	doc := Document(seg, run, &models.Words{Words: words})
	eq(t, []string{*doc.Paragraphs[0].Text, *doc.Paragraphs[1].Text}, []string{"שלום חברים", "שאלה קצרה"})
	if seg.Data.Paragraphs[0].Text != nil {
		t.Fatal("the stored revision got the text")
	}
	if Document(seg, run, nil).Paragraphs[0].Text != nil {
		t.Fatal("text without words")
	}
}

func TestSpeakerRolesFromAiTrns(t *testing.T) {
	eq(t, Role(models.DiarizeParagraph{Label: "הרב", Speaker: "Rav"}), "rav")
	eq(t, Role(models.DiarizeParagraph{Speaker: "Reader"}), "reader")
	eq(t, Role(models.DiarizeParagraph{Label: "תלמיד", Speaker: "SPEAKER_03"}), "student")
	eq(t, Role(models.DiarizeParagraph{Speaker: "SPEAKER_03"}), "student")
	eq(t, Role(models.DiarizeParagraph{}), "")
}

func TestTranscriptParagraphs(t *testing.T) {
	eq(t, TranscriptParagraphs(`<P class="x">שלום &amp; <b>עולם</b></P><p></p><p>a
	b</p>`), []string{"שלום & עולם", "", "a b"})
}

func TestNorm(t *testing.T) {
	eq(t, Norm(`שָׁלוֹם,`), "שלום")
	eq(t, Norm(`"ט"ו"`), "טו")
	eq(t, Norm("-"), "")
}

func TestRound2AsPython(t *testing.T) {
	// Python: round(2765.935, 2) == 2765.93, round(2814.245, 2) == 2814.24, round(0.125, 2) == 0.12
	eq(t, []float64{round2(2765.935), round2(2814.245), round2(0.125), round2(1.005), round2(23.208)},
		[]float64{2765.93, 2814.24, 0.12, 1, 23.21})
}
