package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"wiki_skill/internal/config"
	"wiki_skill/internal/embedworker"
	"wiki_skill/internal/install"
	"wiki_skill/internal/render"
	"wiki_skill/internal/vault"
	"wiki_skill/internal/wikicmd"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var vaultFlag string
	var jsonFlag bool

	root := &cobra.Command{
		Use:   "wiki",
		Short: "Local PARA-vault knowledge base CLI",
	}
	root.PersistentFlags().StringVar(&vaultFlag, "vault", "", "vault root path (defaults to $WIKI_ROOT, then vault in the config file, then ~/notes/wiki)")
	root.PersistentFlags().BoolVar(&jsonFlag, "json", false, "output structured JSON instead of formatted text")

	root.AddCommand(
		newSearchCmd(&vaultFlag, &jsonFlag),
		newReindexCmd(&vaultFlag, &jsonFlag),
		newPruneCmd(&vaultFlag, &jsonFlag),
		newWatchCmd(&vaultFlag, &jsonFlag),
		newIndexCmd(&vaultFlag, &jsonFlag),
		newMapCmd(&vaultFlag),
		newInitCmd(&vaultFlag, &jsonFlag),
		newInstallCmd(),
	)
	return root
}

func errWriter(cmd *cobra.Command, jsonFlag bool) io.Writer {
	if jsonFlag {
		return cmd.OutOrStdout()
	}
	return cmd.ErrOrStderr()
}

func newSearchCmd(vaultFlag *string, jsonFlag *bool) *cobra.Command {
	var topK int
	cmd := &cobra.Command{
		Use:           "search <query>",
		Short:         "Search notes via grep and semantic similarity",
		Args:          cobra.ExactArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := vault.ResolveVaultRoot(*vaultFlag)
			if err != nil {
				render.Error(errWriter(cmd, *jsonFlag), *jsonFlag, err)
				return err
			}
			report, err := wikicmd.Search(root, args[0], embedworker.DefaultEmbedder(), topK)
			if err != nil {
				render.Error(errWriter(cmd, *jsonFlag), *jsonFlag, err)
				return err
			}
			return render.Search(cmd.OutOrStdout(), *jsonFlag, report)
		},
	}
	cmd.Flags().IntVar(&topK, "top-k", 10, "max semantic results")
	return cmd
}

func newReindexCmd(vaultFlag *string, jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:           "reindex",
		Short:         "Rebuild the semantic search index from scratch",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := vault.ResolveVaultRoot(*vaultFlag)
			if err != nil {
				render.Error(errWriter(cmd, *jsonFlag), *jsonFlag, err)
				return err
			}
			report, err := wikicmd.Reindex(root, embedworker.DefaultEmbedder())
			if err != nil {
				render.Error(errWriter(cmd, *jsonFlag), *jsonFlag, err)
				return err
			}
			return render.Reindex(cmd.OutOrStdout(), *jsonFlag, report)
		},
	}
}

func newPruneCmd(vaultFlag *string, jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:           "prune",
		Short:         "Report merge candidates and stale folders",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := vault.ResolveVaultRoot(*vaultFlag)
			if err != nil {
				render.Error(errWriter(cmd, *jsonFlag), *jsonFlag, err)
				return err
			}
			report, err := wikicmd.Prune(root, 0.92, 180)
			if err != nil {
				render.Error(errWriter(cmd, *jsonFlag), *jsonFlag, err)
				return err
			}
			return render.Prune(cmd.OutOrStdout(), *jsonFlag, report)
		},
	}
}

func newWatchCmd(vaultFlag *string, jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:           "watch",
		Short:         "Watch the vault and incrementally update the semantic index",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := vault.ResolveVaultRoot(*vaultFlag)
			if err != nil {
				render.Error(errWriter(cmd, *jsonFlag), *jsonFlag, err)
				return err
			}

			out := cmd.OutOrStdout()
			if *jsonFlag {
				emitJSONLine(out, map[string]interface{}{"event": "watching", "root": root})
			} else {
				fmt.Fprintf(out, "watching %s (ctrl+c to stop)\n", root)
			}

			onBatch := func(upserted, removed []string) {
				if *jsonFlag {
					emitJSONLine(out, map[string]interface{}{
						"event":    "batch",
						"upserted": upserted,
						"removed":  removed,
					})
					return
				}
				fmt.Fprintf(out, "updated %d notes, removed %d\n", len(upserted), len(removed))
			}

			if err := wikicmd.RunWatch(root, embedworker.DefaultEmbedder(), 1500*time.Millisecond, cmd.ErrOrStderr(), onBatch); err != nil {
				render.Error(errWriter(cmd, *jsonFlag), *jsonFlag, err)
				return err
			}
			return nil
		},
	}
}

func newIndexCmd(vaultFlag *string, jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:           "index [name]",
		Short:         "Browse the vault: all folders, or one folder's notes",
		Args:          cobra.MaximumNArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := vault.ResolveVaultRoot(*vaultFlag)
			if err != nil {
				render.Error(errWriter(cmd, *jsonFlag), *jsonFlag, err)
				return err
			}
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			report, err := wikicmd.Index(root, name)
			if err != nil {
				render.Error(errWriter(cmd, *jsonFlag), *jsonFlag, err)
				return err
			}
			return render.Index(cmd.OutOrStdout(), *jsonFlag, report)
		},
	}
}

