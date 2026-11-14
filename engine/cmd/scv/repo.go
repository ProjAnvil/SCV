package main

// repo.go implements the `scv repo` verbs over internal/repo: clone,
// link, pull, list, and remove. Every verb is synchronous and prints one
// JSON value with --json (spec section 6); typed repo errors map onto
// the exit-code table in root.go.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/projanvil/scv/engine/internal/repo"
)

func newRepoCmd() *cobra.Command {
	rcmd := &cobra.Command{
		Use:   "repo",
		Short: "Manage repository entries under <scv-home>/repos",
	}
	rcmd.AddCommand(
		newRepoCloneCmd(),
		newRepoLinkCmd(),
		newRepoPullCmd(),
		newRepoListCmd(),
		newRepoRemoveCmd(),
	)
	return rcmd
}

// repoFail maps a repo-package error onto the exit-code table.
func repoFail(err error) error {
	switch {
	case errors.Is(err, repo.ErrNameExists):
		return fail(exitNameExists, "%v", err)
	case errors.Is(err, repo.ErrNotFound):
		return fail(exitNotFound, "%v", err)
	case errors.Is(err, repo.ErrPathNotFound):
		return fail(exitPathNotFound, "%v", err)
	default:
		return fail(exitError, "%v", err)
	}
}

// entryJSON renders a repo entry as a plain JSON object; the internal
// struct carries no tags, so the CLI owns this wire shape.
func entryJSON(e *repo.Entry) map[string]any {
	return map[string]any{
		"name":        e.Name,
		"path":        e.Path,
		"url":         e.URL,
		"branch":      e.Branch,
		"type":        e.Type,
		"head_commit": e.HeadCommit,
	}
}

func newRepoCloneCmd() *cobra.Command {
	var branch string
	cmd := &cobra.Command{
		Use:   "clone <url>",
		Short: "Clone a remote git URL into <scv-home>/repos (branch defaults to main)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := opts.home()
			if err != nil {
				return fail(exitError, "%v", err)
			}
			entry, err := repo.Clone(home, args[0], branch)
			if err != nil {
				return repoFail(err)
			}
			if opts.jsonOut {
				return emitJSON(entryJSON(entry))
			}
			fmt.Printf("cloned %s (branch %s) -> %s\n", entry.Name, entry.Branch, entry.Path)
			return nil
		},
	}
	cmd.Flags().StringVar(&branch, "branch", "", "branch to clone (default main)")
	return cmd
}

func newRepoLinkCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "link <path>",
		Short: "Symlink a local git repository into <scv-home>/repos (read as-is, never written)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := opts.home()
			if err != nil {
				return fail(exitError, "%v", err)
			}
			entry, err := repo.Link(home, args[0], name)
			if err != nil {
				return repoFail(err)
			}
			if opts.jsonOut {
				return emitJSON(entryJSON(entry))
			}
			fmt.Printf("linked %s -> %s\n", entry.Name, entry.Path)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "entry name (default: base name of the resolved path)")
	return cmd
}

func newRepoPullCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pull <name>",
		Short: "Pull upstream updates for a remote clone (linked entries are refused)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := opts.home()
			if err != nil {
				return fail(exitError, "%v", err)
			}
			entry, err := repo.Pull(home, args[0])
			if err != nil {
				return repoFail(err)
			}
			if opts.jsonOut {
				return emitJSON(entryJSON(entry))
			}
			fmt.Printf("pulled %s (HEAD %s)\n", entry.Name, short(entry.HeadCommit, 12))
			return nil
		},
	}
}

func newRepoListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List repository entries with their type (remote, linked, or dir)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := opts.home()
			if err != nil {
				return fail(exitError, "%v", err)
			}
			entries, err := repo.List(home)
			if err != nil {
				return fail(exitError, "%v", err)
			}
			if opts.jsonOut {
				return emitJSON(entryListJSON(entries))
			}
			for _, e := range entries {
				fmt.Printf("%s\t%s\t%s\t%s\n", e.Name, e.Type, e.Branch, short(e.HeadCommit, 12))
			}
			return nil
		},
	}
}

// entryListJSON renders the list as a JSON array, [] rather than null
// when empty.
func entryListJSON(entries []repo.Entry) []map[string]any {
	out := make([]map[string]any, 0, len(entries))
	for i := range entries {
		out = append(out, entryJSON(&entries[i]))
	}
	return out
}

func newRepoRemoveCmd() *cobra.Command {
	var confirm, withAnalysis bool
	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a repos entry (a linked entry never touches its target directory)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if !confirm {
				return fail(exitUsage, "refusing to remove %q without --confirm", name)
			}
			home, err := opts.home()
			if err != nil {
				return fail(exitError, "%v", err)
			}
			if err := repo.Remove(home, name, withAnalysis); err != nil {
				return repoFail(err)
			}
			analysisDir := filepath.Join(home, "analysis", name)
			kept := false
			if !withAnalysis {
				if info, err := os.Stat(analysisDir); err == nil && info.IsDir() {
					kept = true
				}
			}
			if opts.jsonOut {
				return emitJSON(map[string]any{
					"removed":          name,
					"analysis_dir":     analysisDir,
					"analysis_removed": withAnalysis,
				})
			}
			fmt.Printf("removed %s\n", name)
			if kept {
				fmt.Printf("analysis dir kept: %s (use --with-analysis to remove it too)\n", analysisDir)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "actually remove the entry (refused without this flag)")
	cmd.Flags().BoolVar(&withAnalysis, "with-analysis", false, "also remove <scv-home>/analysis/<name>")
	return cmd
}

// short truncates s to at most n leading characters (commit display).
func short(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
