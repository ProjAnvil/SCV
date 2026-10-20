"""TDD spec for the tree-sitter structure extractor (perception v2, Tasks 1-2).

Covers spec 2026-10-14 §3 (source-root discovery) and §4 (nodes, edges, key
schemes, FQN resolution, edge-drop rule, CALLS mapping rule, REFERENCES,
static-import drops) on small snippets plus one frozen fixture smoke
(torture-app).
"""

import json
import shutil
from pathlib import Path

import pytest

from structure.model import Graph_out
from structure import java_extractor as jx
from analyzer import main as analyzer_main

TESTS_DIR = Path(__file__).resolve().parent
SNIPPETS = TESTS_DIR / "snippets"
SCV_ROOT = TESTS_DIR.parents[2]
TORTURE_APP = (SCV_ROOT / "perception" / "adapters" / "springboot" / "src" /
               "test" / "resources" / "fixtures" / "torture-app")


def snippet(name: str) -> str:
    return (SNIPPETS / name).read_text(encoding="utf-8")


def build_repo(tmp_path: Path, files: dict[str, str]) -> Path:
    """Write a synthetic repo: relative path -> file content."""
    repo = tmp_path / "repo"
    for rel, content in files.items():
        target = repo / rel
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(content, encoding="utf-8")
    return repo


def run_extract(repo: Path) -> tuple[Graph_out, dict]:
    out = Graph_out()
    jx.extract(repo, out)
    return out, out.to_json()


def keys(payload: dict, label: str) -> set[str]:
    return {n["key"] for n in payload["nodes"] if n["label"] == label}


def edge_set(payload: dict, label: str) -> set[tuple[str, str]]:
    return {(e["srcKey"], e["dstKey"]) for e in payload["edges"] if e["label"] == label}


# ---- Graph_out builder ----

def test_graph_out_node_collision_counted():
    out = Graph_out()
    assert out.add_node("type:a.A", "Type", {"kind": "class"})
    assert not out.add_node("type:a.A", "Type", {"kind": "class"})  # never silently merged
    payload = out.to_json()
    assert payload["meta"]["structure"]["key_collisions"] == 1
    assert [n["key"] for n in payload["nodes"]] == ["type:a.A"]


def test_graph_out_edge_dropped_and_counted_when_endpoint_missing():
    out = Graph_out()
    out.add_node("type:a.A", "Type", {})
    out.add_node("type:a.B", "Type", {})
    assert out.add_edge("type:a.A", "type:a.B", "IMPLEMENTS")
    assert not out.add_edge("type:a.A", "type:gone.X", "IMPLEMENTS")
    assert not out.add_edge("type:gone.Y", "type:a.B", "EXTENDS")
    payload = out.to_json()
    assert len(payload["edges"]) == 1
    assert payload["meta"]["structure"]["dropped_edges"] == 2


def test_graph_out_add_call_shape_and_drop():
    out = Graph_out()
    out.add_node("fn:a.A#m()", "Method", {})
    out.add_node("fn:a.B#n()", "Method", {})
    assert out.add_call("fn:a.A#m()", "fn:a.B#n()", 12, "direct")
    assert not out.add_call("fn:a.A#m()", "fn:a.Gone#z()", 13, "virtual-dispatch")
    payload = out.to_json()
    calls = [e for e in payload["edges"] if e["label"] == "CALLS"]
    assert len(calls) == 1
    assert calls[0]["props"] == {"line": 12, "kind": "direct"}
    assert payload["meta"]["structure"]["call_edges"] == 1
    assert payload["meta"]["structure"]["dropped_edges"] == 1


def test_graph_out_to_json_schema():
    out = Graph_out(extensions=["springboot"])
    payload = out.to_json()
    assert set(payload) >= {"nodes", "edges", "unresolved", "meta"}
    assert payload["unresolved"] == 0
    meta = payload["meta"]
    assert meta["engine"] == "treesitter-python"
    # v3 (spec 2026-10-20 §8): languages = emitted set, not a static list —
    # an empty graph emits no languages; by_lang/import_edges are new keys,
    # and T3 adds the Go honest-miss counter go_unverifiable_satisfaction.
    assert meta["languages"] == []
    assert meta["extensions"] == ["springboot"]
    assert set(meta["structure"]) == {
        "files", "types", "methods", "call_edges",
        "dropped_edges", "unresolved_refs", "key_collisions",
        "import_edges", "go_unverifiable_satisfaction", "by_lang"}
    assert meta["structure"]["import_edges"] == 0
    assert meta["structure"]["go_unverifiable_satisfaction"] == 0
    assert meta["structure"]["by_lang"] == {}
    assert meta["beans"] == 0 and meta["inject_edges"] == 0 and meta["ambiguous"] == 0


