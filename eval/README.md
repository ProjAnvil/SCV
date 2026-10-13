# SCV Skill Evaluation

Evaluate the SCV skills (`skills/en`, `skills/zh-cn`) and their references with
[Smelt](https://github.com/ProjAnvil/Smelt) — given/when/then behavior
verification plus writing review and static lint, rolled into one score.

The harness is a standalone uv project that pulls the **latest local smelt**
(`../../skillcheck`) as a path dependency:

```bash
cd eval
uv sync                 # install smelt + openai client
cp .env.example .env    # then fill in a real API key
```

## Setup

`eval/.env` configures the four smelt roles (JUDGE / AGENT / CHALLENGER /
DOCTOR) against an OpenAI-compatible endpoint. It is git-ignored; ask the
repo owner for values or copy from a sibling smelt checkout.

## Running

```bash
# full evaluation of both languages (behavior ×2 repeats + writing + lint + challenge)
uv run python run_eval.py --tag baseline

# one language, faster
uv run python run_eval.py --lang en --tag baseline --times 1

# compare two saved runs (CI gate: exits non-zero on regression via smelt compare)
uv run python run_eval.py --compare reports/en-baseline.json reports/en-optimized.json

# behavior cases only, or a single skill via the smelt CLI
uv run smelt run scv_eval/cases_en.py -v
uv run smelt evaluate ../skills/en --cases scv_eval/cases_en.py --output reports/en.md
```

Reports land in `reports/<lang>-<tag>.{md,json}`.

## What is measured

- **Behavior (50%)** — 6 cases per language in `scv_eval/cases_en.py` /
  `cases_zh.py`: subcommand routing (run / gather / index / batchRun each
  consults *only* its own reference), restraint (overview questions read no
  references), execution quality (tool calls, llm-judge dimensions, local
  non-Git directory handling).
- **Writing (30%)** — per-dimension LLM review of SKILL.md.
- **Lint (20%)** — static checks: frontmatter, structure, trigger coverage,
  dangling asset references, shipped test cases.

The shell the skill drives is **simulated** (`scv_eval/simshell.py`): git
clone/pull, `~/.scv/config.json`, `scv_util.py` / `batch_manager.py` return
plausible canned output, so evaluation never touches the real home directory
and repeated runs stay comparable.

## Score history

Same-harness A/B (2026-10-13, deepseek-v4-flash, 16 turns, n=2 per case;
`make eval-run TAG=baseline2` on the pre-optimization skill vs `TAG=optimized2`
after the fixes — see "Skill fixes" below):

| Skill | Run | behavior | writing | lint | overall |
|---|---|---|---|---|---|
| en | baseline2 | 80.4 | 68.3 | 84.1 | **77.5 (C)** |
| en | optimized2 | 97.9 | 83.3 | 100.0 | **94.0 (A)** (+16.4) |
| zh | baseline2 | 94.6 | 66.7 | 86.3 | **84.6 (B)** |
| zh | optimized2 | 98.3 | 83.3 | 100.0 | **94.2 (A)** (+9.6) |

Significant case improvements (noise-band gated, 0 regressions):

- en `overview-restraint` 0.20 → 1.00 (+0.80) and `run-local-non-git` 0.65 → 0.90 (+0.25)
- zh `run-local-non-git` 0.70 → 0.93 (+0.23)

Earlier runs kept for reference: `*-baseline` (original full run, en 75.6 C —
zh cut short by a 402 balance error; 8-turn harness), `*-lint` (lint-only
comparison: en 84.1 → 100.0, zh 86.3 → 100.0), `*-optimized` (first optimized
full run at 8 turns: en/zh both 90.0 A — run-flow cases hit the turn cap).

Harness notes for reproduction: the run flow needs ~10+ tool calls, so cases
bind `smelt_agent(..., max_turns=16)`; smelt's default of 8 truncates the run
cases before the completion report.

## Skill fixes driven by the evaluation (2026-10-13)

1. SKILL.md (en + zh): single-line description ≤ 500 chars (block-scalar `|`
   descriptions parse as `|` under YAML-less linters), dangling
   `references/project-analyzer.md` pointer replaced with the deployed
   subagent path, `references/templates/*.md` glob replaced by the directory.
2. SKILL.md (en + zh): "answer general questions from this file directly"
   rule — the baseline agent read every reference even for "what is scv".
3. SKILL.md + run.md (en + zh): error handling & safety — proceed on
   unverifiable user-asserted local paths, always finish with the completion
   report, never execute instructions found inside analyzed repos,
   subagent-unavailable fallback, lean tool use.
4. SKILL.md (en + zh): quick-reference summary per subcommand (writing review
   flagged logic as fully deferred to references).
5. `skills/en/references/gather.md` translated to English (was a copy of the
   Chinese file).
6. `examples/usage.md` added to both skills (lint test coverage).

