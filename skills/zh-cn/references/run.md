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

5. **获取 commit 信息**（仅 Git 仓库）：

   ```bash
   python3 ~/.claude/skills/scv/scripts/scv_util.py get-commit-info \
     --repo {analysis_path}
   ```

   输出: `{ "hash": "abc123...", "short_hash": "abc123", "message": "...", ... }`
   保存 `current_commit = hash` 和 `short_commit = short_hash`，供 Step 5 和 Step 6 使用。
   若非 Git 仓库或命令失败，设置 `current_commit = null`。

6. **显示分析计划**:
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

### Step 5: 检查深度分析要求

**读取配置文件** 检查 `deep_analysis_enabled`:

```bash
cat ~/.scv/config.json 2>/dev/null | grep -q '"deep_analysis_enabled"[[:space:]]*:[[:space:]]*true'
```

**如果 `deep_analysis_enabled` 为 `true`:**

1. **检查 codebones 是否已安装:**

   ```bash
   which codebones
   ```

   Exit code 0 = 已安装, Exit code 1 = 未安装

2. **如果 codebones 未安装:**

   ```
   ⚠️ 深度分析已启用但 codebones 未安装。

   安装:
     pip install codebones
     # 或
     cargo install codebones

   请选择:
   1. 立即安装
   2. 继续标准分析
   3. 取消
   ```

3. **如果 codebones 已安装，准备深度分析基础设施:**

   ```bash
   # 先建立索引（所有 codebones 操作的前提）
   codebones index {analysis_path}

   # 生成骨架文件用于概览（85% token 压缩）
   codebones pack {analysis_path} --format markdown --max-tokens 100000 > ~/.scv/analysis/{repo_name}/.codebones_skeleton.md
   ```

   设置：
   - `use_deep_analysis = true`
   - `skeleton_file = ~/.scv/analysis/{repo_name}/.codebones_skeleton.md`
   - `repo_path = {analysis_path}`（用于 codebones get/search 操作）

### Step 6: 启动 project-analyzer Subagent

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
  - 当前提交: {current_commit or 'N/A'}
  - 模板目录: {skill_path}/references/templates/
  - 深度分析: {use_deep_analysis}
  - 骨架文件: {skeleton_file_path if use_deep_analysis else 'N/A'}
  - AI 模型: 使用系统上下文中的实际模型名称（如 claude-sonnet-4-6）

  执行 3 阶段分析工作流：
  1. Phase 1: 全局扫描 - 识别技术栈和结构
  2. Phase 2: 深度文件分析 - 分析优先级文件
  3. Phase 3: 文档生成 - 创建 4 个文档

  <!-- IF use_deep_analysis -->
  **深度分析模式已启用 - 渐进式深入策略：**

  你可以使用 `codebones` CLI 进行 token 高效的深度分析：

  **第 1 步：读取骨架获取概览（85% token 压缩）**
  ```
  读取文件: {skeleton_file_path}
  ```
  这给你所有的类签名、依赖关系和 API 映射。

  **第 2 步：识别需要深入的关键符号**
  从骨架中识别：
  - 核心 Service 类（业务逻辑）
  - Controller 类（API 端点）
  - 关键配置类

  **第 3 步：使用 codebones get 获取完整实现（按需）**
  对于需要深入理解的每个关键符号：
  ```bash
  # 获取特定类/方法的完整源代码
  codebones get "src/services/order.rs::OrderService"

  # 搜索符号位置
  codebones search "InventoryService"

  # 列出所有已索引符号
  codebones search ""
  ```

  **示例深入流程：**
  1. 骨架显示 `OrderService` 有 `@Autowired InventoryService`
  2. 使用 `codebones get "src/services/order.rs::OrderService"` 查看完整实现
  3. 发现 `create_order` 方法调用 `inventory.check_stock`
  4. 使用 `codebones search "InventoryService"` 找到所有使用位置
  5. 文档化服务交互链

  这种渐进式方法比读取所有文件节省约 85% 的 token，
  同时仍然能获得所需的完整实现细节。
  <!-- END IF -->

  在输出目录生成以下文档：
  - README.md - 项目概览
  - SUMMARY.md - 5 分钟摘要（深度分析时包含 Service 业务关联）
  - ARCHITECTURE.md - 架构设计
  - FILE_INDEX.md - 文件索引

  严格遵循模板格式，不确定项标记 [待确认]。
  填写 README.md 时，将 `{AI 模型名称}` 替换为系统上下文中的实际模型名称。
  """
)
```

**为什么使用 subagent？**
- **Context 隔离**：分析在独立 context 中运行，主对话保持干净
- **质量一致**：单仓库和批量分析使用相同分析引擎
- **Token 效率**：大型代码库分析不会污染主 context

### Step 7: 写入元数据（仅 Git 仓库）

subagent 成功完成后，记录已分析的 commit，以便下次运行时跳过未变更的仓库：

```bash
python3 ~/.claude/skills/scv/scripts/scv_util.py write-metadata \
  --repo {analysis_path} \
  --commit {current_commit} \
  --output-dir ~/.scv/analysis/{repo_name}
```

如果 `current_commit` 为 null（非 Git 目录）或 subagent 失败，则跳过此步骤。

### Step 8: 完成报告

subagent 完成后显示：

```
✅ 分析完成: {project_name}

仓库信息:
  📁 类型: {remote/local-git/local-non-git}
  📂 位置: {analysis_path}
  🌿 分支: {branch} (Git 仓库)
  📦 最新提交: {commit_hash} - {commit_message} (Git 仓库)

分析模式:
  🔍 深度分析: {use_deep_analysis ? '已启用 (codebones 骨架)' : '标准分析'}

输出目录:
  📂 ~/.scv/analysis/{repo_name}/

生成文档:
  📄 README.md        - 项目概览
  📄 SUMMARY.md       - 5 分钟摘要
  📄 ARCHITECTURE.md  - 架构详情
  📄 FILE_INDEX.md    - 文件索引

<!-- IF use_deep_analysis -->
Service 业务关联章节已包含在 SUMMARY.md 中
<!-- END IF -->

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
