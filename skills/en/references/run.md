# scv run - Single Repository Deep Analysis

Analyze a single code repository by launching a specialized `project-analyzer` subagent.

## Usage

```
/scv run <repo_path|url> [project_name]
```

**Parameters:**
- `repo_path|url` (required): Local path or remote Git URL
- `project_name` (optional): Custom project name

**Examples:**
```bash
# Remote repository
/scv run https://github.com/user/project.git
/scv run https://github.com/user/project.git "My Project"

# Local Git repository
/scv run ~/projects/my-project
/scv run ~/projects/my-project "My Project"

# Local non-Git directory
/scv run ~/my-folder "My Analysis"
```

## Execution Steps

### Step 1: Initialize Directories

```bash
mkdir -p ~/.scv/repos
mkdir -p ~/.scv/analysis
```

Display storage locations:
```
📁 Repository storage: ~/.scv/repos
📂 Analysis output: ~/.scv/analysis
```

### Step 2: Parse Input and Detect Type

Parse user input, extract:
- **repo_input** (required): Remote Git URL or local path
- **project_name** (optional): Custom project name (default: auto-extracted)

If no arguments, show usage and exit.

**Detect input type:**

| Type | Condition | Handling |
|------|-----------|----------|
| Remote Git URL | `https://`, `git://`, `git@` | Clone to `~/.scv/repos/` |
| Local Git Repo | Path contains `.git` directory | Clone to `~/.scv/repos/` |
| Local non-Git Dir | Other paths | Analyze directly, no copy |

### Step 3: Prepare Repository

**For Remote Git URL:**

1. Extract `repo_name`: `basename(url)` without `.git` suffix
2. Local path: `~/.scv/repos/{repo_name}`
3. Check if exists:
   - Exists: Ask user (pull / re-clone / use existing)
   - Not exists: Execute `git clone`
4. Verify clone success, show repository info

**For Local Git Repository:**

1. Extract `repo_name`: `basename(path)`
2. Clone to `~/.scv/repos/{repo_name}`
3. Verify clone success

**For Local non-Git Directory:**

1. Verify directory exists and is readable
2. Use original path for analysis

### Step 4: Determine Paths and Names

1. **Analysis path**:
   - Remote URL: `~/.scv/repos/{repo_name}`
   - Local Git: `~/.scv/repos/{repo_name}`
   - Local non-Git: Original path

2. **Output path**: Always `~/.scv/analysis/{repo_name}/`

3. **Project name**: Use user-provided name, or `repo_name`

4. **Create output directory**: `mkdir -p ~/.scv/analysis/{repo_name}`

5. **Get commit info** (Git repositories only):

   ```bash
   python3 ~/.claude/skills/scv/scripts/scv_util.py get-commit-info \
     --repo {analysis_path}
   ```

   Output: `{ "hash": "abc123...", "short_hash": "abc123", "message": "...", ... }`
   Store `current_commit = hash` and `short_commit = short_hash` for use in Steps 5 and 6.
   If not a git repo or command fails, set `current_commit = null`.

6. **Display analysis plan**:
   ```
   📋 Analysis Plan

   Project name: {project_name}
   Repository type: {remote/local-git/local-non-git}

   Repository location:
   {analysis_path}

   Output directory:
   ~/.scv/analysis/{repo_name}/

   Launching project-analyzer subagent...
   ```

### Step 5: Check Deep Analysis Requirements

**Read configuration file** to check `deep_analysis_enabled`:

```bash
cat ~/.scv/config.json 2>/dev/null | grep -q '"deep_analysis_enabled"[[:space:]]*:[[:space:]]*true'
```

**If `deep_analysis_enabled` is `true`:**

1. **Check if codebones is installed:**

   ```bash
   which codebones
   ```

   Exit code 0 = installed, Exit code 1 = not installed

2. **If codebones is NOT installed:**

   ```
   ⚠️ Deep analysis is enabled but codebones is not installed.

   Install:
     pip install codebones
     # or
     cargo install codebones

   Would you like to:
   1. Install codebones now
   2. Continue with standard analysis
   3. Cancel
   ```

3. **If codebones IS installed, prepare deep analysis infrastructure:**

   ```bash
   # Build index first (required for all codebones operations)
   codebones index {analysis_path}

   # Generate skeleton for overview (85% token reduction)
   codebones pack {analysis_path} --format markdown --max-tokens 100000 > ~/.scv/analysis/{repo_name}/.codebones_skeleton.md
   ```

   Set:
   - `use_deep_analysis = true`
   - `skeleton_file = ~/.scv/analysis/{repo_name}/.codebones_skeleton.md`
   - `repo_path = {analysis_path}` (for codebones get/search operations)

**If `deep_analysis_enabled` is `false` or not set:**

- Set `use_deep_analysis = false`
- Continue with standard analysis

### Step 6: Launch project-analyzer Subagent

**Launch the specialized subagent:**

