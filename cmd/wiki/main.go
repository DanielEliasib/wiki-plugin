package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"wiki_skill/internal/config"
	"wiki_skill/internal/embedworker"
	"wiki_skill/internal/install"
	"wiki_skill/internal/render"
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
		newMapCmd(&vaultFlag, &jsonFlag),
		newInitCmd(&vaultFlag, &jsonFlag),
		newInstallCmd(&jsonFlag),
	)
	return root
}

// resolveSettings resolves every setting once; a failure is printed the way the command prints errors.
func resolveSettings(cmd *cobra.Command, vaultFlag string, jsonFlag bool) (config.Settings, error) {
	settings, err := config.Resolve(vaultFlag)
	if err != nil {
		render.Error(errWriter(cmd, jsonFlag), jsonFlag, err)
	}
	return settings, err
}

// rejectJSON is for the commands that have no JSON output of their own: --json is a root flag, so it is accepted
// everywhere, and a command that ignored it would look as if it had obeyed.
func rejectJSON(jsonFlag bool, command string) error {
	if jsonFlag {
		return fmt.Errorf("wiki %s has no --json output", command)
	}
	return nil
}

func errWriter(cmd *cobra.Command, jsonFlag bool) io.Writer {
	if jsonFlag {
		return cmd.OutOrStdout()
	}
	return cmd.ErrOrStderr()
}

func newSearchCmd(vaultFlag *string, jsonFlag *bool) *cobra.Command {
	var topK int
	var opts wikicmd.SearchOptions
	cmd := &cobra.Command{
		Use:           "search <query>",
		Short:         "Search notes by keywords and meaning, one result per note",
		Args:          cobra.ExactArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			settings, err := resolveSettings(cmd, *vaultFlag, *jsonFlag)
			if err != nil {
				return err
			}
			root := settings.Vault
			report, err := wikicmd.Search(root, args[0], embedworker.FromSettings(settings), topK, opts)
			if err != nil {
				render.Error(errWriter(cmd, *jsonFlag), *jsonFlag, err)
				return err
			}
			return render.Search(cmd.OutOrStdout(), *jsonFlag, report)
		},
	}
	cmd.Flags().IntVar(&topK, "top-k", 10, "max results")
	cmd.Flags().BoolVar(&opts.WithRecords, "logs", false, "also search logs (logs/ folders) and sources files (_sources.md)")
	cmd.Flags().BoolVar(&opts.WithArchive, "archive", false, "also search archived notes (archive/)")
	return cmd
}

func newReindexCmd(vaultFlag *string, jsonFlag *bool) *cobra.Command {
	var full bool
	cmd := &cobra.Command{
		Use:           "reindex",
		Short:         "Update the semantic search index: embed changed notes, drop deleted ones (--full rebuilds it)",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			settings, err := resolveSettings(cmd, *vaultFlag, *jsonFlag)
			if err != nil {
				return err
			}
			root := settings.Vault
			report, err := wikicmd.Reindex(root, embedworker.FromSettings(settings), full)
			if err != nil {
				render.Error(errWriter(cmd, *jsonFlag), *jsonFlag, err)
				return err
			}
			return render.Reindex(cmd.OutOrStdout(), *jsonFlag, report)
		},
	}
	cmd.Flags().BoolVar(&full, "full", false, "rebuild the whole index from scratch (after changing the embedding model)")
	return cmd
}

func newPruneCmd(vaultFlag *string, jsonFlag *bool) *cobra.Command {
	var similarity, staleDays float64
	cmd := &cobra.Command{
		Use:           "prune",
		Short:         "Report merge candidates and stale folders",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			settings, err := resolveSettings(cmd, *vaultFlag, *jsonFlag)
			if err != nil {
				return err
			}
			report, err := wikicmd.Prune(settings.Vault, similarity, staleDays)
			if err != nil {
				render.Error(errWriter(cmd, *jsonFlag), *jsonFlag, err)
				return err
			}
			return render.Prune(cmd.OutOrStdout(), *jsonFlag, report)
		},
	}
	cmd.Flags().Float64Var(&similarity, "similarity", 0.92, "report note pairs at least this similar (0 to 1)")
	cmd.Flags().Float64Var(&staleDays, "stale-days", 180, "report project and area folders untouched for this many days")
	return cmd
}

func newWatchCmd(vaultFlag *string, jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:           "watch",
		Short:         "Watch the vault and incrementally update the semantic index",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			settings, err := resolveSettings(cmd, *vaultFlag, *jsonFlag)
			if err != nil {
				return err
			}
			root := settings.Vault

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

			if err := wikicmd.RunWatch(root, embedworker.FromSettings(settings), 1500*time.Millisecond, cmd.ErrOrStderr(), onBatch); err != nil {
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
			settings, err := resolveSettings(cmd, *vaultFlag, *jsonFlag)
			if err != nil {
				return err
			}
			root := settings.Vault
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
			settings, err := resolveSettings(cmd, *vaultFlag, *jsonFlag)
			if err != nil {
				return err
			}
			root := settings.Vault
			report, err := wikicmd.Init(root)
			if err != nil {
				render.Error(errWriter(cmd, *jsonFlag), *jsonFlag, err)
				return err
			}
			return render.Init(cmd.OutOrStdout(), *jsonFlag, report)
		},
	}
}

func newInstallCmd(jsonFlag *bool) *cobra.Command {
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
			err := rejectJSON(*jsonFlag, "install")
			if err == nil {
				err = install.Run(install.RunOptions{Repo: repoFlag, Yes: yes, DryRun: dryRun, In: cmd.InOrStdin(), Out: cmd.OutOrStdout()})
			}
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

// hookInput is the part of the SessionStart hook's stdin JSON that wiki map uses.
type hookInput struct {
	Cwd string `json:"cwd"`
}

func newMapCmd(vaultFlag *string, jsonFlag *bool) *cobra.Command {
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
			if err := rejectJSON(*jsonFlag, "map"); err != nil {
				render.Error(cmd.ErrOrStderr(), false, err)
				return err
			}
			if hook {
				return runMapHook(cmd, *vaultFlag)
			}
			settings, err := config.Resolve(*vaultFlag)
			if err != nil {
				render.Error(cmd.ErrOrStderr(), false, err)
				return err
			}
			root := settings.Vault
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
	settings, err := config.Resolve(vaultFlag)
	if err != nil {
		return nil
	}
	out, err := wikicmd.Map(settings.Vault, in.Cwd, time.Now())
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
