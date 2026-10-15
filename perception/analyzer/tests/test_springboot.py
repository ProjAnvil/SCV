"""TDD spec for the springboot language extension (perception v2, Task 3).

Covers spec 2026-10-14 §5 on standalone snippets (fixture-derived shapes):
stereotype detection incl. meta chains, bean naming (two-upper rule, explicit
value), @Bean methods (incl. static) + produced-type suppression, injection
points (constructor / lombok / field / inherited-field / setter / @Bean param,
@Value skip), resolution (qualifier / @Primary / by-name / collection+Optional
unwrap / honest ambiguity), conditions wording, @EnableConfigurationProperties
registration, fully-qualified inline annotations, unresolved accounting, the
registry gate, and the analyzer CLI wiring.
"""

import json
import re
from pathlib import Path

import pytest

from structure.model import Graph_out
from structure import java_extractor as jx
from extensions import registry
from analyzer import main as analyzer_main

TESTS_DIR = Path(__file__).resolve().parent
SNIPPETS = TESTS_DIR / "snippets"


def snippet(name: str) -> str:
    return (SNIPPETS / name).read_text(encoding="utf-8")


def repo_of(tmp_path: Path, *names: str) -> Path:
    """Write snippets into a synthetic repo: the package statement of each
    snippet decides its src/main/java path."""
    repo = tmp_path / "repo"
    for name in names:
        content = snippet(name)
        package = re.match(r"\s*package\s+([\w.]+)\s*;", content).group(1)
        target = repo / "src" / "main" / "java" / Path(*package.split(".")) / name
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(content, encoding="utf-8")
    return repo


def run(repo: Path, extensions: list[str] | None = None) -> tuple[Graph_out, dict]:
    exts = ["springboot"] if extensions is None else extensions
    out = Graph_out(extensions=exts)
    java_files = jx.extract(repo, out)
    registry.run(exts, java_files, out)
    return out, out.to_json()


def bean_props(payload: dict) -> dict[str, dict]:
    return {n["key"]: n["props"] for n in payload["nodes"] if n["label"] == "Bean"}


def injects(payload: dict) -> list[tuple]:
    """(src, dst, via, qualifier, wraps, ambiguous) — the §5 multiset view
    (positional props excluded by design)."""
    return [
        (e["srcKey"], e["dstKey"], e["props"]["via"], e["props"].get("qualifier"),
         e["props"].get("wraps"), e["props"].get("ambiguous", False))
        for e in payload["edges"] if e["label"] == "INJECTS"
    ]


# ---- §5 name table: stereotypes + naming ----

def test_bean_naming_two_upper_and_explicit_value(tmp_path):
    _out, payload = run(repo_of(tmp_path, "SbNaming.java"))
    props = bean_props(payload)
    assert set(props) == {
        "bean:cacheWarmer", "bean:URLValidator", "bean:custom"}
    # two leading uppercase letters are preserved
    assert props["bean:URLValidator"]["beanName"] == "URLValidator"
    assert props["bean:URLValidator"]["type"] == "com.sb.naming.URLValidator"
    assert props["bean:URLValidator"]["stereotype"] == "Component"
    # explicit annotation value wins over the simple name
    assert props["bean:custom"]["beanName"] == "custom"
    assert props["bean:custom"]["type"] == "com.sb.naming.Explicit"
    assert props["bean:cacheWarmer"]["beanName"] == "cacheWarmer"


def test_bean_node_schema_matches_v1(tmp_path):
    _out, payload = run(repo_of(tmp_path, "SbNaming.java"))
    node = next(n for n in payload["nodes"] if n["key"] == "bean:URLValidator")
    assert node["label"] == "Bean"
    assert set(node["props"]) == {"beanName", "type", "stereotype", "primary",
                                  "source"}
    assert node["props"]["primary"] is False
    # source is file-basename:line (positional, but still emitted)
    assert re.fullmatch(r"SbNaming\.java:\d+", node["props"]["source"])


def test_meta_chain_in_source_annotation(tmp_path):
    """@MyRestController -> @RestController via the in-source @interface."""
    _out, payload = run(repo_of(tmp_path, "SbMyRestController.java", "SbMetaApp.java"))
    props = bean_props(payload)
    assert set(props) == {"bean:chainApplication", "bean:metaController"}
    assert props["bean:metaController"]["stereotype"] == "RestController"
    assert props["bean:metaController"]["type"] == "com.sb.meta.MetaController"


def test_meta_chain_springboot_application_table(tmp_path):
    """@SpringBootApplication resolves through the jar-carried table chain
    SpringBootApplication -> SpringBootConfiguration -> Configuration."""
    _out, payload = run(repo_of(tmp_path, "SbMyRestController.java", "SbMetaApp.java"))
    props = bean_props(payload)
    assert props["bean:chainApplication"]["stereotype"] == "Configuration"
    assert props["bean:chainApplication"]["type"] == "com.sb.meta.ChainApplication"


