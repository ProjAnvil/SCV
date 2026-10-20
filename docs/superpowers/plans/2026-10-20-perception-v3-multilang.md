# Perception v3 (multi-language tree-sitter) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the v2 tree-sitter structure layer from Java-only to six languages (java, python, javascript, typescript, go, rust), so every analyzed repo leaves `~/.scv/analysis/{repo}/structure.json` + `structure.vg` — nodes carry `lang`, a new `IMPORTS` (File→File) edge, Go gains syntactic method-set `IMPLEMENTS` — behind a `structure_languages` config gate (default: all on), with the existing loader/graphmcp chain unchanged apart from a `lang` property index.

**Architecture:** five new extractors under `perception/analyzer/structure/` share the v2 `Graph_out` accumulator and the graphify-aligned two-pass CALLS design (spec §3.1); `analyzer.py` gains `--languages` dispatch; perceive.py's detect gate becomes extension-driven (`structure_languages`); loader adds the `lang` property index; fixtures flip python/golang to positive ground truths and add ts/rust/mixed-polyglot.

**Tech Stack:** Python 3.11+ uv project (+5 pinned tree-sitter grammars), Go (loader/graphmcp), existing test suites (pytest + go test + fixture check.py).

**Spec:** `docs/superpowers/specs/2026-10-20-perception-v3-multilang-design.md` (rev 3 — binding; §4 key schemes, §5 edge table, §5.1 Go method-set matching, §3.1 graphify alignment, §8 schema, §11 acceptance).

## Global Constraints

- perceive.py stdout JSON contract + exit-0-always unchanged (v2 spec §2).
- Edges only between source-declared nodes; drops counted; never guess, never dangle.
- Java behavior regression-free: v2 fixture suite + Spoon crosscheck green; structure counts may gain `import_edges` only.
- Every commit leaves: analyzer pytest green (uv), loader/graphmcp go tests green, perception suite green, `make test` fixture check green (migrated in the same task that changes behavior).
- Language blocklists are per-language and bare-identifier-scoped (spec §3.1.3).
- No build tools, no execution inside analyzed repos, `local`/`remote` identical.

---

### Task 1: language table + analyzer dispatch + python extractor + spike gate

**Files:**
- Modify: `perception/analyzer/pyproject.toml` (+`tree-sitter-python`), `perception/analyzer/uv.lock` (uv lock), `perception/analyzer/analyzer.py` (`--languages`), `perception/analyzer/structure/model.py` (LANGUAGES, `lang` prop, IMPORTS counter helper)
- Create: `perception/analyzer/structure/langs.py` (canonical language table: name → suffixes, exclusions), `perception/analyzer/structure/python_extractor.py`, `perception/analyzer/tests/test_python_extractor.py`, `perception/analyzer/tests/snippets/python/*.py`
- Consumes: spec §3.1 (two-pass CALLS, blocklists), §4 (python row: package roots, dotted module path, arity param normalization), §5 (python edge row), §8

**Interfaces (produces, exact):**
- CLI: `analyzer.py <repo> <out_json> [--extensions EXT...] [--languages LANG...]` (default languages: all). Unknown language → stderr warn + ignored, echoed in `meta.languages_requested`.
- `structure/langs.py`: `LANGUAGES: dict[str, LanguageSpec]` with `suffixes: tuple[str,...]`, `walk(repo) -> list[Path]` honoring spec §4 exclusions; `DEFAULT_LANGUAGES` list. THE canonical table — perceive.py (T6) mirrors it, not the reverse.
- `python_extractor.extract(repo, out) -> list[PythonFile]`.
- `model.py`: `Graph_out` nodes gain `lang` prop convention (extractors pass it); `add_import(src, dst)` helper; `meta.languages` becomes the emitted-language set.

