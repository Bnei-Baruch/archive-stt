package subtitles

import "testing"

func TestToSRT(t *testing.T) {
	vtt := "\xef\xbb\xbfWEBVTT\r\nKind: captions\r\n\r\nNOTE a comment\r\n\r\n1\r\n00:00:00.529 --> 00:00:03.143 align:start\r\n" +
		"אילון מאסק ובכירים אחרים קוראים\r\n\r\n2\r\n00:03.143 --> 00:06.353\r\n.לעצור את פיתוח הבינה המלאכותית\r\n\r\n" +
		"00:00:06.353 --> 00:00:07.958\r\n<i>-שלום</i>\r\n,לך\r\n\r\n" +
		"00:00:08.000 --> 00:00:09.000\r\n\r\n\r\n" +
		"1:02:03.400 --> 1:02:04.000\r\nשלום לך.\r\n"
	srt, n, err := ToSRT(vtt)
	if err != nil {
		t.Fatal(err)
	}
	want := "1\n00:00:00,529 --> 00:00:03,143\nאילון מאסק ובכירים אחרים קוראים\n\n" +
		"2\n00:00:03,143 --> 00:00:06,353\nלעצור את פיתוח הבינה המלאכותית.\n\n" +
		"3\n00:00:06,353 --> 00:00:07,958\n-שלום\nלך,\n\n" +
		"4\n01:02:03,400 --> 01:02:04,000\nשלום לך.\n\n"
	if srt != want || n != 4 {
		t.Fatalf("%d\n%s", n, srt)
	}
	if _, _, err := ToSRT("1\n00:00:00,000 --> 00:00:01,000\nx\n"); err == nil {
		t.Fatal("an SRT is not a VTT")
	}
}

func TestFixLine(t *testing.T) {
	for in, want := range map[string]string{
		".שלום":            "שלום.",
		"?מה זה":           "מה זה?",
		"שלום.":            "שלום.",
		".שלום.":           ".שלום.", // ends with punctuation: left as it is
		"-שלום":            "-שלום",
		`מערכות בינה"`:     `מערכות בינה"`,
		"...":              "...",
		"  <b>a</b>   b  ": "a b",
	} {
		if got := fixLine(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
