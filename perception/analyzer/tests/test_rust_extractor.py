"""Rust extractor tests — spec 2026-10-20 §4/§5/§6 (TDD, RED first).

The snippets tree is a Rust mini-crate (name ``store``) covering: crate root
discovery (lib.rs preferred over main.rs; main.rs items conservatively at the
crate root with empty module prefix), the mod tree (``foo.rs`` and
``foo/mod.rs`` forms + an inline ``mod policy { .. }``), the
``type:crate::module::Item`` key scheme, trait method nodes, ``impl Trait for
Type`` IMPLEMENTS (kind: declared), inherent impl method ownership, the CALLS
ladder (use-resolved + fully-qualified paths, ``self.method()``,
``self.field.method()``, let-inferred receivers, ``dyn Trait`` ->
virtual-dispatch), use resolution -> CALLS + REFERENCES + IMPORTS, macro
invocations dropped+counted, an external-trait impl dropped+counted, and the
lang/by_lang meta.
"""
from __future__ import annotations

import json
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from structure import rust_extractor  # noqa: E402
from structure.model import Graph_out  # noqa: E402

SNIPPETS = Path(__file__).resolve().parent / "snippets" / "rust"

# ---- key scheme (spec §4 rust row: type:<crate::module::Item>,
# params as written, self excluded) ----

F_LIB = "file:src/lib.rs"
F_MAIN = "file:src/main.rs"
F_CACHE = "file:src/cache.rs"
F_SHAPES = "file:src/shapes/mod.rs"
F_CIRCLE = "file:src/shapes/circle.rs"

T_STORE = "type:store::Store"
T_REGISTRY = "type:store::Registry"
T_SHAPE = "type:store::shapes::Shape"
T_CIRCLE = "type:store::shapes::circle::Circle"
T_CACHE = "type:store::cache::Cache"
T_MODE = "type:store::cache::Mode"
T_HIT = "type:store::cache::Hit"
T_POLICY = "type:store::cache::policy::Policy"

M_STORE_GET = "fn:store::Store#get(&str)"
M_STORE_PUT = "fn:store::Store#put(&str,&str)"
M_REG_NEW = "fn:store::Registry#new()"
M_REG_REFRESH = "fn:store::Registry#refresh(&str)"
M_REG_GET = "fn:store::Registry#get(&str)"
M_REG_PUT = "fn:store::Registry#put(&str,&str)"
M_SHAPE_AREA = "fn:store::shapes::Shape#area()"
M_CIRCLE_NEW = "fn:store::shapes::circle::Circle#new(f64)"
M_CIRCLE_AREA = "fn:store::shapes::circle::Circle#area()"
M_CACHE_NEW = "fn:store::cache::Cache#new()"
M_CACHE_HIT = "fn:store::cache::Cache#hit(&str)"
M_CACHE_STORE = "fn:store::cache::Cache#store(&str,&str)"
M_CACHE_LOAD = "fn:store::cache#load(f64)"
M_LOOKUP = "fn:store#lookup(&dyn Store,&str)"
M_DESCRIBE = "fn:store#describe()"
M_MAIN = "fn:store#main()"

_ALL_METHODS = {
    M_STORE_GET, M_STORE_PUT, M_REG_NEW, M_REG_REFRESH, M_REG_GET, M_REG_PUT,
    M_SHAPE_AREA, M_CIRCLE_NEW, M_CIRCLE_AREA, M_CACHE_NEW, M_CACHE_HIT,
    M_CACHE_STORE, M_CACHE_LOAD, M_LOOKUP, M_DESCRIBE, M_MAIN,
}
_ALL_TYPES = {T_STORE, T_REGISTRY, T_SHAPE, T_CIRCLE, T_CACHE, T_MODE,
              T_HIT, T_POLICY}
_ALL_FILES = {F_LIB, F_MAIN, F_CACHE, F_SHAPES, F_CIRCLE}


@pytest.fixture(scope="module")
def graph() -> dict:
    out = Graph_out(extensions=[])
    rust_extractor.extract(SNIPPETS, out)
    return out.to_json()


def _node_keys(payload: dict) -> set[str]:
    return {n["key"] for n in payload["nodes"]}


def _edges(payload: dict, label: str) -> list[tuple[str, str, dict]]:
    return [(e["srcKey"], e["dstKey"], e["props"]) for e in payload["edges"]
            if e["label"] == label]


def _node(payload: dict, key: str) -> dict:
    return next(n for n in payload["nodes"] if n["key"] == key)


# ---- crate root discovery + mod tree (spec §4 rust row) ----

def test_crate_root_lib_preferred_and_mod_tree(graph):
    # lib.rs is THE crate root: its `mod shapes;`/`mod cache;` declarations
    # drive the module walk, so module-declared types exist as nodes.
    keys = _node_keys(graph)
    assert _ALL_FILES <= keys
    assert _ALL_TYPES <= keys
    assert "file:Cargo.toml" not in keys       # not a Rust source file
    assert "file:src" not in keys


