"""Behavior cases for the Chinese project-analyzer subagent (agents/zh-cn).

Mirror of agent_cases_en with Chinese task prompts, the zh-cn templates, and
zh wording expectations (组件依赖 / 启动级).
"""

from __future__ import annotations

import os
import sys
from pathlib import Path

from smelt import context as smelt_context
from smelt import new_case, no_tool_call, subagent, task, tool_call

EVAL_DIR = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(EVAL_DIR))

from scv_eval.agent_expectations import WriteContains
from scv_eval.agent_tools import scv_agent_tools
from scv_eval.llm import agent_llm

AGENT = Path(os.environ.get(
    "SCV_EVAL_AGENT_FILE", EVAL_DIR.parent / "agents" / "zh-cn" / "project-analyzer.md"))

TEMPLATE_DIR = EVAL_DIR.parent / "skills" / "zh-cn" / "references" / "templates"

MAX_TURNS = 30


def _fixture_context():
    return smelt_context(files={
        "templates/README.template.md": TEMPLATE_DIR / "README.template.md",
        "templates/SUMMARY.template.md": TEMPLATE_DIR / "SUMMARY.template.md",
        "templates/ARCHITECTURE.template.md": TEMPLATE_DIR / "ARCHITECTURE.template.md",
        "templates/FILE_INDEX.template.md": TEMPLATE_DIR / "FILE_INDEX.template.md",
        "project": EVAL_DIR / "fixtures" / "agent_repo" / "project",
        "structure.json": EVAL_DIR / "fixtures" / "agent_repo" / "structure.json",
        "skeleton.md": EVAL_DIR / "fixtures" / "agent_repo" / "skeleton.md",
    })


def _case(name: str):
    return (
        new_case(name)
        .given(subagent(AGENT, llm=agent_llm(), tools=scv_agent_tools(), max_turns=MAX_TURNS))
        .given(_fixture_context())
    )


def _task_prompt(deep: bool, structure: str, skeleton: str) -> str:
    return (
        "分析代码库并生成文档。\n\n"
        "输入参数：\n"
        "- Project Path: ./project\n"
        "- Output Directory: ./out\n"
        "- Project Name: mini-orders\n"
        "- Current Commit: N/A\n"
        f"- Deep Analysis: {'true' if deep else 'false'}\n"
        f"- Skeleton File: {skeleton}\n"
        f"- Structure Data: {structure}\n"
        "- Templates Directory: ./templates\n"
        "- 输出语言：中文"
    )


# 1) 标准分析：先全局扫描，四份文档齐全，项目名正确落地
standard_four_docs = (
    _case("agent-standard-four-docs")
    .when(task(_task_prompt(deep=False, structure="N/A", skeleton="N/A")))
    .then(tool_call("Glob"))
    .then(WriteContains("README.md", "mini-orders"))
    .then(WriteContains("SUMMARY.md", "mini-orders"))
    .then(WriteContains("ARCHITECTURE.md", "mini-orders"))
    .then(WriteContains("FILE_INDEX.md", "mini-orders"))
)

# 2) 代码感知：组件依赖章节严格按 structure.json 渲染
structure_data_rendered = (
    _case("agent-structure-data-rendered")
    .when(task(_task_prompt(deep=False, structure="./structure.json", skeleton="N/A")))
    .then(WriteContains("ARCHITECTURE.md", "组件依赖"))
    .then(WriteContains("ARCHITECTURE.md", "启动级"))
    .then(WriteContains("ARCHITECTURE.md", "orderService"))
)

# 3) 模式门控：Deep Analysis 关闭时不执行任何 shell 命令
deep_off_no_shell = (
    _case("agent-deep-off-no-shell")
    .when(task(_task_prompt(deep=False, structure="N/A", skeleton="N/A")))
    .then(no_tool_call("Bash"))
)

# 4) 模式门控：Deep Analysis 开启时使用 codebones
deep_on_uses_codebones = (
    _case("agent-deep-on-uses-codebones")
    .when(task(_task_prompt(deep=True, structure="N/A", skeleton="./skeleton.md")))
    .then(tool_call("Bash"))
    .then(WriteContains("SUMMARY.md", "mini-orders"))
)

cases = [standard_four_docs, structure_data_rendered, deep_off_no_shell, deep_on_uses_codebones]
