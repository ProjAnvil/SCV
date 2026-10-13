#!/usr/bin/env python3
"""SCV Code Perception orchestrator.

Contract (spec 2026-10-13 rev2 §5.1): single-line JSON on stdout, exit 0 always.
Usage: perceive.py <repo_path> <analysis_dir> --source local|remote [--lang zh|en]
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
import time
from pathlib import Path

PERCEPTION_ROOT = Path(__file__).resolve().parent
DIST_ROOT = Path(os.environ.get("SCV_PERCEPTION_DIST", Path.home() / ".scv/perception/dist"))
TIER_TIMEOUT = 600  # seconds per tier

# (zh, en) — reason codes never contain tool names (spec §5.3)
FOOTNOTES = {
    "jdk-missing": (
        "已跳过深度结构分析：未检测到 JDK 17+（可选增强，安装后重新运行 /scv run 即可启用）",
        "Deep structural analysis skipped: JDK 17+ not found (optional; re-run /scv run after installing)",
    ),
    "adapter-build-failed": (
        "已跳过深度结构分析：分析组件构建失败（原因码记录于 perception.log，可重试）",
        "Deep structural analysis skipped: analyzer build failed (reason logged; retryable)",
    ),
    "adapter-failed": (
        "已跳过深度结构分析：分析未完成（已尝试降级，详情见 perception.log）",
        "Deep structural analysis skipped: analysis incomplete after fallback (see perception.log)",
    ),
    "degraded-noclasspath": (
        "深度结构分析以降级模式完成，结果可能包含更多不确定标注",
        "Deep structural analysis completed in degraded mode; results may carry extra uncertainty flags",
    ),
    "go-missing": (
        "结构图查询文件未生成（可选增强，需要 Go 1.27+ 运行环境）",
        "Structure-graph query file not generated (optional; requires Go 1.27+)",
    ),
    "mcp-missing": (
        "结构图查询服务未检测到（可选增强，安装说明见 ~/.scv/perception/README.md）",
        "Graph query service not found (optional; see ~/.scv/perception/README.md)",
    ),
}


def log_line(analysis_dir: Path, message: str) -> None:
    stamp = time.strftime("%Y-%m-%dT%H:%M:%S")
    with open(analysis_dir / "perception.log", "a", encoding="utf-8") as fh:
        fh.write(f"{stamp} {message}\n")


def load_manifest(adapter_id: str) -> dict:
    return json.loads((PERCEPTION_ROOT / "adapters" / adapter_id / "MANIFEST.json").read_text(encoding="utf-8"))


def _iter_marker_files(repo: Path, names: list[str], max_depth: int = 3):
    for dirpath, dirnames, filenames in os.walk(repo):
        depth = len(Path(dirpath).relative_to(repo).parts)
        if depth >= max_depth:
            dirnames[:] = []
        for f in filenames:
            if f in names:
                yield Path(dirpath) / f


def detect_framework(repo: Path, manifest: dict) -> bool:
    det = manifest["detect"]
    candidate = False
    for marker_file in _iter_marker_files(repo, det["marker_files"]):
        try:
            content = marker_file.read_text(encoding="utf-8", errors="ignore")
        except OSError:
            continue
        if any(m in content for m in det["markers"]):
            candidate = True
            break
    if not candidate:
        return False
    # second stage: confirm framework ownership in sources
    for src_root in ("src/main/java", "src/main/kotlin"):
        root = repo / src_root
        if not root.is_dir():
            continue
        for dirpath, _dirnames, filenames in os.walk(root):
            for f in filenames:
                if not f.endswith((".java", ".kt")):
                    continue
                try:
                    content = (Path(dirpath) / f).read_text(encoding="utf-8", errors="ignore")
                except OSError:
                    continue
                if any(c in content for c in det["confirm"]):
                    return True
    return False


def resolve_mode(repo: Path, source: str, policy: str) -> str:
    if policy == "never":
        return "noclasspath"
    if source == "remote" and policy != "always":
        return "noclasspath"  # security: never execute builds inside untrusted repos
    if (repo / "pom.xml").is_file():
        return "maven"
    # system gradle only counts when the repo itself is a Gradle project;
    # otherwise host tooling would leak into detection (test_no_build_tool)
    has_gradle_build = (repo / "build.gradle").is_file() or (repo / "build.gradle.kts").is_file()
    if (repo / "gradlew").is_file() or (has_gradle_build and shutil.which("gradle")):
        return "gradle"
    return "noclasspath"


def parse_java_version(out: str):
    m = re.search(r'version "(\d+)(?:\.(\d+))?', out)
    if not m:
        return None
    major = int(m.group(1))
    minor = int(m.group(2) or 0)
    return 8 if major == 1 and minor else major


def probe_java():
    for cmd in (["java", "-version"], [str(Path(os.environ.get("JAVA_HOME", "")) / "bin" / "java"), "-version"] if os.environ.get("JAVA_HOME") else None):
        if not cmd:
            continue
        try:
            res = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
        except (OSError, subprocess.TimeoutExpired):
            continue
        version = parse_java_version(res.stderr or res.stdout)
        if version:
            return version
    return None


def probe_go():
    try:
        res = subprocess.run(["go", "version"], capture_output=True, text=True, timeout=30)
    except (OSError, subprocess.TimeoutExpired):
        return None
    m = re.search(r"go version go(\d+)\.(\d+)", res.stdout)
    if not m:
        return None
    major, minor = int(m.group(1)), int(m.group(2))
    return (major, minor) if (major, minor) >= (1, 27) else None


def render_footnote(reasons: list[str], lang: str = "zh") -> str:
    idx = 0 if lang.startswith("zh") else 1
    parts = []
    for r in reasons:
        if r in FOOTNOTES:
            parts.append(FOOTNOTES[r][idx])
    if not parts:
        return ""
    return "；".join(dict.fromkeys(parts)) if lang.startswith("zh") else "; ".join(dict.fromkeys(parts))


def cleanup_stale(analysis_dir: Path) -> None:
    for name in ("structure.json", "structure.vg", "perception.log"):
        p = analysis_dir / name
        if p.exists():
            p.unlink()


def adapter_source_hash(adapter_dir: Path) -> str:
    h = hashlib.sha256()
    for f in sorted(adapter_dir.rglob("*")):
        if not f.is_file():
            continue
        s = str(f.relative_to(adapter_dir))
        if s.startswith(("build/", ".gradle/", "src/test/")):
            continue
        h.update(s.encode())
        h.update(f.read_bytes())
    return h.hexdigest()


def main() -> int:
    raise NotImplementedError("orchestration added in Task 5")


if __name__ == "__main__":
    sys.exit(main())