- [ ] **Step 1: deps** — `uv add 'tree-sitter-python>=0.23'`; `uv lock`; `uv run pytest` still green.
- [ ] **Step 2: failing tests (TDD)** — python snippets covering: package/module path keys (`analytics.model.event` → `type:analytics.model.event::Event`… wait — spec §4 says `type:<dotted.module.Class>`; use that form verbatim), class/def/async-def Method keys with arity normalization (`fn:<module>#<name>(<n>)`), CONTAINS File→Type, IMPLEMENTS on class bases in-source, CALLS: `self._dedupe()` → owner's method, module-level call, `from .io.source import Loader` + `Loader()` → REFERENCES, bare unknown call → unresolved_refs, builtin bare calls (`len(...)`) never resolve (blocklist), mutual recursion `Pipeline._dedupe ↔ Pipeline._flatten` both CALLS edges emitted, IMPORTS File→File for resolved relative imports, `__init__.py` re-export import → IMPORTS + no dangling.
- [ ] **Step 3: implement** — langs.py + python_extractor per spec §3.1: pass 1 per-file (class/def tables, import tables, method bodies), pass 2 cross-file binding; decorators recorded as props; dataclass/ABC/enum → Type kinds.
- [ ] **Step 4: green + fixture smoke** — pytest green; `uv run python analyzer.py perception/fixture/repos/python-complex /tmp/py.json --languages python` → meta: files=4, types ≥ 8, methods ≥ 15; `meta.languages == ["python"]`.
- [ ] **Step 5: spike gate (spec §11.1)** — pipe `/tmp/py.json` through the loader (`go run perception/loader/main.go /tmp/py.json /tmp/py.vg`, matching its real CLI) then one graphmcp Cypher against the vg (reuse the v2 query-matrix JSON-RPC pattern): `MATCH (n {lang:'python'}) RETURN count(n)` > 0 AND `MATCH (a {key:...Pipeline})-[:CALLS*1..2]->(b) RETURN DISTINCT b` non-empty. If graphmcp needs the lang index to answer from a snapshot, that's T7's loader change — record it, don't fix here.
- [ ] **Step 6: commit** `feat: perception v3 T1 — language table, --languages dispatch, python extractor, spike gate`.

### Task 2: TS/JS extractor (jsts)

**Files:** Modify `pyproject.toml` (+`tree-sitter-javascript`, +`tree-sitter-typescript`), `uv.lock`; create `structure/jsts_extractor.py`, `tests/test_jsts_extractor.py`, `tests/snippets/ts/*`.
- [ ] Failing tests: TS `interface` + `implements` clause → IMPLEMENTS (kind: declared); `extends` → EXTENDS; class methods + `this.x()` → CALLS direct; annotated param type call → CALLS with virtual-dispatch when receiver is interface-typed; module-scope function call → CALLS; `import { X } from './y'` → IMPORTS File→File + type binding; bare import (npm) → dropped+counted; JS untyped `this.x()` on class → CALLS; JS free call unknown → unresolved_refs; export/default handling for Type keys (`type:<module>::<Name>`); re-export one-hop IMPORTS, second hop dropped+counted; `require()` in JS → IMPORTS when relative.
- [ ] Implement (two grammars, one module; TS `import type` → REFERENCES only, no IMPORTS binding).
- [ ] Smoke on a snippets "mini-repo" (tmp tree, 3 files) — counts spot-checked.
- [ ] Commit `feat: v3 T2 — TypeScript/JavaScript extractor`.

### Task 3: Go extractor + method-set IMPLEMENTS

**Files:** Modify `pyproject.toml` (+`tree-sitter-go`); create `structure/go_extractor.py`, `tests/test_go_extractor.py`, `tests/snippets/go/*.go`.
- [ ] Failing tests — structure: package path keys (`type:pkg/queue.Queue`), method_declaration receivers → Method nodes + owner, CONTAINS; interfaces: `method_elem` → Method nodes; embedded interface union; `type_spec` with `struct_type` embedding → EXTENDS; type-set constraint (`A | B`, `~T`) → REFERENCES not IMPLEMENTS.
- [ ] Failing tests — method-set matching (spec §5.1): value-receiver full coverage → IMPLEMENTS kind: method-set receiver: value; pointer-receiver-only coverage → receiver: pointer; struct embed promotion earns coverage; external embed → no edge + `go_unverifiable_satisfaction` counted; simple-name param mismatch → no edge.
- [ ] Failing tests — CALLS: package-qualified call via import resolution; receiver-typed call from explicit `var x T` / param / field; interface-typed receiver → virtual-dispatch; bare predeclared (`append`, `len`) never resolve (full spec list blocklist); `x := pkg.New()` inferred concrete receiver → direct.
- [ ] Failing tests — IMPORTS: import resolves to in-repo package; edge lands on files declaring identifiers actually used; no identifier match → no edge + counted.
- [ ] Implement; smoke: `uv run python analyzer.py perception/fixture/repos/golang-complex /tmp/go.json` — hand-verify the fixture's known pairs (store/worker/queue).
- [ ] Commit `feat: v3 T3 — Go extractor with method-set IMPLEMENTS`.

### Task 4: Rust extractor

**Files:** Modify `pyproject.toml` (+`tree-sitter-rust`); create `structure/rust_extractor.py`, `tests/test_rust_extractor.py`, `tests/snippets/rust/`.
- [ ] Failing tests: crate root discovery (`Cargo.toml` → `src/main.rs`/`src/lib.rs`), `mod` tree walk (`mod foo;` → `foo.rs` / `foo/mod.rs`), `type:crate::module::Item` keys; `impl Trait for Type` → IMPLEMENTS kind: declared; inherent `impl Type` methods → Method nodes; trait method signatures → Method nodes; `use` resolution → CALLS/REFERENCES + IMPORTS File→File; path call `foo::bar()` → CALLS direct; `dyn Trait` receiver → virtual-dispatch; macro calls (`vec!`) dropped+counted.
- [ ] Implement; smoke on snippets mini-crate.
- [ ] Commit `feat: v3 T4 — Rust extractor`.

