# scv batchRun - Batch Parallel Analysis

Batch analyze multiple repositories using parallel `project-analyzer` subagents.

---

## ⛔ Core Constraints (MUST FOLLOW)

| Constraint | Value | Description |
|------------|-------|-------------|
| **Max Concurrency** | **`batch_size` from config** | Hard limit on concurrent subagents per batch (default: 5) |
| **Batch Processing** | **REQUIRED** | Next batch starts only after current batch fully completes |
| **Python-Driven State** | **REQUIRED** | All planning, git ops, and batch splits are done by `batch_manager.py plan`. The LLM NEVER computes batches itself |

**Consequences of violating constraints:**
- Exceeding `batch_size` concurrent subagents exhausts system resources
- Not processing in batches leads to untrackable subagents and lost progress

---

## Usage

```
/scv batchRun [--analyze-only]
```

Reads `~/.scv/config.json`, delegates all planning to `batch_manager.py plan`, then executes analysis in batches.

**Options:**
- `--analyze-only`: Skip git clone/pull, only verify local paths exist

## Core Advantages

| Feature | Description |
|---------|-------------|
| **Python-Controlled Batching** | `batch_manager.py` owns all state, git ops, and batch splits — LLM cannot miscount |
| **Config-Driven Concurrency** | `batch_size` in config.json sets the hard limit, no prompt-level override |
| **Context Isolation** | Each analysis runs in isolated subagent context |
| **Fault Tolerance** | Single failure doesn't affect other analyses |
| **Crash Recovery** | Idempotent `next` command returns in-progress batch on resume |

---

## Execution Steps

### Step 1: Check Configuration

Verify `~/.scv/config.json` exists. If missing:

```
❌ Configuration file not found: ~/.scv/config.json

Please create a config file:

{
  "output_dir": "~/.scv/analysis",
  "batch_size": 5,
  "repos": [
    {
      "type": "remote",
      "url": "https://github.com/user/repo1.git",
      "project_name": "Remote Project 1",
      "branch": "main"
    },
    {
      "type": "local",
      "path": "~/local/path/to/repo2",
      "project_name": "Local Project 2"
    }
  ],
  "parallel": true,
  "fail_fast": false
}
```

### Step 2: Run `batch_manager.py plan` (Python Handles Everything)

> ⛔ **MANDATORY: Call `plan` first. Do NOT read config yourself, do NOT run git manually, do NOT calculate batches.**
>
> `plan` does all of this atomically:
> 1. Reads `~/.scv/config.json` (including `batch_size`)
> 2. Runs `git clone` or `git pull` for every remote repo
> 3. Verifies local repo paths exist
> 4. Splits repos into batches of `batch_size`
> 5. Persists the full execution plan to `~/.scv/sessions/{session_id}.json`

**Get the current session ID** from the OpenCode session context (`session_id` field).
If unavailable, use a timestamp-based fallback: `scv_batch_{timestamp}`.

```bash
python3 ~/.claude/skills/scv/scripts/batch_manager.py plan \
  --session {YOUR_SESSION_ID} \
  --config ~/.scv/config.json
```

For `--analyze-only` mode (skip git operations):

```bash
python3 ~/.claude/skills/scv/scripts/batch_manager.py plan \
  --session {YOUR_SESSION_ID} \
  --config ~/.scv/config.json \
  --analyze-only
```

Expected output:

```json
{
  "status": "planned",
  "session_id": "ses_abc123",
  "total_repos": 10,
  "ready_repos": 8,
  "failed_repos": 1,
  "skipped_repos": 1,
  "total_batches": 2,
  "batch_size": 5,
  "git_errors": [
    { "repo": "some_repo", "error": "git clone failed: ..." }
  ],
  "skipped": [
    { "repo": "unchanged_repo", "reason": "Commit unchanged since 2026-03-17T06:00:00+00:00" }
  ],
  "state_file": "~/.scv/sessions/ses_abc123.json"
}
```

If `failed_repos > 0`, those repos are already marked `failed` in the plan — they will be skipped in batch execution. Report the git errors to the user but continue.

If `skipped_repos > 0`, those repos have the same commit hash as their last successful analysis — no new analysis needed. Report them to the user as "up-to-date".

