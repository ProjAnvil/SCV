#!/usr/bin/env python3
"""Perception analyzer CLI (spec 2026-10-14 §6; v3 2026-10-20 §3).

Usage:
    analyzer.py <repo_dir> <out_json> [--extensions EXT ...]
                                    [--languages LANG ...]

Parses a repository with tree-sitter and writes the Vectra-batch JSON
``{"nodes": [...], "edges": [...], "unresolved": 0, "meta": {...}}``.

``--extensions`` is THE extension-enabling interface: the value list is
echoed verbatim in ``meta.extensions`` (enrichment lands with the registry).
``--languages`` selects the structure extractors (v3 spec §3/§7); the
default is every registered language. Unknown language values are warned
about on stderr and ignored.
Exit 0 on success; non-zero with a stderr message on usage/IO errors.
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))  # direct-script runs

from extensions import registry
from structure import java_extractor, jsts_extractor, python_extractor
from structure import langs as langs_registry
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
    parser.add_argument(
        "--languages", nargs="+", default=[], metavar="LANG",
        help="structure languages to run (e.g. java python); default: all")
    return parser


def _resolve_languages(requested: list[str]) -> list[str]:
    """Validate the --languages list; default = every registered language."""
    known = langs_registry.known()
    if not requested:
        return list(known)
    valid = [l for l in requested if l in known]
    for l in requested:
        if l not in known:
            print(f"analyzer: unknown language {l!r} "
                  f"(known: {', '.join(known)}) — ignored", file=sys.stderr)
    return valid or list(known)


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    repo = Path(args.repo_dir)
    if not repo.is_dir():
        parser.error(f"{repo} is not a directory")

    languages = _resolve_languages(args.languages)
    out = Graph_out(extensions=args.extensions)

    java_files: list = []
    jsts_langs: list[str] = []
    for lang in languages:
        if lang == "java":
            java_files = java_extractor.extract(repo, out)
        elif lang == "python":
            python_extractor.extract(repo, out)
        elif lang in ("javascript", "typescript"):
            jsts_langs.append(lang)
    if jsts_langs:
        # one extraction over the requested file sets so TS↔JS imports bind
        jsts_extractor.extract(repo, out, jsts_langs)
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
