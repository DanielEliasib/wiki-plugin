package ranking

import (
	"reflect"
	"strings"
	"testing"

	"wiki_skill/internal/index"
	"wiki_skill/internal/keyword"
)

func hits(docs []keyword.Doc, order []int, scores []float64) []keyword.Hit {
	var out []keyword.Hit
	for i, d := range order {
		out = append(out, keyword.Hit{Doc: docs[d], Score: scores[i]})
	}
	return out
}

func sem(pairs ...any) []index.Result {
	var out []index.Result
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, index.Result{Path: pairs[i].(string), Score: pairs[i+1].(float64)})
	}
	return out
}

func valid(paths ...string) map[string]bool {
	m := map[string]bool{}
	for _, p := range paths {
		m[p] = true
	}
	return m
}

// Fusion as a table of plain values. The first case is the one that reciprocal rank fusion got wrong: a note first by
// both keyword rankings but near the bottom by meaning must still beat notes merely near the top of all three lists.
func TestFuse(t *testing.T) {
	docs := []keyword.Doc{{Path: "a.md"}, {Path: "b.md"}, {Path: "c.md"}}
	kw := hits(docs, []int{2, 0, 1}, []float64{20, 12, 11})
	cases := []struct {
		name      string
		kw        [][]keyword.Hit
		sem       []index.Result
		valid     map[string]bool
		topK      int
		wantOrder []string
		wantMatch map[string]string
	}{
		{
			name: "a clear keyword winner survives a weak semantic rank",
			kw:   [][]keyword.Hit{kw, kw},
			sem: sem("a.md", 0.68, "b.md", 0.67, "x.md", 0.66,
				"c.md", 0.60, "y.md", 0.40),
			valid: valid("a.md", "b.md", "c.md", "x.md", "y.md"), topK: 1,
			wantOrder: []string{"c.md"},
		},
		{
			name: "without similarities the keyword rankings decide, labelled keyword",
			kw:   [][]keyword.Hit{kw, kw}, valid: valid("a.md", "b.md", "c.md"), topK: 3,
			wantOrder: []string{"c.md", "a.md", "b.md"},
			wantMatch: map[string]string{"c.md": "keyword", "a.md": "keyword"},
		},
		{
			name: "a note only meaning finds is kept and labelled semantic",
			kw:   [][]keyword.Hit{kw, kw}, sem: sem("x.md", 0.9, "c.md", 0.5, "a.md", 0.4),
			valid: valid("a.md", "b.md", "c.md", "x.md"), topK: 5,
			wantMatch: map[string]string{"x.md": "semantic", "c.md": "both"},
		},
		{
			name: "a note whose file is gone is dropped",
			kw:   [][]keyword.Hit{kw}, sem: sem("gone.md", 0.99, "c.md", 0.5),
			valid: valid("a.md", "b.md", "c.md"), topK: 5,
			wantOrder: []string{"c.md", "a.md", "b.md"},
		},
		{
			name:  "equal scores order by path",
			kw:    [][]keyword.Hit{hits(docs, []int{1, 0}, []float64{5, 5})},
			valid: valid("a.md", "b.md"), topK: 5,
			wantOrder: []string{"a.md", "b.md"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := fuse(c.kw, c.sem, c.valid, c.topK)
			if c.wantOrder != nil && !reflect.DeepEqual(f.order, c.wantOrder) {
				t.Errorf("order %v, want %v", f.order, c.wantOrder)
			}
			for p, want := range c.wantMatch {
				if got := f.match(p); got != want {
					t.Errorf("%s matched %q, want %q", p, got, want)
				}
			}
		})
	}
}

func TestRankPicksTheMatchingSectionAndTheLines(t *testing.T) {
	notes := []Note{
		{Path: "p/eval.md", Title: "Speed bench", Date: "2026-03-02",
			Body: "Intro about the setup.\n\n## Speed\nModel X: 12.5 t/s tokens per second.\n"},
		{Path: "p/other.md", Title: "Groceries", Body: "milk and eggs\n"},
	}
	out := Rank(notes, "tokens per second", nil, 5)
	if len(out) != 1 || out[0].Path != "p/eval.md" || out[0].Date != "2026-03-02" || out[0].Match != "keyword" {
		t.Fatalf("got %+v", out)
	}
	if !strings.Contains(out[0].Excerpt, "12.5 t/s") {
		t.Errorf("excerpt %q lacks the fact", out[0].Excerpt)
	}
	if len(out[0].Lines) == 0 {
		t.Error("no best lines")
	}
}

func TestDropHeadingLine(t *testing.T) {
	if got := dropHeadingLine("## Speed\n12.5 t/s", "Speed"); got != "12.5 t/s" {
		t.Errorf("got %q", got)
	}
	if got := dropHeadingLine("## Other\nx", "Speed"); got != "## Other\nx" {
		t.Errorf("got %q", got)
	}
}
