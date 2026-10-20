"""tree-sitter-python structure extraction (spec 2026-10-20 §4/§5/§6, §3.1).

Emits File/Type/Method nodes (props carry ``lang: "python"``) plus
CONTAINS / EXTENDS / IMPLEMENTS / CALLS / REFERENCES / IMPORTS edges into
the shared :class:`~structure.model.Graph_out`.

Graphify-aligned two-pass design (spec §3.1):

- pass 1 — per-file parse: import tables, class tables (bases, methods),
  module-level functions; nodes and CONTAINS edges emitted immediately;
- pass 2 — cross-file binding: module map for IMPORTS, base resolution for
  EXTENDS/IMPLEMENTS, symbol tables (module functions, imported bindings,
  MRO closures) for CALLS/REFERENCES.

Honesty rules (v2 discipline): an unresolvable base/callee increments
``unresolved_refs`` (name resolution failure) or ``dropped_edges``
(constructible-but-ambiguous); bare builtin calls are filtered silently
BEFORE counting — ``len(x)`` is not a repo reference (spec §3.1.3, the
graphify ``append`` lesson).
"""
from __future__ import annotations

import os
from dataclasses import dataclass, field
from pathlib import Path

import tree_sitter_python
from tree_sitter import Language, Parser

from .langs import walk as walk_lang
from .model import Graph_out

_LANG = Language(tree_sitter_python.language())
_PARSER = Parser(_LANG)

# Python builtins — language-local, bare-identifier-scoped (spec §3.1.3).
# ``type``/``object``/``super`` included: a bare call of these names is never
# a repo symbol. Selector calls (``obj.len``) are unaffected — the filter
# only consults bare identifiers.
_PYTHON_BUILTINS = frozenset({
    "abs", "aiter", "all", "any", "ascii", "bin", "bool", "bytearray",
    "bytes", "callable", "chr", "classmethod", "compile", "complex",
    "delattr", "dict", "dir", "divmod", "enumerate", "eval", "exec",
    "filter", "float", "format", "frozenset", "getattr", "globals",
    "hasattr", "hash", "help", "hex", "id", "input", "int", "isinstance",
    "issubclass", "iter", "len", "list", "locals", "map", "max", "min",
    "next", "object", "oct", "open", "ord", "pow", "print", "property",
    "range", "repr", "reversed", "round", "set", "setattr", "slice",
    "sorted", "staticmethod", "str", "sum", "super", "tuple", "type",
    "vars", "zip",
})

# every node type that can appear as a positional parameter pattern
_PARAM_NODES = (
    "identifier", "default_parameter", "typed_parameter",
    "typed_default_parameter", "list_splat_pattern",
    "dictionary_splat_pattern",
)


def _read(node, source: bytes) -> str:
    return source[node.start_byte:node.end_byte].decode("utf-8", "replace")


# ---- pass-1 parse model ----

@dataclass
class PyImport:
    module: str                    # dotted target, '' for bare relative
    level: int                     # 0 absolute; N relative (dots)
    names: list[tuple[str, str]] = field(default_factory=list)
    # names: (imported simple name, local alias) — empty for plain `import x`
    from_import: bool = False      # `from M import ...` (names are members)


@dataclass
class PyMethod:
    name: str
    arity: int                     # declared params, self/cls excluded
    line: int
    node: object = None            # kept for the pass-2 body walk


@dataclass
class PyClass:
    name: str
    bases: list[str] = field(default_factory=list)   # base expression texts
    decorators: list[str] = field(default_factory=list)
    methods: list[PyMethod] = field(default_factory=list)
    line: int = 0


@dataclass
class PyFile:
    rel: str                       # repo-relative posix path
    module: str                    # dotted module path
    imports: list[PyImport] = field(default_factory=list)
    classes: list[PyClass] = field(default_factory=list)
    functions: list[PyMethod] = field(default_factory=list)
    tree: object = None
    source: bytes = b""


