package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"wiki_skill/internal/embedworker"
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