def test_fq_inline_annotation_form(tmp_path):
    """@org.springframework.stereotype.Component on a nested class + an
    FQ-written constructor param type (torture GreetingController$Metrics)."""
    _out, payload = run(repo_of(tmp_path, "SbFqInline.java"))
    props = bean_props(payload)
    assert set(props) == {"bean:host", "bean:metrics", "bean:audit"}
    assert props["bean:metrics"]["stereotype"] == "Component"
    assert props["bean:metrics"]["type"] == "com.sb.fq.Host$Metrics"
    assert injects(payload) == [
        ("bean:metrics", "bean:audit", "constructor", None, None, False)]


# ---- @Bean methods ----

def test_bean_methods_params_and_produced_suppression(tmp_path):
    """@Bean params are injection points (via beanMethod, incl. @Qualifier);
    a class whose type is @Bean-produced contributes NO class-level points
    (its ctor params must not double-count); STATIC @Bean methods count."""
    _out, payload = run(repo_of(tmp_path, "SbBeanMethods.java"))
    props = bean_props(payload)
    assert set(props) == {"bean:beanConfig", "bean:clock", "bean:greetingClient",
                          "bean:emailSender", "bean:smsSender"}
    assert props["bean:clock"]["stereotype"] == "@Bean"
    assert props["bean:clock"]["type"] == "java.time.Clock"
    assert props["bean:greetingClient"]["type"] == "com.sb.beans.GreetingClient"
    assert sorted(injects(payload)) == [
        ("bean:greetingClient", "bean:clock", "beanMethod", None, None, False),
        ("bean:greetingClient", "bean:emailSender", "beanMethod", "emailSender",
         None, False),
    ]


def test_bean_conditions_and_primary(tmp_path):
    _out, payload = run(repo_of(tmp_path, "SbConditions.java"))
    props = bean_props(payload)
    assert props["bean:devBean"]["conditions"] == "Profile(dev)"
    assert (props["bean:condBean"]["conditions"]
            == "ConditionalOnProperty(audit.enabled=true)")
    assert props["bean:missingBean"]["conditions"] == "ConditionalOnMissingBean"
    # no conditions prop when the list is empty
    assert "conditions" not in bean_props(
        run(repo_of(tmp_path, "SbNaming.java"))[1])["bean:custom"]


def test_primary_collapse(tmp_path):
    _out, payload = run(repo_of(tmp_path, "SbResolve.java"))
    assert ("bean:primaryConsumer", "bean:smsChannel", "constructor", None,
            None, False) in injects(payload)


def test_by_name_fallback(tmp_path):
    _out, payload = run(repo_of(tmp_path, "SbResolve.java"))
    assert ("bean:byNameConsumer", "bean:alphaLinker", "field", None, None,
            False) in injects(payload)


def test_ambiguity_flagged_honestly(tmp_path):
    _out, payload = run(repo_of(tmp_path, "SbResolve.java"))
    assert sorted(injects(payload)) == [
        ("bean:ambiguousConsumer", "bean:alphaLinker", "field", None, None, True),
        ("bean:ambiguousConsumer", "bean:betaLinker", "field", None, None, True),
        ("bean:byNameConsumer", "bean:alphaLinker", "field", None, None, False),
        ("bean:primaryConsumer", "bean:smsChannel", "constructor", None, None, False),
    ]
    assert payload["meta"]["ambiguous"] == 2


# ---- injection-point kinds ----

def test_list_set_collection_unwrap_takes_all(tmp_path):
    _out, payload = run(repo_of(tmp_path, "SbWraps.java"))
    got = sorted(injects(payload))
    assert got == [
        ("bean:collConsumer", "bean:wrapEmail", "constructor", None, "Collection",
         False),
        ("bean:collConsumer", "bean:wrapSms", "constructor", None, "Collection",
         False),
        ("bean:listConsumer", "bean:wrapEmail", "constructor", None, "List", False),
        ("bean:listConsumer", "bean:wrapSms", "constructor", None, "List", False),
        ("bean:optionalConsumer", "bean:warmer", "constructor", None, "Optional",
         False),
        ("bean:setConsumer", "bean:wrapEmail", "constructor", None, "Set", False),
        ("bean:setConsumer", "bean:wrapSms", "constructor", None, "Set", False),
    ]


def test_optional_unwrap(tmp_path):
    _out, payload = run(repo_of(tmp_path, "SbWraps.java"))
    assert ("bean:optionalConsumer", "bean:warmer", "constructor", None,
            "Optional", False) in injects(payload)


def test_inherited_autowired_field(tmp_path):
    _out, payload = run(repo_of(tmp_path, "SbInherit.java"))
    assert injects(payload) == [
        ("bean:inheritConsumer", "bean:sink", "inherited-field", None, None, False)]


def test_setter_injection(tmp_path):
    _out, payload = run(repo_of(tmp_path, "SbSetter.java"))
    assert injects(payload) == [
        ("bean:setterConsumer", "bean:helper", "setter", None, None, False)]


