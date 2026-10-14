# Agent Evaluation Report: project-analyzer

- Path: `/Users/yuhaochen/Documents/codebase/projanvil/SCV/agents/en/project-analyzer.md`
- Overall: **77.1 / 100**  Grade: **C**
- Parts: behavior 75.0, writing 75.0, lint 85.4

## Behavior Tests

### ✅ agent-standard-four-docs (100 pts)
- ✔ `tool_call(Glob)` 1.00 (threshold 1.00) — Glob called 7 time(s)
- ✔ `write_contains('README.md', 'mini-orders')` 1.00 (threshold 1.00) — content fragment found
- ✔ `write_contains('SUMMARY.md', 'mini-orders')` 1.00 (threshold 1.00) — content fragment found
- ✔ `write_contains('ARCHITECTURE.md', 'mini-orders')` 1.00 (threshold 1.00) — content fragment found
- ✔ `write_contains('FILE_INDEX.md', 'mini-orders')` 1.00 (threshold 1.00) — content fragment found

### ✅ agent-structure-data-rendered (100 pts)
- ✔ `write_contains('ARCHITECTURE.md', 'Component Dependencies')` 1.00 (threshold 1.00) — content fragment found
- ✔ `write_contains('ARCHITECTURE.md', 'startup-fatal')` 1.00 (threshold 1.00) — content fragment found
- ✔ `write_contains('ARCHITECTURE.md', 'orderService')` 1.00 (threshold 1.00) — content fragment found

### ❌ agent-deep-off-no-shell (0 pts)
- ✘ `no_tool_call(Bash)` 0.00 (threshold 1.00) — Bash was called 1 time(s)

### ✅ agent-deep-on-uses-codebones (100 pts)
- ✔ `tool_call(Bash)` 1.00 (threshold 1.00) — Bash called 3 time(s)
- ✔ `write_contains('SUMMARY.md', 'mini-orders')` 1.00 (threshold 1.00) — content fragment found

## Static Lint

Total 85.4, grade B

| Check | Score | Result |
|---|---|---|
| Agent frontmatter (Claude Code load contract) | 75.0 | ✅ |
| Metadata (frontmatter / name / description) | 60.0 | ✅ |
| Delegation (description routes correctly) | 100.0 | ✅ |
| Document structure (sections / substance) | 100.0 | ✅ |
| Clarity (placeholders / length) | 100.0 | ✅ |

## Writing Review (LLM)

| Dimension | Score | Comment |
|---|---|---|
| Delegation fit | 0.90 | The description clearly states the trigger (deep code repository analysis) and the concrete deliverables (README, SUMMARY, ARCHITECTURE, FILE_INDEX), so a main agent can route confidently. |
| Prompt quality | 0.85 | Role, phased workflow, template contract, input schema, and an output checklist are all explicit, though the template files themselves are external and not shown. |
| Boundaries | 0.70 | It states not to write metadata and not to add new sections in deep analysis, but lacks explicit hand-back conditions or forbidden actions like modifying source code. |
| Conciseness | 0.55 | The prompt is long and repetitive, with Phase 2.5 examples and framework-specific patterns that could be trimmed or moved to referenced docs. |

> A thorough, well-structured analyzer definition with a clear output contract, held back mainly by verbosity and thin negative boundaries.

## Challenge (adversarial probes)

| Type | Trigger | Verdict | Note |
|---|---|---|---|
| route | I just inherited this repo from a dev who left. Can you help me get my head around what's actually in here before I start touching anything? | ⚠ error | ValueError: no agent files found for the router roster: /Users/yuhaochen/Documents/codebase/projanvil/SCV/agents/en |
| route | We're onboarding three new engineers next week and I want them to have something to read so they stop pinging me about where things live. | ⚠ error | ValueError: no agent files found for the router roster: /Users/yuhaochen/Documents/codebase/projanvil/SCV/agents/en |
| route | Before I refactor the payment module I want a map of how everything connects — what calls what, which services depend on which. | ⚠ error | ValueError: no agent files found for the router roster: /Users/yuhaochen/Documents/codebase/projanvil/SCV/agents/en |
| no-route | Read ARCHITECTURE.md and tell me which module I should modify to add a new webhook endpoint. | ⚠ error | ValueError: no agent files found for the router roster: /Users/yuhaochen/Documents/codebase/projanvil/SCV/agents/en |
| no-route | The README is out of date — update the install instructions and bump the version badge to 2.4.1. | ⚠ error | ValueError: no agent files found for the router roster: /Users/yuhaochen/Documents/codebase/projanvil/SCV/agents/en |
| no-route | Review this PR diff and tell me if the new OrderService logic has any bugs. | ⚠ error | ValueError: no agent files found for the router roster: /Users/yuhaochen/Documents/codebase/projanvil/SCV/agents/en |
| ambiguous | Can you give me a quick summary of this repo? | ⚠ error | ValueError: no agent files found for the router roster: /Users/yuhaochen/Documents/codebase/projanvil/SCV/agents/en |
| ambiguous | Document the API endpoints in this service. | ⚠ error | ValueError: no agent files found for the router roster: /Users/yuhaochen/Documents/codebase/projanvil/SCV/agents/en |

## Improvement Suggestions

1. [agent-frontmatter] frontmatter is missing the name field → add 'name: <kebab-case-id>' to the frontmatter
2. [metadata] frontmatter is missing the name field → add 'name: <skill-name>' to the frontmatter
3. Add an explicit prohibition against invoking Bash/shell tools in the deep-off (no-shell) mode of the prompt, since the agent-deep-off-no-shell test failed by calling Bash once.
4. Add explicit hand-back conditions to the Boundaries section so the agent knows when to stop and return control to the user.
5. Add a forbidden-actions list to the Boundaries section that includes modifying source code, which is currently unstated.
6. Trim or relocate the Phase 2.5 examples out of the main prompt into a referenced doc to reduce length and repetition.
7. Move framework-specific patterns into referenced documentation rather than inlining them in the prompt to improve conciseness.
8. Deduplicate repetitive phrasing across the prompt to raise the Conciseness score above its current 0.55.

