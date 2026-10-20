"""Canonical language table (spec 2026-10-20 §4).

THE registry of language names -> file suffixes + walk rules. The analyzer
dispatches on it; perceive.py mirrors the names (v2 precedent: each side
keeps its own walk code, the table is the shared contract).

Languages register here as their extractors land (T1: python; T2: js/ts;
T3: go; T4: rust). ``java`` is listed from the start — its walk stays owned
by ``java_extractor.discover_source_roots`` (v2 source-root semantics, spec
§4 java row).
"""
from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path

# Shared walk exclusions (spec §4): build outputs, caches, vendored trees,
# dot-directories. Java's own root discovery keeps its v2 set.
_EXCLUDED_DIRS = {
    "node_modules", "__pycache__", "venv", ".venv", "env",
    "dist", "build", ".gradle", "target", "vendor", "testdata", "out",
}

# Test trees skipped for the non-java walks (spec §4); java keeps its own
# src/test handling inside source-root discovery.
_TEST_DIRS = {"test", "tests"}


@dataclass(frozen=True)
class LanguageSpec:
    name: str
    suffixes: tuple[str, ...]
    skip_test_dirs: bool = True


_LANGUAGE_SPECS: dict[str, LanguageSpec] = {
    "java": LanguageSpec("java", (".java",), skip_test_dirs=False),
    "python": LanguageSpec("python", (".py",)),
    "javascript": LanguageSpec("javascript", (".js", ".jsx", ".mjs", ".cjs")),
    "typescript": LanguageSpec("typescript", (".ts", ".tsx", ".mts", ".cts")),
}


def known() -> list[str]:
    """Sorted names of the registered languages."""
    return sorted(_LANGUAGE_SPECS)


def spec(name: str) -> LanguageSpec | None:
    return _LANGUAGE_SPECS.get(name)


def walk(repo: Path, name: str) -> list[Path]:
    """Every source file of one language under ``repo``, exclusions applied.

    Sorted for deterministic output. Java callers use
    ``java_extractor.discover_source_roots`` instead (v2 semantics).
    """
    sp = _LANGUAGE_SPECS.get(name)
    if sp is None:
        return []
    files: list[Path] = []
    for dirpath, dirnames, filenames in os.walk(repo):
        dirnames[:] = [d for d in dirnames
                       if d not in _EXCLUDED_DIRS and not d.startswith(".")]
        if sp.skip_test_dirs:
            parts = Path(dirpath).relative_to(repo).parts
            if parts and parts[-1] in _TEST_DIRS:
                dirnames[:] = []
                continue
        for fn in sorted(filenames):
            if fn.endswith(sp.suffixes):
                files.append(Path(dirpath) / fn)
    return sorted(files)
