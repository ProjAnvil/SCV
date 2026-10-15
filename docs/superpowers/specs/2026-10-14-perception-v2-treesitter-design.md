# Perception v2 — uv + Python + tree-sitter Engine — Design Spec (rev 2)

- Date: 2026-10-14 (rev 2: incorporates subagent audit — 2 blockers, all majors)
- Status: audited; ready for planning
- Supersedes (partially): the runtime path of 2026-10-13-code-perception-design.md
  rev2 — the Spoon adapter is retained as a test-time cross-validation engine,
  but the DEFAULT user-facing flow no longer requires a JVM.
- Research basis: session research report "代码结构入 Vectra 对检索的收益 与
  图/边定义" + 5 validated fixtures with ground truth. Audit verdict: no fixture
  case is beyond a syntactic strategy (package+imports FQN, syntactic
  implements/extends, name tables, annotation surface).

## 1. Motivation & positioning

Perception v1 runs structure extraction in Java (Spoon): correct, but a JDK +
Gradle is a hard runtime dependency of `/scv run`'s optional tier, and it only
knows Spring beans. Perception v2 moves extraction to **uv + Python +
tree-sitter**:

- **No JVM on the default path** — the JDK tier disappears from the user's
  ladder.
- **Language-agnostic core** — tree-sitter structure extraction works
  per-grammar; v1 ships Java, the architecture is per-language.
- **Language extension points** — framework enrichment (Spring beans) plugs in
  via a registry; the user toggles them in `~/.scv/config.json`
  (`structure_extensions: ["springboot"]`).

Vocabulary ruling (per spec 2026-10-13 §1.1): `uv`/`uv.lock` are addresses
(exempt); the missing-runtime footnote names the runtime, not the tool —
「未检测到 Python 3.11+ 运行环境」/ "Python 3.11+ runtime not found", following
the JDK/Go precedent.

## 2. What stays unchanged

- **stdout JSON contract** (2026-10-13 §5.1): single line, exit 0 always,
  `{status, tiers, reason, counts, artifacts, mcp_add, footnote}`. `counts`
  gains `structure` (§6).
- **Tier ladder** outside the analyzer: config check → detect → stale cleanup →
  analyzer (uv) → structure.json → Go tier (loader + graphmcp) → MCP hint.
  JDK/dist tiers DELETED from the default path (§7 dangling table).
- **Security**: the analyzer never executes anything inside the analyzed repo;
  `--source remote` and `local` behave identically (logging still records
  source for audit).
- **Artifacts**: `~/.scv/analysis/{project}/structure.json` (+ `.vg`),
  `perception.log`.
- **Spoon adapter**: stays in-tree with its 5-fixture suite as the
  cross-validation engine — repo/test-time only, NOT shipped by install.sh,
  NOT in the default run flow. `structure_build_execution` is REMOVED from
  config.example.json (unknown keys in existing user configs are ignored;
  CHANGELOG carries the migration note).

## 3. Repository layout (delta)

```
perception/
├── perceive.py                 # orchestrator v2 (drops JDK/dist tiers)
├── analyzer/                    # NEW: uv project (tree-sitter engine)
│   ├── pyproject.toml           # deps: tree-sitter, tree-sitter-java (pinned); dev: pytest
│   ├── uv.lock                  # SHIPPED (repo precedent: eval/uv.lock)
│   ├── analyzer.py              # CLI: <repo> <out.json>
│   ├── structure/
│   │   ├── model.py             # node/edge builders, key schemes, dedup, collision counters
│   │   └── java_extractor.py    # tree-sitter-java → File/Type/Method + edges
│   └── extensions/
│       ├── registry.py          # gate via structure_extensions; unknown values warn+ignore (logged)
│       └── springboot.py        # bean + INJECTS extraction (syntactic)
├── loader/                      # ONE change: cycle grading filters INJECTS (§7)
├── graphmcp/                    # unchanged binary; cypher tool description gains new props
└── adapters/springboot/         # Spoon cross-validation engine (repo/test-time)
```

