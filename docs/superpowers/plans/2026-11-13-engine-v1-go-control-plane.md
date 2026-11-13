# SCV Engine v1 (Go Control Plane) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `scv` Go binary (one-shot control plane) that owns repo management, run orchestration, job state/events, command-mode docgen, environment gating, and vectra queries, plus the three additive perceive.py changes and `make pack`.

**Architecture:** New `engine/` Go module wrapping the existing Python data plane (`perceive.py` exec'd per run). State lives on disk (`~/.scv/jobs/<id>/`); hosts poll or tail NDJSON. LLM access is a host-configured command exec'd 4× per run; keys never enter SCV.

**Tech Stack:** Go 1.27, spf13/cobra (CLI), github.com/projanvil/vectra v0.14.4 (in-process queries), Python 3.9+ orchestrator (unchanged), uv-managed analyzer (unchanged).

**Spec:** `docs/superpowers/specs/2026-11-13-engine-v1-go-control-plane-design.md` (rev 2) — the plan argues from the spec; executors read both.

## Global Constraints

- Go module `github.com/projanvil/scv/engine` at `engine/`; `go 1.27`; vectra pinned `v0.14.4` (same as `perception/graphmcp/go.mod`).
- **All code comments, doc comments, test names, and commit messages in English.** Conventional commits (`feat:`, `test:`, `chore:`, `fix:`).
- Every task ships unit tests; coverage gate ≥80% on `engine/internal/...` via `make engine-test` (runs `go test -race -coverprofile`); no task may lower the gate.
- perceive.py changes are **additive only** (spec §12): existing perception tests stay green; run them with `cd perception && uv run --project analyzer python -m pytest tests/ -v`.
- No modifications under `skills/`, `agents/`, `perception/loader/`, `perception/graphmcp/`, `perception/analyzer/` (except the three spec §12 edits in `perception/perceive.py` + `perception/tests/`).
- perceive stdout contract (single-line JSON, exit 0 always) is untouchable.
- Error codes, exact strings: `ENV_INCOMPLETE`, `WORKFLOW_LOCKED`, `WORKFLOW_NOT_CANCELLABLE`, `ANALYZER_FAILED`, `CLONE_FAILED`, `PATH_NOT_FOUND`, `DOCGEN_FAILED`, `STAGE_TIMEOUT`, `VERSION_INCOMPATIBLE`, `INTERRUPTED`, `CANCELLED`.
- Event types: `stage_started|stage_finished|stage_failed|degraded|artifact_produced|job_result`. States: `pending|checking|preparing|detecting|analyzing|grading|query_tier|generating|finalizing|done|failed|cancelled|skipped`.
- Default stage timeout 600s (matches perceive TIER_TIMEOUT).
- No API keys in argv, logs, or state files; config accepts `env:VAR` / `file:/path` references only, passed as references.

## Review Focus

1. **Torn state after crash mid-write** (job.json rename interrupted, truncated last line in events.jsonl): readers must tolerate and recover, never fail the whole job read — pinned in Task 3 (`TestEventsToleratesTruncatedLastLine`, `TestJobSnapshotAtomicSwap`).
2. **perceive stdout is not JSON** (analyzer crash prints traceback): run fails `ANALYZER_FAILED` with log tail, engine never panics — pinned in Task 6 (`TestPerceiveGarbageStdoutFailsAnalyzerFailed`).
3. **Repo name collisions** (`scv repo link` name already exists as a clone, or vice versa; names with spaces/unicode): clear error, nothing clobbered — pinned in Task 4 (`TestLinkNameCollisionRefuses`, `TestNamesWithSpacesRoundTrip`).
4. **Cancel leaves orphan grandchildren** (uv/analyzer children of perceive.py): process-group kill must reap the tree — pinned in Task 9 (`TestCancelKillsProcessGroupTree`).
5. **Host dies mid-run** (backgrounded `scv run`, host process tree reaped): next `jobs status` marks `INTERRUPTED`, re-run with skip works — pinned in Task 3 (`TestDeadPidMarkedInterrupted`).

---

### Task 1: Module scaffold + `internal/config`

**Files:**
- Create: `engine/go.mod`, `engine/Makefile` (targets: `build`, `test`, `test-integration`, `cover`)
- Modify: root `Makefile` — add delegation targets `engine-build`, `engine-test`, `engine-test-integration` (`$(MAKE) -C engine …`); every later task's `make engine-test` and Task 11's `pack: engine-build` resolve against these
- Create: `engine/internal/config/config.go`
- Test: `engine/internal/config/config_test.go`

**Interfaces:**
- Produces: `type LLM struct { Mode string; Command []string; TimeoutS int; Retry int }`; `type Config struct { ScvHome, Path string; LLM *LLM; StructureExtensions []string; StructureLanguages []string; StructureAnalysisEnabled bool; DeepAnalysisEnabled bool }`; `func Load(path, scvHome string) (*Config, error)` (defaults mirror `config.example.json`: extensions `["springboot"]`, all six languages, structure on; unknown keys ignored, no error); `func (c *Config) SecretRefs() []string` (returns `env:`/`file:`-shaped values; validation is warn-only).

- [ ] **Step 1: Write failing tests** — `TestLoadDefaults`, `TestUnknownKeysIgnored`, `TestLLMSectionParsed`, `TestMissingFileYieldsDefaults`. Assertions: defaults exactly `["springboot"]` / six languages / `StructureAnalysisEnabled=true`; absent file is not an error.
- [ ] **Step 2: Run** `cd engine && go test ./internal/config/ -v` — expect FAIL (no package).
- [ ] **Step 3: Implement** — `go mod init github.com/projanvil/scv/engine`; config load via `encoding/json` into a struct with `DisallowUnknownFields` **disabled** (unknown keys ignored by design); Makefile `engine-test`: `go test ./... -race -coverprofile=cover.out && go tool cover -func=cover.out | awk '$$3+0 < 80 && $$1 ~ /internal/ {print; f=1} END {exit f}'` — implementers verify the awk gate fires on low coverage once by temporarily lowering a threshold in a scratch run (not committed).
- [ ] **Step 4: Run tests** — PASS.
- [ ] **Step 5: Commit** — `git add engine/go.mod engine/Makefile engine/internal/config/ && git commit -m "feat(engine): module scaffold and config loading"`

### Task 2: `internal/env` — discovery + unified dependency gate

**Files:**
- Create: `engine/internal/env/env.go`, `engine/internal/env/gate.go`
- Test: `engine/internal/env/env_test.go`, `engine/internal/env/gate_test.go`

**Interfaces:**
- Consumes: `config.Config` (Task 1).
- Produces: `type Tool struct { Path, Version string; OK bool }`; `type Item struct { Item, Status, Severity, Capability, Hint string }` (Status `ok|missing`, Severity `blocking|degrading|none`); `type Env struct { ScvHome, PerceptionRoot, DistRoot string; Uv, Python3, Git, Go, Java *Tool; PrebuiltLoader, PrebuiltGraphMCP bool }`; `type Options struct { ScvHome string; ExtraPath []string; EnvVars []string }` (injectable PATH/env for tests); `func Discover(opts Options) (*Env, error)`; `type Mode struct { Structure, Query, DocGen bool }`; `type GateResult struct { Blocking, Degrading []Item }`; `func (e *Env) Gate(m Mode) GateResult`.
- Gate rules (spec §7): `func (e *Env) Gate(m Mode, cfg *config.Config) GateResult` — cfg is a parameter (docgen/deep-analysis rules read it). Blocking = python3 missing, git missing (when repo ops needed), perception root missing, uv missing **and** `Mode.Structure`; degrading = (go missing AND no prebuilt loader) → capability `query`; docgen-unconfigured when `Mode.DocGen` and `cfg.LLM == nil`; `deep-analysis-unsupported` when `cfg.DeepAnalysisEnabled`.

- [ ] **Step 1: Write failing tests** — `TestDiscoverFindsTools` (uses `t.TempDir()` fake bin dir + `ExtraPath`), `TestDiscoverUvFallbackPaths`, `TestDiscoverPrebuiltDist`, `TestGateBlockingCombinesAllMissing` (uv+perception root both missing ⇒ Blocking has BOTH items — the spec §7 one-shot list), `TestGateDegradingQueryOff`, `TestGateDegradingDocgenUnconfigured`, `TestJavaInformationalNeverBlocking`.
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement** — `exec.LookPath` with prepended `ExtraPath`; uv fallbacks `~/.local/bin/uv`, `/opt/homebrew/bin/uv`; prebuilt check = `dist/loader/perception-loader` + `dist/graphmcp/scv-graph-mcp` exist (mtime freshness left to perceive.py); version probes `uv --version`, `go version` (parse `go1.27+`); JAVA_HOME chain per spec §9.
- [ ] **Step 4: Run tests** — PASS.
- [ ] **Step 5: Commit** — `feat(engine): environment discovery and unified dependency gate`

### Task 3: `internal/job` — state machine + event log

**Files:**
- Create: `engine/internal/job/job.go`, `engine/internal/job/events.go`
- Test: `engine/internal/job/job_test.go`

**Interfaces:**
- Produces: `type State string` with `StatePending`, `StateChecking`, `StatePreparing`, `StateDetecting`, `StateAnalyzing`, `StateGrading`, `StateQueryTier`, `StateGenerating`, `StateFinalizing`, `StateDone`, `StateFailed`, `StateCancelled`, `StateSkipped`; `type EventError struct { Code, Message string; Retryable bool; LogTail string }`; `type Event struct { ID string; Seq int; TS time.Time; Type, Stage string; Detail map[string]any; Error *EventError }`; `type Job struct { ID, State, Stage string; Pid int; StartedAt, UpdatedAt time.Time; Detail map[string]any }`; `func Create(jobsHome string) (*Job, string /*dir*/, error)` (id `j_` + 12 hex); `func (j *Job) Emit(dir string, typ, stage string, detail map[string]any, evErr *EventError) (Event, error)` (appends `events.jsonl`, Seq monotonic per job); `func (j *Job) Snapshot(dir string) error` (write `job.json` via tmp+fsync+rename; record Pid/process-group id); `func LoadStatus(dir string) (*Job, []Event, error)`; `func MarkInterruptedIfDead(j *Job) bool` (pid dead + non-terminal ⇒ State=`failed`, code `INTERRUPTED`; pid reuse tolerated via start-time check `os.FindProcess` + `/proc/<pid>/stat` field 22 comparison, macOS fallback: skip check, trust mtime > 1s).

- [ ] **Step 1: Write failing tests** — `TestSeqMonotonic`, `TestSnapshotAtomicSwap`, `TestEventsToleratesTruncatedLastLine` (append half a JSON line, `LoadStatus` returns events before it, no error), `TestDeadPidMarkedInterrupted` (write pid of a reaped process, non-terminal state ⇒ INTERRUPTED), `TestTerminalJobNotMarked`.
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement** — events append with `\n` + fsync; snapshot tmp file in same dir; state helper `IsTerminal(state)`.
- [ ] **Step 4: Run tests** — PASS.
- [ ] **Step 5: Commit** — `feat(engine): job state machine with crash-tolerant event log`

### Task 4: `internal/repo` — clone / link / pull / list / remove

**Files:**
- Create: `engine/internal/repo/repo.go`
- Test: `engine/internal/repo/repo_test.go`

**Interfaces:**
- Consumes: none (pure git + fs).
- Produces: `type Entry struct { Name, Path, URL, Branch, Type string; HeadCommit string }` (`Type` ∈ `remote|linked|dir`); `type SourceKind string` (`KindURL`, `KindLocalGit`, `KindLocalDir`); `func Classify(source string) SourceKind`; `func Clone(scvHome, url, branch string) (*Entry, error)` (branch default `main`, name from `git_op.py extract_repo_name` rules: last path segment, `.git` stripped); `func Link(scvHome, path, name string) (*Entry, error)` (absolute symlink `repos/<name>` → path); `func Pull(scvHome, name string) (*Entry, error)`; `func List(scvHome string) ([]Entry, error)` (symlink ⇒ `linked`, git remote present ⇒ `remote`); `func Remove(scvHome, name string, withAnalysis bool) error` (refuses unknown names; never touches symlink targets; deletes `analysis/<name>` only with `withAnalysis`); `func HeadCommit(path string) (string, error)`.

- [ ] **Step 1: Write failing tests** — `TestClassify`, `TestCloneAndList`, `TestLinkAndList` (uses `git init` fixture in `t.TempDir()`), `TestLinkNameCollisionRefuses` (name exists as clone dir ⇒ error, clone dir untouched), `TestNamesWithSpacesRoundTrip` (repo name `my proj`, quoted paths survive clone→list→remove), `TestRemoveLinkedLeavesTarget`.
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement** — `os/exec` git commands with `PATH` inheritance; errors wrapped as `CLONE_FAILED`/`PATH_NOT_FOUND` where the pipeline maps them (mapping itself in Task 6).
- [ ] **Step 4: Run tests** — PASS.
- [ ] **Step 5: Commit** — `feat(engine): repo management with linked local sources`

### Task 5: perceive.py — three additive changes (spec §12)

**Files:**
- Modify: `perception/perceive.py` (argparse `--version`; tier gate binary-OR-toolchain; tier-start log lines)
- Test: `perception/tests/test_perceive.py` (append tests; do not edit existing ones)

**Interfaces:**
- Produces: `perceive.py --version` → stdout `{"scv_version": "<CHANGELOG version>", "contract": "1.0"}`, exit 0; tier6/tier8 execution gate becomes "prebuilt binary present (fresh by mtime) OR `probe_go()` OK" — when prebuilt and fresh, `go build` is skipped; before each tier's work a `tierN start` line is logged via existing `log_line` (tiers 1, 2, 3, 6, 8).

- [ ] **Step 1: Write failing tests** (new tests appended; existing ones untouched — the existing suite builds repos inline with `MINIMAL_PATH` and never reaches tier 6, so a **new** e2e harness is required, not an extension): `test_version_flag_contract` (subprocess `--version`, JSON-parse stdout, assert `contract == "1.0"` and `scv_version` non-empty — parse version as the first `## [vX.Y.Z]` heading in CHANGELOG.md, NOT the `## [Unreleased]` head); `test_tier_start_lines_present` (new e2e: fake `dist/loader/perception-loader` executable with fresh mtime + `SCV_PERCEPTION_ANALYZER` fake analyzer + `perception/fixture/repos/golang-complex` repo copy; assert `perception.log` contains `tier1 start`, `tier3 start`, `tier6 start`); `test_prebuilt_binary_skips_go_probe` (same fake-loader setup, PATH without `go`, assert the loader tier still ran). Use the existing seams `SCV_PERCEPTION_DIST`/`SCV_PERCEPTION_ANALYZER`; mirror the inline-repo-builder style of the current tests.
- Also in this task: wrap the existing `go build` calls in `build_loader`/`build_graphmcp` with a dist build lock (lockfile `dist/.build.lock`, `O_EXCL` create + stale takeover by mtime > 10min) — spec §10's build-race flock; additive, tested by `test_concurrent_build_lock_serializes` (two threads through `build_loader` with a fake `go` shim that asserts non-overlap via timestamp file).
- [ ] **Step 2: Run** `cd perception && uv run --project analyzer python -m pytest tests/test_perceive.py -v` — new tests FAIL, all existing PASS.
- [ ] **Step 3: Implement** — keep additions minimal: a `--version` argparse action that prints and exits before tier work; a `prebuilt(path)` helper (exists + mtime newer than sources' newest .go file is a nice-to-have — presence check is the contract); `tier start` lines via existing `log_line` at each tier entry.
- [ ] **Step 4: Run** full perception suite — ALL PASS (regression gate: `uv run --project analyzer python -m pytest tests/ -v`, zero failures).
- [ ] **Step 5: Commit** — `feat(perception): --version contract, prebuilt-binary tier gate, tier-start log lines`

### Task 6: `internal/run` — pipeline (checking gate → perceive exec → events → finalizing)

**Files:**
- Create: `engine/internal/run/run.go`, `engine/internal/run/perceive.go`, `engine/internal/run/registry.go`, `engine/internal/run/metadata.go`
- Test: `engine/internal/run/run_test.go` (uses a **stub perceive.py** script for unit tests; real fixtures are Task 13)

**Interfaces:**
- Consumes: `config.Config`, `env.Env`/`env.Gate` (T2), `job.*` (T3), `repo.*` (T4).
- Produces: `type RunRequest struct { Source, Project, OutputDir, Lang string; SkipUnchanged, AllowDegraded, Force bool }`; `type RunResult struct { Perception map[string]any; OutputDir string; Docs []string; SkippedReason string }`; `func Run(ctx context.Context, cfg *config.Config, req RunRequest, sink func(job.Event)) (*RunResult, error)`; `func ParsePerceiveJSON(line string) (map[string]any, error)` (lenient: unknown fields fine); `func AppendAnalysisDir(registryPath, dir string) error` (`analysis-dirs.json`, dedup); `func WriteMetadata(analysisDir, repoPath, commit string) error` (keys `last_analyzed_commit/last_analyzed_at/created_at/repo_path/scv_version` — port of `scv_util.py write-metadata`, additive); `func ShouldSkip(analysisDir, commit string, docGen bool) bool` (commit match AND `structure.json` exists AND (not docGen OR all four docs exist)).

- Perceive path resolution (unit-test seam): `<perception root>/perceive.py`, overridable by env `SCV_ENGINE_TEST_PERCEIVE` (points at a stub script) — T10/T12 tests rely on this override; document it in the function's doc comment.
- Perceive argv mapping (perceive.py accepts only `--source local|remote`, `perception/perceive.py:470`): `KindURL → --source remote`; `KindLocalGit|KindLocalDir → --source local`. Child env: inherit + `SCV_PERCEPTION_CONFIG=<cfg.Path>` (perceive defaults to `~/.scv/config.json`, which diverges under `--config`/`--scv-home` overrides).
- Stage flow (spec §7 sequences, exact): `checking` (env.Gate; blocking ⇒ emit `stage_failed` `ENV_INCOMPLETE` with `detail.missing` = ALL blocking items, return error; degrading ⇒ one `degraded` event with `detail.missing`) → `preparing` (Classify + Clone/Link/HeadCommit; `CLONE_FAILED`/`PATH_NOT_FOUND`) → skip-check (`pending→skipped` path) → exec perceive with the argv/env mapping above; tail `perception.log` tier lines → `detecting/analyzing/grading/query_tier` events; artifact existence as belt-and-braces for tiers 6/8 → parse stdout JSON (`TestPerceiveGarbageStdoutFailsAnalyzerFailed`: not-JSON ⇒ `stage_failed` `ANALYZER_FAILED` with 2000-char log tail) → docgen (T7 hook) → `finalizing` (WriteMetadata + AppendAnalysisDir) → `done`.
- `--allow-degraded` demotes uv-blocking to degrading (structure skipped: perceive still runs, its `uv-missing` status folds into the gate item, no duplicate event).

- [ ] **Step 1: Write failing tests** — `TestGateEnvIncompleteListsAll` (fake env missing uv+root ⇒ one event, two items), `TestFullSequenceGolden` (stub perceive emitting tier lines; assert exact event sequence incl. `job_result`), `TestNotFrameworkSequence`, `TestGoMissingReducedSequence`, `TestSkipUnchangedWithMissingDocs`, `TestPerceiveGarbageStdoutFailsAnalyzerFailed`, `TestMetadataKeysPort`.
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement** — log tailer: poll file every 200ms, parse lines starting `tier`, map `tier1→detecting`, `tier2|tier3→analyzing`, `tier6→grading`, `tier8→query_tier`; start/finish from `start` suffix vs completion lines (perceive task 5 lines); ctx cancellation ⇒ kill child group.
- [ ] **Step 4: Run** `make engine-test` — PASS.
- [ ] **Step 5: Commit** — `feat(engine): run pipeline with unified gate and tier event mapping`

### Task 7: `internal/docgen` — command mode

**Files:**
- Create: `engine/internal/docgen/docgen.go`
- Test: `engine/internal/docgen/docgen_test.go`, `engine/tests/fake-llm.sh` (test fixture: reads `$SCV_DOC`, cats `testdata/<doc>.md`, exit 0; `fail`/`hang` modes via `$SCV_FAKE_MODE`)

**Interfaces:**
- Consumes: `job.Event` sink (T3).
- Produces: `type Generator struct { Command []string; Timeout time.Duration; Retry int; AssetsRoot, Lang string }`; asset resolution order: `SCV_DOCGEN_ASSETS` env → `AssetsRoot` (`<perception root>/docgen/`) → **repo-checkout fallback** `<repo root>/agents/<lang>/project-analyzer.md` + `<repo root>/skills/<lang>/references/templates/` (lang `zh` → dir `zh-cn`; checkout = the directory containing `perception/`, resolved upward from perception root — spec §8's "repo checkout works" clause); `func (g *Generator) Generate(ctx context.Context, repoPath, analysisDir, structureJSONPath string, summary map[string]any, sink func(job.Event)) ([]string, error)` — 4 calls in order readme→summary→architecture→file_index; env `SCV_DOC`, `SCV_REPOSITORY`, `SCV_ANALYSIS_DIR`, `SCV_STRUCTURE_JSON`, `SCV_TEMPLATE`, `SCV_LANG`; stdin = prompt (project-analyzer body + summary + template); stdout → `<analysisDir>/<Canonical name>`; exit≠0 ⇒ `DOCGEN_FAILED`; timeout ⇒ `STAGE_TIMEOUT`; retry per doc with `detail.attempt`.

- [ ] **Step 1: Write failing tests** — `TestFourDocsGenerated`, `TestEnvContractAndStdin` (fake-llm asserts env vars set + stdin contains template), `TestFailureModeDocgenFailed` (stderr tail in LogTail), `TestTimeoutKillsAndReports`, `TestRetryAttemptsEmitted` (`SCV_FAKE_MODE=fail-once`), `TestAssetsLangMapping` (`zh` → `zh-cn` dir).
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement** — `exec.CommandContext` with process-group setup shared with T9; canonical names `readme→README.md` etc.; testdata assets committed under `engine/internal/docgen/testdata/docgen/` mirroring the pack layout.
- [ ] **Step 4: Run** — PASS.
- [ ] **Step 5: Commit** — `feat(engine): command-mode doc generation`

### Task 8: `internal/query` — vectra in-process

**Files:**
- Create: `engine/internal/query/query.go`
- Test: `engine/internal/query/query_test.go` (build a `.vg` via `perception/loader` in test setup — `go run` from the perception module, or commit a prebuilt fixture `.vg` generated by `make` helper; prefer the latter: `engine/internal/query/testdata/demo.vg`, regeneration note in file comment)

**Interfaces:**
- Consumes: `analysis-dirs.json` registry (T6).
- Produces: `type Store struct { /* opaque */ }`; `func Open(registryPath, defaultRoot string) (*Store, error)` (default root `~/.scv/analysis`; union of registry dirs; lazy per-repo load, mutex-guarded — mirror `perception/graphmcp/main.go:39-107` store pattern); `func (s *Store) Repos() ([]map[string]any, error)`; `func (s *Store) Cypher(repo, q string, limit int) (any, error)`; `func (s *Store) Node(repo, key string) (map[string]any, error)`; `func (s *Store) Impact(repo, key, dir string, labels []string, limit int) (any, error)` (dir `out|in|both`; exclude start node); `func (s *Store) Refresh()`. Vectra call surface: copy exactly from `perception/graphmcp/main.go` handlers (LoadFile/Cypher/NodeByKey/Reachable) — read it before implementing.

- [ ] **Step 1: Write failing tests** — `TestReposListsFixtureVg`, `TestCypherCountMatchesNodes`, `TestImpactExcludesStart`, `TestRegistryUnionTwoRoots`, `TestUnknownRepoErrors`.
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement** — thin wrappers; no new graph logic.
- [ ] **Step 4: Run** — PASS.
- [ ] **Step 5: Commit** — `feat(engine): in-process vectra query store`

### Task 9: locking + cancel (process groups)

**Files:**
- Create: `engine/internal/run/lock.go`, `engine/internal/run/cancel.go`
- Test: `engine/internal/run/lock_test.go`, `engine/internal/run/cancel_test.go`

**Interfaces:**
- Consumes: `job.Job` (pid recorded at Run start).
- Produces: `func AcquireRepoLock(analysisDir string) (release func(), err error)` (flock `<analysisDir>/.scv-lock`, err typed `WORKFLOW_LOCKED`); `func SetProcessGroup()` (call in Run start; `syscall.Setpgid(0,0)`); `func Cancel(dir string, j *job.Job) error` (SIGTERM `-pgid`; wait ≤10s then SIGKILL; state → `cancelled` after group exit; terminal job ⇒ `WORKFLOW_NOT_CANCELLABLE`).

- [ ] **Step 1: Write failing tests** — `TestSecondRunWorkflowsLocked`, `TestCancelKillsProcessGroupTree` (spawn stub perceive that spawns a sleeper child; cancel; assert no process from the group remains via `syscall.Kill(-pgid, 0)`), `TestCancelTerminalJobRefuses`, `TestCancelSetsCancelledState`.
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement** — flock via `syscall.Flock` on opened lock file; cancel waits on state file transition with poll.
- [ ] **Step 4: Run** — PASS.
- [ ] **Step 5: Commit** — `feat(engine): repo locking and process-group cancellation`

### Task 10: CLI `cmd/scv`

**Files:**
- Create: `engine/cmd/scv/main.go`, `engine/cmd/scv/root.go`, `engine/cmd/scv/run.go`, `engine/cmd/scv/repo.go`, `engine/cmd/scv/jobs.go`, `engine/cmd/scv/query.go`, `engine/cmd/scv/env.go`
- Test: `engine/cmd/scv/cli_test.go` (exec the built binary)

**Interfaces:**
- Consumes: all internal packages; cobra.
- Produces: the CLI surface of spec §6 verbatim: global flags `--scv-home/--config/--json/--quiet`; `--version`; `env`; `repo clone|link|pull|list|remove(--confirm, --with-analysis)`; `run` with `--project/--output-dir/--lang/--source/--skip-unchanged/--force/--allow-degraded`; `jobs list|status|events(--since)|cancel`; `query repos|cypher|node|impact(--dir/--labels/--limit)|refresh`. `run` stdout = NDJSON events + terminal `job_result` line; stderr = diagnostics. Sync verbs print one JSON object with `--json`.

- [ ] **Step 1: Write failing tests** — `TestVersionHandshakeOutput` (engine `--version` prints own + supported contract range), `TestVersionHandshakeMatrix` (subtests: perceive contract match ⇒ run proceeds; major mismatch ⇒ exit non-0, error `VERSION_INCOMPATIBLE`; `--version` unsupported by perceive ⇒ run proceeds with `degraded` `contract-unknown`), `TestRunNDJSONContract` (against stub perceive via env var `SCV_ENGINE_TEST_PERCEIVE` pointing at the stub; assert last line `"type":"job_result"` and every line parses), `TestJobsEventsSince`, `TestRepoLinkViaCLI`, `TestRemoveRequiresConfirm`.
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement** — cobra commands; run wires Run() with sink printing NDJSON; version handshake: exec perceive `--version`, compare contract major, `VERSION_INCOMPATIBLE` unless `--force` (absent ⇒ lenient + `degraded` `contract-unknown`).
- [ ] **Step 4: Run** `make engine-test` — PASS.
- [ ] **Step 5: Commit** — `feat(engine): scv CLI with NDJSON run contract`

### Task 11: `make pack` + dist platform selection + docgen assets mirror

**Files:**
- Create: `scripts/pack.sh`, `engine/internal/env/distlayout.go` (selects `dist/<platform>/{loader,graphmcp}` pair into flat `dist/loader/perception-loader` + `dist/graphmcp/scv-graph-mcp` via symlink)
- Modify: `Makefile` (root) — target `pack`
- Test: `engine/internal/env/distlayout_test.go`, `scripts/pack_test.sh` (smoke: pack into tmpdir, tar lists whitelist; run only on dev machines, guarded by `SCV_PACK_TEST=1`)

**Interfaces:**
- Consumes: spec §11 whitelist.
- Produces: `perception-runtime-<ver>.tar.gz` (perceive.py, analyzer/ sans `.venv`, loader/, graphmcp/ sources, adapters/*/MANIFEST.json, `docgen/{en,zh-cn}/` mirrored from `agents/*/project-analyzer.md` + `skills/*/references/templates/`, `dist/<plat>/` prebuilt pairs for linux/darwin × amd64/arm64 built `CGO_ENABLED=0`) and `scv-<ver>-<os>-<arch>` binaries; `env.EnsureDistLayout(distRoot string) error` — **flock-guarded** (reuse the `O_EXCL` + stale-takeover lockfile pattern from Task 5; covers the platform-pair symlink swap, spec §11 "locked like §10"). pack.sh **also writes the `docgen/{en,zh-cn}/` mirror in-checkout at `perception/docgen/`** so a dev checkout satisfies Task 13 without unpacking a tarball. Version = first `## [vX.Y.Z]` heading in CHANGELOG.md (head section is `## [Unreleased]`).

- [ ] **Step 1: Write failing tests** — `TestEnsureDistLayoutSelectsPlatform` (fake `dist/linux-amd64/...` tree ⇒ flat symlinks created), `TestPackWhitelist` (smoke: generated tar contains `docgen/en/`, excludes `.venv`, excludes `adapters/springboot/src`).
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement** — pack.sh: `ver` from CHANGELOG head; `go build` four-platform loop; tar with explicit path list (whitelist enforced by construction); wire into root Makefile `pack: engine-build`.
- [ ] **Step 4: Run** `SCV_PACK_TEST=1 bash scripts/pack_test.sh` — PASS.
- [ ] **Step 5: Commit** — `feat(build): runtime pack with prebuilt dist and docgen assets`

### Task 12: `supervisor` package + Fiber example

**Files:**
- Create: `engine/supervisor/supervisor.go`, `engine/examples/fiber/main.go`
- Test: `engine/supervisor/supervisor_test.go` (external-package test: imports as a host would)

**Interfaces:**
- Consumes: internal packages via a narrow façade.
- Produces: `func New(ctx context.Context, cfg config.Config) (*Supervisor, error)`; `func (s *Supervisor) Env(ctx context.Context) (*env.Env, error)`; `func (s *Supervisor) Run(ctx context.Context, req run.RunRequest) (*JobHandle, error)`; `type JobHandle struct{ ID string }` with `(*JobHandle) Status(ctx) (*job.Job, error)`, `Events(ctx, since int) ([]job.Event, error)`, `Cancel(ctx) error`, `Wait(ctx) (*run.RunResult, error)`; `func (s *Supervisor) Query() (*query.Store, error)`. Example: Fiber app exposing `POST /api/scv/analyze`, `GET /api/scv/jobs/:id`, `GET /api/scv/jobs/:id/events` (SSE bridge), `GET /api/scv/env` (~50 lines, no product claims).

- [ ] **Step 1: Write failing tests** — `TestSupervisorRunEndToEnd` (stub perceive via injected path option; Run→Wait→Status sequence), `TestSupervisorEnvAndQuery`.
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement** — façade only; no logic; example compiles (`go build ./examples/fiber` with gofiber/fiber/v2 dependency, pinned in go.mod).
- [ ] **Step 4: Run** `make engine-test` — PASS.
- [ ] **Step 5: Commit** — `feat(engine): supervisor package and fiber example`

### Task 13: Integration golden tests + coverage gate hardening

**Files:**
- Create: `engine/tests/integration_test.go` (build-tagged `//go:build integration`), `engine/tests/README.md`
- Modify: `engine/Makefile` (`engine-test-integration` target: sets `SCV_TEST_SCV_HOME` to tmp, real perceive path, real fixtures; coverage gate stays on unit suite)

**Interfaces:**
- Consumes: everything; real `perception/perceive.py` against `perception/fixture/` repos (fixture path from spec §14; discover via `../../perception/fixture/repos` — implementer verifies actual fixture layout first and records it in tests/README.md).

- [ ] **Step 1: Write failing tests** (table-driven, one subtest per §7 path): `TestIntegrationFullRun` (golang fixture; full sequence + `structure.vg` + `scv query cypher` count vs structure.json), `TestIntegrationNotFramework` (ruby/plain fixture per availability), `TestIntegrationGoMissingReduced` (PATH sans `go`, empty dist ⇒ degraded `go-missing`), `TestIntegrationAllowDegradedUvMissing` (PATH sans uv ⇒ upfront degraded + reduced sequence), `TestIntegrationDocgenFakeLLM` (fake-llm.sh ⇒ four docs, per-doc events), `TestIntegrationCancelMidAnalyzing`, `TestIntegrationSkipSemantics` (structure-only then llm-configured re-run proceeds).
- [ ] **Step 2: Run** `make engine-test-integration` — FAIL (no tests yet wired).
- [ ] **Step 3: Implement** — test harness: build binary once (`go build -o tmp/scv ./cmd/scv`), per-test tmp `SCV_HOME`, fixture copy (fixtures are read-only ⇒ copy to tmp, never mutate; **`perception/fixture/repos/*` are not git repos — the harness `git init && git commit`s each tmp copy** so `preparing`/HeadCommit/skip semantics have a real HEAD; the full-run path therefore exercises the local-git → `--source local` mapping; a `repo link` + re-run case covers the linked path), `PATH` shims via helper dir.
- [ ] **Step 4: Run unit + integration + perception suites** — ALL PASS: `make engine-test && make engine-test-integration && cd perception && uv run --project analyzer python -m pytest tests/ -v`.
- [ ] **Step 5: Commit** — `test(engine): integration golden suite over perception fixtures`

---

## Execution notes

- Task order is a dependency chain (1→4 foundation, 5 parallel-safe, 6 needs 1–4, 7–9 after 6's interfaces, 10 needs all, 11–12 parallel-safe, 13 last). Tasks 5 and 11/12 can run concurrently with others by different implementers.
- Every task's commit must leave `make engine-test` green (task 1 provides the target from the start with an initially trivially-passing gate; the gate starts enforcing at 80% from task 2 onward).
- The plan deliberately stubs perceive.py at unit level (`SCV_ENGINE_TEST_PERCEIVE`) so engine tests never depend on uv provisioning in CI; only Task 13 (opt-in build tag) and Task 5 touch the real Python chain.
