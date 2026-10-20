"""Go extractor tests — spec 2026-10-20 §4/§5/§5.1/§6 (TDD, RED first).

The snippets tree is a Go mini-repo (module example.com/shapes) covering:
package-path keys, value/pointer receivers, interface method nodes,
embedded-interface union, struct embed EXTENDS + promotion, a type-set
constraint (REFERENCES only), method-set IMPLEMENTS (value / pointer /
promotion-earned / param-mismatch / external-embed honest miss), the CALLS
ladder (var/param/field/interface receivers, pkg.New() inference, imported
package functions, same-package cross-file, predeclared bare-call filter),
IMPORTS with identifier-use matching, and lang/by_lang meta.
"""
from __future__ import annotations

import json
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from structure import go_extractor  # noqa: E402
from structure.model import Graph_out  # noqa: E402

SNIPPETS = Path(__file__).resolve().parent / "snippets" / "go"

# ---- key scheme (spec §4 go row: type:<pkg-path>.<Name>, params as written) ----

F_BASE = "file:base/base.go"
F_IFACE = "file:iface/iface.go"
F_IMPL = "file:impl/impl.go"
F_PTR = "file:impl/ptr.go"
F_PROMO = "file:impl/promo.go"
F_EXTERNAL = "file:impl/external.go"
F_APP = "file:app/app.go"
F_USE = "file:app/use.go"

T_BASE = "type:base.Base"
T_READER = "type:iface.Reader"
T_WRITER = "type:iface.Writer"
T_STORE = "type:iface.Store"
T_INCREMENTER = "type:iface.Incrementer"
T_RUNNER = "type:iface.Runner"
T_LOCKER = "type:iface.Locker"
T_CHOOSER = "type:iface.Chooser"
T_CONSTRAINT = "type:iface.Constraint"
T_MEMORY = "type:impl.Memory"
T_COUNTER = "type:impl.Counter"
T_WRAPPED = "type:impl.Wrapped"
T_GUARD = "type:impl.Guard"
T_SERVICE = "type:app.Service"

M_BASE_HELLO = "fn:base.Base#Hello()"
M_BASE_RUN = "fn:base.Base#Run()"
M_IFACE_GET = "fn:iface.Reader#Get(context.Context,string)"
M_IFACE_PUT = "fn:iface.Writer#Put(context.Context,string,string)"
M_STORE_GET = "fn:iface.Store#Get(context.Context,string)"
M_STORE_PUT = "fn:iface.Store#Put(context.Context,string,string)"
M_IFACE_INC = "fn:iface.Incrementer#Inc()"
M_IFACE_RUN = "fn:iface.Runner#Run()"
M_LOCK = "fn:iface.Locker#Lock()"
M_UNLOCK = "fn:iface.Locker#Unlock()"
M_IFACE_CHOOSE = "fn:iface.Chooser#Choose(int)"
M_NEW_MEMORY = "fn:impl#NewMemory()"
M_MEM_GET = "fn:impl.Memory#Get(context.Context,string)"
M_MEM_PUT = "fn:impl.Memory#Put(context.Context,string,string)"
M_MEM_RECENT = "fn:impl.Memory#Recent()"
M_MEM_CHOOSE = "fn:impl.Memory#Choose(string)"
M_COUNTER_INC = "fn:impl.Counter#Inc()"
M_WRAPPED_GREET = "fn:impl.Wrapped#Greet()"
M_DISPATCH = "fn:app#Dispatch(iface.Store)"
M_REFRESH = "fn:app.Service#Refresh()"
M_VAR_RECV = "fn:app#VarReceiver()"
M_NEW_INFER = "fn:app#NewInfer()"
M_PKG_CALL = "fn:app#PackageCall()"
M_DIRECT_PARAM = "fn:app#DirectParam(impl.Memory)"
M_LITERAL = "fn:app#Literal()"
M_UNKNOWN = "fn:app#Unknown()"
M_SAME_PKG = "fn:app#SamePkg()"
M_HELPER = "fn:app#Helper()"
M_BUMP = "fn:app#Bump()"

