#!/usr/bin/env python3
"""
SCV Utility - CLI tool for metadata management and commit inspection.

Used by:
  - run.md (scv run): directly via bash after analysis completes
  - batch_manager.py (scv batchRun): imported as a Python module

Commands:
  get-commit-info  --repo <path>
  check-skip       --repo <path> --output-dir <path>
  write-metadata   --repo <path> --commit <hash> --output-dir <path>
  update-index     [--analysis-dir <path>] [--project-dir <path>]

Exit codes for check-skip:
  0  analysis needed
  2  commit unchanged → skip
  1  error
"""

import argparse
import json
import re
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional


def _now() -> str:
    return datetime.now(timezone.utc).isoformat()


def _ok(data: dict) -> None:
    print(json.dumps(data, ensure_ascii=False, indent=2))


def _die(msg: str, code: int = 1) -> None:
    print(json.dumps({"error": msg}))
    sys.exit(code)


def get_metadata_path(output_dir: Path) -> Path:
    return output_dir / ".scv_metadata.json"


def read_metadata(output_dir: Path) -> Optional[dict]:
    path = get_metadata_path(output_dir)
    if not path.exists():
        return None
    try:
        with open(path, "r", encoding="utf-8") as f:
            return json.load(f)
    except (json.JSONDecodeError, OSError):
        return None


def write_metadata(
    output_dir: Path,
    commit_hash: str,
    repo_path: str,
    additional_fields: Optional[dict] = None,
) -> None:
    output_dir.mkdir(parents=True, exist_ok=True)
    existing = read_metadata(output_dir)
    created_at = existing.get("created_at") if existing else _now()

    metadata = {
        "last_analyzed_commit": commit_hash,
        "last_analyzed_at": _now(),
        "created_at": created_at,
        "repo_path": str(repo_path),
        "scv_version": "2.0",
    }
    if additional_fields:
        metadata.update(additional_fields)

    with open(get_metadata_path(output_dir), "w", encoding="utf-8") as f:
        json.dump(metadata, f, indent=2, ensure_ascii=False)


def should_skip_analysis(
    output_dir: Path,
    current_commit: str,
) -> tuple[bool, Optional[str]]:
    metadata = read_metadata(output_dir)
    if metadata is None:
        return False, None

    last_commit = metadata.get("last_analyzed_commit")
    if not last_commit:
        return False, None

    if last_commit == current_commit:
        last_time = metadata.get("last_analyzed_at", "unknown time")
        return True, f"Commit unchanged since {last_time}"

    return False, None


def _get_commit_info(repo_path: Path) -> Optional[dict]:
    try:
        from git_op import get_commit_info, is_git_repo

        if not is_git_repo(repo_path):
            return None
        return get_commit_info(repo_path)
    except ImportError:
        pass

    import subprocess

    try:
        result = subprocess.run(
            ["git", "log", "-1", "--format=%H%n%h%n%s%n%an%n%ai"],
            cwd=repo_path,
            capture_output=True,
            text=True,
            timeout=10,
        )
        if result.returncode != 0:
            return None
        lines = result.stdout.strip().splitlines()
        if len(lines) < 5:
            return None
        return {
            "hash": lines[0],
            "short_hash": lines[1],
            "message": lines[2],
            "author": lines[3],
            "date": lines[4],
        }
    except Exception:
        return None


def cmd_get_commit_info(args):
    repo_path = Path(args.repo).expanduser().resolve()
    if not repo_path.exists():
        _die(f"Path not found: {repo_path}")

    info = _get_commit_info(repo_path)
    if info is None:
        _die(f"Not a git repository or no commits: {repo_path}")
        return

    _ok(info)


def cmd_check_skip(args):
    repo_path = Path(args.repo).expanduser().resolve()
    output_dir = Path(args.output_dir).expanduser()

    if not repo_path.exists():
        _die(f"Repo path not found: {repo_path}")

    info = _get_commit_info(repo_path)
    if info is None:
        _ok({"skip": False, "reason": "Not a git repository"})
        sys.exit(0)

    should_skip, reason = should_skip_analysis(output_dir, info["hash"])

    if should_skip:
        existing = read_metadata(output_dir)
        _ok(
            {
                "skip": True,
                "reason": reason,
                "commit": info["short_hash"],
                "last_analyzed_at": (
                    existing.get("last_analyzed_at") if existing else None
                ),
            }
        )
        sys.exit(2)

    _ok(
        {
            "skip": False,
            "current_commit": info["hash"],
            "short_hash": info["short_hash"],
        }
    )


