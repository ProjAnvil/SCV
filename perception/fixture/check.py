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
# unsupported-language repo on the skip path. T8 adds the three new v3
# fixtures (ts / rust / mixed-polyglot) as precise multiset ground truths.
EXPECT_STRUCTURE = ("java-springboot-monolith", "java-springboot-multimodule",
                    "python-complex", "golang-complex",
                    "ts-complex", "rust-complex", "mixed-polyglot")
EXPECT_SKIP = ("ruby-plain",)

# v3 positive ground truths for the non-Java fixtures (spec §11.2):
# files/types/methods frozen at the analyzer's measured truth (asserted as
# floors so honest additive growth does not break the harness); by_lang must
# carry the per-language split and meta.languages must name exactly the one
# language each repo holds. call_edges is best-effort per language (spec §6)
# — a floor of 1 proves resolution produced real edges. golang-complex was
# re-frozen in T8 (external-embed honest-miss case added, §5.1).
STRUCTURE_TRUTHS = {
    "python-complex": {"lang": "python", "files": 4, "types": 9, "methods": 21},
    "golang-complex": {"lang": "go", "files": 5, "types": 10, "methods": 24},
}

# ---- v3 T8: precise multiset ground truths (spec §11.2/§11.3) ----------
# Every value below was hand-derived from the fixture source against the
# extractor rules BEFORE the confirming analyzer run (the derivation
# worksheets live in each fixture's README; discrepancies found during
# reconciliation were fixed on the derivation side unless they were
# analyzer bugs — the java IMPLEMENTS kind prop was one such bug).
# Edge tuples are (srcKey, dstKey, kind, receiver); labels without props
# keep empty strings so one shape serves every label.
_E = lambda src, dst, kind="", recv="": (src, dst, kind, recv)
_im = lambda src, dst: (src, dst, "declared", "")


