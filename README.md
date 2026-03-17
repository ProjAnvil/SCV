# SCV - Source Code Vault

> A powerful codebase management and analysis skill for Claude Code

[![GitHub Repository](https://img.shields.io/badge/GitHub-SCV-blue.svg)](https://github.com/ProjAnvil/SCV)

**SCV** provides three subcommands for comprehensive codebase management and analysis:

- **scv run** - Perform deep analysis on a single codebase
- **scv batchRun** - Batch analyze multiple codebases with parallel subagents
- **scv gather** - Clone and manage remote Git repositories

## Table of Contents

- [Features](#features)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Commands](#commands)
  - [scv run](#scv-run---single-codebase-analysis)
  - [scv batchRun](#scv-batchrun---batch-codebase-analysis)
  - [scv gather](#scv-gather---git-repository-management)
- [Configuration](#configuration)
- [Output Documents](#output-documents)
- [Workflows](#workflows)
- [Best Practices](#best-practices)

## Features

- 🔄 **Remote Repository Management**: Clone, update, and manage remote Git repositories
- 📊 **Deep Code Analysis**: Generate comprehensive documentation for any codebase
- 🚀 **Parallel Processing**: Analyze multiple repositories simultaneously with subagents
- ⚡ **Incremental Analysis**: Skip repos whose HEAD commit hasn't changed since last analysis
- 📝 **Multiple Output Formats**: README, Summary, Architecture, File Index
- 🤖 **Auto-Pull for Remote Repos**: Always analyze the latest code
- 🎯 **Flexible Configuration**: Support both remote and local repositories
- 🌐 **Multi-Language Support**: English and Chinese skills available

## Installation

### Quick Install (Recommended)

Run the installation script:

```bash
# Default (English)
./install.sh

# Chinese version
./install.sh --lang=zh-cn

# Show help
./install.sh --help
```

The script will:
- Create `~/.scv` configuration directory
- Copy the configuration file
- Install the language-specific skill to `~/.claude/skills/scv`
- Install the `project-analyzer` agent to `~/.claude/agents/`

### Directory Structure After Installation

```
~/.scv/
├── config.json      # Repository configuration
├── repos/           # Cloned remote repositories
├── analysis/        # Generated documentation
└── sessions/        # batchRun session state (crash recovery)

~/.claude/
├── agents/
│   └── project-analyzer.md   # Subagent for code analysis
└── skills/scv/
    ├── SKILL.md              # Skill entry point
    ├── scripts/              # Python helper scripts (scv_util, batch_manager, git_op)
    └── references/
        ├── run.md
        ├── batchRun.md
        ├── gather.md
        └── templates/
            ├── README.template.md
            ├── SUMMARY.template.md
            ├── ARCHITECTURE.template.md
            └── FILE_INDEX.template.md
```

## Quick Start

### 1. Clone a Repository

```
/scv gather https://github.com/user/project.git
```

### 2. Analyze the Codebase

```
/scv run ~/.scv/repos/project
```

### 3. View Generated Documentation

```bash
open ~/.scv/analysis/project/README.md
```

## Commands

### scv run - Single Codebase Analysis

Perform deep analysis on a single codebase, generating 4 documents:

- **README.md** - Project overview
- **SUMMARY.md** - 5-minute project summary
- **ARCHITECTURE.md** - Architecture design document
- **FILE_INDEX.md** - File responsibility index

#### Usage

```
/scv run <repo_path|url> [project_name]
```

#### Examples

```
# Remote repository
/scv run https://github.com/user/project.git
/scv run https://github.com/user/project.git "My Project"

# Local Git repository
/scv run ~/projects/my-project

# Local non-Git directory
/scv run ~/my-folder "My Analysis"
```

### scv batchRun - Batch Codebase Analysis

Analyze multiple repositories in parallel using the `project-analyzer` subagent. Each repository gets its own isolated context.

#### Usage

```
/scv batchRun
```

Reads configuration from `~/.scv/config.json`.

#### Key Features

- **Subagent-Based**: Uses dedicated `project-analyzer` subagent for each repository
- **Configurable Concurrency**: `batch_size` in config sets the max parallel subagents (default: 5)
- **Incremental Skip**: Repos with unchanged HEAD commit are automatically skipped
- **Task Tracking**: TodoWrite-based progress visibility
- **Context Isolation**: No context bloat from analyzing multiple repositories
- **Error Resilience**: Individual failures don't affect other analyses
- **Crash Recovery**: Session state persisted to `~/.scv/sessions/` for resumability

### scv gather - Git Repository Management

Clone and manage remote Git repositories.

#### Usage

```
/scv gather <git_url> [branch]           # Clone single repository
/scv gather --batch                      # Batch clone from config
/scv gather --update [repo_name]         # Update specific repository
/scv gather --update-all                 # Update all repositories
/scv gather --list                       # List all repositories
/scv gather --remove <repo_name>         # Remove repository
```

#### Examples

```
# Clone single repository
/scv gather https://github.com/user/project.git
/scv gather https://github.com/user/project.git main

# Batch clone
/scv gather --batch

# List repositories
/scv gather --list

# Update all
/scv gather --update-all
```

## Configuration

### Configuration File

Location: `~/.scv/config.json`

```json
{
  "output_dir": "~/.scv/analysis",
  "batch_size": 5,
  "repos": [
    {
      "type": "remote",
      "url": "https://github.com/user/project1.git",
      "project_name": "Remote Project 1",
      "branch": "main"
    },
    {
      "type": "remote",
      "url": "https://github.com/user/project2.git",
      "project_name": "Remote Project 2",
      "branch": "develop"
    },
    {
      "type": "local",
      "path": "~/local/path/to/project3",
      "project_name": "Local Project 3"
    }
  ],
  "parallel": true,
  "fail_fast": false
}
```

### Configuration Fields

| Field | Required | Description | Default |
|-------|----------|-------------|---------|
| `output_dir` | No | Output directory for analyses | `~/.scv/analysis` |
| `batch_size` | No | Max concurrent subagents per batch for batchRun | `5` |
| `parallel` | No | Parallel execution within a batch | `true` |
| `fail_fast` | No | Stop on first error | `false` |

**Remote Repository:**
- `type`: `"remote"` (required)
- `url`: Git repository URL (required)
- `project_name`: Display name (optional)
- `branch`: Branch to clone/pull (optional)

**Local Repository:**
- `type`: `"local"` (required)
- `path`: Local path (required, supports `~`)
- `project_name`: Display name (optional)

## Output Documents

### README.md
- Project positioning and value
- Technology stack
- Document navigation
- Quick start guide

### SUMMARY.md
- 5-minute project overview
- Technology stack panorama
- Module division
- API overview

### ARCHITECTURE.md
- System architecture diagram (Mermaid)
- Technology selection rationale
- Module design
- Data flow

### FILE_INDEX.md
- File responsibility descriptions
- File dependencies
- Key class/function descriptions

## Workflows

### Workflow 1: Clone and Analyze

```
# 1. Clone repository
/scv gather https://github.com/user/project.git

# 2. Analyze
/scv run ~/.scv/repos/project

# 3. View results
open ~/.scv/analysis/project/README.md
```

### Workflow 2: Batch Analysis

```bash
# 1. Configure repositories
cat > ~/.scv/config.json <<EOF
{
  "output_dir": "~/.scv/analysis",
  "repos": [
    {"type": "remote", "url": "https://github.com/team/backend.git", "project_name": "Backend"},
    {"type": "remote", "url": "https://github.com/team/frontend.git", "project_name": "Frontend"},
    {"type": "local", "path": "~/projects/docs", "project_name": "Docs"}
  ],
  "parallel": true
}
EOF

# 2. Clone remote repos (optional)
/scv gather --batch

# 3. Batch analyze (remote repos auto-pull)
/scv batchRun
```

### Workflow 3: Periodic Updates

```
# Simply run batchRun periodically
# It will auto-pull remote repos and regenerate docs
/scv batchRun
```

## Best Practices

### Single Analysis
- Re-analyze after major codebase changes
- Use custom project names for clarity

### Batch Analysis
- Use `parallel: true` for speed (default)
- Use `fail_fast: true` for CI/CD validation
- Run periodically to keep docs updated

### Configuration
- Group repositories by project or team
- Use remote for stable code, local for work-in-progress
- Add `~/.scv/config.json` to version control (exclude sensitive paths)

## Troubleshooting

**Q: Command not found**

A: Make sure the skill is installed:
```bash
./install.sh --lang=en
```

**Q: Git clone failed**

A: Check Git credentials:
```bash
git config --global credential.helper store
# Or configure SSH keys
```

**Q: Analysis incomplete**

A: Check file permissions and ensure all files are readable.

**Q: Template files missing**

A: Re-run the installation script to restore templates.

## Architecture

SCV uses a skill-based architecture with:

1. **SKILL.md** - Entry point that routes subcommands
2. **references/** - Detailed implementation docs for each subcommand
3. **templates/** - Document generation templates
4. **scripts/** - Python helper scripts (`scv_util.py`, `batch_manager.py`, `git_op.py`)
5. **project-analyzer agent** - Dedicated subagent that performs the actual code analysis

Each language version (en, zh-cn) is self-contained in its own directory.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT License.

## Links

- **Repository**: [https://github.com/ProjAnvil/SCV](https://github.com/ProjAnvil/SCV)
- **中文文档**: [docs/README_CN.md](docs/README_CN.md)