**Create TodoWrite for UI visibility:**

```
TodoWrite([
  { content: "Batch 1/{total_batches}: repos 1-{batch_size}", status: "pending" },
  { content: "Batch 2/{total_batches}: repos ...", status: "pending" },
  ...
  { content: "Generate final summary", status: "pending" }
])
```

### Step 3: Batch Parallel Execution (Loop Driven by `next`)

> ⛔ **The batch loop MUST be driven by `batch_manager.py next`. Never continue from in-context memory.**
>
> `next` returns exactly the repos for the next batch — the LLM forks one subagent per repo, collects all results, reports back, then calls `next` again.

#### 3.1 The Batch Loop

```
┌─────────────────────────────────────────────────────────────────────┐
│  LOOP: repeat until `next` exits with code 2                        │
│                                                                     │
│  ① batch_manager.py next --session {id}                             │
│     → Returns JSON: { batch_num, total_batches, batch_size, repos } │
│     → Exit code 2 = no more batches → stop loop                    │
│                                                                     │
│  ② Fork ALL repos in this batch as background subagents             │
│     (use repo.local_path from the plan — already resolved by plan)  │
│                                                                     │
│  ③ Block until ALL subagents in this batch complete                 │
│     (background_output for each task_id)                            │
│                                                                     │
│  ④ For each result:                                                  │
│     Success → batch_manager.py complete --session {id} --repo {name}│
│     Failure → batch_manager.py fail --session {id} --repo {name}   │
│                                --error "msg"                        │
│                                                                     │
│  ⑤ Mark current batch todo as completed                             │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

#### 3.2 Pseudocode

```
SESSION_ID = {YOUR_SESSION_ID}
BM = "python3 ~/.claude/skills/scv/scripts/batch_manager.py"

while True:
    result = Bash(f"{BM} next --session {SESSION_ID}")
    if result.exit_code == 2:
        break

    batch = parse_json(result.stdout)
    batch_num = batch["batch_num"]
    total_batches = batch["total_batches"]
    repos = batch["repos"]          # local_path already resolved
    output_dir = batch["output_dir"]

    print(f"🔄 Batch {batch_num}/{total_batches} ({len(repos)} repos)")
    TodoWrite - mark "Batch {batch_num}/{total_batches}" as in_progress

    # Fork all subagents simultaneously
    task_ids = {}
    for repo in repos:
        task_id = Agent(
            subagent_type="project-analyzer",
            description=f"Analyze {repo['project_name']}",
            prompt=build_prompt(repo, output_dir),
            run_in_background=True
        )
        task_ids[repo["project_name"]] = task_id

    # Collect all results before proceeding
    for repo_name, task_id in task_ids.items():
        output = background_output(task_id=task_id, block=True, timeout=600000)
        if output.success:
            Bash(f"{BM} complete --session {SESSION_ID} --repo '{repo_name}'")
        else:
            Bash(f"{BM} fail --session {SESSION_ID} --repo '{repo_name}' --error '{output.error}'")

    TodoWrite - mark "Batch {batch_num}/{total_batches}" as completed

print("✅ All batches complete")
```

#### 3.3 Subagent Call Template

```
Agent(
  subagent_type="project-analyzer",
  description="Analyze {repo['project_name']}",
  prompt="""
  Analyze the codebase and generate documentation.

  Input Parameters:
  - Project Path: {repo['local_path']}
  - Output Directory: {output_dir}/{repo['repo_name']}/
  - Project Name: {repo['project_name']}
  - Current Commit: {repo.get('current_commit', 'N/A')}
  - Templates Directory: {skill_path}/references/templates/

  Execute the 3-phase analysis workflow:
  1. Phase 1: Global Scan - Identify tech stack and structure
  2. Phase 2: Deep File Analysis - Analyze priority files
  3. Phase 3: Document Generation - Create 4 documents

  Generate these documents in the output directory:
  - README.md - Project overview
  - SUMMARY.md - 5-minute summary
  - ARCHITECTURE.md - Architecture design
  - FILE_INDEX.md - File index

  Follow the templates strictly and mark uncertain items with [To be confirmed].
  """,
  run_in_background=True
)
```

> **Note:** `repo['current_commit']` is populated by `batch_manager.py plan` and stored in the session state. It is `None` for non-Git local directories.

#### 3.4 Sequential Mode (`parallel: false`)

- Still call `next` to get each batch
- Fork one subagent at a time, block before proceeding to the next
- Suitable for debugging or resource-constrained scenarios

#### 3.5 Crash Recovery

```bash
# See what's still in-progress or pending
python3 ~/.claude/skills/scv/scripts/batch_manager.py resume --session {SESSION_ID}

