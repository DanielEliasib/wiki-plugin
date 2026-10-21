package render

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"wiki_skill/internal/index"
	"wiki_skill/internal/wikicmd"
)

func TestSearchJSON(t *testing.T) {
	report := wikicmd.SearchReport{
		Query:           "test query",
		GrepMatches:     []wikicmd.GrepMatch{{File: "a.md", Line: 3, Text: "hit"}},
		SemanticMatches: []index.Result{{Path: "a.md", Score: 0.9, Snippet: "hit"}},
		SemanticIndexed: true,
	}
	var buf bytes.Buffer
	if err := Search(&buf, true, report); err != nil {
		t.Fatal(err)
	}
	var decoded wikicmd.SearchReport
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Query != report.Query || len(decoded.GrepMatches) != 1 || len(decoded.SemanticMatches) != 1 {
		t.Errorf("got %+v", decoded)
	}
}

func TestSearchHuman(t *testing.T) {
	report := wikicmd.SearchReport{
		Query:           "test query",
		GrepMatches:     []wikicmd.GrepMatch{{File: "a.md", Line: 3, Text: "hit"}},
		SemanticMatches: []index.Result{{Path: "a.md", Score: 0.9, Snippet: "hit"}},
		SemanticIndexed: true,
	}
	var buf bytes.Buffer
	if err := Search(&buf, false, report); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"Grep matches", "a.md", "Semantic matches", "0.900"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in: %s", want, out)
		}
	}
}

func TestSearchHumanNoIndex(t *testing.T) {
	var buf bytes.Buffer
	if err := Search(&buf, false, wikicmd.SearchReport{Query: "q", SemanticIndexed: false}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no semantic index yet") {
		t.Errorf("got %s", buf.String())
	}
}

func TestReindexJSON(t *testing.T) {
	report := wikicmd.ReindexReport{NotesIndexed: 5, DBPath: "/tmp/x/index.sqlite"}
	var buf bytes.Buffer
	if err := Reindex(&buf, true, report); err != nil {
		t.Fatal(err)
	}
	var decoded wikicmd.ReindexReport
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != report {
		t.Errorf("got %+v, want %+v", decoded, report)
	}
}

func TestReindexHuman(t *testing.T) {
	var buf bytes.Buffer
	if err := Reindex(&buf, false, wikicmd.ReindexReport{NotesIndexed: 5, DBPath: "/tmp/x/index.sqlite"}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "5") || !strings.Contains(out, "/tmp/x/index.sqlite") {
		t.Errorf("got %s", out)
	}
}

func TestPruneJSON(t *testing.T) {
	report := wikicmd.PruneReport{
		MergeCandidates: []wikicmd.MergeCandidate{{PathA: "a.md", PathB: "b.md", Similarity: 0.95}},
		StaleFolders:    []wikicmd.StaleFolder{{Bucket: "projects", Name: "demo", DaysIdle: 200}},
	}
	var buf bytes.Buffer
	if err := Prune(&buf, true, report); err != nil {
		t.Fatal(err)
	}
	var decoded wikicmd.PruneReport
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.MergeCandidates) != 1 || len(decoded.StaleFolders) != 1 {
		t.Errorf("got %+v", decoded)
	}
}

func TestPruneHuman(t *testing.T) {
	report := wikicmd.PruneReport{
		MergeCandidates: []wikicmd.MergeCandidate{{PathA: "a.md", PathB: "b.md", Similarity: 0.95}},
		StaleFolders:    []wikicmd.StaleFolder{{Bucket: "projects", Name: "demo", DaysIdle: 200}},
	}
	var buf bytes.Buffer
	if err := Prune(&buf, false, report); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "Orphan") {
		t.Errorf("prune no longer reports orphans: %s", out)
	}
	for _, want := range []string{"Merge candidates", "a.md", "b.md", "Stale folders", "projects/demo"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in: %s", want, out)
		}
	}
}

func TestPruneHumanEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := Prune(&buf, false, wikicmd.PruneReport{}); err != nil {
		t.Fatal(err)
	}
	if strings.Count(buf.String(), "(none)") != 2 {
		t.Errorf("expected 2 (none) sections, got: %s", buf.String())
	}
}

func TestErrorJSON(t *testing.T) {
	var buf bytes.Buffer
	Error(&buf, true, errors.New("boom"))
	var decoded map[string]string
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["error"] != "boom" {
		t.Errorf("got %+v", decoded)
	}
}

func TestErrorHuman(t *testing.T) {
	var buf bytes.Buffer
	Error(&buf, false, errors.New("boom"))
	if !strings.Contains(buf.String(), "boom") {
		t.Errorf("got %s", buf.String())
	}
}
