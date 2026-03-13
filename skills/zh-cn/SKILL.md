---
name: scv
description: |
  SCV (Source Code Vault) - 代码仓库深度分析工具。

  子命令：
  - run <path|url>: 分析单个仓库，生成 4 个结构化文档
  - batchRun: 批量分析多个仓库（使用 subagent 并行执行）
  - gather <options>: 克隆/更新/管理远程 Git 仓库

  当用户需要：
  - 分析代码仓库结构和技术栈
  - 生成项目文档（README、SUMMARY、ARCHITECTURE、FILE_INDEX）
  - 批量分析多个仓库
  - 管理远程仓库克隆
  时主动使用此 skill。

  触发词：分析代码、生成文档、仓库分析、项目文档、批量分析
---

# SCV - Source Code Vault

代码仓库深度分析工具，生成结构化项目文档。

## 命令解析

解析用户输入的第一个参数作为子命令：

```
/scv run <repo_path|url> [project_name]
/scv batchRun
/scv gather <options>
```

| 子命令 | 功能 | 详细文档 |
|--------|------|----------|
| `run` | 单仓库深度分析 | 读取 `references/run.md` |
| `batchRun` | 批量并行分析 | 读取 `references/batchRun.md` |
| `gather` | 仓库管理 | 读取 `references/gather.md` |

## 执行流程

1. **解析用户输入**，提取子命令和参数
2. **加载对应的 reference 文件**：
   - `run` → `references/run.md`
   - `batchRun` → `references/batchRun.md`
   - `gather` → `references/gather.md`
3. **执行子命令逻辑**

## 核心资源

所有分析使用以下资源（位于本 skill 内）：

| 资源 | 路径 | 说明 |
|------|------|------|
| 分析 Prompt | `references/project-analyzer.md` | 核心分析逻辑 |
| 模板文件 | `references/templates/*.md` | 文档生成模板 |
| 配置文件 | `~/.scv/config.json` | 仓库列表和设置 |
| 仓库存储 | `~/.scv/repos/` | 克隆的远程仓库 |
| 分析输出 | `~/.scv/analysis/` | 生成的文档 |

## 输出结构

每个仓库分析后生成 4 个文档：

```
~/.scv/analysis/{project_name}/
├── README.md        # 项目概览入口
├── SUMMARY.md       # 5 分钟项目摘要
├── ARCHITECTURE.md  # 架构设计文档
└── FILE_INDEX.md    # 文件责任索引
```

## 分析原则

1. **代码即真理** - 基于实际代码分析，而非可能过时的注释
2. **命名揭示意图** - 从命名中提取含义作为主要文档来源
3. **依赖暴露架构** - 通过 import 语句理解真正的耦合和分层
4. **测试记录行为** - 测试用例是最准确的使用示例
5. **配置定义边界** - 配置文件揭示系统集成点和环境需求
6. **渐进式深挖** - 先全局后局部，先骨架后血肉