def cmd_write_metadata(args):
    output_dir = Path(args.output_dir).expanduser()
    repo_path = Path(args.repo).expanduser().resolve()

    write_metadata(
        output_dir=output_dir,
        commit_hash=args.commit,
        repo_path=str(repo_path),
    )

    _ok(
        {
            "status": "metadata_written",
            "output_dir": str(output_dir),
            "commit": args.commit,
            "metadata_file": str(get_metadata_path(output_dir)),
        }
    )


# ---------------------------------------------------------------------------
# Index management
# ---------------------------------------------------------------------------


def cmd_update_index(args):
    """Write ~/.scv/index.md from AI-provided entries JSON.

    Accepts --entries-json as a JSON array of objects with keys:
      name     — project directory name
      summary  — one-sentence description
      keywords — comma/、-separated functional keywords (or "-")

    Language is read from ~/.scv/config.json ("lang": "zh-cn" | "en").
    Falls back to "en" if not set.
    """
    analysis_dir = Path(args.analysis_dir or "~/.scv/analysis").expanduser()

    # Read language from config
    config_path = analysis_dir.parent / "config.json"
    lang = "en"
    if config_path.exists():
        try:
            cfg = json.loads(config_path.read_text(encoding="utf-8"))
            lang = cfg.get("lang", "en")
        except Exception:
            pass

    try:
        entries = json.loads(args.entries_json)
    except (json.JSONDecodeError, TypeError) as e:
        _die(f"--entries-json is not valid JSON: {e}")
        return

    if not isinstance(entries, list):
        _die("--entries-json must be a JSON array")
        return

    index_path = analysis_dir.parent / "index.md"
    repos_dir = analysis_dir.parent / "repos"
    timestamp = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M UTC")

    if lang == "zh-cn":
        lines = [
            "# SCV 分析索引",
            "",
            "## 目录结构",
            "",
            "| 目录 | 内容 |",
            "|------|------|",
            f"| `{repos_dir}/` | 源代码仓库 |",
            f"| `{analysis_dir}/` | 代码分析文档 |",
            "",
            "---",
            "",
            "## 项目索引",
            "",
            f"> 自动生成 · {timestamp}",
            "",
            "| # | 项目 | 简介 | 功能职责 |",
            "|---|------|------|--------|",
        ]
    else:
        lines = [
            "# SCV Analysis Index",
            "",
            "## Directory Structure",
            "",
            "| Directory | Contents |",
            "|-----------|----------|",
            f"| `{repos_dir}/` | Source code repositories |",
            f"| `{analysis_dir}/` | Code analysis documents |",
            "",
            "---",
            "",
            "## Project Index",
            "",
            f"> Auto-generated · {timestamp}",
            "",
            "| # | Project | Summary | Responsibilities |",
            "|---|---------|---------|-----------------|",
        ]

    for i, entry in enumerate(entries, 1):
        name = str(entry.get("name", "")).replace("|", "\\|")
        summary = str(entry.get("summary", "")).replace("|", "\\|")
        keywords = str(entry.get("keywords", "-")).replace("|", "\\|")
        lines.append(f"| {i} | {name} | {summary} | {keywords} |")

    lines.append("")
    index_path.parent.mkdir(parents=True, exist_ok=True)
    index_path.write_text("\n".join(lines), encoding="utf-8")

    _ok(
        {
            "status": "index_updated",
            "index_file": str(index_path),
            "total_projects": len(entries),
            "lang": lang,
        }
    )


def main():
    parser = argparse.ArgumentParser(
        description="SCV Utility — metadata management and commit inspection"
    )
    sub = parser.add_subparsers(dest="command", required=True)

    p_ci = sub.add_parser("get-commit-info", help="Print HEAD commit info as JSON")
    p_ci.add_argument("--repo", required=True)

    p_cs = sub.add_parser("check-skip", help="Exit 2=skip, 0=analyze")
    p_cs.add_argument("--repo", required=True)
    p_cs.add_argument("--output-dir", required=True)

    p_wm = sub.add_parser("write-metadata", help="Write .scv_metadata.json")
    p_wm.add_argument("--repo", required=True)
    p_wm.add_argument("--commit", required=True)
    p_wm.add_argument("--output-dir", required=True)

    p_ui = sub.add_parser(
        "update-index", help="Write ~/.scv/index.md from AI-provided entries"
    )
    p_ui.add_argument(
        "--analysis-dir",
        default=None,
        help="Analysis root dir (default: ~/.scv/analysis)",
    )
    p_ui.add_argument(
        "--entries-json",
        required=True,
        help='JSON array of {"name","summary","keywords"} objects',
    )

    args = parser.parse_args()
    {
        "get-commit-info": cmd_get_commit_info,
        "check-skip": cmd_check_skip,
        "write-metadata": cmd_write_metadata,
        "update-index": cmd_update_index,
    }[args.command](args)


if __name__ == "__main__":
    main()
