# Agent Evaluation Report: project-analyzer

- Path: `/Users/yuhaochen/Documents/codebase/projanvil/SCV/agents/zh-cn/project-analyzer.md`
- Overall: **82.2 / 100**  Grade: **B**
- Parts: behavior 75.0, writing 83.8, lint 97.8

## Behavior Tests

### ✅ agent-standard-four-docs (100 pts)
- ✔ `tool_call(Glob)` 1.00 (threshold 1.00) — Glob called 5 time(s)
- ✔ `write_contains('README.md', 'mini-orders')` 1.00 (threshold 1.00) — content fragment found
- ✔ `write_contains('SUMMARY.md', 'mini-orders')` 1.00 (threshold 1.00) — content fragment found
- ✔ `write_contains('ARCHITECTURE.md', 'mini-orders')` 1.00 (threshold 1.00) — content fragment found
- ✔ `write_contains('FILE_INDEX.md', 'mini-orders')` 1.00 (threshold 1.00) — content fragment found

### ✅ agent-structure-data-rendered (100 pts)
- ✔ `write_contains('ARCHITECTURE.md', '组件依赖')` 1.00 (threshold 1.00) — content fragment found
- ✔ `write_contains('ARCHITECTURE.md', '启动级')` 1.00 (threshold 1.00) — content fragment found
- ✔ `write_contains('ARCHITECTURE.md', 'orderService')` 1.00 (threshold 1.00) — content fragment found

### ❌ agent-deep-off-no-shell (0 pts)
- ✘ `no_tool_call(Bash)` 0.00 (threshold 1.00) — Bash was called 1 time(s)

### ✅ agent-deep-on-uses-codebones (100 pts)
- ✔ `tool_call(Bash)` 1.00 (threshold 1.00) — Bash called 1 time(s)
- ✔ `write_contains('SUMMARY.md', 'mini-orders')` 1.00 (threshold 1.00) — content fragment found

## Static Lint

Total 97.8, grade A

| Check | Score | Result |
|---|---|---|
| Agent frontmatter (Claude Code load contract) | 100.0 | ✅ |
| Metadata (frontmatter / name / description) | 100.0 | ✅ |
| Delegation (description routes correctly) | 100.0 | ✅ |
| Document structure (sections / substance) | 100.0 | ✅ |
| Clarity (placeholders / length) | 85.0 | ✅ |

## Writing Review (LLM)

| Dimension | Score | Comment |
|---|---|---|
| Delegation fit | 0.90 | Description clearly states the four deliverables and the scv run delegation trigger, so a main agent can route confidently. |
| Prompt quality | 0.90 | Role, phased workflow, template contract, input schema, and output checklist are all explicit and actionable. |
| Boundaries | 0.95 | Strong constraints: read-only on source, no Bash unless deep analysis, no metadata writing, stop-and-report on missing inputs, hand back after checklist. |
| Conciseness | 0.60 | The prompt is long with some redundancy (deep-analysis rules restated in Phase 2.5, execution steps, and checklist), which inflates token cost. |

> A well-structured, boundary-clear analyzer agent whose main weakness is verbosity from repeated deep-analysis guidance.

## Challenge (adversarial probes)

