# SCV Code Perception — Design Spec

- Date: 2026-10-13
- Status: approved direction (two forks resolved: v1 ships MCP tier; `perception/` layout)
- Provenance: feasibility analysis + 3-round spike (petclinic, Lombok sample, multi-module
  torture-app) — all validated against ground truth; BeanProbe analyzer currently lives in
  `beanprobe/` (uncommitted) awaiting the migration below.

## 1. Product positioning

- **Capability umbrella**: 「代码感知 / Code Perception」— SCV understands framework-level
  structure of a codebase, not just files and names.
- **Feature name (user-visible)**: 「深度结构分析 / Deep Structural Analysis」.
- **Vocabulary rules**: "Spring Boot" may appear (user's stack = input). Java, Spoon, Gradle,
  bean must NOT appear in user-facing text; beans are called 「组件 / components」.
- **No new subcommand.** Perception is embedded in `scv run`: the analyzer flow auto-detects
  the framework and runs the matching adapter. Users never choose "which language tool".
- **Supported adapters (v1)**: Spring Boot only. All user-visible copy states
  「已支持：Spring Boot」 explicitly — no generalization implied.

## 2. Value delivery (two stages)

1. **Immediate** (during `/scv run`): ARCHITECTURE.md gains a "组件依赖 / Component
   Dependencies" section; completion report gains a summary line
   (`检测到 Spring Boot 项目：30 个组件、24 条依赖边、1 处循环依赖`).
2. **Deferred** (future sessions): the graph lands as artifacts that agents query via MCP
   (multi-hop dependency, blast radius, cycles). This is the differentiated value —
   precision an LLM cannot reproduce by reading imports.

## 3. Repository layout

```
SCV/
├── perception/                        # capability layer (user docs talk capability, never language)
│   ├── README.md                      # capability doc + "how to add an adapter" contract
│   ├── perceive.py                    # entry: detect → env check → adapter → artifacts
│   ├── loader/                        # Go module: structure.json → structure.vg (imports github.com/projanvil/vectra)
│   └── adapters/
│       └── springboot/                # THE adapter (migrated from beanprobe/)
│           ├── MANIFEST.json          # declarative adapter contract (see §4)
│           ├── README.md              # adapter-developer facing (may talk Java/Spoon)
│           ├── build.gradle / settings.gradle / src/   # Gradle project (BeanProbe)
│           └── fixtures/              # regression suite with ground truth
├── skills/ agents/ eval/ docs/        # unchanged structure
```

Migration: move `beanprobe/` → `perception/adapters/springboot/` (currently untracked,
plain filesystem move), rootProject renamed `springboot-probe`, class name `BeanProbe`
stays (internal).

## 4. Adapter contract (MANIFEST.json)

```json
{
  "id": "springboot",
  "framework": "Spring Boot",
  "detect": {
    "marker_files": ["pom.xml", "build.gradle", "settings.gradle"],
    "markers": ["spring-boot", "org.springframework.boot"]
  },
  "requires": { "jdk": "17" },
  "build": "gradle installDist -q",
  "run": "build/install/springboot-probe/bin/springboot-probe {mode} {project} {out}",
  "artifact": "structure.json"
}
```

OCP/DIP: `perceive.py` depends only on MANIFEST + the artifact schema. Adding
`adapters/go-gin` later requires zero changes to perception core or skill docs.
No registry infrastructure until adapter #2 exists (YAGNI).

**Artifact schema (the contract)**: Vectra batch format —
`nodes[{key,label,props}]`, `edges[{srcKey,dstKey,label,props}]`. Chosen so the
MCP tier consumes it with no format change.

## 5. Layered runtimes & degradation (design-for-failure)

```
scv run <repo>
 └─ perceive.py <repo> <analysis_dir>
     1. framework detect (per MANIFEST)      miss → silent return (nothing to say)
     2. JDK probe: java -version | $JAVA_HOME/bin/java, parse major
        missing or <17 → {status: skipped, reason: jdk}                → footnote
     3. build adapter dist if absent (MANIFEST.build)
        failure → skipped + reason (network/gradle)                    → footnote
     4. run adapter: maven | gradlew | gradle | noclasspath fallbacks
        non-zero exit → skipped + reason                                → footnote
     5. write structure.json  ← ALWAYS the single source of truth (JDK tier)
     6. Go tier (optional): go toolchain present → loader → structure.vg
        missing/failure → JSON-only, footnote
     7. MCP tier (optional): vectra-mcp binary present → emit the
        `claude mcp add` hint in the completion report
        absent → footnote
```

Rules:

- Perception NEVER fails `/scv run`. Every tier degrades to a one-line footnote:
  「已跳过深度结构分析：未检测到 JDK 17+（可选增强，安装后重新运行 /scv run 即可启用）」.
- Footnotes are attributable (reason recorded), never silent — users must be able to
  distinguish "not analyzed" from "analyzed wrong".
- Cycle reporting is graded: constructor cycles = error (Spring fails startup),
  field/setter cycles = warning (container tolerates via proxies). structure.json
  carries the data; wording differs only at presentation.

## 6. MCP tier (v1, per decision)

- `perception/loader/`: ~60-line Go module requiring `github.com/projanvil/vectra`
  (public module; no local replace). Reads structure.json → AddNode/UpsertEdge →
  IndexProperty(stereotype…) → Seal → SaveFile → `structure.vg` in the analysis dir.
- Serving: `vectra-mcp -snapshot ~/.scv/analysis/{project}/structure.vg`.
  The completion report (and perception/README) shows the exact
  `claude mcp add vectra -- vectra-mcp -snapshot <path>` command only when the
  binary is detected.
- Known upstream gap (documented, non-blocking): `.vg` snapshots do not persist
  property-index declarations, so `lookup_property` is unavailable in `-snapshot`
  mode. Fix belongs in Vectra (persist index keys or re-register on load); bfs /
  neighbors / shortest_path / cycles / stats all work.

## 7. SCV touchpoints (minimal edits)

| File | Change |
|---|---|
| `skills/{en,zh-cn}/SKILL.md` | description: one capability sentence (user vocabulary) |
| `skills/*/references/run.md` | analysis flow gains "深度结构分析" step calling `~/.scv/perception/perceive.py` |
| `config.example.json` | `structure_analysis_enabled: true` (mirrors `deep_analysis_enabled`) |
| `install.sh` | copy `perception/` → `~/.scv/perception/` |
| artifacts | `~/.scv/analysis/{project}/structure.json` (+ `.vg` when Go tier runs) |

## 8. Acceptance criteria (v1)

1. `/scv run` on a Spring Boot repo with JDK+Go present produces: 4 docs + enhanced
   ARCHITECTURE section + structure.json + structure.vg + MCP hint line.
2. Same run with no JDK: analysis completes, footnote present, no artifacts, exit 0.
3. Same run with JDK but no Go: structure.json present, .vg absent, footnote notes tier skip.
4. Non-Spring repo: no perception output at all (silent).
5. Fixtures regression: adapter on `fixtures/gradle-lombok-sample` = 12 beans / 8 edges,
   `fixtures/torture-app` = 30 beans / 24 edges, 0 unresolved.
6. `cycles()` on torture-app fixture returns exactly the CircularA/B SCC through the
   served MCP snapshot.

## 9. Risks & mitigations

| Risk | Mitigation |
|---|---|
| Wrong graph is worse than none (agent trusts bad edges) | v1 graph only enriches docs + queryable artifacts; real-repo eval (extend `eval/` with real open-source Spring projects) gates any quality claims |
| Only one adapter under a broad "perception" umbrella | Explicit 「已支持：Spring Boot」 everywhere; no generalization |
| Dual runtime burden (JDK + Go) unknown adoption | Local one-line trigger log in `~/.scv/` (no telemetry) to measure tier hit-rate |
| Cycle alarm fatigue | Graded error/warning wording (§5) |

## 10. Out of scope (v1)

- Method-level call graph (`--with-callgraph` design reserved, off by default).
- Adapters beyond Spring Boot; adapter registry tooling.
- vectra-server HTTP mode / online incremental updates.
- Upstream Vectra snapshot-index fix (tracked as a Vectra PR, not SCV work).
