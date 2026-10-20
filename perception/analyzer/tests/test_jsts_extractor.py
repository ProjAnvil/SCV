"""jsts extractor tests — spec 2026-10-20 §3.1/§4/§5/§6 (TDD, RED first).

The snippets tree is a mixed TS + JS mini-repo: interfaces/implements,
extends, annotated receivers (class + interface), cross-file imports,
an `import type` (REFERENCES only, no IMPORTS), a one-hop re-export with
a two-hop consumer, a default export, a .tsx file (TSX grammar path),
CJS require with destructuring, bare npm imports, builtins, and one
unknown free call.
"""
from __future__ import annotations

import json
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from structure import jsts_extractor  # noqa: E402
from structure.model import Graph_out  # noqa: E402

SNIPPETS = Path(__file__).resolve().parent / "snippets" / "ts"

# ---- key scheme (spec §4 jsts rows) ----

F_SHAPES = "file:shapes.ts"
F_BASE = "file:base.ts"
F_SERVICE = "file:service.ts"
F_REPORT = "file:report.ts"
F_FORMAT = "file:format.ts"
F_WIDGET = "file:widget.ts"
F_BARREL = "file:barrel.ts"
F_CONSUMER = "file:consumer.ts"
F_PANEL = "file:panel.tsx"
F_UTIL = "file:util.js"
F_APP = "file:app.js"

T_SHAPE = "type:shapes::Shape"
T_CIRCLE = "type:shapes::Circle"
T_BASE = "type:base::Base"
T_SERVICE = "type:service::Service"
T_REPORT = "type:report::Report"
T_SESSION = "type:report::Session"
T_WIDGET = "type:widget::Widget"
T_PANEL = "type:panel::Panel"
T_TRACKER = "type:util::Tracker"

# TS: annotated simple name per param, `?` when unannotated (spec §4)
M_SHAPE_AREA = "fn:shapes::Shape#area()"
M_SHAPE_SCALE = "fn:shapes::Shape#scale(number)"
M_CIRCLE_CTOR = "fn:shapes::Circle#constructor(number)"
M_CIRCLE_AREA = "fn:shapes::Circle#area()"
M_CIRCLE_SCALE = "fn:shapes::Circle#scale(number)"
M_BASE_DESCRIBE = "fn:base::Base#describe()"
M_SVC_AREA = "fn:service::Service#area()"
M_SVC_SCALE = "fn:service::Service#scale(number)"
M_SVC_PROCESS = "fn:service::Service#process(Shape,number)"
M_SVC_RENDER = "fn:service::Service#render(Circle)"
M_SVC_EMIT = "fn:service::Service#emit(Report)"
M_REPORT_RENDER = "fn:report::Report#render()"
M_SESSION_START = "fn:report::Session#start()"
M_FORMAT = "fn:format#format(number)"
M_WIDGET_PAINT = "fn:widget::Widget#paint(?)"
M_CONSUMER_MOUNT = "fn:consumer#mount()"
M_PANEL_MOUNT = "fn:panel::Panel#mount()"
# JS: arity only (spec §4)
M_TRACKER_CTOR = "fn:util::Tracker#constructor(1)"
M_TRACKER_TRACK = "fn:util::Tracker#track(1)"
M_TRACKER_FLUSH = "fn:util::Tracker#flush(1)"
M_UTIL_HELPER = "fn:util#helper(2)"
M_APP_MAIN = "fn:app#main(0)"

_TS_NODE_KEYS = {
    T_SHAPE, T_CIRCLE, T_BASE, T_SERVICE, T_REPORT, T_SESSION, T_WIDGET,
    T_PANEL, M_SHAPE_AREA, M_SHAPE_SCALE, M_CIRCLE_CTOR, M_CIRCLE_AREA,
    M_CIRCLE_SCALE, M_BASE_DESCRIBE, M_SVC_AREA, M_SVC_SCALE, M_SVC_PROCESS,
    M_SVC_RENDER, M_SVC_EMIT, M_REPORT_RENDER, M_SESSION_START, M_FORMAT,
    M_WIDGET_PAINT, M_CONSUMER_MOUNT, M_PANEL_MOUNT,
}
_JS_NODE_KEYS = {T_TRACKER, M_TRACKER_CTOR, M_TRACKER_TRACK, M_TRACKER_FLUSH,
                 M_UTIL_HELPER, M_APP_MAIN}


