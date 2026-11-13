// Package run implements the scv run pipeline (spec section 7): the
// unified checking gate, repo preparing (clone/link), the skip-unchanged
// check, the perceive.py exec with live perception.log tailing mapped to
// stage events, the doc-generation hook (Task 7), and finalizing
// (metadata port + analysis-dir registry).
//
// Every stage transition lands in the job event log (internal/job) and is
// forwarded live to the caller's sink, so `scv run` can stream NDJSON
// progress while the same events persist under ~/.scv/jobs/<id>/.
package run

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/projanvil/scv/engine/internal/config"
	"github.com/projanvil/scv/engine/internal/docgen"
	"github.com/projanvil/scv/engine/internal/env"
	"github.com/projanvil/scv/engine/internal/job"
	"github.com/projanvil/scv/engine/internal/repo"
)

// Pipeline error codes (spec section 7). CLONE_FAILED and PATH_NOT_FOUND
// arrive as repo package sentinels and keep their wire spelling here.
const (
	CodeEnvIncomplete  = "ENV_INCOMPLETE"
	CodeCloneFailed    = "CLONE_FAILED"
	CodePathNotFound   = "PATH_NOT_FOUND"
	CodeNameExists     = "NAME_EXISTS"
	CodeAnalyzerFailed = "ANALYZER_FAILED"
	CodeDocGenFailed   = "DOCGEN_FAILED"
	CodeFinalizeFailed = "FINALIZE_FAILED"
	CodeCancelled      = "CANCELLED"
)

// Sentinel errors the CLI can map with errors.Is. CLONE_FAILED and
// PATH_NOT_FOUND surface through the repo package's own sentinels.
var (
	ErrEnvIncomplete  = errors.New(CodeEnvIncomplete)
	ErrAnalyzerFailed = errors.New(CodeAnalyzerFailed)
)

// SkipReasonCommitUnchanged is the job_result detail.reason of a
// --skip-unchanged hit (spec section 7).
const SkipReasonCommitUnchanged = "commit-unchanged"

// RunRequest is one invocation of the run pipeline. Source is a git URL,
// a local git worktree, or a local directory. Project overrides the
// analysis dir name (and, for linked entries, the repos entry name).
// OutputDir relocates the analysis artifacts; Lang defaults to "zh".
type RunRequest struct {
	Source        string
	Project       string
	OutputDir     string
	Lang          string
	SkipUnchanged bool
	AllowDegraded bool
	Force         bool
}

// RunResult reports what a run produced. Perception is the parsed
// perceive.py stdout JSON (nil when perceive never got that far),
// OutputDir is the analysis dir used, Docs lists generated documents
// (Task 7), and SkippedReason is set when the run or its structure tier
// was skipped ("commit-unchanged", "uv-missing", "disabled").
type RunResult struct {
	Perception    map[string]any
	OutputDir     string
	Docs          []string
	SkippedReason string
}

// DocGenerator is the doc-generation hook seam (spec section 8, Task 7).
// The pipeline wraps one call in the generating stage, hands it the
// perceive summary, and forwards every event it emits through the job
// event log. A nil generator means structure-only runs: the generating
// stage is simply absent.
type DocGenerator interface {
	Generate(ctx context.Context, repoPath, analysisDir, structureJSONPath string, summary map[string]any, sink func(job.Event)) ([]string, error)
}

// discoverFunc is the env discovery seam; production runs use env.Discover
// and tests inject deterministic snapshots.
type discoverFunc func(env.Options) (*env.Env, error)

// Run executes one full pipeline as a structure-only run (no doc
// generator wired). See RunWith for the docgen hook.
func Run(ctx context.Context, cfg *config.Config, req RunRequest, sink func(job.Event)) (*RunResult, error) {
	return runPipeline(ctx, cfg, req, sink, nil, env.Discover)
}

// RunWith is Run with the doc-generation hook injected. Task 7's
// generator (and the CLI wiring it) enter here.
func RunWith(ctx context.Context, cfg *config.Config, req RunRequest, sink func(job.Event), docs DocGenerator) (*RunResult, error) {
	return runPipeline(ctx, cfg, req, sink, docs, env.Discover)
}

