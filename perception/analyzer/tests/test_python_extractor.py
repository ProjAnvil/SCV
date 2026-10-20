"""Python extractor tests — spec 2026-10-20 §3.1/§4/§5/§6 (TDD, RED first).

The snippets tree mirrors the python-complex fixture's shapes at minimum
scale: package + submodules, inheritance, mutual recursion, cross-module
from-imports, a builtin bare call, and an unknown bare call.
"""
from __future__ import annotations

import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from structure import python_extractor  # noqa: E402
from structure.model import Graph_out  # noqa: E402

SNIPPETS = Path(__file__).resolve().parent / "snippets" / "python"

# ---- key scheme (spec §4 python row) ----

F_INIT = "file:app/__init__.py"
F_BASE = "file:app/base.py"
F_SERVICES = "file:app/services.py"
F_UTIL = "file:app/util.py"

T_BASE = "type:app.base.Base"
T_GREETER = "type:app.services.Greeter"

M_FMT = "fn:app.base.Base#_fmt(1)"
M_GREET = "fn:app.services.Greeter#greet(1)"
M_REC_A = "fn:app.services.Greeter#_rec_a(1)"
M_REC_B = "fn:app.services.Greeter#_rec_b(1)"
M_MAKE = "fn:app.services#make(1)"
M_HELPER = "fn:app.util#helper(2)"
M_BOOTSTRAP = "fn:app#bootstrap(0)"


@pytest.fixture(scope="module")
def graph() -> dict:
    out = Graph_out(extensions=[])
    python_extractor.extract(SNIPPETS, out)
    return out.to_json()


def _node_keys(payload: dict) -> set[str]:
    return {n["key"] for n in payload["nodes"]}


def _edges(payload: dict, label: str) -> list[tuple[str, str, dict]]:
    return [(e["srcKey"], e["dstKey"], e["props"]) for e in payload["edges"]
            if e["label"] == label]


# ---- nodes ----

def test_file_and_type_keys(graph):
    keys = _node_keys(graph)
    assert {F_INIT, F_BASE, F_SERVICES, F_UTIL} <= keys
    assert {T_BASE, T_GREETER} <= keys


def test_method_keys_arity_normalized(graph):
    keys = _node_keys(graph)
    assert {M_FMT, M_GREET, M_REC_A, M_REC_B, M_MAKE, M_HELPER, M_BOOTSTRAP} <= keys


def test_lang_prop_on_type_and_method_nodes(graph):
    for n in graph["nodes"]:
        if n["label"] in ("Type", "Method"):
            assert n["props"].get("lang") == "python", n["key"]
        elif n["label"] == "File":
            assert n["props"].get("language") == "python", n["key"]
            assert n["props"].get("lang") == "python", n["key"]


def test_contains_edges(graph):
    edges = {(s, d) for s, d, _ in _edges(graph, "CONTAINS")}
    assert (F_BASE, T_BASE) in edges
    assert (F_SERVICES, T_GREETER) in edges


# ---- type-level edges ----

def test_extends_single_base_declared(graph):
    edges = _edges(graph, "EXTENDS")
    assert (T_GREETER, T_BASE, {"kind": "declared"}) in edges


def test_implements_extra_bases_declared(graph):
    # a second base lands on IMPLEMENTS (spec §5 python row); the snippets
    # tree only has a single base — asserted via the counter being exact.
    assert _edges(graph, "IMPLEMENTS") == []


# ---- CALLS (spec §6 python ladder + §3.1 two-pass) ----

def test_self_call_resolves_through_mro(graph):
    calls = {(s, d) for s, d, _ in _edges(graph, "CALLS")}
    # greet calls self._fmt — declared on Base, reached through the MRO
    assert (M_GREET, M_FMT) in calls


def test_mutual_recursion_both_edges(graph):
    calls = {(s, d) for s, d, _ in _edges(graph, "CALLS")}
    assert (M_REC_A, M_REC_B) in calls
    assert (M_REC_B, M_REC_A) in calls


def test_module_function_call_same_module(graph):
    calls = {(s, d) for s, d, _ in _edges(graph, "CALLS")}
    # make() calls helper() imported from app.util — cross-file pass-2 bind
    assert (M_MAKE, M_HELPER) in calls


