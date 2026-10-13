"""Verify eval/.env credentials with one cheap LLM call.

Usage: uv run python smoke_env.py   (from eval/)
"""

from __future__ import annotations

import os
from pathlib import Path

import smelt

smelt.configure(env_file=str(Path(__file__).resolve().parent / ".env"))
smelt.load_env()

from openai import OpenAI

api_key = os.environ.get("SMELT_API_KEY")
if not api_key or "REPLACE-ME" in api_key:
    raise SystemExit("SMELT_API_KEY is not filled in eval/.env (copy .env.example)")

client = OpenAI(api_key=api_key, base_url=os.environ.get("SMELT_BASE_URL"))
model = os.environ.get("SMELT_JUDGE_MODEL", "deepseek-v4-flash")
resp = client.chat.completions.create(
    model=model,
    messages=[{"role": "user", "content": "Reply with exactly: OK"}],
    max_tokens=8,
    extra_body={"thinking": {"type": "disabled"}},
)
print(f"LLM OK ({model}): {resp.choices[0].message.content}")
