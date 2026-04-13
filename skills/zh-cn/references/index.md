# scv index - 重建分析索引

重建 `~/.scv/index.md` — 所有已分析仓库的摘要索引，包含功能职责关键词。

## 用法

```
/scv index
```

## 执行步骤

> ⚠️ **重要**：下面的 Step 2 是让**你（AI）亲自读文件并理解内容**。
> 不要用 Python 脚本解析 README，不要写代码来提取文字。
> 你直接读取每个 README.md，用自己的理解能力写出摘要和关键词。

### Step 1: 列出所有项目目录

```bash
ls ~/.scv/analysis/
```

收集 `~/.scv/analysis/` 下所有子目录名作为项目列表。若目录为空，显示提示并退出。

### Step 2: 你逐一读取每个项目的 README.md，亲自理解并写出两项内容

对每个项目，**使用文件读取工具**打开 `~/.scv/analysis/{project_name}/README.md`，然后**由你自己**写出：

- **一句话摘要**：这个项目是做什么的？用一句话说清楚核心功能，最长 200 字
- **功能职责关键词**：这个项目涉及哪些核心业务模块或功能领域？提炼出最多 5 个关键词，用 `、` 连接。若实在无法判断，填 `-`

两项内容完全由你根据阅读 README 后的理解来写，**不要执行任何脚本来帮你提取**。

### Step 3: 组装 entries JSON，调用脚本写入

将所有项目整理为 JSON 数组，调用脚本写入索引文件：

```bash
python3 ~/.claude/skills/scv/scripts/scv_util.py update-index \
  --analysis-dir ~/.scv/analysis \
  --entries-json '[
    {"name": "ecif-core",     "summary": "企业客户信息管理平台，提供统一的客户数据服务。", "keywords": "客户管理、数据同步、权限控制、审计日志、API网关"},
    {"name": "log-collector", "summary": "分布式日志采集与分析工具。",                   "keywords": "-"}
  ]'
```

脚本输出（JSON）：
```json
{"status": "index_updated", "index_file": "~/.scv/index.md", "total_projects": 2}
```

### Step 4: 完成报告

显示：

```
✅ 索引已更新: ~/.scv/index.md

  📇 项目总数: {N}
  📂 索引文件: ~/.scv/index.md

快速查看:
  cat ~/.scv/index.md
```

## 生成格式示例

安装时语言为 `zh-cn` 时，生成中文番容：

```markdown
# SCV 分析索引

## 目录结构

| 目录 | 内容 |
|------|------|
| `~/.scv/repos/` | 源代码仓库 |
| `~/.scv/analysis/` | 代码分析文档 |

---

## 项目索引

> 自动生成 · 2026-04-13 02:00 UTC

| # | 项目 | 简介 | 功能职责 |
|---|------|------|--------|
| 1 | ecif-core | 企业客户信息管理平台，提供统一的客户数据服务。 | 客户管理、数据同步、权限控制、审计日志、API网关 |
| 2 | ihybrid-forex | 外汇混合交易系统，支持多币种实时报价与交易执行。 | 报价引擎、订单管理、风控系统、账户结算 |
| 3 | log-collector | 分布式日志采集与分析工具。 | - |
```

## 使用场景

- **`scv run` 之后**: 索引会自动更新（Step 7.5）
- **`scv batchRun` 之后**: 索引会自动更新（Step 6.5）
- **手动重建**: 随时使用 `/scv index` 重建索引
- **索引丢失或损坏**: 运行 `/scv index` 从已有分析数据重新生成
