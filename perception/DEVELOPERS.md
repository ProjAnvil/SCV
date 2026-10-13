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