# Call next again — returns the interrupted batch (idempotent)
python3 ~/.claude/skills/scv/scripts/batch_manager.py next --session {SESSION_ID}
```

`next` is **idempotent**: if a batch is already `in_progress`, it returns that same batch instead of skipping forward.

### Step 4: Track Progress

```
✅ [1/N] {project_name} complete
   📁 Path: {local_path}
   📂 Output: ~/.scv/analysis/{repo_name}/
   📦 Commit: {short_commit_hash} - {commit_message}
   📄 4 documents generated

⏭️ [2/N] {project_name} skipped (commit unchanged)
   📦 Commit: {short_commit_hash} (same as last analysis)
   💡 Docs already up-to-date

❌ [3/N] {project_name} failed
   🚫 Error: {error_message}
```

### Step 5: Generate Batch Summary

```
╔═══════════════════════════════════════════════════╗
║         Batch Analysis Complete                   ║
╚═══════════════════════════════════════════════════╝

Config: ~/.scv/config.json
Output: ~/.scv/analysis/
Batch size: {batch_size}
Mode: {parallel/sequential}

Results:
  ✅ Success: {N}/{total}
  ❌ Failed:  {N}/{total}
  ⏭️ Skipped (unchanged): {N}/{total}

Successful:
  1. [remote] {project_name} → ~/.scv/analysis/{repo_name}/
  2. [local]  {project_name} → ~/.scv/analysis/{repo_name}/

Skipped (commit unchanged):
  1. {project_name} → last analyzed at {last_analyzed_at}

Failed:
  1. {project_name} → {error}
```

### Step 6: Suggest Next Steps

```
📖 Browse docs:    open ~/.scv/analysis/
🔍 Single repo:    /scv run <repo_path>
🔄 Re-run:         /scv batchRun
📋 List repos:     /scv gather --list
📇 View index:     cat ~/.scv/index.md
```

### Step 6.5: Update Analysis Index

After all batches complete, rebuild the global index file. Follow the full steps in `references/index.md`:
List all project directories → read each README to extract summary and functional keywords → assemble entries JSON → call the script to write `~/.scv/index.md`.

See: `references/index.md`

---

## Configuration Reference

### ~/.scv/config.json

```json
{
  "output_dir": "~/.scv/analysis",
  "batch_size": 5,
  "parallel": true,
  "fail_fast": false,
  "repos": [
    {
      "type": "remote",
      "url": "https://github.com/user/repo.git",
      "project_name": "Custom Name",
      "branch": "main"
    },
    {
      "type": "local",
      "path": "~/local/path/to/repo",
      "project_name": "Local Project"
    }
  ]
}
```

| Field | Required | Description | Default |
|-------|----------|-------------|---------|
| `output_dir` | No | Output directory for all analyses | `~/.scv/analysis` |
| `batch_size` | No | Max concurrent subagents per batch | `5` |
| `parallel` | No | Parallel within batch (false = sequential) | `true` |
| `fail_fast` | No | Stop all on first failure | `false` |

---

## Execution Requirement Summary

| Requirement | Rule | Violation Consequence |
|-------------|------|-----------------------|
| Call `plan` first | Always, before any `next` | Stale/missing state |
| Never parse config yourself | `plan` owns config reading | Wrong batch splits |
| Never run git yourself | `plan` owns git ops | Diverged local state |
| Always call `next` for batches | Never compute from memory | Wrong repos, silent skips |
| Collect ALL results before next batch | Serial between batches | Out-of-order state |
| Call `complete` or `fail` for every repo | No exceptions | State diverges from reality |