// runPipeline drives the frozen stage flow of spec section 7:
//
//	checking (unified gate) → preparing (clone/link) → skip-check →
//	perceive (tier events) → generating (hook) → finalizing → done
//
// with the per-path reduced sequences falling out of the gate verdict:
// blocking items fail at checking with every missing item listed at once,
// degrading items drop their stages from the sequence.
func runPipeline(ctx context.Context, cfg *config.Config, req RunRequest, sink func(job.Event), docs DocGenerator, discover discoverFunc) (*RunResult, error) {
	if cfg == nil {
		return nil, errors.New("run: config is required")
	}
	if cfg.ScvHome == "" {
		return nil, errors.New("run: config carries no scv home")
	}
	if sink == nil {
		sink = func(job.Event) {}
	}
	res := &RunResult{}

	j, jobDir, err := job.Create(filepath.Join(cfg.ScvHome, "jobs"))
	if err != nil {
		return nil, fmt.Errorf("run: create job: %w", err)
	}
	// Spec section 10: lead our own process group and record it on the
	// job, so `jobs cancel` can signal the whole run tree.
	SetProcessGroup()
	j.Pid = os.Getpid() // own pgid after SetProcessGroup
	if err := j.Snapshot(jobDir); err != nil {
		return nil, fmt.Errorf("run: record owning pid: %w", err)
	}
	em := &emitter{j: j, dir: jobDir, sink: sink}

	// checking: the unified dependency gate (spec section 7).
	if err := em.enterStage(job.StateChecking); err != nil {
		return res, err
	}
	snapshot, err := discover(env.Options{ScvHome: cfg.ScvHome})
	if err != nil {
		return res, em.fail(job.StateChecking, job.EventError{
			Code:    CodeEnvIncomplete,
			Message: fmt.Sprintf("environment discovery failed: %v", err),
		}, nil, fmt.Errorf("%w: environment discovery failed: %v", ErrEnvIncomplete, err))
	}
	mode := env.Mode{Structure: structureWanted(cfg), Query: true, DocGen: true}
	gate := snapshot.Gate(mode, cfg)
	if req.AllowDegraded {
		gate = demoteUVBlocking(gate)
	}
	if len(gate.Blocking) > 0 {
		return res, em.fail(job.StateChecking, job.EventError{
			Code:    CodeEnvIncomplete,
			Message: "blocking environment items missing: " + itemSummary(gate.Blocking),
		}, map[string]any{"missing": itemsDetail(gate.Blocking)},
			fmt.Errorf("%w: blocking environment items missing: %s", ErrEnvIncomplete, itemSummary(gate.Blocking)))
	}
	structureOn := mode.Structure && !hasItem(gate.Degrading, "uv")
	queryOn := !hasCapability(gate.Degrading, env.CapabilityQuery)
	docgenOn := cfg.LLM != nil
	if len(gate.Degrading) > 0 {
		if err := em.emit(job.EventDegraded, string(job.StateChecking),
			map[string]any{"missing": itemsDetail(gate.Degrading)}, nil); err != nil {
			return res, err
		}
	}
	if err := em.finishStage(job.StateChecking, nil); err != nil {
		return res, err
	}

	// preparing: classify the source, clone or link, resolve the commit.
	if err := em.enterStage(job.StatePreparing); err != nil {
		return res, err
	}
	prep, err := prepare(cfg.ScvHome, req)
	if err != nil {
		return res, em.fail(job.StatePreparing, preparingError(err), nil,
			fmt.Errorf("preparing: %w", err))
	}
	res.OutputDir = prep.analysisDir
	// Per-repo mutual exclusion (spec section 10): hold the analysis
	// dir lock for the rest of the run; a second run on the same dir
	// fails fast here with WORKFLOW_LOCKED.
	release, err := AcquireRepoLock(prep.analysisDir)
	if err != nil {
		return res, em.fail(job.StatePreparing, lockFailureEvent(prep.analysisDir, err), nil,
			fmt.Errorf("preparing: %w", err))
	}
	defer release()
	if err := em.finishStage(job.StatePreparing, prep.detail()); err != nil {
		return res, err
	}

	// skip-check: pending→skipped when the commit is unchanged and every
	// artifact the current mode expects already exists.
	if req.SkipUnchanged && !req.Force && ShouldSkip(prep.analysisDir, prep.commit, docgenOn) {
		res.SkippedReason = SkipReasonCommitUnchanged
		return res, em.terminate(job.StateSkipped,
			map[string]any{"reason": SkipReasonCommitUnchanged}, nil)
	}

	// perceive: exec + live tier event mapping.
	perception, perr := runPerceiveStage(ctx, em, cfg, snapshot, prep, req, structureOn, queryOn)
	if perception != nil {
		res.Perception = perception
	}
	if perr != nil {
		return res, perr
	}
	if status, _ := perception["status"].(string); status == "skipped" {
		// Fold the in-band skip reasons into the run result; the gate
		// already reported them, so no second event (spec section 7).
		if reason, _ := perception["reason"].(string); reason == "uv-missing" || reason == "disabled" {
			res.SkippedReason = reason
		}
	}

	// generating: the Task 7 hook, only when structure succeeded and the
	// llm config survived the gate.
	if status, _ := perception["status"].(string); status == "ok" && docgenOn && docs != nil {
		if err := em.enterStage(job.StateGenerating); err != nil {
			return res, err
		}
		paths, gerr := docs.Generate(ctx, prep.repoPath, prep.analysisDir,
			filepath.Join(prep.analysisDir, "structure.json"), perception, em.forward)
		if gerr != nil {
			// A typed docgen failure carries the precise wire code (e.g.
			// STAGE_TIMEOUT, not DOCGEN_FAILED) and the command's stderr
			// tail; any other error keeps the generic mapping.
			evErr := job.EventError{
				Code:    CodeDocGenFailed,
				Message: gerr.Error(),
			}
			var failure *docgen.Failure
			if errors.As(gerr, &failure) {
				evErr.Code = failure.Code
				evErr.LogTail = failure.LogTail
			}
			return res, em.fail(job.StateGenerating, evErr, nil, fmt.Errorf("docgen: %w", gerr))
		}
		res.Docs = paths
		if err := em.finishStage(job.StateGenerating,
			map[string]any{"documents": len(paths)}); err != nil {
			return res, err
		}
	}

	// finalizing: metadata port + analysis-dir registry.
	if err := em.enterStage(job.StateFinalizing); err != nil {
		return res, err
	}
	if err := WriteMetadata(prep.analysisDir, prep.repoPath, prep.commit); err != nil {
		return res, em.fail(job.StateFinalizing, job.EventError{
			Code:    CodeFinalizeFailed,
			Message: fmt.Sprintf("write metadata: %v", err),
		}, nil, fmt.Errorf("finalizing: write metadata: %w", err))
	}
	registryPath := filepath.Join(cfg.ScvHome, registryFile)
	if err := AppendAnalysisDir(registryPath, prep.analysisDir); err != nil {
		return res, em.fail(job.StateFinalizing, job.EventError{
			Code:    CodeFinalizeFailed,
			Message: fmt.Sprintf("register analysis dir: %v", err),
		}, nil, fmt.Errorf("finalizing: register analysis dir: %w", err))
	}
	if err := em.finishStage(job.StateFinalizing,
		map[string]any{"analysis_dir": prep.analysisDir}); err != nil {
		return res, err
	}

	doneDetail := map[string]any{
		"analysis_dir":      prep.analysisDir,
		"perception_status": perception["status"],
	}
	if reason, ok := perception["reason"]; ok {
		doneDetail["perception_reason"] = reason
	}
	return res, em.terminate(job.StateDone, doneDetail, nil)
}

