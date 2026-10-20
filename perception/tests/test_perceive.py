"""perceive.py unit suite (v2 spec 2026-10-14 §6/§7 + v3 spec 2026-10-20 §7).

Covers the v2 tier set: two-stage springboot detect (unchanged), the uv
probe, the v2 footnote set verbatim, extension gating (unknown values warn +
drop), stale cleanup, label-filtered counts + structure sub-counts +
structure-mode summary wording, and the analyzer tier end-to-end through
SCV_PERCEPTION_ANALYZER fakes (ok / failing / garbage-output / missing-output
paths, uv-missing, structure mode).

v3 additions (plan Task 6): the `structure_languages` config gate (default
all six, unknown warn+ignore, empty = tier off), the extension-driven detect
walk (any enabled language with >= 1 source file runs the structure tier;
`not-framework` only when no enabled language is present), `--languages` fed
to the analyzer from config ∩ detected, and the multi-language aggregate
summary wording (files + language list, zh/en).

End-to-end cases run perceive.py as a subprocess with a controlled HOME
(config) and a minimal PATH — hermetic: no uv/go/java/gradle needed.
"""
import json
import os
import subprocess
import sys
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import perceive

ROOT = Path(__file__).resolve().parents[1]
# uv (~/.local/bin) and go (/opt/homebrew/bin) are intentionally absent so the
# subprocess runs never touch the real toolchain; the fake analyzer shebang
# (/usr/bin/env python3) still resolves.
MINIMAL_PATH = "/usr/bin:/bin"


def _write_fake(path: Path, mode: str, graph: dict | None = None) -> Path:
    path.write_text(FAKE_ANALYZER_TEMPLATE
                    .replace("__MODE__", mode)
                    .replace("__GRAPH__", json.dumps(graph if graph is not None else FAKE_GRAPH)))
    path.chmod(0o755)
    return path


# The graph the fake analyzer emits (single source of truth: injected into the
# fake template AND asserted against in CountsTests). v3-shaped: meta.languages
# + meta.structure.by_lang (v3 spec §8).
FAKE_GRAPH = {
    "nodes": [
        {"key": "file:src/main/java/com/x/App.java", "label": "File"},
        {"key": "type:com.x.App", "label": "Type"},
        {"key": "fn:com.x.App#main(String[])", "label": "Method"},
        {"key": "bean:app", "label": "Bean"},
        {"key": "bean:svc", "label": "Bean"},
        {"key": "bean:repo", "label": "Bean"},
    ],
    "edges": [
        {"srcKey": "file:src/main/java/com/x/App.java", "dstKey": "type:com.x.App", "label": "CONTAINS"},
        {"srcKey": "fn:com.x.App#main(String[])", "dstKey": "fn:com.x.Run#go()", "label": "CALLS"},
        {"srcKey": "bean:app", "dstKey": "bean:svc", "label": "INJECTS"},
        {"srcKey": "bean:svc", "dstKey": "bean:repo", "label": "INJECTS"},
    ],
    "unresolved": 0,
    "meta": {
        "engine": "treesitter-python", "languages": ["java"], "extensions": [],
        "structure": {"files": 7, "types": 8, "methods": 9, "call_edges": 4,
                      "dropped_edges": 1, "unresolved_refs": 2, "key_collisions": 0,
                      "by_lang": {"java": {"files": 7, "types": 8, "methods": 9,
                                           "call_edges": 4}}},
        "beans": 3, "inject_edges": 2, "ambiguous": 0,
    },
}


# A stand-in analyzer executable: same CLI as analyzer.py
# (<repo> <out> [--extensions EXT...] [--languages LANG...]); records its argv
# next to the output file. When the emitted graph is v3-shaped (has
# meta.languages) the fake mirrors the --languages it received into the graph,
# like the real analyzer does; a graph without meta.languages stays v2-shaped
# (the backwards-compat path).
FAKE_ANALYZER_TEMPLATE = '''#!/usr/bin/env python3
import json, sys
args = sys.argv[1:]
repo, out = args[0], args[1]
with open(out + ".argv", "w") as fh:
    json.dump(args, fh)

def flag_values(flag):
    if flag not in args:
        return []
    values = []
    for a in args[args.index(flag) + 1:]:
        if a.startswith("--"):
            break
        values.append(a)
    return values

mode = "__MODE__"
if mode == "fail":
    print("fake analyzer: boom", file=sys.stderr)
    sys.exit(3)
if mode == "garbage":
    with open(out, "w") as fh:
        fh.write("not json at all{")
    sys.exit(0)
if mode == "noout":
    sys.exit(0)
exts = flag_values("--extensions")
langs = flag_values("--languages")
graph = json.loads("""__GRAPH__""")
graph["meta"]["extensions"] = exts
if langs and "languages" in graph["meta"]:
    base = graph["meta"]["structure"]["by_lang"]["java"]
    graph["meta"]["languages"] = langs
    graph["meta"]["structure"]["by_lang"] = {l: dict(base) for l in langs}
if not exts:  # structure mode: a real analyzer emits no extension-layer nodes
    graph["nodes"] = [n for n in graph["nodes"] if n["label"] != "Bean"]
    graph["edges"] = [e for e in graph["edges"] if e["label"] != "INJECTS"]
with open(out, "w") as fh:
    json.dump(graph, fh)
print("fake analyzer: non-fatal stderr noise", file=sys.stderr)
'''


