"""Graph output builder — Vectra batch JSON with drop/collision accounting.

Implements spec 2026-10-14 §4/§6:

- nodes are unique by key; a re-declared key is a *collision* (counted in
  ``meta.structure.key_collisions``, never silently merged),
- an edge is only emitted when BOTH endpoint keys are declared nodes;
  otherwise it is dropped and counted in ``meta.structure.dropped_edges``,
- ``to_json()`` materializes the batch payload
  ``{"nodes": [...], "edges": [...], "unresolved": 0, "meta": {...}}`` with
  structure counts derived from the emitted labels/edges.
"""

from __future__ import annotations

ENGINE = "treesitter-python"
LANGUAGES = ["java"]

_STRUCTURE_KEYS = (
    "files", "types", "methods", "call_edges",
    "dropped_edges", "unresolved_refs", "key_collisions",
)


class Graph_out:
    """Accumulates nodes/edges for one analyzer run."""

    def __init__(self, extensions: list[str] | None = None):
        self._nodes: dict[str, dict] = {}  # key -> node dict, insertion ordered
        self._edges: list[dict] = []
        self.key_collisions = 0
        self.dropped_edges = 0
        self.unresolved_refs = 0
        # bean-layer counter (top-level "unresolved", v1 backward-compat);
        # extensions fill it in (spec §6).
        self.unresolved = 0
        self.meta: dict = {
            "engine": ENGINE,
            "languages": list(LANGUAGES),
            "extensions": list(extensions or []),
            "structure": {k: 0 for k in _STRUCTURE_KEYS},
            "beans": 0,
            "inject_edges": 0,
            "ambiguous": 0,
        }

    # ---- nodes ----

    def add_node(self, key: str, label: str, props: dict | None = None) -> bool:
        """Add a node; False (and a counted collision) when the key exists."""
        if key in self._nodes:
            self.key_collisions += 1
            return False
        self._nodes[key] = {"key": key, "label": label, "props": dict(props or {})}
        return True

    def has_node(self, key: str) -> bool:
        return key in self._nodes

    @property
    def nodes(self) -> list[dict]:
        return list(self._nodes.values())

    # ---- edges ----

    def add_edge(self, src: str, dst: str, label: str,
                 props: dict | None = None) -> bool:
        """Add an edge; dropped+counted when either endpoint is missing."""
        if src not in self._nodes or dst not in self._nodes:
            self.dropped_edges += 1
            return False
        self._edges.append({"srcKey": src, "dstKey": dst,
                            "label": label, "props": dict(props or {})})
        return True

    def add_call(self, src: str, dst: str, line: int, kind: str) -> bool:
        """CALLS edge with line/kind props (same drop rule)."""
        return self.add_edge(src, dst, "CALLS", {"line": line, "kind": kind})

    def count_dropped_edge(self) -> None:
        """Account an edge that was never constructible (unresolved name)."""
        self.dropped_edges += 1

    @property
    def edges(self) -> list[dict]:
        return list(self._edges)

    # ---- output ----

    def to_json(self) -> dict:
        nodes = list(self._nodes.values())
        edges = list(self._edges)
        by_label: dict[str, int] = {}
        for n in nodes:
            by_label[n["label"]] = by_label.get(n["label"], 0) + 1
        meta = dict(self.meta)
        meta["structure"] = {
            "files": by_label.get("File", 0),
            "types": by_label.get("Type", 0),
            "methods": by_label.get("Method", 0),
            "call_edges": sum(1 for e in edges if e["label"] == "CALLS"),
            "dropped_edges": self.dropped_edges,
            "unresolved_refs": self.unresolved_refs,
            "key_collisions": self.key_collisions,
        }
        meta["beans"] = by_label.get("Bean", 0)
        meta["inject_edges"] = sum(1 for e in edges if e["label"] == "INJECTS")
        meta["ambiguous"] = sum(
            1 for e in edges if e["label"] == "INJECTS" and e["props"].get("ambiguous"))
        return {"nodes": nodes, "edges": edges, "unresolved": self.unresolved,
                "meta": meta}
