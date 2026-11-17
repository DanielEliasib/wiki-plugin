package vault

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseFrontmatterSplitsMetaAndBody(t *testing.T) {
	text := "---\ntitle: Example\ntype: atomic\n---\nBody text here.\n"
	meta, body, err := ParseFrontmatter(text)
	if err != nil {
		t.Fatal(err)
	}
	if meta["title"] != "Example" || meta["type"] != "atomic" {
		t.Errorf("got meta %v", meta)
	}
	if body != "Body text here.\n" {
		t.Errorf("got body %q", body)
	}
}

func TestParseFrontmatterNoFrontmatterReturnsEmptyMeta(t *testing.T) {
	meta, body, err := ParseFrontmatter("Just a body, no frontmatter.\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(meta) != 0 {
		t.Errorf("got meta %v, want empty", meta)
	}
	if body != "Just a body, no frontmatter.\n" {
		t.Errorf("got body %q", body)
	}
}

func TestRenderFrontmatterRoundTrips(t *testing.T) {
	meta := map[string]interface{}{"title": "Example", "type": "atomic"}
	rendered, err := RenderFrontmatter(meta, "Body text here.\n")
	if err != nil {
		t.Fatal(err)
	}
	parsedMeta, parsedBody, err := ParseFrontmatter(rendered)
	if err != nil {
		t.Fatal(err)
	}
	if parsedMeta["title"] != "Example" || parsedMeta["type"] != "atomic" {
		t.Errorf("got meta %v", parsedMeta)
	}
	if parsedBody != "Body text here.\n" {
		t.Errorf("got body %q", parsedBody)
	}
}

func TestWriteNoteThenReadNote(t *testing.T) {
	dir := t.TempDir()
	notePath := filepath.Join(dir, "Projects", "demo", "note.md")
	meta := map[string]interface{}{"title": "Demo", "type": "atomic"}
	if err := WriteNote(notePath, meta, "Hello.\n"); err != nil {
		t.Fatal(err)
	}
	note, err := ReadNote(notePath)
	if err != nil {
		t.Fatal(err)
	}
	if note.Meta["title"] != "Demo" || note.Meta["type"] != "atomic" {
		t.Errorf("got meta %v", note.Meta)
	}
	if note.Body != "Hello.\n" {
		t.Errorf("got body %q", note.Body)
	}
	if note.Path != notePath {
		t.Errorf("got path %q", note.Path)
	}
}

func TestIterNotesExcludesHiddenDirs(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "Projects", "demo"), 0o755)
	os.WriteFile(filepath.Join(dir, "Projects", "demo", "a.md"), []byte("a"), 0o644)
	os.MkdirAll(filepath.Join(dir, ".wiki"), 0o755)
	os.WriteFile(filepath.Join(dir, ".wiki", "conventions.md"), []byte("skip me"), 0o644)
	// a git worktree of the vault under .claude: copies of the notes, not notes
	os.MkdirAll(filepath.Join(dir, ".claude", "worktrees", "w", "Projects", "demo"), 0o755)
	os.WriteFile(filepath.Join(dir, ".claude", "worktrees", "w", "Projects", "demo", "a.md"), []byte("a"), 0o644)

	notes, err := IterNotes(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "Projects", "demo", "a.md")}
	if !reflect.DeepEqual(notes, want) {
		t.Errorf("got %v, want %v", notes, want)
	}
}

func TestClassifyDecidesWhatIsANote(t *testing.T) {
	cases := []struct {
		rel  string
		want Kind
	}{
		{"projects/demo/a.md", KindNote},
		{"a.md", KindNote},
		{"projects/demo/_overview.md", KindOverview},
		{"projects/demo/_index.md", KindGenerated},
		{"projects/demo/a.txt", KindNone},
		{"projects/demo/a.md.swp", KindNone},
		{".wiki/conventions.md", KindNone},
		{".obsidian/x.md", KindNone},
		{".claude/worktrees/w/projects/demo/a.md", KindNone},
		{"projects/.hidden/a.md", KindNone},
		{"projects/demo/_other.md", KindNote},
	}
	for _, c := range cases {
		if got := Classify(filepath.FromSlash(c.rel)); got != c.want {
			t.Errorf("Classify(%q) = %v, want %v", c.rel, got, c.want)
		}
	}
}

func TestIterNotesIncludesOverviewsButNotGeneratedIndexes(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "projects", "demo"), 0o755)
	for _, f := range []string{"a.md", "_overview.md", "_index.md"} {
		os.WriteFile(filepath.Join(dir, "projects", "demo", f), []byte("x"), 0o644)
	}
	notes, err := IterNotes(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(dir, "projects", "demo", "_overview.md"),
		filepath.Join(dir, "projects", "demo", "a.md"),
	}
	if !reflect.DeepEqual(notes, want) {
		t.Errorf("got %v, want %v", notes, want)
	}
}

func TestNoteTitleIsFrontmatterTitle(t *testing.T) {
	if got := (Note{Meta: map[string]interface{}{"title": "Demo"}}).Title(); got != "Demo" {
		t.Errorf("got %q", got)
	}
	if got := (Note{Meta: map[string]interface{}{"title": 3}}).Title(); got != "" {
		t.Errorf("non-string title should be empty, got %q", got)
	}
}

func TestParseFrontmatterAcceptsCRLFAndAnEmptyBlock(t *testing.T) {
	cases := []struct {
		name, text, wantTitle, wantBody string
	}{
		{"CRLF", "---\r\ntitle: Example\r\n---\r\nBody.\r\n", "Example", "Body.\r\n"},
		{"empty block", "---\n---\nBody.\n", "", "Body.\n"},
		{"empty block, CRLF", "---\r\n---\r\nBody.\r\n", "", "Body.\r\n"},
		{"a later --- line is the body's", "---\n---\nBody.\n---\nmore\n", "", "Body.\n---\nmore\n"},
		{"no frontmatter", "Just text.\n---\nnot a block\n", "", "Just text.\n---\nnot a block\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			meta, body, err := ParseFrontmatter(c.text)
			if err != nil {
				t.Fatal(err)
			}
			title, _ := meta["title"].(string)
			if title != c.wantTitle || body != c.wantBody {
				t.Errorf("title %q body %q, want %q %q", title, body, c.wantTitle, c.wantBody)
			}
		})
	}
}

func TestIsRecordSpotsLogsAndSources(t *testing.T) {
	cases := map[string]bool{
		"projects/demo/logs/maintenance-log.md":   true,
		"projects/demo/logs/2026/run.md":          true,
		"areas/demo/_sources.md":                  true,
		"projects/demo/notes/first-note.md":       false,
		"projects/demo/_overview.md":              false,
		"projects/demo/notes/logs.md":             false,
		"projects/demo/notes/catalogs/sources.md": false,
	}
	for rel, want := range cases {
		if got := IsRecord(filepath.FromSlash(rel)); got != want {
			t.Errorf("IsRecord(%q) = %v, want %v", rel, got, want)
		}
	}
}

func TestIsArchivedSpotsTheArchiveBucket(t *testing.T) {
	cases := map[string]bool{
		"archive/projects/demo/old.md":        true,
		"archive/old.md":                      true,
		"projects/demo/notes/archive/x.md":    false,
		"projects/archive-tools/_overview.md": false,
	}
	for rel, want := range cases {
		if got := IsArchived(filepath.FromSlash(rel)); got != want {
			t.Errorf("IsArchived(%q) = %v, want %v", rel, got, want)
		}
	}
}
