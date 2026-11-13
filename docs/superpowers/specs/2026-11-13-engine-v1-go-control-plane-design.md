# SCV Engine v1 — Go Control Plane — Design Spec

- Date: 2026-11-13 (rev 2)
- Status: audited — builder check round 1 incorporated (8 findings: 3 author decisions taken, 5 settled as spec calls, listed in §15)
- Conversation basis: Go host integration research (`docs/research/2026-11-13-go-host-integration.md`) + author alignment session (same day). Every "approaches" decision below was picked by the author in that session or in the audit follow-up.
- Supersedes: nothing. Adds a new control plane; the runtime path of Perception v2/v3 (2026-10-14, 2026-10-20 specs) is unchanged except the three additive Python changes in §12.

## 1. Motivation & positioning

SCV today is reachable only through a Claude Code session: skills drive LLM steps, `perception/perceive.py` drives the deterministic analysis chain. Platforms that want SCV at the code level (a Fiber service, a Rust tool, a Python pipeline) have no entry point.

Engine v1 adds a **Go control plane**: one statically linked binary, `scv`, that owns repo management, workflow orchestration, job state, environment discovery, structure-graph queries, and LLM-driven doc generation — while the Python/uv analysis layer (`perception/`) stays as it is, executed as a subprocess (plus the three additive changes in §12).

Positioning, one line: **the control plane is Go and ships as a binary; the data plane stays Python/uv and ships as a source tree; LLM access is injected by the host as a command, never owned by SCV.**

Why now: the query layer is already Go (`vectra` v0.14.4, `graphmcp`), perceive.py's stdout contract is spec-frozen, and state is already on disk — the control plane is a thin owner of pieces that exist, not a rewrite.

## 2. What stays unchanged

- `perception/perceive.py` CLI contract: single-line stdout JSON, exit 0 always, tier ladder, `SCV_PERCEPTION_DIST/CONFIG/ANALYZER` hooks. The only Python changes in this spec are the three additive items in §12.
- `perception/analyzer/` (uv project + uv.lock), `loader/`, `graphmcp/` sources and build logic.
- The skills layer (`skills/`, `agents/`) — Claude Code keeps working untouched. `skills/scripts/batch_manager.py` is not migrated in v1 (§13 roadmap).
- Spoon adapter positioning: dev-time cross-validation engine only. Runtime Spring Boot extraction stays tree-sitter (`perception/analyzer/extensions/springboot.py`). The Java detection matrix (research report §11) is informational, not a tier.
- codebones: engine v1 does not support deep analysis (author decision, audit Q3). `deep_analysis_enabled=true` in config ⇒ engine emits a `degraded` event `deep-analysis-unsupported` and omits deep-analysis prompt sections; v1.x adds PATH discovery (same pattern as uv).
- Security invariants: nothing inside an analyzed repo is ever executed; remote and local repos are treated identically.

## 3. Approaches considered (all rejected/picked in the author session + audit follow-up)

