# SCV engine

The SCV engine is a **one-shot Go control plane over the Python perception
toolchain**: the `scv` binary drives `perceive.py` (structure analysis via
the uv-run analyzer, the loader/graphmcp dist) through a frozen pipeline —
checking → preparing → skip-check → perceive → generating → finalizing —
with every stage transition recorded as a durable, machine-readable event.
Hosts that need progress streams, typed failures, job records, and queries
consume the engine; the Python side stays the data plane and never changes.

Layout of this module: `cmd/scv` (CLI), `internal/` (config, env + gate,
job, repo, run, docgen, query), `supervisor` (embedding façade),
`examples/fiber`, `tests/` (integration suite — see `tests/README.md`).

## Install

Prerequisites on the host: **uv**, **python3** (3.9+), **git**. **Go is
optional** — see the no-Go-host story below. Everything else (tree-sitter
analyzer deps, the loader/graphmcp binaries) is carried by the runtime.

1. Unpack the runtime tarball into `~/.scv`:

   ```
   mkdir -p ~/.scv && tar -C ~/.scv -xzf perception-runtime-<ver>.tar.gz
   ```

   This installs `~/.scv/perception/` (perceive.py, analyzer, loader and
   graphmcp sources, docgen assets, and prebuilt `dist/<platform>/`
   binaries).

2. Put the platform engine binary (`scv-<ver>-<os>-<arch>`, statically
   linked, no dependencies) anywhere on PATH as `scv`.

Both artifacts come from `make pack` at the repository root
(`scripts/pack.sh`). In a repository checkout the engine also runs
straight from source: `make engine-build` (Go ≥ 1.27 needed for that, and
for `make engine-test`).

### The no-Go host (prebuilt dist)

A packed runtime carries prebuilt loader and graphmcp binaries for its
platforms under `perception/dist/<goos>-<goarch>/`. On the first `scv env`
or `scv run`, the engine selects the current platform's pair into the flat
dist layout (`dist/loader/`, `dist/graphmcp/` symlinks) and the dependency
gate accepts the prebuilts **instead of** a Go toolchain — so a host with
no Go installed gets the full chain including `structure.vg` and queries.
Only when neither a Go toolchain (≥ 1.27) nor a prebuilt loader exists does
the run degrade (query tier off, structure.json still produced).

## The `~/.scv` layout

```
~/.scv/
  config.json          engine config; the llm section enables docgen (below)
  analysis-dirs.json   registry of analysis dirs the query verbs serve
  repos/               repos entries: clones and symlinks for linked worktrees
  analysis/<project>/  per-project artifacts: structure.json, structure.vg,
                       perception.log, .scv_metadata.json, and the four
                       generated documents when docgen is configured
  jobs/<id>/           per-run records: job.json snapshot + events.jsonl
  perception/          the runtime (installed from the tarball or a checkout)
```

### config.json — the llm section

```json
{
  "llm": {
    "mode": "command",
    "command": ["claude", "-p"],
    "timeout_s": 600,
    "retry": 1
  }
}
```

With an `llm` section present, every successful structure run also
generates the four project documents (README, SUMMARY, ARCHITECTURE,
FILE_INDEX) by invoking `command` once per document; without it, runs are
structure-only (reported as a `docgen-unconfigured` degraded item).