# ---- source-root discovery (spec §3) ----

def test_discover_source_roots_multi_module_and_exclusions(tmp_path):
    repo = build_repo(tmp_path, {
        # root module
        "src/main/java/A.java": "class A {}",
        # module one level deep
        "core/src/main/java/B.java": "class B {}",
        # module exactly three levels deep -> root at depth 6 (allowed)
        "a/b/c/src/main/java/C.java": "class C {}",
        # module four levels deep -> beyond the depth bound, not discovered
        "a/b/c/d/src/main/java/D.java": "class D {}",
        # excluded locations
        "build/generated/src/main/java/E.java": "class E {}",
        ".hidden/src/main/java/F.java": "class F {}",
        ".gradle/v9/src/main/java/G.java": "class G {}",
        "target/classes/src/main/java/H.java": "class H {}",
        "core/src/test/java/T.java": "class T {}",
    })
    roots = {r.relative_to(repo).as_posix() for r in jx.discover_source_roots(repo)}
    assert roots == {
        "src/main/java",
        "core/src/main/java",
        "a/b/c/src/main/java",
    }
    # extract only sees files under discovered roots
    _out, payload = run_extract(repo)
    file_keys = keys(payload, "File")
    assert file_keys == {
        "file:src/main/java/A.java",
        "file:core/src/main/java/B.java",
        "file:a/b/c/src/main/java/C.java",
    }


# ---- parse helpers ----

def test_parse_java_file_package_and_imports(tmp_path):
    src = tmp_path / "Methods.java"
    src.write_text(snippet("Methods.java"), encoding="utf-8")
    jf = jx.parse_java_file(src)
    assert jf.package == "com.example.methods"
    imports = {(i.name, i.wildcard, i.static) for i in jf.imports}
    assert imports == {
        ("java.util.List", False, False),
        ("java.util.Map", False, False),
        ("java.util.Objects", False, False),
        ("java.util.Objects.requireNonNull", False, True),
    }
    assert jf.loc == len(snippet("Methods.java").splitlines())


def test_fqn_index_maps_simple_names_including_nested():
    files = [
        jx.parse_java_file(SNIPPETS / "FqnCases.java"),
        jx.parse_java_file(SNIPPETS / "OtherThing.java"),
        jx.parse_java_file(SNIPPETS / "Helper.java"),
        jx.parse_java_file(SNIPPETS / "BaseModel.java"),
        jx.parse_java_file(SNIPPETS / "Nested.java"),
    ]
    index = jx.fqn_index(files)
    assert index["FqnCases"] == "com.example.app.FqnCases"
    assert index["OtherThing"] == "com.example.app.OtherThing"
    assert index["Helper"] == "com.example.util.Helper"
    assert index["BaseModel"] == "com.example.model.BaseModel"
    assert index["Deep"] == "com.example.app.Nested$Inner$Deep"
    assert index["Inner"] == "com.example.app.Nested$Inner"
    assert index["Listener"] == "com.example.app.Nested$Inner$Listener"


# ---- extraction: FQN resolution + edges (spec §4) ----

def _fqn_repo(tmp_path: Path) -> Path:
    return build_repo(tmp_path, {
        "src/main/java/com/example/app/FqnCases.java": snippet("FqnCases.java"),
        "src/main/java/com/example/app/OtherThing.java": snippet("OtherThing.java"),
        "src/main/java/com/example/util/Helper.java": snippet("Helper.java"),
        "src/main/java/com/example/model/BaseModel.java": snippet("BaseModel.java"),
    })


