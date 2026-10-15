# Code Perception — v2 Architecture (tree-sitter engine)

How the capability layer works and where the extension points are.
Spec: `docs/superpowers/specs/2026-10-14-perception-v2-treesitter-design.md`.

## Runtime pieces

- `perceive.py` — orchestrator (v2). Detects the framework via adapter
  MANIFESTs, checks runtimes (Python 3.11+ via uv probe, Go), runs the
  analyzer, stages artifacts, drives the loader/MCP tiers. Contract:
  single-line JSON on stdout, exit 0 always.
- `analyzer/` — uv project (Python 3.11+, tree-sitter). `analyzer.py` is the
  CLI; `structure/` is the language-agnostic core; `extensions/` holds the
  framework enrichment modules. `uv.lock` is committed (reproducible
  installs; repo precedent: eval/uv.lock). Tests and fixtures live in
  `analyzer/tests/` and are NEVER installed to user machines.
- `loader/` — Go module (`github.com/projanvil/vectra`): structure.json →
  structure.vg + cycle grading. **Cycle grading is INJECTS-only** (see below).
- `graphmcp/` — Go MCP server over every `~/.scv/analysis/*/structure.vg`;
  its cypher tool description documents the full node/edge surface.
- `adapters/springboot/` — the Spoon engine, **repositioned as the
  cross-validation engine**: repo/test-time only, NOT shipped by install.sh,
  NOT on the run path. Its 5-fixture suite stays the ground truth.

## Artifact schema (the contract between analyzer and loader)

`{"nodes":[{"key","label","props"}], "edges":[{"srcKey","dstKey","label","props"}], "meta":{...}}`

Labels and key shapes:

| label | key | notable props |
|---|---|---|
| `File` | `file:<repo-relative path>` | path, language, loc |
| `Type` | `type:<fqn>` | kind (class/interface/record/enum/annotation), modifiers, loc |
| `Method` | `fn:<fqn>#<name>(<params>)` | name, visibility, static, params, returns, owner, loc |
| `Bean` | `bean:<name>` | beanName, type, stereotype, conditions, source |

Edges: `CONTAINS` (File→Type), `IMPLEMENTS`/`EXTENDS` (Type→Type), `CALLS`
(Method→Method; props line, kind direct|virtual-dispatch), `REFERENCES`
(Method→Type), `INJECTS` (Bean→Bean; prop `via` ∈ {constructor,
lombok-constructor, field, inherited-field, setter, beanMethod}).

Edges are only emitted when BOTH endpoints are source-declared nodes;
otherwise dropped and counted in `meta.structure.dropped_edges`.

**Loader cycle grading (INJECTS-only filter):** vectra `Cycles()` is
label-agnostic, so the loader filters a cycle's edges to `INJECTS` before
via-inspection. A cycle with no INJECTS edge at all (e.g. two methods in
mutual recursion via CALLS) is excluded from `cycles[]` entirely — the
user-facing cycle summary is about bean dependencies. Within a reported
cycle, any INJECTS edge with via constructor/lombok-constructor/beanMethod
grades the whole cycle fatal.

## The extension point

Two orthogonal extension axes:

**Language extensions** (framework enrichment, config `structure_extensions`):

1. The enabling interface is the analyzer CLI flag:
   `analyzer.py <repo_dir> <out_json> [--extensions springboot]`
   (comma/space separated; default: none).
2. Implement `analyzer/extensions/<name>.py` exposing
   `enrich(structure, out)` — it receives the finished structure layer and
   the `Graph_out` builder, and adds nodes/edges/counts. See
   `registry.py` for the dispatch.
3. Register the name in `perceive.py` `KNOWN_EXTENSIONS` (config values not
   in that tuple are dropped with a warning into perception.log — the
   registry double-checks on the analyzer side).
4. The Spring Boot annotation **name table** (terminal stereotypes, jar
   meta chains, behavioral annotations, the Spring Data family) lives at
   the top of `extensions/springboot.py`; in-source `@interface` chains are
   walked before the table is consulted.
5. Add fixtures + regression tests: snippet-level tests per rule plus
   multiset ground-truth assertions in `analyzer/tests/test_fixtures.py`
   (derive expectations by running the Spoon engine over the fixture —
   see cross-validation below).

**Languages** (structure-layer grammars): add
`structure/<lang>_extractor.py` implementing the same contract as
`java_extractor.py` (`extract(repo, out)` plus the exported helpers), pin
the tree-sitter grammar in `pyproject.toml`/`uv.lock`, and dispatch it from
`analyzer.py` (source-root discovery per language). The core (`model.py`,
loader, graphmcp) is language-agnostic and needs no change.

## MANIFEST schema (detect-only)

`adapters/<id>/MANIFEST.json` — only `detect` is consumed by the v2 run
path: `marker_files` (build files to scan, depth ≤3), `markers` (content
substrings marking a candidate), `confirm` (source markers, e.g.
`@SpringBootApplication`). `requires`/`build`/`run` are unused leftovers
from v1 (kept for a possible future deep tier). With
`structure_extensions` empty, detection degrades to marker-file-only Java
detection so the structure core still runs on non-Spring repos.

## Cross-validation (Spoon engine)

The Spoon adapter stays in-tree as the reference engine. The frozen
fixtures' ground truths in `analyzer/tests/test_fixtures.py` were derived
from its output (multiset equality over (key, stereotype, conditions) for
nodes and (srcKey, dstKey, via, qualifier, wraps, ambiguous) for edges);
`make crosscheck` (perception/Makefile, tools/crosscheck.py) re-runs both
engines over the 5 fixtures and diffs the same projection. Spoon runs in
GRADLE mode (classpath via the fixture's own build files; never modified)
— noclasspath mode is degraded and would produce false mismatches. Engine
command chain when driving it manually:

```
cd perception/adapters/springboot && gradle installDist -q
./build/install/springboot-probe/bin/springboot-probe gradle <fixture-copy> /tmp/v1.json
```

## Vocabulary warning

Anything a user can see (perception/README.md, skill docs, report copy) follows the
spec's vocabulary rules — implementation language is invisible, and the
missing-runtime footnote names the runtime, not the tool. This file and the
adapter's own README are developer-facing exceptions.
