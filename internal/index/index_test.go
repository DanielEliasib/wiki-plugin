package index

import (
	"database/sql"
	"reflect"
	"sort"
	"testing"

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
