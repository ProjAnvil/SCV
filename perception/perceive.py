#!/usr/bin/env python3
"""SCV Code Perception orchestrator (v3 — uv + Python + tree-sitter engine).

Contract (spec 2026-10-13 rev2 §5.1): single-line JSON on stdout, exit 0 always.
Usage: perceive.py <repo_path> <analysis_dir> --source local|remote [--lang zh|en]

v2 (spec 2026-10-14): the JDK/Gradle dist tiers are gone — the analyzer runs
via `uv run --project perception/analyzer`; language extensions are gated by
the `structure_extensions` config key (default ["springboot"]); no build ever
executes inside the analyzed repo (local and remote are analyzed identically,
`--source` is still recorded for audit).

v3 (spec 2026-10-20): the structure tier is multi-language — the detect gate
walks the repo for the extensions of every language enabled via the
`structure_languages` config key (default: all six shipped) and the tier runs
when ANY enabled language has >= 1 source file; `not-framework` survives only
as the "no enabled language present" outcome. The analyzer is fed
`--languages` from the config ∩ detected set; the springboot extension keeps
its own Spring-marker detection on top of java, orthogonal to
`structure_languages`.

v4 (spec 2026-11-13 §12): `--version` prints the one-line scv_version
contract and exits before any tier work; the Go tiers (loader / graphmcp)
gate on a prebuilt dist binary OR a live Go toolchain (a fresh prebuilt is
run as-is, no build); every tier entry logs a `tierN start` line; dist
`go build`s are serialized by dist/.build.lock (spec §10).
"""
from __future__ import annotations

import argparse
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
# dist build lock (spec §10): O_EXCL lockfile name + the age at which a
# lock counts as a crashed holder's leftover and is taken over.
BUILD_LOCK_NAME = ".build.lock"
BUILD_LOCK_STALE_SECONDS = 600  # 10 minutes

