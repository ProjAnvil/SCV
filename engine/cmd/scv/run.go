package main

// run.go implements `scv run`: the perceive version handshake, the
// SIGTERM relay that external cancellation depends on, and the pipeline
// invocation whose sink prints the NDJSON event stream on stdout.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/projanvil/scv/engine/internal/config"
	"github.com/projanvil/scv/engine/internal/docgen"
	"github.com/projanvil/scv/engine/internal/env"
	"github.com/projanvil/scv/engine/internal/job"
	"github.com/projanvil/scv/engine/internal/repo"
	"github.com/projanvil/scv/engine/internal/run"
)

// handshakeTimeout bounds the `perceive --version` exec: a hung perceive
// must not hang `scv run` before the pipeline even starts.
const handshakeTimeout = 10 * time.Second

// testPerceiveEnv is the perceive path override consumed by both the
// run pipeline (run.perceivePath, unexported) and the handshake here, so
// a test stub replaces the script everywhere at once.
const testPerceiveEnv = "SCV_ENGINE_TEST_PERCEIVE"

func newRunCmd() *cobra.Command {
	var project, outputDir, lang, sourceMode string
	var skipUnchanged, force, allowDegraded bool

	cmd := &cobra.Command{
		Use:   "run <source>",
		Short: "Run the analysis pipeline on a source (git URL, git worktree, or local directory)",
		Long: "Run the analysis pipeline on <source>: a git URL (cloned), a local git\n" +
			"worktree (linked, analyzed as-is), or a local directory (analyzed in place).\n\n" +
			"stdout is the NDJSON event stream — one event object per line with the\n" +
			"exact schema of the job's events.jsonl — ending with the terminal\n" +
			"job_result line; human diagnostics go to stderr. Exit codes follow the\n" +
			"table documented in root.go (0 covers done, degraded, and skipped runs).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch sourceMode {
			case "", "local", "remote":
				// Accepted for CLI compatibility; engine v1 classifies the
				// source itself (URL / local git / local dir), so the flag
				// is advisory and does not override classification.
			default:
				return fail(exitUsage, "invalid --source %q: want local or remote", sourceMode)
			}
			return runSource(args[0], run.RunRequest{
				Project:       project,
				OutputDir:     outputDir,
				Lang:          lang,
				SkipUnchanged: skipUnchanged,
				AllowDegraded: allowDegraded,
				Force:         force,
			}, force)
		},
	}
	f := cmd.Flags()
	f.StringVar(&project, "project", "", "project name (overrides the analysis dir and repos entry name)")
	f.StringVar(&outputDir, "output-dir", "", "write analysis artifacts to DIR instead of <scv-home>/analysis/<project>")
	f.StringVar(&lang, "lang", "", "document/analysis language (default zh)")
	f.StringVar(&sourceMode, "source", "", "source kind hint: local or remote (advisory; the engine classifies automatically)")
	f.BoolVar(&skipUnchanged, "skip-unchanged", false, "skip the run when the analyzed commit and artifacts are unchanged")
	f.BoolVar(&force, "force", false, "bypass the skip check and override a version-handshake mismatch")
	f.BoolVar(&allowDegraded, "allow-degraded", false, "proceed structure-degraded when uv is missing instead of failing the gate")
	return cmd
}

// runSource drives one full run: signal relay first (so no phase is
// unrelayed), then config, handshake, pipeline, exit-code mapping.
func runSource(source string, req run.RunRequest, force bool) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Signal relay (binding carry-over from the Task 9 review): an
	// external `scv jobs cancel` SIGTERMs the engine's whole process
	// group. Without this handler the engine would die on the default
	// TERM disposition and orphan the perceive subtree, which leads its
	// own process group. Cancelling the run context instead lets the
	// pipeline unwind in order: exec.CommandContext kills the perceive
	// group (grandchildren included), the job records its own cancelled
	// job_result, and the process exits with the CANCELLED exit code.
	// SIGINT gets the same treatment for interactive Ctrl-C. The handler
	// is installed before anything else runs so no phase of the command
	// is left unrelayed.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, os.Interrupt)
	defer signal.Stop(sigs)
	go func() {
		if _, ok := <-sigs; ok {
			cancel()
		}
	}()

	home, err := opts.home()
	if err != nil {
		return fail(exitError, "%v", err)
	}
	cfgPath, err := opts.configPath()
	if err != nil {
		return fail(exitError, "%v", err)
	}
	cfg, err := config.Load(cfgPath, home)
	if err != nil {
		return fail(exitError, "%v", err)
	}
	req.Source = source

	// Version handshake (spec section 12) before any work starts.
	verdict := probePerceiveVersion(home)
	if verdict.mismatch {
		if !force {
			return fail(exitVersionIncompatible,
				"VERSION_INCOMPATIBLE: perceive %s reports contract %q; engine supports [%s, %s]; rerun with --force to override",
				verdict.perceiveVersion, verdict.contract, contractMin, contractMax)
		}
		diagf("warning: perceive contract %s is outside the engine range [%s, %s]; continuing because of --force",
			verdict.contract, contractMin, contractMax)
	}

	printer := &eventPrinter{w: os.Stdout}
	if verdict.unknown {
		// Legacy perceive with no usable --version (spec section 12):
		// proceed leniently with an upfront degraded event. It is printed
		// on the NDJSON stream only — the handshake runs before a job
		// exists, so there is no durable log to append it to yet.
		printer.print(job.Event{
			TS:    time.Now().UTC(),
			Type:  job.EventDegraded,
			Stage: string(job.StateChecking),
			Detail: map[string]any{
				"reason": "contract-unknown",
				"source": "version-handshake",
			},
		})
	}

	// Wire the doc-generation hook when the config carries an llm
	// section (spec section 8); without one the run is structure-only.
	var docs run.DocGenerator
	if cfg.LLM != nil {
		if snap, err := env.Discover(env.Options{ScvHome: home}); err == nil && snap.Python3 != nil {
			docs = &docgen.Generator{
				Command:    cfg.LLM.Command,
				Timeout:    time.Duration(cfg.LLM.TimeoutS) * time.Second,
				Retry:      cfg.LLM.Retry,
				AssetsRoot: filepath.Join(snap.PerceptionRoot, "docgen"),
				Lang:       req.Lang,
			}
		}
		// A failed discovery here leaves docs nil; the pipeline's own
		// gate then fails the run with the precise ENV_INCOMPLETE list.
	}

	first := true
	res, err := run.RunWith(ctx, cfg, req, func(ev job.Event) {
		if first {
			first = false
			diagf("job %s", ev.ID)
		}
		printer.print(ev)
	}, docs)
	if err != nil {
		return fail(mapRunExit(err), "%v", err)
	}
	if res.SkippedReason != "" {
		diagf("skipped: %s", res.SkippedReason)
	}
	diagf("analysis dir: %s", res.OutputDir)
	for _, doc := range res.Docs {
		diagf("document: %s", doc)
	}
	return nil
}

