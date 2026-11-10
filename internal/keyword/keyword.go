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
// what a note is about.
type Index struct {
	docs    []Doc
	entries []entry
	df      map[string]int
	avg     float64
}

func NewIndex(docs []Doc) *Index {
	ix := &Index{docs: docs, df: map[string]int{}}
	total := 0
	for _, d := range docs {
		name := strings.TrimSuffix(filepath.Base(d.Path), ".md")
		var words []string
		for range 3 {
			words = append(words, Terms(name)...)
			words = append(words, Terms(d.Title)...)
		}
		for range 2 {
			words = append(words, Terms(d.Tags)...)
		}
		words = append(words, Terms(filepath.Dir(d.Path))...)
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
			hits = append(hits, Hit{Doc: ix.docs[i], Score: s})
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
