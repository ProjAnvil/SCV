# scv run - 单仓库深度分析

通过启动专用的 `project-analyzer` subagent 分析单个代码仓库。

## 用法

```
/scv run <repo_path|url> [project_name]
```

**参数：**
- `repo_path|url` (必需): 本地路径或远程 Git URL
- `project_name` (可选): 自定义项目名称

**示例：**
```bash
# 远程仓库
/scv run https://github.com/user/project.git
/scv run https://github.com/user/project.git "My Project"

# 本地 Git 仓库
/scv run ~/projects/my-project
/scv run ~/projects/my-project "My Project"

# 本地非 Git 目录
/scv run ~/my-folder "My Analysis"
```

## 执行步骤

### Step 1: 初始化目录

```bash
mkdir -p ~/.scv/repos
mkdir -p ~/.scv/analysis
```

显示存储位置：
```
📁 仓库存储: ~/.scv/repos
📂 分析输出: ~/.scv/analysis
```

### Step 2: 解析输入并检测类型

解析用户输入，提取：
- **repo_input** (必需): 远程 Git URL 或本地路径
- **project_name** (可选): 自定义项目名称（默认：自动提取）

如果无参数，显示用法提示并退出。

**检测输入类型：**

| 类型 | 判断条件 | 处理方式 |
|------|----------|----------|
| 远程 Git URL | `https://`, `git://`, `git@` | 克隆到 `~/.scv/repos/` |
| 本地 Git 仓库 | 路径含 `.git` 目录 | 克隆到 `~/.scv/repos/` |
| 本地非 Git 目录 | 其他路径 | 直接分析，不复制 |

### Step 3: 准备仓库

**远程 Git URL：**

1. 提取 `repo_name`: `basename(url)` 去掉 `.git` 后缀
2. 本地路径: `~/.scv/repos/{repo_name}`
3. 检查是否已存在：
   - 存在：询问用户（pull / 重新克隆 / 使用现有）
   - 不存在：执行 `git clone`
4. 验证克隆成功，显示仓库信息

**本地 Git 仓库：**

1. 提取 `repo_name`: `basename(path)`
2. 克隆到 `~/.scv/repos/{repo_name}`
3. 验证克隆成功

**本地非 Git 目录：**

1. 验证目录存在且可读
2. 直接使用原路径分析

### Step 4: 确定路径和名称

1. **分析路径**:
   - 远程 URL: `~/.scv/repos/{repo_name}`
   - 本地 Git: `~/.scv/repos/{repo_name}`
   - 本地非 Git: 原路径

2. **输出路径**: 始终为 `~/.scv/analysis/{repo_name}/`

3. **项目名称**: 使用用户提供的名称，或使用 `repo_name`

4. **创建输出目录**: `mkdir -p ~/.scv/analysis/{repo_name}`

5. **显示分析计划**:
   ```
   📋 分析计划

   项目名称: {project_name}
   仓库类型: {remote/local-git/local-non-git}

   仓库位置:
   {analysis_path}

   输出目录:
   ~/.scv/analysis/{repo_name}/

   正在启动 project-analyzer subagent...
   ```

### Step 5: 启动 project-analyzer Subagent

**启动专用分析 subagent：**

```
Agent(
  subagent_type="project-analyzer",
  description="分析 {project_name}",
  prompt="""
  分析代码仓库并生成文档。

  输入参数：
  - 项目路径: {analysis_path}
  - 输出目录: {output_dir}
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
  """
)
```

**为什么使用 subagent？**
- **Context 隔离**：分析在独立 context 中运行，主对话保持干净
- **质量一致**：单仓库和批量分析使用相同分析引擎
- **Token 效率**：大型代码库分析不会污染主 context

### Step 6: 完成报告

subagent 完成后显示：

```
✅ 分析完成: {project_name}

仓库信息:
  📁 类型: {remote/local-git/local-non-git}
  📂 位置: {analysis_path}
  🌿 分支: {branch} (Git 仓库)
  📦 最新提交: {commit_hash} - {commit_message} (Git 仓库)

输出目录:
  📂 ~/.scv/analysis/{repo_name}/

生成文档:
  📄 README.md        - 项目概览
  📄 SUMMARY.md       - 5 分钟摘要
  📄 ARCHITECTURE.md  - 架构详情
  📄 FILE_INDEX.md    - 文件索引

下一步:
  - 查看生成的文档
  - 打开 README.md: open ~/.scv/analysis/{repo_name}/README.md

仓库管理:
  - 更新: /scv gather --update {repo_name}
  - 列表: /scv gather --list
  - 删除: /scv gather --remove {repo_name}
```

## 错误处理

**远程 URL 错误：**
- URL 无效：报告错误，建议正确格式
- 克隆失败：检查网络和 Git 凭证
- 需要认证：引导配置 Git 凭证

**本地路径错误：**
- 路径不存在：报告错误
- 权限拒绝：检查读权限
- 空目录：警告但继续生成最小文档

**分析错误：**
- Subagent 失败：报告错误详情，建议重试
- 部分失败：生成可能的部分并报告警告
