#!/usr/bin/env python3
"""Run perceive.py over every fixture repo and assert the expected outcome.

This is the deterministic half of an `scv run`: Step 5.5 of
skills/*/references/run.md invokes

    python3 perceive.py <repo> <analysis_dir> --source local --lang en

and this script does exactly that, once per fixture, then checks the result.
The LLM-driven document tiers (README / SUMMARY / ARCHITECTURE / FILE_INDEX) are
out of scope — they need an agent. What this covers is whether perception
parses the tree and grades it correctly.

Exit status: 0 when every expectation holds, 1 otherwise (so it can sit inside
`make test`).
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

# Which repos must yield a structure graph, and which must skip cleanly.
# v3 (spec 2026-10-20 §10): python-complex / golang-complex flipped from the
# skip path to positive ground truths; ruby-plain keeps a deliberately
# unsupported-language repo on the skip path.
EXPECT_STRUCTURE = ("java-springboot-monolith", "java-springboot-multimodule",
                    "python-complex", "golang-complex")
EXPECT_SKIP = ("ruby-plain",)

# v3 positive ground truths for the non-Java fixtures (spec §11.2):
# files/types/methods frozen at the analyzer's measured truth (asserted as
# floors so honest additive growth does not break the harness); by_lang must
# carry the per-language split and meta.languages must name exactly the one
# language each repo holds. call_edges is best-effort per language (spec §6)
# — a floor of 1 proves resolution produced real edges.
STRUCTURE_TRUTHS = {
    "python-complex": {"lang": "python", "files": 4, "types": 9, "methods": 21},
    "golang-complex": {"lang": "go", "files": 4, "types": 6, "methods": 19},
}


def write_config(fixture_dir: Path, extensions: list[str]) -> Path:
    """Materialise a config file for this run; return its path."""
    name = "config.structure.json" if not extensions else "config.springboot.json"
    path = fixture_dir / name
    path.write_text(json.dumps({"structure_extensions": extensions}), encoding="utf-8")
    return path.resolve()


def run_one(repo: Path, out_dir: Path, env: dict) -> dict | None:
    """Invoke perceive.py once and return its parsed stdout."""
    res = subprocess.run(
        [sys.executable, str(PERCEIVE), str(repo), str(out_dir),
         "--source", "local", "--lang", "en"],
        capture_output=True, text=True, env=env, cwd=PERCEPTION_ROOT,
    )
    if res.returncode != 0:
        print(f"FAIL {repo.name}: perceive.py exited {res.returncode}", file=sys.stderr)
        print(res.stderr[-800:], file=sys.stderr)
        return None
    try:
        return json.loads(res.stdout)
    except json.JSONDecodeError as exc:
        print(f"FAIL {repo.name}: stdout is not JSON ({exc})", file=sys.stderr)
        print(res.stdout[-400:], file=sys.stderr)
        return None


def load(analysis_home: Path, name: str) -> dict | None:
    path = analysis_home / name / "structure.json"
    return json.loads(path.read_text(encoding="utf-8")) if path.is_file() else None


# ---- assertions ---------------------------------------------------------- #

def check_monolith(graph: dict | None, failures: list[str]) -> None:
    """Spring Boot monolith: every injection style, both cycle grades."""
    name = "java-springboot-monolith"
    if graph is None:
        failures.append(f"{name}: no structure.json (expected a component graph)")
        return
    beans = {n["key"]: n for n in graph["nodes"] if n.get("label") == "Bean"}
    injects = [e for e in graph["edges"] if e.get("label") == "INJECTS"]

    for want in ("constructor", "field", "setter", "beanMethod"):
        if want not in {(e.get("props") or {}).get("via") for e in injects}:
            failures.append(f"{name}: no {want!r} injection edge was extracted")

    for want in ("bean:orderController", "bean:cachingRateClient", "bean:acmeProperties"):
        if want not in beans:
            failures.append(f"{name}: missing bean {want}")

    controller = (beans.get("bean:orderController") or {}).get("props") or {}
    if controller.get("stereotype") != "RestController":
        failures.append(f"{name}: orderController stereotype is "
                        f"{controller.get('stereotype')!r}, expected 'RestController'")

    conds = " ".join(str(((n or {}).get("props") or {}).get("conditions") or "")
                     for n in beans.values())
    for want in ("ConditionalOnMissingBean", "ConditionalOnProperty", "Profile"):
        if want not in conds:
            failures.append(f"{name}: no bean records the {want} condition")

    # Known gap, frozen: an on-demand (wildcard) import hides the stereotype.
    if "bean:statusController" in beans:
        failures.append(f"{name}: statusController is now a bean — the wildcard-import "
                        "gap is FIXED, so invert this assertion")

    adj: dict[str, list[str]] = {}
    for edge in injects:
        adj.setdefault(edge["srcKey"], []).append(edge["dstKey"])

    def cyclic(a: str, b: str) -> bool:
        return b in adj.get(a, []) and a in adj.get(b, [])

    if not cyclic("bean:orderService", "bean:pricingService"):
        failures.append(f"{name}: missing the fatal constructor cycle "
                        "orderService <-> pricingService")
    if not cyclic("bean:notificationService", "bean:auditService"):
        failures.append(f"{name}: missing the tolerated field cycle "
                        "notificationService <-> auditService")


def check_multimodule(graph: dict | None, failures: list[str]) -> None:
    """Multi-module: boot class in a submodule, injection across modules."""
    name = "java-springboot-multimodule"
    if graph is None:
        failures.append(f"{name}: no structure.json (expected cross-module beans)")
        return
    beans = {n["key"] for n in graph["nodes"] if n.get("label") == "Bean"}
    for want in ("bean:invoiceController", "bean:invoiceService", "bean:ledgerClient"):
        if want not in beans:
            failures.append(f"{name}: missing bean {want}")
    pairs = {(e["srcKey"], e["dstKey"]) for e in graph["edges"] if e.get("label") == "INJECTS"}
    for src, dst in (("bean:invoiceController", "bean:invoiceService"),
                     ("bean:invoiceService", "bean:ledgerClient")):
        if (src, dst) not in pairs:
            failures.append(f"{name}: missing cross-module edge {src} -> {dst}")


def check_structure_ground_truth(analysis_home: Path, failures: list[str]) -> None:
    """v3 non-Java fixtures: files/types/methods floors + by_lang split +
    meta.languages (spec §11.2), in BOTH config modes (the structure tier is
    language-gated, not extension-gated)."""
    for name, truth in STRUCTURE_TRUTHS.items():
        graph = load(analysis_home, name)
        if graph is None:
            failures.append(f"{name}: no structure.json (expected a structure graph)")
            continue
        meta = graph.get("meta") or {}
        structure = meta.get("structure") or {}
        for key in ("files", "types", "methods"):
            if (structure.get(key) or 0) < truth[key]:
                failures.append(f"{name}: structure.{key}={structure.get(key)!r}, "
                                f"expected >= {truth[key]}")
        if (structure.get("call_edges") or 0) < 1:
            failures.append(f"{name}: structure.call_edges={structure.get('call_edges')!r}, "
                            "expected >= 1")
        if meta.get("languages") != [truth["lang"]]:
            failures.append(f"{name}: meta.languages={meta.get('languages')!r}, "
                            f"expected ['{truth['lang']}']")
        by_lang = structure.get("by_lang") or {}
        entry = by_lang.get(truth["lang"])
        if not isinstance(entry, dict):
            failures.append(f"{name}: meta.structure.by_lang[{truth['lang']!r}] missing")
            continue
        for key in ("files", "types", "methods"):
            if (entry.get(key) or 0) < truth[key]:
                failures.append(f"{name}: by_lang.{truth['lang']}.{key}="
                                f"{entry.get(key)!r}, expected >= {truth[key]}")


def check_skipped(analysis_home: Path, failures: list[str]) -> None:
    """Unsupported-language repos must skip cleanly: no structure graph, exit 0."""
    for name in EXPECT_SKIP:
        if (analysis_home / name / "structure.json").is_file():
            failures.append(f"{name}: emitted structure.json, but no enabled "
                            "language is present and perception must skip")


# ---- driver -------------------------------------------------------------- #

def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--repos", default="fixture/repos")
    ap.add_argument("--analysis", default="fixture/analysis")
    ap.add_argument("--extensions", default="springboot",
                    help="comma-separated; empty string selects structure-only mode")
    args = ap.parse_args()

    repos_home = Path(args.repos)
    analysis_home = Path(args.analysis)
    if not repos_home.is_absolute():
        repos_home = PERCEPTION_ROOT / repos_home
    if not analysis_home.is_absolute():
        analysis_home = PERCEPTION_ROOT / analysis_home

    extensions = [e.strip() for e in args.extensions.split(",") if e.strip()]
    spring = "springboot" in extensions
    cfg = write_config(Path(__file__).resolve().parent, extensions)
    env = dict(os.environ, SCV_PERCEPTION_CONFIG=str(cfg))
    analysis_home.mkdir(parents=True, exist_ok=True)

    targets = sorted(p for p in repos_home.iterdir() if p.is_dir())
    if not targets:
        sys.exit(f"no fixture repos under {repos_home}")

    rows = []
    for repo in targets:
        emit = run_one(repo, analysis_home / repo.name, env)
        if emit is None:
            rows.append((repo.name, "CRASH", "-", "-", "-"))
            continue
        counts = emit.get("counts") or {}
        fatal, tolerated = counts.get("cycles_fatal"), counts.get("cycles_tolerated")
        rows.append((repo.name, emit.get("status") or "?", emit.get("reason") or "-",
                     f"{counts.get('components', '-')}/{counts.get('edges', '-')}",
                     "cycles=n/a" if fatal is None
                     else f"fatal={fatal} tolerated={tolerated}"))

    width = max(len(r[0]) for r in rows)
    print(f"{'repo'.ljust(width)}  {'status':<14} {'reason':<16} {'beans/edges':<13} cycles")
    print("-" * (width + 60))
    for name, status, reason, be, cyc in rows:
        print(f"{name.ljust(width)}  {status:<14} {reason:<16} {be:<13} {cyc}")
    print(f"\nextensions={','.join(extensions) or '(structure mode)'}"
          f"   analysis={analysis_home}")

    failures: list[str] = []
    for name, status, *_ in rows:
        if status == "CRASH":
            failures.append(f"{name}: perceive.py crashed")
        elif name in EXPECT_SKIP and status not in ("skipped", "not-framework"):
            failures.append(f"{name}: expected a clean skip, got status {status!r}")
        elif name in EXPECT_STRUCTURE and status != "ok":
            failures.append(f"{name}: expected status 'ok', got {status!r}")

    # Bean-level expectations only apply when the Spring extension is active;
    # structure mode produces no beans by design. The v3 structure ground
    # truths hold in BOTH modes (the language gate is orthogonal to the
    # extension gate).
    if spring:
        check_monolith(load(analysis_home, "java-springboot-monolith"), failures)
        check_multimodule(load(analysis_home, "java-springboot-multimodule"), failures)
    check_structure_ground_truth(analysis_home, failures)
    check_skipped(analysis_home, failures)

    print()
    if failures:
        print("fixture check FAILED:")
        for item in failures:
            print(f"  - {item}")
        return 1
    print("fixture check OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
