"""Behavior cases for the English SCV skill (skills/en).

Each case binds the real agent LLM from eval/.env plus the simulated shell
tools, mounts the skill's references, and asserts on routing (which reference
is consulted), restraint (which references stay unread), execution (tool
calls), and output quality (llm_judge).

Usable both from run_eval.py and directly:
    uv run smelt evaluate ../skills/en --cases scv_eval/cases_en.py --output reports/en.md
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
SKILL = Path(os.environ.get("SCV_EVAL_SKILL_DIR", EVAL_DIR.parent / "skills" / "en"))

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

# 1) run routing: a remote repo analysis request must consult run.md, prepare
#    the vault, clone, fetch commit info — without touching sibling references.
run_remote = (
    _case("run-remote-repo")
    .when(text("Please analyze the repository https://github.com/user/example-project.git with scv"))
    .then(reference_read("references/run.md"))
    .then(tool_call("run_command"))
    .then(no_reference_read("references/gather.md"))
    .then(no_reference_read("references/batchRun.md"))
    .then(
        llm_judge(
            judge,
            dimensions=[
                "preparation (creates/uses the ~/.scv layout before analysis)",
                "repository handling (clones the remote repo and verifies success)",
                "commit metadata (obtains commit info via the scv_util script)",
                "final reporting (clear completion report; graceful behavior when a subagent tool is unavailable)",
            ],
            include_trace=True,
            threshold=0.7,
        )
    )
)

# 2) gather routing: a clone request must consult gather.md and run git.
gather_clone = (
    _case("gather-clone-repo")
    .when(text("Clone https://github.com/user/awesome-project.git into my scv vault, branch main"))
    .then(reference_read("references/gather.md"))
    .then(tool_call("run_command"))
    .then(no_reference_read("references/run.md"))
)

# 3) index routing: index rebuild must consult index.md, read the READMEs
#    itself (no script parsing), then call update-index.
index_rebuild = (
    _case("index-rebuild")
    .when(text("Rebuild the scv analysis index"))
    .then(reference_read("references/index.md"))
    .then(tool_call("read_file"))
    .then(tool_call("run_command"))
    .then(no_reference_read("references/gather.md"))
)

# 4) batchRun routing: batch analysis must consult batchRun.md.
batch_run = (
    _case("batch-analyze-config")
    .when(text("Batch analyze all repositories in my scv config"))
    .then(reference_read("references/batchRun.md"))
    .then(tool_call("run_command"))
    .then(no_reference_read("references/gather.md"))
)

# 5) restraint: a simple "what is scv" question is answered from SKILL.md alone.
overview = (
    _case("overview-restraint")
    .when(text("What is scv and what can it do for me?"))
    .then(output_contains("SCV"))
    .then(*[no_reference_read(r) for r in _SUBCOMMAND_REFERENCES])
)

# 6) local non-Git directory: analyze in place, no git clone, custom name.
run_local_nongit = (
    _case("run-local-non-git")
    .when(text('Analyze the local folder ~/projects/my-notes with scv. It is not a git repo. Name the project "My Notes"'))
    .then(reference_read("references/run.md"))
    .then(
        llm_judge(
            judge,
            criteria=(
                "The folder is a local non-Git directory: it must be analyzed in place "
                "(no git clone), the output goes under ~/.scv/analysis/my-notes, and the "
                "project is referred to as \"My Notes\""
            ),
            include_trace=True,
            threshold=0.7,
        )
    )
)

cases = [run_remote, gather_clone, index_rebuild, batch_run, overview, run_local_nongit]
