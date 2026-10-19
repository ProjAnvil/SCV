# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed
- **Perception v2 engine (uv + Python + tree-sitter)** — deep structural analysis no longer requires a JDK/Gradle on the runtime path; the analyzer ships as a uv project (`perception/analyzer/`)
  - **Config migration**: `structure_build_execution` is REMOVED (unknown keys in existing `~/.scv/config.json` are ignored — no action needed); no build ever executes inside the analyzed repository anymore, so `local` and `remote` sources are analyzed identically
  - New config key `structure_extensions` (default `["springboot"]`): language extensions to enable; an empty list runs the language-agnostic structure layer only (structure-mode summary reports types/methods/call edges instead of components/edges); unknown values are warned into `perception.log` and dropped
  - Degradation footnotes: `uv-missing` replaces `jdk-missing` (Python 3.11+ runtime wording); `analyzer-failed` replaces `adapter-build-failed`/`adapter-failed`/`degraded-noclasspath`

## [v0.11.1] - 2026-10-19

### Fixed
- **Perception no longer reuses a stale Go binary — phantom dependency cycles in the user report.** `run_loader` and `build_graphmcp` built their binaries only when the file was missing, so an installation that predated a loader change kept running the loader it first built. A 2026-10-13 loader predated the 2026-10-15 INJECTS cycle filter and graded every SCC as `tolerated`, including CALLS-only mutual recursion and single-node self loops — so an upgraded checkout silently disagreed with its own source. Both tiers now rebuild when any Go source or `go.mod` is newer than the binary.
- Cycle counts and cycle grades now reflect the current loader on every machine, not whatever version happened to be cached in `~/.scv/perception/dist/`.

### Added
- `SCV_PERCEPTION_CONFIG` — optional env var pointing `perceive.py` at an alternate config file. Unset, it reads `~/.scv/config.json` exactly as before. It exists so the fixture suite can exercise structure mode without rewriting the user's config.
- **Perception fixture harness** (`perception/fixture/`) — multi-language source trees that exercise `perceive.py` the way `scv run` Step 5.5 does, wired into `make test` as its last step:
  - `java-springboot-monolith` — Java 21 / Spring Boot 3: every injection style (constructor, field, setter, `@Bean` method), `@Qualifier` / `@Primary` / collection / `Optional` / `@Lazy` / `@Value` cases, conditional assembly, and two cycles graded on purpose — one fatal (constructor) and one tolerated (field)
  - `java-springboot-multimodule` — Gradle multi-module with the boot class in a submodule and injection crossing module boundaries
  - `java-plain-core` — plain Java 21 with no Spring, which is analysed only in structure mode
  - `python-complex`, `golang-complex` — assert the Java-only skip path stays clean (exit 0, `status: skipped`, no structure graph)
- `make test EXTENSIONS=` runs the same suite in structure-only mode. Bean-level assertions are skipped there because that mode produces no beans by design.

### Known limitations
- Wildcard (on-demand) imports hide a class's stereotype: a `@RestController` reached through `import org.springframework.web.bind.annotation.*;` is extracted into the structure layer but never becomes a bean. Real Spring codebases use this form often. Frozen by `StatusController` in the monolith fixture — the check asserts it is *not* a bean, so closing the gap requires inverting that assertion.

### Evaluation
- `make test` green: analyzer pytest 72 · perception suite 51 · loader and graphmcp Go tests · fixture check OK in both extension modes
- Monolith fixture: 19 beans / 23 INJECTS edges, fatal 1, tolerated 1 — matching the hand-built ground truth
- Multi-module fixture: 5 beans / 2 edges, both cross-module edges present

## [v0.11.0] - 2026-10-15

