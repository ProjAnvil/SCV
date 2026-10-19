# Perception fixture harness

Multi-language source trees used to exercise `perceive.py` end to end — the
deterministic half of an `scv run`.

`scv run` Step 5.5 (`skills/*/references/run.md`) invokes:

```
python3 perceive.py <repo> <analysis_dir> --source local --lang en
```

This harness runs exactly that, once per fixture, against paths inside this
directory. The LLM-driven document tiers (README / SUMMARY / ARCHITECTURE /
FILE_INDEX) are deliberately out of scope: they need an agent, and what this
harness tests is whether perception parses the tree and grades it correctly.

## Usage

```bash
cd perception

make fixture-list                                  # which repos exist
make fixture-run                                   # Spring Boot component tier
make fixture-run EXTENSIONS=                       # structure-only mode
make fixture-run REPO=java-springboot-monolith     # one repo
make fixture-verify                                # run + assert expectations
make fixture-clean                                 # wipe generated output
```

Paths default to `fixture/repos` and `fixture/analysis`; override with
`REPOS_HOME=` / `ANALYSIS_HOME=`. Nothing is written outside this directory, so
a clean checkout can run the harness without touching `~/.scv/analysis`.

| Variable | Default | Meaning |
|---|---|---|
| `REPOS_HOME` | `fixture/repos` | where the fixture sources live |
| `ANALYSIS_HOME` | `fixture/analysis` | where `structure.json` / `structure.vg` land |
| `REPO` | *(empty)* | restrict the run to one fixture |
| `EXTENSIONS` | `springboot` | comma-separated; empty = structure-only mode |
| `PYTHON` | `python3` | interpreter used for `perceive.py` |

Extension mode matters: with the default `["springboot"]`, a plain-Java repo
reports `not-framework` and is skipped entirely. Structure mode
(`EXTENSIONS=`) is the only way to analyse it. The harness passes the mode
through `SCV_PERCEPTION_CONFIG`, so switching never rewrites
`~/.scv/config.json`.

## Files

| Path | Role |
|---|---|
| `repos/` | checked-in fixture sources (never modified by a run) |
| `analysis/` | generated output — gitignored |
| `run_fixtures.py` | runs `perceive.py` per repo and tabulates status/counts/cycles |
| `verify_fixtures.py` | asserts the frozen expectations |

## Fixtures

| Repo | Language | Expected | What it covers |
|---|---|---|---|
| `java-springboot-monolith` | Java 21 / Spring Boot 3 | `ok` | every injection style, conditional beans, one fatal + one tolerated cycle |
| `java-springboot-multimodule` | Java 21 / Gradle multi-module | `ok` | boot class in a submodule, cross-module injection |
| `java-plain-core` | Java 21, no Spring | `not-framework` in Spring mode; `ok` in structure mode | generics, records, abstract classes, `extends`/`implements`, mutual recursion |
| `python-complex` | Python 3.11 | `not-framework` | dataclasses, metaclasses, decorators, async |
| `golang-complex` | Go 1.22 | `not-framework` | generics, interface embedding, goroutines, channels |

Python and Go are expected to skip: perception's analyzer is Java-only (its only
grammar dependency is `tree-sitter-java`). Their purpose is to freeze that
behaviour — the tier must skip *cleanly* (exit 0, `status: skipped`), not fail
or emit an empty structure graph.

## Frozen expectations

`verify_fixtures.py` asserts, among others:

- all four injection styles (`constructor`, `field`, `setter`, `beanMethod`) are
  extracted from the monolith;
- `bean:orderController` carries stereotype `RestController`;
- conditional annotations (`ConditionalOnMissingBean`, `ConditionalOnProperty`,
  `Profile`) reach the bean's `conditions` property;
- the constructor cycle `orderService <-> pricingService` exists and is graded
  `fatal`, while the field cycle `notificationService <-> auditService` is
  graded `tolerated`;
- the multi-module chain `invoiceController -> invoiceService -> ledgerClient`
  crosses module boundaries;
- Python and Go emit no `structure.json`.

## Known gaps this harness surfaced

1. **Wildcard imports hide stereotypes.** A class annotated `@RestController`
   through an on-demand import (`import org.springframework.web.bind.annotation.*;`)
   is extracted into the structure layer but never registered as a bean.
   `StatusController` freezes the gap: the verifier asserts it is *not* a bean,
   so closing the gap requires inverting that assertion.

2. **Stale dist binaries were silently reused.** `run_loader` / `build_graphmcp`
   only built when the binary was missing, so a user who installed an earlier
   SCV kept running the loader they first built. A 2026-10-13 loader predated
   the 2026-10-15 INJECTS cycle filter and reported every SCC as tolerated —
   including CALLS-only mutual recursion and single-node self loops. Both now
   rebuild when any Go source is newer than the binary.
