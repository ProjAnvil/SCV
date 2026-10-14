# Agent Evaluation Report: project-analyzer

- Path: `/Users/yuhaochen/Documents/codebase/projanvil/SCV/agents/zh-cn/project-analyzer.md`
- Overall: **94.7 / 100**  Grade: **A**
- Parts: behavior 100.0, writing 83.8, lint 97.8

## Behavior Tests

### ✅ agent-standard-four-docs (100 pts)
- ✔ `tool_call(Glob)` 1.00 (threshold 1.00) — Glob called 2 time(s)
- ✔ `write_contains('README.md', 'mini-orders')` 1.00 (threshold 1.00) — content fragment found
- ✔ `write_contains('SUMMARY.md', 'mini-orders')` 1.00 (threshold 1.00) — content fragment found
- ✔ `write_contains('ARCHITECTURE.md', 'mini-orders')` 1.00 (threshold 1.00) — content fragment found
- ✔ `write_contains('FILE_INDEX.md', 'mini-orders')` 1.00 (threshold 1.00) — content fragment found

### ✅ agent-structure-data-rendered (100 pts)
- ✔ `write_contains('ARCHITECTURE.md', '组件依赖')` 1.00 (threshold 1.00) — content fragment found
- ✔ `write_contains('ARCHITECTURE.md', '启动级')` 1.00 (threshold 1.00) — content fragment found
- ✔ `write_contains('ARCHITECTURE.md', 'orderService')` 1.00 (threshold 1.00) — content fragment found

### ✅ agent-deep-off-no-shell (100 pts)
- ✔ `no_tool_call(Bash)` 1.00 (threshold 1.00)

### ✅ agent-deep-on-uses-codebones (100 pts)
- ✔ `tool_call(Bash)` 1.00 (threshold 1.00) — Bash called 2 time(s)
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
| Delegation fit | 0.90 | Description clearly states the deliverable (README/SUMMARY/ARCHITECTURE/FILE_INDEX) and the trigger context (scv run flow), making routing decisions straightforward. |
| Prompt quality | 0.90 | Role, phased workflow, tool guidance, template syntax, input contract, and output checklist are all explicit and actionable. |
| Boundaries | 0.95 | Strong constraints: read-only analysis, no Bash when deep analysis is off, no metadata writing, stop-and-report on missing inputs, and explicit hand-back after completion. |
| Conciseness | 0.60 | The prompt is quite long with some redundancy (e.g., Bash prohibition repeated in three places, deep-analysis rules restated), which inflates the token budget. |

> A well-structured, boundary-conscious analyzer agent whose main weakness is verbosity and repeated constraints that could be tightened.

## Challenge (adversarial probes)

| Type | Trigger | Verdict | Note |
|---|---|---|---|
| route | 我们刚接手一个 2019 年的老 Java 单体仓库，没人写过文档，下周要交接给新团队。你帮我把它整个摸一遍，产出 README、SUMMARY、ARCHITECTURE、FILE_INDEX 四份中文文档放到 docs/analysis/ 下，模板在 templates/ 里。 | ✔ survived |  |
| route | scv run 流程里那个分析阶段该跑了，项目路径 /repo/backend，输出到 /repo/docs，项目名 Backend，深度分析开 true，骨架文件已经生成在 /repo/docs/.codebones_skeleton.md。 | ✔ survived |  |
| route | 别改任何代码啊，我就是想让人读一遍这个 Go 项目，把每个目录干嘛的、入口在哪、服务之间怎么调用的整理成一份架构说明和文件索引，中文的。 | ✔ survived |  |
| no-route | 帮我给这个仓库写一份 README.md，就放在项目根目录，介绍下怎么安装、怎么跑起来、常用命令，别动其他文件。 | ✘ break | delegated to ['project-analyzer'] |
| no-route | 这个 Spring Boot 项目启动报 BeanCreationException，你帮我定位一下是哪个 @Service 的循环依赖导致的，然后改掉它。 | ✘ break | delegated to ['project-analyzer'] |
| no-route | 把 docs/analysis/ 下已经生成好的 SUMMARY.md 和 ARCHITECTURE.md 翻译成英文，再顺手把里面的 Mermaid 图重画得好看点。 | ✘ break | delegated to ['project-analyzer', 'project-analyzer'] |
| ambiguous | 帮我看看这个仓库的代码质量怎么样，有没有明显的坏味道或者架构问题，给我一份评估报告。 | ✘ break | The answer correctly identifies that no repository was specified and that the report wasn't surfaced, but it fails to recognize the routing ambiguity between project-analyzer's fixed four-document output versus a code review/quality assessment subagent, instead promising to re-delegate to the same analyzer. |
| ambiguous | 给这个项目生成一份 API 文档，把每个 REST 端点、请求参数、响应结构都列出来。 | ✘ break | The agent failed to produce any API documentation and never clarified whether the user wanted the endpoint section of the analysis document or a standalone API reference, instead only reporting subagent failure and asking to re-run. |

