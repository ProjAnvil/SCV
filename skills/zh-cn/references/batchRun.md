# scv batchRun - 批量并行分析

使用并行 `project-analyzer` subagent 批量分析多个仓库。

---

## ⛔ 核心约束（必须遵守）

| 约束 | 值 | 说明 |
|------|-----|------|
| **最大并发数** | **config 中的 `batch_size`** | 每批次最多启动的 subagent 数（默认：5），硬性限制，不可覆盖 |
| **批次处理** | **必须** | 所有仓库必须按批次处理，每批次完成后才能启动下一批 |
| **Python 驱动状态** | **必须** | 所有计划制定、git 操作和批次划分均由 `batch_manager.py plan` 完成，LLM 不得自行计算批次 |

**违反约束的后果：**
- 超过 `batch_size` 个并发 subagent 会导致系统资源耗尽
- 不按批次处理会导致 subagent 失控、无法追踪进度

---

## 用法

```
/scv batchRun [--analyze-only]
```

从 `~/.scv/config.json` 读取配置，将所有计划制定委托给 `batch_manager.py plan`，然后按批次执行分析。

**选项：**
- `--analyze-only`: 跳过 git clone/pull，仅验证本地路径是否存在

## 核心优势

| 特性 | 说明 |
|------|------|
| **Python 控制批次** | `batch_manager.py` 负责所有状态、git 操作和批次划分 — LLM 不会计错数 |
| **配置驱动并发** | config.json 中的 `batch_size` 设置硬性限制，不可通过 prompt 覆盖 |
| **Context 隔离** | 每个分析在独立 subagent context 中完成 |
| **容错处理** | 单个失败不影响其他分析 |
| **崩溃恢复** | 幂等的 `next` 命令在恢复时返回进行中的批次 |

---

## 执行步骤

### Step 1: 检查配置

验证 `~/.scv/config.json` 是否存在。如果缺失：

```
❌ 配置文件未找到: ~/.scv/config.json

请创建配置文件:

{
  "output_dir": "~/.scv/analysis",
  "batch_size": 5,
  "repos": [
    {
      "type": "remote",
      "url": "https://github.com/user/repo1.git",
      "project_name": "Remote Project 1",
      "branch": "main"
    },
    {
      "type": "local",
      "path": "~/local/path/to/repo2",
      "project_name": "Local Project 2"
    }
  ],
  "parallel": true,
  "fail_fast": false
}
```

### Step 2: 运行 `batch_manager.py plan`（Python 处理一切）

> ⛔ **强制要求：必须先调用 `plan`。不得自行读取 config，不得自行运行 git，不得自行计算批次。**
>
> `plan` 原子性地完成以下所有操作：
> 1. 读取 `~/.scv/config.json`（包括 `batch_size`）
> 2. 对每个远程仓库执行 `git clone` 或 `git pull`
> 3. 验证本地仓库路径是否存在
> 4. 将仓库划分为 `batch_size` 大小的批次
> 5. 将完整执行计划持久化到 `~/.scv/sessions/{session_id}.json`

**获取当前 session ID**：从 OpenCode 会话上下文（`session_id` 字段）中获取。
如果无法获取，使用时间戳生成回退值：`scv_batch_{timestamp}`。

```bash
python3 ~/.claude/skills/scv/scripts/batch_manager.py plan \
  --session {YOUR_SESSION_ID} \
  --config ~/.scv/config.json
```

`--analyze-only` 模式（跳过 git 操作）：

```bash
python3 ~/.claude/skills/scv/scripts/batch_manager.py plan \
  --session {YOUR_SESSION_ID} \
  --config ~/.scv/config.json \
  --analyze-only
```

预期输出：

```json
{
  "status": "planned",
  "session_id": "ses_abc123",
  "total_repos": 10,
  "ready_repos": 8,
  "failed_repos": 1,
  "skipped_repos": 1,
  "total_batches": 2,
  "batch_size": 5,
  "git_errors": [
    { "repo": "some_repo", "error": "git clone failed: ..." }
  ],
  "skipped": [
    { "repo": "unchanged_repo", "reason": "Commit unchanged since 2026-03-17T06:00:00+00:00" }
  ],
  "state_file": "~/.scv/sessions/ses_abc123.json"
}
```

如果 `failed_repos > 0`，这些仓库已在计划中标记为 `failed` — 批次执行时会自动跳过。向用户报告 git 错误后继续执行。