# (zh, en) — reason codes never contain tool names (spec §5.3); the missing
# runtime footnote names the runtime, not the tool (spec 2026-10-14 §1).
FOOTNOTES = {
    "uv-missing": (
        "已跳过深度结构分析：未检测到 Python 3.11+ 运行环境（可选增强，安装后重新运行 /scv run 即可启用）",
        "Deep structural analysis skipped: Python 3.11+ runtime not found (optional; re-run /scv run after installing)",
    ),
    "analyzer-failed": (
        "已跳过深度结构分析：分析未完成（详情见 perception.log）",
        "Deep structural analysis skipped: analysis incomplete (see perception.log)",
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

# Structure-mode summary wording (v3 spec §7, multi-language aggregate) —
# surfaced as the `structure_summary` stdout field so report rendering needs
# no logic. The language clause is appended from analyzer meta.languages
# (counts passthrough); a v2-shaped analyzer graph without that field renders
# the aggregate only (backwards compat).
STRUCTURE_SUMMARY = (
    "{files} 个文件、{types} 个类型、{methods} 个方法、{call_edges} 条调用边",
    "{files} files, {types} types, {methods} methods, {call_edges} call edges",
)

# Language extensions known to the shipped analyzer (spec §7: config key
# `structure_extensions`, default ["springboot"]; unknown values warn + drop).
KNOWN_EXTENSIONS = ("springboot",)

# v3 spec §4 — language table mirror. The analyzer's structure/langs.py is the
# canonical registry; perceive.py keeps its own walk code (v2 precedent), so
# the suffix + exclusion tables are duplicated here by contract.
LANGUAGE_SUFFIXES = {
    "java": (".java",),
    "python": (".py",),
    "javascript": (".js", ".jsx", ".mjs", ".cjs"),
    "typescript": (".ts", ".tsx", ".mts", ".cts"),
    "go": (".go",),
    "rust": (".rs",),
}
KNOWN_LANGUAGES = tuple(sorted(LANGUAGE_SUFFIXES))

# Shared walk exclusions (spec §4): build outputs, caches, vendored trees,
# dot-directories — mirrors langs.py.
_EXCLUDED_DIRS = {
    "node_modules", "__pycache__", "venv", ".venv", "env",
    "dist", "build", ".gradle", "target", "vendor", "testdata", "out",
}
# Test trees skipped for the non-java walks (spec §4); go keeps test dirs and
# *_test.go files, java keeps its own src/test handling inside source-root
# discovery — neither opts in here.
_TEST_DIRS = {"test", "tests"}
_SKIP_TEST_DIRS = {"python", "javascript", "typescript", "rust"}


def log_line(analysis_dir: Path, message: str) -> None:
    stamp = time.strftime("%Y-%m-%dT%H:%M:%S")
    with open(analysis_dir / "perception.log", "a", encoding="utf-8") as fh:
        fh.write(f"{stamp} {message}\n")


def scv_version() -> str:
    """First released version heading in CHANGELOG.md (spec §12) — the
    `## [Unreleased]` head never counts. "unknown" when the file is absent
    or malformed (installed layouts): --version must honor its contract
    regardless."""
    try:
        text = (PERCEPTION_ROOT.parent / "CHANGELOG.md").read_text(encoding="utf-8")
    except OSError:
        return "unknown"
    m = re.search(r"^## \[(v\d+\.\d+\.\d+)\]", text, re.MULTILINE)
    return m.group(1) if m else "unknown"


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


_SOURCE_ROOT_SUFFIXES = (("src", "main", "java"), ("src", "main", "kotlin"))


def _iter_source_roots(repo: Path, max_depth: int = 3):
    """Yield every src/main/{java,kotlin} dir across the repo (root + submodules).

    The depth bound mirrors the marker scan: a module counts when its directory
    sits within max_depth of the repo root (marker files are found the same
    way); its source root is that dir plus src/main/{java,kotlin}, so the walk
    prunes at max_depth + 3. Contents are NOT walked here — callers scan each
    root separately, so descent stops once a root is yielded.
    """
    for dirpath, dirnames, _filenames in os.walk(repo):
        parts = Path(dirpath).relative_to(repo).parts
        if len(parts) >= max_depth + 3:
            dirnames[:] = []
        if parts[-3:] in _SOURCE_ROOT_SUFFIXES:
            root = Path(dirpath)
            dirnames[:] = []
            yield root


def _has_java_source(repo: Path) -> bool:
    for root in _iter_source_roots(repo):
        for _dirpath, _dirnames, filenames in os.walk(root):
            if any(f.endswith(".java") for f in filenames):
                return True
    return False


def _detect_by_suffix(repo: Path, names: list[str]) -> list[str]:
    """Walk the repo once; return every requested language with >= 1 source
    file. Exclusions mirror the analyzer's langs.py table (spec §4):
    _EXCLUDED_DIRS + dot-dirs always; test/tests subtrees skipped only for
    the languages that opted in (python/js/ts/rust — go and java keep them).
    """
    pending = set(names)
    found: set[str] = set()
    for dirpath, dirnames, filenames in os.walk(repo):
        dirnames[:] = [d for d in dirnames
                       if d not in _EXCLUDED_DIRS and not d.startswith(".")]
        parts = Path(dirpath).relative_to(repo).parts
        test_tree = any(p in _TEST_DIRS for p in parts)
        for fn in filenames:
            for name in pending:
                if name in _SKIP_TEST_DIRS and test_tree:
                    continue
                if fn.endswith(LANGUAGE_SUFFIXES[name]):
                    found.add(name)
        pending -= found
        if not pending:
            break
    return sorted(found)


def detect_languages(repo: Path, enabled: list[str]) -> list[str]:
    """v3 detect gate (spec 2026-10-20 §7): the structure tier runs when ANY
    enabled language has >= 1 source file. Java keeps its v2 source-root walk
    (spec §4 java row); every other language is found by suffix walk."""
    found: list[str] = []
    if "java" in enabled and _has_java_source(repo):
        found.append("java")
    found.extend(_detect_by_suffix(repo, [n for n in enabled if n != "java"]))
    return sorted(found)


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
    # second stage: confirm framework ownership in sources — the root module
    # AND submodule source roots (multi-module layouts keep the boot class in
    # a submodule); roots dedupe by directory.
    scanned: set[Path] = set()
    source_roots: list[Path] = [repo / s for s in ("src/main/java", "src/main/kotlin")]
    source_roots.extend(_iter_source_roots(repo))
    for root in source_roots:
        if not root.is_dir() or root in scanned:
            continue
        scanned.add(root)
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


def resolve_extensions(cfg: dict):
    """Validate `structure_extensions` (default ["springboot"]).

    Unknown values (and non-list shapes) never fail the run: they are dropped
    from the list passed to the analyzer and reported as warning lines (the
    caller flushes them into perception.log AFTER stale cleanup, which wipes
    the log — spec §7; the analyzer-side registry double-checks)."""
    raw = cfg.get("structure_extensions")
    if raw is None:
        return list(KNOWN_EXTENSIONS), []
    if not isinstance(raw, list):
        return [], [f"config structure_extensions not a list ({raw!r}) — ignored"]
    resolved: list[str] = []
    warnings: list[str] = []
    for name in raw:
        if name in KNOWN_EXTENSIONS and name not in resolved:
            resolved.append(name)
        elif name not in KNOWN_EXTENSIONS:
            warnings.append(f"config structure_extensions unknown value {name!r} — dropped")
    return resolved, warnings


def resolve_languages(cfg: dict):
    """Validate `structure_languages` (v3 spec §7: default = every shipped
    language).

    Unknown values warn + are ignored (structure_extensions precedent). An
    empty list — including a non-list shape or a list of only unknown values —
    turns the structure tier off entirely: same clean skip as
    structure_analysis_enabled=false, no footnote (the tier is off by config,
    nothing degraded)."""
    raw = cfg.get("structure_languages")
    if raw is None:
        return list(KNOWN_LANGUAGES), []
    if not isinstance(raw, list):
        return [], [f"config structure_languages not a list ({raw!r}) — ignored"]
    resolved: list[str] = []
    warnings: list[str] = []
    for name in raw:
        if name in LANGUAGE_SUFFIXES and name not in resolved:
            resolved.append(name)
        elif name not in LANGUAGE_SUFFIXES:
            warnings.append(f"config structure_languages unknown value {name!r} — ignored")
    return resolved, warnings


def probe_uv(analysis_dir: Path | None = None):
    """Return the uv executable when uv is on PATH, else None (uv-missing).

    A FAILING `uv run ... python -c pass` probe (environment provisioning
    trouble: network, lock, disk) is NOT uv-missing — that would misreport a
    present runtime as absent. The probe's stderr tail is logged to
    perception.log (when analysis_dir is given) and the uv path is returned
    anyway: run_analyzer then gets its own honest chance to succeed, or fail
    through the analyzer-failed path with its tail in the log."""
    uv = shutil.which("uv")
    if not uv:
        return None
    try:
        res = subprocess.run(
            [uv, "run", "--project", str(PERCEPTION_ROOT / "analyzer"), "python", "-c", "pass"],
            capture_output=True, text=True, timeout=TIER_TIMEOUT)
    except (OSError, subprocess.TimeoutExpired) as e:
        if analysis_dir is not None:
            log_line(analysis_dir, f"tier2 uv probe failed (environment provisioning): {_exc_tail(e)}")
        return uv
    if res.returncode != 0:
        if analysis_dir is not None:
            tail = _log_tail(" ".join(filter(None, (res.stderr, res.stdout))))
            log_line(analysis_dir, f"tier2 uv probe failed (environment provisioning): {tail}")
        return uv
    return uv


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


def prebuilt(path: Path) -> bool:
    """Tier gate (spec §12): a prebuilt dist binary satisfies the Go tiers
    without a toolchain. Presence is the contract — freshness stays the
    build step's call (_go_binary_is_stale): a fresh prebuilt runs as-is, a
    stale one is rebuilt when a toolchain exists (or its tier degrades
    gracefully without one)."""
    return path.exists()


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


def _log_tail(text: str, limit: int = 2000) -> str:
    """Collapse whitespace and keep the tail so one event stays one log line."""
    return " ".join((text or "").split())[-limit:]


def _exc_tail(e) -> str:
    detail = getattr(e, "stderr", None) or str(e)
    return _log_tail(detail)


def run_analyzer(repo: Path, out_json: Path, analysis_dir: Path, extensions: list[str],
                 languages: list[str] | None = None) -> bool:
    """Run the v3 analyzer (uv tier). Honors SCV_PERCEPTION_ANALYZER (a
    substitute analyzer executable invoked directly, bypassing uv — used by
    tests with fakes that accept the same CLI). A duration line plus any
    non-fatal stderr tail lands in perception.log on EVERY run. `languages`
    is the config ∩ detected set fed to --languages (v3 spec §7); empty/None
    omits the flag (the analyzer defaults to every shipped language)."""
    override = os.environ.get("SCV_PERCEPTION_ANALYZER")
    if override:
        cmd = [override, str(repo), str(out_json)]
    else:
        # the analyzer.py path must be absolute: `uv run --project` keeps the
        # caller's CWD, so a bare "analyzer.py" would not resolve.
        cmd = ["uv", "run", "--project", str(PERCEPTION_ROOT / "analyzer"),
               "python", str(PERCEPTION_ROOT / "analyzer" / "analyzer.py"),
               str(repo), str(out_json)]
    if languages:
        cmd += ["--languages", *languages]
    if extensions:
        cmd += ["--extensions", *extensions]
    started = time.monotonic()
    try:
        res = subprocess.run(cmd, capture_output=True, text=True, timeout=TIER_TIMEOUT)
    except (subprocess.SubprocessError, OSError) as e:
        log_line(analysis_dir, f"tier3 analyzer launch failed: {_exc_tail(e)}")
        return False
    duration = time.monotonic() - started
    log_line(analysis_dir, f"tier3 analyzer rc={res.returncode} duration={duration:.1f}s "
                           f"extensions={extensions} languages={languages or []} "
                           f"out={'yes' if out_json.exists() else 'no'}"
                           + (f" stderr: {_log_tail(res.stderr)}" if res.stderr and res.stderr.strip() else ""))
    return res.returncode == 0 and out_json.exists()


def build_counts(graph: dict) -> dict:
    """v2 counts (spec §6): components/edges are label-filtered (Bean nodes /
    INJECTS edges — never len() of the mixed arrays, keeping v1 report
    semantics) and the structure sub-counts ride along from meta. v3 adds the
    meta.languages passthrough (spec §8) for the summary wording."""
    counts = {
        "components": sum(1 for n in graph.get("nodes", []) if n.get("label") == "Bean"),
        "edges": sum(1 for e in graph.get("edges", []) if e.get("label") == "INJECTS"),
        "cycles_fatal": 0, "cycles_tolerated": 0,
    }
    meta = graph.get("meta") or {}
    structure = meta.get("structure") or {}
    counts["structure"] = {k: structure.get(k) for k in ("files", "types", "methods", "call_edges")}
    counts["languages"] = list(meta.get("languages") or [])
    return counts


def render_structure_summary(structure: dict, lang: str = "zh",
                             languages: list[str] | None = None) -> str:
    idx = 0 if lang.startswith("zh") else 1
    text = STRUCTURE_SUMMARY[idx].format(
        files=structure.get("files"), types=structure.get("types"),
        methods=structure.get("methods"), call_edges=structure.get("call_edges"))
    languages = [l for l in (languages or []) if l]
    if languages:
        if lang.startswith("zh"):
            text += f"（{'、'.join(languages)}）"
        else:
            text += f" ({', '.join(languages)})"
    return text


def _go_binary_is_stale(binary: Path, source_dir: Path) -> bool:
    """True when any Go source is newer than the built binary.

    The dist binaries are cached across runs, so a user who installed an earlier
    SCV keeps running the loader they first built. That is not a cosmetic issue:
    the pre-10-15 loader reported every SCC as a tolerable cycle, including
    CALLS-only mutual recursion and single-node self loops, so an upgraded
    checkout silently disagreed with its own source.
    """
    if not binary.exists():
        return True
    built = binary.stat().st_mtime
    try:
        return any(p.stat().st_mtime > built
                   for p in source_dir.glob("*.go")) or \
            (source_dir / "go.mod").stat().st_mtime > built
    except OSError:
        return False


class _DistBuildLock:
    """Serialize dist `go build`s across concurrent runs (spec §10).

    O_EXCL create: exclusive by construction, no flock dependency. A lock
    whose mtime is older than BUILD_LOCK_STALE_SECONDS belonged to a
    crashed holder and is taken over (unlink + re-create); a live holder
    always finishes well inside that window, so waiters make progress
    without an explicit timeout. Release only unlinks a lock whose content
    is still ours (a taken-over lock belongs to its new holder)."""

    def __init__(self, analysis_dir: Path | None = None):
        self._path = DIST_ROOT / BUILD_LOCK_NAME
        self._analysis_dir = analysis_dir
        self._held = False
        self._token = ""

    def acquire(self) -> None:
        self._path.parent.mkdir(parents=True, exist_ok=True)
        while not self._held:
            try:
                fd = os.open(self._path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o644)
            except FileExistsError:
                try:
                    stale = time.time() - self._path.stat().st_mtime > BUILD_LOCK_STALE_SECONDS
                except OSError:
                    continue  # lock vanished between open and stat: retry now
                if stale:
                    if self._analysis_dir is not None:
                        log_line(self._analysis_dir, "dist build lock stale: taken over")
                    try:
                        self._path.unlink()
                    except OSError:
                        pass  # another waiter won the takeover race; retry
                time.sleep(0.05)
                continue
            self._token = f"pid={os.getpid()} acquired={int(time.time())}\n"
            with os.fdopen(fd, "w", encoding="utf-8") as fh:
                fh.write(self._token)
            self._held = True

    def release(self) -> None:
        if not self._held:
            return
        self._held = False
        try:
            if self._path.read_text(encoding="utf-8") == self._token:
                self._path.unlink()
        except OSError:
            pass  # already taken over as stale: nothing of ours left

    def __enter__(self) -> "_DistBuildLock":
        self.acquire()
        return self

    def __exit__(self, *_exc) -> None:
        self.release()


def build_loader(loader_dir: Path, analysis_dir: Path) -> bool:
    """Build the loader into dist (once; stale rebuilds) under the dist
    build lock (spec §10). True when the binary is ready to run."""
    binary = DIST_ROOT / "loader" / "perception-loader"
    if not _go_binary_is_stale(binary, loader_dir):
        return True
    try:
        binary.parent.mkdir(parents=True, exist_ok=True)
        with _DistBuildLock(analysis_dir):
            subprocess.run(["go", "build", "-o", str(binary), "."], cwd=loader_dir,
                           capture_output=True, text=True, timeout=TIER_TIMEOUT, check=True)
        return True
    except (subprocess.SubprocessError, OSError) as e:
        log_line(analysis_dir, f"tier6 loader build failed: {_exc_tail(e)}")
        return False


def run_loader(loader_dir: Path, json_path: Path, vg_path: Path, analysis_dir: Path):
    if not build_loader(loader_dir, analysis_dir):
        return None
    binary = DIST_ROOT / "loader" / "perception-loader"
    try:
        res = subprocess.run([str(binary), str(json_path), str(vg_path)],
                             capture_output=True, text=True, timeout=TIER_TIMEOUT)
    except (subprocess.SubprocessError, OSError) as e:
        log_line(analysis_dir, f"tier6 loader run failed: {e}")
        return None
    if res.returncode != 0:
        log_line(analysis_dir, f"tier6 loader rc={res.returncode} stderr: {_log_tail(res.stderr)}")
        return None
    try:
        return json.loads(res.stdout)
    except json.JSONDecodeError as e:
        log_line(analysis_dir, f"tier6 loader stdout not JSON: {e}")
        return None


def build_graphmcp(analysis_dir: Path) -> Path | None:
    """Build (once) the multi-repo graph-query MCP server. Best-effort: a
    failure here never affects the JSON/vg tiers — it only drops the query
    hint back to the per-snapshot vectra-mcp path."""
    binary = DIST_ROOT / "graphmcp" / "scv-graph-mcp"
    if not _go_binary_is_stale(binary, PERCEPTION_ROOT / "graphmcp"):
        return binary
    try:
        binary.parent.mkdir(parents=True, exist_ok=True)
        with _DistBuildLock(analysis_dir):
            subprocess.run(["go", "build", "-o", str(binary), "."],
                           cwd=PERCEPTION_ROOT / "graphmcp",
                           capture_output=True, text=True, timeout=TIER_TIMEOUT, check=True)
        return binary
    except (subprocess.SubprocessError, OSError) as e:
        log_line(analysis_dir, f"tier8 graphmcp build failed: {_exc_tail(e)}")
        return None


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("repo_path")
    ap.add_argument("analysis_dir")
    ap.add_argument("--source", choices=["local", "remote"], default="local")
    ap.add_argument("--lang", default="zh")
    # --version (spec §12): one-line JSON on stdout, exit 0. argparse's
    # version action fires during parse_args — before positional-argument
    # validation and before any tier work (no analysis dir is touched).
    ap.add_argument("--version", action="version",
                    version=json.dumps({"scv_version": scv_version(),
                                        "contract": "1.0"}))
    args = ap.parse_args()

    repo, analysis_dir = Path(args.repo_path).resolve(), Path(args.analysis_dir).resolve()
    analysis_dir.mkdir(parents=True, exist_ok=True)
    emit = {"status": "skipped", "reason": None,
            "tiers": {"json": False, "vg": False, "mcp_hint": False},
            "counts": None, "artifacts": {"structure_json": None, "structure_vg": None},
            "structure_summary": None, "mcp_add": None, "footnote": ""}

    # SCV_PERCEPTION_CONFIG points at an alternate config file. It exists so the
    # fixture suite can exercise structure mode (structure_extensions: []) without
    # rewriting the user's real ~/.scv/config.json. Unset, behaviour is unchanged.
    cfg_path = Path(os.environ.get("SCV_PERCEPTION_CONFIG")
                    or Path.home() / ".scv/config.json")
    try:
        cfg = json.loads(cfg_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        cfg = {}
    if not cfg.get("structure_analysis_enabled", True):
        emit["status"], emit["reason"] = "skipped", "disabled"
        log_line(analysis_dir, "tier0 disabled by config")
        print(json.dumps(emit, ensure_ascii=False))
        return 0
    extensions, ext_warnings = resolve_extensions(cfg)
    languages, lang_warnings = resolve_languages(cfg)

    # Config warnings must reach perception.log on EVERY exit path (spec
    # §7: unknown values warn into the log) — the not-framework and
    # tier-off returns below happen before the usual post-cleanup flush.
    def _flush_warnings() -> None:
        for warning in ext_warnings + lang_warnings:
            log_line(analysis_dir, warning)

    if not languages:
        # v3 spec §7: empty structure_languages turns the structure tier off
        # entirely — the same clean skip as the enabled flag, no footnote.
        emit["status"], emit["reason"] = "skipped", "disabled"
        log_line(analysis_dir, "tier0 disabled by config (structure_languages is empty)")
        _flush_warnings()
        print(json.dumps(emit, ensure_ascii=False))
        return 0

    # v3 detect gate (spec 2026-10-20 §7): the tier runs when ANY enabled
    # language has >= 1 source file; `not-framework` only when none does.
    detected = detect_languages(repo, languages)
    if not detected:
        emit["status"], emit["reason"] = "not-framework", None
        log_line(analysis_dir, "tier1 detect: not-framework")
        _flush_warnings()
        print(json.dumps(emit, ensure_ascii=False))
        return 0

    # The springboot extension keeps its own Spring-marker detection on top
    # of java (unchanged, v2 two-stage); its structure_extensions gate is
    # orthogonal to structure_languages. Undetected -> the extension simply
    # does not engage: the analyzer runs the language-agnostic core only.
    adapter = load_manifest("springboot")
    spring = bool(extensions) and detect_framework(repo, adapter)
    active_extensions = ["springboot"] if spring else []

    cleanup_stale(analysis_dir)
    _flush_warnings()
    log_line(analysis_dir, "tier1 start")
    detect_line = "tier1 detect: springboot" if spring else "tier1 detect: structure"
    log_line(analysis_dir, f"{detect_line}, source={args.source}, "
                           f"extensions={active_extensions}, languages={detected}")

    reasons: list[str] = []
    log_line(analysis_dir, "tier2 start")
    if os.environ.get("SCV_PERCEPTION_ANALYZER"):
        # substitute analyzer executable: invoked directly, no uv involved
        log_line(analysis_dir, "tier2 analyzer override active (SCV_PERCEPTION_ANALYZER)")
    else:
        uv_bin = probe_uv(analysis_dir)
        log_line(analysis_dir, f"tier2 uv probe: {uv_bin or 'not found'}")
        if not uv_bin:
            emit["reason"] = "uv-missing"
            emit["footnote"] = render_footnote(["uv-missing"], args.lang)
            print(json.dumps(emit, ensure_ascii=False))
            return 0

    log_line(analysis_dir, "tier3 start")
    structure_json = analysis_dir / "structure.json"
    if not run_analyzer(repo, structure_json, analysis_dir, active_extensions, detected):
        emit["reason"] = "analyzer-failed"
        emit["footnote"] = render_footnote(["analyzer-failed"], args.lang)
        print(json.dumps(emit, ensure_ascii=False))
        return 0

    try:
        graph = json.loads(structure_json.read_text(encoding="utf-8"))
        counts = build_counts(graph)
    except Exception as e:
        # Exit-0 contract: unreadable/malformed analyzer output degrades to the
        # same analyzer-failed skip as a crashed analyzer — never a traceback.
        emit["reason"] = "analyzer-failed"
        emit["footnote"] = render_footnote(["analyzer-failed"], args.lang)
        log_line(analysis_dir, f"tier3 analyzer output not a usable graph: {e}")
        print(json.dumps(emit, ensure_ascii=False))
        return 0
    emit["status"], emit["tiers"]["json"] = "ok", True
    emit["artifacts"]["structure_json"] = str(structure_json)
    structure = counts.get("structure") or {}
    if all(isinstance(v, int) for v in structure.values()):
        emit["structure_summary"] = render_structure_summary(
            structure, args.lang, counts.get("languages"))

    vg_path = analysis_dir / "structure.vg"
    loader_info = None
    log_line(analysis_dir, "tier6 start")
    go_ok = probe_go()
    if go_ok or prebuilt(DIST_ROOT / "loader" / "perception-loader"):
        loader_info = run_loader(PERCEPTION_ROOT / "loader", structure_json, vg_path, analysis_dir)
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
    else:
        # Spec §5.1: the adapter never computes SCCs, so when the Go/loader
        # tier did not run the cycle counts are null (not a false zero) and
        # the summary line omits the cycle clause.
        counts["cycles_fatal"] = None
        counts["cycles_tolerated"] = None
    emit["counts"] = counts

    # Fix round 1: the MCP hint is only meaningful when the snapshot it points
    # at actually exists — gate on the vg artifact, not just the binary. When
    # vg is missing, go-missing / mcp-missing reasons already explain it.
    # Graph-query tier (vectra v0.6 Cypher): our own multi-repo server is the
    # preferred registration — one scv-graph registration serves EVERY
    # analyzed repository, so the hint does not depend on this run's vg.
    log_line(analysis_dir, "tier8 start")
    graph_bin = (build_graphmcp(analysis_dir)
                 if go_ok or prebuilt(DIST_ROOT / "graphmcp" / "scv-graph-mcp")
                 else None)
    has_mcp = shutil.which("vectra-mcp") is not None
    ts = time.strftime("%Y-%m-%d %H:%M")
    if graph_bin:
        emit["tiers"]["mcp_hint"] = True
        emit["mcp_add"] = (f"claude mcp add scv-graph -- {graph_bin} "
                           f"# one registration serves every analyzed repo ({ts})")
    elif has_mcp and emit["artifacts"]["structure_vg"]:
        emit["tiers"]["mcp_hint"] = True
        emit["mcp_add"] = (f"claude mcp add scv-structure -- vectra-mcp -snapshot {vg_path} "
                           f"# generated {ts}")
    elif not has_mcp:
        reasons.append("mcp-missing")

    emit["reason"] = (reasons[0] if len(reasons) == 1 else reasons) if reasons else None
    emit["footnote"] = render_footnote(reasons, args.lang)
    log_line(analysis_dir, f"tiers done: {emit['tiers']} reasons={reasons}")
    print(json.dumps(emit, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
