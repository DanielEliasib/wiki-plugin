// Package config reads the optional wiki config file.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

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
	// instead of the local worker (embed_device is then ignored).
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