_ALL_METHODS = {
    M_BASE_HELLO, M_BASE_RUN, M_IFACE_GET, M_IFACE_PUT, M_STORE_GET,
    M_STORE_PUT, M_IFACE_INC, M_IFACE_RUN, M_LOCK, M_UNLOCK, M_IFACE_CHOOSE,
    M_NEW_MEMORY, M_MEM_GET, M_MEM_PUT, M_MEM_RECENT, M_MEM_CHOOSE,
    M_COUNTER_INC, M_WRAPPED_GREET, M_DISPATCH, M_REFRESH, M_VAR_RECV,
    M_NEW_INFER, M_PKG_CALL, M_DIRECT_PARAM, M_LITERAL, M_UNKNOWN,
    M_SAME_PKG, M_HELPER, M_BUMP,
}
_ALL_TYPES = {T_BASE, T_READER, T_WRITER, T_STORE, T_INCREMENTER, T_RUNNER,
              T_LOCKER, T_CHOOSER, T_CONSTRAINT, T_MEMORY, T_COUNTER,
              T_WRAPPED, T_GUARD, T_SERVICE}
_ALL_FILES = {F_BASE, F_IFACE, F_IMPL, F_PTR, F_PROMO, F_EXTERNAL, F_APP, F_USE}


@pytest.fixture(scope="module")
def graph() -> dict:
    out = Graph_out(extensions=[])
    go_extractor.extract(SNIPPETS, out)
    return out.to_json()


def _node_keys(payload: dict) -> set[str]:
    return {n["key"] for n in payload["nodes"]}


def _edges(payload: dict, label: str) -> list[tuple[str, str, dict]]:
    return [(e["srcKey"], e["dstKey"], e["props"]) for e in payload["edges"]
            if e["label"] == label]


def _node(payload: dict, key: str) -> dict:
    return next(n for n in payload["nodes"] if n["key"] == key)


# ---- nodes ----

def test_file_and_type_keys(graph):
    keys = _node_keys(graph)
    assert _ALL_FILES <= keys
    assert _ALL_TYPES <= keys
    assert "file:go.mod" not in keys  # not a Go source file


def test_method_keys_params_as_written(graph):
    keys = _node_keys(graph)
    assert _ALL_METHODS <= keys


def test_lang_prop_on_every_node(graph):
    for n in graph["nodes"]:
        if n["label"] == "File":
            assert n["props"].get("language") == "go", n["key"]
        assert n["props"].get("lang") == "go", n["key"]


def test_receiver_method_owner_and_pointer_prop(graph):
    inc = _node(graph, M_COUNTER_INC)["props"]
    assert inc["owner"] == T_COUNTER
    assert inc["receiver"] == "pointer"      # func (c *Counter) Inc
    get = _node(graph, M_MEM_GET)["props"]
    assert get["owner"] == T_MEMORY
    assert get["receiver"] == "pointer"      # func (m *Memory) Get
    hello = _node(graph, M_BASE_HELLO)["props"]
    assert hello["owner"] == T_BASE
    assert hello["receiver"] == "value"      # func (b Base) Hello


def test_package_function_owner_is_file(graph):
    assert _node(graph, M_NEW_MEMORY)["props"]["owner"] == F_IMPL
    assert _node(graph, M_HELPER)["props"]["owner"] == F_USE


def test_interface_method_nodes(graph):
    get = _node(graph, M_IFACE_GET)["props"]
    assert get["owner"] == T_READER
    assert "receiver" not in get            # interface specs carry no receiver
    put = _node(graph, M_IFACE_PUT)["props"]
    assert put["owner"] == T_WRITER


def test_embedded_interface_expansion_nodes(graph):
    # Store embeds Reader + Writer: the union is materialized as Method nodes
    # owned by Store so virtual dispatch through Store resolves (§3.1.4).
    assert _node(graph, M_STORE_GET)["props"]["owner"] == T_STORE
    assert _node(graph, M_STORE_PUT)["props"]["owner"] == T_STORE


def test_type_kinds(graph):
    assert _node(graph, T_MEMORY)["props"]["kind"] == "struct"
    assert _node(graph, T_STORE)["props"]["kind"] == "interface"
    assert _node(graph, T_READER)["props"]["kind"] == "interface"


def test_visibility_from_case(graph):
    assert _node(graph, M_BASE_HELLO)["props"]["visibility"] == "public"