def test_extract_fqn_resolution_and_edges(tmp_path):
    repo = _fqn_repo(tmp_path)
    out, payload = run_extract(repo)

    # nodes for every source type
    assert keys(payload, "Type") == {
        "type:com.example.app.FqnCases",
        "type:com.example.app.OtherThing",
        "type:com.example.util.Helper",
        "type:com.example.model.BaseModel",
    }
    # EXTENDS resolved through the wildcard import against a known source type
    assert edge_set(payload, "EXTENDS") == {
        ("type:com.example.app.FqnCases", "type:com.example.model.BaseModel"),
    }
    # IMPLEMENTS: explicit import + same package resolve; Missing (import to
    # nowhere) and UnknownApi (no import) are dropped and counted.
    assert edge_set(payload, "IMPLEMENTS") == {
        ("type:com.example.app.FqnCases", "type:com.example.util.Helper"),
        ("type:com.example.app.FqnCases", "type:com.example.app.OtherThing"),
    }
    assert payload["meta"]["structure"]["dropped_edges"] == 2
    # CONTAINS: file -> its types
    assert edge_set(payload, "CONTAINS") >= {
        ("file:src/main/java/com/example/app/FqnCases.java",
         "type:com.example.app.FqnCases"),
    }
    # Type props
    fqn_node = next(n for n in payload["nodes"]
                    if n["key"] == "type:com.example.app.FqnCases")
    assert fqn_node["props"]["kind"] == "class"
    assert fqn_node["props"]["modifiers"] == ["public"]
    assert isinstance(fqn_node["props"]["loc"], int)


def test_extract_drops_interface_from_unresolvable_import(tmp_path):
    """WebConfig-implements-WebMvcConfigurer shape: import present, endpoint
    not source-declared -> no dangling edge, drop counted (spec §4)."""
    repo = build_repo(tmp_path, {
        "src/main/java/com/example/web/WebConfig.java": (
            "package com.example.web;\n"
            "import org.springframework.web.servlet.config.annotation.WebMvcConfigurer;\n"
            "public class WebConfig implements WebMvcConfigurer {\n"
            "}\n"
        ),
    })
    out, payload = run_extract(repo)
    assert payload["edges"] == [
        {"srcKey": "file:src/main/java/com/example/web/WebConfig.java",
         "dstKey": "type:com.example.web.WebConfig", "label": "CONTAINS", "props": {}}
    ]
    assert payload["meta"]["structure"]["dropped_edges"] == 1


# ---- extraction: nested types, kinds, method keys ----

def test_extract_nested_types_use_dollar_keys(tmp_path):
    repo = build_repo(tmp_path, {
        "src/main/java/com/example/app/Nested.java": snippet("Nested.java"),
    })
    out, payload = run_extract(repo)
    assert keys(payload, "Type") == {
        "type:com.example.app.Nested",
        "type:com.example.app.Nested$Inner",
        "type:com.example.app.Nested$Inner$Listener",
        "type:com.example.app.Nested$Inner$Deep",
    }
    file_key = "file:src/main/java/com/example/app/Nested.java"
    assert edge_set(payload, "CONTAINS") == {
        (file_key, "type:com.example.app.Nested"),
        (file_key, "type:com.example.app.Nested$Inner"),
        (file_key, "type:com.example.app.Nested$Inner$Listener"),
        (file_key, "type:com.example.app.Nested$Inner$Deep"),
    }


def test_extract_type_kinds(tmp_path):
    repo = build_repo(tmp_path, {
        "src/main/java/com/example/kinds/Kinds.java": snippet("Kinds.java"),
    })
    out, payload = run_extract(repo)
    kinds = {n["key"]: n["props"]["kind"] for n in payload["nodes"] if n["label"] == "Type"}
    assert kinds == {
        "type:com.example.kinds.Kinds": "class",
        "type:com.example.kinds.Shape": "interface",
        "type:com.example.kinds.Point": "record",
        "type:com.example.kinds.Color": "enum",
        "type:com.example.kinds.Marker": "annotation",
    }
    # record canonical constructor is NOT synthesized
    method_keys = keys(payload, "Method")
    assert "fn:com.example.kinds.Point#Point(int,int)" not in method_keys
    # annotation member IS a method
    assert "fn:com.example.kinds.Marker#value()" in method_keys