@pytest.fixture(scope="module")
def graph() -> dict:
    out = Graph_out(extensions=[])
    jsts_extractor.extract(SNIPPETS, out)
    return out.to_json()


def _node_keys(payload: dict) -> set[str]:
    return {n["key"] for n in payload["nodes"]}


def _edges(payload: dict, label: str) -> list[tuple[str, str, dict]]:
    return [(e["srcKey"], e["dstKey"], e["props"]) for e in payload["edges"]
            if e["label"] == label]


# ---- nodes ----

def test_file_and_type_keys(graph):
    keys = _node_keys(graph)
    files = {F_SHAPES, F_BASE, F_SERVICE, F_REPORT, F_FORMAT, F_WIDGET,
             F_BARREL, F_CONSUMER, F_PANEL, F_UTIL, F_APP}
    types = {T_SHAPE, T_CIRCLE, T_BASE, T_SERVICE, T_REPORT, T_SESSION,
             T_WIDGET, T_PANEL, T_TRACKER}
    assert files <= keys
    assert types <= keys


def test_method_keys_param_normalization(graph):
    keys = _node_keys(graph)
    expected = {M_SHAPE_AREA, M_SHAPE_SCALE, M_CIRCLE_CTOR, M_CIRCLE_AREA,
                M_CIRCLE_SCALE, M_BASE_DESCRIBE, M_SVC_AREA, M_SVC_SCALE,
                M_SVC_PROCESS, M_SVC_RENDER, M_SVC_EMIT, M_REPORT_RENDER,
                M_SESSION_START, M_FORMAT, M_WIDGET_PAINT, M_CONSUMER_MOUNT,
                M_PANEL_MOUNT, M_TRACKER_CTOR, M_TRACKER_TRACK,
                M_TRACKER_FLUSH, M_UTIL_HELPER, M_APP_MAIN}
    assert expected <= keys


def test_default_export_type_key_uses_declared_name(graph):
    # `export default class Session` -> type:report::Session (not ::default)
    keys = _node_keys(graph)
    assert T_SESSION in keys
    contains = {(s, d) for s, d, _ in _edges(graph, "CONTAINS")}
    assert (F_REPORT, T_SESSION) in contains


def test_lang_prop_by_file_language(graph):
    for n in graph["nodes"]:
        lang = n["props"].get("lang")
        if n["label"] == "File":
            assert n["props"].get("language") == lang, n["key"]
            expected = ("typescript" if n["key"].endswith((".ts", ".tsx"))
                        else "javascript")
        elif n["key"] in _JS_NODE_KEYS:
            expected = "javascript"
        else:
            expected = "typescript"
        assert lang == expected, n["key"]


def test_contains_edges(graph):
    edges = {(s, d) for s, d, _ in _edges(graph, "CONTAINS")}
    assert (F_SHAPES, T_SHAPE) in edges
    assert (F_SHAPES, T_CIRCLE) in edges
    assert (F_SERVICE, T_SERVICE) in edges
    assert (F_UTIL, T_TRACKER) in edges
    assert (F_PANEL, T_PANEL) in edges


# ---- type-level edges ----

def test_extends_declared(graph):
    edges = _edges(graph, "EXTENDS")
    assert (T_SERVICE, T_BASE, {"kind": "declared"}) in edges


def test_implements_declared(graph):
    edges = _edges(graph, "IMPLEMENTS")
    assert (T_CIRCLE, T_SHAPE, {"kind": "declared"}) in edges
    assert (T_SERVICE, T_SHAPE, {"kind": "declared"}) in edges


# ---- CALLS (spec §6 ladder + §3.1 two-pass) ----

