"""Behavior cases for the Chinese SCV skill (skills/zh-cn) — mirror of cases_en.

Chinese triggers matching the zh-cn SKILL.md trigger words (分析代码、生成文档、
仓库分析、批量分析). Usable both from run_eval.py and directly:
    uv run smelt evaluate ../skills/zh-cn --cases scv_eval/cases_zh.py --output reports/zh.md
"""

from __future__ import annotations

import os
import sys
from pathlib import Path

EVAL_DIR = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(EVAL_DIR))

from smelt import (
    llm_judge,
    new_case,
    no_reference_read,
    output_contains,
    reference_folder,
    reference_read,
    smelt_agent,
    text,
    tool_call,
)

from scv_eval.llm import agent_llm, judge_llm
from scv_eval.simshell import read_file, run_command

# run_eval.py stages the skill to a temp dir named 'scv' (the deployed name)
# and points SCV_EVAL_SKILL_DIR at it; direct CLI use falls back to the repo path.
SKILL = Path(os.environ.get("SCV_EVAL_SKILL_DIR", EVAL_DIR.parent / "skills" / "zh-cn"))

_SUBCOMMAND_REFERENCES = (
    "references/run.md",
    "references/batchRun.md",
    "references/gather.md",
    "references/index.md",
)


def _case(name: str):
    return (
        new_case(name)
        .given(reference_folder(SKILL))
        # 16 turns: the run flow alone needs ~10 tool calls (dirs, clone,
        # commit info, config, analysis, metadata, index, report); smelt's
        # default 8 caps out before the completion report.
        .given(smelt_agent(SKILL, llm=agent_llm(), tools=[read_file, run_command], max_turns=16))
    )


judge = judge_llm()

# 1) run 路由：远程仓库分析请求必须读取 run.md，准备目录、克隆、取 commit 信息。
run_remote = (
    _case("run-remote-repo")
    .when(text("帮我分析一下这个仓库：https://github.com/user/example-project.git"))
    .then(reference_read("references/run.md"))
    .then(tool_call("run_command"))
    .then(no_reference_read("references/gather.md"))
    .then(no_reference_read("references/batchRun.md"))
    .then(
        llm_judge(
            judge,
            dimensions=[
                "准备步骤（先创建/使用 ~/.scv 目录结构）",
                "仓库处理（克隆远程仓库并确认成功）",
                "提交信息（通过 scv_util 脚本获取 commit 信息）",
                "最终汇报（有清晰的完成报告；subagent 工具不可用时行为得体）",
            ],
            include_trace=True,
            threshold=0.7,
        )
    )
)

# 2) gather 路由：克隆请求必须读取 gather.md 并执行 git 命令。
gather_clone = (
    _case("gather-clone-repo")
    .when(text("把这个仓库克隆到我的 scv 仓库库里：https://github.com/user/awesome-project.git，分支 main"))
    .then(reference_read("references/gather.md"))
    .then(tool_call("run_command"))
    .then(no_reference_read("references/run.md"))
)

# 3) index 路由：重建索引必须读取 index.md，亲自读 README，再调 update-index。
index_rebuild = (
    _case("index-rebuild")
    .when(text("重建一下 scv 的分析索引"))
    .then(reference_read("references/index.md"))
    .then(tool_call("read_file"))
    .then(tool_call("run_command"))
    .then(no_reference_read("references/gather.md"))
)

# 4) batchRun 路由：批量分析必须读取 batchRun.md。
batch_run = (
    _case("batch-analyze-config")
    .when(text("把我 scv 配置里的所有仓库批量分析一遍"))
    .then(reference_read("references/batchRun.md"))
    .then(tool_call("run_command"))
    .then(no_reference_read("references/gather.md"))
)

# 5) 克制：简单介绍性问题只需 SKILL.md 即可回答。
overview = (
    _case("overview-restraint")
    .when(text("scv 是什么？能帮我做什么？"))
    .then(output_contains("SCV"))
    .then(*[no_reference_read(r) for r in _SUBCOMMAND_REFERENCES])
)

# 6) 本地非 Git 目录：原地分析、不克隆、使用自定义项目名。
run_local_nongit = (
    _case("run-local-non-git")
    .when(text("用 scv 分析本地目录 ~/projects/my-notes，它不是 git 仓库，项目名叫“我的笔记”"))
    .then(reference_read("references/run.md"))
    .then(
        llm_judge(
            judge,
            criteria=(
                "该目录是本地非 Git 目录：必须原地分析（不执行 git clone），"
                "输出到 ~/.scv/analysis/my-notes，且项目名使用用户给的自定义名称"
            ),
            include_trace=True,
            threshold=0.7,
        )
    )
)

cases = [run_remote, gather_clone, index_rebuild, batch_run, overview, run_local_nongit]