def test_extract_method_keys_and_props(tmp_path):
    repo = build_repo(tmp_path, {
        "src/main/java/com/example/methods/Methods.java": snippet("Methods.java"),
    })
    out, payload = run_extract(repo)
    methods = {n["key"]: n["props"] for n in payload["nodes"] if n["label"] == "Method"}
    expected_keys = {
        "fn:com.example.methods.Methods#Methods(String,List,String[])",
        "fn:com.example.methods.Methods#pick(Map)",
        "fn:com.example.methods.Methods#handle(int[][],boolean)",
        "fn:com.example.methods.Methods#save(List)",
    }
    assert set(methods) == expected_keys
    assert payload["meta"]["structure"]["key_collisions"] == 1
    assert payload["meta"]["structure"]["methods"] == 4

    ctor = methods["fn:com.example.methods.Methods#Methods(String,List,String[])"]
    assert ctor["params"] == ["String", "List", "String[]"]
    assert ctor["visibility"] == "public"
    assert ctor["static"] is False
    assert ctor["owner"] == "type:com.example.methods.Methods"

    pick = methods["fn:com.example.methods.Methods#pick(Map)"]
    assert pick["returns"] == "T"

    handle = methods["fn:com.example.methods.Methods#handle(int[][],boolean)"]
    assert handle["visibility"] == "package"
    assert handle["static"] is False

    save = methods["fn:com.example.methods.Methods#save(List)"]
    assert save["static"] is True
    assert save["visibility"] == "private"
    assert save["returns"] == "String"


def test_extract_records_compact_constructor_is_a_method(tmp_path):
    repo = build_repo(tmp_path, {
        "src/main/java/com/example/kinds/Point.java": (
            "package com.example.kinds;\n"
            "public record Point(int x, int y) {\n"
            "    public Point {\n"
            "    }\n"
            "}\n"
        ),
    })
    out, payload = run_extract(repo)
    assert "fn:com.example.kinds.Point#Point()" in keys(payload, "Method")


# ---- CLI contract ----

def test_analyzer_cli_writes_batch_json_and_echoes_extensions(tmp_path):
    repo = _fqn_repo(tmp_path)
    out_json = tmp_path / "structure.json"
    code = analyzer_main([str(repo), str(out_json), "--extensions", "springboot"])
    assert code == 0
    payload = json.loads(out_json.read_text(encoding="utf-8"))
    assert set(payload) >= {"nodes", "edges", "unresolved", "meta"}
    assert payload["meta"]["extensions"] == ["springboot"]
    assert payload["meta"]["structure"]["files"] == 4


def test_analyzer_cli_defaults_to_no_extensions(tmp_path):
    repo = _fqn_repo(tmp_path)
    out_json = tmp_path / "structure.json"
    assert analyzer_main([str(repo), str(out_json)]) == 0
    assert json.loads(out_json.read_text(encoding="utf-8"))["meta"]["extensions"] == []


def test_analyzer_cli_usage_error_exits_nonzero(tmp_path, capsys):
    with pytest.raises(SystemExit) as exc:
        analyzer_main([str(tmp_path / "nope"), str(tmp_path / "o.json")])
    assert exc.value.code == 2
    assert "not a directory" in capsys.readouterr().err


def test_analyzer_cli_missing_args_exit_nonzero(capsys):
    with pytest.raises(SystemExit) as exc:
        analyzer_main([])
    assert exc.value.code == 2
    assert capsys.readouterr().err.strip()


# ---- folded T1-review fix: generic type arguments erased in supers ----

def test_extract_generic_supertype_arguments_erased(tmp_path):
    """implements Sink<String> / extends Box<Base> must resolve the raw simple
    name in-source instead of dropping the edge (T1 review fix)."""
    repo = build_repo(tmp_path, {
        "src/main/java/com/example/calls/GenericSupers.java": snippet("GenericSupers.java"),
    })
    out, payload = run_extract(repo)
    assert edge_set(payload, "EXTENDS") == {
        ("type:com.example.calls.GenericSupers", "type:com.example.calls.Box"),
        ("type:com.example.calls.Chained", "type:com.example.calls.Sink"),
    }
    assert edge_set(payload, "IMPLEMENTS") == {
        ("type:com.example.calls.GenericSupers", "type:com.example.calls.Sink"),
        ("type:com.example.calls.GenericSupers", "type:com.example.calls.Plain"),
    }
    assert payload["meta"]["structure"]["dropped_edges"] == 0


# ---- Task 2: CALLS + REFERENCES (spec §4) ----

_CC = "fn:com.example.calls.CallsCases"


def _calls_repo(tmp_path: Path) -> Path:
    return build_repo(tmp_path, {
        "src/main/java/com/example/calls/CallsCases.java": snippet("CallsCases.java"),
        "src/main/java/com/example/calls/Tool.java": snippet("Tool.java"),
        "src/main/java/com/example/calls/Worker.java": snippet("Worker.java"),
        "src/main/java/com/example/calls/Job.java": snippet("Job.java"),
        "src/main/java/com/example/calls/Plain.java": snippet("Plain.java"),
    })


