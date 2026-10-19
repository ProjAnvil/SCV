#!/usr/bin/env python3
"""Run perceive.py over every fixture repository and tabulate the outcome.

This is the deterministic half of an `scv run`: Step 5.5 of
skills/*/references/run.md invokes

    python3 perceive.py <repo> <analysis_dir> --source local --lang en

and this harness does exactly that, once per fixture. The LLM-driven document
tiers (README/SUMMARY/ARCHITECTURE/FILE_INDEX) are out of scope here — they need
an agent, and what we want to test is whether perception parses the tree.

The run reads its config through SCV_PERCEPTION_CONFIG instead of
~/.scv/config.json, so switching between the component tier and structure mode
never touches the user's real configuration.
"""
from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
from pathlib import Path

PERCEPTION_ROOT = Path(__file__).resolve().parent.parent
PERCEIVE = PERCEPTION_ROOT / "perceive.py"


def write_config(fixture_dir: Path, extensions: list[str]) -> Path:
    """Materialise a config file for this run and return its path."""
    name = "config.structure.json" if not extensions else "config.springboot.json"
    path = fixture_dir / name
    path.write_text(json.dumps({"structure_extensions": extensions}), encoding="utf-8")
    return path.resolve()


def run_one(repo: Path, out_dir: Path, env: dict) -> tuple[dict | None, str]:
    """Invoke perceive.py once; return (parsed stdout, stderr)."""
    res = subprocess.run(
        [sys.executable, str(PERCEIVE), str(repo), str(out_dir),
         "--source", "local", "--lang", "en"],
        capture_output=True, text=True, env=env, cwd=PERCEPTION_ROOT,
    )
    if res.returncode != 0:
        print(f"FAIL {repo.name}: perceive.py exited {res.returncode}", file=sys.stderr)
        print(res.stderr[-800:], file=sys.stderr)
        return None, res.stderr
    try:
        return json.loads(res.stdout), res.stderr
    except json.JSONDecodeError as exc:
        print(f"FAIL {repo.name}: stdout is not JSON ({exc})", file=sys.stderr)
        print(res.stdout[-400:], file=sys.stderr)
        return None, ""


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--repos", default="fixture/repos")
    ap.add_argument("--analysis", default="fixture/analysis")
    ap.add_argument("--extensions", default="springboot",
                    help="comma-separated; empty string runs structure mode")
    ap.add_argument("--repo", default="", help="analyse a single fixture")
    args = ap.parse_args()

    repos_home = Path(args.repos)
    analysis_home = Path(args.analysis)
    if not repos_home.is_absolute():
        repos_home = PERCEPTION_ROOT / repos_home
    if not analysis_home.is_absolute():
        analysis_home = PERCEPTION_ROOT / analysis_home

    extensions = [e.strip() for e in args.extensions.split(",") if e.strip()]
    fixture_dir = Path(__file__).resolve().parent
    cfg = write_config(fixture_dir, extensions)
    env = dict(os.environ, SCV_PERCEPTION_CONFIG=str(cfg))
    analysis_home.mkdir(parents=True, exist_ok=True)

    targets = sorted(p for p in repos_home.iterdir() if p.is_dir())
    if args.repo:
        targets = [p for p in targets if p.name == args.repo]
        if not targets:
            sys.exit(f"no fixture repo named {args.repo!r} under {repos_home}")
    if not targets:
        sys.exit(f"no fixture repos under {repos_home}")

    rows = []
    for repo in targets:
        emit, _ = run_one(repo, analysis_home / repo.name, env)
        if emit is None:
            rows.append((repo.name, "CRASH", "-", "-", "-"))
            continue
        counts = emit.get("counts") or {}
        fatal, tolerated = counts.get("cycles_fatal"), counts.get("cycles_tolerated")
        cycles = ("cycles=n/a" if fatal is None
                  else f"fatal={fatal} tolerated={tolerated}")
        rows.append((repo.name, emit.get("status") or "?", emit.get("reason") or "-",
                     f"{counts.get('components', '-')}/{counts.get('edges', '-')}",
                     cycles))

    width = max(len(r[0]) for r in rows)
    print(f"{'repo'.ljust(width)}  {'status':<10} {'reason':<16} {'beans/edges':<13} cycles")
    print("-" * (width + 58))
    for name, status, reason, be, cyc in rows:
        print(f"{name.ljust(width)}  {status:<10} {reason:<16} {be:<13} {cyc}")
    print(f"\nextensions={','.join(extensions) or '(structure mode)'}"
          f"   repos={repos_home}\nanalysis={analysis_home}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
