# Engine integration golden tests

This directory holds the opt-in integration suite (`integration_test.go`,
build tag `//go:build integration`): the **real** `scv` binary driving the
**real** perception toolchain — `perceive.py`, the `uv run --project
analyzer` analyzer, and the loader/graphmcp dist — over the shipped
fixture repos, asserting the full spec §7 event sequences and artifacts.

The suite is excluded from unit runs (the ≥80% coverage gate stays on the
unit suite) and wired as `make engine-test-integration`.

## How to run

From the repository root:

```
make engine-test-integration
```

or directly:

```
cd engine && go test -tags=integration ./tests/ -v -timeout 20m
```

## Machine prerequisites

- **go ≥ 1.27** on PATH (the engine's toolchain floor; builds the test
  binary and the shared dist, and satisfies the full-run gate),
- **git**, **python3** (3.9+; perceive.py's orchestrator floor),
- **uv** on PATH — the analyzer tier runs `uv run --project
  perception/analyzer`. The first-ever run provisions the analyzer venv
  (`perception/analyzer/.venv`, ~30s+ and network); afterwards it is
  cached and each analysis of a fixture takes seconds.

Machine assumption for the uv-missing case: the test strips every PATH
entry that can run `uv` **and** pins `HOME` to an isolated dir, which is
how the engine's discovery fallbacks (`~/.local/bin/uv`,
`/opt/homebrew/bin/uv`) are also hidden. On a machine with uv installed
at `/opt/homebrew/bin/uv` that fallback is an absolute path and the case
cannot be produced by PATH/HOME alone; the machine this suite was built
on has uv at `~/.local/bin/uv`.

## Fixture inventory

`perception/fixture/repos/` (verified 2026-11-14):

| fixture | used by | why |
|---|---|---|
| `golang-complex` | full run, go-missing, uv-missing, docgen, cancel, skip semantics, linked repo, prebuilt dist | the fastest full-chain fixture (go is a shipped structure language) |
| `ruby-plain` | not-framework | ruby is **not** among the six shipped structure languages, so detect returns nothing and the run reduces to the not-framework path |
| `java-plain-core` | — (perception suite) | |
| `java-springboot-monolith` | — (perception suite) | |
| `java-springboot-multimodule` | — (perception suite) | |
| `mixed-polyglot` | — (perception suite) | |
| `python-complex` | — (perception suite) | |
| `rust-complex` | — (perception suite) | |
| `ts-complex` | — (perception suite) | |

The fixtures are **not git repos**: every test copies the fixture it
needs into its own tmp tree and runs `git init -b main` + one commit with
a pinned identity (`test@scv.local` / `SCV Test`, like the engine repo
tests), so preparing/HeadCommit/skip semantics run against a real HEAD.
Fixture copies are never mutated — every case ends with a
`git status --porcelain` emptiness assertion.

## Harness design

- `TestMain` builds the `scv` binary and a **shared real dist**
  (`go build` of `perception/loader` and `perception/graphmcp`) once.
  Because their mtimes are newer than the checked-in sources, every later
  run treats them as fresh prebuilts and never rebuilds — the suite never
  writes into the repo checkout or the real `~/.scv`.
- Every test gets its own tmp `SCV_HOME` (also passed as `--scv-home`)
  and its own `SCV_PERCEPTION_DIST`: the shared dist by default, an empty
  dir for the go-missing case, and freshly-copied real binaries for the
  no-toolchain prebuilt case (the perception suite's fake-loader
  fresh-mtime technique, using the real loader so the resulting
  `structure.vg` is genuinely queryable).
- Toolchain-shaped cases rewrite `PATH` by filtering out every entry that
  can produce the hidden tool (dir-content checks catch symlinked
  brew-style installs) — the same technique as the perception suite's
  `_path_without`.
- `TestIntegrationCancelMidAnalyzing` does not race the real analyzer: it
  points `SCV_ENGINE_TEST_PERCEIVE` at a shim that replays the tier log
  lines up to `tier3 start` and then sleeps, so the run is provably
  mid-analyzing when the test SIGTERMs the engine.
- Docgen uses `fake-llm.sh` (this directory) with `--lang en`; prompt
  assets resolve through the repo-checkout fallback (`agents/en`,
  `skills/en/references/templates`), so no pack step is needed.

## Runtime expectations

Approximately 1–3 minutes on a warm machine: the bulk is seven real
analyzer runs (seconds each on the cached venv) plus the docgen/skip
re-runs. A cold analyzer venv adds a one-time uv provisioning (~30s+).
The Makefile target passes `-timeout 20m`.
