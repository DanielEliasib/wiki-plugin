// Package ranking orders Notes for a search query. It does no I/O: it takes the Notes as values, optionally every
// Note's similarity to the query from the Search index, and returns the best Notes with the section and the lines
// that match.
package ranking

import (
	"sort"
	"strings"

	"wiki_skill/internal/index"
	"wiki_skill/internal/keyword"
)

// Note is what the ranking needs of a Note.
type Note struct {
	Path  string // Vault-relative
	Title string
	Date  string
	Tags  string
	Body  string
}

// Result is one Note, merged from the keyword and the semantic ranking.
type Result struct {
	Path    string   `json:"path"`
	Title   string   `json:"title"`
	Date    string   `json:"date,omitempty"`
	Match   string   `json:"match"`          // "keyword", "semantic" or "both"
	Score   float64  `json:"semantic_score"` // the best section's similarity (0 when only the keywords matched)
	Heading string   `json:"heading,omitempty"`
	Excerpt string   `json:"excerpt"`         // the section that matches best
	Lines   []string `json:"lines,omitempty"` // the note's lines that share the most (and rarest) words with the query
}

// Rank ranks notes three ways, by the query's words (BM25 over the notes, file names and titles weighted; BM25 over
// sections, for a fact on one line of a long note) and by meaning (sem: every note's best section similarity, best
// first; nil when there is no semantic ranking), and merges them into at most topK Results, one per note with the
// section that matches best.
func Rank(notes []Note, query string, sem []index.Result, topK int) []Result {
	docs := make([]keyword.Doc, len(notes))
	byPath := make(map[string]Note, len(notes))
	exists := make(map[string]bool, len(notes))
	for i, n := range notes {
		docs[i] = keyword.Doc{Path: n.Path, Title: n.Title, Tags: n.Tags, Body: n.Body}
		byPath[n.Path] = n
		exists[n.Path] = true
	}
	kix := keyword.NewIndex(docs)
	kw := [][]keyword.Hit{kix.Search(query, len(docs)), sectionHits(docs, query, len(docs))}
	f := fuse(kw, sem, exists, topK)

	out := make([]Result, 0, len(f.order))
	for _, p := range f.order {
		n := byPath[p]
		r := Result{Path: p, Title: n.Title, Date: n.Date, Match: f.match(p)}
		// the excerpt: the semantic half's best section, unless another section matches the query's words clearly
		// better (the semantic half can prefer a section that only shares the note's topic)
		known := keyword.Known(p, n.Title)
		var bestKw index.Chunk
		bestKwScore := -1.0
		for _, c := range index.SplitChunks(n.Title + "\n" + n.Body) {
			if s := kix.Match(c.Text, query, known); s > bestKwScore {
				bestKw, bestKwScore = c, s
			}
		}
		if sr, indexed := f.sem[p]; indexed {
			r.Score = sr.Score
			r.Heading, r.Excerpt = sr.Heading, sr.Excerpt
			if r.Excerpt == "" || bestKwScore > 1.5*kix.Match(r.Excerpt, query, known) {
				r.Heading, r.Excerpt = bestKw.Heading, bestKw.Text
			}
		} else {
			r.Heading, r.Excerpt = bestKw.Heading, bestKw.Text
		}
		r.Excerpt = dropHeadingLine(r.Excerpt, r.Heading)
		r.Lines = kix.BestLines(n.Body, query, known, 3)
		out = append(out, r)
	}
	return out
}

// sectionHits ranks the notes by their best section's BM25 score (one hit per note, best first).
func sectionHits(docs []keyword.Doc, query string, k int) []keyword.Hit {
	var sections []keyword.Doc
	for _, d := range docs {
		for _, c := range index.SplitChunks(d.Title + "\n" + d.Body) {
			sections = append(sections, keyword.Doc{Path: d.Path, Title: d.Title, Body: c.Heading + "\n" + c.Text})
		}
	}
	var out []keyword.Hit
	seen := map[string]bool{}
	for _, h := range keyword.NewSectionIndex(sections).Search(query, len(sections)) {
		if !seen[h.Doc.Path] {
			seen[h.Doc.Path] = true
			out = append(out, h)
			if len(out) == k {
				break
			}
		}
	}
	return out
}

// fused is the outcome of the fusion: the notes to show in order, and how each was found.
type fused struct {
	order       []string
	inKw, inSem map[string]bool
	sem         map[string]index.Result
}

func (f fused) match(path string) string {
	switch {
	case f.inSem[path] && f.inKw[path]:
		return "both"
	case f.inSem[path]:
		return "semantic"
	}
	return "keyword"
}

// fuse merges the rankings. kw: the keyword rankings (by note, by section); sem: every note's best section
// similarity, best first; exists: the notes that are on disk (an index entry for a deleted note is dropped). The
// fusion is a weighted sum of normalised scores (keyword scores over the best one, similarity min-max over all
// notes): half the weight to the keyword rankings together, half to meaning; all of it to the keywords when there
// is no similarity. Reciprocal rank fusion was tried first: it drops a note that one list misses and ignores by how
// much a list's first beats its second, so a date on one line of a long note (first by both keyword rankings, 15th
// by meaning) lost to notes that were merely near the top of all three.
func fuse(kw [][]keyword.Hit, sem []index.Result, exists map[string]bool, topK int) fused {
	pool := 3 * topK // a note in the top pool of a list counts as matched by it (the match label)
	score := map[string]float64{}
	f := fused{inKw: map[string]bool{}, inSem: map[string]bool{}, sem: map[string]index.Result{}}
	kwWeight := 0.5 / float64(len(kw))
	semWeight := 0.5
	if len(sem) == 0 {
		kwWeight, semWeight = 1/float64(len(kw)), 0
	}
	for _, list := range kw {
		if len(list) == 0 || list[0].Score <= 0 {
			continue
		}
		for i, h := range list {
			score[h.Doc.Path] += kwWeight * h.Score / list[0].Score
			if i < pool {
				f.inKw[h.Doc.Path] = true
			}
		}
	}
	if len(sem) > 0 {
		hi, lo := sem[0].Score, sem[len(sem)-1].Score
		for i, r := range sem {
			f.sem[r.Path] = r
			if hi > lo {
				score[r.Path] += semWeight * (r.Score - lo) / (hi - lo)
			}
			if i < pool {
				f.inSem[r.Path] = true
			}
		}
	}
	for p := range score {
		if (f.inKw[p] || f.inSem[p]) && exists[p] {
			f.order = append(f.order, p)
		}
	}
	sort.SliceStable(f.order, func(a, b int) bool {
		if score[f.order[a]] != score[f.order[b]] {
			return score[f.order[a]] > score[f.order[b]]
		}
		return f.order[a] < f.order[b]
	})
	if len(f.order) > topK {
		f.order = f.order[:topK]
	}
	return f
}

// dropHeadingLine removes a chunk's own heading line from its start: the heading is reported on its own.
func dropHeadingLine(text, heading string) string {
	first, rest, _ := strings.Cut(text, "\n")
	if heading != "" && strings.HasPrefix(first, "#") && strings.TrimSpace(strings.TrimLeft(first, "#")) == heading {
		return strings.TrimSpace(rest)
	}
	return text
}
