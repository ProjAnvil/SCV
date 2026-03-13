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

5. **Display analysis plan**:
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

### Step 5: Launch project-analyzer Subagent

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
  """
)
```

**Why use subagent?**
- **Context Isolation**: Analysis runs in isolated context, main conversation stays clean
- **Consistent Quality**: Same analysis engine for both single and batch runs
- **Token Efficiency**: Large codebase analysis doesn't pollute main context

### Step 6: Completion Report

After subagent completes, display:

```
✅ Analysis completed for: {project_name}

Repository information:
  📁 Type: {remote/local-git/local-non-git}
  📂 Location: {analysis_path}
  🌿 Branch: {branch} (for Git repositories)
  📦 Latest commit: {commit_hash} - {commit_message} (for Git repositories)

Output directory:
  📂 ~/.scv/analysis/{repo_name}/

Generated documents:
  📄 README.md        - Project overview
  📄 SUMMARY.md       - 5-minute summary
  📄 ARCHITECTURE.md  - Architecture details
  📄 FILE_INDEX.md    - File index

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
