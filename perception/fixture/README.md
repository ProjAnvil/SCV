# Perception fixture harness

Multi-language source trees used to exercise `perceive.py` end to end — the
deterministic half of an `scv run`.

`scv run` Step 5.5 (`skills/*/references/run.md`) invokes:

```
python3 perceive.py <repo> <analysis_dir> --source local --lang en
```

`fixture/check.py` does exactly that, once per fixture, then asserts the result.
It runs as the last step of `make test`:

```bash
cd perception
make test                    # analyzer + orchestrator + Go tiers + fixtures
make test EXTENSIONS=        # same, in structure-only mode
```

The LLM-driven document tiers (README / SUMMARY / ARCHITECTURE / FILE_INDEX) are
out of scope. They need an agent; what this covers is whether perception parses
the tree and grades it correctly.

## Layout

| Path | Role |
|---|---|
| `repos/` | checked-in fixture sources, never modified by a run |
| `analysis/` | generated output, gitignored |
| `check.py` | runs `perceive.py` per repo, prints a table, asserts expectations |

Paths default to `fixture/repos` and `fixture/analysis`. Override with
`REPOS_HOME=` / `ANALYSIS_HOME=`. Nothing is written outside this directory, so
a clean checkout can run the harness without touching `~/.scv/analysis`. The
extension mode travels through `SCV_PERCEPTION_CONFIG`, so switching never
rewrites `~/.scv/config.json`.

## The two extension modes

`structure_extensions` in `~/.scv/config.json` decides what gets analysed.

| Mode | Value | Behaviour |
|---|---|---|
| Component tier | `["springboot"]` (default) | detects Spring, extracts beans and INJECTS edges. A repo without Spring markers reports `not-framework` and is skipped entirely |
| Structure mode | `[]` | skips framework detection; any Java repo carrying a `pom.xml` / `build.gradle` marker gets the File / Type / Method layer with CALLS, EXTENDS and IMPLEMENTS edges. No beans |

Structure mode is the only way a non-Spring Java repo is analysed at all. It
exists because the structure core is meant to be language-agnostic; today the
analyzer ships only `tree-sitter-java`, so in practice it means "plain Java".
If you only care about Spring Boot, leave the default and ignore this — the
suite passes in either mode, but the bean-level assertions are skipped in
structure mode because that mode produces no beans by design.

## Fixtures

| Repo | Language | Spring mode | Structure mode | Covers |
|---|---|---|---|---|
| `java-springboot-monolith` | Java 21 / Spring Boot 3 | `ok` 19 beans / 23 edges | `ok` | every injection style, conditional beans, one fatal + one tolerated cycle |
| `java-springboot-multimodule` | Java 21 / Gradle multi-module | `ok` 5 / 2 | `ok` | boot class in a submodule, cross-module injection |
| `java-plain-core` | Java 21, no Spring | `not-framework` | `ok` | generics, records, abstract classes, extends/implements, mutual recursion |
| `python-complex` | Python 3.11 | `not-framework` | `not-framework` | dataclasses, metaclasses, decorators, async |
| `golang-complex` | Go 1.22 | `not-framework` | `not-framework` | generics, interface embedding, goroutines, channels |

Python and Go always skip: the analyzer's only grammar dependency is
`tree-sitter-java`. Their purpose is to freeze that behaviour — the tier must
skip *cleanly* (exit 0, `status: skipped`), not crash or emit an empty graph.

## Frozen expectations

`check.py` asserts, among others:

- all four injection styles (`constructor`, `field`, `setter`, `beanMethod`) are
  extracted from the monolith;
- `bean:orderController` carries stereotype `RestController`;
- conditional annotations (`ConditionalOnMissingBean`, `ConditionalOnProperty`,
  `Profile`) reach the bean's `conditions` property;
- the constructor cycle `orderService <-> pricingService` is graded `fatal`,
  while the field cycle `notificationService <-> auditService` is `tolerated`;
- the chain `invoiceController -> invoiceService -> ledgerClient` crosses module
  boundaries;
- Python and Go emit no `structure.json`, and every repo exits 0.

## Known gaps this harness surfaced

1. **Wildcard imports hide stereotypes.** A class annotated `@RestController`
   through an on-demand import (`import org.springframework.web.bind.annotation.*;`)
   is extracted into the structure layer but never registered as a bean.
   `StatusController` freezes the gap: the check asserts it is *not* a bean, so
   closing the gap requires inverting that assertion.

2. **Stale dist binaries were silently reused.** `run_loader` / `build_graphmcp`
   only built when the binary was missing, so a user who installed an earlier
   SCV kept running the loader they first built. A 2026-10-13 loader predated
   the 2026-10-15 INJECTS cycle filter and reported every SCC as tolerated —
   including CALLS-only mutual recursion and single-node self loops. Both now
   rebuild when any Go source is newer than the binary.

## Note on `make test` in a sandbox

The analyzer's pytest suite uses pytest's default temporary directory. Sandboxes
that intercept filesystem calls (for example the WorkBuddy `sitecustomize` shim)
block `mkdir` there, and every test errors at setup. That is environmental, not
a repo defect:

```bash
cd analyzer && uv run pytest --basetemp=.pt    # 72 passed
```