PRECISE_TRUTHS = {
    "ts-complex": {
        "languages": ["javascript", "typescript"],
        "by_lang": {
            "javascript": {"files": 1, "types": 1, "methods": 6,
                           "call_edges": 1},
            "typescript": {"files": 7, "types": 7, "methods": 15,
                           "call_edges": 5},
        },
        "files": [
            "file:src/app.ts", "file:src/index.ts", "file:src/legacy/util.js",
            "file:src/models/base.ts", "file:src/models/circle.ts",
            "file:src/models/polygon.ts", "file:src/services/render.ts",
            "file:src/types.ts",
        ],
        "types": [
            "type:src/legacy/util::Scanner", "type:src/models/base::BaseShape",
            "type:src/models/circle::Circle", "type:src/models/polygon::Polygon",
            "type:src/services/render::Renderer", "type:src/types::Renderable",
            "type:src/types::Shape", "type:src/types::ShapeList",
        ],
        "methods": [
            "fn:src/app#main()",
            "fn:src/legacy/util#broken(0)", "fn:src/legacy/util#scanCircle(1)",
            "fn:src/legacy/util::Scanner#add(1)",
            "fn:src/legacy/util::Scanner#constructor(0)",
            "fn:src/legacy/util::Scanner#describe(0)",
            "fn:src/legacy/util::Scanner#first(0)",
            "fn:src/models/base::BaseShape#area()",
            "fn:src/models/base::BaseShape#name()",
            "fn:src/models/circle::Circle#area()",
            "fn:src/models/circle::Circle#draw()",
            "fn:src/models/circle::Circle#name()",
            "fn:src/models/polygon::Polygon#area()",
            "fn:src/models/polygon::Polygon#perimeter()",
            "fn:src/services/render::Renderer#banner(Shape)",
            "fn:src/services/render::Renderer#head()",
            "fn:src/services/render::Renderer#outline(Circle)",
            "fn:src/services/render::Renderer#scale(Shape,number)",
            "fn:src/types::Renderable#draw()", "fn:src/types::Shape#area()",
            "fn:src/types::Shape#name()",
        ],
        "edges": {
            "CONTAINS": [
                _E("file:src/types.ts", "type:src/types::Shape"),
                _E("file:src/types.ts", "type:src/types::Renderable"),
                _E("file:src/types.ts", "type:src/types::ShapeList"),
                _E("file:src/models/base.ts", "type:src/models/base::BaseShape"),
                _E("file:src/models/circle.ts", "type:src/models/circle::Circle"),
                _E("file:src/models/polygon.ts", "type:src/models/polygon::Polygon"),
                _E("file:src/services/render.ts",
                   "type:src/services/render::Renderer"),
                _E("file:src/legacy/util.js", "type:src/legacy/util::Scanner"),
            ],
            "EXTENDS": [
                _E("type:src/models/polygon::Polygon",
                   "type:src/models/base::BaseShape", "declared"),
            ],
            "IMPLEMENTS": [
                _im("type:src/models/base::BaseShape", "type:src/types::Shape"),
                _im("type:src/models/circle::Circle", "type:src/types::Shape"),
                _im("type:src/models/circle::Circle",
                    "type:src/types::Renderable"),
                _im("type:src/models/polygon::Polygon", "type:src/types::Shape"),
            ],
            "CALLS": [
                _E("fn:src/models/circle::Circle#draw()",
                   "fn:src/models/circle::Circle#area()", "direct", ""),
                _E("fn:src/services/render::Renderer#banner(Shape)",
                   "fn:src/services/render::Renderer#head()", "direct", ""),
                _E("fn:src/services/render::Renderer#banner(Shape)",
                   "fn:src/types::Shape#name()", "virtual-dispatch", ""),
                _E("fn:src/services/render::Renderer#outline(Circle)",
                   "fn:src/models/circle::Circle#draw()", "direct", ""),
                _E("fn:src/services/render::Renderer#scale(Shape,number)",
                   "fn:src/types::Shape#area()", "virtual-dispatch", ""),
                _E("fn:src/legacy/util::Scanner#add(1)",
                   "fn:src/legacy/util::Scanner#describe(0)", "direct", ""),
            ],
            "REFERENCES": [
                _E("fn:src/services/render::Renderer#banner(Shape)",
                   "type:src/types::Shape"),
                _E("fn:src/services/render::Renderer#outline(Circle)",
                   "type:src/models/circle::Circle"),
                _E("fn:src/services/render::Renderer#scale(Shape,number)",
                   "type:src/types::Shape"),
                _E("fn:src/legacy/util#scanCircle(1)",
                   "type:src/models/circle::Circle"),
            ],
            "IMPORTS": [
                _E("file:src/models/base.ts", "file:src/types.ts"),
                _E("file:src/models/circle.ts", "file:src/types.ts"),
                _E("file:src/models/polygon.ts", "file:src/types.ts"),
                _E("file:src/models/polygon.ts", "file:src/models/base.ts"),
                _E("file:src/services/render.ts", "file:src/models/circle.ts"),
                _E("file:src/services/render.ts", "file:src/types.ts"),
                _E("file:src/legacy/util.js", "file:src/models/circle.ts"),
                _E("file:src/index.ts", "file:src/types.ts"),
                _E("file:src/index.ts", "file:src/models/base.ts"),
                _E("file:src/index.ts", "file:src/models/circle.ts"),
                _E("file:src/index.ts", "file:src/models/polygon.ts"),
                _E("file:src/app.ts", "file:src/index.ts"),
            ],
        },
        "structure": {"call_edges": 6, "dropped_edges": 2,
                      "unresolved_refs": 3, "key_collisions": 0,
                      "import_edges": 12, "go_unverifiable_satisfaction": 0},
    },

    "rust-complex": {
        "languages": ["rust"],
        "by_lang": {
            "rust": {"files": 3, "types": 3, "methods": 7, "call_edges": 3},
        },
        "files": [
            "file:src/inventory/mod.rs", "file:src/lib.rs", "file:src/shape.rs",
        ],
        "types": [
            "type:warehouse::inventory::Inventory",
            "type:warehouse::shape::Circle", "type:warehouse::shape::Shape",
        ],
        "methods": [
            "fn:warehouse#audit()", "fn:warehouse#banner(&str)",
            "fn:warehouse#measure(&dyn Shape)",
            "fn:warehouse::inventory::Inventory#area()",
            "fn:warehouse::shape::Circle#area()",
            "fn:warehouse::shape::Shape#area()",
            "fn:warehouse::shape::Shape#scale(f64)",
        ],
        "edges": {
            "CONTAINS": [
                _E("file:src/shape.rs", "type:warehouse::shape::Shape"),
                _E("file:src/shape.rs", "type:warehouse::shape::Circle"),
                _E("file:src/inventory/mod.rs",
                   "type:warehouse::inventory::Inventory"),
            ],
            "IMPLEMENTS": [
                _im("type:warehouse::shape::Circle",
                    "type:warehouse::shape::Shape"),
                _im("type:warehouse::inventory::Inventory",
                    "type:warehouse::shape::Shape"),
            ],
            "CALLS": [
                _E("fn:warehouse::shape::Shape#scale(f64)",
                   "fn:warehouse::shape::Shape#area()", "virtual-dispatch", ""),
                _E("fn:warehouse#audit()",
                   "fn:warehouse::inventory::Inventory#area()", "direct", ""),
                _E("fn:warehouse#measure(&dyn Shape)",
                   "fn:warehouse::shape::Shape#area()", "virtual-dispatch", ""),
            ],
            "REFERENCES": [
                _E("file:src/lib.rs", "type:warehouse::inventory::Inventory"),
                _E("file:src/lib.rs", "type:warehouse::shape::Shape"),
                _E("file:src/inventory/mod.rs", "type:warehouse::shape::Shape"),
                _E("fn:warehouse#audit()",
                   "type:warehouse::inventory::Inventory"),
                _E("fn:warehouse#measure(&dyn Shape)",
                   "type:warehouse::shape::Shape"),
            ],
            "IMPORTS": [
                _E("file:src/lib.rs", "file:src/inventory/mod.rs"),
                _E("file:src/lib.rs", "file:src/shape.rs"),
                _E("file:src/inventory/mod.rs", "file:src/shape.rs"),
            ],
        },
        "structure": {"call_edges": 3, "dropped_edges": 2,
                      "unresolved_refs": 0, "key_collisions": 0,
                      "import_edges": 3, "go_unverifiable_satisfaction": 0},
    },

    "mixed-polyglot": {
        "languages": ["java", "python", "typescript"],
        "by_lang": {
            "java": {"files": 2, "types": 2, "methods": 2, "call_edges": 0},
            "python": {"files": 2, "types": 1, "methods": 2, "call_edges": 1},
            "typescript": {"files": 2, "types": 2, "methods": 2,
                           "call_edges": 0},
        },
        "files": [
            "file:py/__init__.py", "file:py/worker.py",
            "file:src/main/java/com/acme/bridge/Greetable.java",
            "file:src/main/java/com/acme/bridge/Greeter.java",
            "file:ts/contracts.ts", "file:ts/panel.ts",
        ],
        "types": [
            "type:com.acme.bridge.Greetable", "type:com.acme.bridge.Greeter",
            "type:py.worker.Worker", "type:ts/contracts::Notifiable",
            "type:ts/panel::Panel",
        ],
        "methods": [
            "fn:com.acme.bridge.Greetable#greet()",
            "fn:com.acme.bridge.Greeter#greet()",
            "fn:py.worker.Worker#prepare(0)", "fn:py.worker.Worker#start(0)",
            "fn:ts/contracts::Notifiable#notify()",
            "fn:ts/panel::Panel#notify()",
        ],
        "edges": {
            "CONTAINS": [
                _E("file:py/worker.py", "type:py.worker.Worker"),
                _E("file:src/main/java/com/acme/bridge/Greetable.java",
                   "type:com.acme.bridge.Greetable"),
                _E("file:src/main/java/com/acme/bridge/Greeter.java",
                   "type:com.acme.bridge.Greeter"),
                _E("file:ts/contracts.ts", "type:ts/contracts::Notifiable"),
                _E("file:ts/panel.ts", "type:ts/panel::Panel"),
            ],
            "IMPLEMENTS": [
                # java's written-in-source implements carries kind: declared
                # like every other language (spec §5) — the cross-language
                # crosscheck against go/rust/ts.
                _im("type:com.acme.bridge.Greeter",
                    "type:com.acme.bridge.Greetable"),
                _im("type:ts/panel::Panel", "type:ts/contracts::Notifiable"),
            ],
            "CALLS": [
                _E("fn:py.worker.Worker#start(0)",
                   "fn:py.worker.Worker#prepare(0)", "direct", ""),
            ],
            "REFERENCES": [],
            "IMPORTS": [
                # one per language, coexisting in the single graph
                _E("file:src/main/java/com/acme/bridge/Greeter.java",
                   "file:src/main/java/com/acme/bridge/Greetable.java"),
                _E("file:py/__init__.py", "file:py/worker.py"),
                _E("file:ts/panel.ts", "file:ts/contracts.ts"),
            ],
        },
        "structure": {"call_edges": 1, "dropped_edges": 0,
                      "unresolved_refs": 0, "key_collisions": 0,
                      "import_edges": 3, "go_unverifiable_satisfaction": 0},
    },

    # golang-complex: the §5.1 precision contract — the IMPLEMENTS multiset
    # asserted exactly (pointer case, promotion-earned case) plus the
    # honest-miss counter frozen at 1 (external *sync.Mutex embed). Node key
    # sets and the remaining counters are exact too; CALLS/REFERENCES/IMPORTS
    # are covered by their label counts in "structure".
    "golang-complex": {
        "languages": ["go"],
        "by_lang": {
            "go": {"files": 5, "types": 10, "methods": 24, "call_edges": 15},
        },
        "files": [
            "file:internal/store/audit.go", "file:internal/store/store.go",
            "file:internal/worker/worker.go", "file:main.go",
            "file:pkg/queue/queue.go",
        ],
        "types": [
            "type:internal/store.AuditLog", "type:internal/store.Entry",
            "type:internal/store.Locker", "type:internal/store.Memory",
            "type:internal/store.ReadOnlyLog", "type:internal/store.Reader",
            "type:internal/store.Store", "type:internal/store.Writer",
            "type:internal/worker.Pool", "type:pkg/queue.Queue",
        ],
        "methods": [
            "fn:#main()",
            "fn:internal/store#NewMemory()",
            "fn:internal/store.AuditLog#Get(context.Context,string)",
            "fn:internal/store.AuditLog#Put(context.Context,string,string)",
            "fn:internal/store.Locker#Lock()",
            "fn:internal/store.Locker#Unlock()",
            "fn:internal/store.Memory#Get(context.Context,string)",
            "fn:internal/store.Memory#Put(context.Context,string,string)",
            "fn:internal/store.Memory#Recent()",
            "fn:internal/store.ReadOnlyLog#Snapshot()",
            "fn:internal/store.Reader#Get(context.Context,string)",
            "fn:internal/store.Store#Get(context.Context,string)",
            "fn:internal/store.Store#Put(context.Context,string,string)",
            "fn:internal/store.Writer#Put(context.Context,string,string)",
            "fn:internal/worker#NewPool(store.Store,int)",
            "fn:internal/worker.Pool#Start(context.Context)",
            "fn:internal/worker.Pool#handle(string)",
            "fn:internal/worker.Pool#loop(context.Context)",
            "fn:internal/worker.Pool#process(string,int)",
            "fn:internal/worker.Pool#retry(string,int)",
            "fn:pkg/queue#New(int)",
            "fn:pkg/queue.Queue#Drain()", "fn:pkg/queue.Queue#Len()",
            "fn:pkg/queue.Queue#Push(T)",
        ],
        "edges": {
            "EXTENDS": [
                _E("type:internal/store.Store", "type:internal/store.Reader",
                   "declared"),
                _E("type:internal/store.Store", "type:internal/store.Writer",
                   "declared"),
                _E("type:internal/store.ReadOnlyLog",
                   "type:internal/store.Memory", "declared"),
            ],
            "IMPLEMENTS": [
                (src, dst, "method-set", recv)
                for src, dst, recv in [
                    ("type:internal/store.Memory",
                     "type:internal/store.Reader", "pointer"),
                    ("type:internal/store.Memory",
                     "type:internal/store.Writer", "pointer"),
                    ("type:internal/store.Memory",
                     "type:internal/store.Store", "pointer"),
                    ("type:internal/store.AuditLog",
                     "type:internal/store.Reader", "pointer"),
                    ("type:internal/store.AuditLog",
                     "type:internal/store.Writer", "pointer"),
                    ("type:internal/store.AuditLog",
                     "type:internal/store.Store", "pointer"),
                    ("type:internal/store.ReadOnlyLog",
                     "type:internal/store.Reader", "value"),
                    ("type:internal/store.ReadOnlyLog",
                     "type:internal/store.Writer", "value"),
                    ("type:internal/store.ReadOnlyLog",
                     "type:internal/store.Store", "value"),
                ]
            ],
        },
        "structure": {"call_edges": 15, "dropped_edges": 1,
                      "unresolved_refs": 23, "key_collisions": 0,
                      "import_edges": 4, "go_unverifiable_satisfaction": 1},
    },
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


def check_precise_ground_truth(analysis_home: Path, failures: list[str]) -> None:
    """v3 T8 fixtures: node key sets, edge multisets and counter values
    asserted EXACTLY (v2 precision-contract style, spec §11.2/§11.3). The
    golang entry enforces the §5.1 IMPLEMENTS multiset (pointer case,
    promotion-earned case, honest-miss counter)."""
    for name, truth in PRECISE_TRUTHS.items():
        graph = load(analysis_home, name)
        if graph is None:
            failures.append(f"{name}: no structure.json (expected a structure graph)")
            continue
        meta = graph.get("meta") or {}
        structure = meta.get("structure") or {}
        label = f"{name}: "

        # node key sets — exact
        for node_label, want in (("File", truth["files"]),
                                 ("Type", truth["types"]),
                                 ("Method", truth["methods"])):
            got = sorted(n["key"] for n in graph["nodes"]
                         if n.get("label") == node_label)
            if got != want:
                failures.append(f"{label}{node_label} key set mismatch\n"
                                f"    got:  {got}\n"
                                f"    want: {want}")

        # edge multisets — (src, dst, kind, receiver) tuples, exact
        for edge_label, want in truth["edges"].items():
            got = sorted(
                (e["srcKey"], e["dstKey"],
                 (e.get("props") or {}).get("kind") or "",
                 (e.get("props") or {}).get("receiver") or "")
                for e in graph["edges"] if e.get("label") == edge_label)
            if got != sorted(want):
                failures.append(f"{label}{edge_label} multiset mismatch\n"
                                f"    got:  {got}\n"
                                f"    want: {sorted(want)}")
        pinned = set(truth["edges"])
        # labels the truth pins exhaustively (a truth that pins CONTAINS
        # pins every label; golang pins only its §5.1 heritage multisets)
        if "CONTAINS" in pinned:
            extra = sorted({e.get("label") for e in graph["edges"]
                            if e.get("label") not in pinned})
            if extra:
                failures.append(f"{label}unexpected edge labels {extra}")

        # counters — exact
        for key, want in truth["structure"].items():
            if structure.get(key) != want:
                failures.append(f"{label}structure.{key}="
                                f"{structure.get(key)!r}, expected {want!r}")

        # meta.languages + by_lang — exact
        if meta.get("languages") != truth["languages"]:
            failures.append(f"{label}meta.languages={meta.get('languages')!r}, "
                            f"expected {truth['languages']!r}")
        by_lang = structure.get("by_lang") or {}
        for lang, want in truth["by_lang"].items():
            if by_lang.get(lang) != want:
                failures.append(f"{label}by_lang[{lang!r}]="
                                f"{by_lang.get(lang)!r}, expected {want!r}")
        if set(by_lang) != set(truth["by_lang"]):
            failures.append(f"{label}by_lang languages {sorted(by_lang)} != "
                            f"{sorted(truth['by_lang'])}")


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
    check_precise_ground_truth(analysis_home, failures)
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
