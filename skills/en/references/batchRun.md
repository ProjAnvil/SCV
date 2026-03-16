# scv batchRun - Batch Parallel Analysis

Batch analyze multiple repositories using parallel `project-analyzer` subagents.

## Usage

```
/scv batchRun
```

Reads repository configuration from `~/.scv/config.json` and executes batch analysis.

## Core Advantages

| Feature | Description |
|---------|-------------|
| **True Parallelism** | Fork independent `project-analyzer` subagent for each repo |
| **Context Isolation** | Each analysis completes in isolated context |
| **Progress Tracking** | Real-time progress for each repository |
| **Fault Tolerance** | Single failure doesn't affect other analyses |

## Execution Steps

### Step 1: Check and Load Configuration

1. **Verify config file exists**: `~/.scv/config.json`
2. If missing, report error with example:
   ```
   ❌ Configuration file not found: ~/.scv/config.json

   Please create a config file with this structure:

   {
     "output_dir": "~/.scv/analysis",
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

3. **Load and validate configuration**:
   - Parse JSON
   - Verify `repos` array exists
   - Get `output_dir` (default: `~/.scv/analysis`)
   - Validate each repository entry:
     - `type`: must be `"remote"` or `"local"`
     - Remote repo: validate `url` is valid
     - Local repo: validate `path` exists

### Step 2: Display Analysis Plan

```
📋 Batch Analysis Plan

Config file: ~/.scv/config.json
Output directory: ~/.scv/analysis
Repository count: {N} (remote: {N}, local: {N})
Parallel execution: {true/false}
Fail fast: {true/false}

Repositories to analyze:
  1. [remote] {project_name}
     URL: {url}
     Branch: {branch}
     Local path: ~/.scv/repos/{repo_name}
     Output: ~/.scv/analysis/{repo_name}

  2. [local] {project_name}
     Path: {path}
     Output: ~/.scv/analysis/{repo_name}

  ...

Total: {N} repositories
All outputs will be saved to: ~/.scv/analysis/
```

### Step 3: Prepare Repositories

**For remote repositories**, execute git operations before analysis:

1. Extract `repo_name`: `basename(url)` remove `.git`
2. Local path: `~/.scv/repos/{repo_name}`
3. Check if exists:
   - Exists: `git pull {branch}`
   - Not exists: `git clone -b {branch} {url} {repo_name}`
4. Display result:
   ```
   🔄 Remote repository: {project_name}
   📁 Local: ~/.scv/repos/{repo_name}
   🌿 Branch: {branch}

   ✅ Git pull complete
   Latest: {commit_hash} - {commit_message}
   ```

**For local repositories**:
1. Extract `repo_name`: `basename(path)`
2. Verify path exists and is accessible
3. Display:
   ```
   📂 Local repository: {project_name}
   📁 Path: {path}
   ```

### Step 4: Create Task List for Tracking

**Use TodoWrite to create a task list for tracking all repository analyses:**

```
TodoWrite([
  { content: "Analyze {project_name_1}", status: "in_progress", activeForm: "Analyzing {project_name_1}" },
  { content: "Analyze {project_name_2}", status: "pending", activeForm: "Analyzing {project_name_2}" },
  ...
  { content: "Analyze {project_name_N}", status: "pending", activeForm: "Analyzing {project_name_N}" }
])
```

This enables:
- Real-time progress tracking across all repositories
- Clear visibility of completed vs pending tasks
- Final result aggregation in batch summary

### Step 5: Parallel Execution with Concurrency Limit

⚠️ **Critical Constraint: Maximum concurrent subagents MUST be 5. This is a hard limit!**

**Why must we limit concurrency?**
- Too many simultaneous subagents consume excessive system resources
- May cause API rate limiting or timeouts
- Affects overall task stability

**Batch Processing Logic (Pseudocode):**

```
repos = [repo1, repo2, ..., repoN]  # All repositories
BATCH_SIZE = 5                       # Max 5 per batch

for batch_start in range(0, len(repos), BATCH_SIZE):
    batch = repos[batch_start : batch_start + BATCH_SIZE]

    # Step A: Fork subagents for this batch in a single turn
    for repo in batch:
        Agent(subagent_type="project-analyzer", ..., run_in_background=true)

    # Step B: [CRITICAL] Must wait for ALL subagents in this batch to complete
    # Use TaskOutput to block and wait for each subagent
    for each agent_task_id in batch:
        TaskOutput(task_id=agent_task_id, block=true, timeout=600000)

    # Step C: Only after this batch fully completes, proceed to next batch
