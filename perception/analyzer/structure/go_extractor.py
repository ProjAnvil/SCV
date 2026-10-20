"""tree-sitter Go structure extraction (spec 2026-10-20 §4/§5/§5.1/§6, §3.1).

Emits File/Type/Method nodes (``lang: "go"``) plus CONTAINS / EXTENDS /
IMPLEMENTS / CALLS / REFERENCES / IMPORTS edges into the shared
:class:`~structure.model.Graph_out`. Go IMPLEMENTS is extracted by syntactic
method-set matching (spec §5.1, binding) — the one relation graphify punts on.

Graphify-aligned two-pass design (spec §3.1):

- pass 1 — per-file parse: import tables (local names, identifier uses),
  type tables (struct fields/embeds, interface methods/embeds/type-sets),
  function/method signatures; nodes and CONTAINS edges emitted immediately;
- pass 2 — cross-file binding: struct-embed EXTENDS, interface method-set
  union (materialized as Method nodes on the embedding interface, §3.1.4),
  method-set IMPLEMENTS with the §5.1 honest-miss counter, IMPORTS by
  identifier-use matching, and the CALLS/REFERENCES body walks.

Honesty rules (v2 discipline): unresolvable callee/receiver →
``unresolved_refs``; constructible-but-unresolvable heritage (external
embeds) → ``dropped_edges``; bare predeclared calls (``append``, ``len``,
... — the full Go spec list) are filtered silently BEFORE counting, and only
ever as bare identifiers (spec §3.1.3, the graphify ``append`` lesson).
Coverage that external embedding makes unknowable is never guessed: no edge +
``meta.structure.go_unverifiable_satisfaction``.

Grammar node-name pitfalls (tree-sitter-go 0.25, probed):

- ``field_declaration`` has NO ``type``/``name`` fields: the type is the
  first named non-``field_identifier`` child; an embed is a
  ``field_declaration`` with no ``field_identifier`` child at all, and
  ``*sync.Mutex`` embeds keep the ``*`` as an ANONYMOUS sibling of the
  qualified_type — pointer-ness is only visible in the full field text.
- ``key, value string`` is ONE ``parameter_declaration`` holding TWO
  ``identifier`` children (arity = identifier count, every name repeats the
  type text). Variadics are a separate ``variadic_parameter_declaration``
  node whose ``type`` field excludes the ``...``.
- ``method_declaration`` names are ``field_identifier`` (package functions
  are ``identifier``) — ``child_by_field_name("name")`` covers both.
- A type-set constraint is a ``type_elem`` containing ``|`` or
  ``negated_type`` children (``~T`` IS ``negated_type``); a lone ``type_elem``
  is an embedded interface. Type-set terms are REFERENCES, never heritage.
- ``type_declaration`` may hold several ``type_spec`` children (and
  ``type_alias`` children, which are skipped: an alias is not a new node).
- A generic function instantiation ``New[string](64)`` /
  ``pkg.New[string](64)`` parses as a ``type_conversion_expression``
  wrapping a ``generic_type`` — the grammar cannot tell instantiation from
  conversion. Plain conversions (``int(x)``, ``Memory(x)``) stay ordinary
  ``call_expression``s with an identifier callee.
- Import aliases live in ``import_spec`` field ``name`` (a
  ``package_identifier``); the local name defaults to the last path segment.
  ``_``/``.`` imports get local name "" and can never match a use.
"""
from __future__ import annotations

import posixpath
import re
from dataclasses import dataclass, field
from pathlib import Path

import tree_sitter_go
from tree_sitter import Language, Parser

from .langs import walk as walk_lang
from .model import Graph_out

_LANG = Language(tree_sitter_go.language())
_PARSER = Parser(_LANG)

# Go predeclared types — never resolvable to an in-source Type node.
_GO_PREDECLARED_TYPES = frozenset({
    "bool", "byte", "complex64", "complex128", "error", "float32", "float64",
    "int", "int8", "int16", "int32", "int64", "rune", "string",
    "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "any",
    "comparable",
})

# Go predeclared functions — bare-identifier-scoped only (spec §3.1.3, the
# graphify ``append`` lesson: one same-named method once absorbed 330 phantom
# call edges). Selector calls (``pkg.Delete``, ``h.append``) never consult
# this table. The full Go spec list, 17 names.
_GO_PREDECLARED_FUNCS = frozenset({
    "append", "cap", "clear", "close", "complex", "copy", "delete", "imag",
    "len", "make", "max", "min", "new", "panic", "print", "println", "real",
    "recover",
})

# A written type that names exactly one (possibly package-qualified) type,
# with optional generic instantiation arguments: `Memory`, `*Memory`,
# `queue.Queue[string]` (after pointer stripping). Slices/maps/arrays/chans/
# funcs never match (their text starts with a bracket/keyword or fails the
# trailing-args shape).
_NAMED_TYPE_RE = re.compile(
    r"^([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)?)\s*(?:\[.*\])?$",
    re.S,
)

_MODULE_RE = re.compile(r"^module\s+(\S+)", re.M)


def _read(node, source: bytes) -> str:
    return source[node.start_byte:node.end_byte].decode("utf-8", "replace")


