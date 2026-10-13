# BeanProbe

Static Spring Boot bean dependency-graph extractor for SCV. Scans Java source code
(Maven or Gradle projects, multi-module supported) with [Spoon](https://github.com/INRIA/spoon)
and emits a normalized JSON graph — bean nodes plus `INJECTS` edges — in
[Vectra](https://github.com/projanvil/vectra) batch format, ready to load and query via MCP.

Status: **v0.1, spike-derived** (October 2026 feasibility spike; validated against
spring-petclinic and the bundled fixtures — see "Verification" below). Interfaces may change.

## Build & run

Requires JDK 17+ and Gradle (or a target project's own `gradlew`).

```bash
cd perception/adapters/springboot
gradle installDist                # build once
./build/install/springboot-probe/bin/springboot-probe gradle /path/to/spring-project /tmp/beans.json
```

Or directly:

```bash
gradle run --args="gradle /path/to/spring-project /tmp/beans.json"
```

Modes:

| Mode | Classpath source |
|---|---|
| `maven` | `MavenLauncher` resolves the classpath via `mvn dependency:build-classpath` |
| `gradle` | A non-invasive init script (`src/main/resources/cp-init.gradle`) registers a `spoonPrintClasspath` task on the target project — target build files are never modified. Multi-module projects: one source root + classpath per module; unbuilt sibling jars are filtered out (their sources are analyzed directly) |
| `noclasspath` | Degraded mode, no build tool needed; type resolution is weaker and results carry more ambiguity |

Output: `nodes[{key,label,props}]` / `edges[{srcKey,dstKey,label:"INJECTS",props}]`
(Vectra `POST /v1/batch` / `inject_batch` schema).

## The storage & MCP chain

```bash
# 1. extract
springboot-probe gradle /path/to/spring-project beans.json
# 2. load into vectra (small Go loader importing github.com/projanvil/vectra:
#    AddNode/AddEdge per JSON entry, IndexProperty("stereotype"...), Seal, SaveFile)
perception-loader beans.json beans.vg
# 3. serve to any MCP host (Claude Code: claude mcp add vectra -- vectra-mcp -snapshot beans.vg)
vectra-mcp -snapshot beans.vg
```

Agents can then ask multi-hop questions (`bfs`, `neighbors`, `shortest_path`) and get
dependency-cycle reports (`cycles`) directly over the bean graph.

## What it detects

- Stereotypes: `@Component/@Service/@Repository/@Controller/@RestController/@ControllerAdvice/
  @Configuration` plus any custom annotation transitively meta-annotated with them
  (including `@SpringBootApplication`), `@Bean` methods (static included), Spring Data
  repository interfaces, `@EnableConfigurationProperties`-registered property classes.
- Injection edges via: constructor (Spring rules: `@Autowired`-ctor > sole non-default ctor),
  `@RequiredArgsConstructor` (Lombok final/`@NonNull` fields), `@Autowired` fields
  (including inherited from abstract superclasses), setters, `@Bean` method params.
- Resolution: `@Qualifier` > `@Primary` > by-name fallback > honest `ambiguous: true`.
- Wrappers: `List/Set/Collection/Optional/ObjectProvider` are unwrapped; collections
  link to ALL candidates by design (never flagged ambiguous).
- `@ConditionalOn*`/`@Profile` are recorded as node props (`conditions`), never resolved.
- `@Value` params/fields are skipped (not bean dependencies).

## Known static limitations (by design)

- Beans outside the effective `@ComponentScan` scope are still reported (static
  over-approximation; a fixture demonstrates this: `org.thirdparty.compat`).
- Conditional assembly truth values are unknowable statically — flagged, not resolved.
- Generic base-class injection (`AbstractService<T>` with `@Autowired Repo<T>`) needs
  generics substitution; not yet implemented.
- AOP proxies and runtime-registered beans are invisible.

## Fixtures (regression suite)

`src/test/resources/fixtures/` holds two self-contained Spring Boot 3.4 projects
with hand-computed ground truth:

- `gradle-lombok-sample` — single module, Lombok `@RequiredArgsConstructor`,
  `@Qualifier`, ambiguity marking, `@ConditionalOnProperty`/`@Profile`, `@Bean` params.
- `torture-app` — three Gradle modules; custom meta-annotations, inherited `@Autowired`
  fields, record beans, `Optional`/`List` injection, `@Value` mixed ctors, multi-ctor
  `@Autowired` selection, `@Primary`/by-name disambiguation, `@ConditionalOnMissingBean`
  default-override pattern, `@EnableConfigurationProperties`, a deliberate circular
  dependency (caught by vectra `cycles()`), and an out-of-scan false-positive probe.

Expected results, asserted by the regression suite (`gradle test`, fixtures are
copied to a temp dir so nested builds never touch the source tree):
**sample = 12 beans / 8 edges, torture-app = 30 beans / 24 edges, 0 unresolved**
(verify after any analyzer change).

```bash
gradle test
gradle run --args="gradle src/test/resources/fixtures/gradle-lombok-sample /tmp/sample.json"
gradle run --args="gradle src/test/resources/fixtures/torture-app /tmp/torture.json"
```
