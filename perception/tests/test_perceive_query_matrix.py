"""Full-chain query matrix (v2): every fixture (Spring Boot 2 + 3) through
perceive v2 (uv + tree-sitter, the REAL chain) -> structure.vg ->
scv-graph-mcp, then Cypher stereotype queries asserted against the frozen
hand-computed ground truth — plus structure-layer queries (Type/Method/
IMPLEMENTS/CALLS) proving the v2 surface is queryable end to end.

One scv-graph-mcp instance serves ALL repositories in the analysis root —
this is the "repo + graph query language" contract end to end.
"""
from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
import time
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

ROOT = Path(__file__).resolve().parents[1]
FIXTURES = ROOT / "adapters/springboot/src/test/resources/fixtures"


def _tool(name: str) -> bool:
    return shutil.which(name) is not None


def _copy_fixture(name: str, dst: Path) -> None:
    src = FIXTURES / name
    for f in src.rglob("*"):
        if not f.is_file():
            continue
        rel = f.relative_to(src)
        if any(part in ("build", ".gradle") for part in rel.parts):
            continue
        target = dst / rel
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(f.read_bytes())


# Per-fixture stereotype census (hand-computed from the fixture sources;
# unchanged from the v1 matrix — same ground truth, new engine).
GROUND_TRUTH = {
    "gradle-lombok-sample": {
        "RestController": 1, "Service": 2, "Configuration": 2,
        "Repository": 1, "Component": 4, "@Bean": 2,
    },
    "torture-app": {
        "RestController": 1, "Service": 7, "Configuration": 2,
        "Repository": 1, "Component": 13, "@Bean": 4, "ConfigurationProperties": 1,
    },
    "boot2-legacy": {
        "Controller": 1, "ControllerAdvice": 1, "Configuration": 3,
        "Service": 2, "Repository": 1, "Component": 4, "@Bean": 3,
    },
    "boot3-modern": {
        "Configuration": 3, "Service": 1, "Component": 6,
        "Repository": 1, "ConfigurationProperties": 1,
    },
    "boot2-multi": {
        "Configuration": 1, "Service": 2, "Controller": 2, "Repository": 1,
    },
}


class McpClient:
    """Minimal JSON-RPC-over-stdio client for the scv-graph-mcp surface."""

    def __init__(self, proc: subprocess.Popen):
        self.proc = proc
        self._id = 0

    def call(self, method: str, params: dict | None = None) -> dict:
        self._id += 1
        req = {"jsonrpc": "2.0", "id": self._id, "method": method,
               "params": params or {}}
        self.proc.stdin.write(json.dumps(req) + "\n")
        self.proc.stdin.flush()
        deadline = time.time() + 60
        while time.time() < deadline:
            line = self.proc.stdout.readline()
            if not line:
                raise RuntimeError("scv-graph-mcp closed stdout")
            msg = json.loads(line)
            if msg.get("id") == self._id:
                if "error" in msg:
                    raise RuntimeError(msg["error"])
                return msg["result"]
        raise TimeoutError(method)

    def notify(self, method: str) -> None:
        self.proc.stdin.write(json.dumps({"jsonrpc": "2.0", "method": method}) + "\n")
        self.proc.stdin.flush()

    def tool(self, name: str, args: dict) -> dict:
        res = self.call("tools/call", {"name": name, "arguments": args})
        return json.loads(res["content"][0]["text"])