def test_mod_tree_both_file_forms(graph):
    # `mod shapes;` -> shapes/mod.rs (mod.rs form); `mod circle;` inside
    # shapes/mod.rs -> circle.rs (plain-file form); inline `mod policy`
    # in cache.rs -> nested module path within the same file.
    keys = _node_keys(graph)
    assert T_SHAPE in keys and T_CIRCLE in keys and T_POLICY in keys


def test_main_rs_items_at_crate_root_empty_prefix(graph):
    # main.rs exists alongside lib.rs: conservatively its items attach to
    # the crate root with an empty module prefix (documented decision).
    assert _node(graph, M_MAIN)["props"]["owner"] == F_MAIN


# ---- nodes ----

def test_method_keys_params_as_written(graph):
    assert _ALL_METHODS <= _node_keys(graph)


def test_lang_prop_on_every_node(graph):
    for n in graph["nodes"]:
        if n["label"] == "File":
            assert n["props"].get("language") == "rust", n["key"]
        assert n["props"].get("lang") == "rust", n["key"]


def test_type_kinds(graph):
    assert _node(graph, T_REGISTRY)["props"]["kind"] == "struct"
    assert _node(graph, T_MODE)["props"]["kind"] == "enum"
    assert _node(graph, T_STORE)["props"]["kind"] == "trait"
    assert _node(graph, T_HIT)["props"]["kind"] == "alias"
    assert _node(graph, T_SHAPE)["props"]["kind"] == "trait"


def test_trait_method_nodes_owned_by_trait(graph):
    # trait method signatures become Method nodes owned by the trait Type
    # (§3.1.4, the go/jsts interface-member precedent).
    get = _node(graph, M_STORE_GET)["props"]
    assert get["owner"] == T_STORE
    assert get["visibility"] == "public"      # pub trait members are public
    area = _node(graph, M_SHAPE_AREA)["props"]
    assert area["owner"] == T_SHAPE


def test_inherent_impl_method_ownership_and_props(graph):
    new = _node(graph, M_CIRCLE_NEW)["props"]
    assert new["owner"] == T_CIRCLE
    assert new["visibility"] == "public"
    assert new["static"] is True              # no self parameter
    assert new["params"] == "f64"
    assert new["returns"] == "Circle"
    area = _node(graph, M_CIRCLE_AREA)["props"]
    assert area["owner"] == T_CIRCLE
    assert area["static"] is False            # fn area(&self)


def test_impl_trait_methods_belong_to_type(graph):
    # methods of `impl Store for Registry` are Registry's Method nodes.
    get = _node(graph, M_REG_GET)["props"]
    assert get["owner"] == T_REGISTRY
    assert get["static"] is False


def test_free_function_owner_is_file(graph):
    assert _node(graph, M_LOOKUP)["props"]["owner"] == F_LIB
    assert _node(graph, M_CACHE_LOAD)["props"]["owner"] == F_CACHE
    assert _node(graph, M_MAIN)["props"]["visibility"] == "private"


def test_contains_edges_exact(graph):
    edges = {(s, d) for s, d, _ in _edges(graph, "CONTAINS")}
    assert edges == {
        (F_LIB, T_STORE), (F_LIB, T_REGISTRY),
        (F_SHAPES, T_SHAPE), (F_CIRCLE, T_CIRCLE),
        (F_CACHE, T_CACHE), (F_CACHE, T_MODE), (F_CACHE, T_HIT),
        (F_CACHE, T_POLICY),   # inline mod: CONTAINS from the physical file
    }


# ---- IMPLEMENTS (spec §5 rust column: fully explicit, kind: declared) ----

def test_implements_declared_exact(graph):
    edges = {(s, d, p.get("kind")) for s, d, p in _edges(graph, "IMPLEMENTS")}
    assert edges == {
        (T_REGISTRY, T_STORE, "declared"),    # impl Store for Registry
        (T_CIRCLE, T_SHAPE, "declared"),      # impl Shape for Circle
    }


def test_external_trait_impl_dropped_and_counted(graph):
    # impl std::fmt::Display for Cache: trait not in-source -> no edge, no
    # method nodes, honestly counted as one dropped edge.
    keys = _node_keys(graph)
    assert not any("fmt" in k for k in keys)
    assert graph["meta"]["structure"]["dropped_edges"] >= 1


# ---- CALLS (spec §6 rust ladder) ----

