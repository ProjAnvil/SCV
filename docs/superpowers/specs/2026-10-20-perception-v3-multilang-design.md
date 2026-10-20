# Perception v3 — Multi-language tree-sitter Structure Layer — Design Spec

- Date: 2026-10-20
- Status: drafted; awaiting user confirmation before planning
- Builds on: 2026-10-14-perception-v2-treesitter-design.md (v2 shipped in
  v0.11.0); v2 §9 explicitly deferred "languages beyond Java" — this spec is
  that item, plus the repo-per-vectra-file requirement from the user.
- User decisions (brainstorm 2026-10-20):
  1. First batch: **Python, JavaScript, TypeScript, Go, Rust** (Java already
     shipped) — all in one release.
  2. CALLS edges: **best-effort resolution + honest counting** (per-language
     capability varies; never dangle, never guess).
  3. Vectra file form: **one `structure.vg` per repo**, multi-language in a
     single graph, nodes carry `lang`.
  4. Trigger gate: **config switch `structure_languages`, default ALL shipped
     languages enabled**.
  5. Session 2026-10-20 (rev 2): **IMPORTS edge stays** (user confirmed);
     **Go IMPLEMENTS is extracted via syntactic method-set matching**
     (§5.1) — replacing the v1 draft's "documented hole" disposition.
     Research basis: graphify v8 `extractors/go.py` read in full — it emits
     `contains / embeds / imports_from / method / references / calls` only,
     **no implements relation for Go at all**; interface method specs become
     method nodes so calls-by-name can resolve. We go one step further.
  6. Session 2026-10-20 (rev 3): user ruling — **the new extractors align
     with graphify's extractor design** (verified patterns from their
     go.py/source, listed in §3.1); spec otherwise approved as-is; build
     begins against this revision.

### 3.1 Extractor design alignment with graphify (rev 3 ruling)

Adopted from graphify v8's extractor implementations (patterns their corpus
testing already paid for):

1. **Per-file parse → tree walk → local closures**: one `extract_<lang>`
   pass per file collecting into name tables before any edge emission; our
   `Graph_out` stays the single whole-repo accumulator (their dict merge
   step folds in at `to_json()`).
2. **Two-pass CALLS resolution**: pass 1 resolves in-file (locals, params,
   fields, imported bindings); unresolved callees land in a `raw_calls`
   list; pass 2 binds them cross-file by symbol tables. Java's v2 scope
   analysis stays as-is; python/js/ts/go/rust use the two-pass form.
3. **Per-language builtin blocklists, language-local and bare-identifier
   scoped** — the graphify `append` lesson verbatim: a same-named private
   method once absorbed 330 phantom `calls` edges on an 8.9k-node corpus.
   Python (`len`, `print`, `open`, ...), JS (`require`, `parseInt`, ...),
   Go (`append`, `cap`, `close`, ... — the full spec list), Rust (`vec!`,
   macro set) each keep their own frozenset, never a shared table.
4. **Interface/trait member nodes**: interface method specs become Method
   nodes (CONTAINS via the owner type) so calls through an interface can
   resolve by name — applies to Go interfaces, TS interfaces, Rust traits
   (Java v2 already does this implicitly through Type methods).
5. **Confidence stays prop-based, not edge-label-based**: `kind:
   declared|method-set` (IMPLEMENTS), `kind: direct|virtual-dispatch`
   (CALLS) — the schema fork graphify carries (`EXTRACTED/INFERRED/
   AMBIGUOUS` string per edge) is not adopted; our v2 meta counters are
   the honest-accounting surface.

## 1. Motivation & positioning

v2's structure layer only knows Java. Every non-Java repository returns
`status: skipped` and produces **no vectra file at all** — `python-complex`
and `golang-complex` fixtures exist today purely to assert that skip path
stays clean. The user requirement for v3:

> 继续采用 tree-sitter，广泛地对代码库生成元数据，保存到 vectra 里。
> analysis 里每个 repo 都要有自己的代码库 vectra 文件。

v3 makes the structure tier run on **six languages** (java, python,
javascript, typescript, go, rust) so that every analyzed repo — whatever its
language — leaves `~/.scv/analysis/{repo}/structure.json` +
`structure.vg`, queryable through the existing `scv-graph-mcp` server with
zero protocol changes.

Positioning note (post Graphify research, session 2026-10-20): this is the
same tree-sitter route Graphify took; our retained differentiator is the
vectra graph + shipped multi-repo MCP query tier, and (unchanged) the LLM
document layer on top.

## 2. What stays unchanged

- **stdout JSON contract** (v2 §2): single line, exit 0 always,
  `{status, tiers, reason, counts, artifacts, mcp_add, footnote}`.
