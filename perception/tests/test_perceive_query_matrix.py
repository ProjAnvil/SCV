"""Full-chain query matrix: every fixture (Spring Boot 2 + 3) through
perceive -> structure.vg -> scv-graph-mcp, then Cypher stereotype queries
asserted against hand-computed ground truth.

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


def _tool(name: str) -> bool:
    return shutil.which(name) is not None


def _copy_fixture(name: str, dst: Path) -> None:
    src = ROOT / "adapters/springboot/src/test/resources/fixtures" / name
    for f in src.rglob("*"):
        if f.is_file():
            target = dst / f.relative_to(src)
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(f.read_bytes())


# Per-fixture stereotype census (hand-computed from the fixture sources).
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
    """Minimal JSON-RPC-over-stdio client for the scv-graph-mcp demo surface."""

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


class QueryMatrixTests(unittest.TestCase):
    """
    Chain: fixture copy -> perceive.py --source local (shared temp dist)
           -> <analysis>/<repo>/structure.vg -> scv-graph-mcp -> cypher.
    """

    def test_full_chain_query_matrix(self):
        if not (_tool("gradle") and _tool("java") and _tool("go")):
            self.skipTest("gradle/java/go not all on PATH")

        with TemporaryDirectory() as d:
            d = Path(d)
            dist = d / "dist"
            analysis = d / "analysis"
            repos_root = d / "repos"

            # 1) perceive every fixture into one analysis root
            for name in GROUND_TRUTH:
                repo = repos_root / name
                _copy_fixture(name, repo)
                res = subprocess.run(
                    [sys.executable, str(ROOT / "perceive.py"), str(repo),
                     str(analysis / name), "--source", "local", "--lang", "en"],
                    capture_output=True, text=True, timeout=1800,
                    env={**os.environ, "SCV_PERCEPTION_DIST": str(dist)})
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

                # 5) spot checks: full node info + cross-module edge + Boot 2/3 mix
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
            finally:
                proc.terminate()
                proc.wait(timeout=10)
                proc.stdin.close()
                proc.stdout.close()


if __name__ == "__main__":
    unittest.main()
