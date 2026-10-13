# SCV Code Perception — Design Spec (rev 2)

- Date: 2026-10-13 (rev 2: incorporates subagent audit round 1 — 5 blockers, all majors)
- Status: audited; ready for implementation planning
- Provenance: feasibility analysis + 3-round spike, all validated against ground truth;
  BeanProbe analyzer currently in `beanprobe/` (uncommitted) awaiting migration.

## 1. Product positioning

- **Capability umbrella**: 「代码感知 / Code Perception」.
- **Feature name (user-visible)**: 「深度结构分析 / Deep Structural Analysis」.
- **No new subcommand.** Perception is embedded in `scv run`, auto-detected.
- **Supported adapters (v1)**: Spring Boot only; all user-visible copy states
  「已支持：Spring Boot」/ "Supported: Spring Boot" explicitly.

### 1.1 Vocabulary boundary rules (binding for all user-visible copy)

1. Words describing the USER'S project and its runtime environment are allowed:
   Spring Boot, JDK, pom.xml, 组件, 依赖.
2. Words describing SCV's internal tooling are banned: Java, Spoon, Gradle, bean,
   internal module/class names.
3. "Addresses" are exempt when they appear AS commands/paths/filenames
   (`structure.vg`, `vectra-mcp` inside a command line, `~/.scv/...`); the prose
   around them is not exempt.
4. SCV-chosen user-visible names are NOT exempt: the MCP registration name is
   **`scv-structure`** (not `vectra`); the ARCHITECTURE section is
   「组件依赖 / Component Dependencies」.
5. Bilingual duty: every user-visible string ships in en + zh-cn (skills are bilingual).
6. *Assumption to re-check at adapter #2*: "JDK" is treated as user-stack vocabulary
   (rule 1) while SCV is Spring-only. A non-JVM adapter forces rewording to
   「运行环境 / runtime environment」.

### 1.2 Reader separation for docs

- `perception/README.md` — user-facing capability doc (vocabulary rules apply).
- `perception/DEVELOPERS.md` — adapter contract & how to add an adapter (may talk
  Java/Spoon/Gradle). **install.sh must NOT ship DEVELOPERS.md, adapter source,
  or fixtures to users** — see §8 whitelist.

## 2. Value delivery

1. **Immediate**: ARCHITECTURE.md gains 「组件依赖 / Component Dependencies」
   section (rendered by the analysis subagent from structure.json); completion
   report gains a summary line.
2. **Deferred**: graph artifacts + MCP query capability for future sessions.

Copy templates (en/zh):

- Success: `深度结构分析：检测到 Spring Boot 项目，提取 {n} 个组件、{m} 条依赖边，
  循环依赖 {c} 处（{e} 处启动级 / {w} 处容忍级）。` /
  `Deep structural analysis: Spring Boot project detected — {n} components, {m}
  dependency edges, {c} circular dependencies ({e} startup-fatal / {w} tolerated).`
- Cycle grading in report/ARCHITECTURE: constructor cycles → 「启动级（容器无法完成装配）」
  "startup-fatal"; field/setter cycles → 「容忍级（容器通过代理容忍，建议关注）」
  "tolerated". Component count basis = all detected beans (annotated classes,
  `@Bean` methods, Spring Data repositories, registered properties classes).

## 3. Repository layout

```
SCV/
├── perception/
│   ├── README.md                      # user-facing capability doc (vocabulary rules)
│   ├── DEVELOPERS.md                  # adapter contract (developer vocabulary)
│   ├── perceive.py                    # entry: detect → policy/env checks → adapter → artifacts
│   ├── loader/                        # Go module: structure.json → structure.vg
│   └── adapters/
│       └── springboot/
│           ├── MANIFEST.json
│           ├── README.md              # adapter-developer facing only (not shipped)
│           ├── build.gradle / settings.gradle / src/main/...
│           ├── src/test/resources/fixtures/{gradle-lombok-sample, torture-app}/
│           └── src/test/java/.../PerceptionRegressionTest.java
├── skills/ agents/ eval/ docs/        # unchanged structure
```