- **Artifacts**: `~/.scv/analysis/{project}/structure.json` + `structure.vg`
  + `perception.log`; loader + graphmcp Go binaries and their build/rebuild
  triggers.
- **Security**: the analyzer never executes anything inside the analyzed
  repo; `--source remote|local` identical.
- **Java flow exactly as v2**: `java_extractor` + `springboot` extension and
  their 5-fixture precision contract (multiset ground truth, Spoon
  crosscheck) are untouched.
- **Graph_out accounting**: key collisions counted never merged; edges only
  when BOTH endpoints are declared nodes, else drop+count.
- **Loader cycle grading**: INJECTS-only filter — new edge labels (CALLS from
  new languages, IMPORTS) can never reach the user cycle summary.
- **Tier ladder**: config check → detect → stale cleanup → analyzer (uv) →
  structure.json → Go tier (loader + graphmcp) → MCP hint.

## 3. Repository layout (delta)

```
perception/analyzer/
├── pyproject.toml            # + 5 pinned tree-sitter language grammars
├── analyzer.py               # CLI gains --languages; per-language dispatch
├── structure/
│   ├── model.py              # LANGUAGES list grows; add IMPORTS edge helper
│   ├── java_extractor.py     # unchanged (plus IMPORTS emission, §6)
│   ├── python_extractor.py   # NEW
│   ├── jsts_extractor.py     # NEW — one module, two grammars (js + ts)
│   ├── go_extractor.py       # NEW
│   └── rust_extractor.py     # NEW
└── extensions/registry.py    # unchanged; springboot stays java-only
```

