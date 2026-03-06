# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

[Unreleased]: https://github.com/projanvil/SCV/compare/v0.3.0...HEAD
[v0.3.0]: https://github.com/projanvil/SCV/compare/0.2.0...v0.3.0
[0.2.0]: https://github.com/projanvil/SCV/compare/v0.1.0...0.2.0
[v0.1.0]: https://github.com/projanvil/SCV/releases/tag/v0.1.0
