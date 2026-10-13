# SCV 使用示例

每个子命令的示例调用及其触发的流程。

## run — 分析远程仓库

```
/scv run https://github.com/user/project.git
```

流程：创建 `~/.scv` 目录 → `git clone` 到 `~/.scv/repos/project` → 用
`scv_util.py get-commit-info` 读取 commit 信息 → 启动 `project-analyzer`
subagent → 写元数据 → 更新 `~/.scv/index.md` → 输出完成报告，指向
`~/.scv/analysis/project/`（README / SUMMARY / ARCHITECTURE / FILE_INDEX）。

自定义显示名：`/scv run https://github.com/user/project.git "My Project"`

## run — 分析本地非 Git 目录

```
/scv run ~/projects/my-notes "My Notes"
```

不克隆：目录原地分析；输出仍写入 `~/.scv/analysis/my-notes/`；跳过 commit 元数据。

## gather — 克隆、更新、列出、删除

```
/scv gather https://github.com/user/awesome-project.git main   # 克隆 main 分支
/scv gather --batch                                            # 批量克隆配置中的远程仓库
/scv gather --update awesome-project                           # 更新单个仓库
/scv gather --update-all                                       # 更新全部
/scv gather --list                                             # 列出已克隆仓库
/scv gather --remove awesome-project                           # 先要求 yes 确认
```

## batchRun — 批量分析配置中的所有仓库

```
/scv batchRun
```

读取 `~/.scv/config.json`，通过 `batch_manager.py` 生成计划，用同一分析流程处理
所有仓库（远程+本地），更新索引并输出逐仓库汇总。

## index — 重建分析索引

```
/scv index
```

列出 `~/.scv/analysis/`，亲自阅读每个项目的 README.md（禁止用解析脚本），
再通过 `scv_util.py update-index` 写入 `~/.scv/index.md`。