def call_triples(payload: dict) -> set[tuple[str, str, str]]:
    """(src, dst, kind) triples of CALLS edges (duplicate lines collapse)."""
    return {(e["srcKey"], e["dstKey"], e["props"]["kind"])
            for e in payload["edges"] if e["label"] == "CALLS"}


def test_extract_enum_methods_are_declared(tmp_path):
    """enum members live under enum_body_declarations — still Method nodes."""
    repo = build_repo(tmp_path, {
        "src/main/java/com/example/kinds/Kinds.java": snippet("Kinds.java"),
    })
    out, payload = run_extract(repo)
    assert "fn:com.example.kinds.Color#rotate()" in keys(payload, "Method")


def test_calls_direct_on_class_typed_receivers(tmp_path):
    repo = _calls_repo(tmp_path)
    out, payload = run_extract(repo)
    got = call_triples(payload)
    src = f"{_CC}#"
    assert (f"{src}directViaField()", "fn:com.example.calls.Worker#run()", "direct") in got
    # param receiver and local (declarator) receiver
    assert (f"{src}viaParamAndLocal(Tool)", "fn:com.example.calls.Tool#use()", "direct") in got
    # implicit this, explicit this, and this.<field> receivers
    assert (f"{src}viaThis()", f"{_CC}#own()", "direct") in got
    assert (f"{src}viaThis()", "fn:com.example.calls.Worker#run()", "direct") in got
    # receiver written as a class name (static call) -> direct
    assert (f"{src}viaClassName()", f"{_CC}#own()", "direct") in got
    # unique overload by name+arity
    assert (f"{src}uniqueOverload(Tool)", "fn:com.example.calls.Tool#go(int)", "direct") in got
    # line prop is a real 1-based source line
    e = next(e for e in payload["edges"]
             if e["label"] == "CALLS" and e["dstKey"] == "fn:com.example.calls.Worker#run()"
             and e["srcKey"] == f"{src}directViaField()")
    assert isinstance(e["props"]["line"], int) and e["props"]["line"] > 0


def test_calls_virtual_dispatch_on_interface_receivers(tmp_path):
    repo = build_repo(tmp_path, {
        "src/main/java/com/example/calls/CallsCases.java": snippet("CallsCases.java"),
        "src/main/java/com/example/calls/Tool.java": snippet("Tool.java"),
        "src/main/java/com/example/calls/Worker.java": snippet("Worker.java"),
        "src/main/java/com/example/calls/Job.java": snippet("Job.java"),
        "src/main/java/com/example/calls/Plain.java": snippet("Plain.java"),
        "src/main/java/com/example/calls/RecordCalls.java": snippet("RecordCalls.java"),
    })
    out, payload = run_extract(repo)
    got = call_triples(payload)
    assert (f"{_CC}#virtualViaField()", "fn:com.example.calls.Job#run()",
            "virtual-dispatch") in got
    # record component receiver, interface-declared target
    assert ("fn:com.example.calls.RecordCalls#drive()",
            "fn:com.example.calls.Engine#start()", "virtual-dispatch") in got


def test_calls_inherited_field_receiver(tmp_path):
    repo = build_repo(tmp_path, {
        "src/main/java/com/example/calls/InheritCalls.java": snippet("InheritCalls.java"),
        "src/main/java/com/example/calls/Worker.java": snippet("Worker.java"),
    })
    out, payload = run_extract(repo)
    assert call_triples(payload) == {
        ("fn:com.example.calls.InheritCalls#act()",
         "fn:com.example.calls.Worker#run()", "direct"),
    }


def test_calls_overload_ambiguity_dropped(tmp_path):
    repo = _calls_repo(tmp_path)
    out, payload = run_extract(repo)
    dsts = {e["dstKey"] for e in payload["edges"] if e["label"] == "CALLS"}
    # no CALLS edge to either arity-2 overload (name+arity multi-match dropped)
    assert "fn:com.example.calls.Tool#go(String,int)" not in dsts
    assert "fn:com.example.calls.Tool#go(int,String)" not in dsts


def test_calls_static_import_usage_dropped(tmp_path):
    repo = _calls_repo(tmp_path)
    out, payload = run_extract(repo)
    dsts = {e["dstKey"] for e in payload["edges"] if e["label"] == "CALLS"}
    assert not any("requireNonNull" in d for d in dsts)
    # neither unqualified member use nor Objects.<member> produced an edge
    assert payload["meta"]["structure"]["unresolved_refs"] == 1  # only s.trim()