// eventPrinter writes sink events to stdout as NDJSON: one compact JSON
// object per line, the exact schema of events.jsonl (the same struct
// marshaled the same way), with the terminal job_result line last.
type eventPrinter struct {
	w io.Writer
}

func (p *eventPrinter) print(ev job.Event) {
	data, err := json.Marshal(ev)
	if err != nil {
		diagf("warning: render %s event: %v", ev.Type, err)
		return
	}
	fmt.Fprintln(p.w, string(data))
}

// mapRunExit maps a pipeline error onto the exit-code table documented
// in root.go.
func mapRunExit(err error) int {
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, context.Canceled):
		return exitCancelled
	case errors.Is(err, run.ErrEnvIncomplete):
		return exitEnvIncomplete
	case errors.Is(err, run.ErrWorkflowLocked):
		return exitWorkflowLocked
	case errors.Is(err, repo.ErrPathNotFound):
		return exitPathNotFound
	case errors.Is(err, repo.ErrNameExists):
		return exitNameExists
	default:
		return exitError
	}
}

// versionVerdict is the outcome of the pre-run perceive handshake.
type versionVerdict struct {
	ok              bool // contract known and inside the engine range
	mismatch        bool // contract known, major outside the engine range
	unknown         bool // --version unsupported, unparseable, or unrunnable
	perceiveVersion string
	contract        string
}

// probePerceiveVersion runs the spec section 12 handshake: exec
// `<python3> <perceive> --version` and parse {"scv_version","contract"}.
// The perceive script resolution mirrors the run pipeline's seam
// ($SCV_ENGINE_TEST_PERCEIVE, else <perception root>/perceive.py), so a
// test stub is honored here too. A perceive that cannot run --version at
// all, or prints something unparseable, is the legacy path: unknown, and
// the caller proceeds leniently with a contract-unknown degraded event.
// The checking gate reports a genuinely broken environment (missing
// python3, missing perception root) with its full repair list, so this
// probe never fails the run itself.
func probePerceiveVersion(scvHome string) versionVerdict {
	unknown := versionVerdict{unknown: true}
	snap, err := env.Discover(env.Options{ScvHome: scvHome})
	if err != nil || snap.Python3 == nil {
		return unknown
	}
	script := os.Getenv(testPerceiveEnv)
	if script == "" {
		script = filepath.Join(snap.PerceptionRoot, "perceive.py")
	}
	ctx, cancel := context.WithTimeout(context.Background(), handshakeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, snap.Python3.Path, script, "--version").Output()
	if err != nil {
		return unknown
	}
	var v struct {
		ScvVersion string `json:"scv_version"`
		Contract   string `json:"contract"`
	}
	if json.Unmarshal(out, &v) != nil || v.Contract == "" {
		return unknown
	}
	verdict := versionVerdict{perceiveVersion: v.ScvVersion, contract: v.Contract}
	engineMajor := contractMajor(contractMax)
	switch major := contractMajor(v.Contract); {
	case major == 0:
		// Unparseable contract string (e.g. "one"): lenient, like a
		// perceive with no --version at all.
		return unknown
	case major != engineMajor:
		verdict.mismatch = true
	default:
		verdict.ok = true
	}
	return verdict
}

// contractMajor parses the leading integer of a "1.0"-style contract
// version; 0 when there is no parseable major (the handshake compares
// majors only — minor evolution is additive by contract).
func contractMajor(v string) int {
	head, _, _ := strings.Cut(v, ".")
	n, err := strconv.Atoi(strings.TrimSpace(head))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}