def _module_path(rel: str) -> str:
    """Dotted module path of a repo-relative posix path (spec §4).

    ``__init__.py`` maps to its package; a leading ``src.`` component is
    dropped when ``src`` is not itself a package (src-layout repos).
    """
    parts = rel.split("/")
    if parts[-1] == "__init__.py":
        parts = parts[:-1]
    else:
        parts[-1] = parts[-1][: -len(".py")]
    if parts and parts[0] == "src":
        parts = parts[1:]
    return ".".join(p for p in parts if p)


def _param_arity(params_node, source: bytes) -> tuple[int, bool]:
    """(arity, has_receiver): receiver = leading self/cls param."""
    names: list[str] = []
    for child in params_node.children:
        if child.type not in _PARAM_NODES:
            continue
        if child.type == "identifier":
            names.append(_read(child, source))
        else:
            inner = child.child_by_field_name("name")
            if inner is not None:
                names.append(_read(inner, source))
    has_recv = bool(names) and names[0] in ("self", "cls")
    arity = len(names) - (1 if has_recv else 0)
    return arity, has_recv


def _parse_file(path: Path, repo: Path) -> PyFile:
    source = path.read_bytes()
    tree = _PARSER.parse(source)
    root = tree.root_node
    rel = path.relative_to(repo).as_posix()
    pf = PyFile(rel=rel, module=_module_path(rel), tree=root, source=source)

    def _decorator_texts(decorated) -> list[str]:
        out: list[str] = []
        for dec in decorated.children:
            if dec.type == "decorator":
                out.append(_read(dec, source))
        return out

    def _function(defn) -> PyMethod:
        name_node = defn.child_by_field_name("name")
        params = defn.child_by_field_name("parameters")
        arity, _ = _param_arity(params, source) if params is not None else (0, False)
        return PyMethod(name=_read(name_node, source), arity=arity,
                        line=defn.start_point[0] + 1, node=defn)

    def _class(defn, decorators: list[str]) -> PyClass:
        name_node = defn.child_by_field_name("name")
        bases: list[str] = []
        supers = defn.child_by_field_name("superclasses")
        if supers is not None:
            for arg in supers.children:
                if arg.is_named:
                    bases.append(_read(arg, source))
        cls = PyClass(name=_read(name_node, source), bases=bases,
                      decorators=decorators, line=defn.start_point[0] + 1)
        body = defn.child_by_field_name("body")
        if body is not None:
            for stmt in body.children:
                inner = stmt
                stmt_decos: list[str] = []
                if stmt.type == "decorated_definition":
                    stmt_decos = _decorator_texts(stmt)
                    inner = stmt.child_by_field_name("definition")
                if inner is not None and inner.type == "function_definition":
                    cls.methods.append(_function(inner))
        return cls

    for stmt in root.children:
        inner = stmt
        decorators: list[str] = []
        if stmt.type == "decorated_definition":
            decorators = _decorator_texts(stmt)
            inner = stmt.child_by_field_name("definition")
        if inner is None:
            continue
        if inner.type == "class_definition":
            pf.classes.append(_class(inner, decorators))
        elif inner.type == "function_definition":
            pf.functions.append(_function(inner))
        elif inner.type == "import_from_statement":
            mod_node = inner.child_by_field_name("module_name")
            module, level = "", 0
            if mod_node is not None:
                text = _read(mod_node, source)
                if text.startswith("."):
                    level = len(text) - len(text.lstrip("."))
                    module = text.lstrip(".")
                else:
                    module = text
            names: list[tuple[str, str]] = []
            mod_id = mod_node.id if mod_node is not None else None
            for child in inner.children:
                if child.id == mod_id:
                    continue  # the module dotted_name is NOT an imported name
                if child.type == "dotted_name":
                    names.append((_read(child, source).split(".")[-1],
                                  _read(child, source).split(".")[-1]))
                elif child.type == "aliased_import":
                    nm = child.child_by_field_name("name")
                    al = child.child_by_field_name("alias")
                    names.append((_read(nm, source).split(".")[-1],
                                  _read(al, source)))
            pf.imports.append(PyImport(module=module, level=level, names=names,
                                       from_import=True))
        elif inner.type == "import_statement":
            for child in inner.children:
                if child.type == "dotted_name":
                    pf.imports.append(
                        PyImport(module=_read(child, source), level=0))
                elif child.type == "aliased_import":
                    nm = child.child_by_field_name("name")
                    al = child.child_by_field_name("alias")
                    pf.imports.append(
                        PyImport(module=_read(nm, source), level=0,
                                 names=[(_read(nm, source).split(".")[-1],
                                         _read(al, source))]))
    return pf


