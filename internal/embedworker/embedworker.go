package embedworker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"wiki_skill/internal/config"
)

type Embedder func(texts []string) ([][]float64, error)

type request struct {
	Texts []string `json:"texts"`
}

type response struct {
	Vectors [][]float64 `json:"vectors"`
}

func NewEmbedder(workerDir string) Embedder {
	return func(texts []string) ([][]float64, error) {
		reqBytes, err := json.Marshal(request{Texts: texts})
		if err != nil {
			return nil, err
		}

		cmd := exec.Command("uv", "run", "--project", workerDir, "python", "embed_worker.py")
		cmd.Dir = workerDir
		cmd.Stdin = bytes.NewReader(reqBytes)
		if libPath := cudaLibraryPath(workerDir); libPath != "" {
			cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+libPath)
		}

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("embed worker failed: %w: %s", err, stderr.String())
		}

		var resp response
		if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
			return nil, fmt.Errorf("embed worker returned invalid JSON: %w", err)
		}
		return resp.Vectors, nil
	}
}

// cudaLibraryPath finds the pip-installed nvidia-cudnn-cu12 / nvidia-cublas-cu12
// shared library directories inside workerDir's uv-managed venv, so onnxruntime's
// CUDAExecutionProvider can dlopen libcudnn.so/libcublas.so without a system-wide
// CUDA install. Returns "" if the venv or those packages aren't present (e.g. in
// tests using a fake worker), in which case the child process just inherits the
// caller's environment unchanged.
func cudaLibraryPath(workerDir string) string {
	nvidiaDirs, err := filepath.Glob(filepath.Join(workerDir, ".venv", "lib", "python*", "site-packages", "nvidia"))
	if err != nil || len(nvidiaDirs) == 0 {
		return ""
	}
	var libDirs []string
	for _, pkg := range []string{"cudnn", "cublas", "cufft", "nvjitlink", "cuda_runtime", "curand"} {
		lib := filepath.Join(nvidiaDirs[0], pkg, "lib")
		if info, err := os.Stat(lib); err == nil && info.IsDir() {
			libDirs = append(libDirs, lib)
		}
	}
	if len(libDirs) == 0 {
		return ""
	}
	return strings.Join(libDirs, string(os.PathListSeparator))
}

// ResolveWorkerDir finds the embed worker directory: $WIKI_EMBED_WORKER_DIR,
// then embed_worker_dir in the config file, then ../embed_worker next to the
// executable (a `make build` checkout).
func ResolveWorkerDir() (string, error) {
	if dir := os.Getenv("WIKI_EMBED_WORKER_DIR"); dir != "" {
		return dir, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	if cfg.EmbedWorkerDir != "" {
		return cfg.EmbedWorkerDir, nil
	}
	if exe, err := os.Executable(); err == nil {
		if exe, err = filepath.EvalSymlinks(exe); err == nil {
			dir := filepath.Join(filepath.Dir(exe), "..", "embed_worker")
			if _, err := os.Stat(filepath.Join(dir, "embed_worker.py")); err == nil {
				return filepath.Clean(dir), nil
			}
		}
	}
	return "", errors.New("embed worker directory not set: set WIKI_EMBED_WORKER_DIR or embed_worker_dir in the config file")
}

// DefaultEmbedder embeds via the worker found by ResolveWorkerDir. If none can
// be found, the returned Embedder reports that on every call.
func DefaultEmbedder() Embedder {
	dir, err := ResolveWorkerDir()
	if err != nil {
		return func([]string) ([][]float64, error) { return nil, err }
	}
	return NewEmbedder(dir)
}