def _simple_name(text: str) -> str:
    """Simple type name: last dot-segment of the written text (§5.1.4 —
    ``pkg.Foo`` vs local ``Foo`` collide, documented approximation)."""
    return text.split(".")[-1].strip()


def _strip_generics(text: str) -> str:
    """`Queue[T]` -> `Queue` (graphify's #3368 lesson, applied uniformly)."""
    return text.split("[", 1)[0].strip()


def _pkg_of(rel: str) -> str:
    return posixpath.dirname(rel)


def _tkey(pkg: str, name: str) -> str:
    # root-package files (repo-root .go) keep the delimiter: "type:.Main"
    return f"type:{pkg}.{name}" if pkg else f"type:.{name}"


def _fkey_func(pkg: str, name: str, params: list[str]) -> str:
    base = f"{pkg}#{name}" if pkg else f"#{name}"
    return f"fn:{base}({','.join(params)})"


def _fkey_method(pkg: str, tname: str, name: str, params: list[str]) -> str:
    base = f"{pkg}.{tname}#{name}" if pkg else f".{tname}#{name}"
    return f"fn:{base}({','.join(params)})"


def _module_path(repo: Path) -> str | None:
    mod = repo / "go.mod"
    try:
        text = mod.read_text(encoding="utf-8", errors="replace")
    except OSError:
        return None
    m = _MODULE_RE.search(text)
    return m.group(1) if m else None


# ---- pass-1 parse model ----

@dataclass
class GoImport:
    path: str
    local: str          # "" for _ / . imports (never matches a use)
    line: int


@dataclass
class GoFunc:
    name: str
    params: list[str]           # written type text per logical param
    names: list[str]            # param names, "" when unnamed
    results: list[str]          # written result type texts
    line: int
    node: object = None         # body node (functions/methods only)


@dataclass
class GoMethodRef:
    type_name: str
    pointer: bool
    recv_name: str
    func: GoFunc


@dataclass
class GoField:
    name: str | None            # None = embed
    type_text: str
    line: int
    pointer: bool = False


@dataclass
class GoType:
    name: str
    kind: str                   # struct | interface | named
    line: int
    fields: list[GoField] = field(default_factory=list)   # struct fields+embeds
    methods: list[GoFunc] = field(default_factory=list)   # interface method_elem
    embeds: list[GoField] = field(default_factory=list)   # interface type_elem
    typeset_terms: list[tuple[str, int]] = field(default_factory=list)


@dataclass
class GoFile:
    rel: str
    pkg: str
    source: bytes = b""
    tree: object = None
    imports: list[GoImport] = field(default_factory=list)
    types: list[GoType] = field(default_factory=list)
    functions: list[GoFunc] = field(default_factory=list)
    methods: list[GoMethodRef] = field(default_factory=list)
    var_types: dict[str, str] = field(default_factory=dict)
    declared: set[str] = field(default_factory=set)
    uses: dict[str, set[str]] = field(default_factory=dict)


def _sig(fn_node, source: bytes) -> tuple[list[str], list[str], list[str]]:
    """(param type texts, param names, result type texts) as written."""
    params: list[str] = []
    names: list[str] = []
    pnode = fn_node.child_by_field_name("parameters")
    if pnode is not None:
        for c in pnode.children:
            if c.type == "parameter_declaration":
                tnode = c.child_by_field_name("type")
                if tnode is None:
                    continue
                text = _read(tnode, source)
                idents = [x for x in c.children if x.type == "identifier"]
                if idents:
                    for x in idents:
                        params.append(text)
                        names.append(_read(x, source))
                else:
                    params.append(text)
                    names.append("")
            elif c.type == "variadic_parameter_declaration":
                tnode = c.child_by_field_name("type")
                if tnode is None:
                    continue
                params.append("..." + _read(tnode, source))
                nm = c.child_by_field_name("name")
                names.append(_read(nm, source) if nm is not None else "")
    results: list[str] = []
    rnode = fn_node.child_by_field_name("result")
    if rnode is not None:
        if rnode.type == "parameter_list":
            for c in rnode.children:
                if c.type == "parameter_declaration":
                    tnode = c.child_by_field_name("type")
                    if tnode is not None:
                        results.append(_read(tnode, source))
                elif c.type == "variadic_parameter_declaration":
                    tnode = c.child_by_field_name("type")
                    if tnode is not None:
                        results.append("..." + _read(tnode, source))
        else:
            results.append(_read(rnode, source))
    return params, names, results