def extract(repo: Path, out: Graph_out) -> list[PyFile]:
    """Two-pass extraction: nodes + CONTAINS now, everything else after."""
    files = [p for p in walk_lang(repo, "python")]

    parsed: list[PyFile] = []
    for path in files:
        try:
            parsed.append(_parse_file(path, repo))
        except OSError:
            continue

    # ---- pass 1: nodes ----
    loc_total = 0
    for pf in parsed:
        loc = pf.source.count(b"\n") + (0 if pf.source.endswith(b"\n") or
                                        not pf.source else 1)
        file_key = f"file:{pf.rel}"
        out.add_node(file_key, "File",
                     {"path": pf.rel, "language": "python", "lang": "python",
                      "loc": loc})
        for cls in pf.classes:
            tkey = f"type:{pf.module}.{cls.name}"
            out.add_node(tkey, "Type",
                         {"lang": "python", "kind": "class", "modifiers": "",
                          "loc": 0, "line": cls.line,
                          "decorators": cls.decorators})
            out.add_edge(file_key, tkey, "CONTAINS")
            for m in cls.methods:
                out.add_node(f"fn:{pf.module}.{cls.name}#{m.name}({m.arity})",
                             "Method",
                             {"lang": "python", "name": m.name,
                              "visibility": "private" if m.name.startswith("_")
                              else "public", "static": False, "params": m.arity,
                              "returns": "", "loc": 0, "line": m.line,
                              "owner": tkey})
        for fn in pf.functions:
            out.add_node(f"fn:{pf.module}#{fn.name}({fn.arity})", "Method",
                         {"lang": "python", "name": fn.name,
                          "visibility": "private" if fn.name.startswith("_")
                          else "public", "static": False, "params": fn.arity,
                          "returns": "", "loc": 0, "line": fn.line,
                          "owner": file_key})

    _bind(repo, parsed, out)
    return parsed


# ---- pass 2: cross-file binding ----

def _package_of(pf: PyFile) -> list[str]:
    """Package components of a file: itself when an __init__, else parent."""
    if pf.rel.endswith("__init__.py"):
        return pf.module.split(".") if pf.module else []
    parts = pf.module.split(".") if pf.module else []
    return parts[:-1] if len(parts) > 1 else []


def _resolve_relative(module: str, level: int, pf: PyFile) -> str:
    """Target dotted module of a relative import (level >= 1).

    Level 1 = the file's own package; each extra dot climbs one parent.
    An ``__init__`` module IS its package, so level 1 keeps it whole.
    """
    pkg = _package_of(pf)
    keep = max(len(pkg) - (level - 1), 0)
    parts = pkg[:keep]
    if module:
        parts = parts + module.split(".")
    return ".".join(parts)


