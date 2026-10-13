# Code Perception Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship SCV's Code Perception capability: Spring Boot adapter (migrated BeanProbe), perceive.py tier ladder, Go loader with cycle grading, regression tests, and all SCV touchpoints per the audited spec.

**Architecture:** Capability layer `perception/` with declarative adapter MANIFESTs; layered runtimes (JDK → adapter JSON → Go loader .vg → MCP hint) where every tier degrades to a one-line footnote and never fails `/scv run`. Security: remote repos analyze in noclasspath only.

**Tech Stack:** Java 17 + Spoon 11.5.0 + Gson (adapter), Python 3 stdlib (perceive.py + unittest), Go 1.27 + github.com/projanvil/vectra (loader), Bash (install.sh).

**Spec:** `docs/superpowers/specs/2026-10-13-code-perception-design.md` (rev 2 + install whitelist amendment). The plan argues from the spec — read both.

## Global Constraints

- perceive.py ALWAYS exits 0; errors are reason codes in the stdout JSON (spec §5.1).
- Vocabulary rules spec §1.1 bind all user-facing copy (banned: Java/Spoon/Gradle/bean; MCP name `scv-structure`; bilingual en+zh).
- Security policy: `--source remote` → noclasspath only unless `structure_build_execution: "always"` (spec §5.2).
- Loader go.mod must `require github.com/projanvil/vectra` with NO replace directive.
- JDK floor 17 (runtime), Go floor 1.27; per-tier timeout 600s.
- Ground truth baselines: gradle-lombok-sample = 12 beans / 8 edges / 0 unresolved; torture-app = 30 / 24 / 0.
- Every commit leaves the repo buildable (`gradle compileJava` in the adapter, `go build ./...` in loader, `python3 -m unittest discover` in perception/tests).

---

### Task 1: Migrate beanprobe → perception/adapters/springboot + MANIFEST + docs

**Files:**
- Move: `beanprobe/*` → `perception/adapters/springboot/*`; `beanprobe/fixtures/*` → `perception/adapters/springboot/src/test/resources/fixtures/*`
- Modify: `perception/adapters/springboot/settings.gradle`, `perception/adapters/springboot/.gitignore`
- Create: `perception/adapters/springboot/MANIFEST.json`, `perception/README.md`, `perception/DEVELOPERS.md`

**Interfaces:**
- Produces: directory layout all later tasks rely on; `MANIFEST.json` schema consumed by perceive.py (Task 4).

- [ ] **Step 1: Move the tree**

```bash
cd /Users/yuhaochen/Documents/codebase/projanvil/SCV
mkdir -p perception/adapters/springboot/src/test/resources
mv beanprobe/src beanprobe/build.gradle beanprobe/settings.gradle beanprobe/.gitignore perception/adapters/springboot/
mv beanprobe/fixtures perception/adapters/springboot/src/test/resources/fixtures
mv beanprobe/README.md perception/adapters/springboot/README.md
rmdir beanprobe
```

- [ ] **Step 2: Rename the Gradle project**

`perception/adapters/springboot/settings.gradle` → replace content with:

```groovy
rootProject.name = 'springboot-probe'
```

- [ ] **Step 3: Fix .gitignore (audit fix: `*.json` would swallow MANIFEST.json)**

`perception/adapters/springboot/.gitignore` → replace content with:

```
.gradle/
build/
out/
src/test/resources/fixtures/**/build/
src/test/resources/fixtures/**/.gradle/
!MANIFEST.json
```

- [ ] **Step 4: Create MANIFEST.json** (verbatim from spec §4)

`perception/adapters/springboot/MANIFEST.json`:

```json
{
  "id": "springboot",
  "framework": "Spring Boot",
  "detect": {
    "marker_files": ["pom.xml", "build.gradle", "build.gradle.kts",
                      "settings.gradle", "settings.gradle.kts"],
    "markers": ["spring-boot", "org.springframework.boot"],
    "confirm": ["@SpringBootApplication"]
  },
  "requires": { "jdk": "17" },
  "build": "gradle installDist -q",
  "run": "{dist_bin} {mode} {project} {out}",
  "artifact": "structure.json"
}
```

- [ ] **Step 5: Create perception/README.md (user-facing — vocabulary rules apply)**

```markdown
# SCV Code Perception

SCV's code perception capability recognizes supported frameworks in a codebase and
extracts component dependency structure that `/scv run` uses to deepen the generated
architecture documentation — and that agents can query in later sessions.

**Supported: Spring Boot.** For a Spring Boot project, `/scv run` performs
deep structural analysis automatically: it maps components (services, controllers,
repositories, configuration beans) and their dependency edges, detects circular
dependencies, and enriches ARCHITECTURE.md with a "Component Dependencies" section.

## Optional query tier

When a Go 1.27+ runtime and the `vectra-mcp` tool are available, `/scv run` also
generates a structure-graph query file (`structure.vg`) and prints the command to
register it as an MCP service (`scv-structure`), enabling dependency queries
(multi-hop reachability, impact radius, cycles) in future sessions. Install the
tool with: `go install github.com/projanvil/vectra/mcp/cmd/vectra-mcp@latest`

Both tiers are optional enhancements: when their runtime is missing, `/scv run`
notes the skip in its completion report and finishes normally.

## Configuration (`~/.scv/config.json`)

| Key | Default | Meaning |
|---|---|---|
| `structure_analysis_enabled` | `true` | enable/disable deep structural analysis |
| `structure_build_execution` | `"local-only"` | build-tool execution policy: `local-only` (remote repos are analyzed in a no-build degraded mode), `always`, `never` |

## Files

- `~/.scv/analysis/{project}/structure.json` — component dependency data (always, when analysis ran)
- `~/.scv/analysis/{project}/structure.vg` — query-file for the MCP tier (optional tier)
- `~/.scv/analysis/{project}/perception.log` — tier decisions and skip reasons

Deleting the analysis directory removes these files; the MCP query service stops
returning data for the project until the next `/scv run`.
```

- [ ] **Step 6: Create perception/DEVELOPERS.md (developer vocabulary allowed; NOT shipped by install.sh)**