def test_contains_edges(graph):
    edges = {(s, d) for s, d, _ in _edges(graph, "CONTAINS")}
    expected = {
        (F_BASE, T_BASE),
        (F_IFACE, T_READER), (F_IFACE, T_WRITER), (F_IFACE, T_STORE),
        (F_IFACE, T_INCREMENTER), (F_IFACE, T_RUNNER), (F_IFACE, T_LOCKER),
        (F_IFACE, T_CHOOSER), (F_IFACE, T_CONSTRAINT),
        (F_IMPL, T_MEMORY), (F_PTR, T_COUNTER), (F_PROMO, T_WRAPPED),
        (F_EXTERNAL, T_GUARD), (F_APP, T_SERVICE),
    }
    assert edges == expected


# ---- EXTENDS (spec §5 go column: struct embeds + interface embedding) ----

def test_extends_struct_embed_and_interface_embed(graph):
    edges = _edges(graph, "EXTENDS")
    assert (T_WRAPPED, T_BASE, {"kind": "declared"}) in edges   # struct embed
    assert (T_STORE, T_READER, {"kind": "declared"}) in edges   # iface embed
    assert (T_STORE, T_WRITER, {"kind": "declared"}) in edges
    assert len(edges) == 3


def test_external_embed_dropped_and_counted(graph):
    # Guard embeds sync.Mutex — no EXTENDS edge possible, honestly counted.
    extends = {(s, d) for s, d, _ in _edges(graph, "EXTENDS")}
    assert not any(s == T_GUARD for s, d in extends)
    assert graph["meta"]["structure"]["dropped_edges"] == 1


# ---- type-set constraints: REFERENCES only (spec §5 go column) ----

def test_typeset_constraint_references_only(graph):
    refs = {(s, d, p.get("context")) for s, d, p in _edges(graph, "REFERENCES")}
    assert (T_CONSTRAINT, T_READER, "type_constraint") in refs
    assert (T_CONSTRAINT, T_WRITER, "type_constraint") in refs
    # never a heritage edge from a type-set constraint
    for label in ("EXTENDS", "IMPLEMENTS"):
        assert not any(s == T_CONSTRAINT for s, d, _ in _edges(graph, label))
        assert not any(d == T_CONSTRAINT for s, d, _ in _edges(graph, label))


# ---- IMPLEMENTS via method-set matching (spec §5.1) ----

def _implements(graph) -> set[tuple[str, str, str, str]]:
    return {(s, d, p["kind"], p["receiver"])
            for s, d, p in _edges(graph, "IMPLEMENTS")}


def test_method_set_pointer_receiver_full_coverage(graph):
    # Memory's methods are all (m *Memory): only *Memory satisfies the
    # interfaces — the edge says so (§5.1.3).
    impls = _implements(graph)
    assert (T_MEMORY, T_READER, "method-set", "pointer") in impls
    assert (T_MEMORY, T_WRITER, "method-set", "pointer") in impls


def test_method_set_value_receiver_full_coverage(graph):
    # Base.Run is a value receiver: Base itself satisfies Runner.
    assert (T_BASE, T_RUNNER, "method-set", "value") in _implements(graph)


def test_method_set_embedded_interface_union(graph):
    # Store = Reader ∪ Writer; *Memory covers the union (pointer receivers).
    assert (T_MEMORY, T_STORE, "method-set", "pointer") in _implements(graph)


def test_method_set_pointer_receiver_only(graph):
    # Counter.Inc is pointer-receiver: only *Counter satisfies Incrementer.
    assert (T_COUNTER, T_INCREMENTER, "method-set", "pointer") in _implements(graph)


def test_method_set_promotion_earned(graph):
    impls = _implements(graph)
    assert (T_BASE, T_RUNNER, "method-set", "value") in impls      # own method
    assert (T_WRAPPED, T_RUNNER, "method-set", "value") in impls   # promoted


def test_method_set_param_simple_name_mismatch_no_edge(graph):
    # iface.Chooser.Choose(int) vs impl.Memory.Choose(string): no edge.
    assert not any(d == T_CHOOSER for _, d, _, _ in _implements(graph))


def test_method_set_exact_multiset(graph):
    assert _implements(graph) == {
        (T_MEMORY, T_READER, "method-set", "pointer"),
        (T_MEMORY, T_WRITER, "method-set", "pointer"),
        (T_MEMORY, T_STORE, "method-set", "pointer"),
        (T_COUNTER, T_INCREMENTER, "method-set", "pointer"),
        (T_BASE, T_RUNNER, "method-set", "value"),
        (T_WRAPPED, T_RUNNER, "method-set", "value"),
    }


