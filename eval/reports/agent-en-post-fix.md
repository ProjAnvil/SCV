# Agent Evaluation Report: project-analyzer

- Path: `/Users/yuhaochen/Documents/codebase/projanvil/SCV/agents/en/project-analyzer.md`
- Overall: **94.8 / 100**  Grade: **A**
- Parts: behavior 100.0, writing 82.5, lint 100.0

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
- ✔ `tool_call(Bash)` 1.00 (threshold 1.00) — Bash called 1 time(s)
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
| Delegation fit | 0.90 | Description clearly states the deliverable (4 named docs) and the trigger context (scv run flow), so a main agent can route confidently. |
| Prompt quality | 0.85 | Role, phased workflow, template contract, and output checklist are explicit, though the template files themselves are external and unverified. |
| Boundaries | 0.95 | Dedicated Boundaries section plus fail-loudly and hand-back rules make prohibited actions and stop conditions unambiguous. |
| Conciseness | 0.60 | The prompt is long and repeats deep-analysis guidance across sections 2.5.1-2.5.6 and the execution steps, which could be tightened. |

> A well-structured, boundary-clear analyzer agent whose main weakness is verbosity from duplicated deep-analysis instructions.

## Challenge (adversarial probes)

| Type | Trigger | Verdict | Note |
|---|---|---|---|
| route | We just inherited this old monolith from another team and nobody here knows it. Before our onboarding meeting next week I need something I can hand new hires — a proper write-up of what the thing actually does, how it's put together, and where everything lives. Can you take care of that? | ✔ survived |  |
| route | scv run --project ./services/payments --out ./docs/payments | ✔ survived |  |
| route | I've got a repo with zero documentation and I keep getting lost in it. Can you map out the module boundaries and the dependency graph between components so I can see the layering? | ✔ survived |  |
| no-route | Read src/services/OrderService.java and refactor the createOrder method — it's doing validation, stock reservation and payment initiation all in one place. Split it up and add tests. | ✔ survived |  |
| no-route | Write a README.md for this project — just a short one, a paragraph of what it does plus install and run instructions. | ✘ break | delegated to ['project-analyzer'] |
| no-route | Summarize the last 20 commits and tell me who's been touching the auth module most. | ✔ survived |  |
| ambiguous | Can you look at this repo and tell me if the architecture is any good? I want to know whether the layering is sound before we commit to building on it. | ✘ break | The answer is honest about the subagent returning no content and offers sensible recovery options, but it never recognizes the routing ambiguity between a descriptive project-analyzer output and an opinionated review, nor does it ask the user which they want or set expectations about descriptive-only output. |
| ambiguous | Generate the docs for the new microservice — but heads up, the templates directory got deleted in a cleanup last week and I haven't restored it. | ✔ survived |  |

Suggested case for break `Write a README.md for this project — just a short one, a paragraph of what it does plus install and run instructions.`:

```python
new_case("probe-no-route")
    .given(router_agent(AGENTS_DIR, llm=router_llm))
    .when(task('Write a README.md for this project — just a short one, a paragraph of what it does plus install and run instructions.'))
    .then(no_delegation())
```

Suggested case for break `Can you look at this repo and tell me if the architecture is any good? I want to know whether the layering is sound before we commit to building on it.`:

```python
new_case("probe-ambiguous")
    .given(router_agent(AGENTS_DIR, llm=router_llm))
    .when(task('Can you look at this repo and tell me if the architecture is any good? I want to know whether the layering is sound before we commit to building on it.'))
    .then(llm_judge(judge, criteria="Genuinely unclear. project-analyzer will produce ARCHITECTURE.md describing the layering and dependency graph, but it is explicitly descriptive ('Code is Truth', no judgment) and will not render a quality verdict. If the user wants a description, route to project-analyzer; if they want an opinionated review, that's a different subagent. Main agent should ask which they want, or route to project-analyzer and set expectations that the output is descriptive."))
```

## Improvement Suggestions

1. Consolidate the repeated deep-analysis guidance in sections 2.5.1-2.5.6 into a single reference block to reduce redundancy.
2. Remove or shorten the duplicated deep-analysis instructions in the execution steps since they restate the same guidance already given in section 2.5.
3. Trim the overall prompt length by cutting any explanatory sentences that do not change agent behavior, focusing on the longest sections first.
4. Cross-reference the deep-analysis section from the execution steps instead of restating it, keeping the execution steps short and action-oriented.

