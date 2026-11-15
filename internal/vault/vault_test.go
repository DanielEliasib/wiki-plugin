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