def _parse_file(path: Path, repo: Path) -> GoFile:
    source = path.read_bytes()
    root = _PARSER.parse(source).root_node
    rel = path.relative_to(repo).as_posix()
    pf = GoFile(rel=rel, pkg=_pkg_of(rel), source=source, tree=root)

    def _func(fn_node) -> GoFunc:
        name_node = fn_node.child_by_field_name("name")
        params, names, results = _sig(fn_node, source)
        return GoFunc(name=_read(name_node, source), params=params,
                      names=names, results=results,
                      line=fn_node.start_point[0] + 1,
                      node=fn_node.child_by_field_name("body"))

    def _import_spec(spec) -> None:
        pnode = spec.child_by_field_name("path")
        if pnode is None:
            return
        raw = _read(pnode, source).strip('"')
        nnode = spec.child_by_field_name("name")
        local = _read(nnode, source) if nnode is not None else raw.rsplit("/", 1)[-1]
        if local in ("_", "."):
            local = ""
        pf.imports.append(GoImport(path=raw, local=local,
                                   line=spec.start_point[0] + 1))

    def _struct_field(gt: GoType, f) -> None:
        names = [c for c in f.children if c.type == "field_identifier"]
        tnode = f.child_by_field_name("type")
        if tnode is None:
            tnode = next((c for c in f.children
                          if c.is_named and c.type != "field_identifier"), None)
        if tnode is None:
            return
        text = _read(tnode, source)
        line = f.start_point[0] + 1
        if names:
            for nm in names:
                gt.fields.append(GoField(name=_read(nm, source),
                                         type_text=text, line=line))
        else:
            # embed: pointer-ness only from the full text (`*` is an
            # anonymous sibling of the type node in this grammar)
            pointer = _read(f, source).strip().startswith("*")
            gt.fields.append(GoField(name=None, type_text=text, line=line,
                                     pointer=pointer))

    def _type_elem(gt: GoType, elem) -> None:
        is_typeset = any(c.type == "|" or c.type == "negated_type"
                         for c in elem.children)
        line = elem.start_point[0] + 1
        if is_typeset:
            stack = [elem]
            while stack:
                n = stack.pop()
                if n.type == "type_identifier":
                    gt.typeset_terms.append((_read(n, source), line))
                stack.extend(n.children)
            return
        tnode = next((c for c in elem.children if c.is_named), None)
        if tnode is not None:
            gt.embeds.append(GoField(
                name=None, type_text=_read(tnode, source), line=line,
                pointer=_read(elem, source).strip().startswith("*")))

    def _type_spec(spec) -> None:
        name_node = spec.child_by_field_name("name")
        tnode = spec.child_by_field_name("type")
        if name_node is None or tnode is None:
            return
        name = _read(name_node, source)
        line = spec.start_point[0] + 1
        if tnode.type == "struct_type":
            gt = GoType(name=name, kind="struct", line=line)
            for c in tnode.named_children:
                if c.type == "field_declaration_list":
                    for f in c.children:
                        if f.type == "field_declaration":
                            _struct_field(gt, f)
        elif tnode.type == "interface_type":
            gt = GoType(name=name, kind="interface", line=line)
            for elem in tnode.children:
                if elem.type == "method_elem":
                    gt.methods.append(_func(elem))
                elif elem.type == "type_elem":
                    _type_elem(gt, elem)
        else:
            gt = GoType(name=name, kind="named", line=line)
        pf.types.append(gt)
        pf.declared.add(name)

    def _method(stmt) -> None:
        recv = stmt.child_by_field_name("receiver")
        type_name: str | None = None
        pointer = False
        recv_name = ""
        if recv is not None:
            for p in recv.children:
                if p.type != "parameter_declaration":
                    continue
                tn = p.child_by_field_name("type")
                if tn is not None:
                    text = _read(tn, source).strip()
                    pointer = text.startswith("*")
                    type_name = _strip_generics(text.lstrip("*").strip())
                nn = p.child_by_field_name("name")
                if nn is not None:
                    recv_name = _read(nn, source)
                break
        if not type_name:
            return
        pf.methods.append(GoMethodRef(type_name=type_name, pointer=pointer,
                                      recv_name=recv_name, func=_func(stmt)))

    for stmt in root.children:
        t = stmt.type
        if t == "import_declaration":
            for c in stmt.children:
                if c.type == "import_spec_list":
                    for spec in c.children:
                        if spec.type == "import_spec":
                            _import_spec(spec)
                elif c.type == "import_spec":
                    _import_spec(c)
        elif t == "type_declaration":
            for spec in stmt.named_children:
                if spec.type == "type_spec":
                    _type_spec(spec)
        elif t == "function_declaration":
            f = _func(stmt)
            pf.functions.append(f)
            pf.declared.add(f.name)
        elif t == "method_declaration":
            _method(stmt)
        elif t == "var_declaration":
            for vs in stmt.named_children:
                if vs.type != "var_spec":
                    continue
                tn = vs.child_by_field_name("type")
                text = _read(tn, source) if tn is not None else None
                for c in vs.children:
                    if c.type != "identifier":
                        continue
                    nm = _read(c, source)
                    pf.declared.add(nm)
                    if text is not None:
                        pf.var_types[nm] = text
        elif t == "const_declaration":
            for cs in stmt.named_children:
                if cs.type == "const_spec":
                    for c in cs.children:
                        if c.type == "identifier":
                            pf.declared.add(_read(c, source))

    # identifier uses through each import's local name (selector fields +
    # qualified types anywhere in the file) — the IMPORTS match key (§5).
    local_names = {imp.local for imp in pf.imports if imp.local}

    def _visit(n) -> None:
        if n.type == "selector_expression":
            op = n.child_by_field_name("operand")
            fd = n.child_by_field_name("field")
            if (op is not None and op.type == "identifier"
                    and fd is not None):
                nm = _read(op, source)
                if nm in local_names:
                    pf.uses.setdefault(nm, set()).add(_read(fd, source))
        elif n.type == "qualified_type":
            head, _, tail = _read(n, source).partition(".")
            if tail and head in local_names:
                pf.uses.setdefault(head, set()).add(tail.split("[")[0])
        for c in n.children:
            _visit(c)

    _visit(root)
    return pf