def test_this_call_direct(graph):
    calls = {(s, d, p["kind"]) for s, d, p in _edges(graph, "CALLS")}
    assert (M_CIRCLE_SCALE, M_CIRCLE_AREA, "direct") in calls
    assert (M_SVC_SCALE, M_SVC_AREA, "direct") in calls


def test_this_call_through_extends_chain(graph):
    calls = {(s, d) for s, d, _ in _edges(graph, "CALLS")}
    # this.describe() declared on Base, reached through the extends chain
    assert (M_SVC_SCALE, M_BASE_DESCRIBE) in calls


def test_js_this_call_direct(graph):
    calls = {(s, d, p["kind"]) for s, d, p in _edges(graph, "CALLS")}
    assert (M_TRACKER_TRACK, M_TRACKER_FLUSH, "direct") in calls


def test_annotated_class_receiver_direct(graph):
    calls = {(s, d, p["kind"]) for s, d, p in _edges(graph, "CALLS")}
    # render(circle: Circle) -> circle.area() — concrete receiver type
    assert (M_SVC_RENDER, M_CIRCLE_AREA, "direct") in calls


def test_interface_receiver_virtual_dispatch(graph):
    calls = {(s, d, p["kind"]) for s, d, p in _edges(graph, "CALLS")}
    # process(shape: Shape) -> shape.scale(): interface receiver
    assert (M_SVC_PROCESS, M_SHAPE_SCALE, "virtual-dispatch") in calls
    # emit(report: Report) -> report.render(): import-type'd interface
    assert (M_SVC_EMIT, M_REPORT_RENDER, "virtual-dispatch") in calls


def test_module_function_calls(graph):
    calls = {(s, d) for s, d, _ in _edges(graph, "CALLS")}
    assert (M_SVC_PROCESS, M_FORMAT) in calls       # same-run cross-file TS
    assert (M_APP_MAIN, M_UTIL_HELPER) in calls     # JS require binding


def test_builtin_calls_filtered_never_resolve(graph):
    calls = {(s, d) for s, d, _ in _edges(graph, "CALLS")}
    refs = {(s, d) for s, d, _ in _edges(graph, "REFERENCES")}
    assert not any("parseInt" in d for _, d in calls | refs)
    assert not any("console" in d for _, d in calls | refs)
    # parseInt/console.log filtered silently; only mystery() is unresolved
    assert graph["meta"]["structure"]["unresolved_refs"] == 1


def test_unknown_js_free_call_counted(graph):
    assert graph["meta"]["structure"]["unresolved_refs"] == 1


def test_calls_kind_props(graph):
    for _, _, props in _edges(graph, "CALLS"):
        assert props.get("kind") in ("direct", "virtual-dispatch")
        assert isinstance(props.get("line"), int)


# ---- REFERENCES (Method→Type) ----

def test_references_new_and_annotations(graph):
    refs = {(s, d) for s, d, _ in _edges(graph, "REFERENCES")}
    assert refs == {
        (M_SVC_PROCESS, T_SHAPE),   # param annotation shape: Shape
        (M_SVC_RENDER, T_CIRCLE),   # param annotation circle: Circle
        (M_SVC_EMIT, T_REPORT),     # import-type'd annotation report: Report
        (M_PANEL_MOUNT, T_CIRCLE),  # new Circle() in a .tsx file
        (M_APP_MAIN, T_TRACKER),    # new Tracker() via require binding
    }


# ---- IMPORTS (File→File, spec §5 jsts row) ----

def test_imports_edges(graph):
    imports = {(s, d) for s, d, _ in _edges(graph, "IMPORTS")}
    assert imports == {
        (F_SERVICE, F_SHAPES),
        (F_SERVICE, F_BASE),
        (F_SERVICE, F_FORMAT),
        (F_BARREL, F_WIDGET),   # re-export, one hop
        (F_CONSUMER, F_BARREL),
        (F_PANEL, F_SHAPES),
        (F_APP, F_UTIL),        # JS require, relative
    }


def test_import_type_no_imports_edge(graph):
    imports = {(s, d) for s, d, _ in _edges(graph, "IMPORTS")}
    assert (F_SERVICE, F_REPORT) not in imports  # import type: REFERENCES only


