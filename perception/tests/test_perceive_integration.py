"""perceive.py v2 integration suite (spec 2026-10-14 §7/§8; plan Task 7).

Real-chain runs against the actual uv + tree-sitter analyzer — no fakes:
the full ladder detect -> uv analyzer -> loader -> graphmcp over frozen
fixture COPIES (temp dirs, nested build//.gradle stripped so the post-run
"nothing was built inside the repo" assertion has teeth), with a temp
SCV_PERCEPTION_DIST and a temp HOME (config control).

local and remote are identical by construction in v2 — no build ever
executes inside the analyzed repo — so the remote run asserts payload
equality (not just absence of build execution). No JDK/gradle is involved
anywhere on the v2 path: the skip guard is uv (+ go for the vg/mcp tiers).
"""
import json
import os
import re
import shutil
import subprocess
import sys
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

ROOT = Path(__file__).resolve().parents[1]
FIXTURES = ROOT / "adapters/springboot/src/test/resources/fixtures"
PLAIN_JAVA = ROOT / "analyzer/tests/fixtures/plain-java"


def _tool(name: str) -> bool:
    return shutil.which(name) is not None


def _copy_fixture(src_root: Path, name: str, dst: Path) -> None:
    """Copy a fixture tree, stripping nested build//.gradle state so (a) the
    copy starts clean and (b) any such dir found after a run was created by
    the run (v1 ran gradle inside the repo; v2 must not)."""
    src = src_root / name
    for f in src.rglob("*"):
        if not f.is_file():
            continue
        rel = f.relative_to(src)
        if any(part in ("build", ".gradle") for part in rel.parts):
            continue
        target = dst / rel
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(f.read_bytes())


def _path_without(tool: str) -> str:
    """The parent PATH minus the directory holding `tool` (a no-<tool>
    environment that still has everything else, e.g. go)."""
    hit = shutil.which(tool)
    drop = Path(hit).resolve().parent if hit else None
    parts = []
    for entry in os.environ.get("PATH", "").split(os.pathsep):
        if not entry:
            continue
        if drop is not None:
            try:
                if Path(entry).resolve() == drop:
                    continue
            except OSError:
                pass
        parts.append(entry)
    return os.pathsep.join(parts)


def _chain_env(home: Path, dist: Path, cfg=None, path=None):
    """Subprocess env: temp HOME (config control) + temp dist; uv's cache
    keeps pointing at the real one so runs stay warm under the temp HOME."""
    env = {**os.environ, "HOME": str(home), "SCV_PERCEPTION_DIST": str(dist)}
    env.pop("SCV_PERCEPTION_ANALYZER", None)  # the real analyzer, never a fake
    if path is not None:
        env["PATH"] = path
    real_cache = Path.home() / ".cache" / "uv"
    if real_cache.is_dir():
        env.setdefault("UV_CACHE_DIR", str(real_cache))
    # same for Go's caches: a cold temp-HOME module cache would re-download
    # (slow, and a network flake turns into a false tier failure)
    for var, default in (("GOCACHE", "Library/Caches/go-build"),
                         ("GOMODCACHE", "go/pkg/mod")):
        cache = Path.home() / default
        if cache.is_dir():
            env.setdefault(var, str(cache))
    if cfg is not None:
        (home / ".scv").mkdir(parents=True, exist_ok=True)
        (home / ".scv" / "config.json").write_text(json.dumps(cfg))
    return env


def _perceive(repo: Path, out: Path, env, source="local"):
    return subprocess.run(
        [sys.executable, str(ROOT / "perceive.py"), str(repo), str(out),
         "--source", source, "--lang", "en"],
        capture_output=True, text=True, timeout=900, env=env)


def _payload(res: subprocess.CompletedProcess) -> dict:
    return json.loads(res.stdout.strip().splitlines()[-1])


def _assert_no_build_ran(repo: Path) -> None:
    """v2 contract: nothing ever executes inside the analyzed repo — assert
    no build/ or .gradle/ dir appeared anywhere in the copy."""
    offenders = [p for p in repo.rglob("*")
                 if p.is_dir() and p.name in ("build", ".gradle")]
    assert not offenders, f"build state created inside analyzed repo: {offenders}"


