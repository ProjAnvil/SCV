# Perception v2 — uv + Python + tree-sitter Engine — Design Spec

- Date: 2026-10-14
- Status: draft for subagent audit
- Supersedes (partially): the runtime path of 2026-10-13-code-perception-design.md
  rev2 — the Spoon adapter is retained as a test-time cross-validation engine,
  but the DEFAULT user-facing flow no longer requires a JVM.
- Research basis: session research report "代码结构入 Vectra 对检索的收益 与
  图/边定义" (structure-layer schema) + 5 validated fixtures with ground truth.

## 1. Motivation & positioning

Perception v1 runs the structure extraction in Java (Spoon): correct, but it
makes a JDK + Gradle a hard runtime dependency of `/scv run`'s optional tier,
and it only knows Spring beans. Perception v2 moves extraction to
**uv + Python + tree-sitter**:

- **No JVM on the default path** — the JDK tier disappears from the user's
  ladder (JDK-missing footnote is retired for the default flow).
- **Language-agnostic core** — tree-sitter structure extraction (File / Type /
  Method nodes, CONTAINS / CALLS / IMPLEMENTS / EXTENDS / REFERENCES edges)
  works per-grammar; v1 ships Java, the architecture is per-language.
- **Language extension points** — framework-specific enrichment (Spring beans)
  plugs into the core via a registry; the user toggles them in
  `~/.scv/config.json` (`structure_extensions: ["springboot"]`).

## 2. What stays unchanged

- **stdout JSON contract** of perceive.py (spec 2026-10-13 §5.1): single line,
  exit 0 always, `{status, tiers, reason, counts, artifacts, mcp_add, footnote}`.
  `counts` gains a `structure` sub-object (see §6); existing keys keep semantics.
- **Tier ladder** outside the analyzer: config check → detect → stale cleanup →
  analyzer (Python) → structure.json → Go tier (loader + graphmcp binary) → MCP
  hint. The JDK/dist tiers are DELETED from the default path.
- **Security policy**: the analyzer never executes build tools inside the
  analyzed repo (tree-sitter needs no classpath at all — the noclasspath
  distinction collapses; `--source remote` and `local` behave identically,
  logging still records the source for audit).
- **Artifacts**: `~/.scv/analysis/{project}/structure.json` (+ `.vg`),
  `perception.log`. Loader and scv-graph-mcp stay schema-pass-through.
- **Spoon adapter** (`perception/adapters/springboot/`): stays in-tree with its
  5-fixture regression suite as the **cross-validation engine** (dev/test-time
  only). The shared fixtures become the acceptance bar for the new engine.

## 3. Repository layout (delta)

```
perception/
├── perceive.py                 # orchestrator v2 (drops JDK/dist tiers)
├── analyzer/                    # NEW: uv project (tree-sitter engine)
│   ├── pyproject.toml           # deps: tree-sitter, tree-sitter-java (pinned)
│   ├── analyzer.py              # CLI: <repo> <out.json> [--languages java]
│   ├── structure/               # language-agnostic core
│   │   ├── model.py             # node/edge builders, key schemes, dedup
│   │   └── java_extractor.py    # tree-sitter-java → File/Type/Method + edges
│   └── extensions/
│       ├── registry.py          # discover + gate via structure_extensions
│       └── springboot.py        # bean + INJECTS extraction (syntactic)
├── loader/                      # unchanged (schema pass-through; index list grows)
├── graphmcp/                    # unchanged (cypher description mentions new props)
└── adapters/springboot/         # Spoon cross-validation engine (test-time)
```

## 4. Structure layer (language-agnostic core, Java v1)

Nodes (label = type discriminator; keys stable & human-readable):

| label | key | props |
|---|---|---|
| `File` | `file:<repo-relative path>` | path, language, loc |
| `Type` | `type:<fqn>` | kind (class/interface/record/enum), modifiers, loc |
| `Method` | `fn:<type-fqn>#<name>(<param-type-simple-names>)` | name, visibility, static, params, returns, loc, owner (type key) |

Edges:

| label | from → to | props | notes |
|---|---|---|---|
| `CONTAINS` | File → Type | — | only the file/type level; method ownership lives in `props.owner` + key prefix |
| `IMPLEMENTS` | Type → Type | — | syntactic implements clauses |
| `EXTENDS` | Type → Type | — | syntactic extends clauses |
| `CALLS` | Method → Method | line, kind (`direct`/`virtual-dispatch`) | resolved via syntactic FQN (imports + same-package); unresolved call sites are dropped with a counter (not noise edges) |
| `REFERENCES` | Method → Type | line | new/extends/implements/field-type usage within method bodies |

FQN resolution (syntactic): package declaration + imports + same-package
fallback; wildcard imports resolve on a best-effort simple-name match against
known source types; unresolved names are skipped (counted in
`structure.unresolved_refs`).

## 5. Spring Boot extension (`springboot`)

Gated by `structure_extensions` containing `"springboot"` (config, default
`["springboot"]`). Extracts the v1 Bean layer on top of the structure layer:

