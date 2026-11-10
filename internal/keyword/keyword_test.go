package keyword

import (
	"reflect"
	"strings"
	"testing"
)

func TestTermsKeepJoinedWordsAndTheirParts(t *testing.T) {
	got := Terms("What is the Q4_K_M of Qwen3.8?")
	want := []string{"q4_k_m", "q4", "k", "m", "qwen3.8", "qwen3", "8"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := Terms("eval of 2026-03-02."); !reflect.DeepEqual(got[:2], []string{"eval", "2026-03-02"}) {
		t.Errorf("got %v", got)
	}
}

func TestNoteNamedAfterTheQuestionRanksFirst(t *testing.T) {
	ix := NewIndex([]Doc{
		{Path: "p/notes/other.md", Title: "Other", Body: "see the speed bench for the speed numbers"},
		{Path: "p/notes/speed-bench-2026-03-02.md", Title: "Speed bench", Body: "speed 12.5 t/s"},
	})
	hits := ix.Search("How fast was the model in the speed bench of 2026-03-02?", 5)
	if len(hits) == 0 || hits[0].Doc.Path != "p/notes/speed-bench-2026-03-02.md" {
		t.Errorf("got %+v", hits)
	}
}

func TestMatchDiscountsWordsOfTheNoteTitle(t *testing.T) {
	ix := NewIndex([]Doc{{Path: "a.md", Body: "alpha suite power limit"}, {Path: "b.md", Body: "other words"}})
	known := Known("flash-coder-test.md", "Alpha Suite test")
	q := "power limit in the Alpha Suite test"
	if title, fact := ix.Match("Alpha Suite test, model details", q, known), ix.Match("power limit was 90 W", q, known); fact <= title {
		t.Errorf("title line %v should score below the fact line %v", title, fact)
	}
}

func TestBestLinesFindTheFact(t *testing.T) {
	ix := NewIndex([]Doc{{Path: "a.md", Body: "modelz.3 released date"}, {Path: "b.md", Body: "other"}, {Path: "c.md", Body: "more"}})
	body := "# ModelZ notes\nModelZ.3 is good at text.\n|---|---|\n- Ecosystem: ModelZ.3 (released 2026-01-15) has few LoRAs.\nunrelated"
	got := ix.BestLines(body, "When was ModelZ.3 released?", Known("modelz-notes.md", "ModelZ notes"), 1)
	if len(got) != 1 || got[0] != "- Ecosystem: ModelZ.3 (released 2026-01-15) has few LoRAs." {
		t.Errorf("got %q", got)
	}
}

func TestBestLinesCutLongLinesToTheMatch(t *testing.T) {
	ix := NewIndex([]Doc{{Path: "a.md", Body: "judge"}, {Path: "b.md", Body: "x"}, {Path: "c.md", Body: "y"}})
	line := "- Judge calibration: " + strings.Repeat("filler words here ", 20) + "Sonnet strict agreed best with the Opus labels at 89% " + strings.Repeat("tail ", 30)
	got := ix.BestLines(line, "which judge agreed best with the Opus labels", nil, 1)
	if len(got) != 1 || !strings.Contains(got[0], "89%") || len(got[0]) > LineChars+8 || !strings.HasPrefix(got[0], "…") {
		t.Errorf("got %q", got)
	}
}