### Task 5: java IMPORTS + meta completion + mixed-repo schema

**Files:** Modify `structure/java_extractor.py`, `structure/model.py`, `analyzer.py`, `tests/test_extractor.py` (java), new `tests/test_mixed.py`.
- [ ] Failing tests: java single-type import resolving to in-source file → IMPORTS (wildcard → none); `meta.structure.by_lang` per-language counts correct on a mixed tmp repo (java + python snippets); aggregate counters = sums; `meta.languages` sorted set; `import_edges` counted; IMPLEMENTS edges carry `kind: declared` (java/rust) — v2 java tests updated in the same commit.
- [ ] Implement; java fixture suite (test_fixtures.py) still green — structure counts unchanged except `import_edges`.
- [ ] Commit `feat: v3 T5 — java IMPORTS, by_lang meta, mixed-repo schema`.

### Task 6: perceive.py detect rework + config + run.md

**Files:** Modify `perception/perceive.py`, `config.example.json`, `skills/en/references/run.md`, `skills/zh-cn/references/run.md`, `perception/tests/test_perceive.py`.
- [ ] Failing tests: multi-language detect (python tree → tier runs, `--languages` fed from config ∩ detected); `structure_languages` gate (default all; `["java"]` → python repo skipped w/ footnote; empty → tier off; unknown value warn+log); summary line wording en/zh (multi-language structure mode, spec §7); `not-framework` only when no enabled language present.
- [ ] Implement — detect walk mirrors `structure/langs.py` table (perceive.py keeps its own walk code, v2 precedent); analyzer invocation gains `--languages`.
- [ ] config.example.json + run.md Step 5.5 wording table row (en+zh); CHANGELOG unreleased section.
- [ ] Fixture check.py: flip EXPECT lists — python/golang move to EXPECT_STRUCTURE (ground-truth counts asserted: files/types/methods floors + by_lang presence); keep one deliberately-skipped repo (add `ruby-plain` tiny fixture, or reuse a no-language tmp tree in tests).
- [ ] `make test` green end-to-end; commit `feat: v3 T6 — perceive detect rework, structure_languages gate, fixture flip`.

### Task 7: loader lang index + graphmcp matrix

**Files:** Modify `perception/loader/main.go` (+test), `perception/graphmcp/graphmcp_test.go`.
- [ ] Loader: property-index declarations gain `lang` (snapshots answer `MATCH (n {lang:'python'})`); loader go test with IMPORTS-bearing + multi-lang fixture JSON.
- [ ] graphmcp test matrix: lang-anchored query from snapshot; IMPORTS traversal `MATCH (f:File)-[:IMPORTS]->(g) RETURN`; get_node on a python Type.
- [ ] Commit `feat: v3 T7 — loader lang property index, graphmcp query matrix`.

### Task 8: new fixtures + acceptance matrix

**Files:** Create `perception/fixture/repos/ts-complex/`, `rust-complex/`, `mixed-polyglot/`; extend `check.py`.
- [ ] ts-complex (TS + JS: interfaces, implements, this-calls, imports, one re-export chain), rust-complex (nested mods, traits + impls, use-paths, one dyn receiver), mixed-polyglot (java + python + ts) — hand-counted ground truths in check.py (multiset style, per spec §11.2/11.3); golang-complex gains the §5.1 IMPLEMENTS multiset (pointer case, promotion case, honest-miss case + frozen `go_unverifiable_satisfaction`).
- [ ] Full `make test` (analyzer pytest + perception suite + go tests + fixture check, both extension modes) green.
- [ ] Acceptance matrix spec §11 items 1–10 executed and recorded; fix fallout.
- [ ] Commit `feat: v3 T8 — ts/rust/mixed fixtures, acceptance pass`.

### Task 9: docs + CHANGELOG + installer verify

**Files:** Modify `perception/README.md`, `perception/DEVELOPERS.md`, `CHANGELOG.md`, `install.sh` (verify only).
- [ ] README/DEVELOPERS: six-language table, `structure_languages`, Go method-set note + honest approximations, IMPORTS edge, examples.
- [ ] CHANGELOG unreleased entry (new config key, default-all migration note, Java IMPORTS additive note).
- [ ] Verify `install.sh` ships analyzer/ + uv.lock unchanged (`bash -n` + vocabulary grep green).
- [ ] Commit `docs: v3 — six-language structure layer, config and changelog`.

---

**Execution order is strictly sequential (T1 spike gates the rest).** T2/T3/T4 are independent of each other after T1 but share model.py conventions — run in ID order. T6 depends on T1–T5; T7 on T5; T8 on all; T9 last.