### Changed
- **Perception v2 engine — uv + Python + tree-sitter replaces the JVM default path** (design docs under `docs/superpowers/`, two-round subagent-audited spec + plan):
  - **No JDK required on the default path**: the analysis tier now runs `uv` + Python 3.11+; JDK/Gradle footnotes retired (uv-missing/analyzer-failed replace them)
  - **Language-agnostic structure layer**: File / Type / Method nodes with CONTAINS / CALLS / IMPLEMENTS / EXTENDS / REFERENCES edges, syntactic FQN resolution (package + imports + qualified forms), honest drop+count accounting for unresolvable endpoints
  - **Language extension points**: `structure_extensions: ["springboot"]` config (default on) gates the Bean/INJECTS layer; unknown extensions warn + are ignored; with no extension enabled the structure core runs standalone (marker-file Java detection, structure-mode summary line)
  - **Precision preserved**: the tree-sitter springboot extension reproduces the Spoon engine's outputs exactly on all 5 frozen fixtures (multiset ground truth; `make crosscheck` in `perception/` diffs both engines)
  - Loader now grades dependency cycles from INJECTS edges only (CALLS mutual recursion no longer surfaces in the user cycle summary); installer ships the analyzer (uv project + lock) instead of adapter sources
  - The Spoon adapter remains in-tree as a test-time cross-validation engine
- `structure_build_execution` config key removed (obsolete — the default path executes no build tools; migration note above); `structure_extensions` added

### Evaluation
- Analyzer pytest 72 · perception suite 51 (zero skips, real-chain integration + full query matrix) · loader/graphmcp Go tests · Spoon adapter 5/5 · crosscheck 5/5 · acceptance matrix 10/10 (incl. no-JDK full chain to the MCP hint)

## [v0.10.0] - 2026-10-14

### Added
- **Graph query access: repo + Cypher** — `scv-graph-mcp`, a multi-repo query server over every `~/.scv/analysis/*/structure.vg`: one MCP registration (`scv-graph`) serves ALL analyzed repositories with `repos` / `cypher(repo, query)` / `get_node(repo, key)` / `refresh` tools; agents query by repo name plus a Cypher-subset pattern language and get full node info (stereotype, beanName, type, source, conditions)
  - Built automatically by the perception Go tier; the completion-report hint prefers the multi-repo registration, falling back to per-snapshot `vectra-mcp` (`scv-structure`)
  - perception/README documents the query surface with examples (stereotype census, dependency pairs, `*1..2` transitive impact)
- **Vectra v0.6.0 upgrade** — cycle grading now reads `Cycle.Edges` directly (intra-cycle edges with props); v3 snapshots persist property-index declarations, so stereotype-anchored queries work from snapshots (previously `lookup_property` was unavailable in `-snapshot` mode)

### Changed
- `install.sh` perception whitelist ships the `graphmcp/` module

### Test matrix — Spring Boot 2 + 3, full-chain query assertions
- Three new fixtures with hand-computed ground truth:
  - `boot2-legacy` (Boot 2.7): javax lifecycle, field injection, `@Autowired` multi-ctor selection, abstract-base inherited injection, competing `@Profile` beans, `@Value`-mixed ctor — 15 beans / 10 edges / 4 ambiguous
  - `boot3-modern` (Boot 3.5, Java 21): sealed interface + record beans, record constructor binding of a record `@ConfigurationProperties`, `@Primary` collapse, jakarta lifecycle, switch expressions — 12 beans / 6 edges / 0 ambiguous
  - `boot2-multi` (Boot 2.7, 3 modules): boot entry point in the web submodule, cross-module DI — 6 beans / 4 edges
- `PerceptionRegressionTest` grows to 5 fixture cases (gradle-lombok-sample, torture-app, boot2-legacy, boot3-modern, boot2-multi)
- `test_perceive_query_matrix`: per fixture the FULL chain (perceive → structure.vg → scv-graph-mcp) is asserted — per-stereotype Cypher census against ground truth, cross-module dependency pairs, ambiguity counts; one server instance serves all five repos
- `graphmcp`: cross-repo isolation test (no node leakage between repositories)

## [v0.9.0] - 2026-10-14

