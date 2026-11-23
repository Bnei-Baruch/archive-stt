// Package search: a fragment of the lesson's transcript (a search engine's highlight) to its place and time. The
// fragment is given by its first and last words -- as kmedia's links to a selected text (srchstart / srchend) --
// and found among the transcript's words aligned on the heard ones (models.Alignment): it is the same text, so an
// exact match is the rule; a fuzzy one covers the text extracted from the docx a bit differently.
package search

import (
	"container/list"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/Bnei-Baruch/archive-stt/models"
)

var niqqudRE = regexp.MustCompile(`[\x{0591}-\x{05C7}]`)

// Norm is a token as matched: niqqud, punctuation and anything not a letter or a digit stripped, lower case, ё as
// е; "" if nothing is left
func Norm(token string) string {
	var b strings.Builder
	for _, r := range niqqudRE.ReplaceAllString(token, "") {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if r == 'ё' || r == 'Ё' {
				r = 'е'
			}
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// Words are the normalized words of a text (whitespace tokens with nothing left dropped)
func Words(text string) []string {
	var out []string
	for _, t := range strings.Fields(text) {
		if n := Norm(t); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// Pos is a word: as written, its time and, of a transcript, its <p> and its place there (whitespace tokens, as
// the timecode); of heard words P is -1 and W the word's index
type Pos struct {
	Text       string
	P, W       int
	Start, End float64
}

// Match is a place of a query: its first and last word and how many of the query's words are there
type Match struct {
	From, To, Matched int
}

// Index is a transcript's words, normalized, for search
type Index struct {
	words []string
	pos   []Pos
	inv   map[string][]int
}

// NewIndex indexes an alignment's words (the tokens with no letters left are skipped)
func NewIndex(a *models.Alignment) *Index {
	ix := &Index{inv: map[string][]int{}}
	for p, ws := range a.Paragraphs {
		for w, aw := range ws {
			n := Norm(aw.Text)
			if n == "" {
				continue
			}
			ix.inv[n] = append(ix.inv[n], len(ix.words))
			ix.words = append(ix.words, n)
			ix.pos = append(ix.pos, Pos{Text: aw.Text, P: p, W: w, Start: aw.Start, End: aw.End})
		}
	}
	return ix
}

// NewWordsIndex indexes heard words (models.Words)
func NewWordsIndex(words []models.Word) *Index {
	ix := &Index{inv: map[string][]int{}}
	for j, w := range words {
		n := Norm(w.Word)
		if n == "" {
			continue
		}
		ix.inv[n] = append(ix.inv[n], len(ix.words))
		ix.words = append(ix.words, n)
		ix.pos = append(ix.pos, Pos{Text: strings.TrimSpace(w.Word), P: -1, W: j, Start: w.Start, End: w.End})
	}
	return ix
}

func (ix *Index) Len() int { return len(ix.pos) }

// At is the word at t: the last one started by then (the first if none); -1 if there are no words
func (ix *Index) At(t float64) int {
	if len(ix.pos) == 0 {
		return -1
	}
	i := sort.Search(len(ix.pos), func(i int) bool { return ix.pos[i].Start > t })
	return max(i-1, 0)
}

// Range are the words [i, j) heard between from and to: ending after from, starting before to
func (ix *Index) Range(from, to float64) (int, int) {
	i := sort.Search(len(ix.pos), func(i int) bool { return ix.pos[i].End > from })
	j := sort.Search(len(ix.pos), func(i int) bool { return ix.pos[i].Start >= to })
	return i, max(i, j)
}

// Text is the words [i, j] as written, joined with spaces
func (ix *Index) Text(i, j int) string {
	i, j = max(i, 0), min(j, len(ix.pos)-1)
	var parts []string
	for k := i; k <= j; k++ {
		parts = append(parts, ix.pos[k].Text)
	}
	return strings.Join(parts, " ")
}

func (ix *Index) Pos(i int) Pos { return ix.pos[i] }

// exact are the indices where q starts, in order
func (ix *Index) exact(q []string) []int {
	var out []int
	for _, i := range ix.inv[q[0]] {
		if i+len(q) > len(ix.words) {
			continue
		}
		ok := true
		for k := 1; k < len(q); k++ {
			if ix.words[i+k] != q[k] {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, i)
		}
	}
	return out
}

// fuzzy are the places of q with words missing, changed or added, the best first. Every word of q votes for
// where q would start; a place takes the votes of the places up to d around it (a word added or dropped shifts the
// rest; d: a quarter of q, at least 1). At a place q's words are matched in order, each up to d off where it would
// be; a place needs most of them (3 of 5). Of places closer than q's length, the best.
func (ix *Index) fuzzy(q []string) []Match {
	d := max(1, len(q)/4)
	votes := map[int]int{}
	for k, w := range q {
		for _, i := range ix.inv[w] {
			votes[i-k]++
		}
	}
	score := func(o int) int {
		n := 0
		for x := -d; x <= d; x++ {
			n += votes[o+x]
		}
		return n
	}
	need := max(2, (len(q)*3+4)/5)
	var places []int
	for o := range votes {
		if score(o) >= need {
			places = append(places, o)
		}
	}
	sort.Slice(places, func(a, b int) bool {
		sa, sb := score(places[a]), score(places[b])
		return sa > sb || (sa == sb && places[a] < places[b])
	})
	var out []Match
	var taken []int
	for _, o := range places {
		near := false
		for _, t := range taken {
			if o-t < len(q) && t-o < len(q) {
				near = true
				break
			}
		}
		if near {
			continue
		}
		if m := ix.matchAt(q, o, d); m.Matched >= need {
			taken = append(taken, o)
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Matched > out[b].Matched })
	return out
}

// matchAt matches q's words in order at the place o, each up to d off where it would be
func (ix *Index) matchAt(q []string, o, d int) Match {
	m := Match{From: -1, To: -1}
	next := 0
	for k, w := range q {
		for i := max(next, o+k-d); i <= o+k+d && i < len(ix.words); i++ {
			if i >= 0 && ix.words[i] == w {
				if m.From < 0 {
					m.From = i
				}
				m.To, next = i, i+1
				m.Matched++
				break
			}
		}
	}
	return m
}

// find are q's places [from, to] (exact: all of it, in order; fuzzy: the best first) and how they were found
// ("" not found)
func (ix *Index) find(q []string) ([]Match, string) {
	if len(q) == 0 {
		return nil, ""
	}
	if is := ix.exact(q); len(is) > 0 {
		out := make([]Match, len(is))
		for k, i := range is {
			out[k] = Match{i, i + len(q) - 1, len(q)}
		}
		return out, Exact
	}
	if places := ix.fuzzy(q); len(places) > 0 {
		return places, Fuzzy
	}
	return nil, ""
}

const (
	Exact = "exact"
	Fuzzy = "fuzzy"
	// MaxSpan is how many words a fragment may span from its start to its end
	MaxSpan = 300
)

// Locate is the span [from, to] of the fragment that starts with the words start and ends with end (none: start
// alone), and how it was found: exact if both are, fuzzy if either is only nearly or the end is missing; "" not
// found. n: the fragment's length in words, about (0: unknown). Of several places of the start and the end, the
// pair whose span is nearest to n (unknown: the shortest); with no pair, the start's first (exact) or best (fuzzy)
// place.
func (ix *Index) Locate(start, end []string, n int) (from, to int, found string) {
	starts, how := ix.find(start)
	ends, howEnd := ix.find(end)
	switch {
	case len(starts) == 0 && len(ends) == 0:
		return 0, 0, ""
	case len(starts) == 0: // the start is not there: the end alone
		return ends[0].From, ends[0].To, Fuzzy
	case len(ends) == 0:
		if len(end) > 0 {
			how = Fuzzy
		}
		return starts[0].From, starts[0].To, how
	}
	limit := MaxSpan
	if n > 0 {
		limit = max(limit, 2*n)
	}
	best := -1 // the pair's span nearest to n words; without n, the shortest
	for _, s := range starts {
		for _, e := range ends {
			span := e.To - s.From
			if e.To < s.To || e.From < s.From || span > limit {
				continue
			}
			cost := span
			if n > 0 {
				cost = abs(span - (n - 1))
			}
			if best < 0 || cost < best {
				from, to, best = s.From, e.To, cost
			}
		}
	}
	if best < 0 {
		return starts[0].From, starts[0].To, Fuzzy // the end is elsewhere
	}
	if howEnd == Fuzzy {
		how = Fuzzy
	}
	return from, to, how
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Find are the places of the phrase q: all exact ones in order, else the fuzzy ones, the best first; and how
// found ("" none)
func (ix *Index) Find(q []string) ([]Match, string) {
	return ix.find(q)
}

// DefaultSize: indices kept by a Cache with no Size (a lesson's ≈ 0.2-0.5 MB)
const DefaultSize = 1000

// Cache keeps the latest indices, by key (the transcript and the run), at most Size (0: DefaultSize) of them
type Cache struct {
	Size  int
	mu    sync.Mutex
	order *list.List // of *entry, the latest used first
	byKey map[string]*list.Element
}

type entry struct {
	key string
	ix  *Index
}

// Get is the index of key, built by build (nil, nil: none) if not kept
func (c *Cache) Get(key string, build func() (*Index, error)) (*Index, error) {
	c.mu.Lock()
	if c.byKey == nil {
		c.byKey, c.order = map[string]*list.Element{}, list.New()
	}
	if el, ok := c.byKey[key]; ok {
		c.order.MoveToFront(el)
		c.mu.Unlock()
		return el.Value.(*entry).ix, nil
	}
	c.mu.Unlock()
	ix, err := build() // outside the lock: reading a file
	if err != nil || ix == nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.byKey[key]; ok { // built meanwhile
		return el.Value.(*entry).ix, nil
	}
	c.byKey[key] = c.order.PushFront(&entry{key, ix})
	size := c.Size
	if size <= 0 {
		size = DefaultSize
	}
	for c.order.Len() > size {
		last := c.order.Back()
		c.order.Remove(last)
		delete(c.byKey, last.Value.(*entry).key)
	}
	return ix, nil
}
