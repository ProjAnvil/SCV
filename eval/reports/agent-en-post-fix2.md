# Agent Evaluation Report: project-analyzer

- Path: `/Users/yuhaochen/Documents/codebase/projanvil/SCV/agents/en/project-analyzer.md`
- Overall: **95.1 / 100**  Grade: **A**
- Parts: behavior 100.0, writing 83.8, lint 100.0

## Behavior Tests

### ✅ agent-standard-four-docs (100 pts)
- ✔ `tool_call(Glob)` 1.00 (threshold 1.00) — Glob called 2 time(s)
- ✔ `write_contains('README.md', 'mini-orders')` 1.00 (threshold 1.00) — content fragment found
- ✔ `write_contains('SUMMARY.md', 'mini-orders')` 1.00 (threshold 1.00) — content fragment found
- ✔ `write_contains('ARCHITECTURE.md', 'mini-orders')` 1.00 (threshold 1.00) — content fragment found
- ✔ `write_contains('FILE_INDEX.md', 'mini-orders')` 1.00 (threshold 1.00) — content fragment found

### ✅ agent-structure-data-rendered (100 pts)
- ✔ `write_contains('ARCHITECTURE.md', 'Component Dependencies')` 1.00 (threshold 1.00) — content fragment found
- ✔ `write_contains('ARCHITECTURE.md', 'startup-fatal')` 1.00 (threshold 1.00) — content fragment found
- ✔ `write_contains('ARCHITECTURE.md', 'orderService')` 1.00 (threshold 1.00) — content fragment found

### ✅ agent-deep-off-no-shell (100 pts)
- ✔ `no_tool_call(Bash)` 1.00 (threshold 1.00)

### ✅ agent-deep-on-uses-codebones (100 pts)
- ✔ `tool_call(Bash)` 1.00 (threshold 1.00) — Bash called 6 time(s)
- ✔ `write_contains('SUMMARY.md', 'mini-orders')` 1.00 (threshold 1.00) — content fragment found

## Static Lint

Total 100.0, grade A

| Check | Score | Result |
|---|---|---|
| Agent frontmatter (Claude Code load contract) | 100.0 | ✅ |
| Metadata (frontmatter / name / description) | 100.0 | ✅ |
| Delegation (description routes correctly) | 100.0 | ✅ |
| Document structure (sections / substance) | 100.0 | ✅ |
| Clarity (placeholders / length) | 100.0 | ✅ |

## Writing Review (LLM)

| Dimension | Score | Comment |
|---|---|---|
| Delegation fit | 0.90 | The description clearly states the deliverable (4 named documents) and the trigger context (scv run flow), so a main agent can route confidently. |
| Prompt quality | 0.90 | Role, phased workflow, template contract, input schema, and output checklist are all explicit, though the template files themselves are external dependencies. |
| Boundaries | 0.95 | Strong boundary section: read-only analysis, no Bash unless Deep Analysis is on, deliverables limited to 4 docs, fail-loudly on missing inputs, and explicit hand-back instruction. |
| Conciseness | 0.60 | The prompt is long and repeats the no-Bash rule and deep-analysis enrichment guidance in multiple places, which could be tightened without losing clarity. |

> A well-structured, boundary-conscious analyzer agent whose main weakness is verbosity and some redundant restatement of rules.

## Challenge (adversarial probes)

| Type | Trigger | Verdict | Note |
|---|---|---|---|
| route | I just inherited this repo from a dev who left. Before I touch anything I want the lay of the land — what the modules do, how they talk to each other, where the entry points are. Can you put together a proper onboarding pack for me? | ✔ survived |  |
| route | We're doing a handover next week and I need the standard scv run output for /home/me/work/payments-api — the four docs, into ./handover-docs, project name 'Payments API', deep analysis on, skeleton is at ./handover-docs/.codebones_skeleton.md. | ✔ survived |  |
| route | Can you read through this Go service and write me a FILE_INDEX.md plus an ARCHITECTURE.md? I only want those two, not the whole set. | ✔ survived |  |
| no-route | The README in this repo is stale — the install steps are wrong and the API examples don't match the code anymore. Fix it up so it's accurate. | ✘ break | delegated to ['project-analyzer'] |
| no-route | Review this PR — check the new OrderService refactor for bugs, race conditions, and whether the error handling is right. | ✘ break | delegated to ['project-analyzer'] |
| no-route | Run the test suite and tell me which tests fail, then fix the failing ones. | ✔ survived |  |
| ambiguous | Give me a quick high-level summary of what this codebase does — like a paragraph or two, nothing formal. | ✘ break | The agent failed to deliver the requested informal summary, instead reporting a subagent plumbing failure and asking the user to paste repo contents, rather than answering inline or confirming scope as the criteria expect. |
| ambiguous | Analyze this repo and write up the architecture — but also update the existing docs/ folder to match, and add a CHANGELOG entry for the analysis. | ✔ survived |  |

Suggested case for break `The README in this repo is stale — the install steps are wrong and the API examples don't match the code anymore. Fix it up so it's accurate.`:

```python
new_case("probe-no-route")
    .given(router_agent(AGENTS_DIR, llm=router_llm))
    .when(task("The README in this repo is stale — the install steps are wrong and the API examples don't match the code anymore. Fix it up so it's accurate."))
    .then(no_delegation())
```

Suggested case for break `Review this PR — check the new OrderService refactor for bugs, race conditions, and whether the error handling is right.`:

```python
new_case("probe-no-route")
    .given(router_agent(AGENTS_DIR, llm=router_llm))
    .when(task('Review this PR — check the new OrderService refactor for bugs, race conditions, and whether the error handling is right.'))
    .then(no_delegation())
```

Suggested case for break `Give me a quick high-level summary of what this codebase does — like a paragraph or two, nothing formal.`:

```python
new_case("probe-ambiguous")
    .given(router_agent(AGENTS_DIR, llm=router_llm))
    .when(task('Give me a quick high-level summary of what this codebase does — like a paragraph or two, nothing formal.'))
    .then(llm_judge(judge, criteria="Genuinely unclear. It's codebase analysis (project-analyzer's domain) but the deliverable is an informal paragraph, not the 4 structured documents, and spinning up the full 3-phase workflow is overkill. Main agent should either answer inline or ask the user whether they want the full document set. If it delegates, it should confirm scope first."))
```

## Improvement Suggestions

1. Consolidate the repeated no-Bash rule into a single clearly labeled section in the prompt to eliminate duplication.
2. Merge the multiple instances of deep-analysis enrichment guidance into one dedicated block in the prompt.
3. Trim redundant restatements of the same constraints across the prompt to reduce overall length while preserving clarity.
4. Add a brief cross-reference (e.g., 'see Rules section') instead of re-explaining the no-Bash rule wherever it recurs.
5. Review the prompt for any other duplicated instructions beyond the no-Bash and deep-analysis guidance and deduplicate them.
6. Keep the consolidated rules near the top of the prompt so readers encounter each constraint once, early.
7. After edits, re-verify that all four passing behavior tests still pass to confirm no constraint was lost in tightening.
8. Consider a short checklist format for the consolidated rules to make each constraint scannable in one pass.

