// Package keyword ranks notes by the words of a query (BM25), the half of search that finds what the embedding
// half misses: notes named after the thing asked about, model names, numbers and dates.
package keyword

import (
	"math"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

var stop = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a an and are as at be by did do does for from had has have how i in is it its me my
		of on or that the this to was what when where which who whom whose why will with you your about into than then
		there these those can could should would we our us they them he she his her also any all some`) {
		stop[w] = true
	}
}

// Terms are the words of a text, lowercased and without stop words. Words joined by . _ - (q4_k_m, qwen3.8,
// 2026-10-22) are kept whole and also split into their parts.
func Terms(text string) []string {
	var out []string
	isWord := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
	rs := []rune(strings.ToLower(text))
	for i := 0; i < len(rs); {
		if !isWord(rs[i]) {
			i++
			continue
		}
		j := i
		for j < len(rs) && (isWord(rs[j]) || (strings.ContainsRune("._-", rs[j]) && j+1 < len(rs) && isWord(rs[j+1]) && j > i)) {
			j++
		}
		w := string(rs[i:j])
		if !stop[w] {
			out = append(out, w)
		}
		if strings.ContainsAny(w, "._-") {
			for _, p := range strings.FieldsFunc(w, func(r rune) bool { return strings.ContainsRune("._-", r) }) {
				if !stop[p] && p != w {
					out = append(out, p)
				}
			}
		}
		i = j
	}
	return out
}

// Doc is one note to rank.
type Doc struct {
	Path  string // vault-relative
	Title string
	Tags  string
	Body  string
}

type entry struct {
	tf  map[string]int
	len int
}

// Index is a BM25 index over notes. The file name and the title count three times and the tags twice: they say
// what a note is about. NewSectionIndex is the same over sections, where they count once.
type Index struct {
	docs    []Doc
	entries []entry
	df      map[string]int
	avg     float64
}

func NewIndex(docs []Doc) *Index { return newIndex(docs, 3, 2) }

// NewSectionIndex ranks sections (Body: one section's text; Path and Title: its note's). A fact that is one line of a
// long note is lost in the note's BM25 length, and the title weight would favour every note named after the topic;
// here each section stands alone and the title and file name count once.
func NewSectionIndex(sections []Doc) *Index { return newIndex(sections, 1, 1) }

func newIndex(docs []Doc, nameWeight, tagWeight int) *Index {
	ix := &Index{docs: docs, df: map[string]int{}}
	total := 0
	for _, d := range docs {
		name := strings.TrimSuffix(filepath.Base(d.Path), ".md")
		var words []string
		for range nameWeight {
			words = append(words, Terms(name)...)
			words = append(words, Terms(d.Title)...)
		}
		for range tagWeight {
			words = append(words, Terms(d.Tags)...)
		}
		if nameWeight > 1 {
			words = append(words, Terms(filepath.Dir(d.Path))...)
		}
		words = append(words, Terms(d.Body)...)
		tf := map[string]int{}
		for _, w := range words {
			tf[w]++
		}
		for w := range tf {
			ix.df[w]++
		}
		ix.entries = append(ix.entries, entry{tf: tf, len: len(words)})
		total += len(words)
	}
	if len(docs) > 0 {
		ix.avg = float64(total) / float64(len(docs))
	}
	return ix
}

// IDF of a term over the indexed notes.
func (ix *Index) IDF(w string) float64 {
	n, df := float64(len(ix.docs)), float64(ix.df[w])
	return math.Log(1 + (n-df+0.5)/(df+0.5))
}

type Hit struct {
	Doc   Doc
	N     int // the doc's position in the slice given to NewIndex
	Score float64
}

// Search returns the notes containing query words, best first.
func (ix *Index) Search(query string, k int) []Hit {
	q := unique(Terms(query))
	var hits []Hit
	for i, e := range ix.entries {
		s := 0.0
		for _, w := range q {
			if f := float64(e.tf[w]); f > 0 {
				s += ix.IDF(w) * f * 2.2 / (f + 1.2*(0.25+0.75*float64(e.len)/ix.avg))
			}
		}
		if s > 0 {
			hits = append(hits, Hit{Doc: ix.docs[i], N: i, Score: s})
		}
	}
	sort.SliceStable(hits, func(a, b int) bool { return hits[a].Score > hits[b].Score })
	if len(hits) > k {
		hits = hits[:k]
	}
	return hits
}

// Match scores how well a piece of text matches the query: the IDF of the query words it contains. Words in
// `known` (a note's file name and title: they say which note, not where in it) count a fifth.
func (ix *Index) Match(text, query string, known map[string]bool) float64 {
	have := map[string]bool{}
	for _, w := range Terms(text) {
		have[w] = true
	}
	s := 0.0
	for _, w := range unique(Terms(query)) {
		if have[w] {
			if known[w] {
				s += ix.IDF(w) * 0.2
			} else {
				s += ix.IDF(w)
			}
		}
	}
	return s
}

// BestLines are the n lines of body that match the query best (Match, at least one query word), best first.
// Table rows and list items are lines, so this is often the fact itself.
func (ix *Index) BestLines(body, query string, known map[string]bool, n int) []string {
	type scored struct {
		i int
		s float64
	}
	lines := strings.Split(body, "\n")
	var c []scored
	for i, l := range lines {
		if strings.TrimSpace(l) == "" || strings.HasPrefix(strings.TrimSpace(l), "|---") {
			continue
		}
		if s := ix.Match(l, query, known); s > 0 {
			c = append(c, scored{i, s})
		}
	}
	sort.SliceStable(c, func(a, b int) bool { return c[a].s > c[b].s })
	if len(c) > n {
		c = c[:n]
	}
	out := make([]string, len(c))
	for i, x := range c {
		out[i] = ix.window(strings.TrimSpace(lines[x.i]), query, LineChars)
	}
	return out
}

// LineChars: a matching line longer than this is cut to the part with the most query words (a fact late in a long
// list item would otherwise be cut off by whoever shows it).
const LineChars = 360

// window returns the max-character stretch of line (cut at spaces, "…" where cut) holding the most IDF of the
// query's words.
func (ix *Index) window(line, query string, max int) string {
	if len(line) <= max {
		return line
	}
	q := map[string]bool{}
	for _, w := range Terms(query) {
		q[w] = true
	}
	// the byte offsets of the line's words that are query words, with their weight
	type hit struct {
		at int
		w  float64
	}
	var hits []hit
	lower := strings.ToLower(line)
	for i := 0; i < len(lower); {
		j := i
		for j < len(lower) && lower[j] != ' ' {
			j++
		}
		for _, t := range Terms(lower[i:j]) {
			if q[t] {
				hits = append(hits, hit{i, ix.IDF(t)})
				break
			}
		}
		i = j + 1
	}
	best, bestScore := 0, -1.0
	for _, h := range hits {
		start := h.at - 20 // a little context before the first word
		if start < 0 {
			start = 0
		}
		s := 0.0
		for _, o := range hits {
			if o.at >= start && o.at < start+max-10 {
				s += o.w
			}
		}
		if s > bestScore {
			best, bestScore = start, s
		}
	}
	if best+max > len(line) {
		best = len(line) - max
	}
	for best > 0 && line[best-1] != ' ' { // start at a word
		best--
	}
	end := best + max
	if end > len(line) {
		end = len(line)
	}
	for end < len(line) && end > best && line[end] != ' ' { // end at a word
		end--
	}
	out := strings.TrimSpace(line[best:end])
	if best > 0 {
		out = "…" + out
	}
	if end < len(line) {
		out += "…"
	}
	return out
}

// Known is the set of words of a note's file name and title.
func Known(path, title string) map[string]bool {
	k := map[string]bool{}
	for _, w := range Terms(strings.TrimSuffix(filepath.Base(path), ".md") + " " + title) {
		k[w] = true
	}
	return k
}

func unique(ws []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, w := range ws {
		if !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}
