package wikicmd

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"wiki_skill/internal/vault"
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

func makeVault(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := vault.WriteNote(
		filepath.Join(root, "projects", "demo", "_index.md"),
		map[string]interface{}{"title": "Demo Index", "type": "index", "area": "demo"},
		"Links: [[first-note]]\n",
	); err != nil {
		t.Fatal(err)
	}
	if err := vault.WriteNote(
		filepath.Join(root, "projects", "demo", "first-note.md"),
		map[string]interface{}{"title": "First Note", "type": "atomic", "area": "demo"},
		"The first atomic note body.\n",
	); err != nil {
		t.Fatal(err)
	}
	if err := vault.WriteNote(
		filepath.Join(root, "projects", "demo", "orphan-note.md"),
		map[string]interface{}{"title": "Orphan Note", "type": "atomic", "area": "demo"},
		"Nobody links to this one.\n",
	); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestReindexReportsNoteCountAndDBPath(t *testing.T) {
	root := makeVault(t)

	report, err := Reindex(root, fakeEmbedder)
	if err != nil {
		t.Fatal(err)
	}
	if report.NotesIndexed != 2 {
		t.Errorf("got %d notes indexed, want 2", report.NotesIndexed)
	}
	wantDBPath := filepath.Join(root, ".wiki", "index.sqlite")
	if report.DBPath != wantDBPath {
		t.Errorf("got db path %q, want %q", report.DBPath, wantDBPath)
	}
	if _, err := os.Stat(report.DBPath); err != nil {
		t.Fatal(err)
	}
}

func TestSearchReportsGrepAndSemanticMatches(t *testing.T) {
	root := makeVault(t)
	if _, err := Reindex(root, fakeEmbedder); err != nil {
		t.Fatal(err)
	}

	report, err := Search(root, "first atomic note body", fakeEmbedder, 5)
	if err != nil {
		t.Fatal(err)
	}
	if report.Query != "first atomic note body" {
		t.Errorf("got query %q", report.Query)
	}
	found := false
	for _, m := range report.GrepMatches {
		if m.File == "projects/demo/first-note.md" {
			found = true
		}
		if filepath.IsAbs(m.File) {
			t.Errorf("expected vault-relative grep match path, got %q", m.File)
		}
	}
	if !found {
		t.Errorf("missing first-note.md in grep matches: %+v", report.GrepMatches)
	}
	if !report.SemanticIndexed {
		t.Error("expected SemanticIndexed to be true after reindex")
	}
	if len(report.SemanticMatches) == 0 {
		t.Error("expected at least one semantic match")
	}
}

func TestSearchWithoutIndexReportsNotIndexed(t *testing.T) {
	root := makeVault(t)

	report, err := Search(root, "anything", fakeEmbedder, 5)
	if err != nil {
		t.Fatal(err)
	}
	if report.SemanticIndexed {
		t.Error("expected SemanticIndexed false before any reindex")
	}
	if report.SemanticMatches != nil {
		t.Errorf("expected nil semantic matches, got %+v", report.SemanticMatches)
	}
}

func TestPruneReportsNoOrphansAndLeavesVaultIndexAlone(t *testing.T) {
	root := makeVault(t)

	data, err := Prune(root, 0.92, 180)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "orphans") {
		t.Errorf("prune should no longer report orphans: %s", encoded)
	}
}

func TestPruneFlagsStaleProjectFolder(t *testing.T) {
	root := makeVault(t)
	oldTime := time.Now().Add(-200 * 24 * time.Hour)
	filepath.WalkDir(filepath.Join(root, "projects", "demo"), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		return os.Chtimes(p, oldTime, oldTime)
	})

	report, err := Prune(root, 0.92, 180)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, sf := range report.StaleFolders {
		if sf.Bucket == "projects" && sf.Name == "demo" {
			found = true
		}
	}
	if !found {
		t.Errorf("missing projects/demo stale folder in %+v", report.StaleFolders)
	}
}