func newInitCmd(vaultFlag *string, jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:           "init",
		Short:         "Create the vault's folders and conventions file (adds only what is missing)",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := vault.ResolveVaultRoot(*vaultFlag)
			if err != nil {
				render.Error(errWriter(cmd, *jsonFlag), *jsonFlag, err)
				return err
			}
			report, err := wikicmd.Init(root)
			if err != nil {
				render.Error(errWriter(cmd, *jsonFlag), *jsonFlag, err)
				return err
			}
			return render.Init(cmd.OutOrStdout(), *jsonFlag, report)
		},
	}
}

func newInstallCmd() *cobra.Command {
	var repoFlag string
	var yes, dryRun bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Set up Claude Code: link the skill, write the wiki config, register the SessionStart hook",
		Long: "Run from a checkout of the wiki skill repo (or pass --repo). Shows what it will change,\n" +
			"asks to continue, then: links the skill into the Claude config dir, writes the wiki config\n" +
			"file if there is none, and adds the `wiki map --hook` SessionStart hook to settings.json\n" +
			"(the original is kept as settings.json.bak). Anything already in place is skipped.",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			err := runInstall(cmd, repoFlag, yes, dryRun)
			if err != nil {
				render.Error(cmd.ErrOrStderr(), false, err)
			}
			return err
		},
	}
	cmd.Flags().StringVar(&repoFlag, "repo", "", "wiki skill checkout (default: current directory)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask before making changes")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change and stop")
	return cmd
}

func runInstall(cmd *cobra.Command, repoFlag string, yes, dryRun bool) error {
	if repoFlag == "" {
		repoFlag = "."
	}
	repo, err := install.FindRepo(repoFlag)
	if err != nil {
		return err
	}
	claudeDir, err := install.DefaultClaudeDir()
	if err != nil {
		return err
	}
	configPath, err := config.Path()
	if err != nil {
		return err
	}
	plan, err := install.MakePlan(install.Options{RepoDir: repo, ClaudeDir: claudeDir, ConfigPath: configPath})
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	for _, note := range plan.Notes {
		fmt.Fprintf(out, "ok: %s\n", note)
	}
	for _, w := range installWarnings() {
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
	if dryRun {
		return nil
	}
	if !yes {
		fmt.Fprint(out, "Continue? [y/N] ")
		var answer string
		fmt.Fscanln(cmd.InOrStdin(), &answer)
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

// installWarnings lists missing prerequisites; none of them stop the install.
func installWarnings() []string {
	var w []string
	if _, err := exec.LookPath("uv"); err != nil {
		w = append(w, "uv is not on PATH; semantic search needs it (https://docs.astral.sh/uv/)")
	}
	if _, err := exec.LookPath("wiki"); err != nil {
		w = append(w, "wiki is not on PATH; the SessionStart hook runs `wiki`, so run `make install` and add $(go env GOPATH)/bin to PATH")
	}
	return w
}

// hookInput is the part of the SessionStart hook's stdin JSON that wiki map uses.
type hookInput struct {
	Cwd string `json:"cwd"`
}

func newMapCmd(vaultFlag *string) *cobra.Command {
	var hook bool
	var cwdFlag string
	cmd := &cobra.Command{
		Use:   "map",
		Short: "Print the session map (bounded, for the SessionStart hook)",
		Long: "Print the session map: every folder with a gist, plus a relevance-ranked subset of notes.\n" +
			"With --hook, read the SessionStart hook JSON from stdin and print hook JSON; on any error\n" +
			"print nothing and exit 0. The map is read live and nothing is written or embedded.",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if hook {
				return runMapHook(cmd, *vaultFlag)
			}
			root, err := vault.ResolveVaultRoot(*vaultFlag)
			if err != nil {
				render.Error(cmd.ErrOrStderr(), false, err)
				return err
			}
			cwd := cwdFlag
			if cwd == "" {
				if cwd, err = os.Getwd(); err != nil {
					render.Error(cmd.ErrOrStderr(), false, err)
					return err
				}
			}
			out, err := wikicmd.Map(root, cwd, time.Now())
			if err != nil {
				render.Error(cmd.ErrOrStderr(), false, err)
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), out)
			return nil
		},
	}
	cmd.Flags().BoolVar(&hook, "hook", false, "read hook JSON from stdin and emit hook JSON (silent exit 0 on error)")
	cmd.Flags().StringVar(&cwdFlag, "cwd", "", "working directory to match against folder paths (default: current directory)")
	return cmd
}

// runMapHook never fails: a broken vault or input must not break a session.
func runMapHook(cmd *cobra.Command, vaultFlag string) error {
	var in hookInput
	if err := json.NewDecoder(cmd.InOrStdin()).Decode(&in); err != nil {
		return nil
	}
	root, err := vault.ResolveVaultRoot(vaultFlag)
	if err != nil {
		return nil
	}
	out, err := wikicmd.Map(root, in.Cwd, time.Now())
	if err != nil {
		return nil
	}
	emitJSONLine(cmd.OutOrStdout(), map[string]interface{}{
		"hookSpecificOutput": map[string]interface{}{
			"hookEventName":     "SessionStart",
			"additionalContext": out,
		},
	})
	return nil
}

func emitJSONLine(w io.Writer, v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintln(w, string(data))
}
