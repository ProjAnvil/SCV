---
name: scv
description: |
  SCV (Source Code Vault) - Deep Code Repository Analysis Tool.

  Subcommands:
  - run <path|url>: Analyze a single repository, generate 4 structured documents
  - batchRun: Batch analyze multiple repositories (parallel execution with subagents)
  - gather <options>: Clone/update/manage remote Git repositories

  Use this skill when users need to:
  - Analyze codebase structure and technology stack
  - Generate project documentation (README, SUMMARY, ARCHITECTURE, FILE_INDEX)
  - Batch analyze multiple repositories
  - Manage remote repository clones

  Triggers: analyze code, generate docs, repository analysis, project documentation, batch analysis
---

# SCV - Source Code Vault

Deep code repository analysis tool that generates structured project documentation.

## Command Parsing

Parse the first argument as subcommand:

```
/scv run <repo_path|url> [project_name]
/scv batchRun
/scv gather <options>
```

| Subcommand | Function | Reference |
|------------|----------|-----------|
| `run` | Single repository analysis | Read `references/run.md` |
| `batchRun` | Batch parallel analysis | Read `references/batchRun.md` |
| `gather` | Repository management | Read `references/gather.md` |

## Execution Flow

1. **Parse user input**, extract subcommand and arguments
2. **Load corresponding reference file**:
   - `run` → `references/run.md`
   - `batchRun` → `references/batchRun.md`
   - `gather` → `references/gather.md`
3. **Execute subcommand logic**

## Core Resources

All analysis uses these resources (located within this skill):

| Resource | Path | Description |
|----------|------|-------------|
| Analysis Prompt | `references/project-analyzer.md` | Core analysis logic |
| Templates | `references/templates/*.md` | Document generation templates |
| Config File | `~/.scv/config.json` | Repository list and settings |
| Repo Storage | `~/.scv/repos/` | Cloned remote repositories |
| Analysis Output | `~/.scv/analysis/` | Generated documents |

## Output Structure

Each repository analysis generates 4 documents:

```
~/.scv/analysis/{project_name}/
├── README.md        # Project overview entry point
├── SUMMARY.md       # 5-minute project summary
├── ARCHITECTURE.md  # Architecture design document
└── FILE_INDEX.md    # File responsibility index
```

## Analysis Principles

1. **Code is Truth** - Base analysis on actual code, not potentially outdated comments
2. **Naming Reveals Intent** - Extract meaning from names as primary documentation
3. **Dependencies Expose Architecture** - Map imports to understand true coupling
4. **Tests Document Behavior** - Test cases are the most accurate usage examples
5. **Configuration Defines Boundaries** - Config files reveal system integration points
6. **Progressive Deep Dive** - Scan globally first, then focus; skeleton first, then flesh
