# scv gather - 仓库管理

克隆和管理远程 Git 仓库，用于后续分析。

## 用法

```
/scv gather <git_url> [branch]           # 克隆单个仓库
/scv gather --batch [--config <path>]    # 批量克隆配置中的仓库
/scv gather --update [repo_name]         # 更新指定仓库
/scv gather --update-all                 # 更新所有仓库
/scv gather --list                       # 列出所有仓库
/scv gather --remove <repo_name>         # 删除仓库
```

## 执行步骤

### Step 1: 初始化仓库目录

```bash
mkdir -p ~/.scv/repos
```

显示存储位置：
```
📁 仓库存储: ~/.scv/repos
🔧 配置文件: ~/.scv/config.json
```

### Step 2: 解析参数并确定模式

| 模式 | 参数 | 说明 |
|------|------|------|
| 单仓库克隆 | `<git_url> [branch]` | 克隆单个 Git 仓库 |
| 批量克隆 | `--batch` | 从配置文件克隆所有远程仓库 |
| 更新仓库 | `--update [repo_name]` | 更新指定仓库 |
| 更新全部 | `--update-all` | 更新所有仓库 |
| 列出仓库 | `--list` | 显示所有已克隆仓库 |
| 删除仓库 | `--remove <repo_name>` | 删除指定仓库 |

无参数时显示用法：
```
用法: /scv gather <git_url> [branch]
      /scv gather --batch [--config <path>]
      /scv gather --update [repo_name] | --update-all
      /scv gather --list
      /scv gather --remove <repo_name>

命令:
  克隆单个仓库:     /scv gather <git_url> [branch]
  批量克隆:         /scv gather --batch [--config <path>]
  更新仓库:         /scv gather --update [repo_name]
  更新所有仓库:     /scv gather --update-all
  列出仓库:         /scv gather --list
  删除仓库:         /scv gather --remove <repo_name>

配置文件: ~/.scv/config.json

示例:
  /scv gather https://github.com/user/project.git
  /scv gather https://github.com/user/project.git main
  /scv gather --batch
  /scv gather --update my-project
  /scv gather --list
```

### Step 3: 单仓库克隆模式

1. **验证 Git URL**:
   - 检查是否为有效的 Git URL
   - 提取 `repo_name`: `basename(url)` 去掉 `.git`

2. **检查是否已存在**:
   - 目标路径: `~/.scv/repos/{repo_name}`
   - 已存在时询问用户：
     ```
     仓库 '{repo_name}' 已存在于 ~/.scv/repos/{repo_name}
     选项:
       1. 拉取最新更改 (git pull)
       2. 删除并重新克隆
       3. 跳过，使用现有
     选择 [1/2/3]:
     ```

3. **克隆仓库**:
   ```bash
   cd ~/.scv/repos
   git clone {git_url} {repo_name}
   ```
   指定分支时：
   ```bash
   git clone -b {branch} {git_url} {repo_name}
   ```

4. **验证并显示结果**:
   ```
   ✅ 仓库克隆成功
   📁 路径: ~/.scv/repos/{repo_name}
   🔗 URL: {git_url}
   🌿 分支: {current_branch}
   📦 最新提交: {commit_hash} - {commit_message}

   💡 提示: 要将此仓库添加到批量分析配置，请编辑 ~/.scv/config.json
   ```

### Step 4: 批量克隆模式

1. **确定配置文件**:
   - 默认: `~/.scv/config.json`
   - `--config <path>`: 使用指定路径

2. **加载配置并过滤**:
   - 只处理 `type: "remote"` 的仓库
   - 跳过 `type: "local"` 的仓库

3. **显示批量计划**:
   ```
   📋 批量克隆计划
   配置: ~/.scv/config.json
   输出目录: ~/.scv/analysis
   远程仓库: {N}
   本地仓库 (跳过): {N}

   待克隆远程仓库:
     1. [Remote Project 1] https://github.com/user/repo1.git (branch: main)
        → ~/.scv/repos/repo1
     2. [Remote Project 2] https://github.com/user/repo2.git (branch: develop)
        → ~/.scv/repos/repo2

   跳过 (本地):
     3. [Local Project 3] ~/local/repo3 (type: local, 不由 gather 克隆)
   ```

4. **执行克隆**:
   - 检查每个仓库是否存在
   - 存在：执行 `git pull`
   - 不存在：执行 `git clone`

5. **追踪进度**:
   ```
   ✅ [1/2] Remote Project 1 克隆完成
   📁 ~/.scv/repos/repo1
   🌿 分支: main

   ⚠️ [2/2] Remote Project 2 已存在，已更新
   📁 ~/.scv/repos/repo2
   🌿 分支: develop
   ```