// emitter fans every pipeline event out to the durable job log and the
// caller's live sink, and keeps job.json in step with the stage machine.
type emitter struct {
	j    *job.Job
	dir  string
	sink func(job.Event)
}

// emit appends one event to the job log, then forwards it to the sink.
func (e *emitter) emit(typ, stage string, detail map[string]any, evErr *job.EventError) error {
	ev, err := e.j.Emit(e.dir, typ, stage, detail, evErr)
	if err != nil {
		return fmt.Errorf("emit %s:%s: %w", typ, stage, err)
	}
	e.sink(ev)
	return nil
}

// enterStage records the state transition, emits stage_started, and
// persists the snapshot (spec section 7: job.json is fsynced on every
// state transition).
func (e *emitter) enterStage(s job.State) error {
	e.j.State = s
	e.j.Stage = string(s)
	if err := e.emit(job.EventStageStarted, string(s), nil, nil); err != nil {
		return err
	}
	if err := e.j.Snapshot(e.dir); err != nil {
		return fmt.Errorf("snapshot transition: %w", err)
	}
	return nil
}

// finishStage emits stage_finished; the job state is unchanged.
func (e *emitter) finishStage(s job.State, detail map[string]any) error {
	return e.emit(job.EventStageFinished, string(s), detail, nil)
}