def extract(repo: Path, out: Graph_out) -> list[GoFile]:
    """Two-pass extraction: nodes + CONTAINS now, everything else after."""
    parsed: list[GoFile] = []
    for path in walk_lang(repo, "go"):
        try:
            parsed.append(_parse_file(path, repo))
        except OSError:
            continue

    for pf in parsed:
        loc = pf.source.count(b"\n") + (0 if pf.source.endswith(b"\n")
                                        or not pf.source else 1)
        file_key = f"file:{pf.rel}"
        out.add_node(file_key, "File",
                     {"path": pf.rel, "language": "go", "lang": "go",
                      "loc": loc})
        for gt in pf.types:
            tkey = _tkey(pf.pkg, gt.name)
            out.add_node(tkey, "Type",
                         {"lang": "go", "kind": gt.kind, "modifiers": "",
                          "loc": 0, "line": gt.line})
            out.add_edge(file_key, tkey, "CONTAINS")
            for m in gt.methods:  # interface method specs (§3.1.4)
                out.add_node(_fkey_method(pf.pkg, gt.name, m.name, m.params),
                             "Method", _method_props(m, owner=tkey))
        for f in pf.functions:
            out.add_node(_fkey_func(pf.pkg, f.name, f.params), "Method",
                         _method_props(f, owner=file_key))
        for mr in pf.methods:
            props = _method_props(mr.func, owner=_tkey(pf.pkg, mr.type_name))
            props["receiver"] = "pointer" if mr.pointer else "value"
            out.add_node(_fkey_method(pf.pkg, mr.type_name, mr.func.name,
                                      mr.func.params), "Method", props)

    _bind(repo, parsed, out)
    return parsed


def _method_props(f: GoFunc, owner: str) -> dict:
    return {"lang": "go", "name": f.name,
            "visibility": "public" if f.name[:1].isupper() else "private",
            "static": False, "params": ",".join(f.params),
            "returns": ",".join(f.results), "loc": 0, "line": f.line,
            "owner": owner}


# ---- pass 2: cross-file binding ----

