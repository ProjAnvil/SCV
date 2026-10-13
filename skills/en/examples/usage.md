# SCV Usage Examples

Example invocations for every subcommand, with the flow each one triggers.

## run — analyze a remote repository

```
/scv run https://github.com/user/project.git
```

Flow: create `~/.scv` dirs → `git clone` into `~/.scv/repos/project` → read commit
info with `scv_util.py get-commit-info` → launch the `project-analyzer` subagent →
write metadata → update `~/.scv/index.md` → completion report pointing at
`~/.scv/analysis/project/` (README / SUMMARY / ARCHITECTURE / FILE_INDEX).

Custom display name: `/scv run https://github.com/user/project.git "My Project"`

## run — analyze a local non-Git directory

```
/scv run ~/projects/my-notes "My Notes"
```

No clone: the folder is analyzed in place; output still goes to
`~/.scv/analysis/my-notes/`; commit metadata is skipped.

## gather — clone, update, list, remove

```
/scv gather https://github.com/user/awesome-project.git main   # clone branch main
/scv gather --batch                                            # clone all remote repos in config
/scv gather --update awesome-project                           # git pull one repo
/scv gather --update-all                                       # update everything
/scv gather --list                                             # table of cloned repos
/scv gather --remove awesome-project                           # asks yes/no confirmation first
```

## batchRun — analyze everything in the config

```
/scv batchRun
```

Reads `~/.scv/config.json`, plans via `batch_manager.py`, analyzes every repo
(remote + local) with the same analyzer flow, then updates the index and prints
a per-repo summary.

## index — rebuild the analysis index

```
/scv index
```

Lists `~/.scv/analysis/`, reads each project's README.md directly (never via a
parsing script), then writes `~/.scv/index.md` with `scv_util.py update-index`.
