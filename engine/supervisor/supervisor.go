// Package supervisor is the narrow embedding façade over the engine's
// internal packages: a host process (a server, a daemon, the Fiber
// example under examples/fiber) gets asynchronous runs with pollable,
// crash-honest job state, environment discovery, and the query store —
// without importing engine internals itself.
//
// The façade adds no logic of its own: every operation delegates to the
// internal packages (run, job, env, query) and only adapts their
// synchronous, CLI-shaped APIs to an async, handle-based surface.
//
// Process-group note: every run pipeline calls SetProcessGroup on its own
// process, so a run started in-process through this package moves the
// embedding host itself into a new process group (pgid == host pid).
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/projanvil/scv/engine/internal/config"
	"github.com/projanvil/scv/engine/internal/docgen"
	"github.com/projanvil/scv/engine/internal/env"
	"github.com/projanvil/scv/engine/internal/job"
	"github.com/projanvil/scv/engine/internal/query"
	"github.com/projanvil/scv/engine/internal/run"
)

// Supervisor owns the engine lifetime for one embedding host: it holds
// the root context every run derives from, the loaded config, and the
// scv home layout resolved from it. All methods are safe for
// concurrent use.
type Supervisor struct {
	ctx      context.Context
	cfg      config.Config
	jobsHome string
	registry string
	analysis string
}

// New validates cfg, resolves the scv home layout, and prepares the
// jobs directory (idempotent). The ctx passed here is the root of every
// run the supervisor starts: cancelling it unwinds all in-flight runs,
// each of which records its own terminal state. A Config without
// ScvHome is refused — the run pipeline requires one.
func New(ctx context.Context, cfg config.Config) (*Supervisor, error) {
	if cfg.ScvHome == "" {
		return nil, errors.New("supervisor: config carries no scv home")
	}
	jobsHome := filepath.Join(cfg.ScvHome, "jobs")
	if err := os.MkdirAll(jobsHome, 0o755); err != nil {
		return nil, fmt.Errorf("supervisor: prepare jobs home: %w", err)
	}
	return &Supervisor{
		ctx:      ctx,
		cfg:      cfg,
		jobsHome: jobsHome,
		// The registry file name and default analysis root mirror the
		// run pipeline's finalizing convention (internal/run) and the
		// `scv query` CLI wiring.
		registry: filepath.Join(cfg.ScvHome, "analysis-dirs.json"),
		analysis: filepath.Join(cfg.ScvHome, "analysis"),
	}, nil
}

// runOutcome is the terminal outcome of one supervised run, delivered
// exactly once on the handle's done channel.
type runOutcome struct {
	res *run.RunResult
	err error
}

// startResult reports how the synchronous start phase of a run ended:
// the id of the job the pipeline created (learned from its first event)
// or the error of a pipeline that returned before any event existed.
type startResult struct {
	id  string
	err error
}

// Run starts req asynchronously and returns a handle the moment the
// pipeline has created its job, so the id is stable and Status, Events,
// Cancel, and Wait all work right away.
//
// Context derivation: each run's context derives from the context New
// was given, not from ctx — an async run must outlive the caller's
// per-call scope (an HTTP request ends long before its analysis does).
// The per-call ctx bounds only this synchronous start phase; cancelling
// one run is JobHandle.Cancel. Cancelling the supervisor context
// unwinds every in-flight run.
//
// The run pipeline creates and owns its job directory itself
// (<scv-home>/jobs/<id>, with its owning pid recorded there); the
// supervisor learns the id from the run's first sink event — the same
// pattern the scv CLI uses — rather than pre-creating a directory the
// pipeline would never adopt.
//
// Doc generation: when the config carries an llm section the docgen
// generator is wired exactly the way `scv run` wires it (command,
// timeout, retry, perception-root assets, per-request language), so an
// embedding host whose config enables command-mode docgen gets the four
// documents instead of a silent structure-only run. Without an llm
// section the run stays structure-only, like the CLI.
func (s *Supervisor) Run(ctx context.Context, req run.RunRequest) (*JobHandle, error) {
	runCtx, cancel := context.WithCancel(s.ctx)
	start := make(chan startResult, 1)
	done := make(chan runOutcome, 1)

	go func() {
		defer cancel() // release the derived context once the run ends
		var docs run.DocGenerator
		if s.cfg.LLM != nil {
			// A failed discovery here leaves docs nil; the pipeline's own
			// gate then fails the run with the precise ENV_INCOMPLETE
			// list — the CLI's behavior verbatim.
			if snap, err := env.Discover(env.Options{ScvHome: s.cfg.ScvHome}); err == nil && snap.Python3 != nil {
				docs = &docgen.Generator{
					Command:    s.cfg.LLM.Command,
					Timeout:    time.Duration(s.cfg.LLM.TimeoutS) * time.Second,
					Retry:      s.cfg.LLM.Retry,
					AssetsRoot: filepath.Join(snap.PerceptionRoot, "docgen"),
					Lang:       req.Lang,
				}
			}
		}
		started := false
		res, err := run.RunWith(runCtx, &s.cfg, req, func(ev job.Event) {
			if !started {
				started = true
				start <- startResult{id: ev.ID}
			}
		}, docs)
		if !started {
			// Returned before creating a job (e.g. the jobs home is
			// unwritable); err is the reason, defensive for a nil one.
			if err == nil {
				err = errors.New("pipeline returned without creating a job")
			}
			start <- startResult{err: err}
		}
		done <- runOutcome{res: res, err: err}
	}()

	select {
	case sr := <-start:
		if sr.err != nil {
			return nil, fmt.Errorf("supervisor: start run: %w", sr.err)
		}
		return &JobHandle{
			ID:   sr.id,
			dir:  filepath.Join(s.jobsHome, sr.id),
			done: done,
			stop: cancel,
		}, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("supervisor: start run: %w", ctx.Err())
	case <-s.ctx.Done():
		return nil, fmt.Errorf("supervisor: shutting down: %w", s.ctx.Err())
	}
}

