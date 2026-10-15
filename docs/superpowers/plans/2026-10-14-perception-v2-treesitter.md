# Perception v2 (tree-sitter) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Spoon-on-JVM perception engine with a uv+Python+tree-sitter engine: language-agnostic structure graph (File/Type/Method + CONTAINS/CALLS/IMPLEMENTS/EXTENDS/REFERENCES) plus a `springboot` extension point (Bean/INJECTS) gated by `structure_extensions` config, stored/queryable through the existing Vectra chain unchanged.

**Architecture:** analyzer core parses with tree-sitter-java and emits the v2 schema (spec §4/§5/§6); extensions enrich (springboot = name table + syntactic resolution rules); perceive.py drops the JDK/dist tiers and invokes the analyzer via uv; loader gains an INJECTS-only cycle filter; graphmcp/install/docs follow. Spoon adapter stays as test-time cross-validation.

**Tech Stack:** Python 3.11+ (uv project, tree-sitter + tree-sitter-java pinned, pytest dev-dep), Go (loader/graphmcp unchanged apart from the filter), existing perception test suites.

**Spec:** `docs/superpowers/specs/2026-10-14-perception-v2-treesitter-design.md` (rev 2 — binding; carries the name table, key rules, multiset definition, footnote texts, dangling-refs table).

## Global Constraints

- perceive.py stdout JSON contract + exit-0-always (spec 2026-10-13 §5.1) unchanged.
- Vocabulary rules (spec 2026-10-13 §1.1): uv/Python naming follows the runtime-wording ruling (spec v2 §1).
- Edges only between source-declared nodes; drops counted (spec §4).
- Precision contract: §5 multiset equality over the 5 frozen fixtures — fixtures and their Spoon regression test are NOT modified.
- Every commit leaves: analyzer pytest green (via uv), loader/graphmcp go tests green, perceive suite green (rewritten progressively — keep the suite green per commit by migrating tests in the same task that changes behavior).
- Footnote texts verbatim from spec §7 (uv-missing wording uses the runtime-named form).

---

### Task 1: analyzer skeleton + structure extraction + spike gate

**Files:**
- Create: `perception/analyzer/pyproject.toml`, `perception/analyzer/uv.lock` (uv lock), `perception/analyzer/analyzer.py`, `perception/analyzer/structure/model.py`, `perception/analyzer/structure/java_extractor.py`, `perception/analyzer/tests/test_extractor.py`, `perception/analyzer/tests/snippets/*.java`
- Consumes: spec §3 (source-root discovery), §4 (nodes/edges/keys/FQN rules)

**Interfaces (produces, exact):**
- CLI `analyzer.py <repo_dir> <out_json>` → writes Vectra-batch JSON `{"nodes":[...],"edges":[...],"unresolved":0,"meta":{...}}` (meta per spec §6 with `extensions: []`), exit 0 on success, non-zero + stderr on usage/IO errors.
- `structure/model.py`: `Graph_out` builder with `add_node(key,label,props)`, `add_edge(src,dst,label,props)` (drop+count when endpoint missing), `add_call(...)`, `meta` dict, `to_json()`.
- `structure/java_extractor.py`: `extract(repo: Path, out: Graph_out) -> None`; exports `discover_source_roots(repo)`, `parse_java_file(path)`, `fqn_index(files) -> {simple_name: fqn}` helpers.