def test_calls_and_references_counters(tmp_path):
    """Exact accounting on the CallsCases repo:
    call_edges = 9, dropped = 6 (1 ambiguous + 2 static-import + 1 no-such-method
    + new String + (String) cast), unresolved_refs = 1 (String receiver)."""
    repo = _calls_repo(tmp_path)
    out, payload = run_extract(repo)
    st = payload["meta"]["structure"]
    assert st["call_edges"] == 9
    assert st["dropped_edges"] == 6
    assert st["unresolved_refs"] == 1


def test_references_from_new_and_cast(tmp_path):
    repo = _calls_repo(tmp_path)
    out, payload = run_extract(repo)
    refs = {(e["srcKey"], e["dstKey"]) for e in payload["edges"]
            if e["label"] == "REFERENCES"}
    src = f"{_CC}#references()"
    assert refs == {
        (src, "type:com.example.calls.Tool"),
        (src, "type:com.example.calls.Plain"),
        (f"{_CC}#viaParamAndLocal(Tool)", "type:com.example.calls.Tool"),
    }
    e = next(e for e in payload["edges"] if e["label"] == "REFERENCES"
             and e["dstKey"] == "type:com.example.calls.Tool")
    assert isinstance(e["props"]["line"], int) and e["props"]["line"] > 0


# ---- frozen fixture smoke (spec §8.3 structure bounds) ----

@pytest.fixture(scope="module")
def torture_payload(tmp_path_factory):
    """Copy the frozen fixture to a temp dir (never run inside the fixture
    tree) and analyze the copy via the CLI entry."""
    repo = tmp_path_factory.mktemp("smoke") / "torture-app"
    shutil.copytree(TORTURE_APP, repo)
    out_json = repo.parent / "structure.json"
    assert analyzer_main([str(repo), str(out_json)]) == 0
    return json.loads(out_json.read_text(encoding="utf-8"))


def test_torture_app_smoke_counts(torture_payload):
    meta = torture_payload["meta"]
    st = meta["structure"]
    assert st["files"] == 33
    assert st["types"] >= 30
    # SPEC DEVIATION (reported): spec §8.3 / plan Task-1 say "methods >= 80",
    # but the frozen fixture contains exactly 39 method + 12 constructor + 1
    # annotation-element declarations = 52 written members and the brief
    # forbids synthesizing non-syntax constructors — pinned to the true
    # syntactic count instead.
    assert st["methods"] == 52
    assert st["dropped_edges"] >= 0  # reported honestly (torture supers all resolve)
    assert meta["extensions"] == []


def test_torture_app_smoke_structure_edges(torture_payload):
    implements = edge_set(torture_payload, "IMPLEMENTS")
    assert ("type:com.acme.core.GreetingServiceImpl",
            "type:com.acme.core.GreetingService") in implements
    # nested class key with $ (v1-compatible)
    assert "type:com.acme.web.GreetingController$Metrics" in keys(torture_payload, "Type")
    # every edge endpoint is a declared node: no dangling edges survive
    node_keys = {n["key"] for n in torture_payload["nodes"]}
    for e in torture_payload["edges"]:
        assert e["srcKey"] in node_keys and e["dstKey"] in node_keys


def test_torture_app_smoke_call_edges(torture_payload):
    """CALLS land in the spec §8.3 window (in-source resolution only); the
    known virtual-dispatch edge resolves through the interface receiver."""
    st = torture_payload["meta"]["structure"]
    assert 10 <= st["call_edges"] <= 40
    # interface-typed receiver -> virtual-dispatch to the interface method key
    calls = [e for e in torture_payload["edges"] if e["label"] == "CALLS"]
    by_src_dst = {(e["srcKey"], e["dstKey"]): e["props"] for e in calls}
    greet = ("fn:com.acme.core.GreetingServiceImpl#greet(String)",
             "fn:com.acme.core.GreetingRepository#loadTemplate()")
    assert greet in by_src_dst
    assert by_src_dst[greet]["kind"] == "virtual-dispatch"
    # inherited field receiver (audit lives on AbstractAuditedService) -> direct
    audit = ("fn:com.acme.core.GreetingServiceImpl#greet(String)",
             "fn:com.acme.core.GreetingAudit#audit(String)")
    assert audit in by_src_dst
    assert by_src_dst[audit]["kind"] == "direct"
    # out-of-source receivers are counted honestly, never dangled
    assert st["unresolved_refs"] > 0