def _springboot_repo(d: Path) -> Path:
    d.mkdir(parents=True, exist_ok=True)
    (d / "build.gradle").write_text("plugins { id 'org.springframework.boot' version '3.4.5' }")
    src = d / "src/main/java/com/x"
    src.mkdir(parents=True)
    (src / "App.java").write_text("@SpringBootApplication public class App {}")
    return d


def _plain_java_repo(d: Path) -> Path:
    d.mkdir(parents=True, exist_ok=True)
    (d / "pom.xml").write_text("<project/>")
    src = d / "src/main/java/com/x"
    src.mkdir(parents=True)
    (src / "App.java").write_text("public class App {}")
    return d


def _python_repo(d: Path) -> Path:
    d.mkdir(parents=True, exist_ok=True)
    (d / "app.py").write_text("class App:\n    def run(self):\n        return 1\n")
    (d / "README.md").write_text("# python repo\n")
    return d


def _run_perceive(repo, out, cfg, env_extra=None, extra_args=None):
    """Subprocess run with a temp HOME (config control) + minimal PATH."""
    home = out.parent / "home"
    home.mkdir(parents=True, exist_ok=True)
    if cfg is not None:
        (home / ".scv").mkdir(parents=True, exist_ok=True)
        (home / ".scv" / "config.json").write_text(json.dumps(cfg))
    env = {**os.environ, "HOME": str(home), "PATH": MINIMAL_PATH,
           "SCV_PERCEPTION_DIST": str(out.parent / "dist")}
    env.update(env_extra or {})
    return subprocess.run(
        [sys.executable, str(ROOT / "perceive.py"), str(repo), str(out)]
        + (extra_args or ["--source", "local", "--lang", "en"]),
        capture_output=True, text=True, timeout=120, env=env)


def _payload(res: subprocess.CompletedProcess) -> dict:
    return json.loads(res.stdout.strip().splitlines()[-1])


