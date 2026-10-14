"""Sandboxed tool implementations for the project-analyzer agent eval.

The tools mirror the agent's frontmatter allowlist (Read/Write/Glob/Grep/Bash/LSP)
and operate on the real filesystem under the smelt sandbox cwd, where
``context(files=...)`` fixtures are materialized. Writes are recorded in
``WRITES`` so tests can inspect generated documents.
"""

from __future__ import annotations

import fnmatch
import os
import re
import subprocess
from pathlib import Path

from smelt import tool

WRITES: dict[str, str] = {}


def _root() -> Path:
    return Path.cwd()


@tool
def Glob(pattern: str) -> str:
    """Find files by glob pattern, relative paths, one per line"""
    root = _root()
    try:
        hits = [str(p.relative_to(root)) for p in root.glob(pattern) if p.is_file()]
    except (ValueError, NotImplementedError):
        hits = []
    return "\n".join(sorted(hits)[:200]) or "(no matches)"


@tool
def Grep(pattern: str) -> str:
    """Search file contents for a regex pattern; returns path:line:match"""
    root = _root()
    out = []
    rx = re.compile(pattern)
    for p in sorted(root.rglob("*")):
        if not p.is_file() or len(out) > 60:
            break
        try:
            for i, line in enumerate(p.read_text(errors="ignore").splitlines(), 1):
                if rx.search(line):
                    out.append(f"{p.relative_to(root)}:{i}:{line.strip()[:120]}")
        except OSError:
            continue
    return "\n".join(out[:60]) or "(no matches)"


@tool
def Read(file_path: str) -> str:
    """Read a file's contents"""
    p = _root() / file_path
    if not p.is_file():
        return f"error: file not found: {file_path}"
    return p.read_text(errors="ignore")


@tool
def Write(file_path: str, content: str) -> str:
    """Write content to a file (creates parent directories)"""
    p = _root() / file_path
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(content)
    WRITES[file_path] = content
    return f"wrote {len(content)} chars to {file_path}"


_CODEBONES_SAMPLE = '''symbol OrderService (services/order.py)
  deps: UserService, InventoryService
  methods:
    create_order(customer, items) -> validates via UserService.check_customer,
      reserves stock via InventoryService.reserve_stock
'''


@tool
def Bash(command: str) -> str:
    """Run a shell command (codebones CLI available for deep analysis)"""
    if command.strip().startswith("codebones get"):
        return _CODEBONES_SAMPLE
    if command.strip().startswith("codebones search"):
        return "OrderService services/order.py\nUserService services/user.py"
    if command.strip().startswith("codebones"):
        return "codebones: unknown subcommand"
    return f"command not available in sandbox: {command}"


@tool
def LSP(operation: str, symbol: str = "") -> str:
    """Code intelligence (definitions/references)"""
    return "LSP is not available in this environment"


def scv_agent_tools():
    """The tool set handed to evaluate_agent (frontmatter filters as needed)."""
    return [Glob, Grep, Read, Write, Bash, LSP]