// fail emits stage_failed for s, terminates the job as failed, and
// returns ret (the semantic error for the caller) — unless an event
// write itself fails, which wins.
func (e *emitter) fail(s job.State, evErr job.EventError, detail map[string]any, ret error) error {
	if err := e.emit(job.EventStageFailed, string(s), detail, &evErr); err != nil {
		return err
	}
	if err := e.terminate(job.StateFailed, detail, &evErr); err != nil {
		return err
	}
	return ret
}

// terminate moves the job to a terminal state, snapshots it, and emits
// the terminal job_result event whose stage is the terminal state.
func (e *emitter) terminate(state job.State, detail map[string]any, evErr *job.EventError) error {
	e.j.State = state
	e.j.Stage = string(state)
	if evErr != nil {
		e.j.Detail = map[string]any{"error": *evErr}
	}
	if err := e.j.Snapshot(e.dir); err != nil {
		return fmt.Errorf("snapshot terminal state: %w", err)
	}
	return e.emit(job.EventJobResult, string(state), detail, evErr)
}

// forward re-emits an externally built event (the docgen hook's
// sub-events) through the job log — so it gets a sequence number and
// durability — and on to the live sink.
func (e *emitter) forward(ev job.Event) {
	emitted, err := e.j.Emit(e.dir, ev.Type, ev.Stage, ev.Detail, ev.Error)
	if err != nil {
		return
	}
	e.sink(emitted)
}

// prepared is the outcome of the preparing stage.
type prepared struct {
	kind        repo.SourceKind
	repoPath    string // what perceive analyzes
	entryName   string // repos entry name, "" for in-place dirs
	commit      string // HEAD commit, "" for non-git sources
	analysisDir string
}

func (p *prepared) detail() map[string]any {
	return map[string]any{
		"source_kind": string(p.kind),
		"repo_path":   p.repoPath,
		"head_commit": p.commit,
	}
}

// prepare resolves the run source into a repo entry and analysis dir
// (spec section 6): git URLs are cloned (entry name derived from the
// URL), local git worktrees are symlinked (req.Project overriding the
// entry name), local directories are analyzed in place with no entry.
func prepare(scvHome string, req RunRequest) (*prepared, error) {
	kind := repo.Classify(req.Source)
	p := &prepared{kind: kind}

	switch kind {
	case repo.KindURL:
		entry, err := repo.Clone(scvHome, req.Source, "")
		if errors.Is(err, repo.ErrNameExists) {
			entry, err = refreshExistingClone(scvHome, req.Source)
		}
		if err != nil {
			return nil, err
		}
		p.entryName, p.repoPath, p.commit = entry.Name, entry.Path, entry.HeadCommit
	case repo.KindLocalGit:
		entry, err := repo.Link(scvHome, req.Source, req.Project)
		if errors.Is(err, repo.ErrNameExists) {
			entry, err = reuseLink(scvHome, req.Source, req.Project)
		}
		if err != nil {
			return nil, err
		}
		p.entryName, p.repoPath, p.commit = entry.Name, entry.Path, entry.HeadCommit
	default: // KindLocalDir: analyzed in place, commit metadata empty
		abs, err := filepath.Abs(req.Source)
		if err != nil {
			return nil, fmt.Errorf("resolve source: %w", err)
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("%w: %s", repo.ErrPathNotFound, abs)
		}
		p.repoPath = abs
		p.commit, _ = repo.HeadCommit(abs) // best effort: not a worktree → ""
	}

	name := req.Project
	if name == "" && p.entryName != "" {
		name = p.entryName
	}
	if name == "" {
		name = filepath.Base(p.repoPath)
	}
	if req.OutputDir != "" {
		abs, err := filepath.Abs(req.OutputDir)
		if err != nil {
			return nil, fmt.Errorf("resolve output dir: %w", err)
		}
		p.analysisDir = abs
	} else {
		p.analysisDir = filepath.Join(scvHome, "analysis", name)
	}
	return p, nil
}