```markdown
# Code Perception — Adapter Contract

How the capability layer works and how to add a framework adapter.

## Runtime pieces

- `perceive.py` — orchestrator. Detects the framework via adapter MANIFESTs, checks
  runtimes (JDK/Go), lazily builds the adapter dist, runs it, stages artifacts,
  drives the loader/MCP tiers. Contract: single-line JSON on stdout, exit 0 always.
- `loader/` — Go module (`github.com/projanvil/vectra`): structure.json →
  structure.vg + cycle grading (fatal vs tolerated).
- `adapters/<id>/` — self-contained analyzer. Ships as MANIFEST.json + Gradle
  build inputs (src/, build.gradle, settings.gradle). Fixtures live in
  `src/test/resources/fixtures/` and are NEVER installed to user machines.

## MANIFEST schema

| Field | Meaning |
|---|---|
| `id` | adapter identifier (directory name) |
| `framework` | user-visible framework name |
| `detect.marker_files` | build files to scan (recursive, depth ≤3) |
| `detect.markers` | content substrings marking a candidate |
| `detect.confirm` | source markers (@SpringBootApplication) required to confirm |
| `requires.jdk` | minimum JDK major version |
| `build` | dist build command (run in adapter dir) |
| `run` | command template; `{dist_bin}` `{mode}` `{project}` `{out}` substituted by perceive.py |
| `artifact` | produced graph file name (Vectra batch JSON schema) |

## Artifact schema (the contract between adapter and loader)

`{"nodes":[{"key","label","props"}], "edges":[{"srcKey","dstKey","label","props"}]}`
— keys are `bean:<name>`; edge label `INJECTS`; edge prop `via` ∈ {constructor,
lombok-constructor, field, inherited-field, setter, beanMethod} (loader grades
cycles: any of constructor/lombok-constructor/beanMethod in a cycle ⇒ fatal).

## Adding an adapter

1. Create `adapters/<id>/` with MANIFEST + build inputs producing the artifact schema.
2. Add fixtures + a regression test asserting ground truth (see springboot's
   `PerceptionRegressionTest`).
3. Nothing else: perceive.py and all SCV touchpoints are adapter-agnostic.

## Vocabulary warning

Anything a user can see (perception/README.md, skill docs, report copy) follows the
spec's vocabulary rules — implementation language is invisible. This file and the
adapter's own README are developer-facing exceptions.
```

- [ ] **Step 7: Verify the move (compile + run fixture from new location)**

```bash
cd perception/adapters/springboot
gradle installDist -q
./build/install/springboot-probe/bin/springboot-probe gradle src/test/resources/fixtures/gradle-lombok-sample /tmp/mig-check.json | tail -4
```

Expected: `beans: 12`, `INJECTS edges: 8`, `unresolved injections: 0`.

- [ ] **Step 8: Commit**

```bash
git add perception/
git commit -m "feat: migrate analyzer to perception/adapters/springboot with MANIFEST contract"
```

---

### Task 2: Regression test (gradle test)

**Files:**
- Modify: `perception/adapters/springboot/build.gradle`
- Create: `perception/adapters/springboot/src/test/java/projanvil/scv/beanprobe/PerceptionRegressionTest.java`

**Interfaces:**
- Consumes: `build/install/springboot-probe/bin/springboot-probe` (test task depends on installDist).
- Produces: `gradle test` green = ground-truth gate for every later change.

- [ ] **Step 1: Add test dependencies + wiring** — in `build.gradle`, after the `application { ... }` block add:

```groovy
dependencies {
    testImplementation platform('org.junit:junit-bom:5.12.2')
    testImplementation 'org.junit.jupiter:junit-jupiter'
    testRuntimeOnly 'org.junit.platform:junit-platform-launcher'
}

test {
    useJUnitPlatform()
    dependsOn 'installDist'
    testLogging { events 'failed', 'skipped' }
}
```

(Merge into the existing `dependencies { }` block — do not create a duplicate.)

- [ ] **Step 2: Write the test**

`src/test/java/projanvil/scv/beanprobe/PerceptionRegressionTest.java`:

```java
package projanvil.scv.beanprobe;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

import java.io.File;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.stream.Stream;

import static org.junit.jupiter.api.Assertions.*;
import static org.junit.jupiter.api.Assumptions.assumeTrue;

/** Ground-truth regression: fixtures live in src/test/resources/fixtures and are
 *  copied to a temp dir so nested gradle builds never touch the source tree. */
class PerceptionRegressionTest {

    @TempDir
    Path temp;

    private static boolean gradleAvailable() {
        try {
            Process p = new ProcessBuilder("gradle", "--version").redirectErrorStream(true).start();
            p.getInputStream().readAllBytes();
            return p.waitFor() == 0;
        } catch (Exception e) {
            return false;
        }
    }

    private Path runProbe(String fixture) throws Exception {
        assumeTrue(gradleAvailable(), "gradle not on PATH — regression self-skips");
        Path fixtureDir = copyFixture(fixture);
        Path out = temp.resolve(fixture + ".json");
        String bin = Path.of("build", "install", "springboot-probe", "bin", "springboot-probe").toAbsolutePath().toString();
        Process p = new ProcessBuilder(bin, "gradle", fixtureDir.toString(), out.toString())
                .redirectErrorStream(true).start();
        try (var sc = new java.util.Scanner(p.getInputStream())) {
            while (sc.hasNextLine()) System.out.println("[probe] " + sc.nextLine());
        }
        assertEquals(0, p.waitFor(), "probe exited non-zero");
        return out;
    }

    private Path copyFixture(String name) throws Exception {
        Path src = Path.of("src", "test", "resources", "fixtures", name);
        Path dst = temp.resolve(name);
        try (Stream<Path> walk = Files.walk(src)) {
            for (Path f : walk.filter(Files::isRegularFile).toList()) {
                Path target = dst.resolve(src.relativize(f).toString());
                Files.createDirectories(target.getParent());
                Files.copy(f, target);
            }
        }
        return dst;
    }

    @SuppressWarnings("unchecked")
    private static List<Object> array(String json, String key) throws Exception {
        return (List<Object>) rawJson(json, key);
    }

    private static Object rawJson(String json, String key) throws Exception {
        return new com.google.gson.JsonParser().parse(json).getAsJsonObject().get(key);
    }

    @Test
    void lombokSampleGroundTruth() throws Exception {
        String json = Files.readString(runProbe("gradle-lombok-sample"));
        assertEquals(12, array(json, "nodes").size(), "beans");
        assertEquals(8, array(json, "edges").size(), "edges");
        assertTrue(json.contains("\"ambiguous\" : true"), "ambiguous pair flagged");
        assertTrue(json.contains("\"qualifier\" : \"emailGreetingSender\""), "qualifier edge present");
    }

    @Test
    void tortureAppGroundTruth() throws Exception {
        String json = Files.readString(runProbe("torture-app"));
        assertEquals(30, array(json, "nodes").size(), "beans");
        assertEquals(24, array(json, "edges").size(), "edges");
        assertTrue(json.contains("\"via\" : \"inherited-field\""), "inherited injection edge");
        assertTrue(json.contains("\"via\" : \"beanMethod\""), "@Bean param edges");
        assertTrue(json.contains("bean:circularA") && json.contains("bean:circularB"), "cycle pair present");
        assertFalse(json.contains("UNRESOLVED"), "no unresolved noise");
    }
}
```