### Added
- **Code Perception capability (`perception/`)** — `/scv run` now auto-detects supported frameworks and runs deep structural analysis; users never see the implementation language
  - **Spring Boot adapter** (`perception/adapters/springboot/`): Spoon-based static extractor mapping components (services, controllers, repositories, configuration beans) and their dependency edges — multi-module Gradle & Maven, Lombok `@RequiredArgsConstructor`, constructor/field/setter/`@Bean`-method injection, `@Qualifier`/`@Primary`/by-name disambiguation, `List`/`Optional` unwrapping, inherited `@Autowired` fields, conditional-assembly annotations recorded as metadata; validated 100% against hand-computed ground truth on spring-petclinic and two bundled fixtures (12/8 and 30/24 bean/edge baselines asserted by `gradle test`)
  - **Tiered runtimes with graceful degradation** (`perceive.py`): structure.json (JDK 17+) → structure.vg query file (Go 1.27+) → `scv-structure` MCP registration hint (vectra-mcp); every missing tier degrades to a single attributable footnote, never fails the run; `@ConditionalOn*`/`@Profile` truth values are flagged, not guessed
  - **Security policy**: remote-cloned repositories are analyzed in a no-build degraded mode (`structure_build_execution: local-only` default) — build tools never execute inside untrusted repos
  - **Circular-dependency grading**: constructor cycles reported as startup-fatal, field/setter cycles as tolerated (container-tolerated)
  - **SCV touchpoints (bilingual)**: `run.md` Step 5.5, ARCHITECTURE template "Component Dependencies" section rendered strictly from structure.json, `structure_analysis_enabled` / `structure_build_execution` config keys, install.sh whitelist copy
- **Subagent evaluation (`eval/ --target agent`)** — smelt agent-mode evaluation for `project-analyzer` (both languages): 4+4 behavior cases (four-doc workflow discipline, structure-data rendering, deep-analysis mode gating), mini-orders Spring Boot fixture with an embedded constructor cycle, sandboxed tool set, `WriteContains` content assertions
- **Design docs** — audited spec + implementation plan under `docs/superpowers/` (two-round subagent audit; five blockers fixed pre-implementation)

### Changed
- **`project-analyzer` subagent (en + zh-cn)** — evaluation-driven improvements:
  - frontmatter `name` field added; non-existent `LSP` tool removed from the allowlist
  - new **Boundaries** section: deliverables-only, read-only analysis, no shell when deep analysis is off (hard rule at the execution step), hand-back conditions, fail-loudly rule
  - Structure Data rendering instruction and checklist items added; Chinese output must use 「启动级」/「容忍级」 cycle-grading wording
  - "Bean injection" → "dependency injection" (vocabulary rules); Phase 2.5 examples compressed, duplicated guidance consolidated

### Evaluation results (smelt, n=1 per case)
- project-analyzer en: overall **77.1 (C) → 95.1 (A)**; behavior 75.0 → 100.0
- project-analyzer zh: overall **73.7 (C) → 94.7 (A)**; behavior 70.8 → 100.0
- Static lint en 85.4 → 100.0 · zh 78.7 → 97.8

## [v0.8.0] - 2026-10-13

