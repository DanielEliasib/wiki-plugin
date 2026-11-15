package index

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"wiki_skill/internal/embedworker"
)

func fakeEmbedder(texts []string) ([][]float64, error) {
	vectors := make([][]float64, len(texts))
	for i, t := range texts {
		length := float64(len(t)%11 + 1)
		total := 0
		for _, c := range t {
			total += int(c)
		}
		vectors[i] = []float64{length, float64(total%17 + 1)}
	}
	return vectors, nil
}

func countingEmbedder(calls *int) embedworker.Embedder {
	return func(texts []string) ([][]float64, error) {
		*calls++
		return fakeEmbedder(texts)
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := InitDB(db); err != nil {
		t.Fatal(err)
	}
	return db
}

var _ embedworker.Embedder = fakeEmbedder

func TestUpsertThenSearchFindsExactTextMatch(t *testing.T) {
	db := openTestDB(t)
	if err := UpsertNote(db, "a.md", "notes about fastembed setup", fakeEmbedder); err != nil {
		t.Fatal(err)
	}
	if err := UpsertNote(db, "b.md", "completely unrelated grocery list", fakeEmbedder); err != nil {
		t.Fatal(err)
	}

	results, err := Search(db, "notes about fastembed setup", fakeEmbedder, 2)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Path != "a.md" {
		t.Errorf("got %q, want a.md", results[0].Path)
	}
	if results[0].Score < 0.999999 {
		t.Errorf("got score %v, want ~1.0", results[0].Score)
	}
}

func TestUpsertIsIdempotentOnConflict(t *testing.T) {
	db := openTestDB(t)
	if err := UpsertNote(db, "a.md", "first version", fakeEmbedder); err != nil {
		t.Fatal(err)
	}
	if err := UpsertNote(db, "a.md", "second version", fakeEmbedder); err != nil {
		t.Fatal(err)
	}

	var text string
	if err := db.QueryRow("SELECT text FROM notes WHERE path = ?", "a.md").Scan(&text); err != nil {
		t.Fatal(err)
	}
	if text != "second version" {
		t.Errorf("got %q", text)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM notes").Scan(&count)
	if count != 1 {
		t.Errorf("got %d rows, want 1", count)
	}
}

func TestRemoveNoteDeletesRow(t *testing.T) {
	db := openTestDB(t)
	if err := UpsertNote(db, "a.md", "some text", fakeEmbedder); err != nil {
		t.Fatal(err)
	}
	if err := RemoveNote(db, "a.md"); err != nil {
		t.Fatal(err)
	}
	var count int
	db.QueryRow("SELECT COUNT(*) FROM notes").Scan(&count)
	if count != 0 {
		t.Errorf("got %d rows, want 0", count)
	}
}

func TestRebuildIndexReplacesAllRows(t *testing.T) {
	db := openTestDB(t)
	if err := UpsertNote(db, "stale.md", "old text", fakeEmbedder); err != nil {
		t.Fatal(err)
	}

	notes := []NoteText{{Path: "a.md", Text: "fresh text one"}, {Path: "b.md", Text: "fresh text two"}}
	if err := RebuildIndex(db, notes, fakeEmbedder); err != nil {
		t.Fatal(err)
	}

	rows, err := db.Query("SELECT path FROM notes")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	sort.Strings(paths)
	want := []string{"a.md", "b.md"}
	if !reflect.DeepEqual(paths, want) {
		t.Errorf("got %v, want %v", paths, want)
	}
}

func TestUpsertNoteSkipsEmbedderWhenHashUnchanged(t *testing.T) {
	db := openTestDB(t)
	calls := 0
	embed := countingEmbedder(&calls)

	if err := UpsertNote(db, "a.md", "same text", embed); err != nil {
		t.Fatal(err)
	}
	if err := UpsertNote(db, "a.md", "same text", embed); err != nil {
		t.Fatal(err)
	}

	if calls != 1 {
		t.Errorf("got %d embedder calls, want 1", calls)
	}
}

func TestUpsertNoteReembedsWhenHashChanges(t *testing.T) {
	db := openTestDB(t)
	calls := 0
	embed := countingEmbedder(&calls)

	if err := UpsertNote(db, "a.md", "first", embed); err != nil {
		t.Fatal(err)
	}
	if err := UpsertNote(db, "a.md", "second", embed); err != nil {
		t.Fatal(err)
	}

	if calls != 2 {
		t.Errorf("got %d embedder calls, want 2", calls)
	}
}

func TestUpsertBatchEmbedsOnlyChangedNotesInOneCall(t *testing.T) {
	db := openTestDB(t)
	calls := 0
	embed := countingEmbedder(&calls)

	if err := UpsertNote(db, "unchanged.md", "same text", embed); err != nil {
		t.Fatal(err)
	}
	calls = 0 // reset after seeding the "unchanged" row

	upserted, err := UpsertBatch(db, []NoteText{
		{Path: "unchanged.md", Text: "same text"},
		{Path: "new.md", Text: "brand new text"},
	}, embed)
	if err != nil {
		t.Fatal(err)
	}

	if calls != 1 {
		t.Errorf("got %d embedder calls, want 1", calls)
	}
	wantUpserted := []string{"new.md"}
	if !reflect.DeepEqual(upserted, wantUpserted) {
		t.Errorf("got upserted %v, want %v", upserted, wantUpserted)
	}

	rows, err := db.Query("SELECT path FROM notes ORDER BY path")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var p string
		rows.Scan(&p)
		paths = append(paths, p)
	}
	want := []string{"new.md", "unchanged.md"}
	if !reflect.DeepEqual(paths, want) {
		t.Errorf("got %v, want %v", paths, want)
	}
}

func TestInitDBMigratesOldSchemaWithoutHashColumn(t *testing.T) {
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Simulate the pre-hash schema this tool used to create.
	if _, err := conn.Exec(`
		CREATE TABLE notes (
			path TEXT PRIMARY KEY,
			text TEXT NOT NULL,
			embedding BLOB NOT NULL
		)
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(
		"INSERT INTO notes (path, text, embedding) VALUES (?, ?, ?)",
		"old.md", "old text", []byte{0, 0, 0, 0},
	); err != nil {
		t.Fatal(err)
	}

	if err := InitDB(conn); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := conn.QueryRow("SELECT COUNT(*) FROM notes").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected old-schema table to be dropped and recreated empty, got %d rows", count)
	}

	if _, err := conn.Exec(
		"INSERT INTO notes (path, text, hash, embedding) VALUES (?, ?, ?, ?)",
		"new.md", "new text", HashText("new text"), []byte{0, 0, 0, 0},
	); err != nil {
		t.Fatal(err)
	}
}

func TestRebuildIndexKeepsOldIndexWhenEmbeddingFails(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := InitDB(db); err != nil {
		t.Fatal(err)
	}
	ok := func(texts []string) ([][]float64, error) {
		out := make([][]float64, len(texts))
		for i := range texts {
			out[i] = []float64{1, 0}
		}
		return out, nil
	}
	if err := RebuildIndex(db, []NoteText{{Path: "a.md", Text: "a"}, {Path: "b.md", Text: "b"}}, ok); err != nil {
		t.Fatal(err)
	}
	failing := func([]string) ([][]float64, error) { return nil, errors.New("embedder down") }
	if err := RebuildIndex(db, []NoteText{{Path: "c.md", Text: "c"}}, failing); err == nil {
		t.Fatal("want the embedder error")
	}
	var n int
	if err := db.QueryRow("SELECT count(*) FROM notes").Scan(&n); err != nil || n != 2 {
		t.Fatalf("rows after failed rebuild = %d (%v), want the old 2", n, err)
	}
}

func dimEmbedder(dim int) embedworker.Embedder {
	return func(texts []string) ([][]float64, error) {
		out := make([][]float64, len(texts))
		for i, t := range texts {
			v := make([]float64, dim)
			for j := range v {
				v[j] = float64(len(t)%7 + j + 1)
			}
			out[i] = v
		}
		return out, nil
	}
}

func TestFailedWriteLeavesTheOldNoteAndLetsItBeRetried(t *testing.T) {
	db := openTestDB(t)
	if err := UpsertNote(db, "a.md", "# A\nold text\n", fakeEmbedder); err != nil {
		t.Fatal(err)
	}
	oldHash, _, _ := HashForPath(db, "a.md")
	if _, err := db.Exec(`CREATE TRIGGER boom BEFORE INSERT ON chunks WHEN new.text LIKE '%BOOM%' BEGIN SELECT RAISE(ABORT, 'disk full'); END`); err != nil {
		t.Fatal(err)
	}

	newText := "# A\nnew text BOOM\n"
	if _, err := UpsertBatch(db, []NoteText{{Path: "a.md", Text: newText}}, fakeEmbedder); err == nil {
		t.Fatal("expected the write to fail")
	}
	hash, _, _ := HashForPath(db, "a.md")
	if hash != oldHash {
		t.Errorf("hash moved to %s although the write failed", hash)
	}
	var chunks int
	db.QueryRow("SELECT COUNT(*) FROM chunks WHERE path = 'a.md'").Scan(&chunks)
	if chunks == 0 {
		t.Error("the note's chunks were lost by the failed write")
	}

	db.Exec("DROP TRIGGER boom")
	upserted, err := UpsertBatch(db, []NoteText{{Path: "a.md", Text: newText}}, fakeEmbedder)
	if err != nil || len(upserted) != 1 {
		t.Errorf("retry: upserted %v, err %v", upserted, err)
	}
}

func TestSearchRefusesAQueryOfAnotherDimension(t *testing.T) {
	db := openTestDB(t)
	if err := UpsertNote(db, "a.md", "some text", dimEmbedder(2)); err != nil {
		t.Fatal(err)
	}
	for _, dim := range []int{1, 3} {
		_, err := Search(db, "some text", dimEmbedder(dim), 5)
		if err == nil || !strings.Contains(err.Error(), "reindex --full") {
			t.Errorf("dim %d: got err %v, want one that names `wiki reindex --full`", dim, err)
		}
	}
	if _, err := Search(db, "some text", dimEmbedder(2), 5); err != nil {
		t.Errorf("same dimension: %v", err)
	}
}

func TestUpsertRefusesAVectorOfAnotherDimension(t *testing.T) {
	db := openTestDB(t)
	if err := UpsertNote(db, "a.md", "some text", dimEmbedder(2)); err != nil {
		t.Fatal(err)
	}
	if _, err := UpsertBatch(db, []NoteText{{Path: "b.md", Text: "other text"}}, dimEmbedder(3)); err == nil || !strings.Contains(err.Error(), "reindex --full") {
		t.Errorf("got err %v", err)
	}
	if hash, found, _ := HashForPath(db, "b.md"); found {
		t.Errorf("b.md was written (%s)", hash)
	}
}

func TestRebuildSwitchesTheDimension(t *testing.T) {
	db := openTestDB(t)
	if err := UpsertNote(db, "a.md", "some text", dimEmbedder(2)); err != nil {
		t.Fatal(err)
	}
	if err := RebuildIndex(db, []NoteText{{Path: "a.md", Text: "some text"}}, dimEmbedder(3)); err != nil {
		t.Fatal(err)
	}
	if _, err := Search(db, "some text", dimEmbedder(3), 5); err != nil {
		t.Errorf("after the rebuild: %v", err)
	}
	if _, err := Search(db, "some text", dimEmbedder(2), 5); err == nil {
		t.Error("the old dimension should now be refused")
	}
}

func TestEmbedderReturningMixedLengthsIsAnError(t *testing.T) {
	db := openTestDB(t)
	mixed := func(texts []string) ([][]float64, error) {
		out := make([][]float64, len(texts))
		for i := range texts {
			out[i] = make([]float64, 2+i%2)
		}
		return out, nil
	}
	if err := RebuildIndex(db, []NoteText{{Path: "a.md", Text: "x\n\n## b\ny"}}, mixed); err == nil {
		t.Error("expected an error")
	}
}

func TestOpenCreatesTheIndexInsideTheVault(t *testing.T) {
	root := t.TempDir()
	db, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := UpsertNote(db, "a.md", "text", fakeEmbedder); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Path(root)); err != nil {
		t.Errorf("no index file at %s: %v", Path(root), err)
	}
	if got := Path(root); got != filepath.Join(root, ".wiki", "index.sqlite") {
		t.Errorf("Path = %s", got)
	}
}

func TestNearDuplicatesPairsNotesAboveTheThreshold(t *testing.T) {
	db := openTestDB(t)
	same := func(texts []string) ([][]float64, error) {
		out := make([][]float64, len(texts))
		for i, t := range texts {
			if strings.HasPrefix(t, "other") {
				out[i] = []float64{0, 1}
			} else {
				out[i] = []float64{1, 0}
			}
		}
		return out, nil
	}
	for path, text := range map[string]string{"a.md": "first", "b.md": "second", "c.md": "other"} {
		if err := UpsertNote(db, path, text, same); err != nil {
			t.Fatal(err)
		}
	}
	pairs, err := NearDuplicates(db, 0.92)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 1 || pairs[0].PathA != "a.md" || pairs[0].PathB != "b.md" || pairs[0].Similarity < 0.99 {
		t.Errorf("got %+v, want only a.md/b.md", pairs)
	}
}

func TestCosineOfDifferentLengthsIsZeroNotAPanic(t *testing.T) {
	if got := Cosine([]float64{1, 2}, []float64{1, 2, 3}); got != 0 {
		t.Errorf("got %v", got)
	}
}

func TestLockExcludesAnotherHolderUntilReleased(t *testing.T) {
	root := t.TempDir()
	unlock, err := Lock(root)
	if err != nil {
		t.Fatal(err)
	}
	acquired := make(chan struct{})
	go func() {
		u, err := Lock(root)
		if err != nil {
			t.Error(err)
			return
		}
		close(acquired)
		u()
	}()
	select {
	case <-acquired:
		t.Fatal("a second holder got the lock while it was held")
	case <-time.After(200 * time.Millisecond):
	}
	unlock()
	select {
	case <-acquired:
	case <-time.After(5 * time.Second):
		t.Fatal("the lock was not handed over after unlock")
	}
}
