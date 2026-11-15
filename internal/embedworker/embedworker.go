package embedworker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"wiki_skill/internal/config"
)

type Embedder func(texts []string) ([][]float64, error)

type request struct {
	Texts []string `json:"texts"`
}

type response struct {
	Vectors [][]float64 `json:"vectors"`
}

// workerTimeout bounds one call to the worker. The first call downloads the model, which takes minutes on a slow
// connection, so this is generous; a hung uv must not hang search and watch forever.
var workerTimeout = 10 * time.Minute

// NewEmbedder runs the worker in workerDir on the given device ("cpu" or "gpu").
func NewEmbedder(workerDir, device string) Embedder {
	return func(texts []string) ([][]float64, error) {
		reqBytes, err := json.Marshal(request{Texts: texts})
		if err != nil {
			return nil, err
		}

		// --exact keeps the venv to the chosen extra, so switching between cpu
		// and gpu never leaves both fastembed packages installed.
		ctx, cancel := context.WithTimeout(context.Background(), workerTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, "uv", "run", "--exact", "--extra", device, "--project", workerDir, "python", "embed_worker.py")
		cmd.Dir = workerDir
		cmd.Stdin = bytes.NewReader(reqBytes)
		cmd.Env = append(os.Environ(), "WIKI_EMBED_DEVICE="+device)
		if device == "gpu" {
			if libPath := cudaLibraryPath(workerDir); libPath != "" {
				cmd.Env = append(cmd.Env, "LD_LIBRARY_PATH="+libPath)
			}
		}

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		cmd.WaitDelay = 2 * time.Second // children of uv that keep the pipes open must not hold us either

		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("embed worker timed out after %s", workerTimeout)
			}
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

// FromSettings embeds via the endpoint in the settings when one is set, else via the local worker in the settings'
// worker directory on its device. If the settings cannot be used (no worker directory, an unknown device), the
// returned Embedder reports that on every call, so commands that never embed are not stopped by it.
func FromSettings(s config.Settings) Embedder {
	local := localEmbedder(s)
	if s.EmbedURL == "" {
		return local
	}
	remote := NewHTTPEmbedder(s.EmbedURL, s.EmbedModel)
	// The endpoint and the local worker run the same model, so when the
	// endpoint can't be reached the local worker stands in.
	return func(texts []string) ([][]float64, error) {
		vecs, err := remote(texts)
		if err != nil && isUnreachable(err) {
			vecs, localErr := local(texts)
			if localErr != nil {
				return nil, fmt.Errorf("%w (the local worker could not stand in: %v)", err, localErr)
			}
			return vecs, nil
		}
		return vecs, err
	}
}

func localEmbedder(s config.Settings) Embedder {
	var err error
	switch {
	case s.EmbedWorkerDir == "":
		err = errors.New("embed worker directory not set: set WIKI_EMBED_WORKER_DIR or embed_worker_dir in the config file")
	case s.EmbedDevice != "cpu" && s.EmbedDevice != "gpu":
		err = fmt.Errorf("embed_device must be cpu or gpu, got %q", s.EmbedDevice)
	default:
		return NewEmbedder(s.EmbedWorkerDir, s.EmbedDevice)
	}
	return func([]string) ([][]float64, error) { return nil, err }
}