// reuseLink adopts an existing repos entry for a repeated local-git run:
// valid only when the entry still symlinks to the same worktree.
func reuseLink(scvHome, source, name string) (*repo.Entry, error) {
	abs, err := filepath.Abs(source)
	if err != nil {
		return nil, fmt.Errorf("resolve source: %w", err)
	}
	if name == "" {
		name = filepath.Base(abs)
	}
	target := filepath.Join(scvHome, "repos", name)
	link, err := os.Readlink(target)
	if err != nil {
		return nil, fmt.Errorf("%w: repos entry %q exists but is not a symlink to %s: %v",
			repo.ErrNameExists, name, abs, err)
	}
	if link != abs {
		return nil, fmt.Errorf("%w: repos entry %q already points at %s, not %s; pick another --project",
			repo.ErrNameExists, name, link, abs)
	}
	e := &repo.Entry{Name: name, Path: target, Type: repo.TypeLinked}
	e.HeadCommit, _ = repo.HeadCommit(target)
	return e, nil
}

// refreshExistingClone pulls the existing clone of url for a repeated
// remote run instead of refusing the re-run.
func refreshExistingClone(scvHome, url string) (*repo.Entry, error) {
	entries, err := repo.List(scvHome)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Type == repo.TypeRemote && e.URL == url {
			return repo.Pull(scvHome, e.Name)
		}
	}
	return nil, fmt.Errorf("%w: no remote entry for %s under %s", repo.ErrNameExists, url, scvHome)
}

// preparingError maps a prepare failure to its wire code event error.
func preparingError(err error) job.EventError {
	evErr := job.EventError{Code: CodeCloneFailed, Message: err.Error(), Retryable: true}
	switch {
	case errors.Is(err, repo.ErrPathNotFound):
		evErr.Code, evErr.Retryable = CodePathNotFound, false
	case errors.Is(err, repo.ErrNameExists):
		evErr.Code, evErr.Retryable = CodeNameExists, false
	}
	return evErr
}

// structureWanted reports whether the run expects structure output: the
// config must enable the structure tier and name at least one language
// (empty structure_languages turns the tier off, mirroring perceive.py).
func structureWanted(cfg *config.Config) bool {
	return cfg.StructureAnalysisEnabled && len(cfg.StructureLanguages) > 0
}

// demoteUVBlocking moves the uv blocking item to the degrading list —
// the --allow-degraded contract: the run proceeds structure-degraded.
func demoteUVBlocking(g env.GateResult) env.GateResult {
	for i := len(g.Blocking) - 1; i >= 0; i-- {
		if g.Blocking[i].Item == "uv" {
			item := g.Blocking[i]
			item.Severity = env.SeverityDegrading
			g.Blocking = append(g.Blocking[:i], g.Blocking[i+1:]...)
			g.Degrading = append(g.Degrading, item)
		}
	}
	return g
}

func hasItem(items []env.Item, name string) bool {
	for _, it := range items {
		if it.Item == name {
			return true
		}
	}
	return false
}

func hasCapability(items []env.Item, capability string) bool {
	for _, it := range items {
		if it.Capability == capability {
			return true
		}
	}
	return false
}

// itemsDetail renders gate items in the uniform wire schema of spec
// section 7: {"item","status","severity","capability","hint"}.
func itemsDetail(items []env.Item) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		out = append(out, map[string]any{
			"item":       it.Item,
			"status":     it.Status,
			"severity":   it.Severity,
			"capability": it.Capability,
			"hint":       it.Hint,
		})
	}
	return out
}

// itemSummary renders a one-line repair list for error messages.
func itemSummary(items []env.Item) string {
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, fmt.Sprintf("%s (%s)", it.Item, it.Hint))
	}
	return strings.Join(parts, "; ")
}
