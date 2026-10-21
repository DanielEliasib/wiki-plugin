package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) Options {
	t.Helper()
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "SKILL.md"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "embed_worker"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "embed_worker", "embed_worker.py"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return Options{
		RepoDir:    repo,
		ClaudeDir:  filepath.Join(t.TempDir(), ".claude"),
		ConfigPath: filepath.Join(t.TempDir(), "wiki", "config.yaml"),
	}
}

func applyAll(t *testing.T, o Options) Plan {
	t.Helper()
	p, err := MakePlan(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range p.Steps {
		if err := s.Apply(); err != nil {
			t.Fatalf("%s: %v", s.Name, err)
		}
	}
	return p
}

func TestFindRepoRejectsOtherDirectories(t *testing.T) {
	if _, err := FindRepo(t.TempDir()); err == nil {
		t.Fatal("want error for a directory that is not a checkout")
	}
	o := fixture(t)
	if got, err := FindRepo(o.RepoDir); err != nil || got != o.RepoDir {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestInstallOnFreshMachineThenIdempotent(t *testing.T) {
	o := fixture(t)
	if p := applyAll(t, o); len(p.Steps) != 3 {
		t.Fatalf("fresh install planned %d steps, want 3", len(p.Steps))
	}

	if target, err := os.Readlink(filepath.Join(o.ClaudeDir, "skills", "wiki")); err != nil || target != o.RepoDir {
		t.Errorf("skill link: %q, %v", target, err)
	}
	cfg, _ := os.ReadFile(o.ConfigPath)
	if !strings.Contains(string(cfg), "embed_worker_dir: "+filepath.Join(o.RepoDir, "embed_worker")) {
		t.Errorf("config: %q", cfg)
	}
	data, _ := os.ReadFile(filepath.Join(o.ClaudeDir, "settings.json"))
	if !strings.Contains(string(data), `"command": "wiki map --hook"`) {
		t.Errorf("settings: %s", data)
	}

	again, err := MakePlan(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Steps) != 0 {
		t.Errorf("second run planned %d steps, want 0", len(again.Steps))
	}
}

func TestHookMergePreservesExistingSettings(t *testing.T) {
	o := fixture(t)
	if err := os.MkdirAll(o.ClaudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	original := `{"model":"x","big":12345678901234567890,"hooks":{"SessionStart":[{"matcher":"a","hooks":[{"type":"command","command":"other <&>"}]}],"Stop":[]}}`
	path := filepath.Join(o.ClaudeDir, "settings.json")
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	applyAll(t, o)

	data, _ := os.ReadFile(path)
	var got struct {
		Model string `json:"model"`
		Hooks struct {
			SessionStart []struct {
				Matcher string `json:"matcher"`
			} `json:"SessionStart"`
			Stop []any `json:"Stop"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Model != "x" || len(got.Hooks.SessionStart) != 2 || got.Hooks.Stop == nil {
		t.Errorf("settings not merged cleanly: %s", data)
	}
	for _, want := range []string{"12345678901234567890", "other <&>"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("lost %q: %s", want, data)
		}
	}
	if bak, _ := os.ReadFile(path + ".bak"); string(bak) != original {
		t.Errorf("backup differs: %s", bak)
	}
}

func TestInvalidSettingsAreNeverTouched(t *testing.T) {
	o := fixture(t)
	if err := os.MkdirAll(o.ClaudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(o.ClaudeDir, "settings.json")
	if err := os.WriteFile(path, []byte("{ // comment\n}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := MakePlan(o); err == nil {
		t.Fatal("want error for settings that are not valid JSON")
	}
	if data, _ := os.ReadFile(path); string(data) != "{ // comment\n}" {
		t.Errorf("settings modified: %q", data)
	}
}

func TestExistingConfigAndForeignLink(t *testing.T) {
	o := fixture(t)
	if err := os.MkdirAll(filepath.Dir(o.ConfigPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(o.ConfigPath, []byte("vault: /mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	applyAll(t, o)
	if data, _ := os.ReadFile(o.ConfigPath); string(data) != "vault: /mine\n" {
		t.Errorf("existing config overwritten: %q", data)
	}

	link := filepath.Join(o.ClaudeDir, "skills", "wiki")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/somewhere/else", link); err != nil {
		t.Fatal(err)
	}
	if _, err := MakePlan(o); err == nil {
		t.Fatal("want error when the skill link points elsewhere")
	}
}
