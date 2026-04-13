# scv index - Rebuild Analysis Index

Rebuild `~/.scv/index.md` — a summary index of all analyzed repositories, with functional-responsibility keywords.

## Usage

```
/scv index
```

## Execution Steps

> ⚠️ **Important**: Step 2 below means **you (the AI) read the files and understand them yourself**.
> Do NOT use Python scripts to parse READMEs. Do NOT write code to extract text.
> You read each README.md directly and use your own comprehension to write the summary and keywords.

### Step 1: List all project directories

```bash
ls ~/.scv/analysis/
```

Collect all subdirectory names under `~/.scv/analysis/` as the project list. If the directory is empty, display a notice and exit.

### Step 2: Read each project's README.md yourself and write two things

For each project, **use a file-read tool** to open `~/.scv/analysis/{project_name}/README.md`, then **you** write:

- **One-line summary**: What does this project do? One clear sentence, max 200 characters.
- **Functional keywords**: What are the core business modules or functional areas? Extract up to 5 keywords joined with `、`. Use `-` if you genuinely cannot tell.

Both items come entirely from your own reading and understanding of the README. **Do not run any script to help you extract them.**

### Step 3: Compose entries JSON and call the write script

Assemble all projects into a JSON array and call the script to write the index:

```bash
python3 ~/.claude/skills/scv/scripts/scv_util.py update-index \
  --analysis-dir ~/.scv/analysis \
  --entries-json '[
    {"name": "ecif-core",     "summary": "Enterprise customer info platform providing unified data services.", "keywords": "客户管理、数据同步、权限控制、审计日志、API网关"},
    {"name": "log-collector", "summary": "Distributed log collection and analysis tool.",                      "keywords": "-"}
  ]'
```

Script output (JSON):
```json
{"status": "index_updated", "index_file": "~/.scv/index.md", "total_projects": 2}
```

### Step 4: Completion report

Display:

```
✅ Index updated: ~/.scv/index.md

  📇 Total projects: {N}
  📂 Index file: ~/.scv/index.md

Quick view:
  cat ~/.scv/index.md
```

## Sample generated format

When installed with `--lang=en`, the generated file is in English:

```markdown
# SCV Analysis Index

## Directory Structure

| Directory | Contents |
|-----------|----------|
| `~/.scv/repos/` | Source code repositories |
| `~/.scv/analysis/` | Code analysis documents |

---

## Project Index

> Auto-generated · 2026-04-13 02:00 UTC

| # | Project | Summary | Responsibilities |
|---|---------|---------|------------------|
| 1 | ecif-core | Enterprise customer info platform providing unified data services. | 客户管理、数据同步、权限控制、审计日志、API网关 |
| 2 | ihybrid-forex | Forex hybrid trading system with multi-currency realtime quotes. | 报价引擎、订单管理、风控系统、账户结算 |
| 3 | log-collector | Distributed log collection and analysis tool. | - |
```

## When to Use

- **After `scv run`**: Index is automatically updated (Step 7.5)
- **After `scv batchRun`**: Index is automatically updated (Step 6.5)
- **Manual rebuild**: Use `/scv index` to manually rebuild the index at any time
- **Index missing or corrupted**: Run `/scv index` to regenerate from existing analysis data