**Extractor contract** (java_extractor's existing shape, now shared): each
extractor is `extract(repo, out) -> ParsedFileList`, adds its language's
File/Type/Method nodes (with `lang` prop) plus edges into the shared
`Graph_out`, and returns its parsed-file list (the springboot extension keeps
consuming the java one only).

`analyzer.py` CLI: `--languages java python ts ...` (names below); unknown
values warn + ignore (same precedent as `--extensions`). The analyzer
internally intersects requested languages with files actually present.

## 4. Language names, discovery, and FQN schemes

| name | extensions | identity root | Type key | source of FQN |
|---|---|---|---|---|
| `java` | `.java` | `src/main/java` roots (v2 walk) | `type:<fqn>` | package decl + imports (v2, unchanged) |
| `python` | `.py` | package root (deepest dir whose subtree holds the top-level packages; `__init__.py` marks packages) | `type:<dotted.module.Class>` | `from X import Y` / `import X.Y` resolved against the module tree |
| `javascript` | `.js .jsx .mjs .cjs` | repo root | `type:<module-spec>::<ExportName>` | relative specifiers `./x` → in-repo file; bare specifiers external → drop+count |
| `typescript` | `.ts .tsx .mts .cts` | repo root | `type:<module-spec>::<ExportName>` | same, plus TS `import type` tracked for REFERENCES |
| `go` | `.go` | package dir = declaring directory | `type:<pkg-path>.<Type>` | import literals resolved to in-repo package paths |
| `rust` | `.rs` | crate root (`src/main.rs` / `src/lib.rs` per `Cargo.toml`) | `type:<crate::module::Item>` | `mod` declarations walked to module tree; `use` paths resolved against it |

File node key stays `file:<repo-relative path>` for every language; props
`path, language, loc` (v2 already emits `language` on File — Type/Method
nodes GAIN `props.lang` in v3).

Method key stays `fn:<fqn>#<name>(<param-normalized>)`. Param normalization
is per-language and frozen in the spec:

| language | normalization |
|---|---|
| java | v2 rule (generics erased, arrays → `[]`) — unchanged |
| python | arity only: `(n)` — annotations erased (overloading is not a Python semantic) |
| javascript | arity only: `(n)` |
| typescript | annotated types when present, else `?` per param |
| go | param type names as written |
| rust | param type names as written |

Excluded directories for the multi-language walk (shared, additive to v2's
`build/ .gradle/ target/ src/test/ dot-dirs`): `node_modules`, `__pycache__`,
`venv .venv env`, `dist`, `vendor` (Go), `testdata` (Go), `target` (Rust —
already excluded by name), `out`. Test trees: `src/test/` (v2, java),
`*_test.go` files still parsed (they are in-package methods; Go tests are
first-class), `tests/` + `test/` dirs skipped for python/js/ts/rust.

## 5. Edges per language

| edge | java | python | js/ts | go | rust |
|---|---|---|---|---|---|
| `CONTAINS` (File→Type) | v2 | yes | yes | yes | yes |
| `EXTENDS` (Type→Type) | v2 | class bases in-source | `extends` clause in-source | **embedded structs only** | supertrait? no — Rust has none; base via `impl` not applicable → n/a |
| `IMPLEMENTS` (Type→Type) | v2 | class bases in-source (mixed bases counted as IMPLEMENTS) | TS `implements` clause; JS none | **YES — method-set matching, §5.1** | `impl Trait for Type` — fully explicit, most reliable of all six |
| `CALLS` (Method→Method) | v2 scope-based | §6 ladder | §6 ladder | §6 ladder | §6 ladder |
| `REFERENCES` (Method→Type) | v2 | instantiated/referenced names in-source | referenced imported types in-source | struct/interface literals, type names | path-referenced types |
| `IMPORTS` (File→File) — **NEW** | single-type imports resolved to the declaring file; wildcard → no edge | module-level import resolving to an in-repo module file | import/export-from specifier resolving to an in-repo file (one hop; re-export chains beyond one hop → drop+count) | import resolves to an in-repo **package**; the edge lands on each file of that package **declaring an identifier the importing file actually uses** (selector-use match); no identifier match → no edge, counted | `use` resolving to an in-repo module file |

**IMPLEMENTS edge gains a `kind` prop** (new for v3, all languages):
`"declared"` when the relation is written in the source (Java `implements`,
Python base class, TS `implements`, Rust `impl Trait for Type`) and
`"method-set"` when it is deduced by coverage matching (Go only). This is
the EXTRACTED/INFERRED distinction adopted from the graphify comparison —
on the edge where it costs one prop, not a schema fork.

### 5.1 Go IMPLEMENTS — syntactic method-set matching

Go satisfaction is structural: type `T` implements interface `I` iff `T`'s
method set covers every method of `I` with matching signature. Graphify
punts on this (no implements edge at all — interface methods become bare
method nodes). We extract it, bounded to what syntax can prove:

1. **Interface tables**: every in-source `type X interface { ... }` yields
   `I(X) = {(name, param-arity, result-arity, param-simple-types,
   result-simple-types), ...}`. Embedded interfaces (in-source) are unioned
   transitively. Generic type-set constraints (`A | B`, `~T` in type
   parameter lists) are **REFERENCES, never IMPLEMENTS** (graphify's
   same ruling, verified in their source).
2. **Type method sets**: every `func (r T) Name(...)` contributes to
   `M(T)`, split by receiver form — value receiver methods land in
   `M_value(T)`, pointer receiver in `M_ptr(T)`; `M(T) = M_value ∪ M_ptr`.
   **Promotion**: struct embedding unions the embedded in-source type's
   method set transitively (Go's real promotion rule, walked
   syntactically).
3. **Match rule**: `T` implements `I` when `I(T) ⊆ M(T)` on
   (name, param-arity, result-arity) **and** every param/result simple-type
   name equals the interface's, position by position. Edge props:
   - `kind: "method-set"`
   - `receiver: "value"` when coverage holds with `M_value(T)` alone
     (Go semantics: `T` itself satisfies `I`), `receiver: "pointer"` when
     coverage needs pointer-receiver methods (only `*T` satisfies `I` —
     the edge is honest about it, the graph stays single-node per type).
4. **Honest holes, counted not guessed**:
   - an embedded **external** type's promoted methods are unknowable →
     coverage unverifiable → no edge, counted in
     `meta.structure.go_unverifiable_satisfaction`
   - param/result types compared by **simple name**: `pkg.Foo` vs local
     `Foo` collide — documented approximation (same class as v2's
     wildcard-import limitation, frozen in the fixture)
   - O(types × interfaces) with method-set hashing; effectively linear.
5. **Precision contract**: fixture ground truth asserts the emitted
   IMPLEMENTS multiset exactly — including at least one
   `receiver: "pointer"` case, one promotion-earned case, and one
   honest-miss case (external embed). Interfaces from **external**
   packages never get edges (they are not in-source nodes — the v2
   both-endpoints rule already guarantees this).

The Go "IMPLEMENTS hole" from draft rev 1 is **closed** by this section;
the two approximations above replace it as documented limitations.

The IMPORTS edge is the one schema addition (label, File→File, both
endpoints in-source else drop+count). Rationale: it is the broadest,
cheapest metadata ("广泛"), it makes the file-level dependency graph
queryable in every language, and the loader's INJECTS-only cycle filter
already isolates it from cycle grading. Java gains it too (its import
statements are already parsed for FQN resolution — marginal cost), so the
edge is uniform across all six languages. **User-confirmed 2026-10-20.**

**IMPORTS counting ruling (rev 3 addendum, reconciles the T3/T4 build
deviations into one rule):**

- import target is **external** (bare npm specifier, external Go package,
  external crate, external java package) → **no edge, no count** — an
  external import is not a repo reference (same philosophy as the builtin
  filter, spec §3.1.3);
- import target **resolves in-repo** but no symbol/identifier binds
  (Go identifier-use miss, rust `use a::X` with X unknown, python
  from-import of an unknown name, re-export beyond one hop) → **no edge +
  `unresolved_refs` +1** — a name-resolution failure inside the repo is
  exactly what that counter exists for;
- an edge whose endpoints would dangle (target file not a declared node)
  → `dropped_edges` via the standard Graph_out rule.

This replaces the earlier per-language "counted" wording in §5's go cell;
extractors are brought to this rule in T5.

## 6. CALLS resolution ladder (best-effort + honest counting)

All extractors share the v2 discipline: unresolved receiver → drop +
`unresolved_refs`; resolved receiver but ambiguous target (multi-match on
name+arity) → drop + `dropped_edges`; never guess, never dangle. Capability
per language, honest ladder:

- **rust** (most reliable): `use`-resolved paths and fully-qualified paths;
  `impl` blocks give owner directly; kind `direct` always (no dynamic
  dispatch at the syntactic layer — trait-object calls get kind
  `virtual-dispatch` when the receiver is `dyn Trait` or `impl Trait`).
- **go**: receiver type from declared vars (params, `:=`/`var` with explicit
  or literal-inferrable types, struct fields, package vars); package-level
  functions via import resolution; interface-typed receiver → kind
  `virtual-dispatch`, else `direct`.
- **java**: v2 rule, unchanged (scope-based receiver resolution, declared
  type FQN + name + arity).
- **typescript**: annotated parameter/variable types, class hierarchy
  self-type for `this.x()`, module-scope functions; kind per declared type
  (interface → `virtual-dispatch`).
- **python**: `self.x()` / `cls.x()` resolved against the owner type's
  in-source MRO closure; module-level functions by name; `from`-imported
  symbols; `ClassName.x()` static calls when the class is in-source; bare
  calls to unknown names → drop+count.
- **javascript** (least reliable): module-scope functions, imported
  bindings, `this.x()` inside classes; everything else (free dynamic calls)
  → drop+count. Expect high `unresolved_refs` on real JS codebases — this is
  the honest floor, not a failure.

## 7. Detection, config, and the v2 `not-framework` semantics

- New config key **`structure_languages`** (config.example.json + README):
  list of language names (§4 table). Default: ALL shipped —
  `["java", "python", "javascript", "typescript", "go", "rust"]`. Unknown
  values warn into `perception.log` + ignored (structure_extensions
  precedent). Empty list = structure tier fully off (tier skips, footnote
  wording mirrors the extensions-off case).
- **Detect gate rework** in perceive.py: today "has Java + Spring markers →
  adapter flow, else marker-only Java → structure mode, else not-framework".
  v3: walk the repo once for the extensions of every ENABLED language; the
  structure tier runs when ANY enabled language has ≥ 1 source file
  (respecting §4 exclusions). `not-framework` survives only as the
  "no enabled language present" outcome (report wording: skipped, unchanged
  footnote mechanism).
- The springboot extension tier keeps its own Spring-marker detection on top
  of java (unchanged); its `structure_extensions` gate is orthogonal to
  `structure_languages`.
- `analyzer.py --languages` is fed by perceive.py from the config ∩ detected
  set (the tier ladder stays: detect in perceive.py, analyzer re-walks).
- **Summary line** (run.md Step 5.5 table): structure mode wording becomes
  multi-language aggregate + language list, e.g.
  `🧭 Deep structural analysis: {files} files, {types} types, {methods}
  methods, {call_edges} call edges (java, python, ts).` — wording table in
  run.md gains one row; Chinese version mirrors it. Component mode
  (Spring) wording unchanged.

## 8. Output schema (structure.json v3)

- Nodes: Type/Method gain `props.lang`; File already has `language` (aliased
  as `lang` for query uniformity — both written).
- New edge label `IMPORTS` (File→File, props: none in v3).
- `IMPLEMENTS` edges gain `props.kind` (`declared` / `method-set`); Go
  method-set edges additionally carry `props.receiver`
  (`value` / `pointer`, §5.1).
- `meta.languages`: sorted set of languages with ≥ 1 emitted node (replaces
  v2's static `["java"]`).
- `meta.structure` gains **`by_lang`**:
  `{lang: {files, types, methods, call_edges}}`; the existing top-level
  counters remain aggregate sums (backward compat with v2 assertions).
- `meta.structure` gains `import_edges` count and
  `go_unverifiable_satisfaction` count (§5.1 honest-miss counter).
- Top-level `unresolved`, bean counters, `meta.beans/inject_edges/ambiguous`
  unchanged (java+sprint only).

## 9. Loader / graphmcp deltas (Go)

- **Loader**: (a) IMPORTS edges pass through as-is (label-agnostic load,
  already); (b) **verified no `lang` property index needed** (T7 spike,
  2026-10-20: golang-complex snapshot, 29/29 nodes hit by
  `MATCH (n {lang:'go'})`) — snapshot `{prop}` queries fall back to a node
  scan for unindexed keys (vectra v0.6 `anchorCandidates`/`nodePred`), so
  `MATCH (n {lang:'python'})` works from v3 snapshots as-is; (c) cycle
  grading filter already INJECTS-only — new labels can't leak in; loader
  tests extended with an IMPORTS-bearing fixture.
- **graphmcp**: zero code change expected; test matrix gains lang-anchored
  and IMPORTS-traversal Cypher queries.

## 10. Fixtures (perception/fixture/)

- **Flip** `python-complex`, `golang-complex` from skip-path assertions to
  positive ground truths: hand-counted nodes/edges per fixture, asserted as
  multisets (v2 precision-contract style, scoped per language).
  `golang-complex` ground truth includes the §5.1 IMPLEMENTS multiset
  (pointer-receiver case, promotion case, honest-miss case).
- **New fixtures**: `ts-complex` (TS + JS mixed: classes, interfaces,
  implements, imports, this-calls), `rust-complex` (crate with nested mods,
  traits + impls, use-paths), `mixed-polyglot` (java + python + ts in one
  tree — single structure.vg, lang-anchored Cypher census).
- Java fixtures and the Spoon crosscheck: untouched.

## 11. Acceptance criteria

1. **Spike gate (before full build)**: `python-complex` green end-to-end
   through new analyzer + loader + graphmcp (lang-anchored Cypher returns
   the hand-counted nodes) — then the remaining extractors start.
2. Per-language ground truth: python/golang/ts/rust fixtures assert node and
   edge multisets, `by_lang` counts, `unresolved_refs` /
   `dropped_edges` ≥ their hand-frozen floors (honesty assertions).
   golang-complex asserts the §5.1 IMPLEMENTS multiset exactly, plus
   `go_unverifiable_satisfaction` = its frozen count.
3. `mixed-polyglot`: one structure.vg; `MATCH (n {lang:'python'})` and
   IMPORTS traversal both correct; java bean assertions still pass.
4. Java regression: v2 fixture suite + Spoon crosscheck green, byte-identical
   bean/inject multisets; structure counts may gain `import_edges` only.
5. Gate matrix: default config → all six languages analyzed; explicit
   `structure_languages: ["java"]` → python-only repo skipped with footnote;
   empty list → tier off; unknown language value → warn+ignore, logged.
6. Repo with no enabled language (e.g. only `.rb`) → `status: skipped`,
   exit 0, no structure.json/.vg — the skip path stays clean.
7. graphmcp from a v3 snapshot: `repos`/`cypher`/`get_node`/`refresh`
   unchanged; lang-anchored queries work from snapshot (not just live
   build, and without any lang index — §9(b)).
8. Perf: each new fixture < 10s in the analyzer; uv sync still < 60s first
   run (5 new pinned grammars are small).
9. install.sh whitelist already ships `analyzer/` wholesale — verify
   uv.lock update rides along; no installer change expected.
10. run.md Step 5.5 wording table (en + zh) gains the multi-language
    structure-mode row; config.example.json gains `structure_languages`;
    CHANGELOG entry with the config migration note (new key, default all).

## 12. Out of scope (v3)

- Framework enrichment beyond springboot (no FastAPI/Django/Express/Nest
  layers — the registry is ready, the extensions are not).
- Incremental re-parsing (whole-repo per run; scv's commit-level skip
  remains the only incrementality — file-level SHA256 caching is a separate
  future spec, flagged in the Graphify comparison).
- Embedding/vector layer (v2 §9 deferral carried forward unchanged).
- Go implicit interface satisfaction **approximations** (§5.1): simple-name
  param/result type comparison (`pkg.Foo` vs local `Foo`), and unverifiable
  coverage via external embedded types — both frozen in the golang fixture,
  not silently absorbed.
- Symbol-level JS re-export chains (`export { x } from './y'` — y's re-export
  target beyond one hop is dropped+counted).
- Kotlin (perceive.py already walks `src/main/kotlin`; extractor deferred).
