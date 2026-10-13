"""Simulated environment for evaluating the SCV skill.

The SCV skill drives a shell-heavy workflow: git clone, the ~/.scv directory
layout, and the helper scripts (scv_util.py / batch_manager.py). Running it
against a real home directory would be destructive and slow, so ``run_command``
recognizes the commands the references use and returns plausible output, while
``read_file`` serves workspace files plus a simulated ~/.scv tree. Everything
is stateless — repeated runs stay comparable.
"""

from __future__ import annotations

import json
from pathlib import Path

from smelt import tool

CONFIG_JSON = json.dumps(
    {
        "lang": "en",
        "output_dir": "~/.scv/analysis",
        "batch_size": 5,
        "deep_analysis_enabled": False,
        "repos": [
            {
                "type": "remote",
                "url": "https://github.com/user/project1.git",
                "project_name": "Remote Project 1",
                "branch": "main",
            },
            {
                "type": "remote",
                "url": "https://github.com/user/project2.git",
                "project_name": "Remote Project 2",
                "branch": "develop",
            },
            {"type": "local", "path": "~/local/path/to/project3", "project_name": "Local Project 3"},
            {"type": "local", "path": "~/projects/another-repo"},
        ],
        "parallel": False,
        "fail_fast": False,
    },
    indent=2,
)

COMMIT_INFO = json.dumps(
    {
        "hash": "3f9c2e1a7b4d5c6e8f0a1b2c3d4e5f6a7b8c9d0e",
        "short_hash": "3f9c2e1",
        "message": "feat: add config parser and validation",
        "branch": "main",
        "author": "Dev A <deva@example.com>",
        "date": "2026-10-11T09:24:00+08:00",
    }
)

ANALYSIS_READMES = {
    "ecif-core": (
        "# ECIF Core\n\nEnterprise customer information platform. Provides a unified customer\n"
        "data model, sync adapters for CRM feeds, role-based access control, an\n"
        "audit log service, and an API gateway for downstream consumers.\n\n"
        "Stack: Java 21, Spring Boot 3, PostgreSQL, Kafka.\n"
    ),
    "log-collector": (
        "# Log Collector\n\nA small tool that tails log files and ships them to a storage backend.\n"
    ),
}


def _simulated_home_file(path: str) -> str | None:
    """Serve plausible content for reads under the simulated ~/.scv tree."""
    p = path.replace("\\", "/")
    if p.endswith("config.json"):
        return CONFIG_JSON
    if p.endswith("index.md"):
        return (
            "# SCV Analysis Index\n\n| # | Project | Summary | Responsibilities |\n"
            "|---|---------|---------|------------------|\n"
            "| 1 | ecif-core | Enterprise customer info platform. | 客户管理、数据同步、权限控制 |\n"
            "| 2 | log-collector | Distributed log collection tool. | - |\n"
        )
    for name, content in ANALYSIS_READMES.items():
        if name in p and p.endswith("README.md"):
            return content
    if "/.scv/repos/" in p or "/.scv/analysis/" in p:
        return None  # unknown file inside the simulated tree
    return None


@tool
def read_file(path: str) -> str:
    """Read a file: workspace-relative paths read the isolated case workspace; ~/.scv paths return simulated vault content."""
    p = Path(path).expanduser()
    str_p = str(p)
    if str_p.startswith(str(Path.home())):
        content = _simulated_home_file(str_p)
        if content is not None:
            return content
        raise FileNotFoundError(f"no such file in simulated environment: {path}")
    return Path(path).read_text(encoding="utf-8")


@tool
def run_command(cmd: str) -> str:
    """Run a shell command in the simulated environment (git, ls, mkdir, scv scripts)."""
    c = " ".join(cmd.split())

    if c.startswith("mkdir"):
        return ""
    if "git clone" in c:
        target = c.split()[-1] if not c.split()[-1].startswith(("https://", "git@", "git://")) else c.split()[-1].rstrip("/").removesuffix(".git").split("/")[-1]
        return (
            f"Cloning into '{target}'...\n"
            "remote: Enumerating objects: 842, done.\n"
            "remote: Counting objects: 100% (842/842), done.\n"
            "Receiving objects: 100% (842/842), 1.21 MiB | 3.40 MiB/s, done.\n"
            "Resolving deltas: 100% (412/412), done."
        )
    if "git fetch" in c:
        return "remote: Enumerating objects: 5, done.\nUnpacking objects: 100% (3/3), done."
    if "git pull" in c:
        return "Already up to date."
    if "which codebones" in c:
        return "which: no codebones in (/usr/local/bin:/usr/bin:/bin)"
    if "codebones" in c.split()[:1] or c.startswith("codebones"):
        return "zsh: command not found: codebones"
    if "scv_util.py get-commit-info" in c:
        return COMMIT_INFO
    if "scv_util.py write-metadata" in c:
        return json.dumps({"status": "metadata_written", "file": "~/.scv/analysis/.analysis_metadata.json"})
    if "scv_util.py update-index" in c:
        return json.dumps({"status": "index_updated", "index_file": "~/.scv/index.md", "total_projects": 2})
    if "batch_manager.py plan" in c:
        return json.dumps(
            {
                "status": "planned",
                "session_id": "bm-20261013-01",
                "total": 4,
                "remote": 2,
                "local": 2,
                "repos": [
                    {"name": "project1", "type": "remote", "source": "https://github.com/user/project1.git", "output": "~/.scv/analysis/project1"},
                    {"name": "project2", "type": "remote", "source": "https://github.com/user/project2.git", "output": "~/.scv/analysis/project2"},
                    {"name": "project3", "type": "local", "source": "~/local/path/to/project3", "output": "~/.scv/analysis/project3"},
                    {"name": "another-repo", "type": "local", "source": "~/projects/another-repo", "output": "~/.scv/analysis/another-repo"},
                ],
            },
            indent=2,
        )
    if "batch_manager.py start" in c:
        return json.dumps({"status": "started", "session_id": "bm-20261013-01", "total": 4})
    if "batch_manager.py status" in c:
        return json.dumps({"status": "in_progress", "session_id": "bm-20261013-01", "done": 0, "total": 4})
    if "batch_manager.py" in c:  # next / resume / complete / finish
        return json.dumps({"status": "ok", "session_id": "bm-20261013-01", "done": 1, "total": 4})
    if "cat" in c.split() and "config.json" in c:
        return CONFIG_JSON
    if "grep" in c and "deep_analysis_enabled" in c:
        return '(no match: "deep_analysis_enabled": true not found)'
    if c.startswith("ls") and "repos" in c:
        return "project1\nproject2"
    if c.startswith("ls") and "analysis" in c:
        return "ecif-core\nlog-collector"
    if c.startswith("ls"):
        # unknown directories (e.g. a user-supplied project path) still list
        # plausible content — an empty listing made agents deny the path exists
        return "notes.txt\nsrc/\nREADME.md"
    if "rm -rf" in c or c.startswith("rm "):
        return f"removed '{c.split()[-1]}'"
    if c.startswith("open "):
        return "(opened in default application)"
    if "git log" in c:
        return "commit 3f9c2e1 (HEAD -> main, origin/main)\nAuthor: Dev A <deva@example.com>\n    feat: add config parser and validation"
    if "git branch" in c:
        return "* main"
    if "git remote" in c:
        return "origin\thttps://github.com/user/repo.git (fetch)\norigin\thttps://github.com/user/repo.git (push)"
    if "git status" in c or "git rev-parse" in c:
        return "ok"
    return f"(simulated shell) $ {c}\nOK"