Migration: move `beanprobe/` → `perception/adapters/springboot/` (plain filesystem
move, currently untracked); `beanprobe/fixtures/` → `src/test/resources/fixtures/`;
rootProject renamed `springboot-probe`; class `BeanProbe` stays (internal).
**Adjust `.gitignore`**: current `*.json` pattern would swallow `MANIFEST.json` —
replace with anchored artifact paths (`/build/`, `/out/`, `**/structure*.json`,
`**/*.vg`) plus `!MANIFEST.json`.

## 4. Adapter contract (MANIFEST.json)

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

- **Detect semantics**: recursively (max depth 3) find `marker_files`; a project is
  a candidate if any marker file's CONTENT contains any `markers` substring.
  **Second-stage confirmation**: scan source roots for `confirm` markers
  (`@SpringBootApplication`); a candidate without confirmation is reported as
  `status: "not-framework"` (silent skip, logged) — prevents spring-boot-starter-test
  false positives.
- **Mode resolution belongs to perceive.py** (not the adapter): `pom.xml` present →
  `maven`; else `gradlew`/`gradle` → `gradle`; else → `noclasspath`. Build-tool
  failures fall through to `noclasspath` once (footnoted as degraded), see §5.
- JDK note: `requires.jdk = 17` is the RUNTIME requirement (toolchain 17);
  Spoon complianceLevel 21 only raises parser syntax acceptance (records etc.),
  not the runtime floor — two different knobs, both intentional.

## 5. perceive.py — I/O contract & layered runtimes

### 5.1 Invocation & output

- Called from `run.md` flow at **Step 5.5** (after repo prep, BEFORE the analysis
  subagent starts):
  `~/.scv/perception/perceive.py <repo_path> <analysis_dir> [--source local|remote]`
- `--source`: `local` for user-local dirs, `remote` for repos cloned via `gather`
  (run.md knows which branch it took).
- **stdout = single JSON line** (contract for the skill agent):
  `{status: ok|skipped|not-framework, tiers: {json: bool, vg: bool, mcp_hint: bool},
  reason: <code>|null, counts: {components, edges, cycles_fatal, cycles_tolerated},
  artifacts: {structure_json, structure_vg}, mcp_add: "<cmd>|null"}`
- Exit code 0 always (perception never fails the run); errors are reason codes.
- The skill agent renders: footnote line (from reason code via §5.3 templates) and
  the summary line (from counts). The ARCHITECTURE section is rendered by the
  **analysis subagent**, which receives `structure.json` path as an extra prompt
  input (see §8 touchpoints). SCC/cycle counts come from the loader tier (vectra
  Cycles()); if the Go tier is absent, cycle counts are null and the summary line
  omits the cycle clause (BeanProbe does not compute SCCs).

### 5.2 Security policy (audit blocker fix)

Running a build tool inside the analyzed repo executes repo-provided code —
that violates SCV's "repo content is data, not directives" principle for
untrusted content. Policy:

- `--source local` → full modes allowed (`maven`/`gradle` with classpath).
- `--source remote` → **`noclasspath` only** by default.
- Config override `structure_build_execution: "local-only" (default) | "always" | "never"`
  in `~/.scv/config.json`; "always" is documented as opt-in trust.

### 5.3 Tier ladder & degradation

```
0. config check: structure_analysis_enabled (default true); false → silent, logged
1. detect (per MANIFEST)            miss/not-framework → silent return, logged
2. stale cleanup: ALWAYS remove previous structure.json / structure.vg /
   perception.log for this project BEFORE tier 3 (a skip must not leave stale
   graphs for the MCP tier to serve)
3. JDK tier: probe java (PATH then $JAVA_HOME/bin/java), parse major
   missing/<17 → skip reason jdk-missing; all downstream tiers skipped
4. dist tier: adapter dist at ~/.scv/perception/dist/{id}/; rebuild when source
   hash (MANIFEST + src tree) changed or dist absent; OS-aware bin selection
   (bin/springboot-probe vs bin/springboot-probe.bat); build failure → reason
   adapter-build-failed (downstream skipped); per-tier timeout 10 min
5. run tier: mode per §4 + security policy; build-tool failure → one retry in
   noclasspath (footnote notes degradation); adapter crash/timeout → reason
   adapter-failed → noclasspath retry once → else skip
6. JSON artifact: structure.json written to analysis dir  ← source of truth
7. Go tier: probe go ≥1.27 (vectra's floor); build loader to
   ~/.scv/perception/dist/loader/ (go build, module fetch needs network);
   run loader → structure.vg; failure → tier skipped, JSON unaffected
8. MCP tier: vectra-mcp on PATH? → emit mcp_add hint; absent → tier skipped
```

