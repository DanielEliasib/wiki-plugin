package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileIsEmpty(t *testing.T) {
	t.Setenv("WIKI_CONFIG", filepath.Join(t.TempDir(), "nope.yaml"))
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg != (Config{}) {
		t.Errorf("got %+v, want empty", cfg)
	}
}

func TestLoadReadsKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("vault: ~/notes\nembed_worker_dir: /opt/worker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WIKI_CONFIG", path)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Vault != "~/notes" || cfg.EmbedWorkerDir != "/opt/worker" {
		t.Errorf("got %+v", cfg)
	}
}

func TestLoadInvalidYAMLErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("vault: [unclosed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WIKI_CONFIG", path)
	if _, err := Load(); err == nil {
		t.Fatal("want error for invalid YAML")
	}
}

func TestPathHonorsEnv(t *testing.T) {
	t.Setenv("WIKI_CONFIG", "/x/y.yaml")
	got, err := Path()
	if err != nil || got != "/x/y.yaml" {
		t.Errorf("got %q, %v", got, err)
	}
}
