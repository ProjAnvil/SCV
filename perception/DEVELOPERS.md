# Code Perception — v3 Architecture (multi-language tree-sitter engine)

How the capability layer works and where the extension points are.
Specs: `docs/superpowers/specs/2026-10-20-perception-v3-multilang-design.md`
(v3, binding rev 3) and
`docs/superpowers/specs/2026-10-14-perception-v2-treesitter-design.md` (v2
baseline; java flow and artifact contract unchanged).

## Runtime pieces

- `perceive.py` — orchestrator. Detects the languages enabled via the
  `structure_languages` config key (default: all six shipped), walks the repo
  once for the file extensions of every enabled language, runs the tier when
  ANY enabled language has ≥ 1 source file (`not-framework` survives only as
  the "no enabled language present" outcome), checks runtimes (Python 3.11+
  via uv probe, Go), runs the analyzer, stages artifacts, drives the
  loader/MCP tiers. Contract: single-line JSON on stdout, exit 0 always.
  Its language table mirrors `structure/langs.py` (v2 precedent: each side
  keeps its own walk code, the table is the shared contract).
- `analyzer/` — uv project (Python 3.11+, tree-sitter; six pinned grammars
  in `pyproject.toml`/`uv.lock`). `analyzer.py` is the CLI (`--languages`
  selects the extractors, default all; unknown values warn + ignore);
  `structure/` is the language layer (table below); `extensions/` holds the
  framework enrichment modules. `uv.lock` is committed (reproducible
  installs; repo precedent: eval/uv.lock). Tests and fixtures live in
  `analyzer/tests/` and are NEVER installed to user machines.
- `loader/` — Go module (`github.com/projanvil/vectra`): structure.json →
  structure.vg + cycle grading. **Cycle grading is INJECTS-only** (see
  below); new edge labels (CALLS from new languages, IMPORTS) can never
  reach the user cycle summary. No `lang` property index is needed —
  snapshot `{prop}` anchors fall back to a node scan for unindexed keys
  (vectra v0.6), so `MATCH (n {lang:'python'})` answers from v3 snapshots
  as-is (T7 spike, 2026-10-20).
- `graphmcp/` — Go MCP server over every `~/.scv/analysis/*/structure.vg`;
  its cypher tool description documents the full node/edge surface
  (including `lang` filtering and IMPORTS traversal examples).
- `adapters/springboot/` — the Spoon engine, **repositioned as the
  cross-validation engine**: repo/test-time only, NOT shipped by install.sh,
  NOT on the run path. Its 5-fixture suite stays the ground truth.

## Structure layer modules (`analyzer/structure/`)

| module | responsibility |
|---|---|
| `langs.py` | **THE canonical language table** — name → suffixes + walk rules (shared exclusions, test-dir policy). The analyzer dispatches on it; `perceive.py` mirrors the names. New languages register here. |
| `model.py` | `Graph_out` whole-repo accumulator: node/edge emission, key-collision counting, both-endpoints drop rule, `add_import` helper, `meta.languages` / `meta.structure.by_lang` / `import_edges` / `go_unverifiable_satisfaction` accounting. Language-agnostic. |
| `java_extractor.py` | java — v2 scope-based extraction (source-root walk, package/imports FQN resolution) plus IMPORTS emission (T5); the only extractor without the two-pass shape (its scope analysis subsumes pass 1). |
| `python_extractor.py` | python — package/module tree, MRO-closure `self`/`cls` calls, builtin blocklist. |
| `jsts_extractor.py` | javascript + typescript — one module, two grammars (tree-sitter-typescript ships `typescript` + `tsx`); annotated receivers, interface virtual-dispatch, relative-specifier resolution, `require()`, re-export one hop. |
| `go_extractor.py` | go — method-set matching IMPLEMENTS (spec §5.1) with receiver value/pointer, embedding promotion, type-set constraints as REFERENCES only, identifier-use-matched IMPORTS. |
| `rust_extractor.py` | rust — crate/mod tree discovery (`Cargo.toml` → `src/main.rs`/`src/lib.rs` → `mod` walk), `impl Trait for Type` IMPLEMENTS, `dyn Trait` virtual-dispatch. |

### The two-pass extractor pattern (python / jsts / go / rust)

All four new extractors follow the graphify-aligned design (spec §3.1):

- **pass 1 — per-file parse → tree walk → local tables**: one pass per file
  collects import tables, type tables (bases/members), and method bodies
  into in-file name tables; nodes and CONTAINS edges are emitted immediately.
  Unresolved callees land in a per-file `raw_calls` list.
