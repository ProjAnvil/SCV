# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

[Unreleased]: https://github.com/projanvil/SCV/compare/v0.4.0...HEAD
[v0.4.0]: https://github.com/projanvil/SCV/compare/v0.3.0...v0.4.0
[v0.3.0]: https://github.com/projanvil/SCV/compare/0.2.0...v0.3.0
[0.2.0]: https://github.com/projanvil/SCV/compare/v0.1.0...0.2.0
[v0.1.0]: https://github.com/projanvil/SCV/releases/tag/v0.1.0
