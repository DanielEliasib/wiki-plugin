// Package install wires a checkout of this repo into Claude Code: the skill
// symlink, the wiki config file and the SessionStart hook.
package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// HookCommand is the SessionStart hook command the installer registers.
const HookCommand = "wiki map --hook"

// hookMatcher keeps resume and fork silent: they already carry the map in
// their transcript.
const hookMatcher = "startup|clear|compact"

type Options struct {
	RepoDir    string // checkout containing SKILL.md and embed_worker/
	ClaudeDir  string // Claude Code config dir, usually ~/.claude
	ConfigPath string // wiki config file
}

// Step is one change the installer would make.
type Step struct {
	Name   string
	Detail string
	apply  func() error
}

// Apply performs the step.
func (s Step) Apply() error { return s.apply() }

// Plan is the list of changes still to make, plus notes about things that are
// already in place or need the user's attention.
type Plan struct {
	Steps []Step
	Notes []string
}

// DefaultClaudeDir returns $CLAUDE_CONFIG_DIR, else ~/.claude.
func DefaultClaudeDir() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// FindRepo returns dir if it looks like a checkout of this repo.
func FindRepo(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for _, f := range []string{"SKILL.md", filepath.Join("embed_worker", "embed_worker.py")} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			return "", fmt.Errorf("%s is not a wiki skill checkout (missing %s): run from the checkout or pass --repo", dir, f)
		}
	}
	return dir, nil
}

// MakePlan works out what is left to do. It changes nothing.
func MakePlan(o Options) (Plan, error) {
	var p Plan
	if err := planSkillLink(&p, o); err != nil {
		return p, err
	}
	planConfig(&p, o)
	if err := planHook(&p, o); err != nil {
		return p, err
	}
	return p, nil
}

func planSkillLink(p *Plan, o Options) error {
	link := filepath.Join(o.ClaudeDir, "skills", "wiki")
	target, err := os.Readlink(link)
	switch {
	case err == nil && target == o.RepoDir:
		p.Notes = append(p.Notes, "skill already linked at "+link)
	case err == nil:
		return fmt.Errorf("%s already links to %s; remove it first to install this checkout", link, target)
	case errors.Is(err, fs.ErrNotExist):
		p.Steps = append(p.Steps, Step{
			Name:   "Link the skill",
			Detail: fmt.Sprintf("%s -> %s", link, o.RepoDir),
			apply: func() error {
				if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
					return err
				}
				return os.Symlink(o.RepoDir, link)
			},
		})
	default:
		return fmt.Errorf("%s exists and is not a symlink; remove it first to install this checkout", link)
	}
	return nil
}

func planConfig(p *Plan, o Options) {
	if _, err := os.Stat(o.ConfigPath); err == nil {
		p.Notes = append(p.Notes, fmt.Sprintf("%s exists and is left alone; make sure embed_worker_dir is %s", o.ConfigPath, filepath.Join(o.RepoDir, "embed_worker")))
		return
	}
	content := fmt.Sprintf("# vault: ~/notes/wiki\nembed_worker_dir: %s\n# embed_device: cpu   # or gpu (NVIDIA, CUDA 12)\n", filepath.Join(o.RepoDir, "embed_worker"))
	p.Steps = append(p.Steps, Step{
		Name:   "Write the wiki config",
		Detail: o.ConfigPath,
		apply: func() error {
			if err := os.MkdirAll(filepath.Dir(o.ConfigPath), 0o755); err != nil {
				return err
			}
			return os.WriteFile(o.ConfigPath, []byte(content), 0o644)
		},
	})
}

func planHook(p *Plan, o Options) error {
	path := filepath.Join(o.ClaudeDir, "settings.json")
	settings, existed, err := readSettings(path)
	if err != nil {
		return err
	}
	if hasHook(settings) {
		p.Notes = append(p.Notes, "SessionStart hook already registered in "+path)
		return nil
	}
	detail := "add a SessionStart hook running `" + HookCommand + "` to " + path
	if existed {
		detail += " (keys are re-sorted; the original is saved as settings.json.bak)"
	}
	p.Steps = append(p.Steps, Step{
		Name:   "Register the SessionStart hook",
		Detail: detail,
		apply:  func() error { return writeSettings(path, settings, existed) },
	})
	return nil
}

func readSettings(path string) (settings map[string]any, existed bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]any{}, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&settings); err != nil {
		return nil, true, fmt.Errorf("%s is not valid JSON, not touching it: %w", path, err)
	}
	if settings == nil {
		settings = map[string]any{}
	}
	return settings, true, nil
}

// hasHook reports whether a SessionStart hook already runs HookCommand.
func hasHook(settings map[string]any) bool {
	hooks, _ := settings["hooks"].(map[string]any)
	entries, _ := hooks["SessionStart"].([]any)
	for _, e := range entries {
		entry, _ := e.(map[string]any)
		inner, _ := entry["hooks"].([]any)
		for _, h := range inner {
			hook, _ := h.(map[string]any)
			if hook["command"] == HookCommand {
				return true
			}
		}
	}
	return false
}

func writeSettings(path string, settings map[string]any, existed bool) error {
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		if settings["hooks"] != nil {
			return fmt.Errorf("%s: \"hooks\" is not an object, not touching it", path)
		}
		hooks = map[string]any{}
		settings["hooks"] = hooks
	}
	entries, ok := hooks["SessionStart"].([]any)
	if !ok && hooks["SessionStart"] != nil {
		return fmt.Errorf("%s: hooks.SessionStart is not a list, not touching it", path)
	}
	hooks["SessionStart"] = append(entries, map[string]any{
		"matcher": hookMatcher,
		"hooks":   []any{map[string]any{"type": "command", "command": HookCommand}},
	})

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(settings); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if existed {
		orig, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path+".bak", orig, 0o600); err != nil {
			return err
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