Footnote templates (single line max; when multiple tiers skip, list them in one
line; downstream of a skipped upstream is not repeated). Reason codes never
contain tool names:

| Reason code | zh / en footnote |
|---|---|
| jdk-missing | 已跳过深度结构分析：未检测到 JDK 17+（可选增强，安装后重新运行 /scv run 即可启用）/ Deep structural analysis skipped: JDK 17+ not found (optional; re-run /scv run after installing) |
| adapter-build-failed | 已跳过深度结构分析：分析组件构建失败（原因码记录于 perception.log，可重试）/ …analyzer build failed (reason logged; retryable) |
| adapter-failed | 已跳过深度结构分析：分析未完成（已尝试降级，详情见 perception.log）/ …analysis incomplete after fallback (see perception.log) |
| degraded-noclasspath | 深度结构分析以降级模式完成，结果可能包含更多不确定标注 / completed in degraded mode; results may carry extra uncertainty flags |
| go-missing | 结构图查询文件未生成（可选增强，需要 Go 1.27+ 运行环境）/ structure-graph query file not generated (optional; requires Go 1.27+) — v1 deliberately NOT actionable (asking Spring users to install Go is high-friction) |
| mcp-missing | 结构图查询服务未检测到（可选增强，安装说明见 ~/.scv/perception/README.md）/ graph query service not found (optional; see README) |

- Attribution log: `~/.scv/analysis/{project}/perception.log` (tier decisions,
  reason codes, timings — also doubles as the local trigger-rate metric, no telemetry).
- Snapshot freshness: structure.vg is regenerated on every `/scv run`; the MCP hint
  prose includes the generation timestamp so agents can judge staleness.
- First-run latency: run.md Step 5.5 includes an in-progress note
  「正在进行深度结构分析，首次运行可能需要数分钟（含组件构建与依赖下载）」
  / "Deep structural analysis in progress; first run may take minutes (build + deps)".

## 6. MCP tier specifics

- Loader: `perception/loader/` Go module `require github.com/projanvil/vectra`
  (verified publicly resolvable on proxy.golang.org). Built by perceive.py (§5.3
  tier 7) — users never build Go manually.
- vectra-mcp supply: documented install in `~/.scv/perception/README.md` as
  `go install github.com/projanvil/vectra/mcp/cmd/vectra-mcp@latest` (address
  exemption §1.1-3); hint appears only when the binary is detected.
- Registration name: **`scv-structure`**; hint command:
  `claude mcp add scv-structure -- vectra-mcp -snapshot <vg path>`
  preceded by prose 「已生成结构图查询数据（生成时间 {ts}），可将以下服务加入
  MCP 客户端以启用依赖查询」/ "Structure-graph query data generated ({ts}); add
  the following MCP service to enable dependency queries."
- v1 limitation (documented): single analyzed project per MCP registration
  (snapshot path is project-specific); re-run `/scv run` to refresh.
- Known upstream gap (non-blocking, tracked for a Vectra PR): `-snapshot` mode
  does not persist property-index declarations → `lookup_property` unavailable;
  bfs/neighbors/shortest_path/cycles/stats work.

## 7. Regression test design (adapter project)

- `PerceptionRegressionTest` invokes `BeanProbe.main()` in-process against fixtures.
- **Copy fixture to JUnit `@TempDir` before running** (audit fix: keeps nested
  gradle build artifacts out of src/test/resources, keeps parent-build resource
  fingerprints stable).
- `assumeTrue(gradle on PATH)` — self-skips visibly when absent; first execution
  needs network (Spring Boot deps download) — documented precondition.