| Approach | Verdict | Why |
|---|---|---|
| Python sidecar daemon (FastAPI + SSE) | rejected | extra daemon lifecycle ops for every host; its only edge (real-time streaming) requires changing perceive.py's output model wholesale |
| Host as MCP client of an extended graphmcp | rejected | MCP tool calls are synchronous request/response; multi-minute orchestration needs a job/polling shim on top; queries gain a network hop |
| Per-language thin SDKs (each exec's perceive.py itself) | rejected | N reimplementations of job lifecycle, locking, and state parsing; every new ecosystem pays full price again |
| Go control-plane binary, **one-shot only**, importable as a Go package | **picked** | one implementation of workflow management; every language already knows how to exec a binary and parse JSON; state on disk makes polling trivial; Go hosts import the same package and skip exec |
| Built-in LLM client (openai-compat/anthropic in SCV) | rejected for v1 | API keys would have to enter and be custody-managed by SCV (process lists, state files, logs); provider matrix to maintain |
| **Host-injected LLM command** (SCV execs a configured command per document) | **picked** | keys never enter SCV; hosts keep their own model/auth; SCV stays a pure orchestrator |
| Serve mode (`scv serve` HTTP+SSE) | deferred, not v1 | hosts own their HTTP surface; stage-level progress makes 2s polling lossless; an HTTP wrapper can be added to the supervisor package later without contract changes |
| No-Go hosts get `.vg` by engine exec'ing the prebuilt loader itself | rejected (audit Q1) | forks orchestration into the engine; grading event semantics would need redefinition and cycle data re-sourced from loader stdout |
| No-Go hosts get `.vg` via **small additive perceive.py change** (binary-OR-toolchain gate) | **picked** (audit Q1) | keeps the single orchestration path in perceive.py; additive and backward compatible; Python delta grows from one item to three (§12) — accepted by author |
| Local git source: clone to `~/.scv/repos` (run.md semantics) | rejected (audit Q2) | clones a second copy of code the host already has on disk |
| Local git source: **symlink into `~/.scv/repos`**, analyze the linked worktree | **picked** (audit Q2) | `~/.scv/repos/<name>` → symlink to the original path; analysis sees the working tree as-is (uncommitted changes included — documented semantics); `scv repo list` shows one namespace with `type: linked \| remote` |
| codebones bundled into the pack / embedded in the binary | rejected (audit Q3) | third-party Rust binary: bundling takes on its version/license/platform matrix; PATH discovery (v1.x) matches the uv pattern and keeps SCV unburdened |

## 4. Architecture

```
┌─────────────────────────── scv binary (Go, one-shot) ───────────────────────────┐
│  repo mgmt        clone (remote) / link (local) / pull / list / remove           │
│  run supervisor   exec python3 perceive.py → tail perception.log → tier events;   │
│                   exec dist loader artifacts; flock per analysis dir             │
│  jobs             ~/.scv/jobs/<id>/{job.json, events.jsonl, pid}; NDJSON stdout   │
│  doc generation   exec configured LLM command 4× (README/SUMMARY/ARCH/FILE_INDEX)│
│                   stdin=prompt, stdout=markdown, exit code = completion signal   │
│  env discovery    uv / python3 / git / go(optional) / java(informational) + roots │
│  query            vectra v0.14.4 in-process over registered analysis dirs         │
│                   (graphmcp binary stays for MCP/HTTP consumers, unchanged)      │
└──────────────────────────────────────────────────────────────────────────────────┘
        │ exec + files (control plane ⇄ data plane)        │ import (Go hosts)
        ▼
  python3 perceive.py → uv run analyzer → structure.json → dist loader → structure.vg
```

Two consumers, same package:

- Non-Go hosts: exec `scv` subcommands, parse JSON. The host backgrounds long runs itself (every language can background a child process); `scv` never daemonizes.
- Go hosts (Fiber component): import the supervisor package; no exec, no extra process. The Fiber component exposes REST/SSE by bridging job state — the serving layer belongs to the host, not SCV.

## 5. Repository layout (delta)

```
engine/                          # NEW — Go module (github.com/projanvil/scv/engine)
├── go.mod                       # go 1.27; vectra v0.14.4 (same pin as graphmcp)
├── cmd/scv/main.go              # thin CLI shell over the packages below
├── internal/
│   ├── env/                     # discovery: uv, python3, git, go, java, roots
│   ├── repo/                    # clone/link/pull/list/remove (port of git_op.py semantics)
│   ├── job/                     # job registry, state machine, events.jsonl, flock, cancel
│   ├── run/                     # perceive.py exec, log tail → events, artifact checks
│   ├── docgen/                  # LLM command mode: prompt assembly, 4× exec, validation
│   ├── query/                   # vectra in-process: repos/cypher/node/impact/refresh
│   └── config/                  # ~/.scv/config.json load/validate + engine/llm keys
├── supervisor/                  # EXPORTED package — the Fiber-component entry point
│   └── supervisor.go            # Run(ctx, RunRequest, opts) (*Job, error), Env(), Query()
└── tests/                       # integration: fixture repos + fake-llm.sh golden runs
Makefile                         # engine build/test + `make pack` (§11)
```

`supervisor/` is deliberately outside `internal/`: it is the contract for Go-host embedding. Everything else is private.

## 6. CLI surface

Global flags: `--scv-home` (default `~/.scv`), `--config` (default `<scv-home>/config.json`), `--json` (machine output), `--quiet`.

```
scv --version                          # engine version + contract versions it supports
scv env                                # environment snapshot + diagnostics (JSON)
scv repo clone <url> [--branch b]      # sync; JSON result on stdout
scv repo link <path> [--name NAME]     # sync; symlink a local repo into ~/.scv/repos
scv repo pull <name>                   # sync (remote repos only)
scv repo list                          # sync; entries carry type: remote|linked
scv repo remove <name> --confirm       # refuses without --confirm
scv run <source>
    [--project NAME] [--output-dir DIR] [--lang zh|en]
    [--source local|remote] [--skip-unchanged] [--force]
    [--allow-degraded]
    # <source> = local path (git or non-git) or git URL
scv jobs list
scv jobs status <id>
scv jobs events <id> [--since SEQ]
scv jobs cancel <id>
scv query repos
scv query cypher <repo> <query> [--limit N]
scv query node <repo> <key>
scv query impact <repo> <key> [--dir out|in|both] [--labels l1,l2] [--limit N]
scv query refresh
```

Source handling (audit Q2 decision):

- **git URL** ⇒ `git clone` into `~/.scv/repos/<name>` (branch defaults to `main`, port of git_op.py).
- **local git repo** ⇒ `~/.scv/repos/<name>` becomes a **symlink** to the absolute path; the analysis reads the worktree as-is, uncommitted changes included. This is documented, intended semantics — a run reflects the tree at run time. `--project NAME` overrides the entry name (verbatim, matching run.md's basename convention).
- **local non-git directory** ⇒ analyzed directly, no repos entry, commit metadata null (run.md Step 4.5 semantics).

**`scv run` output contract:** stdout is NDJSON — one event object per line, identical schema to `events.jsonl` (§7), ending with one terminal `job_result` object. stderr carries human diagnostics and passthrough child-process logs. A host that backgrounds `scv run` can tail stdout for live progress *or* ignore it and poll `scv jobs status`; both are first-class.

Synchronous verbs (`repo *`, `env`, `query *`) print a single JSON object (or plain text without `--json`) and exit 0/non-0.

`scv repo remove` deletes only the `~/.scv/repos/<name>` entry (clone or symlink); it prints a note that `~/.scv/analysis/<name>` remains and offers `--with-analysis` to remove it too (spec call, audit Q8). Removing a linked entry never touches the target directory.

## 7. Job state model

Job home: `~/.scv/jobs/<id>/` — `job.json` (current snapshot), `events.jsonl` (append-only), `pid` (process group id while running), `llm/` (captured doc outputs). Analysis artifacts land in `~/.scv/analysis/<project>/` (perceive contract) or `--output-dir`; every run's analysis dir is appended to `<scv-home>/analysis-dirs.json` so `scv query` aggregates all registered roots (spec call, audit Q6 — graphmcp's hardcoded `~/.scv/analysis` root stays untouched).

States:

```
pending → checking → preparing → detecting → analyzing → grading → query_tier → generating → finalizing → done
                                                       (any running state) → failed | cancelled
pending → skipped   (--skip-unchanged hit)
```

**Unified dependency gate (`checking`, first stage)** — author decision post-audit: all environment/dependency missing conditions (uv, go/dist, docgen config, …) are one mechanism, evaluated once before any work starts, instead of surfacing one at a time mid-run. The engine runs the full env discovery (the same model as `scv env`, §9) against the run's mode and classifies every item with a uniform schema:

```json
{"item":"uv","status":"missing","severity":"blocking","capability":"structure","hint":"install uv — https://docs.astral.sh/uv/"}
```

- **blocking-missing** (python3, git when repo verbs are needed, perception root, uv when structure output is expected): the job fails at `checking` with error code `ENV_INCOMPLETE` and `detail.missing[]` listing **every** missing blocking item at once — one failure, complete repair list, never one-at-a-time. `--allow-degraded` demotes uv to degrading (structure tier skipped, run proceeds).
- **degrading-missing** (go toolchain AND dist binaries both absent → query tier off; `llm` unconfigured → docgen off; `deep_analysis_enabled` → unsupported): ONE upfront `degraded` event carrying `detail.missing[]` (all items, same schema); the corresponding stages are simply absent from the rest of the sequence.

Runtime-only failures (analyzer-failed, clone-failed, DOCGEN_FAILED, timeouts) are not env conditions — they stay per-stage `stage_failed` events where they happen.

Event sourcing (audit Q4 resolution): perceive.py gains additive `tier-start` log lines (§12), so every stage transition has a deterministic source:

- `checking` — engine-owned: the unified gate above
- `preparing` start/finish — engine-owned (clone/link + `get-commit-info` port)
- `detecting/analyzing/grading/query_tier` — `tier1/tier2-3/tier6/tier8` start+finish lines from `perception.log`; artifact existence (`structure.json`, `structure.vg`) is the belt-and-braces check for tiers that historically logged nothing on success
- `generating` — engine-owned per-document events (§8)
- `finalizing` — engine-owned (metadata port)

Per-path frozen sequences (golden-test basis):

- **full run**: every stage in order
- **blocking env missing**: `pending → checking → failed(ENV_INCOMPLETE)` — nothing else runs; all missing items listed in one error
- **degrading env missing**: checking emits the upfront `degraded` event, then the reduced sequence runs — e.g. query tier off: `checking → preparing → detecting → analyzing → finalizing → done` (no grading/query_tier)
- **not-framework**: preparing → detecting (finish carries `detail.perception_status="not-framework"`) → finalizing → done; no analyzing/grading events
- **uv-missing with `--allow-degraded`**: structure tier skipped — `checking → preparing → detecting → finalizing → done`; perceive's in-band `uv-missing` reason is folded into the gate's diagnostics item, not duplicated as a second event
- **skipped**: `pending → skipped` with `detail.reason="commit-unchanged"`; no other stages run

`--skip-unchanged` semantics (spec call, audit Q7): skip only when head commit matches **and** the artifacts expected by the current mode exist (`structure.json`; plus the four documents when `llm` is configured). Otherwise re-run — a structure-only past run never traps a later docgen-configured run in `skipped`.

Event object:

```json
{"id":"j_...", "seq":12, "ts":"2026-11-13T09:00:00Z", "type":"stage_started|stage_finished|stage_failed|degraded|artifact_produced|job_result",
 "stage":"analyzing", "detail":{...per-stage payload...},
 "error":{"code":"ANALYZER_FAILED","message":"...","retryable":true,"log_tail":"<=2000 chars"}}
```

Polling contract: `jobs status` returns the `job.json` snapshot; `jobs events --since SEQ` returns events after that sequence number. Env-dependent degradation (uv/go+dist/docgen/deep-analysis) is reported once, upfront, by the checking gate (§7) — it is an event, never a mid-run surprise. Runtime failures keep the perceive contract (`exit 0 always`, reasons in-band) and map to `stage_failed` with `log_tail`.

Crash/interrupt semantics: `job.json` is fsynced on every state transition. On any `scv jobs` read, a job whose pid is dead and whose state is non-terminal is marked `failed` with code `INTERRUPTED` (recovery is: run again; skip-unchanged makes re-runs cheap; pid reuse tolerated by also checking start time).

## 8. Doc generation — command mode (the only LLM path in v1)

Prompt assets ship in the runtime pack (spec call, audit Q2): `docgen/{en,zh-cn}/` containing `project-analyzer.md` and the four templates, mirrored from `agents/` + `skills/*/references/templates/` by `make pack`. The engine resolves them from the perception root. `--lang zh` maps to `zh-cn` assets. (If the pack is absent on a dev machine, a repo checkout works — resolution order is perception root first.)

Config:

```json
{"llm": {"mode": "command",
         "command": ["claude", "-p"],
         "timeout_s": 600,
         "retry": 1}}
```

No `llm` section ⇒ the run is structure-only: `generating` is skipped, the checking gate reports it as a degrading-missing item (`docgen-unconfigured`) in its upfront `degraded` event, final state is `done`. This is the zero-configuration default.

Invocation — the engine calls the command **four times, once per document**, in order `README.md, SUMMARY.md, ARCHITECTURE.md, FILE_INDEX.md`:

- argv: exactly the configured command; document identity and context go via env, never argv (nothing sensitive in process lists):
  - `SCV_DOC` = `readme|summary|architecture|file_index`
  - `SCV_REPOSITORY` = analyzed repo path, `SCV_ANALYSIS_DIR` = artifact dir
  - `SCV_STRUCTURE_JSON`, `SCV_TEMPLATE` (the template file path for this doc), `SCV_LANG`
- stdin: the assembled prompt — project-analyzer instruction body + structure summary (from perceive stdout: counts/languages/cycles) + the template for this doc. Prompt assembly is a pure function over files; no deep-analysis sections in v1 (§2). If `deep_analysis_enabled=true`, the prompt is assembled identically and the run carries the `deep-analysis-unsupported` degraded event.
- stdout: raw markdown for that one document. The engine writes it to `<analysis_dir>/<canonical name>` (`readme`→`README.md`, `summary`→`SUMMARY.md`, `architecture`→`ARCHITECTURE.md`, `file_index`→`FILE_INDEX.md`).
- Completion signal: **process exit code.** 0 = success (output non-empty, else treated as failure); non-0 = `DOCGEN_FAILED` with stderr tail in `log_tail`; engine timeout = `STAGE_TIMEOUT`, kill process group. Retry: up to `llm.retry` times per document, each attempt a `stage_started` sub-event with `detail.attempt`.
- Per-document events give the host 4-cell progress granularity inside `generating` with zero cooperation required from the command. A cooperating command MAY also write `~/.scv/jobs/<id>/llm/progress.jsonl` (NDJSON `{"doc":..., "note":...}`), which the engine tails into the event stream — optional protocol, not required in v1.

Key custody: the engine never reads, stores, or logs API keys. Whatever auth the command needs is the host's business. Config values of the form `env:VAR` / `file:/path` are the only secret-shaped values allowed anywhere in config, and they are passed as references only.

A single-call variant (`--out-dir`, command writes all four files) was considered and deferred: four calls buy per-document progress and retry with no filesystem convention. If token cost proves real, add `llm.batch: true` in a minor version.

`~/.scv/index.md` is not written by engine v1 (index summaries require LLM-read README semantics that belong to the skill flow; revisited with batch in v1.x).

## 9. Environment discovery (`scv env`)

| Item | Resolution order | Missing ⇒ |
|---|---|---|
| scv home | `--scv-home` → `$SCV_HOME` → `~/.scv` | created on first use |
| perception root | `$SCV_PERCEPTION_ROOT` → `<scv home>/perception` | diagnostic `perception-missing` (run fails fast) |
| dist dir | `$SCV_PERCEPTION_DIST` → `<perception root>/dist` | see below |
| uv | `$SCV_UV` → PATH → `~/.local/bin/uv`, `/opt/homebrew/bin/uv` | diagnostic `uv-missing`; runs degrade to structure-skip |
| python3 | PATH (3.9+ sufficient — orchestrator only) | hard failure |
| git | PATH | hard failure for repo verbs |
| Go ≥1.27 | `go version` | **not required** when dist carries prebuilt binaries (§12 gate change); only a `go-missing` degradation when both toolchain and binaries are absent |
| java/JAVA_HOME | env → `/usr/libexec/java_home` → sdkman → JVM dir scan | informational only; never blocks |
| codebones | not probed in v1 (v1.x: PATH discovery, uv pattern) | — |

`scv env` always exits 0 and returns the full snapshot + `diagnostics[]`; hosts decide. `scv env` and the run-time `checking` gate share one diagnostics schema and one discovery implementation — `scv env` is exactly what checking will see, minus the run-mode classification.

## 10. Concurrency & lifecycle

- The host backgrounds `scv run` (or imports the supervisor and runs it in a goroutine). The engine never daemonizes, never detaches.
- Per-repo mutual exclusion: flock on `<analysis_dir>/.scv-lock` — a second `scv run` on the same analysis dir fails fast with `WORKFLOW_LOCKED` (`--analysis-dir` override for deliberate parallel re-analysis).
- `jobs cancel <id>` = SIGTERM to the recorded process group (engine sets setpgid on itself at start), state → `cancelled` after the group exits. Cancel of a terminal job = `WORKFLOW_NOT_CANCELLABLE`, no side effects.
- Dist build race (concurrent runs triggering `go build` in the same dist) is prevented by an flock around any build; runs that find fresh binaries skip the build (perceive.py already does mtime staleness checks).

## 11. Packaging (`make pack`)

Two artifacts, both attached to a release:

1. `perception-runtime-<ver>.tar.gz` — platform-independent sources: `perceive.py`, `analyzer/` (sources + `uv.lock`, no `.venv`), `loader/`, `graphmcp/` (sources), `adapters/*/MANIFEST.json`, **`docgen/{en,zh-cn}/`** (audit Q2); plus prebuilt `loader/perception-loader` and `graphmcp/scv-graph-mcp` for linux/darwin × amd64/arm64 (CGO_ENABLED=0) under `dist/<platform>/`. The engine, on first run or `scv env`, selects the current platform's pair into the flat `dist/loader/` + `dist/graphmcp/` layout perceive.py expects (symlink or copy, locked like §10). Hosts need no Go toolchain (§12 gate change makes the prebuilt binaries sufficient).
2. `scv-<ver>-<os>-<arch>[.exe]` — the engine binary, same four platforms, statically linked, embeds vectra.

`scv --version` handshake (§12) covers runtime↔engine compatibility; the engine looks for the runtime at perception-root resolution (§9) and, if absent, prints install guidance (v1 does not auto-download; go:embed bundling of the runtime is a listed future option).

## 12. Python-side changes (three items, all additive, all backward compatible)

1. **`perceive.py --version`** — prints `{"scv_version": "0.13.0", "contract": "1.0"}`, exit 0. The engine knows its supported contract range; major mismatch ⇒ run fails fast with `VERSION_INCOMPATIBLE` (`--force` overrides); missing `--version` ⇒ legacy mode: lenient parsing + `degraded` event `contract-unknown`.
2. **Tier gate: binary-OR-toolchain** (audit Q1 author decision) — the `if probe_go()` gates around loader/graphmcp become "run if the dist binary exists (fresh by mtime) OR the Go toolchain is present (then build if stale)". Hosts with prebuilt binaries and no Go now get the full `.vg` + query chain. Absence of both remains the `go-missing` degradation.
3. **Additive tier-start log lines** (audit Q4) — `tier1 start`, `tier2 start`, `tier3 start`, `tier6 start`, `tier8 start` lines before each stage, so every stage transition has a deterministic log source. Existing tests assert with `assertIn` on completion lines; additive lines do not break them.

Contract version bumps follow the existing additive-only evolution of the stdout JSON.

## 13. Scope & roadmap

In v1: `scv env/repo/link/run/jobs/query`, command-mode docgen, `make pack`, the three Python changes, supervisor package for Go hosts, Fiber example (`engine/examples/fiber/` — a ~50-line REST+SSE bridge over jobs; host-owned serving, shipped as a sample, not a product).

Explicitly not in v1: `scv serve`/daemon/SSE in-engine; `batch` (multi-repo jobs grouped by `batch_id`, port of `batch_manager.py` semantics with `~/.scv/sessions/` format compat — the natural v1.1, together with index.md); codebones/deep analysis (v1.x: PATH discovery + conditional prompt sections); built-in LLM HTTP client; Spoon runtime tier; embedding/vector search; Windows CI (paths handled, CI later).

Estimate: engine core (env/repo/job/run/docgen/query) 2.5–3 wk; CLI + contracts 1 wk; pack + Python changes 0.5–1 wk; integration tests 1 wk ⇒ **≈ 5–6.5 person-weeks**.

## 14. Testing

- Integration: `engine/tests/` drives `scv run` over `perception/fixture/` repos (multi-language, springboot monolith/multimodule, not-framework) — golden checks on job.json states, the per-path event sequences of §7, artifact existence, perceive stdout passthrough.
- Unified dependency gate: env with uv+perception-root both missing ⇒ ONE `ENV_INCOMPLETE` failure listing both items (not first-miss-abort); `--allow-degraded` with uv missing ⇒ upfront degraded event + reduced sequence; `scv env` output equals the gate's discovery input.
- No-Go host: PATH stripped of `go` with prebuilt dist present ⇒ full chain to `.vg` and `scv query` (validates the §12 gate change); dist empty and no Go ⇒ `go-missing` degradation golden.
- Linked local repo: fixture with uncommitted change ⇒ run reflects the dirty tree; `repo list` shows `type: linked`; `repo remove` removes the symlink only, target untouched.
- Docgen: `tests/fake-llm.sh` — reads `$SCV_DOC`, cats the corresponding fixture markdown, exit 0; failure-modes fixture (exit 1, timeout via sleep) for `DOCGEN_FAILED`/`STAGE_TIMEOUT` paths; docgen asset resolution from the packed `docgen/` tree.
- Skip semantics: structure-only run, then llm-configured re-run on same commit ⇒ runs (not `skipped`), documents produced.
- Concurrency: two parallel runs on one repo ⇒ one `WORKFLOW_LOCKED`; cancel mid-analyzing on a slow fixture ⇒ `cancelled` + no orphan children (process-group kill asserted).
- Contract: `--version` handshake matrix (match / major-mismatch / absent).

## 15. Builder check round 1 — resolutions

| # | Finding | Resolution |
|---|---|---|
| 1 | prebuilt dist vs `if probe_go()` gate contradiction | author: additive gate change (§12.2) |
| 2 | docgen prompt assets not in runtime whitelist | spec call: `docgen/` in pack (§8, §11) |
| 3 | local-git source: run.md clone vs git_op in-place | author: symlink into `~/.scv/repos` (§6) |
| 4 | perception.log lacks stage-start markers; tiers 6/8 silent on success | author (via Q1): tier-start lines (§12.3) + artifact belt-and-braces (§7) |
| 5 | deep analysis / codebones scope | author: v1 out, degraded event, v1.x PATH discovery (§2) |
| 6 | `--output-dir` runs outside query surface | spec call: `analysis-dirs.json` registry (§7) |
| 7 | skip-unchanged traps docgen re-runs | spec call: commit + expected-artifacts check (§7) |
| 8 | `repo remove` blast radius | spec call: checkout/symlink only, `--with-analysis` opt-in (§6) |
| 9 | (post-audit, author) uv/go/mcp/docgen missing conditions scattered across tiers | unified dependency gate: one `checking` stage, one diagnostics schema, all blocking items in one `ENV_INCOMPLETE` error, all degrading items in one upfront `degraded` event (§6, §7, §9) |