```
Agent(
  subagent_type="project-analyzer",
  description="Analyze {project_name}",
  prompt="""
  Analyze the codebase and generate documentation.

  Input Parameters:
  - Project Path: {analysis_path}
  - Output Directory: {output_dir}
  - Project Name: {project_name}
  - Current Commit: {current_commit or 'N/A'}
  - Templates Directory: {skill_path}/references/templates/
  - Deep Analysis: {use_deep_analysis}
  - Skeleton File: {skeleton_file_path if use_deep_analysis else 'N/A'}
  - AI Model: Use your actual model name from the system context (e.g., claude-sonnet-4-6)

  Execute the 3-phase analysis workflow:
  1. Phase 1: Global Scan - Identify tech stack and structure
  2. Phase 2: Deep File Analysis - Analyze priority files
  3. Phase 3: Document Generation - Create 4 documents

  <!-- IF use_deep_analysis -->
  **Deep Analysis Mode Enabled - Progressive Deep Dive Strategy:**

  You have access to `codebones` CLI for token-efficient deep analysis:

  **Step 1: Read skeleton for overview (85% token reduction)**
  ```
  Read file: {skeleton_file_path}
  ```
  This gives you all class signatures, dependencies, and API mappings.

  **Step 2: Identify key symbols to deep dive**
  From the skeleton, identify:
  - Core Service classes (business logic)
  - Controller classes (API endpoints)
  - Critical configuration classes

  **Step 3: Use codebones get for full implementation (on-demand)**
  For each key symbol you need to understand deeply:
  ```bash
  # Get full source code of a specific class/method
  codebones get "path/to/file.rs::ClassName"
  codebones get "src/services/order.rs::OrderService.create_order"
  codebones get "src/controllers/user_controller.rs::UserController"
  ```

  **Step 4: Use codebones search to find related symbols**
  ```bash
  # Find all symbols containing "Order"
  codebones search "Order"

  # List all indexed symbols
  codebones search ""
  ```

  **Example deep dive workflow:**
  1. Skeleton shows `OrderService` has `@Autowired InventoryService`
  2. Use `codebones get "src/services/order.rs::OrderService"` to see full implementation
  3. See `create_order` method calls `inventory.check_stock`
  4. Use `codebones search "InventoryService"` to find all usages
  5. Document the service interaction chain

  This progressive approach saves ~85% tokens compared to reading all files,
  while still getting full implementation details where needed.
  <!-- END IF -->

  Generate these documents in the output directory:
  - README.md - Project overview
  - SUMMARY.md - 5-minute summary (include Service Business Association if deep analysis)
  - ARCHITECTURE.md - Architecture design
  - FILE_INDEX.md - File index

  Follow the templates strictly and mark uncertain items with [To be confirmed].
  When filling README.md, replace `{AI Model Name}` with your actual model name from the system context.
  """
)
```

**Why use subagent?**
- **Context Isolation**: Analysis runs in isolated context, main conversation stays clean
- **Consistent Quality**: Same analysis engine for both single and batch runs
- **Token Efficiency**: Large codebase analysis doesn't pollute main context

### Step 7: Write Metadata (Git repositories only)

After the subagent completes successfully, record the analyzed commit so future runs can skip unchanged repos:

```bash
python3 ~/.claude/skills/scv/scripts/scv_util.py write-metadata \
  --repo {analysis_path} \
  --commit {current_commit} \
  --output-dir ~/.scv/analysis/{repo_name}
```

Skip this step if `current_commit` is null (non-Git directory) or if the subagent failed.

### Step 8: Completion Report

After subagent completes, display:

```
✅ Analysis completed for: {project_name}

Repository information:
  📁 Type: {remote/local-git/local-non-git}
  📂 Location: {analysis_path}
  🌿 Branch: {branch} (for Git repositories)
  📦 Latest commit: {commit_hash} - {commit_message} (for Git repositories)

Analysis mode:
  🔍 Deep Analysis: {use_deep_analysis ? 'Enabled (codebones skeleton)' : 'Standard'}

Output directory:
  📂 ~/.scv/analysis/{repo_name}/

Generated documents:
  📄 README.md        - Project overview
  📄 SUMMARY.md       - 5-minute summary
  📄 ARCHITECTURE.md  - Architecture details
  📄 FILE_INDEX.md    - File index

<!-- IF use_deep_analysis -->
Service Business Association section included in SUMMARY.md
<!-- END IF -->

Next steps:
  - Review generated documents
  - Open README.md: open ~/.scv/analysis/{repo_name}/README.md

Repository management:
  - Update: /scv gather --update {repo_name}
  - List: /scv gather --list
  - Remove: /scv gather --remove {repo_name}
```

## Error Handling

**Remote URL errors:**
- Invalid URL: Report error, suggest correct format
- Clone failed: Check network and Git credentials
- Authentication required: Guide to configure Git credentials

**Local path errors:**
- Path not found: Report error
- Permission denied: Check read permissions
- Empty directory: Warn but proceed with minimal documentation

**Analysis errors:**
- Subagent failure: Report error details, suggest retry
- Partial failure: Generate what's possible and report warnings
