package markup

// Word matching of a reference text against heard words: the part of ai-trns's textdiff.py the markup needs.
// Both sides are normalized (niqqud, quotes, punctuation stripped); words at or above FuzzyThreshold character
// similarity count as equal, so spelling variants (ktiv male / haser) still match. difflib is the Go port of
// Python's, so the alignment is the same as ai-trns's.

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/pmezard/go-difflib/difflib"
)

const (
	FuzzyThreshold = 0.8
	// above this many cells, a replace block skips the fuzzy DP
	maxDPCells = 1_000_000
)

var (
	niqqudRE = regexp.MustCompile(`[\x{0591}-\x{05C7}]`)
	quoteRE  = regexp.MustCompile(`["'׳״‘’“”]`)
	punctRE  = regexp.MustCompile(`[,.;:!?()]`)
)

// Norm strips niqqud, quotes and punctuation; a token with no letters/digits left (dash, maqaf...) is not a word: ""
func Norm(token string) string {
	norm := punctRE.ReplaceAllString(quoteRE.ReplaceAllString(niqqudRE.ReplaceAllString(token, ""), ""), "")
	for _, r := range norm {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return norm
		}
	}
	return ""
}

func chars(s string) []string {
	out := make([]string, 0, len(s))
	for _, r := range s {
		out = append(out, string(r))
	}
	return out
}

func similar(a, b string, threshold float64) bool {
	return a == b || difflib.NewMatcher(chars(a), chars(b)).Ratio() >= threshold
}

// Op is an alignment step: eq / sub (I, J), del (I), ins (J); indices into ref and hyp
type Op struct {
	Kind string
	I, J int
}

// fuzzyOps is the edit-distance alignment of one replace block, with absolute indices
func fuzzyOps(ref, hyp []string, i0, j0 int, threshold float64) []Op {
	n, m := len(ref), len(hyp)
	if n*m > maxDPCells {
		ops := make([]Op, 0, n+m)
		for i := 0; i < n; i++ {
			ops = append(ops, Op{"del", i0 + i, -1})
		}
		for j := 0; j < m; j++ {
			ops = append(ops, Op{"ins", -1, j0 + j})
		}
		return ops
	}

	sim := make([][]bool, n)
	for i := range sim {
		sim[i] = make([]bool, m)
		for j := range sim[i] {
			sim[i][j] = similar(ref[i], hyp[j], threshold)
		}
	}
	cost := make([][]int, n+1)
	for i := range cost {
		cost[i] = make([]int, m+1)
		cost[i][0] = i
	}
	for j := 0; j <= m; j++ {
		cost[0][j] = j
	}
	subCost := func(i, j int) int {
		if sim[i][j] {
			return 0
		}
		return 1
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			cost[i][j] = min(cost[i-1][j-1]+subCost(i-1, j-1), cost[i-1][j]+1, cost[i][j-1]+1)
		}
	}

	var ops []Op
	i, j := n, m
	for i > 0 || j > 0 {
		switch {
		case i > 0 && j > 0 && cost[i][j] == cost[i-1][j-1]+subCost(i-1, j-1):
			kind := "sub"
			if sim[i-1][j-1] {
				kind = "eq"
			}
			ops = append(ops, Op{kind, i0 + i - 1, j0 + j - 1})
			i, j = i-1, j-1
		case i > 0 && cost[i][j] == cost[i-1][j]+1:
			ops = append(ops, Op{"del", i0 + i - 1, -1})
			i--
		default:
			ops = append(ops, Op{"ins", -1, j0 + j - 1})
			j--
		}
	}
	for a, b := 0, len(ops)-1; a < b; a, b = a+1, b-1 {
		ops[a], ops[b] = ops[b], ops[a]
	}
	return ops
}

// WordOps aligns the normalized reference words to the heard ones
func WordOps(ref, hyp []string, threshold float64) []Op {
	var ops []Op
	matcher := difflib.NewMatcherWithJunk(ref, hyp, false, nil)
	for _, c := range matcher.GetOpCodes() {
		switch c.Tag {
		case 'e':
			for k := 0; k < c.I2-c.I1; k++ {
				ops = append(ops, Op{"eq", c.I1 + k, c.J1 + k})
			}
		case 'd':
			for i := c.I1; i < c.I2; i++ {
				ops = append(ops, Op{"del", i, -1})
			}
		case 'i':
			for j := c.J1; j < c.J2; j++ {
				ops = append(ops, Op{"ins", -1, j})
			}
		default:
			ops = append(ops, fuzzyOps(ref[c.I1:c.I2], hyp[c.J1:c.J2], c.I1, c.J1, threshold)...)
		}
	}
	return ops
}

// fields splits on whitespace, as Python's str.split()
func fields(s string) []string {
	return strings.Fields(s)
}