如果 `skipped_repos > 0`，这些仓库的 commit hash 与上次分析相同，无需重新分析。向用户报告为"文档已是最新"。

**创建 TodoWrite 以显示 UI 进度：**

```
TodoWrite([
  { content: "批次 1/{total_batches}: 仓库 1-{batch_size}", status: "pending" },
  { content: "批次 2/{total_batches}: 仓库 ...", status: "pending" },
  ...
  { content: "生成最终汇总", status: "pending" }
])
```

### Step 3: 批次并行执行（由 `next` 驱动）

> ⛔ **批次循环必须由 `batch_manager.py next` 驱动，绝不能从 in-context 记忆中继续。**
>
> `next` 精确返回下一批的仓库列表 — LLM 为每个仓库 fork 一个 subagent，收集所有结果，上报后再次调用 `next`。

#### 3.1 批次循环

```
┌─────────────────────────────────────────────────────────────────────┐
│  循环：重复执行，直到 `next` 退出码为 2                              │
│                                                                     │
│  ① batch_manager.py next --session {id}                             │
│     → 返回 JSON: { batch_num, total_batches, batch_size, repos }    │
│     → 退出码 2 = 无更多批次 → 退出循环                              │
│                                                                     │
│  ② 将本批次所有仓库同时 fork 为后台 subagent                        │
│     （使用计划中已解析好的 repo.local_path）                        │
│                                                                     │
│  ③ 阻塞等待本批次所有 subagent 完成                                  │
│     （对每个 task_id 调用 background_output）                       │
│                                                                     │
│  ④ 对每个结果：                                                      │
│     成功 → batch_manager.py complete --session {id} --repo {name}  │
│     失败 → batch_manager.py fail --session {id} --repo {name}      │
│                                --error "msg"                        │
│                                                                     │
│  ⑤ 标记当前批次 todo 为已完成                                        │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

#### 3.2 执行伪代码

```
SESSION_ID = {YOUR_SESSION_ID}
BM = "python3 ~/.claude/skills/scv/scripts/batch_manager.py"

while True:
    result = Bash(f"{BM} next --session {SESSION_ID}")
    if result.exit_code == 2:
        break

    batch = parse_json(result.stdout)
    batch_num = batch["batch_num"]
    total_batches = batch["total_batches"]
    repos = batch["repos"]          # local_path 已由 plan 解析完毕
    output_dir = batch["output_dir"]

    print(f"🔄 批次 {batch_num}/{total_batches}（{len(repos)} 个仓库）")
    TodoWrite - 标记 "批次 {batch_num}/{total_batches}" 为进行中

    # 同时 fork 所有 subagent
    task_ids = {}
    for repo in repos:
        task_id = Agent(
            subagent_type="project-analyzer",
            description=f"分析 {repo['project_name']}",
            prompt=build_prompt(repo, output_dir),
            run_in_background=True
        )
        task_ids[repo["project_name"]] = task_id

    # 收集所有结果后再继续
    for repo_name, task_id in task_ids.items():
        output = background_output(task_id=task_id, block=True, timeout=600000)
        if output.success:
            Bash(f"{BM} complete --session {SESSION_ID} --repo '{repo_name}'")
        else:
            Bash(f"{BM} fail --session {SESSION_ID} --repo '{repo_name}' --error '{output.error}'")

    TodoWrite - 标记 "批次 {batch_num}/{total_batches}" 为已完成

print("✅ 所有批次完成")
```

#### 3.3 Subagent 调用模板

```
Agent(
  subagent_type="project-analyzer",
  description="分析 {repo['project_name']}",
  prompt="""
  分析代码仓库并生成文档。

  输入参数：
  - 项目路径: {repo['local_path']}
  - 输出目录: {output_dir}/{repo['repo_name']}/
  - 项目名称: {repo['project_name']}
  - 当前提交: {repo.get('current_commit', 'N/A')}
  - 模板目录: {skill_path}/references/templates/

  执行 3 阶段分析工作流：
  1. Phase 1: 全局扫描 - 识别技术栈和结构
  2. Phase 2: 深度文件分析 - 分析优先级文件
  3. Phase 3: 文档生成 - 创建 4 个文档

  在输出目录生成以下文档：
  - README.md - 项目概览
  - SUMMARY.md - 5 分钟摘要
  - ARCHITECTURE.md - 架构设计
  - FILE_INDEX.md - 文件索引

  严格遵循模板格式，不确定项标记 [待确认]。
  """,
  run_in_background=True
)
```

> **说明：** `repo['current_commit']` 由 `batch_manager.py plan` 填充并存储在 session 状态中。非 Git 本地目录时值为 `None`。

#### 3.4 顺序执行模式（`parallel: false`）

- 仍调用 `next` 获取每批
- 每次只 fork 一个 subagent，阻塞等待完成后再处理下一个
- 适用于调试或资源受限场景

#### 3.5 崩溃恢复

```bash
# 查看仍在进行中或待处理的任务
python3 ~/.claude/skills/scv/scripts/batch_manager.py resume --session {SESSION_ID}

