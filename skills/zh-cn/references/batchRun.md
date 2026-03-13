# scv batchRun - 批量并行分析

使用并行 `project-analyzer` subagent 批量分析多个仓库。

## 用法

```
/scv batchRun
```

从 `~/.scv/config.json` 读取仓库配置，批量执行分析。

## 核心优势

| 特性 | 说明 |
|------|------|
| **真正并行** | 为每个仓库 fork 独立的 `project-analyzer` subagent |
| **Context 隔离** | 每个分析在独立 context 中完成 |
| **进度追踪** | 实时显示每个仓库的分析进度 |
| **容错处理** | 单个失败不影响其他分析 |

## 执行步骤

### Step 1: 检查并加载配置

1. **验证配置文件存在**: `~/.scv/config.json`
2. 如果缺失，报告错误并提供示例：
   ```
   ❌ 配置文件未找到: ~/.scv/config.json

   请创建以下结构的配置文件:

   {
     "output_dir": "~/.scv/analysis",
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

3. **加载并验证配置**：
   - 解析 JSON
   - 验证 `repos` 数组存在
   - 获取 `output_dir`（默认: `~/.scv/analysis`）
   - 验证每个仓库条目：
     - `type`: 必须是 `"remote"` 或 `"local"`
     - 远程仓库：验证 `url` 有效
     - 本地仓库：验证 `path` 存在

### Step 2: 显示分析计划

```
📋 批量分析计划

配置文件: ~/.scv/config.json
输出目录: ~/.scv/analysis
仓库数量: {N} (远程: {N}, 本地: {N})
并行执行: {true/false}
快速失败: {true/false}

待分析仓库:
  1. [remote] {project_name}
     URL: {url}
     分支: {branch}
     本地路径: ~/.scv/repos/{repo_name}
     输出: ~/.scv/analysis/{repo_name}

  2. [local] {project_name}
     路径: {path}
     输出: ~/.scv/analysis/{repo_name}

  ...

