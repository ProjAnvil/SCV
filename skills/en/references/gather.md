# scv gather - Repository Management

Clone and manage remote Git repositories for later analysis.

## Usage

```
/scv gather <git_url> [branch]           # Clone a single repository
/scv gather --batch [--config <path>]    # Batch-clone repositories from the config
/scv gather --update [repo_name]         # Update a specific repository
/scv gather --update-all                 # Update all repositories
/scv gather --list                       # List all repositories
/scv gather --remove <repo_name>         # Remove a repository
```

## Execution Steps

### Step 1: Initialize the repository directory

```bash
mkdir -p ~/.scv/repos
```

Display storage locations:
```
📁 Repository storage: ~/.scv/repos
🔧 Config file: ~/.scv/config.json
```

### Step 2: Parse arguments and determine the mode

| Mode | Arguments | Description |
|------|-----------|-------------|
| Single clone | `<git_url> [branch]` | Clone a single Git repository |
| Batch clone | `--batch` | Clone all remote repositories from the config file |
| Update repo | `--update [repo_name]` | Update a specific repository |
| Update all | `--update-all` | Update all repositories |
| List repos | `--list` | Show all cloned repositories |
| Remove repo | `--remove <repo_name>` | Delete a specific repository |

With no arguments, show usage:
```
Usage: /scv gather <git_url> [branch]
      /scv gather --batch [--config <path>]
      /scv gather --update [repo_name] | --update-all
      /scv gather --list
      /scv gather --remove <repo_name>

Commands:
  Clone a single repo:    /scv gather <git_url> [branch]
  Batch clone:            /scv gather --batch [--config <path>]
  Update a repo:          /scv gather --update [repo_name]
  Update all repos:       /scv gather --update-all
  List repos:             /scv gather --list
  Remove a repo:          /scv gather --remove <repo_name>

Config file: ~/.scv/config.json

Examples:
  /scv gather https://github.com/user/project.git
  /scv gather https://github.com/user/project.git main
  /scv gather --batch
  /scv gather --update my-project
  /scv gather --list
```

### Step 3: Single repository clone mode

1. **Validate the Git URL**:
   - Check that it is a valid Git URL
   - Extract `repo_name`: `basename(url)` without the `.git` suffix

2. **Check whether it already exists**:
   - Target path: `~/.scv/repos/{repo_name}`
   - If it exists, ask the user:
     ```
     Repository '{repo_name}' already exists at ~/.scv/repos/{repo_name}
     Options:
       1. Pull latest changes (git pull)
       2. Delete and re-clone
       3. Skip, use the existing copy
     Choice [1/2/3]:
     ```

3. **Clone the repository**:
   ```bash
   cd ~/.scv/repos
   git clone {git_url} {repo_name}
   ```
   With an explicit branch:
   ```bash
   git clone -b {branch} {git_url} {repo_name}
   ```

4. **Verify and show the result**:
   ```
   ✅ Repository cloned successfully
   📁 Path: ~/.scv/repos/{repo_name}
   🔗 URL: {git_url}
   🌿 Branch: {current_branch}
   📦 Latest commit: {commit_hash} - {commit_message}

   💡 Tip: to add this repository to the batch analysis config, edit ~/.scv/config.json
   ```

### Step 4: Batch clone mode

1. **Determine the config file**:
   - Default: `~/.scv/config.json`
   - `--config <path>`: use the given path

2. **Load the config and filter**:
   - Only process repositories with `type: "remote"`
   - Skip repositories with `type: "local"`

3. **Show the batch plan**:
   ```
   📋 Batch Clone Plan
   Config: ~/.scv/config.json
   Output directory: ~/.scv/analysis
   Remote repositories: {N}
   Local repositories (skipped): {N}

   Remote repositories to clone:
     1. [Remote Project 1] https://github.com/user/repo1.git (branch: main)
        → ~/.scv/repos/repo1
     2. [Remote Project 2] https://github.com/user/repo2.git (branch: develop)
        → ~/.scv/repos/repo2

   Skipped (local):
     3. [Local Project 3] ~/local/repo3 (type: local, not cloned by gather)
   ```

4. **Execute the clones**:
   - For each repository, check whether it exists
   - Exists: run `git pull`
   - Not exists: run `git clone`

5. **Track progress**:
   ```
   ✅ [1/2] Remote Project 1 cloned
   📁 ~/.scv/repos/repo1
   🌿 Branch: main

   ⚠️ [2/2] Remote Project 2 already exists, updated
   📁 ~/.scv/repos/repo2
   🌿 Branch: develop
   ```

