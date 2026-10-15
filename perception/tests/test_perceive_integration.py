import json, os, shutil, subprocess, sys, unittest
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


@unittest.skip("perception v2: rewritten in T7")
class IntegrationTests(unittest.TestCase):
    def test_local_torture_run(self):
        if not (_tool("gradle") and _tool("java")):
            self.skipTest("gradle/java not on PATH")
        with TemporaryDirectory() as d:
            d = Path(d)
            _copy_fixture("torture-app", d / "repo")
            out = d / "analysis"
            res = subprocess.run(
                [sys.executable, str(ROOT / "perceive.py"), str(d / "repo"), str(out),
                 "--source", "local", "--lang", "en"],
                capture_output=True, text=True, timeout=1200,
                env={**os.environ, "SCV_PERCEPTION_DIST": str(d / "dist")})
            self.assertEqual(res.returncode, 0, res.stderr)
            payload = json.loads(res.stdout.strip().splitlines()[-1])
            self.assertEqual(payload["status"], "ok")
            self.assertTrue(payload["tiers"]["json"])
            self.assertEqual(payload["counts"]["components"], 30)
            self.assertEqual(payload["counts"]["edges"], 24)
            self.assertTrue((out / "structure.json").exists())
            log = (out / "perception.log").read_text(encoding="utf-8")
            self.assertIn("mode=gradle", log)  # local + build.gradle => gradle mode logged

    def test_remote_policy_forces_noclasspath(self):
        if not (_tool("gradle") and _tool("java")):
            self.skipTest("gradle/java not on PATH")
        with TemporaryDirectory() as d:
            d = Path(d)
            _copy_fixture("gradle-lombok-sample", d / "repo")
            out = d / "analysis"
            res = subprocess.run(
                [sys.executable, str(ROOT / "perceive.py"), str(d / "repo"), str(out),
                 "--source", "remote", "--lang", "en"],
                capture_output=True, text=True, timeout=1200,
                env={**os.environ, "SCV_PERCEPTION_DIST": str(d / "dist")})
            payload = json.loads(res.stdout.strip().splitlines()[-1])
            self.assertEqual(payload["status"], "ok")
            log = (out / "perception.log").read_text(encoding="utf-8")
            self.assertNotIn("mode=gradle", log)  # never ran a build inside the repo

    def test_not_framework_silent(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            res = subprocess.run(
                [sys.executable, str(ROOT / "perceive.py"), str(d), str(d / "analysis"), "--source", "local"],
                capture_output=True, text=True, timeout=120)
            payload = json.loads(res.stdout.strip().splitlines()[-1])
            self.assertEqual(payload["status"], "not-framework")


if __name__ == "__main__":
    unittest.main()