总计: {N} 个仓库
所有输出将保存到: ~/.scv/analysis/
```

### Step 3: 准备仓库

**对于远程仓库**，在分析前执行 git 操作：

1. 提取 `repo_name`: `basename(url)` 去掉 `.git`
2. 本地路径: `~/.scv/repos/{repo_name}`
3. 检查是否存在：
   - 存在：`git pull {branch}`
   - 不存在：`git clone -b {branch} {url} {repo_name}`
4. 显示结果：
   ```
   🔄 远程仓库: {project_name}
   📁 本地: ~/.scv/repos/{repo_name}
   🌿 分支: {branch}

   ✅ Git pull 完成
   最新: {commit_hash} - {commit_message}
   ```

**对于本地仓库**：
1. 提取 `repo_name`: `basename(path)`
2. 验证路径存在且可访问
3. 显示：
   ```
   📂 本地仓库: {project_name}
   📁 路径: {path}
   ```

### Step 4: 创建任务列表进行追踪

**使用 TodoWrite 创建任务列表追踪所有仓库分析：**

```
TodoWrite([
  { content: "分析 {project_name_1}", status: "in_progress", activeForm: "正在分析 {project_name_1}" },
  { content: "分析 {project_name_2}", status: "pending", activeForm: "正在分析 {project_name_2}" },
  ...
  { content: "分析 {project_name_N}", status: "pending", activeForm: "正在分析 {project_name_N}" }
])
```

这样可以：
- 实时追踪所有仓库的分析进度
- 清晰显示已完成与待处理任务
- 在批量摘要中汇总最终结果

### Step 5: 并发执行（限制最大并发数）

**最大并发 subagent 数：5**

当分析超过 5 个仓库时，按每批 5 个进行处理：
- 第一批：仓库 1-5 → fork 5 个 subagent
- 等待批次完成
- 下一批：仓库 6-10 → fork 5 个 subagent
- 继续直到全部完成

**每批使用 Agent tool fork subagent：**

```
Agent(
  subagent_type="project-analyzer",
  description="分析 {project_name}",
  prompt="""
  分析代码仓库并生成文档。

  输入参数：
  - 项目路径: {analysis_path}
  - 输出目录: {output_dir}/{repo_name}/
  - 项目名称: {project_name}
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
  run_in_background=true
)
```

**每个 subagent 完成后，更新任务列表：**

```
TodoWrite - 将对应任务标记为已完成
```

**当 `parallel = false` 时，顺序执行：**

逐个处理仓库，等待每个完成后再开始下一个。

### Step 6: 追踪进度

**并行模式下**，每个 subagent 完成时收到通知并更新任务状态：

```
✅ [1/3] {project_name} 完成
   类型: remote
   📁 本地: ~/.scv/repos/{repo_name}
   📂 输出: ~/.scv/analysis/{repo_name}/
   📄 4 个文档已生成
   🌿 分支: {branch}

✅ [2/3] {project_name} 完成
   类型: local
   📁 路径: {path}
   📂 输出: ~/.scv/analysis/{repo_name}/
   📄 4 个文档已生成

❌ [3/3] {project_name} 失败 - 分析失败
   类型: {remote/local}
   📁 路径: {actual_path}
   🚫 错误: {error_message}
```

### Step 7: 生成批量摘要

所有分析完成后（或 `fail_fast = true` 时停止），验证 TodoWrite 中所有任务已完成：

```
╔════════════════════════════════════════════════════════════╗
║         批量分析完成                                        ║
╚════════════════════════════════════════════════════════════╝

配置文件: ~/.scv/config.json
输出目录: ~/.scv/analysis
执行方式: {sequential/parallel}
持续时间: {X 分钟 Y 秒}

结果:
  ✅ 成功: {N}/{total}
  ❌ 失败: {N}/{total}
  ⏭️ 跳过: {N}/{total}

  远程仓库: {N} 个已分析
  本地仓库: {N} 个已分析

成功仓库:
  1. [remote] {project_name} → ~/.scv/analysis/{repo_name}/
  2. [local] {project_name} → ~/.scv/analysis/{repo_name}/
  ...

失败仓库:
  1. [remote] {project_name} → {error}
  2. [local] {project_name} → {error}
  ...

输出位置:
  所有分析保存到: ~/.scv/analysis/
  远程仓库克隆到: ~/.scv/repos/
```

### Step 8: 建议下一步

```
接下来可以做什么？

  📖 浏览文档
     - 打开 ~/.scv/analysis/ 查看生成的文档

  🔍 分析单个仓库
     - 使用 /scv run <repo_path> 进行针对性分析

  🌐 收集远程仓库
     - 使用 /scv gather --batch 克隆/拉取所有远程仓库

  🔄 重新运行批量分析
     - 使用 /scv batchRun 重新生成所有文档（会自动拉取远程仓库）

  📋 列出所有仓库
     - 使用 /scv gather --list 查看已克隆的远程仓库
```

## 配置文件 Schema

### ~/.scv/config.json

```json
{
  "output_dir": "~/.scv/analysis",
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
  ],
  "parallel": true,
  "fail_fast": false
}
```

### 字段说明

| 字段 | 必填 | 说明 | 默认值 |
|------|------|------|--------|
| `output_dir` | 否 | 所有分析的输出目录 | `~/.scv/analysis` |
| `parallel` | 否 | 并行执行 | `true` |
| `fail_fast` | 否 | 遇到错误停止 | `false` |

**远程仓库字段：**
- `type`: `"remote"` (必需)
- `url`: Git 仓库 URL (必需)
- `project_name`: 项目名称 (可选)
- `branch`: 分支 (可选)

**本地仓库字段：**
- `type`: `"local"` (必需)
- `path`: 本地路径 (必需，支持 `~`)
- `project_name`: 项目名称 (可选)

## 错误处理

| 错误 | 处理方式 |
|------|----------|
| 配置文件未找到 | 显示示例配置，退出 |
| 无效的仓库类型 | 报告错误，跳过该仓库 |
| Git pull 失败 | 报告错误，根据 `fail_fast` 决定是否继续 |
| 本地路径不存在 | 报告错误，跳过该仓库 |
| 分析失败 | 记录错误，继续其他仓库 |

## Subagent 策略详解

### 为什么使用 Subagent？

1. **Context 隔离**：每个仓库分析在独立 context 中，避免 token 累积
2. **真正并行**：多个 subagent 同时运行，大幅减少总时间
3. **容错性**：单个分析失败不影响其他
4. **可追踪**：每个 subagent 完成时收到通知

### 使用 project-analyzer Subagent

使用专用的 `project-analyzer` subagent 类型：
- **工具**：Read, Write, Glob, Grep, LSP（专为代码分析优化）
- **专注**：单一代码深度分析
- **输出**：遵循模板的结构化文档

## 最佳实践

1. **首次运行**：先运行 `/scv gather --batch` 克隆所有远程仓库
2. **定期更新**：`batchRun` 会自动 pull，但也可手动 `--update-all`
3. **检查配置**：定期查看 `~/.scv/gather --list` 确认仓库状态
4. **并行设置**：根据机器性能调整 `parallel` 和仓库数量
