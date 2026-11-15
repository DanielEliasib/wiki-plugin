// Package config reads the optional wiki config file.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds settings that can also come from flags or environment variables.
type Config struct {
	// Vault is the vault root path.
	Vault string `yaml:"vault"`
	// EmbedWorkerDir is the directory containing embed_worker.py.
	EmbedWorkerDir string `yaml:"embed_worker_dir"`
	// EmbedDevice is "cpu" (default) or "gpu" (NVIDIA CUDA 12).
	EmbedDevice string `yaml:"embed_device"`
	// EmbedURL, when set, is an OpenAI-compatible /v1/embeddings endpoint used
	// instead of the local worker; embed_device and embed_worker_dir then only matter for the local worker that
	// stands in when the endpoint cannot be reached.
	EmbedURL string `yaml:"embed_url"`
	// EmbedModel is the optional "model" field sent to EmbedURL.
	EmbedModel string `yaml:"embed_model"`
}

// Path returns the config file location: $WIKI_CONFIG, else
// <user config dir>/wiki/config.yaml.
func Path() (string, error) {
	if env := os.Getenv("WIKI_CONFIG"); env != "" {
		return env, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "wiki", "config.yaml"), nil
}

// Load reads the config file. A missing file yields an empty Config.
func Load() (Config, error) {
	var cfg Config
	path, err := Path()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Settings is every setting, resolved once: flag over environment variable over the config file over the default.
//
//	setting          flag      environment             config file       default
//	Vault            --vault   WIKI_ROOT               vault             ~/notes/wiki
//	EmbedURL                   WIKI_EMBED_URL          embed_url         (none: the local worker)
//	EmbedModel                                         embed_model       (none)
//	EmbedWorkerDir             WIKI_EMBED_WORKER_DIR   embed_worker_dir  ../embed_worker next to the executable
//	EmbedDevice                WIKI_EMBED_DEVICE       embed_device      cpu
type Settings struct {
	Vault          string
	EmbedURL       string
	EmbedModel     string
	EmbedWorkerDir string // "" when none is set or found
	EmbedDevice    string // not validated here: only the embedder needs it to be cpu or gpu
}

// Resolve reads the config file once and applies the precedence above. vaultFlag is the --vault value.
func Resolve(vaultFlag string) (Settings, error) {
	cfg, err := Load()
	if err != nil {
		return Settings{}, err
	}
	s := Settings{
		Vault:          first(vaultFlag, os.Getenv("WIKI_ROOT"), cfg.Vault),
		EmbedURL:       first(os.Getenv("WIKI_EMBED_URL"), cfg.EmbedURL),
		EmbedModel:     cfg.EmbedModel,
		EmbedWorkerDir: first(os.Getenv("WIKI_EMBED_WORKER_DIR"), cfg.EmbedWorkerDir),
		EmbedDevice:    first(os.Getenv("WIKI_EMBED_DEVICE"), cfg.EmbedDevice, "cpu"),
	}
	if s.Vault == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Settings{}, err
		}
		s.Vault = filepath.Join(home, "notes", "wiki")
	} else if s.Vault, err = expandHome(s.Vault); err != nil {
		return Settings{}, err
	}
	if s.EmbedWorkerDir == "" {
		s.EmbedWorkerDir = workerDirNextToExecutable()
	}
	return s, nil
}

func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func expandHome(p string) (string, error) {
	// only "~" and "~/...": "~bob" is another user's home, which is not looked up
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~")), nil
}

// workerDirNextToExecutable finds ../embed_worker next to the executable (a `make build` checkout).
func workerDirNextToExecutable() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return ""
	}
	dir := filepath.Join(filepath.Dir(exe), "..", "embed_worker")
	if _, err := os.Stat(filepath.Join(dir, "embed_worker.py")); err != nil {
		return ""
	}
	return filepath.Clean(dir)
}
