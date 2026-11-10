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
	if got := Terms("eval of 2026-10-22."); !reflect.DeepEqual(got[:2], []string{"eval", "2026-10-22"}) {
		t.Errorf("got %v", got)
	}
}

func TestNoteNamedAfterTheQuestionRanksFirst(t *testing.T) {
	ix := NewIndex([]Doc{
		{Path: "p/notes/other.md", Title: "Other", Body: "see the conversational eval for the speed numbers"},
		{Path: "p/notes/conversational-eval-2026-10-22.md", Title: "Conversational eval", Body: "speed 43.1 t/s"},
	})
	hits := ix.Search("How fast was the model in the conversational eval of 2026-10-22?", 5)
	if len(hits) == 0 || hits[0].Doc.Path != "p/notes/conversational-eval-2026-10-22.md" {
		t.Errorf("got %+v", hits)
	}
}

func TestMatchDiscountsWordsOfTheNoteTitle(t *testing.T) {
	ix := NewIndex([]Doc{{Path: "a.md", Body: "flash coder gpu power limit"}, {Path: "b.md", Body: "other words"}})
	known := Known("flash-coder-test.md", "Flash Coder test")
	q := "GPU power limit in the Flash Coder test"
	if title, fact := ix.Match("Flash Coder test, model details", q, known), ix.Match("GPU power limit was 170 W", q, known); fact <= title {
		t.Errorf("title line %v should score below the fact line %v", title, fact)
	}
}

func TestBestLinesFindTheFact(t *testing.T) {
	ix := NewIndex([]Doc{{Path: "a.md", Body: "flux.3 released date"}, {Path: "b.md", Body: "other"}, {Path: "c.md", Body: "more"}})
	body := "# FLUX notes\nFLUX.3 is good at text.\n|---|---|\n- Ecosystem: FLUX.3 (released 2026-10-15) has few LoRAs.\nunrelated"
	got := ix.BestLines(body, "When was FLUX.3 released?", Known("flux-notes.md", "FLUX notes"), 1)
	if len(got) != 1 || got[0] != "- Ecosystem: FLUX.3 (released 2026-10-15) has few LoRAs." {
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