def test_multi_ctor_autowired_wins(tmp_path):
    _out, payload = run(repo_of(tmp_path, "SbMultiCtor.java"))
    assert injects(payload) == [
        ("bean:multiCtor", "bean:sink", "constructor", None, None, False)]


def test_value_params_skipped(tmp_path):
    _out, payload = run(repo_of(tmp_path, "SbValue.java"))
    assert injects(payload) == [
        ("bean:valueConsumer", "bean:sink", "constructor", None, None, False)]
    assert payload["unresolved"] == 0


def test_lombok_required_args_constructor(tmp_path):
    _out, payload = run(repo_of(tmp_path, "SbLombok.java"))
    # @Component("qual") names the bean "qual" — the qualifier matches by name
    assert sorted(injects(payload)) == [
        ("bean:lombokConsumer", "bean:qual", "lombok-constructor", "qual",
         None, False),
        ("bean:lombokConsumer", "bean:repo", "lombok-constructor", None, None, False),
    ]


# ---- @EnableConfigurationProperties + records ----

def test_enable_configuration_properties_record(tmp_path):
    _out, payload = run(repo_of(tmp_path, "SbEcp.java"))
    props = bean_props(payload)
    assert set(props) == {"bean:ecpConfig", "bean:featureProps", "bean:cardChannel"}
    fp = props["bean:featureProps"]
    assert fp["stereotype"] == "ConfigurationProperties"
    assert fp["type"] == "com.sb.ecp.FeatureProps"
    assert fp["conditions"] == "EnableConfigurationProperties(EcpConfig)"
    # record components are the canonical constructor: constructor injection
    assert ("bean:cardChannel", "bean:featureProps", "constructor", None, None,
            False) in injects(payload)


# ---- unresolved accounting ----

def test_unresolved_counts_non_framework_only(tmp_path):
    _out, payload = run(repo_of(tmp_path, "SbUnresolved.java"))
    assert payload["unresolved"] == 1  # External (java.time.ZoneId is noise)
    assert not any(e["label"] == "INJECTS" for e in payload["edges"])


def test_injects_edge_prop_schema(tmp_path):
    _out, payload = run(repo_of(tmp_path, "SbWraps.java"))
    e = next(e for e in payload["edges"] if e["label"] == "INJECTS"
             and e["props"].get("wraps") == "List")
    assert set(e["props"]) == {"via", "wraps", "at"}
    assert re.fullmatch(r"SbWraps\.java:\d+", e["props"]["at"])


# ---- meta + registry + CLI wiring ----

def test_meta_beans_inject_edges_ambiguous_counts(tmp_path):
    _out, payload = run(repo_of(tmp_path, "SbWraps.java"))
    meta = payload["meta"]
    assert meta["extensions"] == ["springboot"]
    assert meta["beans"] == 7
    assert meta["inject_edges"] == 7
    assert meta["ambiguous"] == 0
    assert payload["unresolved"] == 0


def test_registry_unknown_extension_warns_and_ignores(tmp_path, capsys):
    repo = repo_of(tmp_path, "SbNaming.java")
    out = Graph_out(extensions=["nope", "springboot"])
    java_files = jx.extract(repo, out)
    registry.run(["nope", "springboot"], java_files, out)
    err = capsys.readouterr().err
    assert "nope" in err and "springboot" in err
    payload = out.to_json()
    assert payload["meta"]["beans"] == 3  # the known extension still ran
    assert payload["meta"]["extensions"] == ["nope", "springboot"]  # echoed


def test_registry_empty_extension_list_runs_nothing(tmp_path):
    repo = repo_of(tmp_path, "SbNaming.java")
    out = Graph_out(extensions=[])
    java_files = jx.extract(repo, out)
    registry.run([], java_files, out)
    payload = out.to_json()
    assert payload["meta"]["beans"] == 0
    assert not any(n["label"] == "Bean" for n in payload["nodes"])
    assert payload["meta"]["structure"]["files"] == 1  # structure still emitted


def test_analyzer_cli_wires_springboot_extension(tmp_path):
    repo = repo_of(tmp_path, "SbNaming.java")
    out_json = tmp_path / "structure.json"
    code = analyzer_main([str(repo), str(out_json), "--extensions", "springboot"])
    assert code == 0
    payload = json.loads(out_json.read_text(encoding="utf-8"))
    assert payload["meta"]["extensions"] == ["springboot"]
    assert payload["meta"]["beans"] == 3
    assert {n["key"] for n in payload["nodes"] if n["label"] == "Bean"} == {
        "bean:cacheWarmer", "bean:URLValidator", "bean:custom"}


def test_analyzer_cli_without_extensions_has_no_beans(tmp_path):
    repo = repo_of(tmp_path, "SbNaming.java")
    out_json = tmp_path / "structure.json"
    assert analyzer_main([str(repo), str(out_json)]) == 0
    payload = json.loads(out_json.read_text(encoding="utf-8"))
    assert payload["meta"]["beans"] == 0
    assert payload["meta"]["inject_edges"] == 0
    assert not any(n["label"] == "Bean" for n in payload["nodes"])
    assert payload["meta"]["structure"]["files"] == 1