// JobHandle is one asynchronously running job. ID is valid the moment
// Run returns; the unexported fields carry the job directory and the
// run's terminal outcome. All methods are safe for concurrent use.
type JobHandle struct {
	// ID is the job id (the <scv-home>/jobs/<id> directory name).

	ID string

	dir  string
	done chan runOutcome
	stop context.CancelFunc
}

// Status reads the job's durable snapshot and events-log position,
// applying the spec section 7 crash rule — a non-terminal job whose
// owning process died is marked failed/INTERRUPTED and the flip is
// persisted — the same read semantics as `scv jobs status`. In-process
// the owning process is the host itself, so the rule only fires for
// jobs adopted after a host crash.
func (h *JobHandle) Status(ctx context.Context) (*job.Job, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("supervisor: status %s: %w", h.ID, err)
	}
	j, _, err := job.LoadStatus(h.dir)
	if err != nil {
		return nil, fmt.Errorf("supervisor: load job %s: %w", h.ID, err)
	}
	if job.MarkInterruptedIfDead(j) {
		if err := j.Snapshot(h.dir); err != nil {
			return nil, fmt.Errorf("supervisor: persist INTERRUPTED state: %w", err)
		}
	}
	return j, nil
}

// Events returns the job's events with Seq greater than since (0 for
// the whole log), in log order — the cursor shape of
// `scv jobs events --since`.
func (h *JobHandle) Events(ctx context.Context, since int) ([]job.Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("supervisor: events %s: %w", h.ID, err)
	}
	_, events, err := job.LoadStatus(h.dir)
	if err != nil {
		return nil, fmt.Errorf("supervisor: load job %s: %w", h.ID, err)
	}
	if since <= 0 {
		return events, nil
	}
	out := make([]job.Event, 0, len(events))
	for _, ev := range events {
		if ev.Seq > since {
			out = append(out, ev)
		}
	}
	return out, nil
}

// Cancel stops the run behind the handle.
//
// Cancel path: in-process runs are cancelled through their derived
// context — exec.CommandContext kills the perceive subtree and the
// pipeline records its own cancelled terminal state, the same unwind
// the scv CLI drives when relaying an external SIGTERM. run.Cancel, the
// external `scv jobs cancel` path, signals the job's owning process
// group; in-process that group is the embedding host itself (the
// pipeline records os.Getpid()), so calling it here would terminate
// the host. Cancelling an already-terminal job is refused with the
// pipeline's WORKFLOW_NOT_CANCELLABLE sentinel; a cancel that loses
// the race with a finishing run leaves that run's own result standing.
func (h *JobHandle) Cancel(ctx context.Context) error {
	j, err := h.Status(ctx)
	if err != nil {
		return err
	}
	if job.IsTerminal(j.State) {
		return fmt.Errorf("%w: job %s already ended as %s",
			run.ErrWorkflowNotCancellable, h.ID, j.State)
	}
	h.stop()
	return nil
}

// Wait blocks until the run reaches its terminal outcome and returns
// the pipeline's result. The error is the pipeline's own, returned
// unwrapped so errors.Is keeps matching the run package's sentinels
// (ErrEnvIncomplete, ErrAnalyzerFailed, ...) and context.Canceled for
// cancelled runs. Wait cancelled through ctx returns ctx.Err() without
// stopping the run and may be called again.
func (h *JobHandle) Wait(ctx context.Context) (*run.RunResult, error) {
	select {
	case o := <-h.done:
		h.done <- o // keep the outcome in place for repeat or concurrent Waiters
		return o.res, o.err
	case <-ctx.Done():
		return nil, fmt.Errorf("supervisor: wait %s: %w", h.ID, ctx.Err())
	}
}

// Env discovers the host environment snapshot (tools, perception and
// dist roots) for display or gate checks — a thin pass-through to
// env.Discover pinned to the supervisor's scv home.
func (s *Supervisor) Env(ctx context.Context) (*env.Env, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("supervisor: env: %w", err)
	}
	e, err := env.Discover(env.Options{ScvHome: s.cfg.ScvHome})
	if err != nil {
		return nil, fmt.Errorf("supervisor: discover env: %w", err)
	}
	return e, nil
}

// Query opens the vectra query store over the scv home's analysis-dir
// registry — the same store the `scv query` CLI serves.
func (s *Supervisor) Query() (*query.Store, error) {
	store, err := query.Open(s.registry, s.analysis)
	if err != nil {
		return nil, fmt.Errorf("supervisor: open query store: %w", err)
	}
	return store, nil
}
