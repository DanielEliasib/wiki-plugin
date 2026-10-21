package embedworker

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFakeWorker(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	pyproject := `[project]
name = "fake-worker"
version = "0.1.0"
requires-python = ">=3.11"
dependencies = []

[tool.uv]
package = false
`
	script := `import json, sys
payload = json.load(sys.stdin)
vectors = []
for text in payload["texts"]:
    length = len(text) % 11 + 1
    total = sum(ord(c) for c in text) % 17 + 1
    vectors.append([float(length), float(total)])
json.dump({"vectors": vectors}, sys.stdout)
`
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(pyproject), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "embed_worker.py"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestNewEmbedderReturnsVectorsFromFakeWorker(t *testing.T) {
	dir := writeFakeWorker(t)
	embed := NewEmbedder(dir)

	vectors, err := embed([]string{"hi", "hello there"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 2 {
		t.Fatalf("got %d vectors, want 2", len(vectors))
	}
	// "hi" has length 2 -> (2%11+1)=3, ord sum 104+105=209 -> 209%17+1=6
	if vectors[0][0] != 3 || vectors[0][1] != 6 {
		t.Errorf("got %v, want [3 6]", vectors[0])
	}
}

func TestDefaultEmbedderHonorsEnvVar(t *testing.T) {
	dir := writeFakeWorker(t)
	t.Setenv("WIKI_EMBED_WORKER_DIR", dir)

	embed := DefaultEmbedder()
	vectors, err := embed([]string{"hi"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 1 {
		t.Fatalf("got %d vectors, want 1", len(vectors))
	}
}

func TestDefaultEmbedderReadsConfigFile(t *testing.T) {
	dir := writeFakeWorker(t)
	t.Setenv("WIKI_EMBED_WORKER_DIR", "")
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("embed_worker_dir: "+dir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WIKI_CONFIG", cfgPath)
	got, err := ResolveWorkerDir()
	if err != nil || got != dir {
		t.Errorf("got %q, %v; want %q", got, err, dir)
	}
}

func TestDefaultEmbedderErrorsWhenUnconfigured(t *testing.T) {
	t.Setenv("WIKI_EMBED_WORKER_DIR", "")
	t.Setenv("WIKI_CONFIG", filepath.Join(t.TempDir(), "none.yaml"))
	if _, err := DefaultEmbedder()([]string{"hi"}); err == nil {
		t.Fatal("want error when no worker dir is configured")
	}
}
