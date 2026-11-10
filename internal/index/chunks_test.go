package index

import (
	"strings"
	"testing"
)

func TestSplitChunksBySection(t *testing.T) {
	text := "Title\nIntro paragraph that is long enough to stand on its own as a chunk of the note, with some words " +
		"about the setup and the reasons behind it, so it passes the minimum size of a chunk without any help from the next section.\n\n## Speed\nGPU power limit was 170 W; " +
		"43.1 t/s on the bench, measured three times with the same flags and the same prompt length of 6409 tokens, with the card at its default clocks and the fans on auto.\n" +
		"```\n# not a heading\n```\n## Notes\nshort"
	chunks := SplitChunks(text)
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks: %+v", len(chunks), chunks)
	}
	if chunks[0].Heading != "" || !strings.HasPrefix(chunks[0].Text, "Intro") {
		t.Errorf("first chunk %+v", chunks[0])
	}
	// the short "Notes" section is merged into the one before; the fenced "# not a heading" is not a heading
	if chunks[1].Heading != "Speed" || !strings.Contains(chunks[1].Text, "# not a heading") || !strings.HasSuffix(chunks[1].Text, "short") {
		t.Errorf("second chunk %+v", chunks[1])
	}
}

func TestSplitChunksCutsLongSections(t *testing.T) {
	para := strings.Repeat("word ", 100) // 500 characters
	chunks := SplitChunks("Title\n## Long\n" + para + "\n\n" + para + "\n\n" + para)
	if len(chunks) < 2 {
		t.Fatalf("got %d chunks", len(chunks))
	}
	for _, c := range chunks {
		if len(c.Text) > ChunkMaxChars || c.Heading != "Long" {
			t.Errorf("chunk of %d chars, heading %q", len(c.Text), c.Heading)
		}
	}
}

func TestSearchReturnsTheBestSectionOfEachNote(t *testing.T) {
	db := openTestDB(t)
	// an embedder that only sees whether a text mentions "power": the query and the matching section line up
	embed := func(texts []string) ([][]float64, error) {
		out := make([][]float64, len(texts))
		for i, s := range texts {
			if strings.Contains(s, "power") && !strings.Contains(s, "Intro") {
				out[i] = []float64{1, 0}
			} else {
				out[i] = []float64{0, 1}
			}
		}
		return out, nil
	}
	text := "Coder test\nIntro " + strings.Repeat("about the model ", 20) + "\n## Speed\nThe GPU power limit was 170 W " + strings.Repeat("during the bench ", 15)
	if err := UpsertNote(db, "a.md", text, embed); err != nil {
		t.Fatal(err)
	}
	res, err := Search(db, "power", embed, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Heading != "Speed" || !strings.Contains(res[0].Excerpt, "170 W") || res[0].Score < 0.99 {
		t.Errorf("got %+v", res)
	}
	if err := RemoveNote(db, "a.md"); err != nil {
		t.Fatal(err)
	}
	var n int
	db.QueryRow("SELECT COUNT(*) FROM chunks").Scan(&n)
	if n != 0 {
		t.Errorf("%d chunks left after RemoveNote", n)
	}
}

func TestInitDBAddsChunksAndMarksOldNotesForReembedding(t *testing.T) {
	db := openTestDB(t)
	if err := UpsertNote(db, "a.md", "Title\nbody", fakeEmbedder); err != nil {
		t.Fatal(err)
	}
	// an index from before chunks: drop the table, then open it again
	if _, err := db.Exec("DROP TABLE chunks"); err != nil {
		t.Fatal(err)
	}
	if err := InitDB(db); err != nil {
		t.Fatal(err)
	}
	if hash, _, _ := HashForPath(db, "a.md"); hash != "" {
		t.Errorf("hash %q should be cleared so the note is embedded again", hash)
	}
	calls := 0
	if _, err := UpsertBatch(db, []NoteText{{Path: "a.md", Text: "Title\nbody"}}, countingEmbedder(&calls)); err != nil || calls != 1 {
		t.Errorf("err %v, embedder calls %d", err, calls)
	}
}