**Key custody:** the engine never reads, stores, or logs API keys. The
command is invoked exactly as configured — document identity and context
travel via environment variables (`SCV_DOC`, `SCV_TEMPLATE`, ...), never
argv — so whatever auth the command needs stays between the host and the
command (environment references, the command's own login, ...).

## Quickstart

```
scv env                                   # snapshot + dependency diagnostics
scv repo link ~/code/myproject --name myproject
scv run ~/code/myproject                  # NDJSON event stream on stdout
scv jobs list                             # recent jobs
scv jobs status <id>                      # durable snapshot of one job
scv query cypher myproject "MATCH (n) RETURN n LIMIT 5"
```

`scv run` also accepts git URLs (cloned under `~/.scv/repos`) and plain
local directories (analyzed in place). Useful flags: `--project` (name),
`--lang` (documents; default zh), `--skip-unchanged`, `--allow-degraded`,
`--force`.

## The NDJSON run contract

`scv run` stdout is one JSON object per line — the exact schema of the
job's durable `events.jsonl` — ending with the terminal `job_result` line:

```json
{"id":"19vrpx2q","seq":1,"ts":"...","type":"stage_started","stage":"checking"}
{"id":"19vrpx2q","seq":2,"ts":"...","type":"stage_finished","stage":"preparing","detail":{"source_kind":"local_git","head_commit":"..."}}
{"id":"19vrpx2q","seq":9,"ts":"...","type":"job_result","stage":"done","detail":{"analysis_dir":"...","perception_status":"ok"}}
```

Fields are snake_case (`id`, `seq`, `ts`, `type`, `stage`, `detail`,
`error` with `code`/`message`/`retryable`/`log_tail`). Human diagnostics
go to stderr (`--quiet` silences them). The same events the stream
carries are persisted under `~/.scv/jobs/<id>/events.jsonl`, so
`scv jobs events <id>` replays any run.

## Exit codes

| code | meaning |
|---|---|
| 0 | success — including degraded-but-done and skipped runs |
| 1 | generic failure (CLONE_FAILED, PULL_FAILED, ANALYZER_FAILED, DOCGEN_FAILED, FINALIZE_FAILED, query/store errors, I/O errors) |
| 2 | usage error (bad flags or arguments; cobra's own failures) |
| 3 | VERSION_INCOMPATIBLE — run refused by the version handshake |
| 4 | WORKFLOW_NOT_CANCELLABLE — `jobs cancel` on an ended job |
| 5 | WORKFLOW_LOCKED — run refused by the per-analysis-dir lock |
| 6 | NOT_FOUND — unknown job id or repos entry |
| 7 | NAME_EXISTS — repo clone/link name collision |
| 8 | PATH_NOT_FOUND — missing run source / repo link path |
| 9 | ENV_INCOMPLETE — blocking gate items at checking |
| 10 | CANCELLED — the run ended cancelled |

## Known issues

- **PATH-shared-dir filtering in the integration suite** (`engine/tests`):
  the go/uv-missing cases drop every PATH entry that can produce the tool.
  On a machine where one directory holds several tools (e.g. brew's
  `/opt/homebrew/bin` holding both `git` and `uv`), filtering for one tool
  removes the others from the rewritten PATH too — the case then exercises
  a different environment than intended. See `engine/tests/README.md`.
- **brew-uv assumption**: the uv-missing integration case hides uv via
  PATH + HOME rewriting, but discovery also probes `/opt/homebrew/bin/uv`
  as an absolute fallback. On a machine with uv installed there the case
  cannot be produced and skips. Removing the assumption needs an
  engine-side env seam (e.g. a test-only discovery override).
- **Second Ctrl-C is swallowed**: `scv run` relays the first SIGINT/SIGTERM
  to an orderly pipeline unwind (perceive subtree killed, CANCELLED result
  recorded). The relay is single-shot: a second Ctrl-C during the unwind is
  buffered and not acted on, so a stuck unwind must be ended with SIGKILL.
- **Supervisor side effect on the host process group**: every run pipeline
  calls `SetProcessGroup` (its own pgid, so `jobs cancel` can signal the
  run tree). In-process — a run started through the `supervisor` package —
  that re-parents the **embedding host's own process group**: the host
  leaves its original group and any external group-signal semantics
  targeting the host's old group no longer reach it.

## Development

```
make engine-test              # unit tests: -race + >=80% coverage gate
make engine-test-integration  # real scv + perceive.py over fixtures
make pack                     # release artifacts (tarball + binaries)
```

The integration suite's machine prerequisites and fixture inventory are
documented in `engine/tests/README.md`.
