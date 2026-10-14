# Agent Evaluation Report: project-analyzer

- Path: `/Users/yuhaochen/Documents/codebase/projanvil/SCV/agents/zh-cn/project-analyzer.md`
- Overall: **73.7 / 100**  Grade: **C**
- Parts: behavior 70.8, writing 75.0, lint 78.7

## Behavior Tests

### ✅ agent-standard-four-docs (100 pts)
- ✔ `tool_call(Glob)` 1.00 (threshold 1.00) — Glob called 4 time(s)
- ✔ `write_contains('README.md', 'mini-orders')` 1.00 (threshold 1.00) — content fragment found
- ✔ `write_contains('SUMMARY.md', 'mini-orders')` 1.00 (threshold 1.00) — content fragment found
- ✔ `write_contains('ARCHITECTURE.md', 'mini-orders')` 1.00 (threshold 1.00) — content fragment found
- ✔ `write_contains('FILE_INDEX.md', 'mini-orders')` 1.00 (threshold 1.00) — content fragment found

### ❌ agent-structure-data-rendered (83 pts)
- ✔ `write_contains('ARCHITECTURE.md', '组件依赖')` 1.00 (threshold 1.00) — content fragment found
- ✘ `write_contains('ARCHITECTURE.md', '启动级')` 0.50 (threshold 1.00) — *ARCHITECTURE.md* written but content lacks '启动级'
- ✔ `write_contains('ARCHITECTURE.md', 'orderService')` 1.00 (threshold 1.00) — content fragment found

### ❌ agent-deep-off-no-shell (0 pts)
- ✘ `no_tool_call(Bash)` 0.00 (threshold 1.00) — Bash was called 1 time(s)

### ✅ agent-deep-on-uses-codebones (100 pts)
- ✔ `tool_call(Bash)` 1.00 (threshold 1.00) — Bash called 3 time(s)
- ✔ `write_contains('SUMMARY.md', 'mini-orders')` 1.00 (threshold 1.00) — content fragment found

## Static Lint

Total 78.7, grade C

| Check | Score | Result |
|---|---|---|
| Agent frontmatter (Claude Code load contract) | 75.0 | ✅ |
| Metadata (frontmatter / name / description) | 60.0 | ✅ |
| Delegation (description routes correctly) | 80.0 | ✅ |
| Document structure (sections / substance) | 100.0 | ✅ |
| Clarity (placeholders / length) | 85.0 | ✅ |

## Writing Review (LLM)

| Dimension | Score | Comment |
|---|---|---|
| Delegation fit | 0.85 | The description clearly states the trigger (deep code repo analysis producing README/SUMMARY/ARCHITECTURE/FILE_INDEX), so a main agent can route confidently, though it doesn't mention the optional deep-analysis flag that changes behavior. |
| Prompt quality | 0.90 | Role, phased workflow, tool table, template contract, edge cases, and a verification checklist are all explicit and well-structured. |
| Boundaries | 0.70 | It notes not to write metadata and to mark unknowns as [待确认], but lacks explicit hand-back conditions or forbidden actions like modifying source code. |
| Conciseness | 0.55 | The prompt is quite long with repetitive Phase 2.5 subsections and duplicated examples that could be trimmed without losing clarity. |

> A thorough, well-contracted analyzer prompt that is slightly verbose and could tighten its boundary and conciseness sections.

## Challenge (adversarial probes)

| Type | Trigger | Verdict | Note |
|---|---|---|---|
| route | 我刚接手这个仓库，完全不知道从哪看起。能不能帮我把它整个捋一遍，最后给我几份能快速上手的说明文档？ | ⚠ error | ValueError: no agent files found for the router roster: /Users/yuhaochen/Documents/codebase/projanvil/SCV/agents/zh-cn |
| route | 帮我把 src/ 下面那些 service 之间的调用关系搞清楚，然后写进架构文档里，最好带上依赖图。 | ⚠ error | ValueError: no agent files found for the router roster: /Users/yuhaochen/Documents/codebase/projanvil/SCV/agents/zh-cn |
| route | 这个项目没有 README，新人 onboarding 很痛苦。你能扫一遍代码库，产出一套中文的项目说明吗？ | ⚠ error | ValueError: no agent files found for the router roster: /Users/yuhaochen/Documents/codebase/projanvil/SCV/agents/zh-cn |
| no-route | 帮我修复 src/auth/login.py 里那个 token 过期判断的 bug，顺便补个单元测试。 | ⚠ error | ValueError: no agent files found for the router roster: /Users/yuhaochen/Documents/codebase/projanvil/SCV/agents/zh-cn |
| no-route | 把 README.md 里那段英文介绍翻译成中文，再调整一下排版。 | ⚠ error | ValueError: no agent files found for the router roster: /Users/yuhaochen/Documents/codebase/projanvil/SCV/agents/zh-cn |
| no-route | 帮我 review 一下这个 PR 的改动，看看有没有安全漏洞和性能问题。 | ⚠ error | ValueError: no agent files found for the router roster: /Users/yuhaochen/Documents/codebase/projanvil/SCV/agents/zh-cn |
| ambiguous | 帮我看看这个仓库整体是干嘛的，简单说两句就行，不用写文档。 | ⚠ error | ValueError: no agent files found for the router roster: /Users/yuhaochen/Documents/codebase/projanvil/SCV/agents/zh-cn |
| ambiguous | 给这个项目生成一份 API 接口文档，把每个端点的请求参数和返回值都列出来。 | ⚠ error | ValueError: no agent files found for the router roster: /Users/yuhaochen/Documents/codebase/projanvil/SCV/agents/zh-cn |

## Improvement Suggestions

1. [agent-frontmatter] frontmatter is missing the name field → add 'name: <kebab-case-id>' to the frontmatter
2. [metadata] frontmatter is missing the name field → add 'name: <skill-name>' to the frontmatter
3. [delegation] description never says when to delegate to this subagent → add a 'Use when ...' sentence naming the triggering situations
4. [clarity] body is ~3264 words, exceeding 3000; skills should stay concise — move details into references/ or other companion files → move details into references/ and keep SKILL.md an overview
5. Add explicit hand-back conditions and forbidden actions (e.g., do not modify source code) to the Boundaries section of the prompt.
6. Trim repetitive Phase 2.5 subsections and duplicated examples in the prompt to improve conciseness.
7. Ensure ARCHITECTURE.md generation includes the required '启动级' content, as expected by the agent-structure-data-rendered test.
8. Investigate and prevent the Bash tool call in the agent-deep-off-no-shell scenario, since no shell usage is expected there.