Suggested case for break `帮我给这个仓库写一份 README.md，就放在项目根目录，介绍下怎么安装、怎么跑起来、常用命令，别动其他文件。`:

```python
new_case("probe-no-route")
    .given(router_agent(AGENTS_DIR, llm=router_llm))
    .when(task('帮我给这个仓库写一份 README.md，就放在项目根目录，介绍下怎么安装、怎么跑起来、常用命令，别动其他文件。'))
    .then(no_delegation())
```

Suggested case for break `这个 Spring Boot 项目启动报 BeanCreationException，你帮我定位一下是哪个 @Service 的循环依赖导致的，然后改掉它。`:

```python
new_case("probe-no-route")
    .given(router_agent(AGENTS_DIR, llm=router_llm))
    .when(task('这个 Spring Boot 项目启动报 BeanCreationException，你帮我定位一下是哪个 @Service 的循环依赖导致的，然后改掉它。'))
    .then(no_delegation())
```

Suggested case for break `把 docs/analysis/ 下已经生成好的 SUMMARY.md 和 ARCHITECTURE.md 翻译成英文，再顺手把里面的 Mermaid 图重画得好看点。`:

```python
new_case("probe-no-route")
    .given(router_agent(AGENTS_DIR, llm=router_llm))
    .when(task('把 docs/analysis/ 下已经生成好的 SUMMARY.md 和 ARCHITECTURE.md 翻译成英文，再顺手把里面的 Mermaid 图重画得好看点。'))
    .then(no_delegation())
```

Suggested case for break `帮我看看这个仓库的代码质量怎么样，有没有明显的坏味道或者架构问题，给我一份评估报告。`:

```python
new_case("probe-ambiguous")
    .given(router_agent(AGENTS_DIR, llm=router_llm))
    .when(task('帮我看看这个仓库的代码质量怎么样，有没有明显的坏味道或者架构问题，给我一份评估报告。'))
    .then(llm_judge(judge, criteria='路由不明确：project-analyzer 会做架构与依赖分析，可能顺带发现架构问题，但其产物是四份固定文档而非“质量评估报告”，且不做代码评审结论。主 agent 应询问用户是要生成标准分析文档，还是走代码评审/质量评估子代理。'))
```

Suggested case for break `给这个项目生成一份 API 文档，把每个 REST 端点、请求参数、响应结构都列出来。`:

```python
new_case("probe-ambiguous")
    .given(router_agent(AGENTS_DIR, llm=router_llm))
    .when(task('给这个项目生成一份 API 文档，把每个 REST 端点、请求参数、响应结构都列出来。'))
    .then(llm_judge(judge, criteria='路由不明确：project-analyzer 的 SUMMARY.md 会包含核心端点表，但那是概览级而非完整 API 参考文档。主 agent 应确认用户要的是分析文档中的端点章节，还是独立的 API 参考文档（后者更适合专门的 API 文档子代理）。'))
```

## Improvement Suggestions

1. [clarity] body is ~3333 words, exceeding 3000; skills should stay concise — move details into references/ or other companion files → move details into references/ and keep SKILL.md an overview
2. Consolidate the Bash prohibition into a single statement in the prompt instead of repeating it in three places to reduce redundancy.
3. Merge the restated deep-analysis rules into one section of the prompt to eliminate duplicated instructions.
4. Trim the overall prompt length to lower token usage while preserving all required behavioral constraints.
5. Review the prompt for any other repeated guidance beyond the Bash and deep-analysis examples and deduplicate it.
6. After edits, re-run the four behavior tests to confirm the consolidated prompt still passes all cases.
7. Add a brief conciseness check to the prompt-authoring process so future revisions avoid restating the same rule in multiple sections.