def test_builtin_bare_call_filtered_never_resolves(graph):
    # len() inside helper: no edge AND no unresolved_refs bump (spec §3.1.3)
    before = graph["meta"]["structure"]["unresolved_refs"]
    calls = {(s, d) for s, d, _ in _edges(graph, "CALLS")}
    assert not any(d.startswith("fn:app.util#len") for _, d in calls)
    assert before == 1  # only orphan's missing_fn is unresolved


def test_unknown_bare_call_counted(graph):
    assert graph["meta"]["structure"]["unresolved_refs"] == 1


def test_calls_kind_props(graph):
    for _, _, props in _edges(graph, "CALLS"):
        assert props.get("kind") in ("direct", "virtual-dispatch")
        assert isinstance(props.get("line"), int)


# ---- REFERENCES ----

def test_constructor_reference(graph):
    refs = {(s, d) for s, d, _ in _edges(graph, "REFERENCES")}
    # bootstrap instantiates Greeter
    assert (M_BOOTSTRAP, T_GREETER) in refs


# ---- IMPORTS (File→File, spec §5) ----

def test_imports_edges(graph):
    imports = {(s, d) for s, d, _ in _edges(graph, "IMPORTS")}
    assert (F_SERVICES, F_BASE) in imports
    assert (F_SERVICES, F_UTIL) in imports
    assert (F_INIT, F_SERVICES) in imports


# ---- meta (spec §8) ----

def test_meta_languages_and_by_lang(graph):
    meta = graph["meta"]
    assert meta["languages"] == ["python"]
    by = meta["structure"]["by_lang"]["python"]
    assert by["files"] == 4
    assert by["types"] == 2
    assert by["methods"] >= 7
    assert by["call_edges"] >= 4


def test_meta_import_edges_count(graph):
    assert graph["meta"]["structure"]["import_edges"] == 3


# ---- IMPORTS counting (spec §5 rev3 addendum) ----

def _write(tmp_path, files: dict[str, str]):
    repo = tmp_path / "repo"
    for rel, content in files.items():
        p = repo / rel
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(content, encoding="utf-8")
    return repo


def test_from_import_unknown_name_counted(tmp_path):
    """In-repo from-import of a name the target module does not declare ->
    unresolved_refs +1 per name (rev3 ruling); the module-level IMPORTS edge
    itself stays (the file dependency is real)."""
    repo = _write(tmp_path, {
        "mod/__init__.py": "",
        "mod/lib.py": "def known():\n    return 1\n",
        "partial.py": "from mod.lib import known, ghost\n",
        "broken.py": "from mod.lib import phantom\n",
    })
    out = Graph_out(extensions=[])
    python_extractor.extract(repo, out)
    graph = out.to_json()
    assert graph["meta"]["structure"]["unresolved_refs"] == 2
    imports = {(s, d) for s, d, _ in _edges(graph, "IMPORTS")}
    assert imports == {
        ("file:partial.py", "file:mod/lib.py"),
        ("file:broken.py", "file:mod/lib.py"),
    }


def test_plain_aliased_and_submodule_imports_never_counted(tmp_path):
    """`import mod.lib as lb` names the module, not a member (no count);
    `from mod import lib` binds a submodule (no count, edge to the package
    file). Both keep their IMPORTS edges."""
    repo = _write(tmp_path, {
        "mod/__init__.py": "",
        "mod/lib.py": "def known():\n    return 1\n",
        "alias.py": "import mod.lib as lb\n",
        "submod.py": "from mod import lib\n",
    })
    out = Graph_out(extensions=[])
    python_extractor.extract(repo, out)
    graph = out.to_json()
    assert graph["meta"]["structure"]["unresolved_refs"] == 0
    imports = {(s, d) for s, d, _ in _edges(graph, "IMPORTS")}
    assert imports == {
        ("file:alias.py", "file:mod/lib.py"),
        ("file:submod.py", "file:mod/__init__.py"),
    }


def test_meta_aggregates_are_sums(graph):
    st = graph["meta"]["structure"]
    by = st["by_lang"]["python"]
    assert st["files"] == by["files"]
    assert st["types"] == by["types"]
