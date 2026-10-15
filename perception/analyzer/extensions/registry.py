"""Extension registry (spec 2026-10-14 §5/§6).

Gates language extensions on the analyzer's ``--extensions`` list (THE
extension-enabling interface): known extensions are invoked with the parsed
``JavaFile`` list and the :class:`~structure.model.Graph_out`; unknown values
are warned about on stderr and ignored (perceive.py surfaces the warning).
"""

from __future__ import annotations

import sys

from . import springboot

# extension name -> enrich(java_files, out)
_EXTENSIONS = {
    "springboot": springboot.enrich,
}


def available() -> list[str]:
    """Sorted names of the known extensions."""
    return sorted(_EXTENSIONS)


def run(enabled: list[str], java_files: list, out) -> None:
    """Run every requested extension; unknown names warn + are ignored."""
    for name in enabled:
        enrich = _EXTENSIONS.get(name)
        if enrich is None:
            print(f"analyzer: unknown extension {name!r} "
                  f"(known: {', '.join(available())}) — ignored", file=sys.stderr)
            continue
        enrich(java_files, out)