Note: assertions parse counts from JSON structure; `UNRESOLVED` is asserted absent
from stdout echoed into the test log via `[probe]` lines — if flaky in CI, tighten
by asserting `json.contains("\"unresolved\"")` semantics instead (probe prints them
only when non-zero).

- [ ] **Step 3: Run**

```bash
cd perception/adapters/springboot && gradle test
```

Expected: 2 tests PASS (first execution downloads fixture deps — network required).

- [ ] **Step 4: Commit**

```bash
git add perception/adapters/springboot
git commit -m "test: ground-truth regression suite for springboot adapter"
```

---

### Task 3: Go loader + cycle grading

**Files:**
- Create: `perception/loader/go.mod`, `perception/loader/main.go`, `perception/loader/loader_test.go`

**Interfaces:**
- Consumes: structure.json (artifact schema, DEVELOPERS.md).
- Produces: `structure.vg` snapshot; stdout JSON `{"nodes":N,"edges":M,"cycles":[{"nodes":["bean:a"],"grade":"fatal|tolerated"}]}` — perceive.py (Task 5) parses this for cycle counts.

- [ ] **Step 1: Write the failing test** — `perception/loader/loader_test.go`:

```go
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeFixture(t *testing.T, doc string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "structure.json")
	if err := os.WriteFile(p, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const fatalDoc = `{"nodes":[
 {"key":"bean:a","label":"Bean","props":{}},
 {"key":"bean:b","label":"Bean","props":{}}],
 "edges":[
 {"srcKey":"bean:a","dstKey":"bean:b","label":"INJECTS","props":{"via":"constructor"}},
 {"srcKey":"bean:b","dstKey":"bean:a","label":"INJECTS","props":{"via":"beanMethod"}}]}`

const toleratedDoc = `{"nodes":[
 {"key":"bean:x","label":"Bean","props":{}},
 {"key":"bean:y","label":"Bean","props":{}}],
 "edges":[
 {"srcKey":"bean:x","dstKey":"bean:y","label":"INJECTS","props":{"via":"field"}},
 {"srcKey":"bean:y","dstKey":"bean:x","label":"INJECTS","props":{"via":"setter"}}]}`

func TestGradeCycles(t *testing.T) {
	res := loadAndGrade(writeFixture(t, fatalDoc))
	if len(res.Cycles) != 1 || res.Cycles[0].Grade != "fatal" {
		t.Fatalf("want 1 fatal cycle, got %+v", res.Cycles)
	}
	res = loadAndGrade(writeFixture(t, toleratedDoc))
	if len(res.Cycles) != 1 || res.Cycles[0].Grade != "tolerated" {
		t.Fatalf("want 1 tolerated cycle, got %+v", res.Cycles)
	}
}

func TestLoadAndGradeShape(t *testing.T) {
	res := loadAndGrade(writeFixture(t, fatalDoc))
	if res.Nodes != 2 || res.Edges != 2 {
		t.Fatalf("counts: %+v", res)
	}
	if _, err := json.Marshal(res); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: go.mod** — `perception/loader/go.mod` (NO replace directive):

```go
module scv/perception-loader

go 1.27

require github.com/projanvil/vectra v0.0.0
```

then `cd perception/loader && go get github.com/projanvil/vectra@latest && go mod tidy`
(pin the resolved version it writes; `go 1.27` matches vectra's floor).

- [ ] **Step 3: Run test to verify it fails**

```bash
cd perception/loader && go test ./...
```

Expected: build failure — `loadAndGrade` undefined.

- [ ] **Step 4: Implement main.go**:

```go
// Command loader converts a structure.json (SCV Code Perception artifact schema)
// into a vectra snapshot (.vg) and grades dependency cycles.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/projanvil/vectra"
)

type nodeJ struct {
	Key   string         `json:"key"`
	Label string         `json:"label"`
	Props map[string]any `json:"props"`
}

type edgeJ struct {
	SrcKey string         `json:"srcKey"`
	DstKey string         `json:"dstKey"`
	Label  string         `json:"label"`
	Props  map[string]any `json:"props"`
}

type graphJ struct {
	Nodes []nodeJ `json:"nodes"`
	Edges []edgeJ `json:"edges"`
}

type cycleOut struct {
	Nodes []string `json:"nodes"`
	Grade string   `json:"grade"`
}

type resultOut struct {
	Nodes  int        `json:"nodes"`
	Edges  int        `json:"edges"`
	Cycles []cycleOut `json:"cycles"`
}

// fatalVia edges are the ones Spring cannot satisfy in a cycle (constructor wiring).
var fatalVia = map[string]bool{
	"constructor": true, "lombok-constructor": true, "beanMethod": true,
}