def test_calls_exact_multiset(graph):
    calls = {(s, d, p["kind"]) for s, d, p in _edges(graph, "CALLS")}
    assert calls == {
        (M_REG_NEW, M_CACHE_NEW, "direct"),        # cache::Cache::new()
        (M_REG_REFRESH, M_CACHE_HIT, "direct"),    # self.cache.hit(key)
        (M_REG_GET, M_REG_REFRESH, "direct"),      # self.refresh(key)
        (M_REG_PUT, M_CACHE_STORE, "direct"),      # self.cache.store(..)
        (M_LOOKUP, M_STORE_GET, "virtual-dispatch"),  # dyn Store receiver
        (M_DESCRIBE, M_CIRCLE_NEW, "direct"),      # use-resolved Circle::new
        (M_DESCRIBE, M_CIRCLE_AREA, "direct"),     # let-inferred receiver
        (M_DESCRIBE, M_CACHE_LOAD, "direct"),      # fully-qualified path
        (M_MAIN, M_REG_NEW, "direct"),             # use-resolved Registry::new
        (M_MAIN, M_REG_PUT, "direct"),             # let-inferred receiver
    }


def test_calls_kind_props(graph):
    for _, _, props in _edges(graph, "CALLS"):
        assert props.get("kind") in ("direct", "virtual-dispatch")
        assert isinstance(props.get("line"), int)


def test_macro_invocations_dropped_and_counted(graph):
    # vec![key] inside Cache::hit: no edge, one dropped edge per invocation.
    calls = {(s, d) for s, d, _ in _edges(graph, "CALLS")}
    assert not any("vec" in d.lower() or "println" in d.lower()
                   for _, d in calls)
    st = graph["meta"]["structure"]
    assert st["dropped_edges"] == 2   # vec! + the external Display impl


def test_unresolved_counted(graph):
    # mystery(key) in Cache::hit + String::new() in cache::load — the honest
    # floor (external/unresolvable callees are counted, never guessed).
    assert graph["meta"]["structure"]["unresolved_refs"] == 2


# ---- REFERENCES (spec §5 rust column: path-referenced types) ----

def test_references_signatures_fields_uses(graph):
    refs = {(s, d) for s, d, _ in _edges(graph, "REFERENCES")}
    assert (M_LOOKUP, T_STORE) in refs          # param &dyn Store
    assert (M_REG_NEW, T_REGISTRY) in refs      # return + struct literal
    assert (M_CIRCLE_NEW, T_CIRCLE) in refs
    assert (M_CACHE_NEW, T_CACHE) in refs
    assert (T_REGISTRY, T_CACHE) in refs        # field cache::Cache
    assert (F_LIB, T_CIRCLE) in refs            # use crate::...::Circle
    assert (F_CIRCLE, T_SHAPE) in refs          # use super::Shape
    assert (F_MAIN, T_REGISTRY) in refs         # use store::Registry


def test_references_external_types_skipped(graph):
    refs = {(s, d) for s, d, _ in _edges(graph, "REFERENCES")}
    assert not any("String" == d.rsplit("::", 1)[-1] or "fmt" in d
                   for _, d in refs)


# ---- IMPORTS (File→File, spec §5 rust column) ----

def test_imports_exact(graph):
    imports = {(s, d) for s, d, _ in _edges(graph, "IMPORTS")}
    assert imports == {
        (F_MAIN, F_LIB),      # use store::Registry -> src/lib.rs
        (F_LIB, F_CIRCLE),    # use crate::shapes::circle::Circle
        (F_CIRCLE, F_SHAPES), # use super::Shape -> shapes/mod.rs
    }


def test_imports_external_crate_no_edge_no_count(graph):
    # std::fmt use-like paths never produce edges nor drops; the only
    # dropped edges are the vec! macro + the external Display impl.
    st = graph["meta"]["structure"]
    assert st["dropped_edges"] == 2
    assert st["import_edges"] == 3


# ---- meta (spec §8) ----

def test_meta_languages_and_by_lang(graph):
    meta = graph["meta"]
    assert meta["languages"] == ["rust"]
    by = meta["structure"]["by_lang"]["rust"]
    assert by == {"files": 5, "types": 8, "methods": 16, "call_edges": 10}


def test_meta_aggregates_and_honest_counters(graph):
    st = graph["meta"]["structure"]
    assert st["files"] == 5
    assert st["types"] == 8
    assert st["methods"] == 16
    assert st["call_edges"] == 10
    assert st["import_edges"] == 3
    assert st["dropped_edges"] == 2
    assert st["unresolved_refs"] == 2
    assert st["key_collisions"] == 0


# ---- langs.py registration + analyzer dispatch ----

def test_langs_registration():
    from structure import langs
    assert "rust" in langs.known()
    files = langs.walk(SNIPPETS, "rust")
    assert {f.name for f in files} == {"lib.rs", "main.rs", "cache.rs",
                                       "mod.rs", "circle.rs"}


def test_analyzer_dispatch(tmp_path):
    import analyzer as analyzer_mod
    out_json = tmp_path / "rust.json"
    rc = analyzer_mod.main([str(SNIPPETS), str(out_json),
                            "--languages", "rust"])
    assert rc == 0
    payload = json.loads(out_json.read_text())
    assert payload["meta"]["languages"] == ["rust"]
    by = payload["meta"]["structure"]["by_lang"]["rust"]
    assert by == {"files": 5, "types": 8, "methods": 16, "call_edges": 10}
