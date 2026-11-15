package install

import (
	"fmt"
	"io"
	"os/exec"
	"strings"

	"wiki_skill/internal/config"
)

// RunOptions is what `wiki install` was asked to do.
type RunOptions struct {
	Repo   string // the checkout; "" means the current directory
	Yes    bool   // do not ask before making changes
	DryRun bool   // show what would change and stop
	In     io.Reader
	Out    io.Writer
}

// Run shows the plan, asks to continue (unless Yes) and applies it.
func Run(o RunOptions) error {
	if o.Repo == "" {
		o.Repo = "."
	}
	repo, err := FindRepo(o.Repo)
	if err != nil {
		return err
	}
	claudeDir, err := DefaultClaudeDir()
	if err != nil {
		return err
	}
	configPath, err := config.Path()
	if err != nil {
		return err
	}
	plan, err := MakePlan(Options{RepoDir: repo, ClaudeDir: claudeDir, ConfigPath: configPath})
	if err != nil {
		return err
	}

	out := o.Out
	for _, note := range plan.Notes {
		fmt.Fprintf(out, "ok: %s\n", note)
	}
	for _, w := range Warnings() {
		fmt.Fprintf(out, "warning: %s\n", w)
	}
	if len(plan.Steps) == 0 {
		fmt.Fprintln(out, "Nothing to do.")
		return nil
	}
	fmt.Fprintln(out, "Will:")
	for _, s := range plan.Steps {
		fmt.Fprintf(out, "  - %s: %s\n", s.Name, s.Detail)
	}
	if o.DryRun {
		return nil
	}
	if !o.Yes {
		fmt.Fprint(out, "Continue? [y/N] ")
		var answer string
		fmt.Fscanln(o.In, &answer)
		if !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
			return fmt.Errorf("aborted, nothing changed")
		}
	}
	for _, s := range plan.Steps {
		if err := s.Apply(); err != nil {
			return fmt.Errorf("%s: %w", s.Name, err)
		}
		fmt.Fprintf(out, "done: %s\n", s.Name)
	}
	fmt.Fprintln(out, "Next: `wiki init` to create a vault (or point vault: at an existing one), then `wiki reindex`. Restart Claude Code to load the skill and hook.")
	return nil
}

// Warnings lists missing prerequisites; none of them stop the install.
func Warnings() []string {
	var w []string
	if _, err := exec.LookPath("uv"); err != nil {
		w = append(w, "uv is not on PATH; semantic search needs it (https://docs.astral.sh/uv/)")
	}
	if _, err := exec.LookPath("rg"); err != nil {
		w = append(w, "rg is not on PATH; `wiki search` leaves out its exact phrase matches without it (https://github.com/BurntSushi/ripgrep)")
	}
	if _, err := exec.LookPath("wiki"); err != nil {
		w = append(w, "wiki is not on PATH; the SessionStart hook runs `wiki`, so run `make install` and add $(go env GOPATH)/bin to PATH")
	}
	return w
}