- [ ] **Step 1: uv project** — `pyproject.toml`: `requires-python = ">=3.11"`, deps `tree-sitter>=0.25,<0.26`, `tree-sitter-java>=0.23,<0.24`; `[dependency-groups] dev = ["pytest>=8"]`. `uv lock` + `uv sync`; `uv run pytest` works on an empty test.
- [ ] **Step 2: failing extractor tests (TDD)** — snippets covering: package+import FQN (single + wildcard + same-package), nested class `$`, record/enum/interface/annotation kinds, method key normalization (generics erased, varargs/array `[]`, overload collision counter), CONTAINS file→type, IMPLEMENTS/EXTENDS with both endpoints in-source, edge DROPPED when interface comes from an unresolvable import (counted), source-root discovery on a multi-module tree incl. `build/`/`src/test/` exclusions.
- [ ] **Step 3: implement** model + java_extractor per spec §4 (tree-sitter queries: package_declaration, import_declaration, class/interface/record/enum/annotation_declaration, method_declaration, constructor_declaration, superclasses/super_interfaces). FQN resolution: explicit imports → wildcard vs known simple names → same package; unresolved → skipped + `unresolved_refs`.
- [ ] **Step 4: green + fixture smoke** — `uv run pytest` green; `uv run python analyzer.py <torture-app fixture copy> /tmp/t.json` prints meta: files=33, types ≥30, methods ≥80; `jq '.meta'`-style check in the test.
- [ ] **Step 5: spike gate (spec §8.1)** — pipe `/tmp/t.json` through the loader (`go run` from perception/loader) to `/tmp/t.vg` and run one scv-graph-mcp/graphmcp-store Cypher (`MATCH (t:Type)-[:IMPLEMENTS]->(i {key:'type:com.acme.core.GreetingService'}) RETURN t.key` → `type:com.acme.core.GreetingServiceImpl`); record output in the report. Fix fallout if any.
- [ ] **Step 6: commit** `feat: perception v2 analyzer skeleton — tree-sitter structure layer + spike gate`.

### Task 2: CALLS + REFERENCES edges

**Files:** modify `java_extractor.py`; extend `tests/test_extractor.py` + snippets.
**Consumes:** spec §4 CALLS mapping rule (receiver declared type FQN + name + arity; multi-match → drop+count), REFERENCES, static-import drop rule, dispatch kind.
- [ ] Failing tests: method-invocation `foo.bar()` where `foo`'s declared type is a local/param/field with in-source FQN → CALLS `direct`; invocation on an interface-typed receiver → `virtual-dispatch` to the interface method key; arity/overload ambiguity → dropped+counted; `new Foo(...)` inside a method → REFERENCES; static-import usage dropped+counted.
- [ ] Implement (scope analysis: params, locals from declarators, fields of owner type chain, `this.` implied receiver).
- [ ] Fixture smoke: torture-app `call_edges` lands in 10–40; spot-check 3 known edges (e.g. `GreetingServiceImpl#greet` CALLS something in `JpaGreetingRepository`).
- [ ] Commit `feat: CALLS/REFERENCES with syntactic receiver resolution`.

### Task 3: springboot extension (beans + INJECTS)

**Files:** create `extensions/registry.py`, `extensions/springboot.py`; extend tests (`test_springboot.py` + snippets mirroring the trickiest fixture cases standalone).
**Consumes:** spec §5 in full (name table verbatim, meta-annotation walk in-source first, @Bean-produced suppression, qualifier/primary/by-name/list-unwrap, @EnableConfigurationProperties `.class` matching, multi-ctor selection, Lombok, conditions wording, java.*-via-import).
- [ ] registry: reads enabled list; unknown → warn+ignore; calls `springboot.enrich(structure, out)`.
- [ ] Failing tests per §5 bullet (one snippet per rule — reuse fixture classes copied into snippets): two-upper naming, @MyRestController in-source meta chain, @SpringBootApplication table chain, @Bean param + suppression, @Qualifier, @Primary collapse, byName, List unwrap, Optional unwrap, inherited @Autowired, multi-ctor @Autowired, @Value skip, conditions string, @EnableConfigurationProperties record.
- [ ] Implement; `analyzer.py` wires extension output into nodes/edges + meta (beans/inject_edges/ambiguous counts, top-level `unresolved`).
- [ ] Commit `feat: springboot language extension — bean graph on tree-sitter`.

### Task 4: fixture ground-truth suite (the multiset bar)

**Files:** create `analyzer/tests/test_fixtures.py` (+ tiny non-Spring java fixture dir under `analyzer/tests/fixtures/plain-java/`).
**Consumes:** spec §5 multiset definition, frozen fixture counts.
- [ ] Expected multisets embedded as literals (12/8, 30/24 with 4 amb + cycle pair, 15/10 with 4 amb, 12/6, 6/4) with per-edge tuples (src,dst,via,qualifier,wraps,ambiguous) and bean (key,stereotype,conditions) tuples — hand-derived from the existing Spoon outputs (run the old engine once to dump them, paste as literals).
- [ ] Assertions: multiset equality per fixture; structure meta bounds (torture: files=33, call_edges 10–40, key_collisions reported); plain-java fixture → structure-mode only (no Bean label) and detect passes via marker-file rule.
- [ ] Green; commit `test: v2 fixture ground-truth suite (5 fixtures + plain-java)`.

