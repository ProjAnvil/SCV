"""Behavior cases for the English project-analyzer subagent (agents/en).

Each case binds the subagent over the real agent .md (system prompt = body,
frontmatter tools allowlist enforced), mounts the mini-orders fixture plus the
REAL SCV templates, and asserts workflow discipline (scan before deep read),
deliverables (4 documents), the code-perception contract (Component
Dependencies rendered strictly from structure.json), and mode gating
(codebones only when Deep Analysis is on).

Usable from run_eval.py (--target agent) and directly via evaluate_agent.
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
    "SCV_EVAL_AGENT_FILE", EVAL_DIR.parent / "agents" / "en" / "project-analyzer.md"))

TEMPLATE_DIR = EVAL_DIR.parent / "skills" / "en" / "references" / "templates"

# The full analysis loop (scan -> read priority files -> read 4 templates ->
# write 4 docs) comfortably exceeds smelt's default 8 turns.
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
        "Analyze the codebase and generate documentation.\n\n"
        "Input Parameters:\n"
        "- Project Path: ./project\n"
        "- Output Directory: ./out\n"
        "- Project Name: mini-orders\n"
        "- Current Commit: N/A\n"
        f"- Deep Analysis: {'true' if deep else 'false'}\n"
        f"- Skeleton File: {skeleton}\n"
        f"- Structure Data: {structure}\n"
        "- Templates Directory: ./templates"
    )


# 1) Standard run: global scan first, then all 4 documents, grounded in the
#    real project name.
standard_four_docs = (
    _case("agent-standard-four-docs")
    .when(task(_task_prompt(deep=False, structure="N/A", skeleton="N/A")))
    .then(tool_call("Glob"))
    .then(WriteContains("README.md", "mini-orders"))
    .then(WriteContains("SUMMARY.md", "mini-orders"))
    .then(WriteContains("ARCHITECTURE.md", "mini-orders"))
    .then(WriteContains("FILE_INDEX.md", "mini-orders"))
)

# 2) Code perception: Component Dependencies section rendered strictly from
#    structure.json — section present, startup-fatal grading wording used,
#    and the bean names come from the data (grounding, not fabrication).
structure_data_rendered = (
    _case("agent-structure-data-rendered")
    .when(task(_task_prompt(deep=False, structure="./structure.json", skeleton="N/A")))
    .then(WriteContains("ARCHITECTURE.md", "Component Dependencies"))
    .then(WriteContains("ARCHITECTURE.md", "startup-fatal"))
    .then(WriteContains("ARCHITECTURE.md", "orderService"))
)

# 3) Mode gating: Deep Analysis off means no codebones / no shell at all.
deep_off_no_shell = (
    _case("agent-deep-off-no-shell")
    .when(task(_task_prompt(deep=False, structure="N/A", skeleton="N/A")))
    .then(no_tool_call("Bash"))
)

# 4) Mode gating: Deep Analysis on must use the codebones CLI.
deep_on_uses_codebones = (
    _case("agent-deep-on-uses-codebones")
    .when(task(_task_prompt(deep=True, structure="N/A", skeleton="./skeleton.md")))
    .then(tool_call("Bash"))
    .then(WriteContains("SUMMARY.md", "mini-orders"))
)

cases = [standard_four_docs, structure_data_rendered, deep_off_no_shell, deep_on_uses_codebones]
