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


def _os_bin(dist_dir: Path, name: str) -> Path:
    sub = "bin/" + (name + ".bat" if os.name == "nt" else name)
    return dist_dir / sub


def _record_build_error(e) -> None:
    sys.stderr.write(f"adapter build error: {e}\n")


def _usable_dist(dist_dir: Path, adapter_id: str) -> Path | None:
    """A dist counts as usable when its launcher binary is present."""
    return dist_dir if _os_bin(dist_dir, adapter_id + "-probe").exists() else None


def ensure_dist(adapter: dict) -> Path | None:
    adapter_dir = PERCEPTION_ROOT / "adapters" / adapter["id"]
    dist_dir = DIST_ROOT / adapter["id"]
    hash_file = dist_dir / ".source-hash"
    current = adapter_source_hash(adapter_dir)
    if dist_dir.exists():
        bin_path = _os_bin(dist_dir, adapter["id"] + "-probe")
        if (bin_path.exists() and hash_file.exists()
                and hash_file.read_text().strip() == current):
            return dist_dir
    try:
        subprocess.run(adapter["build"], shell=True, cwd=adapter_dir,
                       capture_output=True, text=True, timeout=TIER_TIMEOUT, check=True)
    except (subprocess.SubprocessError, OSError) as e:
        # Ruling (pre-flight scan): a failed rebuild must not discard a
        # previously built, still-usable dist — reuse it and log; only give
        # up when no usable dist exists.
        _record_build_error(e)
        reused = _usable_dist(dist_dir, adapter["id"]) if dist_dir.exists() else None
        if reused:
            sys.stderr.write(f"adapter build error: reusing previous dist at {dist_dir}\n")
            return dist_dir
        return None
    install_dir = adapter_dir / "build" / "install" / (adapter["id"] + "-probe")
    if not _os_bin(install_dir, adapter["id"] + "-probe").exists():
        _record_build_error(RuntimeError(f"installDist produced no launcher in {install_dir}"))
        reused = _usable_dist(dist_dir, adapter["id"]) if dist_dir.exists() else None
        return reused
    # installDist writes under the adapter's build/ tree; stage the launcher
    # tree into DIST_ROOT so the dist is self-contained and hash-keyed.
    if dist_dir.exists():
        shutil.rmtree(dist_dir)
    shutil.copytree(install_dir, dist_dir, dirs_exist_ok=True)
    hash_file.write_text(current)
    return dist_dir


def run_adapter(adapter: dict, dist_dir: Path, mode: str, repo: Path, out_json: Path) -> bool:
    bin_path = _os_bin(dist_dir, adapter["id"] + "-probe")
    cmd = [str(bin_path), mode, str(repo), str(out_json)]
    try:
        res = subprocess.run(cmd, capture_output=True, text=True, timeout=TIER_TIMEOUT)
    except (subprocess.SubprocessError, OSError):
        return False
    return res.returncode == 0 and out_json.exists()


