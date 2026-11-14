package main

// env.go implements `scv env`: the environment snapshot plus the shared
// diagnostics schema (spec section 9). It always exits 0 — hosts decide
// what to do with the report — and its diagnostics are exactly what the
// run-time checking gate produces, minus the run-mode classification.

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/projanvil/scv/engine/internal/config"
	"github.com/projanvil/scv/engine/internal/env"
)

func newEnvCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "env",
		Short: "Print the environment snapshot and dependency diagnostics (always exits 0)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := opts.home()
			if err != nil {
				return fail(exitError, "%v", err)
			}
			// Discovery through the layout-ensuring entry point (spec
			// section 11): `scv env` on a fresh install selects the
			// current platform's prebuilt pair into the flat dist layout,
			// so the report (and every later run) sees the prebuilts. A
			// discovery failure still fails the verb; a layout failure
			// only warns — the snapshot stays reportable and the verb
			// keeps its exit-0 contract.
			snapshot, err := env.DiscoverEnsuringLayout(env.Options{ScvHome: home})
			if snapshot == nil {
				return fail(exitError, "%v", err)
			}
			if err != nil {
				diagf("warning: %v", err)
			}
			// A broken config file degrades to zero config here (the gate
			// treats nil as defaults); `scv env` stays exit 0 per spec.
			cfgPath, err := opts.configPath()
			if err != nil {
				return fail(exitError, "%v", err)
			}
			cfg, err := config.Load(cfgPath, home)
			if err != nil {
				diagf("warning: %v (continuing with default config)", err)
				cfg = nil
			}
			gate := snapshot.Gate(env.Mode{}, cfg)
			if opts.jsonOut {
				return emitJSON(envJSON(snapshot, gate))
			}
			printEnvText(snapshot, gate)
			return nil
		},
	}
}

// envJSON builds the machine-readable snapshot: roots, tools, prebuilt
// dist binaries, and the diagnostics list in the uniform wire schema
// shared with the checking gate.
func envJSON(snapshot *env.Env, gate env.GateResult) map[string]any {
	items := make([]map[string]any, 0, len(gate.Blocking)+len(gate.Degrading))
	for _, it := range append(gate.Blocking, gate.Degrading...) {
		items = append(items, map[string]any{
			"item":       it.Item,
			"status":     it.Status,
			"severity":   it.Severity,
			"capability": it.Capability,
			"hint":       it.Hint,
		})
	}
	return map[string]any{
		"scv_home":        snapshot.ScvHome,
		"perception_root": snapshot.PerceptionRoot,
		"dist_root":       snapshot.DistRoot,
		"tools": map[string]any{
			"uv":      toolJSON(snapshot.Uv),
			"python3": toolJSON(snapshot.Python3),
			"git":     toolJSON(snapshot.Git),
			"go":      toolJSON(snapshot.Go),
			"java":    toolJSON(snapshot.Java),
		},
		"prebuilt": map[string]any{
			"loader":   snapshot.PrebuiltLoader,
			"graphmcp": snapshot.PrebuiltGraphMCP,
		},
		"diagnostics": items,
	}
}

// toolJSON renders a discovered tool; nil (not found) stays null.
func toolJSON(t *env.Tool) map[string]any {
	if t == nil {
		return nil
	}
	return map[string]any{"path": t.Path, "version": t.Version, "ok": t.OK}
}

// printEnvText renders the human-readable report.
func printEnvText(snapshot *env.Env, gate env.GateResult) {
	fmt.Printf("scv home:        %s\n", snapshot.ScvHome)
	fmt.Printf("perception root: %s\n", snapshot.PerceptionRoot)
	fmt.Printf("dist root:       %s\n", snapshot.DistRoot)
	for _, entry := range []struct {
		name string
		tool *env.Tool
	}{
		{"python3", snapshot.Python3},
		{"git", snapshot.Git},
		{"uv", snapshot.Uv},
		{"go", snapshot.Go},
		{"java", snapshot.Java},
	} {
		fmt.Printf("%s: %s\n", entry.name, toolLine(entry.tool))
	}
	fmt.Printf("prebuilt loader:    %t\n", snapshot.PrebuiltLoader)
	fmt.Printf("prebuilt graphmcp:  %t\n", snapshot.PrebuiltGraphMCP)
	if items := append(gate.Blocking, gate.Degrading...); len(items) > 0 {
		fmt.Println("diagnostics:")
		for _, it := range items {
			fmt.Printf("  [%s] %s: %s\n", it.Severity, it.Item, it.Hint)
		}
	} else {
		fmt.Println("diagnostics: none — environment is complete")
	}
}

// toolLine renders one tool for the text report.
func toolLine(t *env.Tool) string {
	switch {
	case t == nil:
		return "missing"
	case !t.OK:
		return fmt.Sprintf("unusable (%s)", t.Version)
	case t.Version != "":
		return fmt.Sprintf("%s (%s)", t.Version, t.Path)
	default:
		return fmt.Sprintf("ok (%s)", t.Path)
	}
}
