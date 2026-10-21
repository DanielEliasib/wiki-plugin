package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCmd(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := newRootCmd()
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

func TestUnknownSubcommandFails(t *testing.T) {
	_, stderr, err := runCmd(t, "bogus")
	if err == nil {
		t.Fatal("expected an error for an unknown subcommand")
	}
	if !strings.Contains(stderr, "unknown command") {
		t.Errorf("expected cobra's unknown command message, got %q", stderr)
	}
}

func TestSearchFlagWorksBeforeAndAfterPositionalArg(t *testing.T) {
	root := t.TempDir()

	stdoutBefore, _, err := runCmd(t, "search", "hello", "--vault", root)
	if err != nil {
		t.Fatalf("query-then-flag order failed: %v", err)
	}
	if !strings.Contains(stdoutBefore, "no semantic index yet") {
		t.Errorf("got %q", stdoutBefore)
	}

	stdoutAfter, _, err := runCmd(t, "search", "--vault", root, "hello")
	if err != nil {
		t.Fatalf("flag-then-query order failed: %v", err)
	}
	if !strings.Contains(stdoutAfter, "no semantic index yet") {
		t.Errorf("got %q", stdoutAfter)
	}
}

func TestReindexJSONFlagProducesParseableOutput(t *testing.T) {
	root := t.TempDir()

	stdout, _, err := runCmd(t, "reindex", "--vault", root, "--json")
	if err != nil {
		t.Fatalf("reindex failed: %v", err)
	}
	var decoded struct {
		NotesIndexed int    `json:"notes_indexed"`
		DBPath       string `json:"db_path"`
	}
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatalf("stdout was not valid JSON: %v\n%s", err, stdout)
	}
	if decoded.NotesIndexed != 0 {
		t.Errorf("got %d notes indexed, want 0 for an empty vault", decoded.NotesIndexed)
	}
	wantDBPath := filepath.Join(root, ".wiki", "index.sqlite")
	if decoded.DBPath != wantDBPath {
		t.Errorf("got db path %q, want %q", decoded.DBPath, wantDBPath)
	}
}

func writeVaultFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMapHookReadsStdinAndEmitsAdditionalContext(t *testing.T) {
	root := t.TempDir()
	writeVaultFile(t, root, "projects/ecu/_overview.md", "---\ntitle: ECU\nsummary: ESP32 OBD-II device.\npaths:\n  - /work/ecu\n---\nProse.\n")
	writeVaultFile(t, root, "projects/ecu/can-bus.md", "---\ntitle: CAN Bus\n---\nBody.\n")

	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetIn(strings.NewReader(`{"cwd":"/work/ecu/src","source":"startup","session_id":"s"}`))
	cmd.SetArgs([]string{"map", "--hook", "--vault", root})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	var decoded struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("hook output is not JSON: %v\n%s", err, out.String())
	}
	h := decoded.HookSpecificOutput
	if h.HookEventName != "SessionStart" {
		t.Errorf("got hookEventName %q", h.HookEventName)
	}
	for _, want := range []string{"wiki search <query>", "- ecu (1): ESP32 OBD-II device. ← cwd", "CAN Bus"} {
		if !strings.Contains(h.AdditionalContext, want) {
			t.Errorf("additionalContext missing %q:\n%s", want, h.AdditionalContext)
		}
	}
}

func TestMapHookIsSilentAndSucceedsOnError(t *testing.T) {
	cases := map[string]struct {
		vault string
		stdin string
	}{
		"missing vault": {vault: filepath.Join(t.TempDir(), "nope"), stdin: `{"cwd":"/x"}`},
		"bad stdin":     {vault: t.TempDir(), stdin: `not json`},
	}
	for name, c := range cases {
		cmd := newRootCmd()
		var out, errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		cmd.SetIn(strings.NewReader(c.stdin))
		cmd.SetArgs([]string{"map", "--hook", "--vault", c.vault})
		if err := cmd.Execute(); err != nil {
			t.Errorf("%s: hook must exit 0, got %v", name, err)
		}
		if out.Len() != 0 || errOut.Len() != 0 {
			t.Errorf("%s: hook must print nothing, got stdout %q stderr %q", name, out.String(), errOut.String())
		}
	}
}

func TestMapCwdFlagPrintsPlainText(t *testing.T) {
	root := t.TempDir()
	writeVaultFile(t, root, "areas/health/sleep.md", "---\ntitle: Sleep\n---\nBody.\n")

	stdout, _, err := runCmd(t, "map", "--cwd", "/anywhere", "--vault", root)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(strings.TrimSpace(stdout), "{") || !strings.Contains(stdout, "- health (1)") {
		t.Errorf("expected plain text map, got %q", stdout)
	}
}

func TestMapNeverWritesToTheVault(t *testing.T) {
	root := t.TempDir()
	writeVaultFile(t, root, "areas/health/sleep.md", "---\ntitle: Sleep\n---\nBody.\n")

	if _, _, err := runCmd(t, "map", "--cwd", "/x", "--vault", root); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".wiki", "areas/health/_index.md"} {
		if _, err := os.Stat(filepath.Join(root, p)); err == nil {
			t.Errorf("wiki map must not create %s", p)
		}
	}
}

func TestIndexBrowsesFoldersAndOneFolder(t *testing.T) {
	root := t.TempDir()
	writeVaultFile(t, root, "areas/health/_overview.md", "---\ntitle: Health\nsummary: Sleep and fitness.\n---\n")
	writeVaultFile(t, root, "areas/health/sleep.md", "---\ntitle: Sleep\n---\nBody.\n")

	stdout, _, err := runCmd(t, "index", "--vault", root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "health") || !strings.Contains(stdout, "Sleep and fitness.") {
		t.Errorf("got %q", stdout)
	}

	stdout, _, err = runCmd(t, "index", "health", "--vault", root, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Folder string `json:"folder"`
		Notes  []struct {
			Path  string `json:"path"`
			Title string `json:"title"`
		} `json:"notes"`
	}
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Folder != "health" || len(decoded.Notes) != 1 || decoded.Notes[0].Title != "Sleep" {
		t.Errorf("got %+v", decoded)
	}

	if _, _, err := runCmd(t, "index", "nope", "--vault", root); err == nil {
		t.Error("expected an error for an unknown folder")
	}
}

func TestInstallDryRunChangesNothing(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "embed_worker"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"SKILL.md", filepath.Join("embed_worker", "embed_worker.py")} {
		if err := os.WriteFile(filepath.Join(repo, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	claudeDir := filepath.Join(t.TempDir(), ".claude")
	t.Setenv("CLAUDE_CONFIG_DIR", claudeDir)
	t.Setenv("WIKI_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

	stdout, _, err := runCmd(t, "install", "--repo", repo, "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Link the skill", "Write the wiki config", "Register the SessionStart hook"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("plan missing %q:\n%s", want, stdout)
		}
	}
	if _, err := os.Stat(claudeDir); err == nil {
		t.Error("dry run created the Claude dir")
	}
}

func TestInstallAbortsWithoutConfirmation(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), ".claude"))
	_, _, err := runCmd(t, "install", "--repo", repo)
	if err == nil {
		t.Fatal("want error for a directory that is not a checkout")
	}
}
