"""LLM client factory for the SCV eval: builds agents/judges from eval/.env."""

from __future__ import annotations

import os
from pathlib import Path

import smelt
from smelt import OpenAIChatClient

EVAL_DIR = Path(__file__).resolve().parents[1]

smelt.configure(env_file=str(EVAL_DIR / ".env"))
smelt.load_env()


def _role_llm(role: str) -> OpenAIChatClient:
    """Role-specific SMELT_<ROLE>_* wins; shared SMELT_API_KEY / SMELT_BASE_URL
    are the fallback — fill the key once in .env and it applies to every role."""
    model = os.environ.get(f"SMELT_{role}_MODEL")
    if not model:
        raise RuntimeError(
            f"SMELT_{role}_MODEL is not set in eval/.env "
            f"(copy .env.example to .env and fill it in)"
        )
    return OpenAIChatClient(
        model=model,
        base_url=os.environ.get(f"SMELT_{role}_BASE_URL") or os.environ.get("SMELT_BASE_URL"),
        api_key=os.environ.get(f"SMELT_{role}_API_KEY") or os.environ.get("SMELT_API_KEY"),
        # deepseek-v4-flash defaults to thinking mode, which requires passing
        # reasoning_content back each turn; disabling keeps the loop plain.
        extra_body={"thinking": {"type": "disabled"}},
    )


def agent_llm() -> OpenAIChatClient:
    """The LLM driving the SCV skill agent under test."""
    return _role_llm("AGENT")


def judge_llm() -> OpenAIChatClient:
    """The LLM grading outputs and reviewing SKILL.md writing."""
    return _role_llm("JUDGE")


def challenger_llm() -> OpenAIChatClient:
    """The LLM adversarially probing description/routing (challenge tier)."""
    return _role_llm("CHALLENGER")
