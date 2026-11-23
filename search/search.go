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

// Pos is a word of the transcript: its <p>, its place there (whitespace tokens, as the timecode) and its time
type Pos struct {
	P, W       int
	Start, End float64
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
			ix.pos = append(ix.pos, Pos{P: p, W: w, Start: aw.Start, End: aw.End})
		}
	}
	return ix
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

// fuzzy are the places of q with words missing, changed or added, the best first: every word of q votes for where
// q would start; a place takes the votes of its neighbours too (a word added or dropped shifts the rest by one).
// A place needs most of q (3 of 5); of places closer than q's length, the best. The words matched at a place
// give its span [from, to].
func (ix *Index) fuzzy(q []string) [][2]int {
	votes := map[int]int{}
	for k, w := range q {
		for _, i := range ix.inv[w] {
			votes[i-k]++
		}
	}
	score := func(o int) int { return votes[o-1] + votes[o] + votes[o+1] }
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
	var out [][2]int
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
		taken = append(taken, o)
		from, to := -1, -1
		for k, w := range q {
			for d := -1; d <= 1; d++ {
				i := o + k + d
				if i >= 0 && i < len(ix.words) && ix.words[i] == w {
					if from < 0 || i < from {
						from = i
					}
					to = max(to, i)
					break
				}
			}
		}
		if from >= 0 {
			out = append(out, [2]int{from, to})
		}
	}
	return out
}

// find are q's places [from, to] (exact: all of it, in order; fuzzy: the best first) and how they were found
// ("" not found)
func (ix *Index) find(q []string) ([][2]int, string) {
	if len(q) == 0 {
		return nil, ""
	}
	if is := ix.exact(q); len(is) > 0 {
		out := make([][2]int, len(is))
		for k, i := range is {
			out[k] = [2]int{i, i + len(q) - 1}
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
		return ends[0][0], ends[0][1], Fuzzy
	case len(ends) == 0:
		if len(end) > 0 {
			how = Fuzzy
		}
		return starts[0][0], starts[0][1], how
	}
	limit := MaxSpan
	if n > 0 {
		limit = max(limit, 2*n)
	}
	best := -1 // the pair's span nearest to n words; without n, the shortest
	for _, s := range starts {
		for _, e := range ends {
			span := e[1] - s[0]
			if e[1] < s[1] || e[0] < s[0] || span > limit {
				continue
			}
			cost := span
			if n > 0 {
				cost = abs(span - (n - 1))
			}
			if best < 0 || cost < best {
				from, to, best = s[0], e[1], cost
			}
		}
	}
	if best < 0 {
		return starts[0][0], starts[0][1], Fuzzy // the end is elsewhere
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