func TestProcessBatchUpsertsChangedAndRemovesDeleted(t *testing.T) {
	root := makeVault(t)
	if _, err := Reindex(root, fakeEmbedder); err != nil {
		t.Fatal(err)
	}

	newNotePath := filepath.Join(root, "projects", "demo", "new-note.md")
	if err := vault.WriteNote(
		newNotePath,
		map[string]interface{}{"title": "New Note", "type": "atomic", "area": "demo"},
		"Freshly written note.\n",
	); err != nil {
		t.Fatal(err)
	}
	deletedPath := filepath.Join(root, "projects", "demo", "first-note.md")

	db, err := sql.Open("sqlite", filepath.Join(root, ".wiki", "index.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	upserted, removed, err := ProcessBatch(root, db, []string{newNotePath}, []string{deletedPath}, fakeEmbedder)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(upserted, []string{"projects/demo/new-note.md"}) {
		t.Errorf("got upserted %v", upserted)
	}
	if !reflect.DeepEqual(removed, []string{"projects/demo/first-note.md"}) {
		t.Errorf("got removed %v", removed)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM notes WHERE path = ?", "projects/demo/new-note.md").Scan(&count)
	if count != 1 {
		t.Errorf("expected new-note.md to be indexed")
	}
	db.QueryRow("SELECT COUNT(*) FROM notes WHERE path = ?", "projects/demo/first-note.md").Scan(&count)
	if count != 0 {
		t.Errorf("expected first-note.md to be removed from index")
	}
}

func TestProcessBatchSkipsEmbedderForUnchangedNote(t *testing.T) {
	root := makeVault(t)
	if _, err := Reindex(root, fakeEmbedder); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", filepath.Join(root, ".wiki", "index.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	calls := 0
	countingEmbedder := func(texts []string) ([][]float64, error) {
		calls++
		return fakeEmbedder(texts)
	}

	unchangedPath := filepath.Join(root, "projects", "demo", "first-note.md")
	upserted, removed, err := ProcessBatch(root, db, []string{unchangedPath}, nil, countingEmbedder)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Errorf("got %d embedder calls, want 0 for an unchanged note", calls)
	}
	if len(upserted) != 0 {
		t.Errorf("expected no upserted paths for unchanged note, got %v", upserted)
	}
	if removed != nil {
		t.Errorf("expected nil removed, got %v", removed)
	}
}

func TestReindexWritesGeneratedVaultIndexAndSkipsItInSearchIndex(t *testing.T) {
	root := makeVault(t)

	report, err := Reindex(root, fakeEmbedder)
	if err != nil {
		t.Fatal(err)
	}
	if report.NotesIndexed != 2 {
		t.Errorf("got %d notes indexed, want 2 (generated _index.md is not searchable)", report.NotesIndexed)
	}

	data, err := os.ReadFile(filepath.Join(root, "projects", "demo", "_index.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{"[[first-note]]", "[[orphan-note]]"} {
		if !strings.Contains(got, want) {
			t.Errorf("generated index missing %s:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Links:") {
		t.Errorf("hand-written index content survived regeneration:\n%s", got)
	}
}

func TestReindexWritesVaultIndexEvenWhenEmbeddingFails(t *testing.T) {
	root := makeVault(t)
	failing := func(texts []string) ([][]float64, error) { return nil, errors.New("embed worker down") }

	if _, err := Reindex(root, failing); err == nil {
		t.Fatal("expected the embedding failure to surface")
	}
	data, err := os.ReadFile(filepath.Join(root, "projects", "demo", "_index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[[orphan-note]]") {
		t.Errorf("vault index was not regenerated before embedding:\n%s", data)
	}
}

func TestProcessBatchRegeneratesAffectedVaultIndexAndIgnoresIndexFiles(t *testing.T) {
	root := makeVault(t)
	if _, err := Reindex(root, fakeEmbedder); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(root, ".wiki", "index.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	newNotePath := filepath.Join(root, "projects", "demo", "new-note.md")
	if err := vault.WriteNote(newNotePath,
		map[string]interface{}{"title": "New Note", "type": "atomic"}, "Fresh.\n"); err != nil {
		t.Fatal(err)
	}
	removedPath := filepath.Join(root, "projects", "demo", "first-note.md")
	if err := os.Remove(removedPath); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(root, "projects", "demo", "_index.md")

	upserted, _, err := ProcessBatch(root, db, []string{newNotePath, indexPath}, []string{removedPath}, fakeEmbedder)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(upserted, []string{"projects/demo/new-note.md"}) {
		t.Errorf("got upserted %v, want only the new note", upserted)
	}
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "[[new-note]]") || strings.Contains(got, "[[first-note]]") {
		t.Errorf("vault index not refreshed:\n%s", got)
	}
}

func writeOverview(t *testing.T, root, kind, name string, meta map[string]interface{}) {
	t.Helper()
	meta["type"] = "overview"
	if err := vault.WriteNote(filepath.Join(root, kind, name, "_overview.md"), meta, "Authored prose.\n"); err != nil {
		t.Fatal(err)
	}
}

func writeNote(t *testing.T, root, rel, title string, extra map[string]interface{}, body string) string {
	t.Helper()
	meta := map[string]interface{}{"title": title, "type": "atomic"}
	for k, v := range extra {
		meta[k] = v
	}
	path := filepath.Join(root, rel)
	if err := vault.WriteNote(path, meta, body); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestIndexBrowsesFoldersThenOneFolder(t *testing.T) {
	root := makeVault(t)
	writeOverview(t, root, "projects", "demo", map[string]interface{}{"title": "Demo", "summary": "A demo project."})
	writeNote(t, root, "areas/health/sleep.md", "Sleep", nil, "Sleep notes.\n")

	top, err := Index(root, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []FolderSummary{
		{Kind: "projects", Name: "demo", Gist: "A demo project.", NoteCount: 2},
		{Kind: "areas", Name: "health", Gist: "", NoteCount: 1},
	}
	got := top.Folders
	sort.Slice(got, func(i, j int) bool { return got[i].Name < got[j].Name })
	sort.Slice(want, func(i, j int) bool { return want[i].Name < want[j].Name })
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got folders %+v, want %+v", got, want)
	}

	one, err := Index(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	wantNotes := []NoteSummary{
		{Path: "projects/demo/first-note.md", Title: "First Note"},
		{Path: "projects/demo/orphan-note.md", Title: "Orphan Note"},
	}
	if one.Folder != "demo" || !reflect.DeepEqual(one.Notes, wantNotes) {
		t.Errorf("got %+v, want notes %+v", one, wantNotes)
	}

	if _, err := Index(root, "nope"); err == nil {
		t.Error("expected an error for an unknown folder")
	}
}

var mapNow = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

func ageNote(t *testing.T, path string, days float64) {
	t.Helper()
	mtime := mapNow.Add(-time.Duration(days * 24 * float64(time.Hour)))
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func lineIndex(lines []string, substr string) int {
	for i, l := range lines {
		if strings.Contains(l, substr) {
			return i
		}
	}
	return -1
}

func mapVault(t *testing.T) string {
	root := t.TempDir()
	writeOverview(t, root, "projects", "ecu", map[string]interface{}{
		"title": "ECU", "summary": "ESP32 OBD-II device.", "paths": []string{"~/source_code/ecu", "/work/ecu/"},
	})
	writeOverview(t, root, "areas", "health", map[string]interface{}{"title": "Health"})
	ageNote(t, writeNote(t, root, "projects/ecu/can-bus.md", "CAN Bus", nil, "Bus notes.\n"), 400)
	ageNote(t, writeNote(t, root, "areas/health/sleep.md", "Sleep", nil, "Sleep.\n"), 400)
	ageNote(t, writeNote(t, root, "areas/health/hub.md", "Hub", nil, "Hub.\n"), 400)
	for _, n := range []string{"a", "b", "c"} {
		writeNote(t, root, "areas/health/linker-"+n+".md", "Linker "+n, nil, "See [[hub]] and [[hub|alias]].\n")
		ageNote(t, filepath.Join(root, "areas/health/linker-"+n+".md"), 400)
	}
	return root
}

func TestMapListsEveryFolderWithGistCountAndNudge(t *testing.T) {
	root := mapVault(t)

	out, err := Map(root, "/somewhere/else", mapNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"wiki search <query>",
		"- ecu (1): ESP32 OBD-II device.",
		"- health (5): Health",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("map missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "← cwd") {
		t.Errorf("no folder should be tagged when cwd matches nothing:\n%s", out)
	}
}

func TestMapRanksByBacklinksAndPutsPinsFirst(t *testing.T) {
	root := mapVault(t)
	writeNote(t, root, "areas/health/pinned.md", "Pinned One", map[string]interface{}{"pinned": true}, "Pinned.\n")
	ageNote(t, filepath.Join(root, "areas/health/pinned.md"), 400)

	out, err := Map(root, "/somewhere/else", mapNow)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	pin, hub, sleep := lineIndex(lines, "Pinned One"), lineIndex(lines, "Hub ["), lineIndex(lines, "Sleep [")
	if pin < 0 || hub < 0 || sleep < 0 {
		t.Fatalf("missing note lines (pin %d, hub %d, sleep %d):\n%s", pin, hub, sleep, out)
	}
	if !(pin < hub && hub < sleep) {
		t.Errorf("want pinned, then the backlinked hub, then the unlinked note; got %d, %d, %d:\n%s", pin, hub, sleep, out)
	}
}

func TestMapIgnoresLinksFromGeneratedIndexAndOverview(t *testing.T) {
	root := mapVault(t)
	// Sleep is linked only from _overview.md and the generated _index.md. If those links counted,
	// Sleep (3 backlinks) would outrank the fresh Linker a; ignored, it has none and is old.
	if err := os.WriteFile(filepath.Join(root, "areas/health/_overview.md"),
		[]byte("---\ntitle: Health\ntype: overview\n---\n[[sleep]] [[sleep]] [[sleep]]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteVaultIndexes(root); err != nil {
		t.Fatal(err)
	}
	ageNote(t, filepath.Join(root, "areas/health/linker-a.md"), 0)

	out, err := Map(root, "/somewhere/else", mapNow)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	linker, sleep := lineIndex(lines, "Linker a ["), lineIndex(lines, "Sleep [")
	if linker < 0 || sleep < 0 || linker > sleep {
		t.Errorf("fresh note should outrank Sleep when its overview/index links are ignored:\n%s", out)
	}
}

func TestMapCwdMatchBoostsFolderNotesAndTagsIt(t *testing.T) {
	root := mapVault(t)

	cases := []struct {
		cwd     string
		matches bool
	}{
		{os.ExpandEnv("$HOME/source_code/ecu"), true},
		{os.ExpandEnv("$HOME/source_code/ecu/firmware/src"), true},
		{"/work/ecu", true},
		{os.ExpandEnv("$HOME/source_code/ecu-tools"), false},
		{"/unrelated", false},
	}
	for _, c := range cases {
		out, err := Map(root, c.cwd, mapNow)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(out, "\n")
		tagged := lineIndex(lines, "- ecu (1): ESP32 OBD-II device. ← cwd") >= 0
		if tagged != c.matches {
			t.Errorf("cwd %q: tagged=%v, want %v", c.cwd, tagged, c.matches)
		}
		// CAN Bus has no backlinks and is old; only the cwd bonus can lift it above Hub.
		boosted := lineIndex(lines, "CAN Bus [") >= 0 && lineIndex(lines, "CAN Bus [") < lineIndex(lines, "Hub [")
		if boosted != c.matches {
			t.Errorf("cwd %q: cwd note ranked above hub=%v, want %v:\n%s", c.cwd, boosted, c.matches, out)
		}
	}
}

func TestMapStaysWithinTokenBudget(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 30; i++ {
		name := fmt.Sprintf("proj-%02d", i)
		writeOverview(t, root, "projects", name, map[string]interface{}{"title": name, "summary": "A project with a reasonably descriptive one-line gist."})
		for j := 0; j < 5; j++ {
			writeNote(t, root, fmt.Sprintf("projects/%s/note-%d.md", name, j), fmt.Sprintf("Note %d of %s with a long descriptive title", j, name), nil, "x\n")
		}
	}

	out, err := Map(root, "/x", mapNow)
	if err != nil {
		t.Fatal(err)
	}
	if tokens := len(out) / 4; tokens > 750 {
		t.Errorf("map is ~%d tokens, want at most 750", tokens)
	}
	if !strings.Contains(out, "proj-00") {
		t.Errorf("folder list must still be complete:\n%s", out)
	}
}

func TestPruneStaleFolderIgnoresRegeneratedVaultIndex(t *testing.T) {
	root := makeVault(t)
	oldTime := time.Now().Add(-200 * 24 * time.Hour)
	for _, n := range []string{"first-note.md", "orphan-note.md"} {
		if err := os.Chtimes(filepath.Join(root, "projects", "demo", n), oldTime, oldTime); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Reindex(root, fakeEmbedder); err != nil { // writes a fresh _index.md
		t.Fatal(err)
	}

	report, err := Prune(root, 0.92, 180)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.StaleFolders) != 1 || report.StaleFolders[0].Name != "demo" {
		t.Errorf("a regenerated _index.md must not hide staleness, got %+v", report.StaleFolders)
	}
}

func TestWriteVaultIndexesLeavesUnchangedIndexUntouched(t *testing.T) {
	root := makeVault(t)
	if err := WriteVaultIndexes(root); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "projects", "demo", "_index.md")
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	if err := WriteVaultIndexes(root); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(old.Truncate(time.Nanosecond)) && time.Since(info.ModTime()) < time.Hour {
		t.Errorf("unchanged index was rewritten (mtime %v)", info.ModTime())
	}
}

func TestIndexDistinguishesSameNamedFoldersInDifferentBuckets(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "projects/career/offer.md", "Offer", nil, "x\n")
	writeNote(t, root, "areas/career/contract.md", "Contract", nil, "x\n")

	_, err := Index(root, "career")
	if err == nil || !strings.Contains(err.Error(), "projects/career") || !strings.Contains(err.Error(), "areas/career") {
		t.Fatalf("a bare ambiguous name must error and list both folders, got %v", err)
	}

	got, err := Index(root, "areas/career")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Notes) != 1 || got.Notes[0].Title != "Contract" {
		t.Errorf("got %+v, want only the Area's note", got)
	}
}

func TestMapSkipsNoteWithUnreadableFrontmatter(t *testing.T) {
	root := mapVault(t)
	if err := os.WriteFile(filepath.Join(root, "areas/health/broken.md"), []byte("---\ntitle: [unclosed\n---\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := Map(root, "/x", mapNow)
	if err != nil {
		t.Fatalf("one bad note must not blank the map: %v", err)
	}
	if !strings.Contains(out, "Hub [") || strings.Contains(out, "broken") {
		t.Errorf("want the other notes listed and the broken one skipped:\n%s", out)
	}
}

func TestMapNudgeIsFactualNotAnOrder(t *testing.T) {
	out, err := Map(mapVault(t), "/x", mapNow)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "`wiki search <query>`") {
		t.Errorf("nudge must still name the command:\n%s", out)
	}
	if strings.Contains(out, "Search it") {
		t.Errorf("nudge should state facts, not give an order:\n%s", out)
	}
}