### Task 5: perceive.py v2 + config + unit-test rewrite

**Files:** modify `perception/perceive.py`, `config.example.json`, rewrite `perception/tests/test_perceive.py` (+ integration tests updated in Task 7).
**Consumes:** spec §6 counts rules, §7 runtime table + footnote texts + dangling table.
- [ ] Delete ensure_dist/adapter_source_hash/resolve_mode/cp-init dependency/MANIFEST run; add `probe_uv()` (uv on PATH + `uv run --project analyzer python -c pass` probe), `run_analyzer(repo, out, analysis_dir)` (timeout, stderr tail → perception.log), extension gating from config (`structure_extensions`, default ["springboot"]); detect unchanged except empty-extension marker-only path; footnotes FOOTNOTES updated to the v2 set verbatim (uv-missing runtime-worded, analyzer-failed reworded); counts label-filtered (Bean/INJECTS) + structure sub-counts + structure-mode summary wording en/zh constants; mcp tiers unchanged.
- [ ] config.example.json: + `structure_extensions: ["springboot"]`, − `structure_build_execution`.
- [ ] Rewrite unit tests to the v2 tier set (detect incl. empty-extension marker path, uv probe parsing, footnote texts, cleanup, counts filtering via a fake analyzer JSON).
- [ ] Green; commit `feat: perceive.py v2 — uv analyzer tier, extension gating, v2 footnotes`.

### Task 6: loader INJECTS filter + graphmcp docs + installer

**Files:** modify `perception/loader/main.go` (+ test), `perception/graphmcp/main.go` (description text only), `install.sh`, `perception/README.md`, `perception/DEVELOPERS.md`.
- [ ] Loader: grade cycles from INJECTS edges only (`c.Edges` filtered by edge label before via-inspection; a cycle whose members connect via CALLS only is still reported in cycles[] but graded from INJECTS edges — per spec §7 table; add a go test: mixed CALLS+INJECTS cycle grades from INJECTS).
- [ ] graphmcp cypher tool description: mention Type/Method nodes and CALLS/IMPLEMENTS edges.
- [ ] install.sh Step 4.5: whitelist = perceive.py, README.md, loader/, graphmcp/, analyzer/ (incl. uv.lock, excl. tests), MANIFEST detect-only copy; adapter sources/build files NO longer shipped.
- [ ] README/DEVELOPERS rewritten for v2 (no-JDK path, extensions config, crosscheck).
- [ ] go tests + `bash -n` + vocabulary grep green; commit `feat: loader INJECTS cycle filter, installer ships analyzer, v2 docs`.

### Task 7: integration rewrites + full acceptance

**Files:** rewrite `perception/tests/test_perceive_integration.py`; `Makefile` (+ `crosscheck` target running both engines, multiset diff via a small python script `tools/crosscheck.py`); run the acceptance matrix.
- [ ] Integration tests v2: local torture run via temp uv project env `SCV_PERCEPTION_ANALYZER` override (defaults to repo path) — status ok, counts 30/24 + structure counts, vg tier, remote == local (no build execution anywhere), not-framework silent, extension-off run (temp config) → no beans, uv-missing path (PATH stripped) → footnote + exit 0.
- [ ] Query matrix: unchanged file, must pass unmodified.
- [ ] No-JDK run (PATH without java) → full tier chain to mcp hint.
- [ ] `make crosscheck` green on 5 fixtures (multiset diff empty for covered scope).
- [ ] Acceptance matrix spec §8 items 1–10 recorded in report; fix fallout.
- [ ] Commit `feat: v2 integration suite + crosscheck + acceptance pass`.

---

## Self-Review

- Spec coverage: §3→T1, §4→T1/T2, §5→T3/T4, §6→T5, §7→T5/T6 (danglers table each mapped), §8→T4/T7 (+spike T1), §2 constants→all.
- No placeholders; fixture counts from validated Spoon baselines; multiset literals derived by running the old engine once (T4 step 1) rather than hand-typed from memory.
- Type consistency: analyzer CLI args, `Graph_out` API, footnote keys, config key names cross-checked against spec tables.