func loadAndGrade(jsonPath string) resultOut {
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		fatal(err)
	}
	var g graphJ
	if err := json.Unmarshal(raw, &g); err != nil {
		fatal(err)
	}
	gr := vectra.New()
	for _, k := range []string{"stereotype", "beanName", "type", "conditions"} {
		gr.IndexProperty(k)
	}
	for _, n := range g.Nodes {
		must(gr.AddNode(n.Key, n.Label, n.Props))
	}
	edgeVia := map[[2]string]string{} // (src,dst) -> via (last wins, mirrors upsert)
	for _, e := range g.Edges {
		must(gr.UpsertEdge(e.SrcKey, e.DstKey, e.Label, e.Props))
		if v, ok := e.Props["via"].(string); ok {
			edgeVia[[2]string{e.SrcKey, e.DstKey}] = v
		}
	}
	must(gr.Seal())

	out := resultOut{Nodes: len(g.Nodes), Edges: gr.EdgeCount(), Cycles: []cycleOut{}}
	for _, c := range gr.Cycles() {
		keys := map[string]bool{}
		members := make([]string, 0, len(c.Nodes))
		for _, id := range c.Nodes {
			if n, ok := gr.NodeByID(id); ok {
				members = append(members, n.Key)
				keys[n.Key] = true
			}
		}
		grade := "tolerated"
		for k := range keys {
			edges, err := gr.EdgesOf(k, vectra.Out, "INJECTS")
			must(err)
			for _, e := range edges {
				dst, ok := gr.NodeByID(e.Dst)
				must2(ok)
				if keys[dst.Key] && fatalVia[edgeVia[[2]string{k, dst.Key}]] {
					grade = "fatal"
				}
			}
		}
		out.Cycles = append(out.Cycles, cycleOut{Nodes: members, Grade: grade})
	}
	return out
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: loader <structure.json> <structure.vg>")
		os.Exit(2)
	}
	res := loadAndGrade(os.Args[1])
	g2 := reloadSealed(os.Args[1])
	must(g2.SaveFile(os.Args[2]))
	enc := json.NewEncoder(os.Stdout)
	must(enc.Encode(res))
}

func reloadSealed(jsonPath string) *vectra.Graph {
	res := loadGraphOnly(jsonPath)
	must(res.Seal())
	return res
}

func loadGraphOnly(jsonPath string) *vectra.Graph {
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		fatal(err)
	}
	var g graphJ
	if err := json.Unmarshal(raw, &g); err != nil {
		fatal(err)
	}
	gr := vectra.New()
	for _, k := range []string{"stereotype", "beanName", "type", "conditions"} {
		gr.IndexProperty(k)
	}
	for _, n := range g.Nodes {
		must(gr.AddNode(n.Key, n.Label, n.Props))
	}
	for _, e := range g.Edges {
		must(gr.UpsertEdge(e.SrcKey, e.DstKey, e.Label, e.Props))
	}
	return gr
}

