#!/usr/bin/env python3
"""Perception v2 analyzer CLI (spec 2026-10-14 §6).

Usage:
    analyzer.py <repo_dir> <out_json> [--extensions springboot]

Parses a repository with tree-sitter and writes the Vectra-batch JSON
``{"nodes": [...], "edges": [...], "unresolved": 0, "meta": {...}}``.
``--extensions`` is THE extension-enabling interface: the value list is
echoed verbatim in ``meta.extensions`` (enrichment lands with the registry).
Exit 0 on success; non-zero with a stderr message on usage/IO errors.
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))  # direct-script runs

from extensions import registry
from structure import java_extractor
from structure.model import Graph_out


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="analyzer.py",
        description="tree-sitter structure analyzer -> Vectra batch JSON")
    parser.add_argument("repo_dir", help="repository root to analyze")
    parser.add_argument("out_json", help="output JSON path")
    parser.add_argument(
        "--extensions", nargs="+", default=[], metavar="EXT",
        help="language extensions to enable (e.g. springboot); default: none")
    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    repo = Path(args.repo_dir)
    if not repo.is_dir():
        parser.error(f"{repo} is not a directory")

    out = Graph_out(extensions=args.extensions)
    java_files = java_extractor.extract(repo, out)
    registry.run(args.extensions, java_files, out)
    payload = out.to_json()

    out_path = Path(args.out_json)
    try:
        with out_path.open("w", encoding="utf-8") as fh:
            json.dump(payload, fh, indent=2)
            fh.write("\n")
    except OSError as err:
        print(f"analyzer: cannot write {out_path}: {err}", file=sys.stderr)
        return 1

    st = payload["meta"]["structure"]
    print("meta: files={files} types={types} methods={methods} "
          "call_edges={call_edges} dropped_edges={dropped_edges} "
          "unresolved_refs={unresolved_refs} key_collisions={key_collisions}"
          .format(**st))
    if args.extensions:
        meta = payload["meta"]
        print(f"beans: {meta['beans']} inject_edges: {meta['inject_edges']} "
              f"ambiguous: {meta['ambiguous']} unresolved: {payload['unresolved']}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