def test_external_embed_unverifiable_counted(graph):
    # Guard embeds sync.Mutex: coverage against every non-type-set interface
    # it does not provably satisfy is unverifiable -> counted, never guessed.
    assert not any(s == T_GUARD for s, _, _, _ in _implements(graph))
    st = graph["meta"]["structure"]
    assert st["go_unverifiable_satisfaction"] == 7   # Guard x 7 interfaces


# ---- CALLS (spec §6 go ladder) ----

def test_calls_interface_receiver_virtual_dispatch(graph):
    calls = {(s, d, p["kind"]) for s, d, p in _edges(graph, "CALLS")}
    assert (M_DISPATCH, M_STORE_GET, "virtual-dispatch") in calls


def test_calls_concrete_param_receiver_direct(graph):
    calls = {(s, d, p["kind"]) for s, d, p in _edges(graph, "CALLS")}
    assert (M_DIRECT_PARAM, M_MEM_RECENT, "direct") in calls


def test_calls_field_receiver_direct(graph):
    calls = {(s, d, p["kind"]) for s, d, p in _edges(graph, "CALLS")}
    assert (M_REFRESH, M_MEM_RECENT, "direct") in calls


def test_calls_var_declared_receiver_promoted_method(graph):
    calls = {(s, d) for s, d, _ in _edges(graph, "CALLS")}
    assert (M_VAR_RECV, M_BASE_HELLO) in calls    # var w impl.Wrapped; w.Hello()


def test_calls_new_inference(graph):
    calls = {(s, d) for s, d, _ in _edges(graph, "CALLS")}
    assert (M_NEW_INFER, M_NEW_MEMORY) in calls   # impl.NewMemory() itself
    assert (M_NEW_INFER, M_MEM_PUT) in calls      # mem inferred as Memory


def test_calls_imported_package_function(graph):
    calls = {(s, d) for s, d, _ in _edges(graph, "CALLS")}
    assert (M_PKG_CALL, M_NEW_MEMORY) in calls


def test_calls_same_package_cross_file(graph):
    calls = {(s, d) for s, d, _ in _edges(graph, "CALLS")}
    assert (M_SAME_PKG, M_HELPER) in calls


def test_calls_promoted_method_on_own_type(graph):
    calls = {(s, d) for s, d, _ in _edges(graph, "CALLS")}
    assert (M_WRAPPED_GREET, M_BASE_HELLO) in calls


def test_calls_var_receiver_concrete(graph):
    calls = {(s, d) for s, d, _ in _edges(graph, "CALLS")}
    assert (M_BUMP, M_COUNTER_INC) in calls       # var c impl.Counter; c.Inc()


def test_calls_exact_multiset(graph):
    calls = {(s, d, p["kind"]) for s, d, p in _edges(graph, "CALLS")}
    assert calls == {
        (M_DISPATCH, M_STORE_GET, "virtual-dispatch"),
        (M_DIRECT_PARAM, M_MEM_RECENT, "direct"),
        (M_REFRESH, M_MEM_RECENT, "direct"),
        (M_VAR_RECV, M_BASE_HELLO, "direct"),
        (M_NEW_INFER, M_NEW_MEMORY, "direct"),
        (M_NEW_INFER, M_MEM_PUT, "direct"),
        (M_PKG_CALL, M_NEW_MEMORY, "direct"),
        (M_SAME_PKG, M_HELPER, "direct"),
        (M_WRAPPED_GREET, M_BASE_HELLO, "direct"),
        (M_BUMP, M_COUNTER_INC, "direct"),
    }


def test_bare_predeclared_funcs_filtered(graph):
    # make/len/append inside Memory.Recent: no edges, no counts (§3.1.3).
    calls = {(s, d) for s, d, _ in _edges(graph, "CALLS")}
    assert not any("append" in d or "#len" in d or "make" in d
                   for _, d in calls)
    # only mystery() + context.Background() x2 are unresolved
    assert graph["meta"]["structure"]["unresolved_refs"] == 3


def test_calls_kind_props(graph):
    for _, _, props in _edges(graph, "CALLS"):
        assert props.get("kind") in ("direct", "virtual-dispatch")
        assert isinstance(props.get("line"), int)


# ---- REFERENCES (Method→Type for literals/signatures, Type→Type constraints) ----

