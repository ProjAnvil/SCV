# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