- **Beans**: stereotype annotations (transitive meta-annotations within source;
  jar-carried meta chains fall back to a name table: the known Spring set),
  `@Bean` methods, `@ConfigurationProperties` via `@EnableConfigurationProperties`
  registration, name rules (explicit value > decapitalized simple name, two-upper
  rule preserved).
- **INJECTS edges** (Bean → Bean, props via/qualifier/ambiguous/at): constructor
  params (incl. `@Value` skip, multi-ctor `@Autowired` selection), Lombok
  `@RequiredArgsConstructor` final/`@NonNull` fields, `@Autowired` fields (own +
  superclass walk in source), setters, `@Bean` method params.
- **Resolution rules** (syntactic): candidate beans = those whose class
  `IMPLEMENTS`/`EXTENDS` (syntactic edges) the injected type FQN; qualifier →
  name match; `@Primary` collapse; by-name fallback on field/param name;
  `List/Set/Collection/Optional` unwrap (collections take all candidates);
  ambiguity flagged honestly. Injection-type FQN via imports — including
  `java.*` (e.g. `java.time.Clock` resolves a `@Bean Clock` producer).
- **Spring Data repositories**: interface whose `extends` clause resolves to the
  `org.springframework.data.repository.Repository` family (via imports) →
  `data-repository` stereotype.
- **Conditions**: `@ConditionalOn*`/`@Profile` recorded as props (same wording).

**Precision contract**: the extension must reproduce the 5-fixture ground
truths (beans/edges/ambiguity) EXACTLY. Known syntactic limits (documented
deviations allowed ONLY where a fixture exercises them — current expectation:
none): jar-inherited meta-annotation chains outside the known-name table;
generic type-variable injection in framework superclasses.

## 6. Output schema (structure.json v2)

Vectra batch format as before, plus:

- node props for Method include `owner`; bean nodes unchanged (v1 schema).
- top-level `"meta"`: `{"engine": "treesitter-python", "languages": ["java"],
  "extensions": ["springboot"], "structure": {"files": F, "types": T,
  "methods": M, "call_edges": C, "unresolved_refs": U}, "beans": B,
  "inject_edges": I, "ambiguous": A}`.
- perceive.py `counts` keeps `components`/`edges` = bean-layer counts (backward
  compatible report lines) and adds `structure` (files/types/methods/call_edges).
  When the springboot extension is disabled: `components`/`edges` = structure
  totals instead; the summary line wording switches accordingly.

## 7. Runtime & config changes

| Item | v1 | v2 |
|---|---|---|
| Analyzer runtime | JDK 17 + Gradle dist (lazy build) | `uv run --project perception/analyzer` (uv required; footnoted if missing) |
| Tiers | JDK → dist → run | **analyzer (uv)** → json → Go → MCP |
| Config | `structure_analysis_enabled`, `structure_build_execution` | + `structure_extensions: ["springboot"]`; `structure_build_execution` retained but documented as Spoon-engine-only (deep/test path) |
| Footnotes | jdk-missing, adapter-build-failed… | `uv-missing` (new), `analyzer-failed`; JDK footnotes retired from default path |

run.md Step 5.5 copy updates: first-run note no longer mentions "component
build + dependency download" (uv sync is seconds); config gate mentions the
extension list. Bilingual.

## 8. Acceptance criteria

1. Same 5 fixtures through the NEW engine reproduce v1 ground truths:
   gradle-lombok-sample 12/8, torture-app 30/24, boot2-legacy 15/10 (4 amb),
   boot3-modern 12/6, boot2-multi 6/4 — asserted in a new
   `analyzer/tests/test_fixtures.py` (uv/pytest, tree-sitter only, no network
   beyond initial uv sync).
2. Full-chain query matrix re-passes unchanged (perceive v2 → .vg →
   scv-graph-mcp → per-stereotype Cypher census).
3. Structure-layer assertions: torture-app reports files=26 (module build files
   excluded — sources only), types ≥ 30, methods ≥ 80, call_edges ≥ 100,
   unresolved_refs bounded (< 20% of call sites); `IMPLEMENTS` edge exists for
   `type:com.acme.core.GreetingServiceImpl` → `type:com.acme.core.GreetingService`.
4. Extension gate: `structure_extensions: []` in a temp config → no Bean nodes,
   no INJECTS edges, status ok, structure counts present; `repos`/`cypher` on
   Type/Method nodes still queryable.
5. No-JDK environment (PATH without java): full flow still reaches vg + MCP
   hint (uv + go only).
6. Spoon cross-validation: adapters' `gradle test` still green (engine retained
   untouched); a `make crosscheck` target runs both engines on the fixtures and
   diffs bean-layer outputs (dev tool, not CI-blocking).
7. Vocabulary rules (spec 2026-10-13 §1.1) unchanged; install.sh ships
   `analyzer/` (pyproject + sources); `uv-missing` footnote follows the same
   template shape.
8. perception unit/integration suites updated and green; existing user-facing
   artifacts (perception.log, structure.json location) unchanged.

## 9. Out of scope (v2)

- Languages beyond Java in the structure core.
- Incremental/differential re-parsing (full rebuild per run remains).
- Method-level entry-point inference for dead-code queries (schema-ready, no
  `entry` prop computation).
- Removing the Spoon adapter (kept for cross-validation; revisit after two
  stable releases).
