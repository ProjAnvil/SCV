import json, sys, unittest
from pathlib import Path
from tempfile import TemporaryDirectory

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import perceive


class DetectTests(unittest.TestCase):
    def setUp(self):
        self.manifest = json.loads((Path(__file__).resolve().parents[1]
                                    / "adapters/springboot/MANIFEST.json").read_text())

    def test_gradle_marker_with_confirm(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            (d / "build.gradle").write_text("plugins { id 'org.springframework.boot' version '3.4.5' }")
            src = d / "src/main/java/com/x"
            src.mkdir(parents=True)
            (src / "App.java").write_text("@SpringBootApplication public class App {}")
            self.assertTrue(perceive.detect_framework(d, self.manifest))

    def test_kts_marker(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            (d / "build.gradle.kts").write_text('implementation("org.springframework.boot:spring-boot-starter")')
            src = d / "src/main/java/com/x"
            src.mkdir(parents=True)
            (src / "App.java").write_text("@SpringBootApplication class App")
            self.assertTrue(perceive.detect_framework(d, self.manifest))

    def test_marker_without_confirm_is_rejected(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            (d / "build.gradle").write_text("testImplementation 'org.springframework.boot:spring-boot-starter-test'")
            self.assertFalse(perceive.detect_framework(d, self.manifest))

    def test_non_framework(self):
        with TemporaryDirectory() as d:
            self.assertFalse(perceive.detect_framework(Path(d), self.manifest))


class ModeTests(unittest.TestCase):
    def test_pom(self):
        with TemporaryDirectory() as d:
            d = Path(d); (d / "pom.xml").write_text("<project/>")
            self.assertEqual(perceive.resolve_mode(d, "local", "local-only"), "maven")

    def test_remote_forces_noclasspath(self):
        with TemporaryDirectory() as d:
            d = Path(d); (d / "pom.xml").write_text("<project/>")
            self.assertEqual(perceive.resolve_mode(d, "remote", "local-only"), "noclasspath")

    def test_remote_always_policy(self):
        with TemporaryDirectory() as d:
            d = Path(d); (d / "pom.xml").write_text("<project/>")
            self.assertEqual(perceive.resolve_mode(d, "remote", "always"), "maven")

    def test_never_policy(self):
        with TemporaryDirectory() as d:
            d = Path(d); (d / "pom.xml").write_text("<project/>")
            self.assertEqual(perceive.resolve_mode(d, "local", "never"), "noclasspath")

    def test_no_build_tool(self):
        with TemporaryDirectory() as d:
            self.assertEqual(perceive.resolve_mode(Path(d), "local", "local-only"), "noclasspath")


class PureTests(unittest.TestCase):
    def test_java_version_parse(self):
        self.assertEqual(perceive.parse_java_version('openjdk version "17.0.1"'), 17)
        self.assertEqual(perceive.parse_java_version('openjdk version "21" 2023-09-19'), 21)
        self.assertEqual(perceive.parse_java_version('java version "1.8.0_492"'), 8)
        self.assertIsNone(perceive.parse_java_version("garbage"))

    def test_footnote_single_line_stacking(self):
        got = perceive.render_footnote(["jdk-missing", "go-missing", "mcp-missing"], "zh")
        self.assertEqual(got.count("\n"), 0)

    def test_footnote_known_reasons(self):
        for reason in ["jdk-missing", "adapter-build-failed", "adapter-failed",
                       "degraded-noclasspath", "go-missing", "mcp-missing"]:
            for lang in ("zh", "en"):
                self.assertIn(reason, perceive.FOOTNOTES)

    def test_stale_cleanup(self):
        with TemporaryDirectory() as d:
            d = Path(d)
            for f in ("structure.json", "structure.vg", "perception.log"):
                (d / f).write_text("stale")
            perceive.cleanup_stale(d)
            for f in ("structure.json", "structure.vg", "perception.log"):
                self.assertFalse((d / f).exists())


if __name__ == "__main__":
    unittest.main()
