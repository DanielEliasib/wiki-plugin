package embedworker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wiki_skill/internal/config"
)

func writeFakeWorker(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	pyproject := `[project]
name = "fake-worker"
version = "0.1.0"
requires-python = ">=3.11"
dependencies = []

[project.optional-dependencies]
cpu = []
gpu = []

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
	embed := NewEmbedder(dir, "cpu")

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

func TestFromSettingsUsesTheWorker(t *testing.T) {
	dir := writeFakeWorker(t)
	vectors, err := FromSettings(config.Settings{EmbedWorkerDir: dir, EmbedDevice: "cpu"})([]string{"hi"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 1 {
		t.Fatalf("got %d vectors, want 1", len(vectors))
	}
}

func TestFromSettingsErrorsWhenNoWorkerIsSet(t *testing.T) {
	if _, err := FromSettings(config.Settings{EmbedDevice: "cpu"})([]string{"hi"}); err == nil {
		t.Fatal("want error when no worker dir is configured")
	}
}

func TestFromSettingsRejectsAnUnknownDeviceOnUse(t *testing.T) {
	embed := FromSettings(config.Settings{EmbedWorkerDir: writeFakeWorker(t), EmbedDevice: "tpu"})
	if _, err := embed([]string{"hi"}); err == nil || !strings.Contains(err.Error(), "cpu or gpu") {
		t.Errorf("got %v", err)
	}
}
