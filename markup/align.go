package markup

// The transcript's words on the heard ones: every whitespace token of every <p> of the doc2html document gets a
// time. It is what search needs -- a word of the transcript (by its <p> and its place in it) to a moment of the
// media, a fragment of the text to its time -- and it is kept apart from the segmentation (models.Alignment).

import (
	"github.com/Bnei-Baruch/archive-stt/models"
)

// token is a whitespace token of the transcript: its <p> and its place there; norm: what is matched ("": not a
// word -- punctuation, a dash, the editors' mark "קריין:")
type token struct {
	P, W int
	Text string
	norm string
}

// transcriptTokens are the tokens of every paragraph, in order. The marks are not heard: their tokens get no norm.
// A mark glued to the word ("קריין:טקסט") leaves the word's part: the text after the mark ends with the
// paragraph's last tokens.
func transcriptTokens(paras []string) []token {
	var out []token
	for i, p := range paras {
		all := fields(p)
		rest := fields(markRE.ReplaceAllString(p, ""))
		skip := len(all) - len(rest)
		for k, t := range all {
			tok := token{P: i, W: k, Text: t}
			if k >= skip {
				tok.norm = Norm(rest[k-skip])
			}
			out = append(out, tok)
		}
	}
	return out
}

// tokenMatch is every token's heard word (index into words) and how: "eq" the same word, "sub" another one in its
// place; -1 / "" not heard
func tokenMatch(toks []token, words []string) ([]int, []string) {
	return tokenMatchWith(AnchoredOps, toks, words)
}

func tokenMatchWith(ops func(ref, hyp []string, threshold float64) []Op, toks []token,
	words []string) ([]int, []string) {
	var ref []string
	var refTok []int
	for k, t := range toks {
		if t.norm != "" {
			ref = append(ref, t.norm)
			refTok = append(refTok, k)
		}
	}
	var hyp []string
	var heard []int
	for j, w := range words {
		if norm := Norm(w); norm != "" {
			hyp = append(hyp, norm)
			heard = append(heard, j)
		}
	}
	match := make([]int, len(toks))
	kind := make([]string, len(toks))
	for k := range match {
		match[k] = -1
	}
	for _, op := range ops(ref, hyp, FuzzyThreshold) {
		if op.Kind == "eq" || op.Kind == "sub" {
			match[refTok[op.I]], kind[refTok[op.I]] = heard[op.J], op.Kind
		}
	}
	return match, kind
}

// AlignWords is the time of every word of the transcript (its doc2html paragraphs) on the heard words. A word
// matched to a heard one (the same or another in its place) takes its time; the rest share the gap between their
// matched neighbours evenly (before the first: at its start, after the last: at its end).
func AlignWords(paras []string, words []models.Word) *models.Alignment {
	toks := transcriptTokens(paras)
	texts := make([]string, len(words))
	for j, w := range words {
		texts[j] = w.Word
	}
	match, kind := tokenMatch(toks, texts)

	start := make([]float64, len(toks))
	end := make([]float64, len(toks))
	stats := map[string]int{"words": len(toks), "eq": 0, "sub": 0}
	prev := -1 // the last matched token
	for k := 0; k <= len(toks); k++ {
		if k < len(toks) && match[k] < 0 {
			continue
		}
		if k < len(toks) {
			start[k], end[k] = words[match[k]].Start, words[match[k]].End
			stats[kind[k]]++
		}
		// the tokens between prev and k: evenly in the gap
		n := k - prev - 1
		if n > 0 {
			a, b := 0.0, 0.0
			switch {
			case prev >= 0 && k < len(toks):
				a, b = end[prev], max(end[prev], start[k])
			case prev >= 0:
				a, b = end[prev], end[prev]
			case k < len(toks):
				a, b = start[k], start[k]
			}
			for m := 0; m < n; m++ {
				start[prev+1+m] = a + (b-a)*float64(m)/float64(n)
				end[prev+1+m] = a + (b-a)*float64(m+1)/float64(n)
			}
		}
		prev = k
	}

	out := &models.Alignment{Paragraphs: make([][]models.AlignedWord, len(paras)), Stats: stats}
	for i := range out.Paragraphs {
		out.Paragraphs[i] = []models.AlignedWord{}
	}
	for k, t := range toks {
		out.Paragraphs[t.P] = append(out.Paragraphs[t.P], models.AlignedWord{Text: t.Text, Start: round2(start[k]),
			End: round2(end[k]), Match: kind[k]})
	}
	return out
}
