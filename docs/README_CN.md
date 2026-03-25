# SCV - Source Code Vault

> 专为 Claude Code 设计的强大代码库管理和分析工具

[![GitHub Repository](https://img.shields.io/badge/GitHub-SCV-blue.svg)](https://github.com/ProjAnvil/SCV)

**SCV** 提供三个子命令用于全面的代码库管理和分析：

- **scv run** - 对单个代码库进行深度分析
- **scv batchRun** - 批量分析多个代码库（使用 subagent 并行执行）
- **scv gather** - 克隆和管理远程 Git 仓库

## 📖 语言 / Language

- [English](../README.md) | [中文文档](./README_CN.md)

## 目录

- [特性](#特性)
- [安装](#安装)
- [快速开始](#快速开始)
- [命令说明](#命令说明)
  - [scv run](#scv-run---单个代码库分析)
  - [scv batchRun](#scv-batchrun---批量代码库分析)
  - [scv gather](#scv-gather---git-仓库管理)
- [配置文件](#配置文件)
- [输出文档说明](#输出文档说明)
- [工作流程](#工作流程)
- [最佳实践](#最佳实践)
- [故障排除](#故障排除)

## 特性

- 🔄 **远程仓库管理**：克隆、更新和管理远程 Git 仓库
- 📊 **深度代码分析**：为任何代码库生成全面的文档
- 🚀 **并行处理**：使用 subagent 同时分析多个仓库（最大并发 5 个）
- ⚡ **增量分析**：自动跳过 HEAD commit 未变化的仓库，无需重复分析
- 🔍 **深度分析模式**：集成 [codebones](https://github.com/creynir/codebones) 实现 token 高效的代码探索
- 📝 **多种输出格式**：README、摘要、架构、文件索引
- 🤖 **远程仓库自动拉取**：始终分析最新代码
- 🎯 **灵活配置**：支持远程和本地仓库
- 🌐 **多语言支持**：英文和中文 skill 可选

## 安装

### 前置要求

- **Claude Code CLI** - [安装指南](https://github.com/anthropics/claude-code)
- **codebones**（可选，用于深度分析）- Token 高效的代码探索工具
  > codebones 可在深度分析时实现 85% token 压缩。未安装时，深度分析将回退到标准文件读取。

### 快速安装（推荐）

运行安装脚本：

```bash
# 默认（英文）
./install.sh

# 中文版本
./install.sh --lang=zh-cn

# 查看帮助
./install.sh --help
```

该脚本将：
- 创建 `~/.scv` 配置目录
- 复制配置文件
- 安装所选语言的 skill 到 `~/.claude/skills/scv`
- 安装 `project-analyzer` agent 到 `~/.claude/agents/`

### 安装 codebones（可选，用于深度分析）

```bash
# 方式 1: pip
pip install codebones

# 方式 2: cargo
cargo install codebones

# 验证安装
codebones --version
```

> **什么是 codebones？** [codebones](https://github.com/creynir/codebones) 是一个 CLI 工具，可将代码精简为结构骨架，实现 85% token 压缩同时保留所有类签名、依赖关系和 API 映射。启用深度分析时，SCV 使用 codebones：
> - 生成压缩的骨架概览
> - 按需获取特定符号的完整实现（`codebones get`）
> - 搜索符号并追踪依赖（`codebones search`）

### 安装后目录结构

```
~/.scv/
├── config.json      # 仓库配置
├── repos/           # 克隆的远程仓库
├── analysis/        # 生成的文档
└── sessions/        # batchRun 会话状态（崩溃恢复用）

~/.claude/skills/scv/
├── SKILL.md         # Skill 入口
├── scripts/         # Python 辅助脚本（scv_util、batch_manager、git_op）
└── references/
    ├── run.md
    ├── batchRun.md
    ├── gather.md
    └── templates/
        ├── README.template.md
        ├── SUMMARY.template.md
        ├── ARCHITECTURE.template.md
        └── FILE_INDEX.template.md

~/.claude/agents/
└── project-analyzer.md   # 分析专用 subagent
```

## 快速开始

### 1. 克隆仓库

```
/scv gather https://github.com/user/project.git
```

### 2. 分析代码库

```
/scv run ~/.scv/repos/project
```

### 3. 查看生成的文档

```bash
open ~/.scv/analysis/project/README.md
```

## 命令说明

### scv run - 单个代码库分析

对一个代码库进行深度分析，生成 4 个文档：

- **README.md** - 项目总览入口
- **SUMMARY.md** - 项目摘要（5分钟了解项目全貌）
- **ARCHITECTURE.md** - 架构设计文档
- **FILE_INDEX.md** - 文件索引

#### 使用方法

```
/scv run <repo_path|url> [project_name]
```

#### 示例

```
# 远程仓库
/scv run https://github.com/user/project.git
/scv run https://github.com/user/project.git "我的项目"

# 本地 Git 仓库
/scv run ~/projects/my-project

# 本地非 Git 目录
/scv run ~/my-folder "我的分析"
```

### scv batchRun - 批量代码库分析

使用 `project-analyzer` subagent 并行分析多个仓库。每个仓库拥有独立的上下文。

#### 使用方法

```
/scv batchRun
```

从 `~/.scv/config.json` 读取配置。

#### 核心特性

- **Subagent 驱动**：每个仓库使用专用的 `project-analyzer` subagent
- **可配置并发**：config 中的 `batch_size` 设置最大并行数（默认 5）
- **增量跳过**：HEAD commit 未变化的仓库自动跳过，无需重复分析
- **任务追踪**：基于 TodoWrite 的进度可视化
- **上下文隔离**：避免分析多个仓库时的上下文膨胀
- **错误容错**：单个失败不影响其他分析
- **崩溃恢复**：会话状态持久化至 `~/.scv/sessions/`，支持断点续跑

### scv gather - Git 仓库管理

克隆和管理远程 Git 仓库。

#### 使用方法

```
/scv gather <git_url> [branch]           # 克隆单个仓库
/scv gather --batch                      # 从配置批量克隆
/scv gather --update [repo_name]         # 更新指定仓库
/scv gather --update-all                 # 更新所有仓库
/scv gather --list                       # 列出所有仓库
/scv gather --remove <repo_name>         # 删除仓库
```

#### 示例

```
# 克隆单个仓库
/scv gather https://github.com/user/project.git
/scv gather https://github.com/user/project.git main

# 批量克隆
/scv gather --batch

# 列出仓库
/scv gather --list

# 更新所有
/scv gather --update-all
```

## 配置文件

### 配置文件位置

`~/.scv/config.json`

```json
{
  "output_dir": "~/.scv/analysis",
  "batch_size": 5,
  "repos": [
    {
      "type": "remote",
      "url": "https://github.com/user/project1.git",
      "project_name": "远程项目 1",
      "branch": "main"
    },
    {
      "type": "remote",
      "url": "https://github.com/user/project2.git",
      "project_name": "远程项目 2",
      "branch": "develop"
    },
    {
      "type": "local",
      "path": "~/local/path/to/project3",
      "project_name": "本地项目 3"
    }
  ],
  "parallel": true,
  "fail_fast": false
}
```

### 配置字段说明

| 字段 | 必填 | 说明 | 默认值 |
|------|------|------|--------|
| `output_dir` | 否 | 所有分析的输出目录 | `~/.scv/analysis` |
| `batch_size` | 否 | batchRun 每批最大并发 subagent 数 | `5` |
| `parallel` | 否 | 批内并行执行 | `true` |
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

## 输出文档说明

### README.md

项目总览，包含：
- 项目定位和价值
- 技术栈
- 文档导航
- 项目统计
- 核心模块速览
- 快速开始指南

### SUMMARY.md

5分钟快速了解项目，包含：
- 项目类型和核心功能
- 技术栈全景
- 目录结构
- 模块划分
- 数据模型
- API 概览

### ARCHITECTURE.md

架构设计文档，包含：
- 系统架构图（Mermaid）
- 技术选型说明
- 模块设计
- 数据流
- 接口设计

### FILE_INDEX.md

文件索引，包含：
- 所有文件的职责说明
- 文件依赖关系
- 关键类/函数说明
- 快速定位指南

## 工作流程

### 流程 1：克隆并分析

```
# 1. 克隆远程仓库
/scv gather https://github.com/user/project.git

# 2. 分析
/scv run ~/.scv/repos/project

# 3. 查看结果
open ~/.scv/analysis/project/README.md
```

### 流程 2：批量分析

```bash
# 1. 配置仓库
cat > ~/.scv/config.json <<EOF
{
  "output_dir": "~/.scv/analysis",
  "repos": [
    {"type": "remote", "url": "https://github.com/team/backend.git", "project_name": "后端"},
    {"type": "remote", "url": "https://github.com/team/frontend.git", "project_name": "前端"},
    {"type": "local", "path": "~/projects/docs", "project_name": "文档"}
  ],
  "parallel": true
}
EOF

# 2. 克隆远程仓库（可选）
/scv gather --batch

# 3. 批量分析（远程仓库自动拉取）
/scv batchRun
```

### 流程 3：定期更新

```
# 直接运行 batchRun，它会自动拉取远程仓库最新代码
/scv batchRun
```

## 最佳实践

### 单个分析
- 在代码库重大变更后重新分析
- 使用自定义项目名称提高可读性

### 批量分析
- 使用 `parallel: true` 提高速度（默认）
- 使用 `fail_fast: true` 用于 CI/CD 验证
- 定期运行保持文档更新

### 配置管理
- 按项目或团队分组配置仓库
- 远程仓库用于稳定代码，本地仓库用于进行中的工作
- 可将 `~/.scv/config.json` 加入版本控制（排除敏感路径）

## 故障排除

**Q: 命令未找到**

A: 确保已安装 skill：
```bash
./install.sh --lang=zh-cn
```

**Q: Git 克隆失败**

A: 检查 Git 凭证：
```bash
git config --global credential.helper store
# 或配置 SSH 密钥
```

**Q: 分析不完整**

A: 检查文件权限，确保所有文件可读。

**Q: 模板文件缺失**

A: 重新运行安装脚本恢复模板。

## 架构

SCV 使用基于 skill 的架构：

1. **SKILL.md** - 入口点，路由子命令
2. **references/** - 各子命令的详细实现文档
3. **templates/** - 文档生成模板
4. **scripts/** - Python 辅助脚本（`scv_util.py`、`batch_manager.py`、`git_op.py`）
5. **project-analyzer agent** - 专用分析 subagent，负责实际代码分析

每个语言版本（en, zh-cn）都是独立的目录。

## 贡献

欢迎贡献！请随时提交 Pull Request。

## 许可证

本项目采用 MIT 许可证。

## 链接

- **仓库地址**：[https://github.com/ProjAnvil/SCV](https://github.com/ProjAnvil/SCV)
- **English README**: [../README.md](../README.md)
