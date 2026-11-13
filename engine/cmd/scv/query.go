package main

// query.go implements the `scv query` verbs over internal/query: repo
// listing, Cypher, node lookup, blast-radius impact, and refresh. The
// store aggregates every registered analysis root, so queries see all
// runs including --output-dir ones.

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/projanvil/scv/engine/internal/query"
)

func newQueryCmd() *cobra.Command {
	q := &cobra.Command{
		Use:   "query",
		Short: "Query structure graphs across all registered analysis roots",
	}
	q.AddCommand(
		newQueryReposCmd(),
		newQueryCypherCmd(),
		newQueryNodeCmd(),
		newQueryImpactCmd(),
		newQueryRefreshCmd(),
	)
	return q
}

// openStore opens the query store over the scv home's analysis-dirs
// registry and default analysis root.
func openStore() (*query.Store, error) {
	home, err := opts.home()
	if err != nil {
		return nil, err
	}
	return query.Open(filepath.Join(home, "analysis-dirs.json"), filepath.Join(home, "analysis"))
}

func newQueryReposCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "repos",
		Short: "List queryable repos with their snapshot paths and node/edge counts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return fail(exitError, "open store: %v", err)
			}
			repos, err := store.Repos()
			if err != nil {
				return fail(exitError, "%v", err)
			}
			if opts.jsonOut {
				return emitJSON(repos)
			}
			for _, r := range repos {
				fmt.Printf("%v\t%v nodes\t%v edges\t%v\n", r["repo"], r["nodes"], r["edges"], r["file"])
			}
			return nil
		},
	}
}

func newQueryCypherCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "cypher <repo> <query>",
		Short: "Run a Cypher-subset statement against a repo's structure graph",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return fail(exitError, "open store: %v", err)
			}
			result, err := store.Cypher(args[0], args[1], limit)
			if err != nil {
				return fail(exitError, "%v", err)
			}
			return printRows(result)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "row limit (0 means the default cap)")
	return cmd
}

func newQueryNodeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "node <repo> <key>",
		Short: "Look up one node by key",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return fail(exitError, "open store: %v", err)
			}
			node, err := store.Node(args[0], args[1])
			if err != nil {
				return fail(exitError, "%v", err)
			}
			if opts.jsonOut {
				return emitJSON(node)
			}
			return printRows(node)
		},
	}
}

func newQueryImpactCmd() *cobra.Command {
	var dir string
	var labels string
	var limit int
	cmd := &cobra.Command{
		Use:   "impact <repo> <key>",
		Short: "Blast-radius analysis: every node transitively reachable from a key",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return fail(exitError, "open store: %v", err)
			}
			var labelList []string
			if labels != "" {
				for _, l := range strings.Split(labels, ",") {
					if l = strings.TrimSpace(l); l != "" {
						labelList = append(labelList, l)
					}
				}
			}
			result, err := store.Impact(args[0], args[1], dir, labelList, limit)
			if err != nil {
				return fail(exitError, "%v", err)
			}
			return printRows(result)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "traversal direction: out (default), in, or both")
	cmd.Flags().StringVar(&labels, "labels", "", "comma-separated edge labels restricting the traversal")
	cmd.Flags().IntVar(&limit, "limit", 0, "node limit (0 means the default cap)")
	return cmd
}

func newQueryRefreshCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refresh",
		Short: "Rescan all registered analysis roots for new or removed snapshots",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return fail(exitError, "open store: %v", err)
			}
			store.Refresh()
			repos, err := store.Repos()
			if err != nil {
				return fail(exitError, "%v", err)
			}
			if opts.jsonOut {
				return emitJSON(map[string]any{"refreshed": true, "repos": len(repos)})
			}
			fmt.Printf("refreshed: %d repos\n", len(repos))
			return nil
		},
	}
}

// printRows renders a query result: one compact JSON value with --json,
// or an indented block for human reading (query outputs are structured
// data; indented JSON is their plain-text form).
func printRows(result any) error {
	if opts.jsonOut {
		return emitJSON(result)
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fail(exitError, "render result: %v", err)
	}
	fmt.Println(string(data))
	return nil
}
