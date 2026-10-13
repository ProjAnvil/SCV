"""Evaluate all SCV skills (en + zh-cn) with smelt and write reports.

Usage (from eval/):
    uv run python run_eval.py                     # both languages, tag "baseline"
    uv run python run_eval.py --lang en --tag v2  # one language, custom tag
    uv run python run_eval.py --times 1           # faster, noisier

Reports land in reports/<lang>-<tag>.{md,json}; pass --compare to diff two
saved JSON runs.
"""

from __future__ import annotations

import argparse
import importlib
import os
import shutil
import sys
import tempfile
from pathlib import Path

EVAL_DIR = Path(__file__).resolve().parent
sys.path.insert(0, str(EVAL_DIR))

import smelt  # noqa: E402

smelt.configure(env_file=str(EVAL_DIR / ".env"))
smelt.load_env()

from smelt import compare, evaluate_skill  # noqa: E402

from scv_eval.llm import agent_llm, judge_llm  # noqa: E402

SKILLS = {
    "en": EVAL_DIR.parent / "skills" / "en",
    "zh": EVAL_DIR.parent / "skills" / "zh-cn",
}
CASES = {"en": "scv_eval.cases_en", "zh": "scv_eval.cases_zh"}


def stage(skill: Path) -> Path:
    """Copy the skill to a temp dir named 'scv' — the deployed directory name.

    The repo keeps language variants under skills/en and skills/zh-cn, but
    install.sh deploys both as ~/.claude/skills/scv; lint's metadata check
    compares the frontmatter name against the directory name, so evaluating
    the staged copy measures the artifact users actually get.
    """
    stage_root = Path(tempfile.mkdtemp(prefix="scv-eval-stage-"))
    dst = stage_root / "scv"
    shutil.copytree(skill, dst)
    return dst


def evaluate(lang: str, tag: str, times: int, lint_only: bool = False) -> None:
    staged = stage(SKILLS[lang])
    os.environ["SCV_EVAL_SKILL_DIR"] = str(staged)
    cases = importlib.import_module(CASES[lang]).cases
    builder = evaluate_skill(staged, judge=None if lint_only else judge_llm())
    if lint_only:
        builder = (
            builder.with_lint()
            .with_writing(enabled=False)
            .with_suggestions(enabled=False)
            .with_challenge(enabled=False)
        )
    else:
        builder = (
            builder.with_cases(*cases)
            .with_times(times)
            .with_suggestions(max_items=8)
        )
    report = builder.run()
    reports = EVAL_DIR / "reports"
    reports.mkdir(exist_ok=True)
    md = report.save(reports / f"{lang}-{tag}.md")
    js = report.save(reports / f"{lang}-{tag}.json")

    def pct(x) -> str:
        return f"{x * 100:.1f}" if x is not None else "- (error)"

    print(f"\n=== {lang} ({tag}) ===")
    print(f"behavior: {pct(report.behavior_score)}")
    print(f"writing:  {pct(report.writing_score)}")
    print(f"lint:     {pct(report.lint_score)}")
    print(f"overall:  {report.overall_score:.1f} ({report.grade})")
    print("suggestions:")
    for i, s in enumerate(report.suggestions, 1):
        print(f"  {i}. {s}")
    print(f"reports: {md} | {js}")


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--lang", choices=["en", "zh", "all"], default="all")
    ap.add_argument("--tag", default="baseline", help="report filename tag")
    ap.add_argument("--times", type=int, default=2, help="repeats per behavior case")
    ap.add_argument("--lint-only", action="store_true", help="static lint only — no LLM calls, no cost")
    ap.add_argument(
        "--compare",
        nargs=2,
        metavar=("OLD_JSON", "NEW_JSON"),
        help="diff two saved JSON reports and exit",
    )
    args = ap.parse_args()

    if args.compare:
        diff = compare(args.compare[0], args.compare[1])
        print(diff.to_markdown())
        return 0

    langs = ["en", "zh"] if args.lang == "all" else [args.lang]
    for lang in langs:
        evaluate(lang, args.tag, args.times, lint_only=args.lint_only)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
