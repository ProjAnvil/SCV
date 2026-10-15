#!/usr/bin/env python3
"""Cross-validation: the reference (Spoon) engine vs the v2 tree-sitter
analyzer over the 5 frozen fixtures (spec 2026-10-14 §5 precision contract;
plan Task 7 — run via `make crosscheck` from perception/).

Both engines run over TEMP COPIES of each fixture (the frozen trees are
never touched; Spoon's GRADLE mode builds inside its own copy). Both dumps
are projected through the SAME multiset tuple projection —

    beans:   (key, stereotype, conditions)
    edges:   (srcKey, dstKey, via, qualifier, wraps, ambiguous)

— and compared as sorted multisets (collections.Counter diff). Positional
props (at/source), ordering, and the Spring-Data repository rule are out of
scope by the §5 definition.

Spoon runs in GRADLE mode (classpath resolved via the fixture's own build
files — never modified, cp-init.gradle registers a read-only task):
noclasspath mode is measurably DEGRADED on every fixture (see
analyzer/tests/test_fixtures.py) and would produce false mismatches.
Requires java + gradle + uv on PATH; the first run of a fixture may need
the network for plugin/dependency resolution (cached afterwards).
"""
from __future__ import annotations

import json
import shutil
import subprocess
import sys
import tempfile
from collections import Counter
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]          # perception/
FIXTURES = ROOT / "adapters/springboot/src/test/resources/fixtures"
ADAPTER = ROOT / "adapters/springboot"
SPOON_BIN = ADAPTER / "build/install/springboot-probe/bin/springboot-probe"
ANALYZER = ROOT / "analyzer"

FIXTURE_NAMES = (
    "gradle-lombok-sample",
    "boot2-legacy",
    "boot3-modern",
    "boot2-multi",
    "torture-app",
)

ENGINE_TIMEOUT = 900  # Spoon gradle mode resolves dependencies on first run


def die(msg: str) -> None:
    print(f"crosscheck: {msg}", file=sys.stderr)
    sys.exit(2)


def copy_fixture(name: str, dst: Path) -> Path:
    """Temp copy with nested build/.gradle state stripped (Spoon creates its
    own inside the copy; tree-sitter would just have to exclude it)."""
    src = FIXTURES / name
    for f in src.rglob("*"):
        if not f.is_file():
            continue
        rel = f.relative_to(src)
        if any(part in ("build", ".gradle") for part in rel.parts):
            continue
        target = dst / rel
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(f.read_bytes())
    return dst


def ensure_spoon() -> None:
    """Build the reference engine once (gradle installDist in the adapter)."""
    if SPOON_BIN.exists():
        return
    print("crosscheck: building reference engine (gradle installDist)...")
    try:
        res = subprocess.run(["gradle", "installDist", "-q"], cwd=ADAPTER,
                             capture_output=True, text=True, timeout=ENGINE_TIMEOUT)
    except (OSError, subprocess.TimeoutExpired) as e:
        die(f"gradle installDist failed to launch: {e}")
    if res.returncode != 0 or not SPOON_BIN.exists():
        tail = " ".join((res.stderr or "").split())[-1500:]
        die(f"gradle installDist failed: {tail}")


def run_engine(cmd: list[str], out_json: Path, cwd: Path | None = None) -> dict:
    try:
        res = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True,
                             timeout=ENGINE_TIMEOUT)
    except (OSError, subprocess.TimeoutExpired) as e:
        die(f"engine launch failed ({cmd[0]}): {e}")
    if res.returncode != 0 or not out_json.exists():
        tail = " ".join(filter(None, (res.stderr, res.stdout)))[-1500:]
        die(f"engine {cmd[0]} failed (rc={res.returncode}): {tail}")
    return json.loads(out_json.read_text(encoding="utf-8"))


def bean_tuples(payload: dict) -> Counter:
    return Counter(
        (n["key"], n["props"].get("stereotype"), n["props"].get("conditions"))
        for n in payload["nodes"] if n.get("label") == "Bean")


def edge_tuples(payload: dict) -> Counter:
    return Counter(
        (e["srcKey"], e["dstKey"], e["props"].get("via"), e["props"].get("qualifier"),
         e["props"].get("wraps"), e["props"].get("ambiguous", False))
        for e in payload["edges"] if e.get("label") == "INJECTS")


def describe_diff(kind: str, want: Counter, got: Counter, limit: int = 10) -> list[str]:
    lines = []
    for tup, n in sorted((want - got).items())[:limit]:
        lines.append(f"    reference-only {kind} x{n}: {tup}")
    for tup, n in sorted((got - want).items())[:limit]:
        lines.append(f"    treesitter-only {kind} x{n}: {tup}")
    return lines


def main() -> int:
    if not shutil.which("uv"):
        die("uv not on PATH")
    ensure_spoon()

    failures = 0
    with tempfile.TemporaryDirectory(prefix="scv-crosscheck-") as tmp:
        tmp = Path(tmp)
        for name in FIXTURE_NAMES:
            spoon_repo = copy_fixture(name, tmp / f"{name}-spoon")
            ts_repo = copy_fixture(name, tmp / f"{name}-ts")
            spoon_out = tmp / f"{name}.spoon.json"
            ts_out = tmp / f"{name}.ts.json"

            spoon = run_engine([str(SPOON_BIN), "gradle", str(spoon_repo), str(spoon_out)],
                               spoon_out)
            ts = run_engine(
                ["uv", "run", "--project", str(ANALYZER), "python",
                 str(ANALYZER / "analyzer.py"), str(ts_repo), str(ts_out),
                 "--extensions", "springboot"], ts_out)

            bean_diff = describe_diff("bean", bean_tuples(spoon), bean_tuples(ts))
            edge_diff = describe_diff("edge", edge_tuples(spoon), edge_tuples(ts))
            if bean_diff or edge_diff:
                failures += 1
                print(f"FAIL {name}")
                for line in bean_diff + edge_diff:
                    print(line)
            else:
                print(f"PASS {name} "
                      f"(beans={sum(bean_tuples(ts).values())} "
                      f"injects={sum(edge_tuples(ts).values())})")

    if failures:
        print(f"crosscheck: {failures}/{len(FIXTURE_NAMES)} fixtures FAILED")
        return 1
    print(f"crosscheck: all {len(FIXTURE_NAMES)} fixtures match "
          "(§5 multiset projection)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