def run_loader(loader_dir: Path, json_path: Path, vg_path: Path):
    binary = DIST_ROOT / "loader" / "perception-loader"
    try:
        if not binary.exists():
            binary.parent.mkdir(parents=True, exist_ok=True)
            subprocess.run(["go", "build", "-o", str(binary), "."], cwd=loader_dir,
                           capture_output=True, text=True, timeout=TIER_TIMEOUT, check=True)
        res = subprocess.run([str(binary), str(json_path), str(vg_path)],
                             capture_output=True, text=True, timeout=TIER_TIMEOUT)
        if res.returncode != 0:
            return None
        return json.loads(res.stdout)
    except (subprocess.SubprocessError, OSError, json.JSONDecodeError):
        return None


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("repo_path")
    ap.add_argument("analysis_dir")
    ap.add_argument("--source", choices=["local", "remote"], default="local")
    ap.add_argument("--lang", default="zh")
    args = ap.parse_args()

    repo, analysis_dir = Path(args.repo_path).resolve(), Path(args.analysis_dir).resolve()
    analysis_dir.mkdir(parents=True, exist_ok=True)
    emit = {"status": "skipped", "reason": None,
            "tiers": {"json": False, "vg": False, "mcp_hint": False},
            "counts": None, "artifacts": {"structure_json": None, "structure_vg": None},
            "mcp_add": None, "footnote": ""}

    cfg_path = Path.home() / ".scv/config.json"
    try:
        cfg = json.loads(cfg_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        cfg = {}
    if not cfg.get("structure_analysis_enabled", True):
        emit["status"], emit["reason"] = "skipped", "disabled"
        log_line(analysis_dir, "tier0 disabled by config")
        print(json.dumps(emit, ensure_ascii=False))
        return 0
    policy = cfg.get("structure_build_execution", "local-only")

    adapter = load_manifest("springboot")
    if not detect_framework(repo, adapter):
        emit["status"], emit["reason"] = "not-framework", None
        log_line(analysis_dir, "tier1 detect: not-framework")
        print(json.dumps(emit, ensure_ascii=False))
        return 0

    cleanup_stale(analysis_dir)
    # Resolve and log the mode BEFORE any adapter execution (ruling: the tier1
    # detect line must carry mode= so remote/noclasspath runs are auditable).
    mode = resolve_mode(repo, args.source, policy)
    log_line(analysis_dir, f"tier1 detect: springboot, source={args.source}, "
                           f"policy={policy}, mode={mode}")

    reasons: list[str] = []
    java = probe_java()
    if not java or java < int(adapter["requires"]["jdk"]):
        reasons.append("jdk-missing")
        log_line(analysis_dir, f"tier3 jdk probe: {java}")
        emit["reason"] = "jdk-missing"
        emit["footnote"] = render_footnote(reasons, args.lang)
        print(json.dumps(emit, ensure_ascii=False))
        return 0

    dist_dir = ensure_dist(adapter)
    if dist_dir is None:
        emit["reason"] = "adapter-build-failed"
        emit["footnote"] = render_footnote(["adapter-build-failed"], args.lang)
        log_line(analysis_dir, "tier4 dist build failed")
        print(json.dumps(emit, ensure_ascii=False))
        return 0

    structure_json = analysis_dir / "structure.json"
    ok = run_adapter(adapter, dist_dir, mode, repo, structure_json)
    degraded = False
    if not ok and mode != "noclasspath":
        log_line(analysis_dir, f"tier5 mode {mode} failed; falling back to noclasspath")
        ok = run_adapter(adapter, dist_dir, "noclasspath", repo, structure_json)
        degraded = ok
    if not ok:
        emit["reason"] = "adapter-failed"
        emit["footnote"] = render_footnote(["adapter-failed"], args.lang)
        log_line(analysis_dir, "tier5 adapter failed (incl. noclasspath fallback)")
        print(json.dumps(emit, ensure_ascii=False))
        return 0

    emit["status"], emit["tiers"]["json"] = "ok", True
    emit["artifacts"]["structure_json"] = str(structure_json)
    graph = json.loads(structure_json.read_text(encoding="utf-8"))
    counts = {"components": len(graph["nodes"]), "edges": len(graph["edges"]),
              "cycles_fatal": 0, "cycles_tolerated": 0}
    if degraded:
        reasons.append("degraded-noclasspath")

    vg_path = analysis_dir / "structure.vg"
    loader_info = None
    if probe_go():
        loader_info = run_loader(PERCEPTION_ROOT / "loader", structure_json, vg_path)
    else:
        reasons.append("go-missing")
    if loader_info:
        emit["tiers"]["vg"] = True
        emit["artifacts"]["structure_vg"] = str(vg_path)
        for c in loader_info.get("cycles", []):
            if c.get("grade") == "fatal":
                counts["cycles_fatal"] += 1
            else:
                counts["cycles_tolerated"] += 1
    emit["counts"] = counts

    if shutil.which("vectra-mcp"):
        emit["tiers"]["mcp_hint"] = True
        ts = time.strftime("%Y-%m-%d %H:%M")
        emit["mcp_add"] = (f"claude mcp add scv-structure -- vectra-mcp -snapshot {vg_path} "
                           f"# generated {ts}")
    else:
        reasons.append("mcp-missing")

    emit["reason"] = (reasons[0] if len(reasons) == 1 else reasons) if reasons else None
    emit["footnote"] = render_footnote(reasons, args.lang)
    log_line(analysis_dir, f"tiers done: {emit['tiers']} reasons={reasons}")
    print(json.dumps(emit, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