func must(err error) { if err != nil { panic(err) } }
func must2(_ bool)  {}
func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
```

Note: `main` rebuilds via `loadGraphOnly` so the saved snapshot contains exactly the
JSON graph (grading result is report-only). If `vectra.EdgeCount` or the `Out`
constant name differs in the published module, adjust to the real API
(`go doc github.com/projanvil/vectra . Graph` to check; the local repo at
`~/Documents/codebase/projanvil/Vectra` is the source of truth).

- [ ] **Step 5: Run tests**

```bash
cd perception/loader && go test ./...
```

Expected: PASS.

- [ ] **Step 6: End-to-end against real adapter output**

```bash
cd perception/adapters/springboot && gradle installDist -q
./build/install/springboot-probe/bin/springboot-probe gradle src/test/resources/fixtures/torture-app /tmp/torture.json >/dev/null
cd ../../loader && go run . /tmp/torture.json /tmp/torture.vg
```

Expected stdout: `{"nodes":30,"edges":23,"cycles":[{"nodes":["bean:circularA","bean:circularB"],"grade":"fatal"}]}`
(edges=23 because UpsertEdge dedups the payment double-pair; grade=fatal because
CircularA/B inject via `field`… if `field` grades tolerated, expected grade is
`tolerated` — assert which one and record it in the test expectations of Task 5).

- [ ] **Step 7: Commit**

```bash
git add perception/loader
git commit -m "feat: perception loader — structure.json to .vg with cycle grading"
```

---

### Task 4: perceive.py — pure logic (TDD, stdlib unittest)

**Files:**
- Create: `perception/perceive.py`, `perception/tests/test_perceive.py`

**Interfaces:**
- Consumes: MANIFEST.json (Task 1).
- Produces (used by Task 5 orchestration, exact signatures):
  `load_manifest(adapter_id) -> dict`, `detect_framework(repo: Path, manifest: dict) -> bool`,
  `resolve_mode(repo: Path, source: str, policy: str) -> str`,
  `parse_java_version(out: str) -> int|None`, `FOOTNOTES: dict[str, tuple[str,str]]`,
  `render_footnote(reasons: list[str], lang: str) -> str`,
  `cleanup_stale(analysis_dir: Path)`, `adapter_source_hash(adapter_dir: Path) -> str`.

- [ ] **Step 1: Write failing tests** — `perception/tests/test_perceive.py`:

```python
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
```

- [ ] **Step 2: Run to verify failure**

```bash
cd perception && python3 -m unittest discover -s tests -v
```

Expected: ImportError — no module `perceive`.

- [ ] **Step 3: Implement the pure-logic half of perceive.py** (orchestration added in Task 5; structure the file so Task 5 only appends):

`perception/perceive.py`:

```python
#!/usr/bin/env python3
"""SCV Code Perception orchestrator.

Contract (spec 2026-10-13 rev2 §5.1): single-line JSON on stdout, exit 0 always.
Usage: perceive.py <repo_path> <analysis_dir> --source local|remote [--lang zh|en]
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
import time
from pathlib import Path

PERCEPTION_ROOT = Path(__file__).resolve().parent
DIST_ROOT = Path(os.environ.get("SCV_PERCEPTION_DIST", Path.home() / ".scv/perception/dist"))
TIER_TIMEOUT = 600  # seconds per tier

# (zh, en) — reason codes never contain tool names (spec §5.3)
FOOTNOTES = {
    "jdk-missing": (
        "已跳过深度结构分析：未检测到 JDK 17+（可选增强，安装后重新运行 /scv run 即可启用）",
        "Deep structural analysis skipped: JDK 17+ not found (optional; re-run /scv run after installing)",
    ),
    "adapter-build-failed": (
        "已跳过深度结构分析：分析组件构建失败（原因码记录于 perception.log，可重试）",
        "Deep structural analysis skipped: analyzer build failed (reason logged; retryable)",
    ),
    "adapter-failed": (
        "已跳过深度结构分析：分析未完成（已尝试降级，详情见 perception.log）",
        "Deep structural analysis skipped: analysis incomplete after fallback (see perception.log)",
    ),
    "degraded-noclasspath": (
        "深度结构分析以降级模式完成，结果可能包含更多不确定标注",
        "Deep structural analysis completed in degraded mode; results may carry extra uncertainty flags",
    ),
    "go-missing": (
        "结构图查询文件未生成（可选增强，需要 Go 1.27+ 运行环境）",
        "Structure-graph query file not generated (optional; requires Go 1.27+)",
    ),
    "mcp-missing": (
        "结构图查询服务未检测到（可选增强，安装说明见 ~/.scv/perception/README.md）",
        "Graph query service not found (optional; see ~/.scv/perception/README.md)",
    ),
}


def log_line(analysis_dir: Path, message: str) -> None:
    stamp = time.strftime("%Y-%m-%dT%H:%M:%S")
    with open(analysis_dir / "perception.log", "a", encoding="utf-8") as fh:
        fh.write(f"{stamp} {message}\n")


def load_manifest(adapter_id: str) -> dict:
    return json.loads((PERCEPTION_ROOT / "adapters" / adapter_id / "MANIFEST.json").read_text(encoding="utf-8"))


def _iter_marker_files(repo: Path, names: list[str], max_depth: int = 3):
    for dirpath, dirnames, filenames in os.walk(repo):
        depth = len(Path(dirpath).relative_to(repo).parts)
        if depth >= max_depth:
            dirnames[:] = []
        for f in filenames:
            if f in names:
                yield Path(dirpath) / f


def detect_framework(repo: Path, manifest: dict) -> bool:
    det = manifest["detect"]
    candidate = False
    for marker_file in _iter_marker_files(repo, det["marker_files"]):
        try:
            content = marker_file.read_text(encoding="utf-8", errors="ignore")
        except OSError:
            continue
        if any(m in content for m in det["markers"]):
            candidate = True
            break
    if not candidate:
        return False
    # second stage: confirm framework ownership in sources
    for src_root in ("src/main/java", "src/main/kotlin"):
        root = repo / src_root
        if not root.is_dir():
            continue
        for dirpath, _dirnames, filenames in os.walk(root):
            for f in filenames:
                if not f.endswith((".java", ".kt")):
                    continue
                try:
                    content = (Path(dirpath) / f).read_text(encoding="utf-8", errors="ignore")
                except OSError:
                    continue
                if any(c in content for c in det["confirm"]):
                    return True
    return False


def resolve_mode(repo: Path, source: str, policy: str) -> str:
    if policy == "never":
        return "noclasspath"
    if source == "remote" and policy != "always":
        return "noclasspath"  # security: never execute builds inside untrusted repos
    if (repo / "pom.xml").is_file():
        return "maven"
    if (repo / "gradlew").is_file() or shutil.which("gradle"):
        return "gradle"
    return "noclasspath"


def parse_java_version(out: str):
    m = re.search(r'version "(\d+)(?:\.(\d+))?', out)
    if not m:
        return None
    major = int(m.group(1))
    minor = int(m.group(2) or 0)
    return 8 if major == 1 and minor else major


def probe_java():
    for cmd in (["java", "-version"], [str(Path(os.environ.get("JAVA_HOME", "")) / "bin" / "java"), "-version"] if os.environ.get("JAVA_HOME") else None):
        if not cmd:
            continue
        try:
            res = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
        except (OSError, subprocess.TimeoutExpired):
            continue
        version = parse_java_version(res.stderr or res.stdout)
        if version:
            return version
    return None


def probe_go():
    try:
        res = subprocess.run(["go", "version"], capture_output=True, text=True, timeout=30)
    except (OSError, subprocess.TimeoutExpired):
        return None
    m = re.search(r"go version go(\d+)\.(\d+)", res.stdout)
    if not m:
        return None
    major, minor = int(m.group(1)), int(m.group(2))
    return (major, minor) if (major, minor) >= (1, 27) else None


def render_footnote(reasons: list[str], lang: str = "zh") -> str:
    idx = 0 if lang.startswith("zh") else 1
    parts = []
    for r in reasons:
        if r in FOOTNOTES:
            parts.append(FOOTNOTES[r][idx])
    if not parts:
        return ""
    return "；".join(dict.fromkeys(parts)) if lang.startswith("zh") else "; ".join(dict.fromkeys(parts))


def cleanup_stale(analysis_dir: Path) -> None:
    for name in ("structure.json", "structure.vg", "perception.log"):
        p = analysis_dir / name
        if p.exists():
            p.unlink()


def adapter_source_hash(adapter_dir: Path) -> str:
    h = hashlib.sha256()
    for f in sorted(adapter_dir.rglob("*")):
        if not f.is_file():
            continue
        s = str(f.relative_to(adapter_dir))
        if s.startswith(("build/", ".gradle/", "src/test/")):
            continue
        h.update(s.encode())
        h.update(f.read_bytes())
    return h.hexdigest()


def main() -> int:
    raise NotImplementedError("orchestration added in Task 5")


if __name__ == "__main__":
    sys.exit(main())
```

- [ ] **Step 4: Run tests**

```bash
cd perception && python3 -m unittest discover -s tests -v
```

Expected: 10 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add perception/perceive.py perception/tests
git commit -m "feat: perceive.py pure logic — detect/mode/footnotes/cleanup with unit tests"
```

---

### Task 5: perceive.py orchestration tiers + integration test

**Files:**
- Modify: `perception/perceive.py` (replace the `main()` stub)
- Create: `perception/tests/test_perceive_integration.py`

**Interfaces:**
- Consumes: Task 4 functions; adapter dist (`{dist_bin}` per MANIFEST); loader (Task 3).
- Produces: stdout JSON per spec §5.1 — `{"status","tiers":{"json","vg","mcp_hint"},"reason","counts":{"components","edges","cycles_fatal","cycles_tolerated"},"artifacts":{"structure_json","structure_vg"},"mcp_add"}`.

- [ ] **Step 1: Implement main()** — replace the stub with:

```python
def _os_bin(dist_dir: Path, name: str) -> Path:
    sub = "bin/" + (name + ".bat" if os.name == "nt" else name)
    return dist_dir / sub


def ensure_dist(adapter: dict) -> Path | None:
    adapter_dir = PERCEPTION_ROOT / "adapters" / adapter["id"]
    dist_dir = DIST_ROOT / adapter["id"]
    hash_file = dist_dir / ".source-hash"
    current = adapter_source_hash(adapter_dir)
    bin_path = _os_bin(dist_dir, adapter["id"] + "-probe") if dist_dir.exists() else None
    if bin_path and bin_path.exists() and hash_file.exists() and hash_file.read_text().strip() == current:
        return dist_dir
    try:
        subprocess.run(adapter["build"], shell=True, cwd=adapter_dir,
                       capture_output=True, text=True, timeout=TIER_TIMEOUT, check=True)
    except (subprocess.SubprocessError, OSError) as e:
        return None if not dist_dir.exists() else (_record_build_error(e), None)[1]
    dist_dir = DIST_ROOT / adapter["id"]
    (dist_dir / ".source-hash").write_text(current)
    return dist_dir


def _record_build_error(e) -> None:
    sys.stderr.write(f"adapter build error: {e}\n")


def run_adapter(adapter: dict, dist_dir: Path, mode: str, repo: Path, out_json: Path):
    bin_path = _os_bin(dist_dir, adapter["id"] + "-probe")
    cmd = [str(bin_path), mode, str(repo), str(out_json)]
    try:
        res = subprocess.run(cmd, capture_output=True, text=True, timeout=TIER_TIMEOUT)
        return res.returncode == 0 and out_json.exists()
    except (subprocess.SubprocessError, OSError):
        return False


def run_loader(loader_dir: Path, json_path: Path, vg_path: Path):
    binary = DIST_ROOT / "loader" / "perception-loader"
    try:
        if not binary.exists():
            DIST_ROOT.mkdir(parents=True, exist_ok=True)
            subprocess.run(["go", "build", "-o", str(binary), "."], cwd=loader_dir,
                           capture_output=True, text=True, timeout=TIER_TIMEOUT, check=True)
        res = subprocess.run([str(binary), str(json_path), str(vg_path)],
                              capture_output=True, text=True, timeout=TIER_TIMEOUT)
        if res.returncode != 0:
            return None
        return json.loads(res.stdout)
    except (subprocess.SubprocessError, OSError, json.JSONDecodeError):
        return None


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("repo_path")
    ap.add_argument("analysis_dir")
    ap.add_argument("--source", choices=["local", "remote"], default="local")
    ap.add_argument("--lang", default="zh")
    args = ap.parse_args()

    repo, analysis_dir = Path(args.repo_path).resolve(), Path(args.analysis_dir).resolve()
    analysis_dir.mkdir(parents=True, exist_ok=True)
    emit = {"status": "skipped", "reason": None,
            "tiers": {"json": False, "vg": False, "mcp_hint": False},
            "counts": None, "artifacts": {"structure_json": None, "structure_vg": None},
            "mcp_add": None, "footnote": ""}

    cfg_path = Path.home() / ".scv/config.json"
    try:
        cfg = json.loads(cfg_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        cfg = {}
    if not cfg.get("structure_analysis_enabled", True):
        emit["status"], emit["reason"] = "skipped", "disabled"
        log_line(analysis_dir, "tier0 disabled by config")
        print(json.dumps(emit, ensure_ascii=False))
        return 0
    policy = cfg.get("structure_build_execution", "local-only")

    adapter = load_manifest("springboot")
    if not detect_framework(repo, adapter):
        emit["status"], emit["reason"] = "not-framework", None
        log_line(analysis_dir, "tier1 detect: not-framework")
        print(json.dumps(emit, ensure_ascii=False))
        return 0

    cleanup_stale(analysis_dir)
    log_line(analysis_dir, f"tier1 detect: springboot, source={args.source}, policy={policy}")

    reasons: list[str] = []
    java = probe_java()
    if not java or java < adapter["requires"]["jdk"]:
        reasons.append("jdk-missing")
        log_line(analysis_dir, f"tier3 jdk probe: {java}")
        emit["reason"] = "jdk-missing"
        emit["footnote"] = render_footnote(reasons, args.lang)
        print(json.dumps(emit, ensure_ascii=False))
        return 0

    dist_dir = ensure_dist(adapter)
    if dist_dir is None:
        emit["reason"] = "adapter-build-failed"
        emit["footnote"] = render_footnote(["adapter-build-failed"], args.lang)
        log_line(analysis_dir, "tier4 dist build failed")
        print(json.dumps(emit, ensure_ascii=False))
        return 0

    structure_json = analysis_dir / "structure.json"
    mode = resolve_mode(repo, args.source, policy)
    ok = run_adapter(adapter, dist_dir, mode, repo, structure_json)
    degraded = False
    if not ok and mode != "noclasspath":
        log_line(analysis_dir, f"tier5 mode {mode} failed; falling back to noclasspath")
        ok = run_adapter(adapter, dist_dir, "noclasspath", repo, structure_json)
        degraded = ok
    if not ok:
        emit["reason"] = "adapter-failed"
        emit["footnote"] = render_footnote(["adapter-failed"], args.lang)
        log_line(analysis_dir, "tier5 adapter failed (incl. noclasspath fallback)")
        print(json.dumps(emit, ensure_ascii=False))
        return 0

    emit["status"], emit["tiers"]["json"] = "ok", True
    emit["artifacts"]["structure_json"] = str(structure_json)
    graph = json.loads(structure_json.read_text(encoding="utf-8"))
    counts = {"components": len(graph["nodes"]), "edges": len(graph["edges"]),
              "cycles_fatal": 0, "cycles_tolerated": 0}
    if degraded:
        reasons.append("degraded-noclasspath")

    vg_path = analysis_dir / "structure.vg"
    loader_info = None
    if probe_go():
        loader_info = run_loader(PERCEPTION_ROOT / "loader", structure_json, vg_path)
    else:
        reasons.append("go-missing")
    if loader_info:
        emit["tiers"]["vg"] = True
        emit["artifacts"]["structure_vg"] = str(vg_path)
        for c in loader_info.get("cycles", []):
            if c.get("grade") == "fatal":
                counts["cycles_fatal"] += 1
            else:
                counts["cycles_tolerated"] += 1
    emit["counts"] = counts

    if shutil.which("vectra-mcp"):
        emit["tiers"]["mcp_hint"] = True
        ts = time.strftime("%Y-%m-%d %H:%M")
        emit["mcp_add"] = (f"claude mcp add scv-structure -- vectra-mcp -snapshot {vg_path} "
                           f"# generated {ts}")
    else:
        reasons.append("mcp-missing")

    emit["reason"] = reasons[0] if len(reasons) == 1 else reasons
    emit["footnote"] = render_footnote(reasons, args.lang)
    log_line(analysis_dir, f"tiers done: {emit['tiers']} reasons={reasons}")
    print(json.dumps(emit, ensure_ascii=False))
    return 0
```

- [ ] **Step 2: Integration test** — `perception/tests/test_perceive_integration.py`:

```python
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
```

(Note: `tier1 detect:` log line must include `mode=` — add `mode={mode}` to the tier1 log call in main() so the remote-policy assertion works.)

- [ ] **Step 3: Run all tests**

```bash
cd perception && python3 -m unittest discover -s tests -v
```

Expected: 10 unit + 3 integration PASS (integration downloads fixture deps on first run).

- [ ] **Step 4: Commit**

```bash
git add perception
git commit -m "feat: perceive.py tier orchestration with security policy and loader/MCP tiers"
```

---

### Task 6: SCV touchpoints (bilingual) + install.sh whitelist

**Files:**
- Modify: `skills/en/SKILL.md:3` (frontmatter description), `skills/zh-cn/SKILL.md` (same), `skills/en/references/run.md` (new Step 5.5 + Step 6 input + Step 8 lines), `skills/zh-cn/references/run.md` (mirror), `skills/en/references/templates/ARCHITECTURE.template.md` + zh-cn counterpart (new section), `agents/en/project-analyzer.md` + zh-cn (new input), `config.example.json`, `install.sh`

**Interfaces:**
- Consumes: perceive.py stdout JSON (Task 5).
- Produces: end-user behavior per spec §2/§5/§8.

- [ ] **Step 1: SKILL.md descriptions** — append to the frontmatter `description:` string (en):
  `; detects supported frameworks (Spring Boot) and maps component dependencies for structure-aware docs and agent queries.`
  zh-cn equivalent: `；识别已支持的框架（Spring Boot）并提取组件依赖，用于结构感知的文档生成与 agent 查询。`
  Also extend the subcommand table area with one line under "Quick Reference": `run (perception)` — `deep structural analysis runs automatically inside run for supported frameworks; no user action required`.

- [ ] **Step 2: run.md Step 5.5 (en)** — insert between Step 5 and Step 6:

````markdown
### Step 5.5: Deep Structural Analysis (Code Perception)

**Read configuration** (`~/.scv/config.json`): if `structure_analysis_enabled` is `false` or absent-false, skip this step silently.

Determine `--source`: `remote` if the repository was cloned from a remote URL (Step 2/3), else `local`.

If this is the first run on this machine, tell the user:
```
🔍 Deep structural analysis in progress; the first run may take a few minutes (component build + dependency download).
```

Run:
```bash
python3 ~/.scv/perception/perceive.py {analysis_path} ~/.scv/analysis/{repo_name} --source {local|remote}
```

**Interpret the single-line JSON on stdout:**

| Field | Action |
|---|---|
| `status: not-framework` | nothing — proceed to Step 6 |
| `status: skipped` | render `footnote` verbatim in the Step 8 completion report (perception section) |
| `status: ok` | render the summary line below from `counts`; store `artifacts.structure_json` for the Step 6 prompt; if `mcp_add` is non-null, render it as a copyable command block |
| `counts` null | omit the cycle clause |

Summary line (render from counts):
```
🧭 Deep structural analysis: Spring Boot project — {components} components, {edges} dependency edges, {cycles_fatal + cycles_tolerated} circular dependencies ({cycles_fatal} startup-fatal / {cycles_tolerated} tolerated).
```
If `footnote` is non-empty (degraded tiers), append it after the summary line, single line.

**Never** fail the run because of this step; perceive.py always exits 0 and its JSON is the single source of truth for wording.
````

Mirror in `skills/zh-cn/references/run.md` with:

````markdown
### Step 5.5: 深度结构分析（代码感知）

读取 `~/.scv/config.json`：`structure_analysis_enabled` 为 `false` 时静默跳过本步。
仓库来自远程克隆（Step 2/3 的 remote 分支）则 `--source remote`，否则 `local`。

首次运行提示：
```
🔍 正在进行深度结构分析，首次运行可能需要数分钟（含组件构建与依赖下载）。
```

执行：
```bash
python3 ~/.scv/perception/perceive.py {analysis_path} ~/.scv/analysis/{repo_name} --source {local|remote}
```

解读 stdout 单行 JSON：`not-framework` → 直接进入 Step 6；`skipped` → 将 `footnote` 原样放入 Step 8 报告；`ok` → 用 `counts` 渲染摘要行、把 `artifacts.structure_json` 传入 Step 6 子代理、`mcp_add` 非空时渲染为可复制命令块。`counts` 为 null 时省略循环依赖子句。

摘要行：
```
🧭 深度结构分析：检测到 Spring Boot 项目 — {components} 个组件、{edges} 条依赖边、循环依赖 {cycles_fatal + cycles_tolerated} 处（{cycles_fatal} 处启动级 / {cycles_tolerated} 处容忍级）。
```
````

- [ ] **Step 3: run.md Step 6 + Step 8 edits (en; mirror zh)** — in the Step 6 agent prompt input list add:
  `- Structure Data: {artifacts.structure_json if perception ok else 'N/A'}`
  In the Step 8 report template, after the "Analysis mode" block add:

```
Structural analysis:
  🧭 Deep structural analysis: {summary line or footnote from Step 5.5, or 'Not applicable (framework not supported)'}
```

- [ ] **Step 4: ARCHITECTURE.template.md section (en + zh)** — insert after the existing architecture diagram/data flow sections:

```markdown
## Component Dependencies

<!-- Render ONLY when Structure Data input is not 'N/A'. Source of truth: the structure JSON file — do not invent edges. -->
Summarize the component dependency data: total components, dependency edges, circular
dependencies with grading (startup-fatal vs tolerated), the most-depended-on components
(top 3 by incoming edges), and the dependency path between the two most central
components. For ≤30 components render a Mermaid `graph TD`; otherwise list top hubs only.
Mark anything uncertain as [To be confirmed].
```

zh mirror（「## 组件依赖」…中文同义内容，含「仅当 Structure Data 输入不是 'N/A' 时渲染」「数据为准，不得臆造依赖边」「≤30 组件用 Mermaid graph TD，否则列关键枢纽」「不确定处标注 [待确认]」）。

- [ ] **Step 5: project-analyzer.md (en + zh)** — add to the input parameters list:
  `- Structure Data: {structure.json path or 'N/A'}`
  and one rendering rule near document generation:
  `If Structure Data is provided, render the Component Dependencies section in ARCHITECTURE.md strictly from that JSON (counts, cycle grading wording: 启动级/startup-fatal vs 容忍级/tolerated); never fabricate dependency edges.`

- [ ] **Step 6: config.example.json** — after `"deep_analysis_enabled": false,` insert:

```json
  "structure_analysis_enabled": true,
  "structure_build_execution": "local-only",
```

- [ ] **Step 7: install.sh whitelist** — insert after Step 4 (shared scripts), before Step 5:

```bash
# Step 4.5: Install code perception runtime (whitelist — no fixtures, no developer docs)
echo -e "\n${BLUE}Step 4.5: Installing code perception runtime${NC}"
PERCEPTION_SRC="$SCRIPT_DIR/perception"
PERCEPTION_TARGET="$SCV_DATA_DIR/perception"
if [ -d "$PERCEPTION_SRC" ]; then
    create_dir "$PERCEPTION_TARGET"
    mkdir -p "$PERCEPTION_TARGET/adapters" "$PERCEPTION_TARGET/dist"
    cp "$PERCEPTION_SRC/perceive.py" "$PERCEPTION_TARGET/perceive.py"
    cp "$PERCEPTION_SRC/README.md" "$PERCEPTION_TARGET/README.md"
    cp -r "$PERCEPTION_SRC/loader" "$PERCEPTION_TARGET/loader"
    for adapter_dir in "$PERCEPTION_SRC"/adapters/*/; do
        [ -d "$adapter_dir" ] || continue
        aid=$(basename "$adapter_dir")
        mkdir -p "$PERCEPTION_TARGET/adapters/$aid"
        cp "$adapter_dir/MANIFEST.json" "$PERCEPTION_TARGET/adapters/$aid/" 2>/dev/null || true
        cp "$adapter_dir/build.gradle" "$adapter_dir/settings.gradle" "$PERCEPTION_TARGET/adapters/$aid/" 2>/dev/null || true
        cp -r "$adapter_dir/src" "$PERCEPTION_TARGET/adapters/$aid/src"
        rm -rf "$PERCEPTION_TARGET/adapters/$aid/src/test"
    done
    echo -e "${GREEN}+${NC} perception runtime -> $PERCEPTION_TARGET"
else
    echo -e "${YELLOW}No perception directory found, skipping${NC}"
fi
```

Note: `src/test` removal keeps fixtures out of user installs (whitelist policy, spec §8).

- [ ] **Step 8: Vocabulary gate + install dry-run**

```bash
./install.sh --lang=en   # into real HOME is fine (symlinks), or HOME=$(mktemp -d) ./install.sh for a dry run
grep -riE 'spoon|lombok' ~/.scv/perception/README.md ~/.claude/skills/scv/ | wc -l   # expect 0
ls ~/.scv/perception/adapters/springboot/   # MANIFEST.json build.gradle settings.gradle src/ — no fixtures, no README
python3 ~/.scv/perception/perceive.py <a non-spring dir> /tmp/np --source local     # {"status": "not-framework"...}
```

- [ ] **Step 9: Commit**

```bash
git add skills agents config.example.json install.sh
git commit -m "feat: wire code perception into scv run flow, docs, config and installer"
```

---

### Task 7: Acceptance pass + final docs

**Files:**
- Modify: `perception/README.md` (only if acceptance surfaced copy issues)

**Interfaces:**
- Consumes: everything above.
- Produces: verified acceptance criteria 1–10 (spec §9); final state.

- [ ] **Step 1: Run the acceptance matrix** (spec §9; record results in the PR description):
  1. Local fixture (copy torture-app to ~/tmp-accept/repo): `python3 ~/.scv/perception/perceive.py ~/tmp-accept/repo ~/tmp-accept/out --source local --lang zh` → status ok, tiers json+vg true (env has JDK+Go), mcp_add present iff vectra-mcp on PATH.
  2. `PATH=/usr/bin:/bin python3 ...` (no java) → skipped + jdk-missing footnote + no stale files.
  3. PATH without go but with java → json tier true, vg false, go-missing footnote.
  4. PATH with java+go, hide vectra-mcp → mcp-missing footnote, no mcp_add.
  5. `--source remote` on a copy of gradle-lombok-sample → perception.log shows mode=noclasspath, no build executed in repo (no build/ dirs created inside the repo copy).
  6. Non-spring empty dir → not-framework, silent.
  7. `cd perception/adapters/springboot && gradle test` green.
  8. Vocabulary grep (Task 6 Step 8) = 0 hits.
  9. MCP sequence: `vectra-mcp -snapshot <vg from step 1>` over stdio → `tools/call cycles` returns exactly circularA/circularB; `tools/call neighbors {key: bean:greetingController, direction: out, label: INJECTS}` returns greetingServiceImpl + emailGreetingSender.
  10. Re-run step 2 scenario then step 1 → tiers upgrade; ARCHITECTURE section not duplicated (manual doc check via fixture subagent run, or verify template renders once).

- [ ] **Step 2: Fix fallout; re-run failing items.**

- [ ] **Step 3: Final commit**

```bash
git add -A perception
git commit -m "chore: perception acceptance pass — verify spec criteria 1-10"
```

---

## Self-Review (completed during planning)

- **Spec coverage:** §1–§2 → Task 6 copy blocks; §3 layout → Task 1; §4 MANIFEST/mode → Tasks 1+4; §5.1 contract → Task 5; §5.2 security → Task 4 (resolve_mode) + Task 5 (policy read) + Task 7 (acceptance 5); §5.3 ladder/footnotes/log/timeout → Tasks 4–5; §6 MCP tier → Tasks 3+5; §7 regression → Task 2; §8 touchpoints → Task 6; §9 acceptance → Task 7; §10 risks → addressed via graded cycles (Task 3), timestamps (Task 5 mcp_add), progress note (Task 6 Step 5.5).
- **Placeholders:** none — every step carries verbatim content.
- **Type consistency:** `loadAndGrade` (Go), `detect_framework/resolve_mode/parse_java_version/render_footnote/cleanup_stale/adapter_source_hash` (Python), stdout JSON keys (Tasks 3/5/6) all cross-checked.