6. **生成摘要**:
   ```
   ╔════════════════════════════════════════════════════════════╗
   ║         批量克隆完成                                        ║
   ╚════════════════════════════════════════════════════════════╝

   配置: ~/.scv/config.json
   结果:
     ✅ 新克隆: {N}/{total}
     ✅ 已更新: {N}/{total}
     ⚠️ 跳过: {N}/{total}
     ❌ 失败: {N}/{total}

   已处理远程仓库:
     1. Remote Project 1 → ~/.scv/repos/repo1
     2. Remote Project 2 → ~/.scv/repos/repo2

   失败仓库:
     1. Remote Project 3 → {error}

   下一步:
     - 运行 /scv batchRun 分析所有仓库（远程 + 本地）
   ```

### Step 5: 更新仓库模式

**更新单个仓库** (`--update [repo_name]`):

1. 未指定名称时，列出可用仓库让用户选择
2. 验证仓库存在于 `~/.scv/repos/{repo_name}`
3. 执行更新：
   ```bash
   cd ~/.scv/repos/{repo_name}
   git fetch origin
   git pull origin {current_branch}
   ```
4. 显示结果：
   ```
   ✅ 仓库更新成功
   📁 ~/.scv/repos/{repo_name}
   🌿 分支: {branch}
   📦 最新提交: {commit_hash} - {commit_message}
   ```

**更新所有仓库** (`--update-all`):

1. 列出 `~/.scv/repos/` 中所有目录
2. 对每个 Git 仓库执行 `git pull`
3. 显示汇总结果

### Step 6: 列出仓库模式

1. 扫描 `~/.scv/repos/` 目录
2. 对每个子目录：
   - 检查是否为 Git 仓库
   - 提取：名称、远程 URL、当前分支、最后提交日期

3. 显示表格：
   ```
   📁 本地仓库 (存储于 ~/.scv/repos/)

   | 名称          | 分支    | 最后提交           | URL                          |
   |---------------|---------|--------------------|------------------------------|
   | my-app        | main    | 2025-01-15 10:30   | github.com/user/my-app.git   |
   | api-service   | develop | 2025-01-14 14:22   | github.com/user/api.git      |
   | cli-tools     | master  | 2025-01-10 09:15   | github.com/user/cli.git      |

   总计: 3 个仓库
   ```

### Step 7: 删除仓库模式

1. 验证 `repo_name` 已提供
2. 确认删除：
   ```
   ⚠️  即将删除仓库: {repo_name}
   📁 位置: ~/.scv/repos/{repo_name}

   这将删除整个目录及其所有内容。
   这不会从 ~/.scv/config.json 中移除条目。

   确定吗？输入 'yes' 确认:
   ```
3. 确认后执行：
   ```bash
   rm -rf ~/.scv/repos/{repo_name}
   ```
4. 报告成功：
   ```
   ✅ 仓库已删除
   📁 ~/.scv/repos/{repo_name}
   💡 提示: 要从配置中移除，请编辑 ~/.scv/config.json
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
  "parallel": false,
  "fail_fast": false
}
```

### 目录名称提取

**远程仓库**:
- URL: `https://github.com/user/my-project.git` → 目录: `my-project`
- URL: `git@github.com:user/awesome-app.git` → 目录: `awesome-app`

**本地仓库**:
- 路径: `~/projects/my-app` → 目录: `my-app`

## 错误处理

| 错误 | 处理方式 |
|------|----------|
| 认证失败 | 引导配置 Git 凭证 |
| 无效 URL | 报告错误，建议正确格式 |
| 仓库已存在 | 询问用户操作 |
| 权限不足 | 检查目录权限 |
| 网络错误 | 检查网络连接和仓库可访问性 |

## 与其他命令集成

```bash
# 1. 克隆仓库
/scv gather https://github.com/user/awesome-project.git

# 2. 立即分析
/scv run ~/.scv/repos/awesome-project
```

```bash
# 1. 创建配置
cat > ~/.scv/config.json <<EOF
{
  "output_dir": "~/.scv/analysis",
  "repos": [
    {"type": "remote", "url": "https://github.com/team/backend.git", "project_name": "Backend"},
    {"type": "remote", "url": "https://github.com/team/frontend.git", "project_name": "Frontend"},
    {"type": "local", "path": "~/projects/docs", "project_name": "Docs"}
  ]
}
EOF

# 2. 批量克隆远程仓库
/scv gather --batch

# 3. 验证
/scv gather --list

# 4. 批量分析（远程 + 本地）
/scv batchRun
```

## 最佳实践

1. **使用有意义的项目名称** 便于识别
2. **指定分支** 当需要特定版本时
3. **定期更新** 使用 `--update-all`
4. **清理** 使用 `--remove` 删除不再需要的仓库
5. **检查状态** 使用 `--list` 查看所有仓库

## 注意事项

- `scv gather` **只处理** 配置中 `type: "remote"` 的仓库
- `scv batchRun` 处理**远程和本地**两种类型
- 目录名称**自动从 URL 或路径提取**
- `project_name` 用于文档和报告中的显示
- 所有分析输出存储在**同一个** `output_dir` 中
