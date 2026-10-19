#!/usr/bin/env python3
"""Assert the frozen expectations over the fixture analysis output.

Companion to run_fixtures.py: it runs the harness first (through `make
fixture-verify`) and then checks that each fixture produced what it is supposed
to. Every assertion here encodes a behaviour worth keeping — including one known
gap, which is asserted in its current (broken) state so that closing it has to
update this file.
"""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path


def load(analysis_home: Path, name: str) -> dict | None:
    path = analysis_home / name / "structure.json"
    return json.loads(path.read_text(encoding="utf-8")) if path.is_file() else None


def check_monolith(graph: dict | None, failures: list[str]) -> None:
    """Spring Boot monolith: every injection style, both cycle grades."""
    name = "java-springboot-monolith"
    if graph is None:
        failures.append(f"{name}: no structure.json (expected a full component graph)")
        return
    beans = {n["key"]: n for n in graph["nodes"] if n.get("label") == "Bean"}
    injects = [e for e in graph["edges"] if e.get("label") == "INJECTS"]
    vias = {(e.get("props") or {}).get("via") for e in injects}

    for want in ("constructor", "field", "setter", "beanMethod"):
        if want not in vias:
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
                        "gap is FIXED, so this assertion must be inverted")

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


def check_skipped(analysis_home: Path, failures: list[str]) -> None:
    """Non-Java repos must skip cleanly, never emit a structure graph."""
    for name in ("python-complex", "golang-complex"):
        if (analysis_home / name / "structure.json").is_file():
            failures.append(f"{name}: emitted structure.json, but perception is "
                            "Java-only and should have skipped")


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--analysis", default="fixture/analysis")
    args = ap.parse_args()
    analysis_home = Path(args.analysis)

    failures: list[str] = []
    check_monolith(load(analysis_home, "java-springboot-monolith"), failures)
    check_multimodule(load(analysis_home, "java-springboot-multimodule"), failures)
    check_skipped(analysis_home, failures)

    print()
    if failures:
        print("fixture-verify FAILED:")
        for item in failures:
            print(f"  - {item}")
        return 1
    print("fixture-verify OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
