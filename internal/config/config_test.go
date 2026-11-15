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

func writeConfig(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WIKI_CONFIG", path)
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"WIKI_ROOT", "WIKI_EMBED_URL", "WIKI_EMBED_WORKER_DIR", "WIKI_EMBED_DEVICE"} {
		t.Setenv(k, "")
	}
	t.Setenv("WIKI_CONFIG", filepath.Join(t.TempDir(), "none.yaml"))
}

// The precedence of every setting, flag over environment over config file over default.
func TestResolvePrecedence(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		config string
		env    map[string]string
		flag   string
		check  func(Settings) string // returns "" when right
	}{
		{"vault defaults to ~/notes/wiki", "", nil, "",
			func(s Settings) string { return want(s.Vault, filepath.Join(home, "notes", "wiki")) }},
		{"vault from the config file", "vault: /cfg/path\n", nil, "",
			func(s Settings) string { return want(s.Vault, "/cfg/path") }},
		{"vault: ~ expands", "vault: ~/notes/x\n", nil, "",
			func(s Settings) string { return want(s.Vault, filepath.Join(home, "notes", "x")) }},
		{"vault: env over config", "vault: /cfg/path\n", map[string]string{"WIKI_ROOT": "/env/path"}, "",
			func(s Settings) string { return want(s.Vault, "/env/path") }},
		{"vault: flag over env", "", map[string]string{"WIKI_ROOT": "/env/path"}, "/flag/path",
			func(s Settings) string { return want(s.Vault, "/flag/path") }},
		{"embed url from the config file, with its model", "embed_url: http://cfg/v1/embeddings\nembed_model: m\n", nil, "",
			func(s Settings) string {
				return want(s.EmbedURL+" "+s.EmbedModel, "http://cfg/v1/embeddings m")
			}},
		{"embed url: env over config", "embed_url: http://cfg/v1/embeddings\n", map[string]string{"WIKI_EMBED_URL": "http://env/v1/embeddings"}, "",
			func(s Settings) string { return want(s.EmbedURL, "http://env/v1/embeddings") }},
		{"no embed url means the local worker", "", nil, "",
			func(s Settings) string { return want(s.EmbedURL, "") }},
		{"worker dir from the config file", "embed_worker_dir: /cfg/worker\n", nil, "",
			func(s Settings) string { return want(s.EmbedWorkerDir, "/cfg/worker") }},
		{"worker dir: env over config", "embed_worker_dir: /cfg/worker\n", map[string]string{"WIKI_EMBED_WORKER_DIR": "/env/worker"}, "",
			func(s Settings) string { return want(s.EmbedWorkerDir, "/env/worker") }},
		{"device defaults to cpu", "", nil, "",
			func(s Settings) string { return want(s.EmbedDevice, "cpu") }},
		{"device from the config file", "embed_device: gpu\n", nil, "",
			func(s Settings) string { return want(s.EmbedDevice, "gpu") }},
		{"device: env over config", "embed_device: gpu\n", map[string]string{"WIKI_EMBED_DEVICE": "cpu"}, "",
			func(s Settings) string { return want(s.EmbedDevice, "cpu") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clearEnv(t)
			if c.config != "" {
				writeConfig(t, c.config)
			}
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			s, err := Resolve(c.flag)
			if err != nil {
				t.Fatal(err)
			}
			if msg := c.check(s); msg != "" {
				t.Error(msg)
			}
		})
	}
}

func want(got, want string) string {
	if got != want {
		return "got " + got + ", want " + want
	}
	return ""
}

func TestResolveReportsABrokenConfigFile(t *testing.T) {
	clearEnv(t)
	writeConfig(t, "vault: [unclosed\n")
	if _, err := Resolve(""); err == nil {
		t.Error("want an error naming the broken config file")
	}
}

func TestResolveKeepsAnUnknownDeviceForTheEmbedderToReject(t *testing.T) {
	clearEnv(t)
	t.Setenv("WIKI_EMBED_DEVICE", "tpu")
	s, err := Resolve("")
	if err != nil || s.EmbedDevice != "tpu" {
		t.Errorf("got %q, %v: a bad device must not stop commands that never embed", s.EmbedDevice, err)
	}
}