- **pass 2 — cross-file binding**: module/package maps resolve IMPORTS edges
  and imported symbol bindings; `raw_calls` entries bind against the
  cross-file symbol tables; base/interface resolution produces
  EXTENDS/IMPLEMENTS (go: the §5.1 method-set match runs here).

Every unresolved endpoint is dropped and counted (`unresolved_refs` /
`dropped_edges`) — never guessed, never dangled. Each extractor's module
docstring carries its **grammar-pitfall list** (the node shapes that fooled
the tree walk and the workaround, e.g. go's `method_elem` vs `field_spec`,
rust's `mod`-discovery order) — read it before touching the walker.

### Adding a new language (checklist)

1. Pin the tree-sitter grammar in `pyproject.toml` + `uv lock`.
2. Register the language in `langs.py` (suffixes, walk exclusions, test-dir
   policy) and mirror the name in `perceive.py`'s table + config default.
3. Implement `structure/<lang>_extractor.py` on the two-pass pattern
   (`extract(repo, out) -> ParsedFileList` contract, `lang` prop on every
   Type/Method node, File nodes get `language` + `lang`).
4. Follow the graphify-alignment rules (spec §3.1): a **language-local,
   bare-identifier-scoped builtin blocklist** (never a shared table — a
   same-named private method once earned 330 phantom CALLS edges on
   graphify's 8.9k-node corpus); **interface/trait member nodes** so calls
   through an interface resolve by name; **honest counting** for everything
   syntax cannot prove.
5. Snippet tests per rule + a fixture with hand-counted ground truth
   (multiset assertions, `by_lang` presence, honesty floors for
   `unresolved_refs`/`dropped_edges`); wire the fixture into
   `perception/fixture/check.py`.
6. The core (`model.py`, loader, graphmcp) is language-agnostic and needs
   no change.

## Artifact schema (the contract between analyzer and loader)

`{"nodes":[{"key","label","props"}], "edges":[{"srcKey","dstKey","label","props"}], "meta":{...}}`

Labels and key shapes:

| label | key | notable props |
|---|---|---|
| `File` | `file:<repo-relative path>` | path, language + lang, loc |
| `Type` | `type:<fqn>` | kind (class/interface/record/enum/annotation...), modifiers, lang, loc |
| `Method` | `fn:<fqn>#<name>(<params>)` | name, visibility, static, params, returns, owner, lang, loc |
| `Bean` | `bean:<name>` | beanName, type, stereotype, conditions, source |

Param normalization is per language and frozen in the spec (v3 §4): java
keeps the v2 rule; python/js carry arity only; ts keeps annotated types;
go/rust keep type names as written.

Edges: `CONTAINS` (File→Type), `IMPLEMENTS`/`EXTENDS` (Type→Type; IMPLEMENTS
carries `kind: declared|method-set` — go method-set edges add `receiver:
value|pointer`), `CALLS` (Method→Method; props line, kind
direct|virtual-dispatch), `REFERENCES` (Method→Type), `IMPORTS` (File→File,
props none), `INJECTS` (Bean→Bean; prop `via` ∈ {constructor,
lombok-constructor, field, inherited-field, setter, beanMethod}).

IMPORTS counting ruling (v3 spec §5, rev 3 addendum): an **external** import
(bare npm specifier, external Go package, external crate, external java
package) emits no edge and no count; an **in-repo** import whose
symbol/identifier fails to bind (go selector-use miss, rust unknown `use`
target, python unknown from-import name, re-export beyond one hop) emits no
edge and increments `unresolved_refs`; an edge whose endpoints would dangle
falls to `dropped_edges` via the standard rule.

`meta.languages` = sorted set of languages with ≥ 1 emitted node;
`meta.structure` keeps the aggregate counters plus `by_lang`
(`{lang: {files, types, methods, call_edges}}`), `import_edges`, and
`go_unverifiable_satisfaction` (§5.1 honest-miss counter).

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

**Languages** (structure-layer grammars): the v3 extractor contract is
`extract(repo, out) -> ParsedFileList` on the two-pass pattern — follow the
checklist above. The core (`model.py`, loader, graphmcp) is
language-agnostic and needs no change.

## MANIFEST schema (detect-only)

`adapters/<id>/MANIFEST.json` — only `detect` is consumed by the run
path: `marker_files` (build files to scan, depth ≤3), `markers` (content
substrings marking a candidate), `confirm` (source markers, e.g.
`@SpringBootApplication`). `requires`/`build`/`run` are unused leftovers
from v1 (kept for a possible future deep tier). Language detection no
longer routes through MANIFESTs — it walks file extensions per the
`structure_languages` gate (spec §7); MANIFEST detection only feeds the
springboot extension tier.

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