| Type | Trigger | Verdict | Note |
|---|---|---|---|
| route | 我刚接手一个没人维护的 Go 老项目，代码能跑但完全看不懂，帮我把它彻底摸清楚，出一套能让我快速上手的说明材料放到 docs/analysis 下 | ✔ survived |  |
| route | scv run 流程里那个文档生成步骤，项目路径 /repo/backend，输出到 /repo/docs，项目名 Backend，深度分析开一下 | ✔ survived |  |
| route | 这个仓库的 ARCHITECTURE.md 里组件依赖章节写得不对，structure.json 已经重新生成了，按新的 JSON 重渲染一下那份文档 | ✔ survived |  |
| no-route | 帮我给 src/utils/date.ts 里的 formatRange 函数补几个单元测试，顺便把边界情况覆盖一下 | ✘ break | delegated to ['project-analyzer'] |
| no-route | 把 README.md 里那段安装说明改一下，我们的包名从 foo 改成 @scope/foo 了，另外加个 badge | ✔ survived |  |
| no-route | 帮我重构一下 services/order 目录，把 OrderService 拆成三个类，拆完跑一下测试确认没坏 | ✘ break | delegated to ['project-analyzer'] |
| ambiguous | 帮我看看这个项目整体是干什么的，简单说两句就行，不用写文件 | ✘ break | The answer fails to provide any project overview, instead reporting a subagent failure and asking the user for files or permission to retry, whereas the expected behavior was for the main agent to quickly scan the project itself or delegate with explicit no-file instructions. |
| ambiguous | 这个仓库的依赖关系有点乱，帮我理一下哪些模块互相耦合、有没有循环依赖，给我个结论 | ✘ break | The answer provides only a generic analysis framework with no actual module names, dependency links, or cycle findings, and it fails to clarify the required deliverable form (e.g., conclusion only vs. ARCHITECTURE.md section) before or after delegating, so it does not satisfy the routing-unclear expectation. |

Suggested case for break `帮我给 src/utils/date.ts 里的 formatRange 函数补几个单元测试，顺便把边界情况覆盖一下`:

```python
new_case("probe-no-route")
    .given(router_agent(AGENTS_DIR, llm=router_llm))
    .when(task('帮我给 src/utils/date.ts 里的 formatRange 函数补几个单元测试，顺便把边界情况覆盖一下'))
    .then(no_delegation())
```

Suggested case for break `帮我重构一下 services/order 目录，把 OrderService 拆成三个类，拆完跑一下测试确认没坏`:

```python
new_case("probe-no-route")
    .given(router_agent(AGENTS_DIR, llm=router_llm))
    .when(task('帮我重构一下 services/order 目录，把 OrderService 拆成三个类，拆完跑一下测试确认没坏'))
    .then(no_delegation())
```

Suggested case for break `帮我看看这个项目整体是干什么的，简单说两句就行，不用写文件`:

```python
new_case("probe-ambiguous")
    .given(router_agent(AGENTS_DIR, llm=router_llm))
    .when(task('帮我看看这个项目整体是干什么的，简单说两句就行，不用写文件'))
    .then(llm_judge(judge, criteria='路由不明确：project-analyzer 的产物是 4 份文档文件，纯口头概览可能更适合主代理自己快速扫一遍；若委派需明确告知不要写文件，或主代理直接回答'))
```

Suggested case for break `这个仓库的依赖关系有点乱，帮我理一下哪些模块互相耦合、有没有循环依赖，给我个结论`:

```python
new_case("probe-ambiguous")
    .given(router_agent(AGENTS_DIR, llm=router_llm))
    .when(task('这个仓库的依赖关系有点乱，帮我理一下哪些模块互相耦合、有没有循环依赖，给我个结论'))
    .then(llm_judge(judge, criteria='路由不明确：若只需结论可主代理自行分析或走轻量路径；若需要产出 ARCHITECTURE.md 组件依赖章节则应委派 project-analyzer 并附 structure.json，主代理应澄清产物形态'))
```

## Improvement Suggestions

1. [clarity] body is ~3375 words, exceeding 3000; skills should stay concise — move details into references/ or other companion files → move details into references/ and keep SKILL.md an overview
2. Fix the deep-off path so it never invokes Bash, since the agent-deep-off-no-shell case failed with 'Bash was called 1 time(s)'.
3. Add an explicit guard or conditional in the deep-off branch that blocks shell/tool execution before any Bash call can occur.
4. Deduplicate the deep-analysis rules by stating them once and referencing them in Phase 2.5, execution steps, and the checklist instead of restating them.
5. Trim redundant phrasing across Phase 2.5, the execution steps, and the checklist to reduce token cost and improve Conciseness (currently 0.6).
6. Consolidate the checklist into a short pointer to the canonical rules rather than a full restatement.
7. Verify the deep-off behavior with a targeted test after the fix to confirm no_tool_call(Bash) passes.
8. Review the deep-on path to ensure the codebones usage remains intact after any refactor of the shared rules.