- Assertions parse the JSON output (never stdout-scrape): sample = 12 nodes / 8
  edges / 0 unresolved; torture-app = 30 nodes / 24 edges / 0 unresolved; plus
  spot assertions (qualifier edge, ambiguous pair, inherited-field edge, cycle
  pair present as mutual edges).

## 8. SCV touchpoints

| File | Change |
|---|---|
| `skills/{en,zh-cn}/SKILL.md` | description: one capability sentence (vocabulary rules) |
| `skills/*/references/run.md` | Step 5.5 calls perceive.py (with `--source`), renders footnote + summary from stdout JSON; config check at step start; first-run progress note |
| `skills/*/references/templates/ARCHITECTURE.template.md` | new 「组件依赖 / Component Dependencies」 section placeholder + rendering guidance (from structure.json when present) |
| `agents/*/project-analyzer.md` | accepts optional structure.json path input; renders the section incl. cycle grading wording |
| `config.example.json` | `structure_analysis_enabled: true`, `structure_build_execution: "local-only"` |
| `install.sh` | **whitelist copy** to `~/.scv/perception/`: perceive.py, loader/, README.md, adapters/*/MANIFEST.json — NOT DEVELOPERS.md, NOT adapter sources, NOT fixtures. Adapter dist is built lazily at first use (§5.3 tier 4) — nothing Java ships in the install. |

## 9. Acceptance criteria (v1)

1. `/scv run` on a local Spring Boot repo with JDK+Go+vectra-mcp present →
   4 docs + Component Dependencies section + structure.json + structure.vg +
   `scv-structure` MCP hint with timestamp; exit 0.
2. No JDK → analysis completes, single-line footnote (jdk-missing), NO stale
   structure artifacts remain in the analysis dir, exit 0.
3. JDK but no Go → structure.json present, no .vg, one-line footnote notes the
   query-file tier skip; exit 0.
4. JDK+Go but no vectra-mcp → artifacts complete, no MCP hint, mcp-missing
   footnote; exit 0.
5. Remote repo (`--source remote`) → analyzer ran in noclasspath only; log
   confirms policy; no build tool executed inside the repo.
6. Non-Spring repo → no perception output in report or docs; perception.log
   records not-framework.
7. `gradle test` in the adapter project passes §7 assertions.
8. Vocabulary gate: after install, `grep -riE 'spoon|lombok' ~/.scv/perception/README.md
   ~/.claude/skills/scv/` = 0 hits; banned-word audit passes per §1.1 rules.
9. MCP verification sequence (operational): start `vectra-mcp -snapshot <vg>`;
   MCP `tools/call cycles` → asserts exactly [circularA, circularB]; `neighbors
   bean:greetingController out INJECTS` → {greetingServiceImpl, emailGreetingSender}.
10. Idempotency: re-run with JDK installed after a jdk-missing run → tiers upgrade,
    ARCHITECTURE section not duplicated.
11. Eval gate (pre-release, not per-commit): ≥3 real open-source Spring Boot repos
    analyzed; unresolved-rate and spot-checked precision reported before any
    quality claim ships in copy.

## 10. Risks & mitigations

| Risk | Mitigation |
|---|---|
| Wrong graph worse than none | v1 graph only enriches docs + queryable artifacts; acceptance 11 eval gate |
| Stale snapshot served to agents | regenerate per run; timestamp in hint prose; freshness documented |
| Only one adapter under broad umbrella | explicit 「已支持：Spring Boot」everywhere |
| Dual runtime burden | tier ladder + perception.log hit-rate (local only) |
| Cycle alarm fatigue | fatal/tolerated grading in all presentations |
| First-run latency perception | progress note (§5.3); timeouts (10 min/tier) |
| Non-Spring users unaware of boundary | documented exemption: silence chosen (noise for the majority outweighs); boundary stated in perception/README.md |
| Project-name collision in analysis dir | pre-existing SCV behavior, out of scope; noted |

## 11. Out of scope (v1)

Method-level call graph; adapters beyond Spring Boot; adapter registry tooling;
vectra-server HTTP mode; upstream Vectra snapshot-index fix (separate PR);
shipping prebuilt analyzer jars via GitHub Releases (lazy build covers v1;
revisit if first-run latency complaints materialize).