@unittest.skipUnless(_tool("uv") and _tool("go"), "uv/go not on PATH")
class QueryMatrixTests(unittest.TestCase):
    """
    Chain: fixture copy -> perceive.py --source local (real uv analyzer,
    shared temp dist + temp HOME) -> <analysis>/<repo>/structure.vg ->
    scv-graph-mcp -> cypher.
    """

    def test_full_chain_query_matrix(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            dist = d / "dist"
            home = d / "home"
            analysis = d / "analysis"
            repos_root = d / "repos"
            env = {**os.environ, "HOME": str(home), "SCV_PERCEPTION_DIST": str(dist)}
            env.pop("SCV_PERCEPTION_ANALYZER", None)
            real_cache = Path.home() / ".cache" / "uv"
            if real_cache.is_dir():
                env.setdefault("UV_CACHE_DIR", str(real_cache))
            for var, default in (("GOCACHE", "Library/Caches/go-build"),
                                 ("GOMODCACHE", "go/pkg/mod")):
                cache = Path.home() / default
                if cache.is_dir():
                    env.setdefault(var, str(cache))

            # 1) perceive every fixture into one analysis root (real chain)
            for name in GROUND_TRUTH:
                repo = repos_root / name
                _copy_fixture(name, repo)
                res = subprocess.run(
                    [sys.executable, str(ROOT / "perceive.py"), str(repo),
                     str(analysis / name), "--source", "local", "--lang", "en"],
                    capture_output=True, text=True, timeout=900, env=env)
                self.assertEqual(0, res.returncode, f"{name}: {res.stderr[-800:]}")
                payload = json.loads(res.stdout.strip().splitlines()[-1])
                self.assertEqual("ok", payload["status"], name)
                self.assertTrue(payload["tiers"]["vg"], f"{name}: vg tier missing")

            # 2) build the query server
            binary = d / "scv-graph-mcp"
            subprocess.run(["go", "build", "-o", str(binary), "."],
                           cwd=ROOT / "graphmcp", check=True, timeout=600,
                           capture_output=True, text=True)

            # 3) serve every repo from one registration
            proc = subprocess.Popen(
                [str(binary), "-analysis-dir", str(analysis)],
                stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                stderr=subprocess.DEVNULL, text=True, bufsize=1)
            try:
                client = McpClient(proc)
                client.call("initialize", {
                    "protocolVersion": "2025-06-18", "capabilities": {},
                    "clientInfo": {"name": "matrix", "version": "0"}})
                client.notify("notifications/initialized")

                listing = client.tool("repos", {})
                self.assertEqual(
                    sorted(GROUND_TRUTH), sorted(r["repo"] for r in listing["repos"]),
                    "one server serves every analyzed repo")

                # 4) the matrix: per repo, per stereotype, count via Cypher
                for repo, expected in GROUND_TRUTH.items():
                    for stereotype, want in expected.items():
                        got = client.tool("cypher", {
                            "repo": repo,
                            "query": f"MATCH (n {{stereotype:'{stereotype}'}}) RETURN n",
                        })
                        self.assertEqual(
                            want, got["total"],
                            f"{repo} stereotype {stereotype}: want {want}, got {got['total']}")

                # 5) component-layer spot checks (full node info +
                #    cross-module edge + Boot 2/3 ambiguity mix)
                node = client.tool("get_node", {"repo": "boot3-modern", "key": "bean:cardChannel"})
                self.assertEqual("Component", node["props"]["stereotype"])
                self.assertIn("CardChannel", node["props"]["type"])

                dep = client.tool("cypher", {
                    "repo": "boot2-multi",
                    "query": "MATCH (c {stereotype:'Controller'})-[:INJECTS]->(s {stereotype:'Service'}) RETURN c.key, s.key"})
                pairs = {(r["c.key"], r["s.key"]) for r in dep["rows"]}
                self.assertEqual(
                    {("bean:customerController", "bean:customerService"),
                     ("bean:loyaltyController", "bean:loyaltyService")},
                    pairs, "web->service cross-module dependency pairs")

                amb = client.tool("cypher", {
                    "repo": "boot2-legacy",
                    "query": "MATCH (a)-[e:INJECTS]->(b) WHERE e.ambiguous = true RETURN a.key, b.key"})
                self.assertEqual(4, amb["total"], "boot2 legacy ambiguous pairs")

                # 6) structure-layer queries (v2 surface, spec §8.4/§5)
                impl = client.tool("cypher", {
                    "repo": "torture-app",
                    "query": "MATCH (t:Type)-[:IMPLEMENTS]->"
                             "(i {key:'type:com.acme.core.GreetingService'}) RETURN t.key"})
                self.assertEqual(
                    ["type:com.acme.core.GreetingServiceImpl"],
                    [r["t.key"] for r in impl["rows"]])

                calls = client.tool("cypher", {
                    "repo": "torture-app",
                    "query": "MATCH (a {key:'fn:com.acme.core.GreetingServiceImpl#greet(String)'})"
                             "-[:CALLS]->(b) RETURN b.key"})
                self.assertEqual(
                    {"fn:com.acme.core.GreetingRepository#loadTemplate()",
                     "fn:com.acme.core.GreetingAudit#audit(String)"},
                    {r["b.key"] for r in calls["rows"]}, "CALLS traversal from greet(String)")

                # CALLS callers-by-name (virtual dispatch into the interface
                # method key) and the Method/Type census through the vg
                callers = client.tool("cypher", {
                    "repo": "torture-app",
                    "query": "MATCH (m:Method)-[:CALLS]->(n:Method {name:'greet'}) "
                             "RETURN m.owner, m.name"})
                self.assertEqual(
                    [("type:com.acme.web.GreetingController", "greet")],
                    [(r["m.owner"], r["m.name"]) for r in callers["rows"]])

                methods = client.tool("cypher", {
                    "repo": "torture-app", "query": "MATCH (n:Method) RETURN n"})
                self.assertEqual(52, methods["total"], "Method census through the vg")
                types = client.tool("cypher", {
                    "repo": "torture-app", "query": "MATCH (n:Type) RETURN n"})
                self.assertEqual(34, types["total"], "Type census through the vg")
            finally:
                proc.terminate()
                proc.wait(timeout=10)
                proc.stdin.close()
                proc.stdout.close()


if __name__ == "__main__":
    unittest.main()
