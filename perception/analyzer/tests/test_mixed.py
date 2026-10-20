"""Mixed-repository schema tests — spec 2026-10-20 §8 (plan Task 5).

One tree, three languages (java + python + typescript): meta.languages as a
sorted set of emitted languages, by_lang per-language counts, aggregate
counters = sums over languages, IMPORTS edges emitted per language, and the
springboot extension behaving identically in mixed mode vs a java-only run.
"""
from __future__ import annotations

import json
import shutil
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from analyzer import main as analyzer_main  # noqa: E402

TESTS_DIR = Path(__file__).resolve().parent
PY_SNIPPETS = TESTS_DIR / "snippets" / "python"

JAVA_APP = """package com.mixed.app;

import com.mixed.core.Persist;
import org.springframework.stereotype.Component;

@Component
public class App {
    Persist store;
}
"""

JAVA_PERSIST = "package com.mixed.core;\n\npublic class Persist {\n}\n"

TS_A = """export class Circle {
  area(): number {
    return 1;
  }
}

export function make(): number {
  return 2;
}
"""

TS_B = """import { Circle } from "./a";

export class Panel {
  mount(): void {
    const c = new Circle();
  }
}
"""

TS_C = """import { make } from "./a";

export function use(): number {
  return make();
}
"""

F_APP = "file:src/main/java/com/mixed/app/App.java"
F_PERSIST = "file:src/main/java/com/mixed/core/Persist.java"


def _build_mixed(tmp_path: Path) -> Path:
    repo = tmp_path / "mixed-repo"
    java_app = repo / "src/main/java/com/mixed/app"
    java_core = repo / "src/main/java/com/mixed/core"
    java_app.mkdir(parents=True)
    java_core.mkdir(parents=True)
    (java_app / "App.java").write_text(JAVA_APP, encoding="utf-8")
    (java_core / "Persist.java").write_text(JAVA_PERSIST, encoding="utf-8")
    # python: the snippets package copied verbatim (module paths are
    # absolute, so the package must sit at the repo root)
    shutil.copytree(PY_SNIPPETS / "app", repo / "app")
    ts = repo / "ts"
    ts.mkdir()
    (ts / "a.ts").write_text(TS_A, encoding="utf-8")
    (ts / "b.ts").write_text(TS_B, encoding="utf-8")
    (ts / "c.ts").write_text(TS_C, encoding="utf-8")
    return repo


@pytest.fixture(scope="module")
def mixed(tmp_path_factory) -> dict:
    repo = _build_mixed(tmp_path_factory.mktemp("mixed"))
    out_json = repo.parent / "mixed.json"
    assert analyzer_main(
        [str(repo), str(out_json), "--extensions", "springboot"]) == 0
    return json.loads(out_json.read_text(encoding="utf-8"))


def _imports(payload: dict) -> set[tuple[str, str]]:
    return {(e["srcKey"], e["dstKey"]) for e in payload["edges"]
            if e["label"] == "IMPORTS"}


# ---- meta.languages + by_lang (spec §8) ----

def test_meta_languages_sorted_set(mixed):
    assert mixed["meta"]["languages"] == ["java", "python", "typescript"]


def test_by_lang_counts(mixed):
    by = mixed["meta"]["structure"]["by_lang"]
    assert by["java"] == {"files": 2, "types": 2, "methods": 0,
                          "call_edges": 0}
    assert by["python"] == {"files": 4, "types": 2, "methods": 9,
                            "call_edges": 5}
    assert by["typescript"] == {"files": 3, "types": 2, "methods": 4,
                                "call_edges": 1}


def test_aggregates_are_sums(mixed):
    st = mixed["meta"]["structure"]
    by = st["by_lang"]
    assert st["files"] == sum(v["files"] for v in by.values()) == 9
    assert st["types"] == sum(v["types"] for v in by.values()) == 6
    assert st["methods"] == sum(v["methods"] for v in by.values()) == 13
    assert st["call_edges"] == sum(v["call_edges"] for v in by.values()) == 6


# ---- IMPORTS edges per language ----

def test_imports_edges_by_language(mixed):
    imports = _imports(mixed)
    java_edges = {(s, d) for s, d in imports
                  if s.endswith(".java") and d.endswith(".java")}
    py_edges = {(s, d) for s, d in imports
                if s.endswith(".py") and d.endswith(".py")}
    ts_edges = {(s, d) for s, d in imports
                if s.endswith(".ts") and d.endswith(".ts")}
    # java: App's single-type import of the in-source Persist
    assert java_edges == {(F_APP, F_PERSIST)}
    # python: the snippets tree's three from-imports (all names bind)
    assert py_edges == {
        ("file:app/__init__.py", "file:app/services.py"),
        ("file:app/services.py", "file:app/base.py"),
        ("file:app/services.py", "file:app/util.py"),
    }
    # ts: b and c import from a
    assert ts_edges == {
        ("file:ts/b.ts", "file:ts/a.ts"),
        ("file:ts/c.ts", "file:ts/a.ts"),
    }
    assert mixed["meta"]["structure"]["import_edges"] == 6


# ---- springboot extension in mixed mode ----

def test_mixed_beans_and_java_by_lang_match_java_only(tmp_path):
    """The java layer is untouched by the other extractors: a java-only run
    and the mixed run agree on by_lang.java and on the bean count."""
    repo = _build_mixed(tmp_path)
    mixed_json = tmp_path / "m.json"
    java_json = tmp_path / "j.json"
    assert analyzer_main(
        [str(repo), str(mixed_json), "--extensions", "springboot"]) == 0
    assert analyzer_main(
        [str(repo), str(java_json), "--extensions", "springboot",
         "--languages", "java"]) == 0
    m = json.loads(mixed_json.read_text(encoding="utf-8"))
    j = json.loads(java_json.read_text(encoding="utf-8"))
    assert m["meta"]["beans"] == j["meta"]["beans"] == 1
    assert (m["meta"]["structure"]["by_lang"]["java"]
            == j["meta"]["structure"]["by_lang"]["java"])
    # the mixed run reports only the languages it actually emitted
    assert j["meta"]["languages"] == ["java"]


def test_mixed_cli_smoke(mixed):
    """CLI ran green over the mixed tree (fixture above); no cross-language
    key collisions and every edge endpoint is a declared node."""
    node_keys = {n["key"] for n in mixed["nodes"]}
    assert mixed["meta"]["structure"]["key_collisions"] == 0
    for e in mixed["edges"]:
        assert e["srcKey"] in node_keys and e["dstKey"] in node_keys