class DetectTests(unittest.TestCase):
    def setUp(self):
        self.manifest = json.loads((ROOT / "adapters/springboot/MANIFEST.json").read_text())

    def test_gradle_marker_with_confirm(self):
        with TemporaryDirectory() as d:
            d = _springboot_repo(Path(d))
            self.assertTrue(perceive.detect_framework(d, self.manifest))

    def test_kts_marker(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            (d / "build.gradle.kts").write_text('implementation("org.springframework.boot:spring-boot-starter")')
            src = d / "src/main/java/com/x"
            src.mkdir(parents=True)
            (src / "App.java").write_text("@SpringBootApplication class App")
            self.assertTrue(perceive.detect_framework(d, self.manifest))

    def test_multi_module_submodule_confirm(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            (d / "build.gradle").write_text("plugins { id 'org.springframework.boot' version '3.4.5' }")
            src = d / "service/src/main/java/com/x"
            src.mkdir(parents=True)
            (src / "App.java").write_text("@SpringBootApplication public class App {}")
            self.assertTrue(perceive.detect_framework(d, self.manifest))

    def test_marker_without_confirm_is_rejected(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            (d / "build.gradle").write_text("testImplementation 'org.springframework.boot:spring-boot-starter-test'")
            self.assertFalse(perceive.detect_framework(d, self.manifest))

    def test_non_framework(self):
        with TemporaryDirectory() as d:
            self.assertFalse(perceive.detect_framework(Path(d), self.manifest))


class LanguageGateTests(unittest.TestCase):
    """`structure_languages` config gate (v3 spec §7): default = every
    shipped language; unknown values warn + are ignored (structure_extensions
    precedent); an empty list turns the structure tier off entirely."""

    def test_absent_key_defaults_to_all_six(self):
        languages, warnings = perceive.resolve_languages({})
        self.assertEqual(languages,
                         ["go", "java", "javascript", "python", "rust", "typescript"])
        self.assertEqual(warnings, [])

    def test_empty_list_turns_tier_off(self):
        languages, warnings = perceive.resolve_languages({"structure_languages": []})
        self.assertEqual((languages, warnings), ([], []))

    def test_unknown_values_warned_and_ignored(self):
        languages, warnings = perceive.resolve_languages(
            {"structure_languages": ["java", "kotlin", "kotlin"]})
        self.assertEqual(languages, ["java"])
        self.assertEqual(len(warnings), 2)  # both kotlin occurrences
        self.assertTrue(all("kotlin" in w and "ignored" in w for w in warnings))

    def test_non_list_shape_ignored(self):
        languages, warnings = perceive.resolve_languages({"structure_languages": "java"})
        self.assertEqual(languages, [])
        self.assertEqual(len(warnings), 1)
        self.assertIn("not a list", warnings[0])


class LanguageDetectTests(unittest.TestCase):
    """v3 detect gate (spec 2026-10-20 §7): walk the repo once for the
    extensions of every enabled language — the tier runs when ANY enabled
    language has >= 1 source file. Java keeps its v2 source-root walk (spec
    §4 java row); walk exclusions mirror the analyzer's langs.py table."""

    def test_python_tree_detected(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            (d / "app.py").write_text("print('hi')\n")
            self.assertEqual(perceive.detect_languages(d, ["java", "python"]), ["python"])

    def test_plain_java_detected_without_marker_file(self):
        # v3 flip: the build-marker file is no longer part of the gate —
        # java sources under a discovered source root are enough.
        with TemporaryDirectory() as d:
            d = _plain_java_repo(Path(d))
            (d / "pom.xml").unlink()
            self.assertEqual(perceive.detect_languages(d, ["java"]), ["java"])

    def test_stray_java_outside_source_roots_not_detected(self):
        # java discovery keeps v2 semantics (spec §4: src/main/java roots),
        # unlike the bare-suffix walk the other languages use.
        with TemporaryDirectory() as d:
            d = Path(d)
            (d / "App.java").write_text("public class App {}\n")
            self.assertEqual(perceive.detect_languages(d, ["java"]), [])

    def test_excluded_dirs_skipped(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            for name in ("node_modules", "__pycache__", "venv", ".venv", "dist",
                         "build", "target", "vendor", "testdata", "out", ".git"):
                (d / name).mkdir()
                (d / name / "mod.py").write_text("x = 1\n")
            self.assertEqual(perceive.detect_languages(d, ["python"]), [])

    def test_python_test_trees_skipped(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            (d / "tests" / "deep").mkdir(parents=True)
            (d / "tests" / "deep" / "test_app.py").write_text("def test_x(): pass\n")
            self.assertEqual(perceive.detect_languages(d, ["python"]), [])

    def test_go_test_dirs_still_count(self):
        # spec §4: go does NOT skip test dirs and keeps *_test.go files
        # (in-package methods are first-class).
        with TemporaryDirectory() as d:
            d = Path(d)
            (d / "test").mkdir()
            (d / "test" / "queue_test.go").write_text("package test\n\nfunc TestQ(t *T) {}\n")
            self.assertEqual(perceive.detect_languages(d, ["go"]), ["go"])

    def test_ruby_only_not_detected(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            (d / "app.rb").write_text("class App; end\n")
            self.assertEqual(perceive.detect_languages(d, list(perceive.KNOWN_LANGUAGES)), [])

    def test_mixed_tree_all_six(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            (d / "src/main/java/com/x").mkdir(parents=True)
            (d / "src/main/java/com/x/A.java").write_text("public class A {}\n")
            for name in ("app.py", "mod.js", "widget.jsx", "api.ts", "main.go", "lib.rs"):
                (d / name).write_text("// source\n")
            self.assertEqual(perceive.detect_languages(d, list(perceive.KNOWN_LANGUAGES)),
                             ["go", "java", "javascript", "python", "rust", "typescript"])


class ExtensionTests(unittest.TestCase):
    def test_absent_key_defaults_to_springboot(self):
        extensions, warnings = perceive.resolve_extensions({})
        self.assertEqual(extensions, ["springboot"])
        self.assertEqual(warnings, [])

    def test_empty_list_disables_all(self):
        extensions, warnings = perceive.resolve_extensions({"structure_extensions": []})
        self.assertEqual(extensions, [])
        self.assertEqual(warnings, [])

    def test_unknown_values_warned_and_dropped(self):
        extensions, warnings = perceive.resolve_extensions(
            {"structure_extensions": ["quarkus", "springboot", "quarkus"]})
        self.assertEqual(extensions, ["springboot"])
        self.assertEqual(len(warnings), 2)  # both quarkus occurrences
        self.assertTrue(all("quarkus" in w and "dropped" in w for w in warnings))

    def test_duplicates_deduped(self):
        extensions, warnings = perceive.resolve_extensions(
            {"structure_extensions": ["springboot", "springboot"]})
        self.assertEqual(extensions, ["springboot"])
        self.assertEqual(warnings, [])

    def test_non_list_shape_ignored(self):
        extensions, warnings = perceive.resolve_extensions({"structure_extensions": "springboot"})
        self.assertEqual(extensions, [])
        self.assertEqual(len(warnings), 1)
        self.assertIn("not a list", warnings[0])


class UvProbeTests(unittest.TestCase):
    def test_uv_not_on_path(self):
        with mock.patch.object(perceive.shutil, "which", return_value=None):
            self.assertIsNone(perceive.probe_uv())

    def test_probe_success_returns_uv(self):
        with mock.patch.object(perceive.shutil, "which", return_value="/usr/bin/uv"), \
             mock.patch.object(perceive.subprocess, "run") as run:
            run.return_value = subprocess.CompletedProcess(args=[], returncode=0)
            self.assertEqual(perceive.probe_uv(), "/usr/bin/uv")
        cmd = run.call_args[0][0]
        self.assertEqual(cmd[0], "/usr/bin/uv")
        self.assertTrue(cmd[3].endswith("analyzer"))  # --project perception/analyzer
        self.assertEqual(cmd[-3:], ["python", "-c", "pass"])

    def test_probe_command_failure_proceeds(self):
        # rc != 0 is provisioning trouble, NOT a missing runtime: the probe
        # must return uv so run_analyzer gets the honest chance to fail as
        # analyzer-failed (never uv-missing).
        with mock.patch.object(perceive.shutil, "which", return_value="/usr/bin/uv"), \
             mock.patch.object(perceive.subprocess, "run") as run:
            run.return_value = subprocess.CompletedProcess(args=[], returncode=2, stderr="lock error")
            self.assertEqual(perceive.probe_uv(), "/usr/bin/uv")

    def test_probe_failure_logs_tail(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            with mock.patch.object(perceive.shutil, "which", return_value="/usr/bin/uv"), \
                 mock.patch.object(perceive.subprocess, "run") as run:
                run.return_value = subprocess.CompletedProcess(
                    args=[], returncode=1, stderr="uv: network unreachable")
                self.assertEqual(perceive.probe_uv(d), "/usr/bin/uv")
            log = (d / "perception.log").read_text(encoding="utf-8")
            self.assertIn("uv probe failed", log)
            self.assertIn("network unreachable", log)

    def test_probe_timeout_proceeds(self):
        # same ruling as rc != 0: a hung probe is provisioning trouble, and
        # the exception branch logs its tail just like the rc != 0 branch
        with TemporaryDirectory() as d:
            d = Path(d)
            with mock.patch.object(perceive.shutil, "which", return_value="/usr/bin/uv"), \
                 mock.patch.object(perceive.subprocess, "run",
                                   side_effect=subprocess.TimeoutExpired(cmd="uv", timeout=1)):
                self.assertEqual(perceive.probe_uv(d), "/usr/bin/uv")
            log = (d / "perception.log").read_text(encoding="utf-8")
            self.assertIn("uv probe failed", log)
            self.assertIn("timed out", log.lower())


class FootnoteTests(unittest.TestCase):
    def test_v2_reason_set_exactly(self):
        self.assertEqual(set(perceive.FOOTNOTES),
                         {"uv-missing", "analyzer-failed", "go-missing", "mcp-missing"})

    def test_uv_missing_runtime_wording(self):
        zh, en = perceive.FOOTNOTES["uv-missing"]
        self.assertIn("未检测到 Python 3.11+ 运行环境", zh)
        self.assertIn("重新运行 /scv run", zh)
        self.assertIn("Python 3.11+ runtime not found", en)
        self.assertIn("re-run /scv run", en)
        for text in (zh, en):  # vocabulary ruling: the runtime is named, never the tool
            self.assertNotIn("uv", text)

    def test_analyzer_failed_wording_verbatim(self):
        zh, en = perceive.FOOTNOTES["analyzer-failed"]
        self.assertEqual(zh, "已跳过深度结构分析：分析未完成（详情见 perception.log）")
        self.assertEqual(en, "Deep structural analysis skipped: analysis incomplete (see perception.log)")

    def test_v1_reasons_retired(self):
        for gone in ("jdk-missing", "adapter-build-failed", "adapter-failed", "degraded-noclasspath"):
            self.assertNotIn(gone, perceive.FOOTNOTES)

    def test_footnote_single_line_stacking(self):
        got = perceive.render_footnote(["uv-missing", "go-missing", "mcp-missing"], "zh")
        self.assertEqual(got.count("\n"), 0)
        self.assertIn("；", got)


class PureTests(unittest.TestCase):
    def test_stale_cleanup(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            for f in ("structure.json", "structure.vg", "perception.log"):
                (d / f).write_text("stale")
            perceive.cleanup_stale(d)
            for f in ("structure.json", "structure.vg", "perception.log"):
                self.assertFalse((d / f).exists())


class CountsTests(unittest.TestCase):
    def test_label_filtered_counts_and_structure(self):
        counts = perceive.build_counts(FAKE_GRAPH)
        self.assertEqual(counts["components"], 3)  # Bean nodes only, not len(nodes)=6
        self.assertEqual(counts["edges"], 2)       # INJECTS edges only, not len(edges)=4
        self.assertEqual(counts["structure"],
                         {"files": 7, "types": 8, "methods": 9, "call_edges": 4})

    def test_structure_mode_graph(self):
        graph = {
            "nodes": [{"key": "type:com.x.A", "label": "Type"},
                      {"key": "fn:com.x.A#run()", "label": "Method"}],
            "edges": [{"srcKey": "fn:com.x.A#run()", "dstKey": "fn:com.x.B#g()", "label": "CALLS"}],
            "unresolved": 0,
            "meta": {"structure": {"files": 3, "types": 3, "methods": 4, "call_edges": 2,
                                   "dropped_edges": 0, "unresolved_refs": 0, "key_collisions": 0}},
        }
        counts = perceive.build_counts(graph)
        self.assertEqual((counts["components"], counts["edges"]), (0, 0))
        self.assertEqual(counts["structure"],
                         {"files": 3, "types": 3, "methods": 4, "call_edges": 2})

    def test_structure_summary_wording_multilang(self):
        # v3 spec §7: multi-language aggregate + language list.
        st = {"files": 33, "types": 34, "methods": 52, "call_edges": 19}
        self.assertEqual(perceive.render_structure_summary(st, "zh", ["java", "python"]),
                         "33 个文件、34 个类型、52 个方法、19 条调用边（java、python）")
        self.assertEqual(perceive.render_structure_summary(st, "en", ["java", "python"]),
                         "33 files, 34 types, 52 methods, 19 call edges (java, python)")

    def test_structure_summary_without_languages(self):
        # v2-shaped analyzer output (no meta.languages): the wording degrades
        # to the aggregate without the language clause — never a crash.
        st = {"files": 3, "types": 3, "methods": 4, "call_edges": 2}
        self.assertEqual(perceive.render_structure_summary(st, "en"),
                         "3 files, 3 types, 4 methods, 2 call edges")
        self.assertEqual(perceive.render_structure_summary(st, "zh", []),
                         "3 个文件、3 个类型、4 个方法、2 条调用边")

    def test_languages_passthrough(self):
        # counts.languages mirrors analyzer meta.languages (v3 §8); a v2-shaped
        # graph without the key degrades to [].
        counts = perceive.build_counts(FAKE_GRAPH)
        self.assertEqual(counts["structure"],
                         {"files": 7, "types": 8, "methods": 9, "call_edges": 4})
        self.assertEqual(counts["languages"], ["java"])
        legacy = json.loads(json.dumps(FAKE_GRAPH))
        del legacy["meta"]["languages"]
        self.assertEqual(perceive.build_counts(legacy)["languages"], [])


class RunAnalyzerTests(unittest.TestCase):
    """In-process run_analyzer contract under the SCV_PERCEPTION_ANALYZER
    override (direct invocation, duration + stderr-tail logging every run)."""

    def test_ok_run_logs_duration_and_stderr(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            fake = _write_fake(d / "fake-analyzer", "ok")
            repo = _plain_java_repo(d / "repo")
            out = d / "out" / "structure.json"
            (d / "out").mkdir()
            with mock.patch.dict(os.environ, {"SCV_PERCEPTION_ANALYZER": str(fake)}):
                self.assertTrue(perceive.run_analyzer(repo, out, d / "out", ["springboot"]))
            self.assertTrue(out.exists())
            log = (d / "out" / "perception.log").read_text(encoding="utf-8")
            self.assertIn("tier3 analyzer rc=0 duration=", log)
            self.assertIn("extensions=['springboot']", log)
            self.assertIn("stderr: fake analyzer: non-fatal stderr noise", log)
            argv = json.loads((d / "out" / "structure.json.argv").read_text())
            self.assertEqual(argv, [str(repo), str(out), "--extensions", "springboot"])

    def test_failing_run_returns_false(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            fake = _write_fake(d / "fake-analyzer", "fail")
            repo = _plain_java_repo(d / "repo")
            (d / "out").mkdir()
            with mock.patch.dict(os.environ, {"SCV_PERCEPTION_ANALYZER": str(fake)}):
                self.assertFalse(perceive.run_analyzer(repo, d / "out" / "structure.json", d / "out", []))
            log = (d / "out" / "perception.log").read_text(encoding="utf-8")
            self.assertIn("tier3 analyzer rc=3", log)
            self.assertIn("boom", log)

    def test_exit_zero_without_output_returns_false(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            fake = _write_fake(d / "fake-analyzer", "noout")
            repo = _plain_java_repo(d / "repo")
            (d / "out").mkdir()
            with mock.patch.dict(os.environ, {"SCV_PERCEPTION_ANALYZER": str(fake)}):
                self.assertFalse(perceive.run_analyzer(repo, d / "out" / "structure.json", d / "out", []))
            log = (d / "out" / "perception.log").read_text(encoding="utf-8")
            self.assertIn("out=no", log)

    def test_no_extensions_passes_no_flag(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            fake = _write_fake(d / "fake-analyzer", "ok")
            repo = _plain_java_repo(d / "repo")
            (d / "out").mkdir()
            out = d / "out" / "structure.json"
            with mock.patch.dict(os.environ, {"SCV_PERCEPTION_ANALYZER": str(fake)}):
                self.assertTrue(perceive.run_analyzer(repo, out, d / "out", []))
            argv = json.loads(Path(str(out) + ".argv").read_text())
            self.assertEqual(argv, [str(repo), str(out)])

    def test_languages_passed_through(self):
        # v3 §7: perceive feeds the analyzer --languages from config ∩ detected.
        with TemporaryDirectory() as d:
            d = Path(d)
            fake = _write_fake(d / "fake-analyzer", "ok")
            repo = _plain_java_repo(d / "repo")
            (d / "out").mkdir()
            out = d / "out" / "structure.json"
            with mock.patch.dict(os.environ, {"SCV_PERCEPTION_ANALYZER": str(fake)}):
                self.assertTrue(perceive.run_analyzer(repo, out, d / "out", [], ["java"]))
            argv = json.loads(Path(str(out) + ".argv").read_text())
            self.assertEqual(argv, [str(repo), str(out), "--languages", "java"])
            log = (d / "out" / "perception.log").read_text(encoding="utf-8")
            self.assertIn("languages=['java']", log)

    def test_launch_error_returns_false(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            repo = _plain_java_repo(d / "repo")
            (d / "out").mkdir()
            with mock.patch.dict(os.environ, {"SCV_PERCEPTION_ANALYZER": str(d / "missing-exe")}):
                self.assertFalse(perceive.run_analyzer(repo, d / "out" / "structure.json", d / "out", []))
            log = (d / "out" / "perception.log").read_text(encoding="utf-8")
            self.assertIn("tier3 analyzer launch failed", log)

    def test_uv_invocation_uses_absolute_analyzer_path(self):
        # `uv run --project` keeps the caller's CWD: the analyzer.py path must
        # be absolute or the run fails with "can't open file".
        with TemporaryDirectory() as d:
            d = Path(d)
            repo = _plain_java_repo(d / "repo")
            (d / "out").mkdir()
            out = d / "out" / "structure.json"

            def fake_run(cmd, **kwargs):
                out.write_text("{}")
                return subprocess.CompletedProcess(args=cmd, returncode=0)

            env = {k: v for k, v in os.environ.items() if k != "SCV_PERCEPTION_ANALYZER"}
            with mock.patch.dict(os.environ, env, clear=True), \
                 mock.patch.object(perceive.subprocess, "run", side_effect=fake_run) as run:
                self.assertTrue(perceive.run_analyzer(repo, out, d / "out", ["springboot"]))
            cmd = run.call_args[0][0]
            self.assertEqual(cmd[:2], ["uv", "run"])
            self.assertEqual(cmd[2], "--project")
            self.assertTrue(cmd[3].endswith("analyzer"), cmd)
            self.assertEqual(cmd[4], "python")
            self.assertTrue(cmd[5].endswith("analyzer.py"), cmd)
            self.assertTrue(Path(cmd[5]).is_absolute(), cmd)
            self.assertEqual(cmd[-2:], ["--extensions", "springboot"])


class EndToEndTests(unittest.TestCase):
    """Subprocess runs of perceive.py v2 with fakes + controlled HOME/PATH."""

    def test_fake_ok_remote_source(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            fake = _write_fake(d / "fake-analyzer", "ok")
            repo = _springboot_repo(d / "repo")
            out = d / "analysis"
            res = _run_perceive(repo, out, cfg={}, env_extra={"SCV_PERCEPTION_ANALYZER": str(fake)},
                                extra_args=["--source", "remote", "--lang", "en"])
            self.assertEqual(res.returncode, 0, res.stderr)
            self.assertEqual(len(res.stdout.strip().splitlines()), 1)  # single-line contract
            payload = _payload(res)
            self.assertEqual(payload["status"], "ok")
            self.assertTrue(payload["tiers"]["json"])
            self.assertEqual(payload["counts"]["components"], 3)
            self.assertEqual(payload["counts"]["edges"], 2)
            self.assertEqual(payload["counts"]["structure"],
                             {"files": 7, "types": 8, "methods": 9, "call_edges": 4})
            self.assertEqual(payload["counts"]["languages"], ["java"])
            self.assertEqual(payload["structure_summary"],
                             "7 files, 8 types, 9 methods, 4 call edges (java)")
            self.assertEqual((out / "structure.json").read_text(encoding="utf-8")[:1], "{")
            log = (out / "perception.log").read_text(encoding="utf-8")
            self.assertIn("source=remote", log)          # --source still audited
            self.assertIn("extensions=['springboot']", log)
            self.assertIn("duration=", log)
            argv = json.loads(Path(str(out / "structure.json") + ".argv").read_text())
            self.assertEqual(argv[argv.index("--extensions") + 1], "springboot")
            self.assertEqual(argv[argv.index("--languages") + 1], "java")

    def test_failing_fake_footnote_and_exit_zero(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            fake = _write_fake(d / "fake-analyzer", "fail")
            repo = _springboot_repo(d / "repo")
            out = d / "analysis"
            res = _run_perceive(repo, out, cfg={}, env_extra={"SCV_PERCEPTION_ANALYZER": str(fake)})
            self.assertEqual(res.returncode, 0, res.stderr)
            payload = _payload(res)
            self.assertEqual(payload["status"], "skipped")
            self.assertEqual(payload["reason"], "analyzer-failed")
            self.assertEqual(payload["footnote"],
                             "Deep structural analysis skipped: analysis incomplete (see perception.log)")
            self.assertFalse((out / "structure.json").exists())

    def test_garbage_output_is_analyzer_failed(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            fake = _write_fake(d / "fake-analyzer", "garbage")
            repo = _springboot_repo(d / "repo")
            out = d / "analysis"
            res = _run_perceive(repo, out, cfg={}, env_extra={"SCV_PERCEPTION_ANALYZER": str(fake)})
            self.assertEqual(res.returncode, 0, res.stderr)
            payload = _payload(res)
            self.assertEqual(payload["reason"], "analyzer-failed")
            log = (out / "perception.log").read_text(encoding="utf-8")
            self.assertIn("not a usable graph", log)

    def test_uv_missing_footnote_and_exit_zero(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            repo = _springboot_repo(d / "repo")
            out = d / "analysis"
            # minimal PATH has no uv; no analyzer override -> uv-missing gate
            res = _run_perceive(repo, out, cfg={})
            self.assertEqual(res.returncode, 0, res.stderr)
            payload = _payload(res)
            self.assertEqual(payload["status"], "skipped")
            self.assertEqual(payload["reason"], "uv-missing")
            self.assertIn("Python 3.11+ runtime not found", payload["footnote"])
            self.assertIn("re-run /scv run", payload["footnote"])
            self.assertFalse((out / "structure.json").exists())
            log = (out / "perception.log").read_text(encoding="utf-8")
            self.assertIn("uv probe: not found", log)
            self.assertNotIn("uv probe failed", log)  # absent ≠ provisioning failure

    def test_uv_probe_failure_shim_is_analyzer_failed(self):
        # uv PRESENT but `uv run` provisioning fails (network/lock): the run
        # must NOT report uv-missing — it proceeds to the analyzer tier, which
        # fails through the same shim and lands on analyzer-failed with both
        # tails in perception.log (folded T5-review fix).
        with TemporaryDirectory() as d:
            d = Path(d)
            shim_dir = d / "shim"
            shim_dir.mkdir()
            shim = shim_dir / "uv"
            shim.write_text('#!/bin/sh\necho "shim uv: lock unavailable (network down)" >&2\nexit 1\n')
            shim.chmod(0o755)
            repo = _springboot_repo(d / "repo")
            out = d / "analysis"
            res = _run_perceive(repo, out, cfg={},
                                env_extra={"PATH": f"{shim_dir}:{MINIMAL_PATH}"})
            self.assertEqual(res.returncode, 0, res.stderr)
            payload = _payload(res)
            self.assertEqual(payload["status"], "skipped")
            self.assertEqual(payload["reason"], "analyzer-failed")
            self.assertIn("analysis incomplete (see perception.log)", payload["footnote"])
            self.assertNotIn("runtime not found", payload["footnote"])
            self.assertFalse((out / "structure.json").exists())
            log = (out / "perception.log").read_text(encoding="utf-8")
            self.assertIn("uv probe failed", log)       # provisioning tail logged
            self.assertIn("lock unavailable", log)
            self.assertIn("tier3 analyzer rc=1", log)   # and the analyzer run's tail

    def test_structure_mode_plain_java(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            fake = _write_fake(d / "fake-analyzer", "ok")
            repo = _plain_java_repo(d / "repo")  # no spring content anywhere
            out = d / "analysis"
            res = _run_perceive(repo, out, cfg={"structure_extensions": []},
                                env_extra={"SCV_PERCEPTION_ANALYZER": str(fake)})
            self.assertEqual(res.returncode, 0, res.stderr)
            payload = _payload(res)
            self.assertEqual(payload["status"], "ok")
            self.assertEqual(payload["counts"]["components"], 0)
            self.assertEqual(payload["counts"]["edges"], 0)
            self.assertEqual(payload["structure_summary"],
                             "7 files, 8 types, 9 methods, 4 call edges (java)")
            log = (out / "perception.log").read_text(encoding="utf-8")
            self.assertIn("languages=['java']", log)
            self.assertIn("extensions=[]", log)
            argv = json.loads(Path(str(out / "structure.json") + ".argv").read_text())
            self.assertNotIn("--extensions", argv)
            self.assertEqual(argv[argv.index("--languages") + 1], "java")

    def test_plain_java_with_extensions_on_runs_structure_mode(self):
        # v3 §7 flip: extensions on but no Spring markers no longer yields
        # not-framework — the structure tier engages, only the bean layer
        # stays off (the springboot extension's detection is independent).
        with TemporaryDirectory() as d:
            d = Path(d)
            fake = _write_fake(d / "fake-analyzer", "ok")
            repo = _plain_java_repo(d / "repo")
            out = d / "analysis"
            res = _run_perceive(repo, out, cfg={}, env_extra={"SCV_PERCEPTION_ANALYZER": str(fake)})
            self.assertEqual(res.returncode, 0, res.stderr)
            payload = _payload(res)
            self.assertEqual(payload["status"], "ok")
            self.assertEqual(payload["counts"]["components"], 0)
            self.assertEqual(payload["structure_summary"],
                             "7 files, 8 types, 9 methods, 4 call edges (java)")
            argv = json.loads(Path(str(out / "structure.json") + ".argv").read_text())
            self.assertNotIn("--extensions", argv)
            self.assertEqual(argv[argv.index("--languages") + 1], "java")

    def test_python_tree_structure_mode(self):
        # v3: a python-only repo runs the structure tier under the DEFAULT
        # config — python is enabled by default, spring is undetected.
        with TemporaryDirectory() as d:
            d = Path(d)
            fake = _write_fake(d / "fake-analyzer", "ok")
            repo = _python_repo(d / "repo")
            out = d / "analysis"
            res = _run_perceive(repo, out, cfg={}, env_extra={"SCV_PERCEPTION_ANALYZER": str(fake)})
            self.assertEqual(res.returncode, 0, res.stderr)
            payload = _payload(res)
            self.assertEqual(payload["status"], "ok")
            self.assertEqual(payload["counts"]["components"], 0)
            self.assertEqual(payload["counts"]["languages"], ["python"])
            self.assertEqual(payload["structure_summary"],
                             "7 files, 8 types, 9 methods, 4 call edges (python)")
            argv = json.loads(Path(str(out / "structure.json") + ".argv").read_text())
            self.assertNotIn("--extensions", argv)
            self.assertEqual(argv[argv.index("--languages") + 1], "python")

    def test_python_tree_with_java_only_config_is_not_framework(self):
        # gate matrix (spec §11.5): structure_languages ["java"] on a
        # python-only repo -> no enabled language present -> not-framework.
        with TemporaryDirectory() as d:
            d = Path(d)
            fake = _write_fake(d / "fake-analyzer", "ok")
            repo = _python_repo(d / "repo")
            out = d / "analysis"
            res = _run_perceive(repo, out, cfg={"structure_languages": ["java"]},
                                env_extra={"SCV_PERCEPTION_ANALYZER": str(fake)})
            self.assertEqual(res.returncode, 0, res.stderr)
            payload = _payload(res)
            self.assertEqual(payload["status"], "not-framework")
            self.assertIsNone(payload["reason"])
            self.assertFalse((out / "structure.json").exists())
            log = (out / "perception.log").read_text(encoding="utf-8")
            self.assertIn("tier1 detect: not-framework", log)

    def test_empty_language_list_disables_tier(self):
        # v3 §7: empty structure_languages = structure tier fully off — the
        # same clean skip as structure_analysis_enabled=false, no footnote.
        with TemporaryDirectory() as d:
            d = Path(d)
            fake = _write_fake(d / "fake-analyzer", "ok")
            repo = _springboot_repo(d / "repo")
            out = d / "analysis"
            res = _run_perceive(repo, out, cfg={"structure_languages": []},
                                env_extra={"SCV_PERCEPTION_ANALYZER": str(fake)})
            self.assertEqual(res.returncode, 0, res.stderr)
            payload = _payload(res)
            self.assertEqual(payload["status"], "skipped")
            self.assertEqual(payload["reason"], "disabled")
            self.assertEqual(payload["footnote"], "")
            self.assertFalse((out / "structure.json").exists())
            log = (out / "perception.log").read_text(encoding="utf-8")
            self.assertIn("structure_languages", log)

    def test_unknown_language_warned_logged_and_ignored(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            fake = _write_fake(d / "fake-analyzer", "ok")
            repo = _springboot_repo(d / "repo")
            out = d / "analysis"
            res = _run_perceive(repo, out,
                                cfg={"structure_languages": ["java", "kotlin", "java"]},
                                env_extra={"SCV_PERCEPTION_ANALYZER": str(fake)})
            self.assertEqual(res.returncode, 0, res.stderr)
            payload = _payload(res)
            self.assertEqual(payload["status"], "ok")
            log = (out / "perception.log").read_text(encoding="utf-8")
            self.assertIn("kotlin", log)
            self.assertIn("ignored", log)
            argv = json.loads(Path(str(out / "structure.json") + ".argv").read_text())
            self.assertEqual(argv[argv.index("--languages") + 1], "java")
            self.assertNotIn("kotlin", argv)

    def test_legacy_fake_without_languages_field(self):
        # backwards compat: a v2-shaped analyzer graph (no meta.languages,
        # no by_lang) must not crash the run — the summary degrades to the
        # aggregate without the language clause.
        with TemporaryDirectory() as d:
            d = Path(d)
            legacy = json.loads(json.dumps(FAKE_GRAPH))
            del legacy["meta"]["languages"]
            del legacy["meta"]["structure"]["by_lang"]
            fake = _write_fake(d / "fake-analyzer", "ok", graph=legacy)
            repo = _plain_java_repo(d / "repo")
            out = d / "analysis"
            res = _run_perceive(repo, out, cfg={"structure_extensions": []},
                                env_extra={"SCV_PERCEPTION_ANALYZER": str(fake)})
            self.assertEqual(res.returncode, 0, res.stderr)
            payload = _payload(res)
            self.assertEqual(payload["status"], "ok")
            self.assertEqual(payload["counts"]["languages"], [])
            self.assertEqual(payload["structure_summary"],
                             "7 files, 8 types, 9 methods, 4 call edges")

    def test_unknown_extension_warned_dropped_and_logged(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            fake = _write_fake(d / "fake-analyzer", "ok")
            repo = _springboot_repo(d / "repo")
            out = d / "analysis"
            res = _run_perceive(repo, out, cfg={"structure_extensions": ["springboot", "micronaut"]},
                                env_extra={"SCV_PERCEPTION_ANALYZER": str(fake)})
            self.assertEqual(res.returncode, 0, res.stderr)
            payload = _payload(res)
            self.assertEqual(payload["status"], "ok")
            log = (out / "perception.log").read_text(encoding="utf-8")
            self.assertIn("micronaut", log)
            self.assertIn("dropped", log)
            argv = json.loads(Path(str(out / "structure.json") + ".argv").read_text())
            self.assertEqual(argv[argv.index("--extensions") + 1], "springboot")
            self.assertNotIn("micronaut", argv)

    def test_disabled_by_config(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            repo = _springboot_repo(d / "repo")
            out = d / "analysis"
            res = _run_perceive(repo, out, cfg={"structure_analysis_enabled": False})
            payload = _payload(res)
            self.assertEqual(payload["status"], "skipped")
            self.assertEqual(payload["reason"], "disabled")

    def test_unknown_language_logged_on_not_framework_path(self):
        # T8 acceptance item 5 fallout (spec §7: unknown values warn into
        # perception.log): the not-framework early return must still flush
        # the config warnings — a typo'd language plus a repo with no
        # enabled language must not swallow the clue.
        with TemporaryDirectory() as d:
            d = Path(d)
            repo = _python_repo(d / "repo")  # python tree, java-only config
            out = d / "analysis"
            res = _run_perceive(repo, out,
                                cfg={"structure_languages": ["java", "kotlin"]})
            self.assertEqual(res.returncode, 0, res.stderr)
            payload = _payload(res)
            self.assertEqual(payload["status"], "not-framework")
            log = (out / "perception.log").read_text(encoding="utf-8")
            self.assertIn("unknown value 'kotlin'", log)
            self.assertIn("ignored", log)
            self.assertFalse((out / "structure.json").exists())

    def test_unknown_language_logged_on_tier_off_path(self):
        # same flush contract for the tier-off (empty-after-validation) path
        with TemporaryDirectory() as d:
            d = Path(d)
            repo = _python_repo(d / "repo")
            out = d / "analysis"
            res = _run_perceive(repo, out,
                                cfg={"structure_languages": ["kotlin"]})
            self.assertEqual(res.returncode, 0, res.stderr)
            payload = _payload(res)
            self.assertEqual(payload["status"], "skipped")
            self.assertEqual(payload["reason"], "disabled")
            log = (out / "perception.log").read_text(encoding="utf-8")
            self.assertIn("unknown value 'kotlin'", log)
            self.assertFalse((out / "structure.json").exists())


if __name__ == "__main__":
    unittest.main()