def _bind(repo: Path, parsed: list[GoFile], out: Graph_out) -> None:
    module = _module_path(repo)
    files_by_pkg: dict[str, list[GoFile]] = {}
    for pf in parsed:
        files_by_pkg.setdefault(pf.pkg, []).append(pf)
    pkg_dirs = set(files_by_pkg)

    type_of: dict[tuple[str, str], str] = {}          # (pkg, name) -> tkey
    go_type_of: dict[str, GoType] = {}                # tkey -> GoType
    type_file: dict[str, GoFile] = {}                 # tkey -> declaring file
    type_decl: dict[str, tuple[str, str]] = {}        # tkey -> (pkg, name)
    func_of: dict[tuple[str, str], tuple[str, list[str], GoFile]] = {}
    methods_of: dict[str, dict[str, tuple[str, list[str], list[str], bool, GoFile]]] = {}
    var_of: dict[tuple[str, str], str] = {}
    for pf in parsed:
        for gt in pf.types:
            tkey = _tkey(pf.pkg, gt.name)
            type_of[(pf.pkg, gt.name)] = tkey
            type_decl[tkey] = (pf.pkg, gt.name)
            go_type_of.setdefault(tkey, gt)
            type_file.setdefault(tkey, pf)
        for f in pf.functions:
            func_of[(pf.pkg, f.name)] = (_fkey_func(pf.pkg, f.name, f.params),
                                          f.results, pf)
        for mr in pf.methods:
            tkey = _tkey(pf.pkg, mr.type_name)
            methods_of.setdefault(tkey, {})[mr.func.name] = (
                _fkey_method(pf.pkg, mr.type_name, mr.func.name,
                             mr.func.params),
                mr.func.params, mr.func.results, mr.pointer, pf)
        for vn, vt in pf.var_types.items():
            var_of[(pf.pkg, vn)] = vt

    def _resolve_pkg_path(path: str) -> str | None:
        if module:
            if path == module:
                return "" if "" in pkg_dirs else None
            if path.startswith(module + "/"):
                rel = path[len(module) + 1:]
                return rel if rel in pkg_dirs else None
            return None
        if path in pkg_dirs:
            return path
        for d in sorted(pkg_dirs, key=lambda x: -len(x)):
            if path.endswith("/" + d):
                return d
        return None

    import_map: dict[str, dict[str, str | None]] = {}
    for pf in parsed:
        import_map[pf.rel] = {imp.local: _resolve_pkg_path(imp.path)
                              for imp in pf.imports if imp.local}

    def _resolve_type(text: str, pf: GoFile) -> str | None:
        """Written type text -> in-source Type key (pointer/generic-stripped)."""
        t = text.strip()
        while t.startswith("*"):
            t = t[1:].strip()
        m = _NAMED_TYPE_RE.match(t)
        if m is None:
            return None
        base = m.group(1)
        if "." in base:
            pkg_local, name = base.split(".", 1)
            pkg = import_map.get(pf.rel, {}).get(pkg_local)
            if pkg is None:
                return None
            return type_of.get((pkg, name))
        if base in _GO_PREDECLARED_TYPES:
            return None
        return type_of.get((pf.pkg, base))

    # ---- EXTENDS: struct embeds (spec §5 go column) ----
    extends_seen: set[tuple[str, str]] = set()
    for tkey, gt in go_type_of.items():
        if gt.kind != "struct":
            continue
        pf = type_file[tkey]
        for f in gt.fields:
            if f.name is not None:
                continue
            target = _resolve_type(f.type_text, pf)
            if target is None:
                out.count_dropped_edge()  # external embed — honest drop
                continue
            if (tkey, target) not in extends_seen:
                extends_seen.add((tkey, target))
                out.add_edge(tkey, target, "EXTENDS", {"kind": "declared"})

    # ---- REFERENCES: struct field types (Type→Type) ----
    for tkey, gt in go_type_of.items():
        if gt.kind != "struct":
            continue
        pf = type_file[tkey]
        for f in gt.fields:
            if f.name is None:
                continue
            target = _resolve_type(f.type_text, pf)
            if target is not None and target != tkey:
                out.add_edge(tkey, target, "REFERENCES", {"line": f.line})

    # ---- interface method-set union (spec §5.1.1) ----
    iface_methods: dict[str, dict[str, tuple[list[str], list[str], int]]] = {}
    for tkey, gt in go_type_of.items():
        if gt.kind == "interface":
            iface_methods[tkey] = {m.name: (m.params, m.results, m.line)
                                   for m in gt.methods}
    iface_tainted: dict[str, bool] = {}
    iface_full: dict[str, dict[str, tuple[list[str], list[str]]]] = {}

    def _iface_closure(tkey: str, stack: set[str]) -> dict[str, tuple[list[str], list[str]]]:
        if tkey in iface_full:
            return iface_full[tkey]
        gt = go_type_of.get(tkey)
        full = {nm: (p, r) for nm, (p, r, _l) in iface_methods.get(tkey, {}).items()}
        if gt is not None and tkey not in stack:
            stack.add(tkey)
            for emb in gt.embeds:
                target = _resolve_type(emb.type_text, type_file[tkey])
                if target is None:
                    out.count_dropped_edge()  # external interface embed
                    iface_tainted[tkey] = True
                    continue
                if go_type_of.get(target) is None or \
                        go_type_of[target].kind != "interface":
                    continue  # invalid Go — silently skipped
                if (tkey, target) not in extends_seen:
                    extends_seen.add((tkey, target))
                    out.add_edge(tkey, target, "EXTENDS", {"kind": "declared"})
                for nm, pr in _iface_closure(target, stack).items():
                    full.setdefault(nm, pr)
            stack.discard(tkey)
        iface_full[tkey] = full
        return full

    for tkey, gt in go_type_of.items():
        if gt.kind != "interface":
            continue
        pkg, name = type_decl[tkey]
        full = _iface_closure(tkey, set())
        own = {m.name for m in gt.methods}
        # materialize the union as Method nodes on the embedding interface
        # so calls through it resolve by name (§3.1.4)
        for nm, (p, r) in full.items():
            if nm in own:
                continue
            mk = _fkey_method(pkg, name, nm, p)
            if not out.has_node(mk):
                out.add_node(mk, "Method",
                             _method_props(GoFunc(name=nm, params=p, names=[],
                                                  results=r, line=gt.line),
                                          owner=tkey))
        # type-set constraints: REFERENCES only, never heritage (§5 go column)
        for term, line in gt.typeset_terms:
            target = _resolve_type(term, type_file[tkey])
            if target is not None and target != tkey:
                out.add_edge(tkey, target, "REFERENCES",
                             {"line": line, "context": "type_constraint"})

    # ---- REFERENCES: signature param/result types (Method→Type) ----
    for pf in parsed:
        for gt in pf.types:
            if gt.kind != "interface":
                continue
            tkey = _tkey(pf.pkg, gt.name)
            for m in gt.methods:
                mk = _fkey_method(pf.pkg, gt.name, m.name, m.params)
                for text in m.params + m.results:
                    target = _resolve_type(text, pf)
                    if target is not None:
                        out.add_edge(mk, target, "REFERENCES", {"line": m.line})

    # ---- type method sets with promotion (spec §5.1.2) ----
    def _sig_of(params: list[str], results: list[str]) -> tuple:
        return (len(params), len(results),
                tuple(_simple_name(t) for t in params),
                tuple(_simple_name(t) for t in results))

    ms_cache: dict[str, tuple[dict, dict, bool]] = {}

    def _method_sets(tkey: str, stack: set[str]) -> tuple[dict, dict, bool]:
        """(M_value, M_ptr, tainted): name -> (params, results)."""
        if tkey in ms_cache:
            return ms_cache[tkey]
        if tkey in stack:
            return ({}, {}, False)
        stack.add(tkey)
        mv: dict[str, tuple[list[str], list[str]]] = {}
        mp: dict[str, tuple[list[str], list[str]]] = {}
        tainted = False
        gt = go_type_of.get(tkey)
        if gt is not None:
            if gt.kind == "interface":
                for nm, pr in _iface_closure(tkey, set()).items():
                    mv.setdefault(nm, pr)
                    mp.setdefault(nm, pr)
                tainted = iface_tainted.get(tkey, False)
            else:
                for nm, (_mk, p, r, pointer, _dpf) in methods_of.get(tkey, {}).items():
                    if not pointer:
                        mv.setdefault(nm, (p, r))
                    mp.setdefault(nm, (p, r))
                for f in gt.fields:
                    if f.name is not None:
                        continue
                    target = _resolve_type(f.type_text, type_file[tkey])
                    if target is None:
                        tainted = True  # external embed: promotion unknowable
                        continue
                    ev, ep, et = _method_sets(target, stack)
                    if f.pointer:
                        for nm, pr in {**ev, **ep}.items():
                            mv.setdefault(nm, pr)
                            mp.setdefault(nm, pr)
                    else:
                        for nm, pr in ev.items():
                            mv.setdefault(nm, pr)
                        for nm, pr in ep.items():
                            mp.setdefault(nm, pr)
                    tainted = tainted or et
        stack.discard(tkey)
        ms_cache[tkey] = (mv, mp, tainted)
        return mv, mp, tainted

    # ---- IMPLEMENTS via method-set matching (spec §5.1.3/.4) ----
    unverifiable = 0
    matchable = [tk for tk, gt in go_type_of.items()
                 if gt.kind == "interface" and not gt.typeset_terms]
    for tkey, gt in go_type_of.items():
        if gt.kind == "interface":
            continue  # interface-to-interface satisfaction is the EXTENDS embed
        mv, mp, tainted = _method_sets(tkey, set())
        for ikey in matchable:
            if ikey == tkey:
                continue
            full = _iface_closure(ikey, set())
            if not full:
                continue  # empty interface: matching everything is noise
            if iface_tainted.get(ikey, False):
                if all(nm in mp and _sig_of(*mp[nm]) == _sig_of(*full[nm])
                       for nm in full):
                    unverifiable += 1  # might demand more than we know
                continue
            cov_v = all(nm in mv and _sig_of(*mv[nm]) == _sig_of(*full[nm])
                        for nm in full)
            cov_p = all(nm in mp and _sig_of(*mp[nm]) == _sig_of(*full[nm])
                        for nm in full)
            if cov_v:
                out.add_edge(tkey, ikey, "IMPLEMENTS",
                             {"kind": "method-set", "receiver": "value"})
            elif cov_p:
                out.add_edge(tkey, ikey, "IMPLEMENTS",
                             {"kind": "method-set", "receiver": "pointer"})
            elif tainted:
                unverifiable += 1  # promotion might have covered it
    out.go_unverifiable_satisfaction = unverifiable

    # ---- IMPORTS: identifier-use matching (spec §5 go column) ----
    # rev3 counting ruling: an identifier used through an in-repo import
    # that NO file of that package declares is a name-resolution failure
    # inside the repo -> unresolved_refs +1 per missed identifier (no edge
    # for that use — there is no declaring file to land on).
    for pf in parsed:
        fk = f"file:{pf.rel}"
        seen: set[str] = set()
        for imp in pf.imports:
            if not imp.local:
                continue
            pkg = _resolve_pkg_path(imp.path)
            if pkg is None:
                continue
            used = pf.uses.get(imp.local, set())
            if not used:
                continue  # no identifier use -> no edge, not counted
            pkg_files = files_by_pkg.get(pkg, [])
            declared_all: set[str] = set()
            for tf in pkg_files:
                declared_all |= tf.declared
            for ident in sorted(used - declared_all):
                out.unresolved_refs += 1  # in-repo identifier-use miss
            for tf in pkg_files:
                if tf.rel == pf.rel or not (used & tf.declared):
                    continue
                tk = f"file:{tf.rel}"
                if tk not in seen and out.add_import(fk, tk):
                    seen.add(tk)

    # ---- method lookup for CALLS (own first, then promoted; interfaces
    # resolve through the expanded method set) ----
    def _lookup_method(tkey: str, name: str,
                       stack: frozenset[str] = frozenset()) -> tuple[str, list[str], list[str], GoFile] | None:
        """(method key, params, results, declaring file) or None."""
        if tkey in stack:
            return None
        stack = stack | {tkey}
        gt = go_type_of.get(tkey)
        if gt is not None and gt.kind == "interface":
            full = _iface_closure(tkey, set())
            if name in full:
                p, r = full[name]
                pkg, tname = type_decl[tkey]
                return (_fkey_method(pkg, tname, name, p), p, r,
                        type_file[tkey])
            return None
        own = methods_of.get(tkey, {})
        if name in own:
            mk, p, r, _ptr, dpf = own[name]
            return (mk, p, r, dpf)
        if gt is not None and gt.kind == "struct":
            for f in gt.fields:
                if f.name is not None:
                    continue
                target = _resolve_type(f.type_text, type_file[tkey])
                if target is None:
                    continue
                found = _lookup_method(target, name, stack)
                if found is not None:
                    return found
        return None

    # ---- CALLS / REFERENCES: walk every function + method body ----
    for pf in parsed:
        im = import_map[pf.rel]
        source = pf.source

        def _type_of_name(nm: str) -> str | None:
            return None  # replaced per-unit (closure over scope)

        def _walk_unit(src_key: str, scope: dict[str, str], body) -> None:
            def resolve_text(text: str) -> str | None:
                if text.startswith("type:"):
                    return text  # already resolved (cross-package inference)
                return _resolve_type(text, pf)

            def _type_of_name(nm: str) -> str | None:
                if nm in scope:
                    t = resolve_text(scope[nm])
                    return t
                if (pf.pkg, nm) in var_of:
                    return resolve_text(var_of[(pf.pkg, nm)])
                return type_of.get((pf.pkg, nm))

            def _chain_type(node) -> str | None:
                """a.b.c… as a receiver: identifier -> type, then field hops."""
                if node is None:
                    return None
                if node.type == "identifier":
                    return _type_of_name(_read(node, source))
                if node.type == "selector_expression":
                    head = _chain_type(node.child_by_field_name("operand"))
                    if head is None:
                        return None
                    gt = go_type_of.get(head)
                    if gt is None or gt.kind != "struct":
                        return None
                    fld = _read(node.child_by_field_name("field"), source)
                    for f in gt.fields:
                        if f.name == fld:
                            return resolve_text(f.type_text)
                    return None
                return None

            def _method_call(tkey: str, name: str, line: int) -> None:
                gt = go_type_of.get(tkey)
                if gt is not None and gt.kind == "interface":
                    found = _lookup_method(tkey, name)
                    if found is not None:
                        out.add_call(src_key, found[0], line,
                                     "virtual-dispatch")
                    else:
                        out.unresolved_refs += 1
                    return
                found = _lookup_method(tkey, name)
                if found is not None:
                    out.add_call(src_key, found[0], line, "direct")
                else:
                    out.unresolved_refs += 1

            def _result_texts_of(call) -> list[str] | None:
                """Resolved Type keys of a call target's results (the texts
                are written in the DECLARING file's package context — a bare
                `*Memory` in impl means nothing in app)."""
                fn = (call if call.type == "type_conversion_expression"
                      else call.child_by_field_name("function"))
                if fn is None:
                    return None
                if fn.type == "identifier":
                    name = _read(fn, source)
                    if name in _GO_PREDECLARED_FUNCS:
                        return None
                    f = func_of.get((pf.pkg, name))
                    if f is None:
                        return None
                    return [_resolve_type(t, f[2]) or "" for t in f[1]]
                if fn.type == "type_conversion_expression":
                    # `New[string](64)` — the grammar calls a generic function
                    # instantiation a conversion (see handle_call)
                    f = _generic_conversion_func(fn)
                    if f is None:
                        return None
                    return [_resolve_type(t, f[2]) or "" for t in f[1]]
                if fn.type != "selector_expression":
                    return None
                op = fn.child_by_field_name("operand")
                fld_node = fn.child_by_field_name("field")
                if (op is None or fld_node is None
                        or op.type != "identifier"):
                    return None
                nm = _read(op, source)
                fld = _read(fld_node, source)
                if nm in im:
                    pkg = im[nm]
                    if pkg is None:
                        return None
                    f = func_of.get((pkg, fld))
                    if f is None:
                        return None
                    return [_resolve_type(t, f[2]) or "" for t in f[1]]
                t = _type_of_name(nm)
                if t is None:
                    return None
                found = _lookup_method(t, fld)
                if found is None:
                    return None
                return [_resolve_type(tx, found[3]) or ""
                        for tx in found[2]]

            def _expr_type_text(v) -> str | None:
                if v.type == "unary_expression":
                    op = v.child_by_field_name("operand")
                    if op is not None:
                        v = op
                if v.type == "composite_literal":
                    tnode = v.child_by_field_name("type")
                    return _read(tnode, source) if tnode is not None else None
                if v.type == "call_expression":
                    texts = _result_texts_of(v)
                    if texts and len(texts) == 1:
                        return texts[0]
                return None

            def _generic_conversion_func(conv) -> tuple | None:
                """func_of entry when a type_conversion_expression is really a
                generic function instantiation (`New[string](64)`,
                `pkg.New[string](64)`), else None."""
                inner = next((c for c in conv.children if c.is_named), None)
                if inner is None or inner.type != "generic_type":
                    return None
                base_node = inner.child_by_field_name("type")
                if base_node is None:
                    return None
                base = _read(base_node, source)
                if "." in base:
                    pkg_local, name = base.split(".", 1)
                    pkg = im.get(pkg_local)
                    if pkg is None:
                        return None
                    return func_of.get((pkg, name))
                return func_of.get((pf.pkg, base))

            def handle_call(call) -> None:
                if call.type == "type_conversion_expression":
                    fn = call  # no "function" field on this shape
                else:
                    fn = call.child_by_field_name("function")
                if fn is None:
                    return
                line = call.start_point[0] + 1
                if fn.type == "identifier":
                    name = _read(fn, source)
                    if name in _GO_PREDECLARED_FUNCS:
                        return  # not a repo reference (§3.1.3)
                    f = func_of.get((pf.pkg, name))
                    if f is not None:
                        out.add_call(src_key, f[0], line, "direct")
                        return
                    t = type_of.get((pf.pkg, name))
                    if t is not None:  # conversion T(x)
                        out.add_edge(src_key, t, "REFERENCES", {"line": line})
                        return
                    out.unresolved_refs += 1
                    return
                if fn.type == "type_conversion_expression":
                    # A generic function instantiation parses as a
                    # conversion (`queue.New[string](64)`) — the grammar
                    # cannot tell them apart. Functions win; a genuine
                    # conversion to an in-source generic type REFERENCES it.
                    f = _generic_conversion_func(fn)
                    if f is not None:
                        out.add_call(src_key, f[0], line, "direct")
                        return
                    inner = next((c for c in fn.children if c.is_named), None)
                    if inner is not None and inner.type == "generic_type":
                        base_node = inner.child_by_field_name("type")
                        if base_node is not None:
                            base = _read(base_node, source)
                            if "." in base:
                                pkg_local, name = base.split(".", 1)
                                pkg = im.get(pkg_local)
                                if pkg is not None:
                                    t = type_of.get((pkg, name))
                                    if t is not None:
                                        out.add_edge(src_key, t, "REFERENCES",
                                                     {"line": line})
                                        return
                            elif base not in _GO_PREDECLARED_TYPES:
                                t = type_of.get((pf.pkg, base))
                                if t is not None:
                                    out.add_edge(src_key, t, "REFERENCES",
                                                 {"line": line})
                                    return
                    out.unresolved_refs += 1
                    return
                if fn.type != "selector_expression":
                    return  # computed callee — not nameable
                op = fn.child_by_field_name("operand")
                fld_node = fn.child_by_field_name("field")
                if op is None or fld_node is None:
                    return
                fld = _read(fld_node, source)
                if op.type == "identifier":
                    nm = _read(op, source)
                    if nm in im:  # package-qualified call pkg.Fn / pkg.T
                        pkg = im[nm]
                        if pkg is None:
                            out.unresolved_refs += 1  # external package
                            return
                        f = func_of.get((pkg, fld))
                        if f is not None:
                            out.add_call(src_key, f[0], line, "direct")
                            return
                        t = type_of.get((pkg, fld))
                        if t is not None:
                            out.add_edge(src_key, t, "REFERENCES",
                                         {"line": line})
                            return
                        out.unresolved_refs += 1
                        return
                    t = _type_of_name(nm)
                    if t is None:
                        out.unresolved_refs += 1
                        return
                    _method_call(t, fld, line)
                    return
                t = _chain_type(op)  # a.b.Method() — field-of receiver
                if t is None:
                    out.unresolved_refs += 1
                    return
                _method_call(t, fld, line)

            def visit(n) -> None:
                if n is None:
                    return
                t = n.type
                if t in ("call_expression", "type_conversion_expression"):
                    handle_call(n)
                elif t == "composite_literal":
                    tnode = n.child_by_field_name("type")
                    if tnode is not None:
                        target = resolve_text(_read(tnode, source))
                        if target is not None:
                            out.add_edge(src_key, target, "REFERENCES",
                                         {"line": n.start_point[0] + 1})
                elif t == "var_declaration":
                    for vs in n.named_children:
                        if vs.type != "var_spec":
                            continue
                        tn = vs.child_by_field_name("type")
                        if tn is None:
                            continue
                        text = _read(tn, source)
                        for c in vs.children:
                            if c.type == "identifier":
                                scope.setdefault(_read(c, source), text)
                elif t == "short_var_declaration":
                    left = n.child_by_field_name("left")
                    right = n.child_by_field_name("right")
                    if left is not None and right is not None:
                        names = [c for c in left.children
                                 if c.type == "identifier"]
                        values = [c for c in right.children if c.is_named]
                        if len(values) == 1 and len(names) > 1:
                            texts = _result_texts_of(values[0])
                            for nm_node, tx in zip(names, texts or []):
                                if tx:
                                    scope.setdefault(
                                        _read(nm_node, source), tx)
                        else:
                            for nm_node, v in zip(names, values):
                                tx = _expr_type_text(v)
                                if tx:
                                    scope.setdefault(
                                        _read(nm_node, source), tx)
                for c in n.children:
                    visit(c)

            visit(body)

        for f in pf.functions:
            src_key = _fkey_func(pf.pkg, f.name, f.params)
            scope = {nm: tx for nm, tx in zip(f.names, f.params) if nm}
            for text in f.params + f.results:
                target = _resolve_type(text, pf)
                if target is not None:
                    out.add_edge(src_key, target, "REFERENCES",
                                 {"line": f.line})
            if f.node is not None:
                _walk_unit(src_key, scope, f.node)

        for mr in pf.methods:
            src_key = _fkey_method(pf.pkg, mr.type_name, mr.func.name,
                                   mr.func.params)
            scope = {nm: tx
                     for nm, tx in zip(mr.func.names, mr.func.params) if nm}
            if mr.recv_name:
                scope[mr.recv_name] = ("*" if mr.pointer else "") + mr.type_name
            for text in mr.func.params + mr.func.results:
                target = _resolve_type(text, pf)
                if target is not None:
                    out.add_edge(src_key, target, "REFERENCES",
                                 {"line": mr.func.line})
            if mr.func.node is not None:
                _walk_unit(src_key, scope, mr.func.node)
