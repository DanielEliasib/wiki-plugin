package vault

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestResolveVaultRootDefaultsToHomeNotesWiki(t *testing.T) {
	t.Setenv("WIKI_ROOT", "")
	t.Setenv("WIKI_CONFIG", filepath.Join(t.TempDir(), "none.yaml"))
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveVaultRoot("")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "notes", "wiki")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveVaultRootHonorsEnvVar(t *testing.T) {
	t.Setenv("WIKI_ROOT", "/env/path")
	got, err := ResolveVaultRoot("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/env/path" {
		t.Errorf("got %q, want /env/path", got)
	}
}

func TestResolveVaultRootFlagWinsOverEnvVar(t *testing.T) {
	t.Setenv("WIKI_ROOT", "/env/path")
	got, err := ResolveVaultRoot("/flag/path")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/flag/path" {
		t.Errorf("got %q, want /flag/path", got)
	}
}

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

func TestIterNotesExcludesDotWikiDir(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "Projects", "demo"), 0o755)
	os.WriteFile(filepath.Join(dir, "Projects", "demo", "a.md"), []byte("a"), 0o644)
	os.MkdirAll(filepath.Join(dir, ".wiki"), 0o755)
	os.WriteFile(filepath.Join(dir, ".wiki", "conventions.md"), []byte("skip me"), 0o644)

	notes, err := IterNotes(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "Projects", "demo", "a.md")}
	if !reflect.DeepEqual(notes, want) {
		t.Errorf("got %v, want %v", notes, want)
	}
}

func TestResolveVaultRootUsesConfigFile(t *testing.T) {
	t.Setenv("WIKI_ROOT", "")
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("vault: /cfg/path\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WIKI_CONFIG", cfgPath)
	got, err := ResolveVaultRoot("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/cfg/path" {
		t.Errorf("got %q, want /cfg/path", got)
	}
	t.Setenv("WIKI_ROOT", "/env/path")
	got, _ = ResolveVaultRoot("")
	if got != "/env/path" {
		t.Errorf("env should win over config, got %q", got)
	}
}