def test_references_composite_literal_and_signatures(graph):
    refs = {(s, d) for s, d, p in _edges(graph, "REFERENCES")
            if p.get("context") != "type_constraint"}
    assert (M_NEW_MEMORY, T_MEMORY) in refs       # result + &Memory{} literal
    assert (M_LITERAL, T_MEMORY) in refs          # result + impl.Memory{} literal
    assert (M_DISPATCH, T_STORE) in refs          # param s iface.Store
    assert (M_DIRECT_PARAM, T_MEMORY) in refs     # param m impl.Memory
    assert (M_PKG_CALL, T_MEMORY) in refs         # result *impl.Memory
    assert (T_SERVICE, T_MEMORY) in refs          # field inner impl.Memory


def test_references_external_types_skipped(graph):
    # context.Context params, sync.Mutex fields, []string results: no edges.
    refs = {(s, d) for s, d, _ in _edges(graph, "REFERENCES")}
    assert not any("Context" in d or "Mutex" in d for _, d in refs)


# ---- IMPORTS (File→File, identifier-use matching, spec §5 go column) ----

def test_imports_identifier_use_matching(graph):
    imports = {(s, d) for s, d, _ in _edges(graph, "IMPORTS")}
    assert imports == {
        (F_APP, F_IFACE),     # app.go uses iface.Store
        (F_APP, F_IMPL),      # app.go uses impl.Memory / impl.NewMemory
        (F_APP, F_PROMO),     # app.go uses impl.Wrapped
        (F_USE, F_PTR),       # use.go uses impl.Counter (not the blank base)
        (F_PROMO, F_BASE),    # promo.go embeds base.Base
    }


def test_imports_blank_import_no_edge_no_count(graph):
    # use.go blank-imports base: no identifier use possible -> no edge, and
    # per the task ruling (spec §5 go column) not counted as a drop.
    st = graph["meta"]["structure"]
    assert st["dropped_edges"] == 1   # only Guard's external embed


def test_imports_identifier_use_miss_counted(tmp_path):
    """In-repo import whose package declares none of the used identifiers
    -> no edge for those uses + unresolved_refs +1 per missed identifier
    (rev3 ruling); identifiers the package does declare still earn edges."""
    repo = tmp_path / "repo"
    repo.mkdir()
    (repo / "go.mod").write_text("module example.com/miss\n", encoding="utf-8")
    (repo / "util").mkdir()
    (repo / "util" / "util.go").write_text(
        "package util\n\nfunc Real() int { return 1 }\n", encoding="utf-8")
    (repo / "main.go").write_text(
        "package main\n\nimport \"example.com/miss/util\"\n\n"
        "var _ = util.Phantom\n\nfunc main() {\n\t_ = util.Real()\n"
        "\t_ = util.Ghost\n}\n", encoding="utf-8")
    out = Graph_out(extensions=[])
    go_extractor.extract(repo, out)
    graph = out.to_json()
    st = graph["meta"]["structure"]
    assert st["unresolved_refs"] == 2     # Ghost + Phantom: in-repo misses
    imports = {(s, d) for s, d, _ in _edges(graph, "IMPORTS")}
    assert imports == {("file:main.go", "file:util/util.go")}


# ---- meta (spec §8) ----

def test_meta_languages_and_by_lang(graph):
    meta = graph["meta"]
    assert meta["languages"] == ["go"]
    by = meta["structure"]["by_lang"]["go"]
    assert by == {"files": 8, "types": 14, "methods": 29, "call_edges": 10}


def test_meta_aggregates_and_honest_counters(graph):
    st = graph["meta"]["structure"]
    assert st["files"] == 8
    assert st["types"] == 14
    assert st["methods"] == 29
    assert st["call_edges"] == 10
    assert st["import_edges"] == 5
    assert st["dropped_edges"] == 1
    assert st["unresolved_refs"] == 3
    assert st["key_collisions"] == 0
    assert st["go_unverifiable_satisfaction"] == 7


# ---- analyzer dispatch (langs.py registration + --languages wiring) ----

def test_analyzer_dispatch(tmp_path):
    import analyzer as analyzer_mod
    out_json = tmp_path / "go.json"
    rc = analyzer_mod.main([str(SNIPPETS), str(out_json), "--languages", "go"])
    assert rc == 0
    payload = json.loads(out_json.read_text())
    assert payload["meta"]["languages"] == ["go"]
    by = payload["meta"]["structure"]["by_lang"]["go"]
    assert by["files"] == 8
    assert by["types"] == 14
    assert by["methods"] == 29