@unittest.skipUnless(_tool("uv") and _tool("go"), "uv/go not on PATH")
class RealChainTests(unittest.TestCase):
    """Full ladder on torture-app through the REAL analyzer via uv."""

    def test_local_torture_run(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            _copy_fixture(FIXTURES, "torture-app", d / "repo")
            out = d / "analysis"
            res = _perceive(d / "repo", out, _chain_env(d / "home", d / "dist", cfg={}))
            self.assertEqual(res.returncode, 0, res.stderr)
            self.assertEqual(len(res.stdout.strip().splitlines()), 1)  # single-line contract
            payload = _payload(res)
            self.assertEqual(payload["status"], "ok")
            self.assertIsNone(payload["reason"])
            self.assertEqual(payload["tiers"], {"json": True, "vg": True, "mcp_hint": True})
            # v1-frozen component ground truth + label-filtered counts (§6)
            self.assertEqual(payload["counts"]["components"], 30)
            self.assertEqual(payload["counts"]["edges"], 24)
            self.assertEqual(payload["counts"]["cycles_fatal"], 0)
            self.assertEqual(payload["counts"]["cycles_tolerated"], 1)  # circularA<->B, field-injected
            # structure sub-counts (§8.3; methods pinned to the fixture's true
            # syntactic count — spec erratum: ">= 80" is wrong, it is exactly 52)
            st = payload["counts"]["structure"]
            self.assertEqual(st["files"], 33)
            self.assertEqual(st["types"], 34)
            self.assertEqual(st["methods"], 52)
            self.assertTrue(10 <= st["call_edges"] <= 40, st)
            self.assertRegex(payload["structure_summary"], r"^\d+ types, \d+ methods, \d+ call edges$")
            # artifacts + tier log lines
            for key in ("structure_json", "structure_vg"):
                self.assertTrue(Path(payload["artifacts"][key]).is_file())
            log = (out / "perception.log").read_text(encoding="utf-8")
            self.assertIn("tier1 detect: springboot, source=local", log)
            self.assertIn("tier2 uv probe:", log)
            self.assertIn("tier3 analyzer rc=0 duration=", log)
            self.assertIn("extensions=['springboot']", log)
            _assert_no_build_ran(d / "repo")

    def test_remote_equals_local(self):
        # v2 security model: no build executes inside the repo, so remote and
        # local analyze identically (--source is audit-only). Same fixture,
        # same config, separate outputs — payloads must match apart from the
        # artifacts/mcp_add strings that embed temp paths and timestamps.
        with TemporaryDirectory() as d:
            d = Path(d)
            env = _chain_env(d / "home", d / "dist", cfg={})
            payloads = {}
            for source in ("local", "remote"):
                repo = d / f"repo-{source}"
                _copy_fixture(FIXTURES, "torture-app", repo)
                out = d / f"analysis-{source}"
                res = _perceive(repo, out, env, source=source)
                self.assertEqual(res.returncode, 0, res.stderr)
                payloads[source] = _payload(res)
                self.assertEqual(payloads[source]["status"], "ok")
                _assert_no_build_ran(repo)
            normalize = lambda p: {k: v for k, v in p.items()
                                   if k not in ("artifacts", "mcp_add")}
            self.assertEqual(normalize(payloads["local"]), normalize(payloads["remote"]))
            for source in ("local", "remote"):
                log = (d / f"analysis-{source}" / "perception.log").read_text(encoding="utf-8")
                self.assertIn(f"source={source}", log)  # still recorded for audit


@unittest.skipUnless(_tool("uv"), "uv not on PATH")
class StructureModeTests(unittest.TestCase):
    """Extension gate (§8.5): structure_extensions off + plain Java."""

    def test_extension_off_structure_mode(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            _copy_fixture(FIXTURES, "torture-app", d / "repo")
            out = d / "analysis"
            res = _perceive(d / "repo", out,
                            _chain_env(d / "home", d / "dist", cfg={"structure_extensions": []}))
            self.assertEqual(res.returncode, 0, res.stderr)
            payload = _payload(res)
            self.assertEqual(payload["status"], "ok")
            self.assertEqual(payload["counts"]["components"], 0)  # no Bean layer
            self.assertEqual(payload["counts"]["edges"], 0)       # no INJECTS layer
            # structure layer still fully present
            st = payload["counts"]["structure"]
            self.assertEqual((st["files"], st["types"], st["methods"]), (33, 34, 52))
            self.assertTrue(10 <= st["call_edges"] <= 40, st)
            self.assertRegex(payload["structure_summary"], r"^\d+ types, \d+ methods, \d+ call edges$")
            graph = json.loads((out / "structure.json").read_text(encoding="utf-8"))
            self.assertNotIn("Bean", {n["label"] for n in graph["nodes"]})
            self.assertNotIn("INJECTS", {e["label"] for e in graph["edges"]})
            self.assertIn("Type", {n["label"] for n in graph["nodes"]})
            self.assertIn("Method", {n["label"] for n in graph["nodes"]})
            log = (out / "perception.log").read_text(encoding="utf-8")
            self.assertIn("java (marker-only)", log)
            self.assertIn("extensions=[]", log)
            self.assertEqual(graph["meta"]["extensions"], [])

    def test_plain_java_fixture_structure_mode(self):
        # non-Spring Java tree: marker-file detect (pom.xml + sources, no
        # spring content) -> structure mode, status ok (§8.5)
        with TemporaryDirectory() as d:
            d = Path(d)
            _copy_fixture(PLAIN_JAVA.parent, "plain-java", d / "repo")
            out = d / "analysis"
            res = _perceive(d / "repo", out,
                            _chain_env(d / "home", d / "dist", cfg={"structure_extensions": []}))
            self.assertEqual(res.returncode, 0, res.stderr)
            payload = _payload(res)
            self.assertEqual(payload["status"], "ok")
            self.assertEqual(payload["counts"]["components"], 0)
            self.assertEqual(payload["counts"]["structure"],
                             {"files": 3, "types": 3, "methods": 4, "call_edges": 2})
            self.assertEqual(payload["structure_summary"], "3 types, 4 methods, 2 call edges")


class DegradedPathTests(unittest.TestCase):
    """Toolchain-degradation paths that need no toolchain to test."""

    def test_not_framework_silent(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            out = d / "analysis"
            res = _perceive(d, out, _chain_env(d / "home", d / "dist", cfg={}))
            self.assertEqual(res.returncode, 0, res.stderr)
            self.assertEqual(len(res.stdout.strip().splitlines()), 1)
            payload = _payload(res)
            self.assertEqual(payload["status"], "not-framework")
            self.assertIsNone(payload["reason"])
            self.assertFalse(payload["tiers"]["json"])
            # silent: no tiers ran, no artifacts, and the log records only
            # the detect decision (no probe/analyzer/loader noise)
            self.assertFalse((out / "structure.json").exists())
            log_lines = [line.split(" ", 1)[1] for line in
                         (out / "perception.log").read_text(encoding="utf-8").splitlines()]
            self.assertEqual(log_lines, ["tier1 detect: not-framework"])

    def test_uv_missing_footnote_and_exit_zero(self):
        # PATH without uv (everything else intact): uv-missing footnote,
        # exit 0, and a clean "not found" probe line (no provisioning error).
        with TemporaryDirectory() as d:
            d = Path(d)
            _copy_fixture(FIXTURES, "torture-app", d / "repo")
            out = d / "analysis"
            res = _perceive(d / "repo", out,
                            _chain_env(d / "home", d / "dist", cfg={},
                                       path=_path_without("uv")))
            self.assertEqual(res.returncode, 0, res.stderr)
            payload = _payload(res)
            self.assertEqual(payload["status"], "skipped")
            self.assertEqual(payload["reason"], "uv-missing")
            self.assertIn("Python 3.11+ runtime not found", payload["footnote"])
            self.assertFalse((out / "structure.json").exists())
            log = (out / "perception.log").read_text(encoding="utf-8")
            self.assertIn("uv probe: not found", log)
            self.assertNotIn("uv probe failed", log)


if __name__ == "__main__":
    unittest.main()