**Source-root discovery** (replaces v1's gradle-derived roots): walk the repo
(mirror perceive.py `_iter_source_roots`: module dirs to depth 3, source roots
to depth 6); every `src/main/java` directory is a root; exclusions: `build/`,
`.gradle/`, `target/`, `src/test/`, dot-dirs. Detection stays MANIFEST-driven
(marker files + content markers) exactly as v1; when `structure_extensions`
is EMPTY, the springboot detect gate degrades to marker-file-only Java
detection (any `pom.xml`/`build.gradle*` + at least one `src/**.java` file) so
the structure core still runs on non-Spring Java repos.

## 4. Structure layer (language-agnostic core, Java v1)

Nodes (label = type discriminator):

| label | key | props |
|---|---|---|
| `File` | `file:<repo-relative path>` | path, language, loc |
| `Type` | `type:<fqn>` | kind (class/interface/record/enum/annotation), modifiers, loc |
| `Method` | `fn:<type-fqn>#<name>(<param-normalized>)` | name, visibility, static, params, returns, loc, owner (type key) |

Key rules: nested classes use `$` (matches v1 output, e.g.
`GreetingController$Metrics`); param normalization — arrays/varargs → `[]`,
generics erased to raw simple names, e.g. `fn:com.x.S#create(List[])`;
collisions increment `meta.structure.key_collisions` (never silently merged).

Edges — **an edge is emitted only when BOTH endpoint keys are source-declared
nodes; otherwise dropped and counted in `meta.structure.dropped_edges`**
(this is what keeps `WebConfig implements WebMvcConfigurer` (jar interface)
from producing a dangling edge that makes the loader's Seal fail):

| label | from → to | props | notes |
|---|---|---|---|
| `CONTAINS` | File → Type | — | file/type level only; method ownership via `props.owner` + key prefix |
| `IMPLEMENTS` | Type → Type | — | syntactic clauses, both endpoints in-source |
| `EXTENDS` | Type → Type | — | syntactic, both endpoints in-source |
| `CALLS` | Method → Method | line, kind (`direct`/`virtual-dispatch`) | mapping rule: receiver's declared type FQN + method name + arity; multiple overloads match → drop+count |
| `REFERENCES` | Method → Type | line | types instantiated/cast/implemented-anonymously inside method bodies |

FQN resolution (syntactic): package declaration + single-type imports +
same-package fallback; wildcard imports resolve by simple-name match against
known source types; static imports do not contribute CALLS edges (drop+count);
unresolved receiver names increment `meta.structure.unresolved_refs`
(receiver-FQN resolution failures only — the dropped-call counter above is
separate).

## 5. Spring Boot extension (`springboot`)

Gated by `structure_extensions` containing `"springboot"` (default
`["springboot"]`). Runs on top of the structure layer.

**Annotation name table** (the terminal-stereotype mapping; annotation
resolution: import, same-package, OR fully-qualified inline form — e.g.
`@org.springframework.stereotype.Component` on the nested `Metrics` class):

- Stereotypes (terminal): `Component`, `Service`, `Repository`, `Controller`,
  `RestController`, `ControllerAdvice`, `RestControllerAdvice`, `Configuration`
  → stereotype = simple name.
- Meta chains in the table (source-defined chains like `@MyRestController`
  resolve by walking in-source `@interface` annotations first; the table covers
  jar-carried chains): `SpringBootApplication → Configuration` (chain:
  SpringBootConfiguration → Configuration), `SpringBootConfiguration →
  Configuration`, `EnableAutoConfiguration` (no stereotype),
  `ConfigurationProperties` (stereotype `ConfigurationProperties` when directly
  annotated), `EnableConfigurationProperties` (registration, see below).
- Behavioral annotations: `Autowired`, `Qualifier`, `Primary`, `Value`, `Bean`,
  `Profile`, `ConditionalOn*` (prefix match → conditions props),
  `RequiredArgsConstructor` (Lombok), `NonNull` (Lombok).
- Spring Data family (imports resolved to FQN):
  `org.springframework.data.repository.Repository`,
  `CrudRepository`, `PagingAndSortingRepository`,
  `org.springframework.data.jpa.repository.JpaRepository` → interface whose
  extends clause resolves into this family = `data-repository` stereotype.
  **Deviation-allowed, no fixture coverage** (documented; excluded from the
  crosscheck diff scope).

**Beans**: stereotype classes (transitive meta walk: in-source chains first,
then the table), `@Bean` methods (incl. static), `@ConfigurationProperties`
classes registered via `@EnableConfigurationProperties(X.class)` (syntactic
`.class` argument match against source types), name rules (explicit value >
decapitalized simple name; two-leading-uppercase preserved).

**INJECTS edges** (Bean → Bean, props via/qualifier/wraps/ambiguous):
constructor params (@Value skipped; multi-ctor: `@Autowired`-annotated wins,
else sole non-default, else skip+log), Lombok `@RequiredArgsConstructor`
final/`@NonNull` fields, `@Autowired` fields (own + superclass walk in source),
setters, `@Bean` method params. **A class whose type is produced by a `@Bean`
method contributes NO class-level injection points** (its dependencies are the
method params — prevents the v1 double-count, torture 24 not 26).

**Resolution** (syntactic): candidates = beans whose type FQN EQUALS the
injected type FQN or transitively implements/extends it via in-source edges
(the equality case covers `@Bean Clock clock()` consumed as `java.time.Clock`
— import-resolvable). Qualifier → bean-name match; `@Primary` collapse;
by-name fallback (field/param name == bean name); `List/Set/Collection/
Optional` unwrap (collections take ALL candidates, never ambiguous);
ambiguity flagged honestly. Conditions recorded as props with v1 wording.

**Precision contract ("EXACTLY")**: semantic multiset equality per fixture —
bean nodes matched on (key, stereotype, conditions) and edges on
(srcKey, dstKey, via, qualifier, wraps, ambiguous). Order, `at`/`source`
positional props, and the `data-repository` rule are OUT of scope. The same
definition bounds `make crosscheck` (Spoon vs tree-sitter diff).

## 6. Output schema (structure.json v2)

Vectra batch format as before, plus:

- Method node props include `owner`; bean nodes keep the v1 schema.
- Top-level `"unresolved"` (bean-layer int, backward-compat with the v1
  regression assertions) is retained.
- Top-level `"meta"`:
  `{"engine": "treesitter-python", "languages": ["java"], "extensions": [...],
  "structure": {"files": F, "types": T, "methods": M, "call_edges": C,
  "dropped_edges": D, "unresolved_refs": U, "key_collisions": K},
  "beans": B, "inject_edges": I, "ambiguous": A}`.
- perceive.py `counts`: with the springboot extension ON,
  `components`/`edges` are **filtered by label** (`Bean` nodes / `INJECTS`
  edges — not `len()` of the mixed arrays), keeping v1 report semantics.
  With ALL extensions OFF (structure mode), the summary line switches to
  `{types} types, {methods} methods, {call_edges} call edges`
  (en) / `{types} 个类型、{methods} 个方法、{call_edges} 条调用边` (zh).

## 7. Runtime, config, and dangling references from v1

| Item | v1 | v2 |
|---|---|---|
| Analyzer runtime | JDK 17 + Gradle dist (lazy build) | `uv run --project perception/analyzer python analyzer.py <repo> <out>`; uv ≥0.5, Python ≥3.11; `.venv` + uv cache are user-machine state; first `uv sync` target < 60s; fixtures analyze < 10s each |
| Tiers | JDK → dist → run | **analyzer (uv)** → json → Go → MCP |
| Config | `structure_analysis_enabled`, `structure_build_execution` | `structure_analysis_enabled` (unchanged) + `structure_extensions: ["springboot"]`; `structure_build_execution` REMOVED (migration note in CHANGELOG) |
| Footnotes | jdk-missing, adapter-build-failed, adapter-failed, degraded-noclasspath | `uv-missing` (new, runtime-named wording above), `analyzer-failed` (covers sync/run failures; reworded text drops "after fallback"); JDK footnotes retired |

**Dangling v1 references and their v2 disposition** (all resolved by the plan):

| Dangler | Disposition |
|---|---|
| MANIFEST `requires/build/run` | detect-only; run/build fields unused (kept for future deep tier) |
| install.sh Step 4.5 adapter build inputs | whitelist swaps them for `analyzer/` incl. `uv.lock`; adapter sources NOT shipped |
| `ensure_dist`/`adapter_source_hash`/`resolve_mode`/`cp-init.gradle` | deleted from perceive.py (gradle/maven/noclasspath modes collapse — tree-sitter needs no classpath) |
| test_perceive.py jdk/mode assertions | rewritten to the v2 tier set (uv probe, footnotes) |
| test_perceive_integration.py `mode=gradle` + gradle/java skip guards | rewritten: no build-tool execution to assert; skip guard = uv only |
| Loader "unchanged" | NOT unchanged: **cycle grading filters to INJECTS edges** (vectra Cycles is label-agnostic; CALLS mutual recursion must not reach the user's cycle summary). Loader tests extended. |

## 8. Acceptance criteria

1. **Spike gate (before full build)**: one fixture (torture-app) green
   end-to-end through the new analyzer + loader + graphmcp before the
   remaining tasks start.
2. Same 5 fixtures through the NEW engine reproduce v1 ground truths under the
   §5 multiset definition: 12/8, 30/24, 15/10 (4 amb), 12/6, 6/4 — asserted in
   `analyzer/tests/test_fixtures.py` (uv + pytest, tree-sitter only).
3. Structure-layer assertions on torture-app (recomputed from the fixture):
   files = 33 (java sources), types ≥ 30, methods ≥ 80, call_edges 10–40
   (in-source resolution only), `dropped_edges` reported,
   `IMPLEMENTS` edge `type:com.acme.core.GreetingServiceImpl` →
   `type:com.acme.core.GreetingService` present.
4. Full-chain query matrix re-passes unchanged (perceive v2 → .vg →
   scv-graph-mcp → per-stereotype Cypher census).
5. Extension gate: temp config `structure_extensions: []` → no Bean nodes, no
   INJECTS edges, structure counts present, Type/Method Cypher queries work;
   non-Spring Java repo (fixture: plain java tree, no spring markers) →
   structure-mode run, status ok. Unknown extension value → warn + ignore,
   logged in perception.log.
6. No-JDK environment (PATH without java): flow reaches vg + MCP hint (uv + go
   only).
7. Error paths: analyzer crash / uv sync failure → `analyzer-failed` footnote,
   cause tail in perception.log, exit 0.
8. Perf: fixture analysis < 10s each; first uv sync < 60s (warm machine,
   network available); report timings in perception.log.
9. Vocabulary rules unchanged (§1 wording); install.sh ships `analyzer/` +
   `uv.lock` via whitelist.
10. perception unit/integration suites rewritten to v2 and green; loader suite
    green with the INJECTS-filter change; Spoon adapter `gradle test` still
    green (engine untouched); `make crosscheck` diffs both engines on the 5
    fixtures per the §5 multiset definition (dev tool).

## 9. Out of scope (v2)

Languages beyond Java; incremental re-parsing; entry-point inference for
dead-code queries; removing the Spoon adapter (revisit after two stable
releases); embedding/vector layer (separate future spec).