def test_reexport_two_hop_dropped_and_counted(graph):
    # consumer imports Widget through the barrel: the symbol binding needs a
    # second hop (barrel re-exports from widget) -> dropped+counted (§12).
    assert graph["meta"]["structure"]["dropped_edges"] == 3


def test_bare_import_dropped_and_counted(graph):
    # "lodash-es" (TS) and require("left-pad") (JS): both bare npm specifiers.
    # dropped_edges == 2 (bare) + 1 (two-hop re-export binding) — exactness
    # is asserted here together with the re-export case above.
    assert graph["meta"]["structure"]["dropped_edges"] >= 2


# ---- IMPORTS counting (spec §5 rev3 addendum) ----

def test_import_unknown_name_counted_default_never(tmp_path):
    """Relative specifier resolving to an in-repo file whose export table
    lacks the imported name -> unresolved_refs +1 per name (rev3 ruling);
    a default import never names an export-table entry -> never counted.
    The file-level IMPORTS edges stay (the dependency is real)."""
    repo = tmp_path / "repo"
    repo.mkdir()
    (repo / "mod.ts").write_text(
        "export class Klass {}\n"
        "export function fn(): void {}\n", encoding="utf-8")
    (repo / "a.ts").write_text(
        "import { Klass, ghost } from \"./mod\";\n"
        "export function ua(): void {\n  const k = new Klass();\n}\n",
        encoding="utf-8")
    (repo / "b.ts").write_text(
        "import { phantom } from \"./mod\";\n", encoding="utf-8")
    (repo / "d.ts").write_text(
        "import def from \"./mod\";\n", encoding="utf-8")
    out = Graph_out(extensions=[])
    jsts_extractor.extract(repo, out)
    graph = out.to_json()
    st = graph["meta"]["structure"]
    assert st["unresolved_refs"] == 2   # ghost + phantom; default import never
    assert st["dropped_edges"] == 0
    imports = {(s, d) for s, d, _ in _edges(graph, "IMPORTS")}
    assert imports == {
        ("file:a.ts", "file:mod.ts"),
        ("file:b.ts", "file:mod.ts"),
        ("file:d.ts", "file:mod.ts"),
    }


# ---- meta (spec §8) ----

def test_meta_languages_and_by_lang(graph):
    meta = graph["meta"]
    assert meta["languages"] == ["javascript", "typescript"]
    by = meta["structure"]["by_lang"]
    assert by["typescript"] == {"files": 9, "types": 8, "methods": 17,
                                "call_edges": 8}
    assert by["javascript"] == {"files": 2, "types": 1, "methods": 5,
                                "call_edges": 2}


def test_meta_honest_counters_exact(graph):
    st = graph["meta"]["structure"]
    assert st["files"] == 11
    assert st["types"] == 9
    assert st["methods"] == 22
    assert st["call_edges"] == 10
    assert st["dropped_edges"] == 3
    assert st["unresolved_refs"] == 1
    assert st["key_collisions"] == 0
    assert st["import_edges"] == 7


def test_meta_aggregates_are_sums(graph):
    st = graph["meta"]["structure"]
    by = st["by_lang"]
    assert st["files"] == by["typescript"]["files"] + by["javascript"]["files"]
    assert st["methods"] == (by["typescript"]["methods"]
                             + by["javascript"]["methods"])


# ---- analyzer dispatch (langs.py registration + --languages wiring) ----

def test_analyzer_dispatch(tmp_path):
    import analyzer as analyzer_mod
    out_json = tmp_path / "jsts.json"
    rc = analyzer_mod.main([str(SNIPPETS), str(out_json),
                            "--languages", "typescript", "javascript"])
    assert rc == 0
    payload = json.loads(out_json.read_text())
    assert payload["meta"]["languages"] == ["javascript", "typescript"]
    assert payload["meta"]["structure"]["by_lang"]["typescript"]["files"] == 9
    assert payload["meta"]["structure"]["by_lang"]["javascript"]["files"] == 2