def _bind(repo: Path, parsed: list[PyFile], out: Graph_out) -> None:
    module_map = {pf.module: pf for pf in parsed}
    file_of_module = {pf.module: f"file:{pf.rel}" for pf in parsed}

    # symbol tables: simple name -> (kind, key) per module + per-file imports
    mod_functions: dict[str, dict[str, tuple[int, str]]] = {}
    mod_classes: dict[str, dict[str, str]] = {}
    for pf in parsed:
        fns: dict[str, tuple[int, str]] = {}
        for fn in pf.functions:
            fns[fn.name] = (fn.arity, f"fn:{pf.module}#{fn.name}({fn.arity})")
        mod_functions[pf.module] = fns
        cls: dict[str, str] = {}
        for c in pf.classes:
            cls[c.name] = f"type:{pf.module}.{c.name}"
        mod_classes[pf.module] = cls

    # imported bindings per file: local name -> ("fn", arity, key) | ("cls", key)
    imports_of: dict[str, dict[str, tuple]] = {}
    for pf in parsed:
        bindings: dict[str, tuple] = {}
        for imp in pf.imports:
            if not imp.from_import:
                continue  # plain `import x` binds the module itself
            target = imp.module
            if imp.level:
                target = _resolve_relative(imp.module, imp.level, pf)
            for simple, alias in imp.names:
                local = alias or simple
                tf = mod_functions.get(target, {})
                if simple in tf:
                    bindings[local] = ("fn",) + tf[simple]
                    continue
                tc = mod_classes.get(target, {})
                if simple in tc:
                    bindings[local] = ("cls", tc[simple])
        imports_of[pf.rel] = bindings

    # IMPORTS edges + rev3 counting ruling: an in-repo from-import of a
    # name the target module does not declare (function/class/submodule) is
    # a name-resolution failure inside the repo -> unresolved_refs +1 per
    # name. The module-level edge itself stays: the file dependency is real
    # (the task's ruling summary counts, it does not retract edges).
    for pf in parsed:
        file_key = f"file:{pf.rel}"
        seen: set[str] = set()
        for imp in pf.imports:
            target = imp.module
            if imp.level:
                target = _resolve_relative(imp.module, imp.level, pf)
            tgt_file = file_of_module.get(target)
            if tgt_file and tgt_file != file_key and tgt_file not in seen:
                if out.add_import(file_key, tgt_file):
                    seen.add(tgt_file)
            if not imp.from_import:
                continue  # `import x` / `import x as y`: module binding only
            tf = mod_functions.get(target, {})
            tc = mod_classes.get(target, {})
            for simple, _alias in imp.names:
                if (simple in tf or simple in tc
                        or f"{target}.{simple}" in file_of_module):
                    continue  # binds: member, or a submodule of the target
                out.unresolved_refs += 1  # in-repo target, unknown name

    # base resolution + MRO closure
    def _resolve_base(pf: PyFile, base_text: str) -> str | None:
        name = base_text.split(".")[-1].strip()
        if not name or name in _PYTHON_BUILTINS:
            return None
        own = mod_classes.get(pf.module, {})
        if name in own:
            return own[name]
        b = imports_of.get(pf.rel, {}).get(name)
        if b and b[0] == "cls":
            return b[1]
        return None

    mro: dict[str, list[str]] = {}

    def _closure(tkey: str, seen: set[str] | None = None) -> list[str]:
        if tkey in mro:
            return mro[tkey]
        seen = seen or set()
        if tkey in seen:
            return [tkey]
        seen.add(tkey)
        chain = [tkey]
        for parent in _base_parents.get(tkey, ()):
            for p in _closure(parent, seen):
                if p not in chain:
                    chain.append(p)
        mro[tkey] = chain
        return chain

    _base_parents: dict[str, list[str]] = {}
    for pf in parsed:
        for cls in pf.classes:
            tkey = f"type:{pf.module}.{cls.name}"
            parents: list[str] = []
            for i, base in enumerate(cls.bases):
                resolved = _resolve_base(pf, base)
                if resolved is None:
                    out.count_dropped_edge()  # base not in-source (spec §4)
                    continue
                parents.append(resolved)
                label = "EXTENDS" if i == 0 else "IMPLEMENTS"
                out.add_edge(tkey, resolved, label, {"kind": "declared"})
            _base_parents[tkey] = parents
    for tkey in list(_base_parents):
        _closure(tkey)

    # method tables per type key: name -> (arity, method key)
    type_methods: dict[str, dict[str, tuple[int, str]]] = {}
    for pf in parsed:
        for cls in pf.classes:
            tkey = f"type:{pf.module}.{cls.name}"
            tbl: dict[str, tuple[int, str]] = {}
            for m in cls.methods:
                tbl[m.name] = (m.arity,
                               f"fn:{pf.module}.{cls.name}#{m.name}({m.arity})")
            type_methods[tkey] = tbl

    # CALLS / REFERENCES — walk every method body
    for pf in parsed:
        bindings = imports_of.get(pf.rel, {})
        own_fns = mod_functions.get(pf.module, {})
        own_cls = mod_classes.get(pf.module, {})

        def _owner_of(fn: PyMethod, cls: PyClass | None) -> tuple[str, list[str]]:
            if cls is not None:
                tkey = f"type:{pf.module}.{cls.name}"
                return f"fn:{pf.module}.{cls.name}#{fn.name}({fn.arity})", _closure(tkey)
            return f"fn:{pf.module}#{fn.name}({fn.arity})", []

        def _call_arity(call_node) -> int:
            args = call_node.child_by_field_name("arguments")
            if args is None:
                return 0
            return sum(1 for c in args.children
                       if c.is_named and c.type != "keyword_argument")

        def _emit_call(src: str, dst: str, line: int) -> None:
            out.add_call(src, dst, line, "direct")

        def _resolve_call(call_node, src_key: str, mro_chain: list[str]) -> None:
            fn_node = call_node.child_by_field_name("function")
            if fn_node is None:
                return
            line = call_node.start_point[0] + 1
            if fn_node.type == "attribute":
                obj_node = fn_node.child_by_field_name("object")
                attr_node = fn_node.child_by_field_name("attribute")
                if obj_node is None or attr_node is None:
                    return
                obj = _read(obj_node, pf.source)
                attr = _read(attr_node, pf.source)
                arity = _call_arity(call_node)
                if obj in ("self", "cls"):
                    for tkey in mro_chain:
                        tbl = type_methods.get(tkey, {})
                        if attr in tbl:
                            _emit_call(src_key, tbl[attr][1], line)
                            return
                    out.unresolved_refs += 1
                    return
                # ClassName.method() — in-source class
                target_cls = own_cls.get(obj) or (
                    bindings.get(obj, (None,))[1] if obj in bindings and
                    bindings[obj][0] == "cls" else None)
                if target_cls:
                    tbl = type_methods.get(target_cls, {})
                    if attr in tbl:
                        _emit_call(src_key, tbl[attr][1], line)
                        return
                    out.unresolved_refs += 1
                    return
                # module_alias.fn() — imported module or same module
                target_mod = None
                for imp in pf.imports:
                    mod = imp.module if not imp.level else _resolve_relative(
                        imp.module, imp.level, pf)
                    tail = mod.split(".")[-1] if mod else ""
                    if tail == obj:
                        target_mod = mod
                        break
                if target_mod is None and obj == pf.module.split(".")[-1]:
                    target_mod = pf.module
                if target_mod is not None:
                    tbl = mod_functions.get(target_mod, {})
                    if attr in tbl:
                        _emit_call(src_key, tbl[attr][1], line)
                        return
                out.unresolved_refs += 1
                return
            if fn_node.type == "identifier":
                name = _read(fn_node, pf.source)
                if name in _PYTHON_BUILTINS:
                    return  # not a repo reference (spec §3.1.3)
                arity = _call_arity(call_node)
                if name in own_fns and own_fns[name][0] == arity:
                    _emit_call(src_key, own_fns[name][1], line)
                    return
                b = bindings.get(name)
                if b and b[0] == "fn" and b[1] == arity:
                    _emit_call(src_key, b[2], line)
                    return
                if b and b[0] == "cls":
                    out.add_edge(src_key, b[1], "REFERENCES", {"line": line})
                    return
                if name in own_cls:
                    out.add_edge(src_key, own_cls[name], "REFERENCES",
                                 {"line": line})
                    return
                out.unresolved_refs += 1

        def _walk_body(node, src_key: str, mro_chain: list[str]) -> None:
            if node is None:
                return
            if node.type == "call":
                _resolve_call(node, src_key, mro_chain)
            for child in node.children:
                _walk_body(child, src_key, mro_chain)

        for cls in pf.classes:
            for m in cls.methods:
                src, chain = _owner_of(m, cls)
                _walk_body(m.node, src, chain)
        for fn in pf.functions:
            src, _ = _owner_of(fn, None)
            _walk_body(fn.node, src, [])