```

**⚠️ Strict Execution Requirements:**
1. **DO NOT fork more than 5 subagents in a single turn**
2. **Each batch MUST use TaskOutput with block=true to wait for completion** - not just fire-and-forget
3. **Only after current batch fully completes** can the next batch be started in a new turn

**For each batch, use Agent tool to fork subagents:**

```
Agent(
  subagent_type="project-analyzer",
  description="Analyze {project_name}",
  prompt="""
  Analyze the codebase and generate documentation.

  Input Parameters:
  - Project Path: {analysis_path}
  - Output Directory: {output_dir}/{repo_name}/
  - Project Name: {project_name}
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
  run_in_background=true
)
```

**When each subagent completes, update the task list:**

```
TodoWrite - mark the corresponding task as completed
```

**When `parallel = false`, execute sequentially:**

Process repositories one by one, waiting for each to complete before starting the next.

### Step 6: Track Progress

**In parallel mode**, receive notification when each subagent completes and update task status:

```
✅ [1/3] {project_name} complete
   Type: remote
   📁 Local: ~/.scv/repos/{repo_name}
   📂 Output: ~/.scv/analysis/{repo_name}/
   📄 4 documents generated
   🌿 Branch: {branch}

✅ [2/3] {project_name} complete
   Type: local
   📁 Path: {path}
   📂 Output: ~/.scv/analysis/{repo_name}/
   📄 4 documents generated

❌ [3/3] {project_name} failed - Analysis failed
   Type: {remote/local}
   📁 Path: {actual_path}
   🚫 Error: {error_message}
```

### Step 7: Generate Batch Summary

After all analyses complete (or stop if `fail_fast = true`), verify all tasks in TodoWrite are completed:

```
╔════════════════════════════════════════════════════════════╗
║         Batch Analysis Complete                            ║
╚════════════════════════════════════════════════════════════╝

Config file: ~/.scv/config.json
Output directory: ~/.scv/analysis
Execution mode: {sequential/parallel}
Duration: {X minutes Y seconds}

Results:
  ✅ Success: {N}/{total}
  ❌ Failed: {N}/{total}
  ⏭️ Skipped: {N}/{total}

  Remote repositories: {N} analyzed
  Local repositories: {N} analyzed

Successful repositories:
  1. [remote] {project_name} → ~/.scv/analysis/{repo_name}/
  2. [local] {project_name} → ~/.scv/analysis/{repo_name}/
  ...

Failed repositories:
  1. [remote] {project_name} → {error}
  2. [local] {project_name} → {error}
  ...

Output locations:
  All analyses saved to: ~/.scv/analysis/
  Remote repos cloned to: ~/.scv/repos/
```

### Step 8: Suggest Next Steps

```
What's next?

  📖 Browse documentation
     - Open ~/.scv/analysis/ to view generated documents

  🔍 Analyze single repository
     - Use /scv run <repo_path> for targeted analysis

  🌐 Collect remote repositories
     - Use /scv gather --batch to clone/pull all remote repos

  🔄 Re-run batch analysis
     - Use /scv batchRun to regenerate all documents (auto-pulls remote repos)

  📋 List all repositories
     - Use /scv gather --list to view cloned remote repos
```

## Configuration File Schema

### ~/.scv/config.json

```json
{
  "output_dir": "~/.scv/analysis",
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
  ],
  "parallel": true,
  "fail_fast": false
}
```

### Field Descriptions

| Field | Required | Description | Default |
|-------|----------|-------------|---------|
| `output_dir` | No | Output directory for all analyses | `~/.scv/analysis` |
| `parallel` | No | Execute in parallel | `true` |
| `fail_fast` | No | Stop on error | `false` |

**Remote repository fields:**
- `type`: `"remote"` (required)
- `url`: Git repository URL (required)
- `project_name`: Project name (optional)
- `branch`: Branch (optional)

**Local repository fields:**
- `type`: `"local"` (required)
- `path`: Local path (required, supports `~`)
- `project_name`: Project name (optional)

## Error Handling

| Error | Handling |
|-------|----------|
| Config file not found | Show example config, exit |
| Invalid repository type | Report error, skip repository |
| Git pull failed | Report error, decide based on `fail_fast` |
| Local path not found | Report error, skip repository |
| Analysis failed | Log error, continue with other repositories |

## Subagent Strategy Details

### Why Use Subagents?

1. **Context Isolation**: Each repository analysis in isolated context, avoids token accumulation
2. **True Parallelism**: Multiple subagents run simultaneously, significantly reduces total time
3. **Fault Tolerance**: Single analysis failure doesn't affect others
4. **Trackability**: Receive notification when each subagent completes

### Subagent Type Selection

Using `project-analyzer` subagent type - a specialized agent with:
- **Tools**: Read, Write, Glob, Grep, LSP (optimized for code analysis)
- **Focus**: Single-purpose deep code analysis
- **Output**: Structured documentation following templates

## Best Practices

1. **First run**: Run `/scv gather --batch` first to clone all remote repos
2. **Regular updates**: `batchRun` auto-pulls, but can manually `--update-all`
3. **Check config**: Periodically view `~/.scv/gather --list` to confirm repo status
4. **Parallel settings**: Adjust `parallel` and repo count based on machine performance