6. **Generate the summary**:
   ```
   ╔════════════════════════════════════════════════════════════╗
   ║         Batch Clone Complete                                 ║
   ╚════════════════════════════════════════════════════════════╝

   Config: ~/.scv/config.json
   Results:
     ✅ Newly cloned: {N}/{total}
     ✅ Updated: {N}/{total}
     ⚠️ Skipped: {N}/{total}
     ❌ Failed: {N}/{total}

   Remote repositories processed:
     1. Remote Project 1 → ~/.scv/repos/repo1
     2. Remote Project 2 → ~/.scv/repos/repo2

   Failed repositories:
     1. Remote Project 3 → {error}

   Next steps:
     - Run /scv batchRun to analyze all repositories (remote + local)
   ```

### Step 5: Update repository mode

**Update a single repository** (`--update [repo_name]`):

1. Without a name, list available repositories and let the user choose
2. Verify the repository exists at `~/.scv/repos/{repo_name}`
3. Run the update:
   ```bash
   cd ~/.scv/repos/{repo_name}
   git fetch origin
   git pull origin {current_branch}
   ```
4. Show the result:
   ```
   ✅ Repository updated successfully
   📁 ~/.scv/repos/{repo_name}
   🌿 Branch: {branch}
   📦 Latest commit: {commit_hash} - {commit_message}
   ```

**Update all repositories** (`--update-all`):

1. List all directories under `~/.scv/repos/`
2. Run `git pull` for every Git repository
3. Show a summary of the results

### Step 6: List repositories mode

1. Scan the `~/.scv/repos/` directory
2. For each subdirectory:
   - Check whether it is a Git repository
   - Extract: name, remote URL, current branch, last commit date

3. Display the table:
   ```
   📁 Local repositories (stored in ~/.scv/repos/)

   | Name          | Branch  | Last commit        | URL                          |
   |---------------|---------|--------------------|------------------------------|
   | my-app        | main    | 2025-01-15 10:30   | github.com/user/my-app.git   |
   | api-service   | develop | 2025-01-14 14:22   | github.com/user/api.git      |
   | cli-tools     | master  | 2025-01-10 09:15   | github.com/user/cli.git      |

   Total: 3 repositories
   ```

### Step 7: Remove repository mode

1. Verify that `repo_name` was provided
2. Confirm the deletion:
   ```
   ⚠️  About to delete repository: {repo_name}
   📁 Location: ~/.scv/repos/{repo_name}

   This deletes the entire directory and all of its contents.
   This does NOT remove the entry from ~/.scv/config.json.

   Are you sure? Type 'yes' to confirm:
   ```
3. After confirmation, execute:
   ```bash
   rm -rf ~/.scv/repos/{repo_name}
   ```
4. Report success:
   ```
   ✅ Repository deleted
   📁 ~/.scv/repos/{repo_name}
   💡 Tip: to also remove it from the config, edit ~/.scv/config.json
   ```

## Config File Schema

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
  "parallel": false,
  "fail_fast": false
}
```

### Directory name extraction

**Remote repositories**:
- URL: `https://github.com/user/my-project.git` → directory: `my-project`
- URL: `git@github.com:user/awesome-app.git` → directory: `awesome-app`

**Local repositories**:
- Path: `~/projects/my-app` → directory: `my-app`

## Error Handling

| Error | Handling |
|-------|----------|
| Authentication failed | Guide the user to configure Git credentials |
| Invalid URL | Report the error, suggest the correct format |
| Repository already exists | Ask the user what to do |
| Insufficient permissions | Check directory permissions |
| Network error | Check the network connection and repository accessibility |

## Integration with Other Commands

```bash
# 1. Clone a repository
/scv gather https://github.com/user/awesome-project.git

# 2. Analyze it right away
/scv run ~/.scv/repos/awesome-project
```

```bash
# 1. Create the config
cat > ~/.scv/config.json <<EOF
{
  "output_dir": "~/.scv/analysis",
  "repos": [
    {"type": "remote", "url": "https://github.com/team/backend.git", "project_name": "Backend"},
    {"type": "remote", "url": "https://github.com/team/frontend.git", "project_name": "Frontend"},
    {"type": "local", "path": "~/projects/docs", "project_name": "Docs"}
  ]
}
EOF

# 2. Batch-clone the remote repositories
/scv gather --batch

# 3. Verify
/scv gather --list

# 4. Batch-analyze (remote + local)
/scv batchRun
```

## Best Practices

1. **Use meaningful project names** for easier identification
2. **Specify a branch** when you need a specific version
3. **Update regularly** with `--update-all`
4. **Clean up** with `--remove` for repositories you no longer need
5. **Check status** with `--list` to see all repositories

## Notes

- `scv gather` processes **only** repositories with `type: "remote"` in the config
- `scv batchRun` processes **both remote and local** types
- Directory names are **extracted automatically** from the URL or path
- `project_name` is used for display in documents and reports
- All analysis output is stored in the **same** `output_dir`