# 再次调用 next — 返回被中断的批次（幂等操作）
python3 ~/.claude/skills/scv/scripts/batch_manager.py next --session {SESSION_ID}
```

`next` 是**幂等的**：如果某批次已处于 `in_progress` 状态，它会返回同一批次而不是向前跳。崩溃后可安全重新运行，不会重复或遗漏任何仓库。

### Step 4: 追踪进度

```
✅ [1/N] {project_name} 完成
   📁 路径: {local_path}
   📂 输出: ~/.scv/analysis/{repo_name}/
   📦 提交: {short_commit_hash} - {commit_message}
   📄 已生成 4 个文档

⏭️ [2/N] {project_name} 已跳过（提交未变更）
   📦 提交: {short_commit_hash}（与上次分析相同）
   💡 文档已是最新，无需重新分析

❌ [3/N] {project_name} 失败
   🚫 错误: {error_message}
```

### Step 5: 生成批量摘要

```
╔════════════════════════════════════════════════════════════╗
║         批量分析完成                                        ║
╚════════════════════════════════════════════════════════════╝

配置文件: ~/.scv/config.json
输出目录: ~/.scv/analysis/
批次大小: {batch_size}
执行方式: {sequential/parallel}

结果:
  ✅ 成功: {N}/{total}
  ❌ 失败: {N}/{total}
  ⏭️ 跳过（提交未变更）: {N}/{total}

成功仓库:
  1. [remote] {project_name} → ~/.scv/analysis/{repo_name}/
  2. [local]  {project_name} → ~/.scv/analysis/{repo_name}/

跳过仓库（提交未变更）:
  1. {project_name} → 上次分析: {last_analyzed_at}

失败仓库:
  1. {project_name} → {error}
```

### Step 6: 建议下一步

```
📖 浏览文档:    open ~/.scv/analysis/
🔍 分析单仓库:  /scv run <repo_path>
🔄 重新运行:    /scv batchRun
📋 列出仓库:    /scv gather --list
📇 查看索引:    cat ~/.scv/index.md
```

### Step 6.5: 更新分析索引

所有批次完成后，重建全局索引文件。按照 `references/index.md` 中的完整步骤执行：
列出所有项目目录 → 逐个读取 README 提取摘要和功能职责关键词 → 组装 entries JSON → 调用脚本写入 `~/.scv/index.md`。

详见：`references/index.md`

---

## 配置文件参考

### ~/.scv/config.json

```json
{
  "output_dir": "~/.scv/analysis",
  "batch_size": 5,
  "parallel": true,
  "fail_fast": false,
  "repos": [
    {
      "type": "remote",
      "url": "https://github.com/user/repo.git",
      "project_name": "自定义名称",
      "branch": "main"
    },
    {
      "type": "local",
      "path": "~/local/path/to/repo",
      "project_name": "本地项目"
    }
  ]
}
```

| 字段 | 必填 | 说明 | 默认值 |
|------|------|------|--------|
| `output_dir` | 否 | 所有分析的输出目录 | `~/.scv/analysis` |
| `batch_size` | 否 | 每批次最大并发 subagent 数 | `5` |
| `parallel` | 否 | 批次内并行（false = 顺序） | `true` |
| `fail_fast` | 否 | 第一个失败时停止全部 | `false` |

---

## 执行要求汇总

| 要求 | 规则 | 违反后果 |
|------|------|----------|
| 必须先调用 `plan` | 始终在 `next` 之前执行 | 状态过时或缺失 |
| 不得自行解析 config | `plan` 负责 config 读取 | 批次划分错误 |
| 不得自行运行 git | `plan` 负责 git 操作 | 本地状态分叉 |
| 必须调用 `next` 获取批次 | 不得从记忆中计算 | 仓库错误，静默跳过 |
| 收集所有结果后再进入下一批 | 批次间串行 | 状态乱序 |
| 每个仓库必须调用 `complete` 或 `fail` | 不得例外 | 状态与实际不符 |