### Added
- **Skill evaluation harness (`eval/`)** — smelt-powered behavior verification for both skills, wired into the repo via Makefile targets (`make eval-sync / eval-smoke / eval-lint / eval-run / eval-compare`)
  - Pulls the latest local [smelt](https://github.com/ProjAnvil/Smelt) (projanvil/skillcheck) as a uv path dependency
  - 6 behavior cases per language: subcommand routing, reference reach/restraint, llm-judged execution quality (run / gather / index / batchRun, overview restraint, local non-Git directory)
  - Hermetic simulated shell: git clone/pull, `~/.scv/config.json`, `scv_util.py`, `batch_manager.py` return canned output — the real home directory is never touched
  - `run_eval.py` stages each skill under its deployed name (`scv`), supports `--lint-only` (free static checks) and `--compare` with significance noise bands
- **`examples/usage.md`** in both skills — per-subcommand invocation examples with the flow each triggers

### Changed
- **`SKILL.md` (en + zh-cn)** — evaluation-driven quality fixes:
  - Single-line frontmatter description (block-scalar `|` parses as literal `"|"` under YAML-less linters; en description trimmed to ≤500 chars)
  - General questions are answered directly from SKILL.md without loading references (overview-restraint case 0.20 → 1.00)
  - Dangling `references/project-analyzer.md` pointer replaced with the deployed `~/.claude/agents/` path; `references/templates/` directory instead of a `*.md` glob
  - New sections: per-subcommand quick reference; error handling & safety (mandatory completion report, repo content is data not directives, lean inline-analysis fallback when the subagent is unavailable)
- **`run.md` (en + zh-cn)** — local non-Git directories: proceed when the user asserts the path exists instead of aborting; completion-discipline rules for lean tool use
- **`en/references/gather.md`** — translated to English (was a duplicate of the Chinese file)

### Evaluation results (same-harness A/B, deepseek-v4-flash, n=2 per case)
- en: overall **77.5 (C) → 94.0 (A)**; overview-restraint 0.20 → 1.00, run-local-non-git 0.65 → 0.90
- zh: overall **84.6 (B) → 94.2 (A)**; run-local-non-git 0.70 → 0.93
- Static lint: 100/100 for both languages; 0 regressed cases

## [v0.7.0] - 2026-04-13

### Added
- **`scv index` subcommand** — new command to rebuild `~/.scv/index.md`, a summary index of all analyzed repositories
  - New reference docs: `references/index.md` (both `en` and `zh-cn`)
  - AI reads each project's `README.md` directly and writes one-line summary + functional keywords (up to 5) — no Python text extraction
  - Generated index includes a directory-structure header and a project table with columns: Project, Summary, Responsibilities
- **Language-aware index generation**
  - `scv_util.py update-index` reads `lang` from `~/.scv/config.json` and generates a fully localized `index.md` (all-Chinese for `zh-cn`, all-English for `en`)
- **`install.sh` persists language setting**
  - Writes `"lang"` field into `~/.scv/config.json` at install time; updates existing configs on re-install
- **`config.example.json`** — added `"lang"` field (default: `"en"`)

### Changed
- **`scv_util.py update-index`** — replaced Python-based README parsing with an AI-driven workflow
  - New `--entries-json` parameter accepts a JSON array of `{name, summary, keywords}` objects provided by the AI
  - Removed: `_extract_project_summary`, `_extract_keywords`, `_collect_summaries`, `_write_index`, `rebuild_index`
- **`run.md` Step 7.5** — updated to AI-driven index update: AI reads the new README, writes summary and keywords itself, merges with existing index, then calls the script to write the file
- **`batchRun.md` Step 6.5** — updated to delegate to `references/index.md` workflow instead of calling the script directly
- **`SKILL.md`** (both `en` and `zh-cn`) — added `index` subcommand to description, command list, and routing table

## [v0.6.1] - 2026-03-27

### Added
- **Multi-language installer support**
  - `install.sh` now accepts `--lang` parameter (`en` or `zh-cn`)
  - Default language is English (`en`)
  - Usage: `./install.sh --lang=zh-cn` or `./install.sh --lang zh-cn`

- **New config options in config.example.json**
  - `batch_size`: Control parallel analysis batch size (default: 5)
  - `deep_analysis_enabled`: Toggle deep analysis mode (default: false)

## [v0.6.0] - 2026-03-25

### Added
- **Deep Analysis with codebones Integration**
  - New progressive deep dive strategy: skeleton overview → targeted `codebones get` for full implementations
  - `codebones get <symbol>` - Fetch complete source code of specific classes/methods on-demand
  - `codebones search <query>` - Search symbols and trace dependencies across the codebase
  - `codebones outline <path>` - Get file/directory structure without reading full content
  - 85% token reduction compared to reading all files, while still getting full implementation details

- **Enhanced project-analyzer Agent**
  - Added `Bash` tool to execute codebones CLI commands during deep analysis
  - New Phase 2.5 deep analysis workflow with on-demand code fetching
  - Framework-specific extraction patterns (Java/Spring, Python/FastAPI, Go, Node.js/NestJS)
  - Enhanced output sections: Service dependencies, API method chains, business rules from actual code

### Changed
- **Deep Analysis Workflow Refactored**
  - Previously: Only generated skeleton file (85% token savings) but stopped there
  - Now: Skeleton overview → identify key symbols → `codebones get` for actual implementations
  - Deep analysis is now truly "deep" - fetches complete implementations when needed

### Dependencies
- **codebones** (optional): Required for deep analysis mode
  - Install: `pip install codebones` or `cargo install codebones`
  - GitHub: [https://github.com/creynir/codebones](https://github.com/creynir/codebones)

## [v0.5.0] - 2026-03-17

### Added
- **Incremental analysis (skip unchanged repos)**
  - `batchRun` and `run` now detect the HEAD commit of each repository before analysis
  - Repos whose HEAD commit hasn't changed since the last analysis are skipped automatically
  - Per-repo `.scv_metadata.json` stores the last-analyzed commit hash and timestamp
  - `batchRun` plan step marks unchanged repos as `skipped`, `complete` step writes metadata on success
  - `run.md` updated with commit detection (Step 4), metadata writing (Step 6), and updated step numbering

- **`scv_util.py` — new CLI utility script**
  - Provides three CLI commands: `get-commit-info`, `check-skip`, `write-metadata`
  - `check-skip` exits with code `2` when a repo can be skipped (no changes since last run)
  - `write-metadata` writes `.scv_metadata.json` to the analysis output directory
  - Also importable as a Python module (used by `batch_manager.py`)

- **`git_op.py` enhancements**
  - Added `get_head_commit(repo_path)` to retrieve the current HEAD commit hash
  - Added `get_commit_info(repo_path)` to return commit hash + author + message as a dict

### Changed
- **`batch_manager.py` refactored**
  - `plan()` now marks repos with unchanged commits as `skipped` before dispatching subagents
  - `complete()` writes `.scv_metadata.json` via `scv_util` after successful analysis
  - Imports migrated from removed `metadata.py` to `scv_util.py`

- **`project-analyzer` agent updated (en + zh-cn)**
  - Added `Current Commit` as an input parameter passed by the orchestrator
  - Clarified that the agent displays the commit in its output but does not write metadata itself

- **`batchRun.md` updated (en + zh-cn)**
  - Plan output example now includes `skipped_repos` and per-repo `skipped` flag
  - Step 4 documents the skip logic and exit-code handling
  - Step 5 summary includes skipped repo count

### Removed
- **`metadata.py`** — logic fully consolidated into `scv_util.py`

## [v0.4.1] - 2026-03-09

### Fixed
- **batchRun concurrency control**
  - Added explicit batch processing pseudocode with `TaskOutput(block=true)` to enforce waiting
  - Added strict execution requirements to prevent forking more than 5 subagents per turn
  - Added warning about why concurrency must be limited (resource consumption, API rate limiting)
  - Updated both English and Chinese versions of `batchRun.md`

## [v0.4.0] - 2026-03-13

### Changed
- **Migrated from Commands to Skill architecture**
  - Converted `scv.run`, `scv.batchRun`, `scv.gather` commands into a unified `scv` skill
  - New subcommand syntax: `/scv run`, `/scv batchRun`, `/scv gather`
  - Updated `install.sh` to install skill instead of commands
  - Old commands are automatically removed during installation

- **Language-specific skill structure**
  - Separated English and Chinese skills into `skills/en/` and `skills/zh-cn/`
  - Each language skill is self-contained with its own templates
  - Install script copies selected language skill as `~/.claude/skills/scv`
  - Removed `prompts/` and `commands/` directories (now integrated into skills)

- **Unified analysis engine with project-analyzer subagent**
  - Both `run` and `batchRun` now use the same `project-analyzer` subagent
  - Consistent analysis quality across single and batch operations
  - Context isolation for all repository analyses

### Added
- **New agent definition with bilingual support: `agents/{en,zh-cn}/project-analyzer.md`**
  - Specialized subagent with optimized tool set (Read, Write, Glob, Grep, LSP)
  - Single-purpose deep code analysis agent
  - Generates 4 standardized documents (README, SUMMARY, ARCHITECTURE, FILE_INDEX)
  - Language-specific agent installed based on `--lang` parameter

- **Subagent-based parallel execution for batchRun**
  - Each repository analysis runs in an independent subagent
  - Maximum 5 concurrent subagents with batched processing
  - TodoWrite-based task tracking for progress visibility
  - True parallel processing with isolated contexts
  - Prevents context bloat when analyzing multiple repositories

### Architecture
- **Unified flow:**
  ```
  /scv run <path>     → Agent(project-analyzer) → 4 docs
  /scv batchRun       → Agent(project-analyzer) × N (max 5 parallel) → 4 docs × N
  ```

- **Directory structure:**
  ```
  SCV/
  ├── agents/
  │   ├── en/
  │   │   └── project-analyzer.md    # English agent definition
  │   └── zh-cn/
  │       └── project-analyzer.md    # Chinese agent definition
  ├── skills/
  │   ├── en/                        # English skill
  │   │   ├── SKILL.md
  │   │   └── references/
  │   │       ├── run.md             # Uses project-analyzer subagent
  │   │       ├── batchRun.md        # Uses project-analyzer subagent (max 5 parallel)
  │   │       ├── gather.md
  │   │       └── templates/
  │   └── zh-cn/                     # Chinese skill (same structure)
  └── install.sh                     # Installs agent + skill based on --lang
  ```

## [v0.3.0] - 2026-03-06

### Added
- **Project Analyzer Enhancements**
  - Added "Recommended Tools" section with guidance on using Glob, Grep, Read, and LSP
  - Added "Template Syntax Reference" table explaining placeholder, loop, and conditional syntax
  - Added "Edge Cases Handling" section covering 7 common scenarios
  - Added "Output Checklist" for quality verification before completion
  - Added token optimization tips for large projects

### Changed
- **Project Analyzer Improvements**
  - Enhanced "Execution Instructions" with concrete 3-step workflow
  - Moved output language setting to the beginning of the document
  - Expanded "Analysis Principles" with more actionable guidance
  - Improved overall structure and clarity for both English and Chinese versions

### Fixed
- **Installation Script**
  - Fixed `install.sh` to only remove `scv.*` commands instead of entire `~/.claude/commands` directory
  - Prevents accidental deletion of other user-installed Claude commands

## [0.2.0] - 2026-01-30

### Changed
- Restructured prompts directory with multi-language support (en, zh-cn)

## [v0.1.0] - 2025-01-28

### Added
- Initial project release with MIT license
- SCV (Source Code Viewer) core functionality
- Project analyzer prompts and templates
- Skills: `scv.run`, `scv.batchRun`, `scv.gather`

[Unreleased]: https://github.com/projanvil/SCV/compare/v0.6.0...HEAD
[v0.6.0]: https://github.com/projanvil/SCV/compare/v0.5.0...v0.6.0
[v0.5.0]: https://github.com/projanvil/SCV/compare/v0.4.1...v0.5.0
[v0.4.1]: https://github.com/projanvil/SCV/compare/v0.4.0...v0.4.1
[v0.4.0]: https://github.com/projanvil/SCV/compare/v0.3.0...v0.4.0
[v0.3.0]: https://github.com/projanvil/SCV/compare/0.2.0...v0.3.0
[0.2.0]: https://github.com/projanvil/SCV/compare/v0.1.0...0.2.0
[v0.1.0]: https://github.com/projanvil/SCV/releases/tag/v0.1.0
