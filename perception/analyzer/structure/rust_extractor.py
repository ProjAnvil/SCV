"""tree-sitter Rust structure extraction (spec 2026-10-20 §4/§5/§6, §3.1).

Emits File/Type/Method nodes (``lang: "rust"``) plus CONTAINS / IMPLEMENTS /
CALLS / REFERENCES / IMPORTS edges into the shared
:class:`~structure.model.Graph_out`. Rust has no base classes -> EXTENDS is
never emitted (spec §5 rust column); IMPLEMENTS is fully explicit
(``impl Trait for Type``) and carries ``kind: "declared"`` — the most
reliable of the six languages.

Graphify-aligned two-pass design (spec §3.1), run as four stages:

1. crate/mod discovery + per-file parse: every ``Cargo.toml`` directory is a
   crate; the crate root is ``src/lib.rs`` (preferred) or ``src/main.rs``.
   ``mod foo;`` declarations are walked transitively to ``foo.rs`` /
   ``foo/mod.rs`` files; inline ``mod foo { .. }`` blocks are nested module
   paths inside the declaring file.
2. declaration emission: File/Type nodes + CONTAINS, trait member Method
   nodes (§3.1.4), free-function Method nodes.
3. ``use`` binding resolution against the module tree (crate name /
   ``crate`` / ``self`` / ``super`` heads, aliasing, ``{}``-groups) ->
   IMPORTS File->File + REFERENCES File->Type edges.
4. impl-block binding (IMPLEMENTS + method nodes on the owner Type) and the
   CALLS/REFERENCES body walks (``let`` inference included).

Honesty rules (v2 discipline): unresolvable callee/receiver -> ``unresolved_refs``;
constructible-but-unresolvable heritage -> ``dropped_edges``; macro
invocations (``vec!``, ``println!``, ...) are dropped and counted in
``dropped_edges`` per site; external crate paths (``std::...``,
``String::new``) count as ``unresolved_refs`` — the honest floor, never a
guess.

Grammar node-name pitfalls (tree-sitter-rust 0.24, probed):

- A method call is a plain ``call_expression`` whose function field is a
  ``field_expression`` — and its receiver field is named **``value``** (NOT
  ``object`` as in the go/jsts grammars); ``field`` holds the
  ``field_identifier``. ``self`` is its OWN node type (not ``identifier``)
  inside ``field_expression``/paths — check for it explicitly.
- Trait methods come in TWO shapes: bodyless ``function_signature_item``
  and default-bodied ``function_item`` — both must be collected.
- ``impl_item`` has fields ``trait`` (absent for inherent impls; a
  ``type_identifier`` or ``scoped_type_identifier``), ``type``, ``body``.
- ``use_declaration`` flattening: field ``argument`` -> ``scoped_identifier``
  / ``scoped_use_list`` (field ``path`` + a ``use_list`` child) /
  ``use_as_clause`` (fields ``path`` + ``alias``) / ``use_wildcard``. The
  ``crate`` and ``super`` path heads are keyword nodes — path segments are
  safest taken from the node TEXT split on ``::``.
- ``impl Trait`` in type position is ``abstract_type`` (field ``trait``);
  ``dyn Trait`` is ``dynamic_type``; both nest inside ``reference_type`` —
  the parameter's ``type`` field text (``&dyn Store``) is the anchor.
- ``let_declaration`` has fields ``pattern`` / ``value`` / ``type``
  (optional annotation); a ``let mut x`` pattern is still a bare
  ``identifier`` (``mutable_specifier`` is a sibling, and also a child of
  ``reference_expression`` — unwrap by field, not by first child).
- Generic function calls (``foo::<T>(x)``) wrap the callee in
  ``generic_function`` (field ``function``) — unwrap before resolving.
- ``struct`` literals are ``struct_expression`` with fields ``name`` and
  ``body`` (field_initializer_list) — the type comes from ``name``.

Conservative decisions (documented, spec-binding gaps):

- When both ``src/lib.rs`` and ``src/main.rs`` exist, lib.rs is THE crate
  root and main.rs is treated as a second root: its items attach to the
  crate-root namespace with an empty module prefix (possible key
  collisions are counted by Graph_out, never merged).
- ``.rs`` files not reachable from any root (orphan targets) attach to the
  crate root the same way.
- ``impl ExternalTrait for Type`` / ``impl Trait for ExternalType``: no
  IMPLEMENTS edge (one counted drop) and the impl's methods get no nodes
  (no owner Type node exists); their bodies are not walked.
- Glob ``use a::*;`` binds nothing: no IMPORTS edge, no count (the go
  blank-import precedent).
- A ``use`` resolving to an in-source Type emits a REFERENCES edge
  File->Type (spec §5 rust column "path-referenced types"; the other
  REFERENCES sources are field types Type->Type and signature types
  Method->Type).
- Return-type texts used for ``let`` inference are resolved in the
  DECLARING module's context (the go cross-package lesson).
"""
from __future__ import annotations

import os
import re
from dataclasses import dataclass, field
from pathlib import Path

import tree_sitter_rust
from tree_sitter import Language, Parser

from .langs import walk as walk_lang
from .model import Graph_out

_LANG = Language(tree_sitter_rust.language())
_PARSER = Parser(_LANG)

# directories never searched for Cargo.toml / module files (spec §4)
_SKIP_DIRS = {
    "node_modules", "__pycache__", "venv", ".venv", "env", "dist", "build",
    ".gradle", "target", "vendor", "testdata", "out", "test", "tests",
}

_CARGO_NAME_RE = re.compile(r'^\s*name\s*=\s*"([^"]+)"', re.M)
_WS_RE = re.compile(r"\s+")


def _read(node, source: bytes) -> str:
    return source[node.start_byte:node.end_byte].decode("utf-8", "replace")


def _norm(text: str) -> str:
    """Collapse whitespace runs to one space (key determinism)."""
    return _WS_RE.sub(" ", text).strip()


def _norm_crate_name(name: str) -> str:
    return name.strip().replace("-", "_")


def _strip_ref_dyn(t: str) -> str:
    """&T / &mut T / &'a T / *T / Box<T> / dyn T / impl T -> T (best effort)."""
    t = t.strip()
    for _ in range(3):
        changed = False
        m = re.match(r"^[&*](?:\s*(?:mut\s+|'\w+\s+))?(.*)$", t)
        if m:
            t, changed = m.group(1).strip(), True
        m = re.match(r"^Box<(.*)>$", t)
        if m:
            t, changed = m.group(1).strip(), True
        m = re.match(r"^(?:dyn|impl)\s+(.*)$", t)
        if m:
            t, changed = m.group(1).strip(), True
        if not changed:
            break
    return t


def _is_public(node) -> bool:
    return any(c.type == "visibility_modifier" for c in node.children)


# ---- keys ----

def _mod_prefix(crate: str, path: tuple[str, ...]) -> str:
    return "::".join((crate,) + path)


def _tkey(crate: str, path: tuple[str, ...], name: str) -> str:
    return f"type:{_mod_prefix(crate, path)}::{name}"


def _fkey_free(crate: str, path: tuple[str, ...], name: str,
               params: list[str]) -> str:
    return f"fn:{_mod_prefix(crate, path)}#{name}({','.join(params)})"


def _fkey_method(crate: str, path: tuple[str, ...], tname: str, name: str,
                 params: list[str]) -> str:
    return f"fn:{_mod_prefix(crate, path)}::{tname}#{name}({','.join(params)})"


# ---- pass-1 parse model ----

@dataclass
class RustUse:
    segments: tuple[str, ...]
    local: str
    line: int


@dataclass
class RustFn:
    name: str
    params: list[str]           # written type texts, self excluded
    names: list[str]            # param names, "" when unnamed
    results: list[str]          # written result type texts
    line: int
    static: bool                # True when the fn takes no self
    visibility: str
    node: object = None         # body node


@dataclass
class RustField:
    name: str
    type_text: str
    line: int


@dataclass
class RustType:
    name: str
    kind: str                   # struct | enum | trait | alias | union
    line: int
    visibility: str
    fields: list[RustField] = field(default_factory=list)
    methods: list[RustFn] = field(default_factory=list)  # trait signatures


@dataclass
class RustImpl:
    trait_text: str | None      # None = inherent impl
    type_text: str
    line: int
    methods: list[RustFn] = field(default_factory=list)


@dataclass
class RustModule:
    crate: str = ""
    path: tuple[str, ...] = ()  # global module path (set at mount time)
    file: "RustFile | None" = None
    rel: tuple[str, ...] = ()   # path relative to the file's mount point
    types: list[RustType] = field(default_factory=list)
    functions: list[RustFn] = field(default_factory=list)
    impls: list[RustImpl] = field(default_factory=list)
    uses: list[RustUse] = field(default_factory=list)
    mod_decls: list[str] = field(default_factory=list)  # `mod foo;` (files)
    macros: int = 0             # item-position macro invocations
    bindings: dict = field(default_factory=dict)       # filled in stage 3


@dataclass
class RustFile:
    rel: str                    # repo-relative path
    source: bytes = b""
    tree: object = None
    crate: str = ""
    modules: dict[tuple[str, ...], RustModule] = field(default_factory=dict)


def _fn_of(node, source: bytes, visibility: str) -> RustFn:
    params: list[str] = []
    names: list[str] = []
    static = True
    pnode = node.child_by_field_name("parameters")
    if pnode is not None:
        for c in pnode.children:
            if c.type == "self_parameter":
                static = False
            elif c.type == "parameter":
                tn = c.child_by_field_name("type")
                text = _norm(_read(tn, source)) if tn is not None else "?"
                pn = c.child_by_field_name("pattern")
                if pn is None or pn.type != "identifier":
                    pn = next((x for x in c.children
                               if x.type == "identifier"), None)
                params.append(text)
                names.append(_read(pn, source) if pn is not None else "")
    results: list[str] = []
    rnode = node.child_by_field_name("return_type")
    if rnode is not None:
        results.append(_norm(_read(rnode, source)))
    return RustFn(name=_read(node.child_by_field_name("name"), source),
                  params=params, names=names, results=results,
                  line=node.start_point[0] + 1, static=static,
                  visibility=visibility,
                  node=node.child_by_field_name("body"))


def _struct_fields(tnode, source: bytes) -> list[RustField]:
    fields: list[RustField] = []
    for lst in tnode.named_children:
        if lst.type != "field_declaration_list":
            continue
        for f in lst.children:
            if f.type != "field_declaration":
                continue
            fn = f.child_by_field_name("name")
            ftn = f.child_by_field_name("type")
            if fn is None or ftn is None:
                continue
            fields.append(RustField(name=_read(fn, source),
                                    type_text=_read(ftn, source),
                                    line=f.start_point[0] + 1))
    return fields


def _flatten_use(arg, source: bytes, prefix: tuple[str, ...],
                 acc: list[tuple[tuple[str, ...], str]]) -> None:
    """(segments, local-name) pairs from one use argument subtree."""
    if arg is None:
        return
    t = arg.type
    if t in ("identifier", "scoped_identifier", "scoped_type_identifier",
             "crate", "super"):
        segs = prefix + tuple(_norm(_read(arg, source)).split("::"))
        acc.append((segs, segs[-1]))
    elif t == "use_as_clause":
        inner = arg.child_by_field_name("path")
        alias = arg.child_by_field_name("alias")
        if inner is not None and alias is not None:
            segs = prefix + tuple(_norm(_read(inner, source)).split("::"))
            acc.append((segs, _read(alias, source)))
    elif t == "use_list":
        for c in arg.children:
            if c.is_named and c.type != "comment":
                _flatten_use(c, source, prefix, acc)
    elif t == "scoped_use_list":
        pnode = arg.child_by_field_name("path")
        pfx = (prefix + tuple(_norm(_read(pnode, source)).split("::"))
               if pnode is not None else prefix)
        for c in arg.children:
            if c.is_named and c.type != "comment":
                _flatten_use(c, source, pfx, acc)
    # use_wildcard: binds nothing (documented conservative decision)


def _collect(stmts, source: bytes, file: RustFile,
             rel: tuple[str, ...]) -> RustModule:
    mod = RustModule(file=file, rel=rel)
    for stmt in stmts:
        if not stmt.is_named or stmt.type == "comment":
            continue
        t = stmt.type
        if t in ("struct_item", "union_item"):
            kind = "struct" if t == "struct_item" else "union"
            mod.types.append(RustType(
                name=_read(stmt.child_by_field_name("name"), source),
                kind=kind, line=stmt.start_point[0] + 1,
                visibility="public" if _is_public(stmt) else "private",
                fields=_struct_fields(stmt, source)))
        elif t == "enum_item":
            mod.types.append(RustType(
                name=_read(stmt.child_by_field_name("name"), source),
                kind="enum", line=stmt.start_point[0] + 1,
                visibility="public" if _is_public(stmt) else "private"))
        elif t == "type_item":
            mod.types.append(RustType(
                name=_read(stmt.child_by_field_name("name"), source),
                kind="alias", line=stmt.start_point[0] + 1,
                visibility="public" if _is_public(stmt) else "private"))
        elif t == "trait_item":
            vis = "public" if _is_public(stmt) else "private"
            gt = RustType(
                name=_read(stmt.child_by_field_name("name"), source),
                kind="trait", line=stmt.start_point[0] + 1,
                visibility=vis)
            body = stmt.child_by_field_name("body")
            if body is not None:
                for c in body.children:
                    if c.type in ("function_signature_item",
                                  "function_item"):
                        gt.methods.append(_fn_of(c, source, vis))
                    elif c.type == "macro_invocation":
                        mod.macros += 1
            mod.types.append(gt)
        elif t == "function_item":
            mod.functions.append(
                _fn_of(stmt, source,
                       "public" if _is_public(stmt) else "private"))
        elif t == "impl_item":
            tnode = stmt.child_by_field_name("trait")
            ynode = stmt.child_by_field_name("type")
            impl = RustImpl(
                trait_text=(_read(tnode, source) if tnode is not None
                            else None),
                type_text=_read(ynode, source) if ynode is not None else "",
                line=stmt.start_point[0] + 1)
            body = stmt.child_by_field_name("body")
            if body is not None:
                for c in body.children:
                    if c.type == "function_item":
                        impl.methods.append(
                            _fn_of(c, source,
                                   "public" if _is_public(c) else "private"))
                    elif c.type == "macro_invocation":
                        mod.macros += 1
            mod.impls.append(impl)
        elif t == "use_declaration":
            acc: list[tuple[tuple[str, ...], str]] = []
            _flatten_use(stmt.child_by_field_name("argument"), source, (),
                         acc)
            for segs, local in acc:
                mod.uses.append(RustUse(
                    segments=segs, local=local,
                    line=stmt.start_point[0] + 1))
        elif t == "mod_item":
            name = _read(stmt.child_by_field_name("name"), source)
            body = stmt.child_by_field_name("body")
            if body is not None:  # inline module: nested path, same file
                file.modules[rel + (name,)] = _collect(
                    body.children, source, file, rel + (name,))
            else:                 # `mod foo;` -> sibling file (claimed later)
                mod.mod_decls.append(name)
        elif t == "macro_invocation":
            mod.macros += 1
        # static/const/extern/attribute items: no nodes (conservative)
    return mod


def _parse_file(path: Path, repo: Path) -> RustFile:
    source = path.read_bytes()
    root = _PARSER.parse(source).root_node
    rf = RustFile(rel=path.relative_to(repo).as_posix(), source=source,
                  tree=root)
    rf.modules[()] = _collect(root.children, source, rf, ())
    return rf


# ---- crate discovery ----

def _cargo_name(toml: Path, fallback: str) -> str:
    try:
        text = toml.read_text(encoding="utf-8", errors="replace")
    except OSError:
        return _norm_crate_name(fallback)
    m = _CARGO_NAME_RE.search(text)
    return _norm_crate_name(m.group(1) if m else fallback)


def _find_crates(repo: Path) -> dict[str, str]:
    """repo-relative crate dir -> crate name (one per Cargo.toml)."""
    crates: dict[str, str] = {}
    for dirpath, dirnames, filenames in os.walk(repo):
        dirnames[:] = [d for d in dirnames
                       if d not in _SKIP_DIRS and not d.startswith(".")]
        if "Cargo.toml" in filenames:
            rel = Path(dirpath).relative_to(repo)
            crates["" if rel == Path(".") else rel.as_posix()] = _cargo_name(
                Path(dirpath) / "Cargo.toml", Path(dirpath).name)
    return crates


def _crate_dir_of(f: Path, repo: Path, crates: dict[str, str]) -> str:
    parts = f.relative_to(repo).parts[:-1]
    for i in range(len(parts), -1, -1):
        cand = "/".join(parts[:i])
        if cand in crates:
            return cand
    return ""


def _mod_candidates(file: Path, rel: tuple[str, ...],
                    name: str) -> list[Path]:
    """`mod name;` declared inside module (file, rel) -> candidate files.

    Rust 2018 rule: a module's children live in the directory that would
    hold its mod.rs — dir(F) for mod.rs / lib.rs / main.rs, dir(F)/<stem>
    for a plain <stem>.rs file — plus the inline-module rel path.
    """
    if file.name in ("mod.rs", "lib.rs", "main.rs"):
        base = file.parent
    else:
        base = file.parent / file.stem
    if rel:
        base = base.joinpath(*rel)
    return [base / f"{name}.rs", base / name / "mod.rs"]


# ---- module index ----

class _Index:
    """crate name -> module path -> modules (root paths may hold 2+)."""

    def __init__(self) -> None:
        self.mods: dict[str, dict[tuple[str, ...], list[RustModule]]] = {}

    def add(self, mod: RustModule) -> None:
        self.mods.setdefault(mod.crate, {}).setdefault(
            mod.path, []).append(mod)

    def all_modules(self) -> list[RustModule]:
        out: list[RustModule] = []
        for crate in sorted(self.mods):
            for path in sorted(self.mods[crate]):
                out.extend(self.mods[crate][path])
        return out

    def crate_names(self) -> set[str]:
        return set(self.mods)

    def modules_at(self, crate: str,
                   path: tuple[str, ...]) -> list[RustModule]:
        return self.mods.get(crate, {}).get(path, [])

    def module_at(self, crate: str,
                  path: tuple[str, ...]) -> RustModule | None:
        mods = self.modules_at(crate, path)
        return mods[0] if mods else None

    def child_mods(self, crate: str, path: tuple[str, ...]) -> set[str]:
        n = len(path)
        return {p[-1] for p in self.mods.get(crate, {})
                if len(p) == n + 1 and p[:n] == path}

    def type_in(self, crate: str, path: tuple[str, ...],
                name: str) -> RustType | None:
        for m in self.modules_at(crate, path):
            for t in m.types:
                if t.name == name:
                    return t
        return None

    def fn_in(self, crate: str, path: tuple[str, ...],
              name: str) -> dict | None:
        for m in self.modules_at(crate, path):
            for f in m.functions:
                if f.name == name:
                    return {"key": _fkey_free(crate, path, f.name, f.params),
                            "name": f.name, "params": f.params,
                            "results": f.results, "crate": crate, "mod": path,
                            "file": m.file.rel if m.file else None}
        return None

    def file_of(self, crate: str, path: tuple[str, ...]) -> str | None:
        m = self.module_at(crate, path)
        return m.file.rel if m is not None and m.file is not None else None


# ---- resolution helpers shared by stages 3/4 ----

def _resolve_path(segs: tuple[str, ...], crate: str,
                  mod_path: tuple[str, ...], index: _Index) -> dict | None:
    """Path segments -> {kind: module|type|fn|assoc, ...} or None."""
    if not segs:
        return None
    i, cur_crate, cur_mod = 0, crate, mod_path
    head = segs[0]
    if head == "crate":
        cur_mod, i = (), 1
    elif head == "self":
        i = 1
    elif head == "super":
        while i < len(segs) and segs[i] == "super":
            if not cur_mod:
                return None
            cur_mod, i = cur_mod[:-1], i + 1
        if i >= len(segs):
            return {"kind": "module", "crate": cur_crate, "mod": cur_mod,
                    "file": index.file_of(cur_crate, cur_mod)}
    else:
        if head in index.child_mods(crate, mod_path):
            pass  # child module of the current one — walk below
        elif head == crate:
            cur_mod, i = (), 1
        elif head in index.crate_names() and head != crate:
            cur_crate, cur_mod, i = head, (), 1  # in-repo crate
        else:
            return None  # external crate / unknown head
    while i < len(segs) - 1:
        s = segs[i]
        if s in index.child_mods(cur_crate, cur_mod):
            cur_mod, i = cur_mod + (s,), i + 1
            continue
        if index.type_in(cur_crate, cur_mod, s) is not None:
            return {"kind": "assoc", "tkey": _tkey(cur_crate, cur_mod, s),
                    "names": tuple(segs[i + 1:])}
        return None
    if i >= len(segs):
        return {"kind": "module", "crate": cur_crate, "mod": cur_mod,
                "file": index.file_of(cur_crate, cur_mod)}
    last = segs[i]
    if last in index.child_mods(cur_crate, cur_mod):
        return {"kind": "module", "crate": cur_crate,
                "mod": cur_mod + (last,),
                "file": index.file_of(cur_crate, cur_mod + (last,))}
    if index.type_in(cur_crate, cur_mod, last) is not None:
        return {"kind": "type", "tkey": _tkey(cur_crate, cur_mod, last),
                "crate": cur_crate, "mod": cur_mod,
                "file": index.file_of(cur_crate, cur_mod)}
    rec = index.fn_in(cur_crate, cur_mod, last)
    if rec is not None:
        return {"kind": "fn", "rec": rec, "file": rec["file"]}
    return None


def _resolve_type_name(text: str, m: RustModule, index: _Index) -> str | None:
    """Written type text -> in-source Type key (or None)."""
    t = _strip_ref_dyn(_norm(text))
    if "<" in t:
        t = t.split("<", 1)[0].strip()
    if not t:
        return None
    if "::" in t:
        desc = _resolve_path(tuple(t.split("::")), m.crate, m.path, index)
        if desc is not None and desc["kind"] == "type":
            return desc["tkey"]
        return None
    if index.type_in(m.crate, m.path, t) is not None:
        return _tkey(m.crate, m.path, t)
    b = m.bindings.get(t)
    if b is not None and b["kind"] == "type":
        return b["tkey"]
    return None


# ---- extraction ----

def extract(repo: Path, out: Graph_out) -> list[RustFile]:
    """Four-stage extraction: parse, declarations, uses, impls + bodies."""
    rs_files = walk_lang(repo, "rust")
    if not rs_files:
        return []

    crates = _find_crates(repo)
    groups: dict[str, list[Path]] = {}
    for f in rs_files:
        groups.setdefault(_crate_dir_of(f, repo, crates), []).append(f)

    index = _Index()
    parsed: list[RustFile] = []
    by_path: dict[Path, RustFile] = {}

    def _parsed(path: Path) -> RustFile:
        rf = by_path.get(path)
        if rf is None:
            try:
                rf = _parse_file(path, repo)
            except OSError:
                rf = RustFile(rel=path.relative_to(repo).as_posix())
                rf.modules[()] = RustModule(file=rf)
            by_path[path] = rf
            parsed.append(rf)
        return rf

    for cdir in sorted(groups):
        crate_files = set(groups[cdir])
        crate = crates.get(cdir) or _norm_crate_name(
            Path(cdir).name if cdir else repo.name)
        roots: list[Path] = []
        for cand in ("src/lib.rs", "src/main.rs"):  # lib.rs preferred
            p = (repo / cdir / cand) if cdir else (repo / cand)
            if p in crate_files:
                roots.append(p)
        mount: dict[Path, tuple[str, ...]] = {}
        queue: list[tuple[tuple[str, ...], Path]] = [((), r) for r in roots]
        while queue:
            mpath, path = queue.pop(0)
            if path in mount:
                continue
            mount[path] = mpath
            rf = _parsed(path)
            for rel, mod in rf.modules.items():
                for decl in mod.mod_decls:
                    for cand_file in _mod_candidates(path, rel, decl):
                        if cand_file in crate_files and \
                                cand_file not in mount:
                            queue.append((mpath + rel + (decl,), cand_file))
                            break
        for f in crate_files:  # orphans: crate root, empty prefix
            if f not in mount:
                mount[f] = ()
                _parsed(f)
        for path in sorted(mount):
            rf = by_path.get(path)
            if rf is None:
                continue
            rf.crate = crate
            for rel, mod in rf.modules.items():
                mod.crate = crate
                mod.path = mount[path] + rel
                index.add(mod)

    modules = index.all_modules()
    tables = {"type_of": {}, "type_ctx": {}, "methods_of": {}, "func_of": {}}
    _emit_declarations(index, modules, out, tables)
    _resolve_bindings(index, modules, out)
    _emit_impls(index, modules, out, tables)
    _bind_bodies(index, modules, out, tables)
    return parsed


def _method_props(f: RustFn, owner: str) -> dict:
    return {"lang": "rust", "name": f.name, "visibility": f.visibility,
            "static": f.static, "params": ",".join(f.params),
            "returns": ",".join(f.results), "loc": 0, "line": f.line,
            "owner": owner}


# ---- stage 2: declarations ----

def _emit_declarations(index: _Index, modules: list[RustModule],
                        out: Graph_out, tables: dict) -> None:
    type_of = tables["type_of"]
    type_ctx = tables["type_ctx"]
    methods_of = tables["methods_of"]
    func_of = tables["func_of"]

    for m in modules:
        if m.file is None:
            continue
        loc = m.file.source.count(b"\n") + (
            0 if m.file.source.endswith(b"\n") or not m.file.source else 1)
        file_key = f"file:{m.file.rel}"
        if m.rel == ():  # one File node per physical file
            out.add_node(file_key, "File",
                         {"path": m.file.rel, "language": "rust",
                          "lang": "rust", "loc": loc})
        for _i in range(m.macros):
            out.count_dropped_edge()
        for t in m.types:
            tk = _tkey(m.crate, m.path, t.name)
            out.add_node(tk, "Type",
                         {"lang": "rust", "kind": t.kind, "modifiers": "",
                          "loc": 0, "line": t.line})
            out.add_edge(file_key, tk, "CONTAINS")
            type_of[tk] = t
            type_ctx[tk] = (m.crate, m.path)
            if t.kind == "trait":
                for f in t.methods:  # trait member nodes (§3.1.4)
                    mk = _fkey_method(m.crate, m.path, t.name, f.name,
                                      f.params)
                    out.add_node(mk, "Method", _method_props(f, owner=tk))
                    methods_of.setdefault(tk, {})[f.name] = {
                        "key": mk, "name": f.name, "params": f.params,
                        "results": f.results, "crate": m.crate,
                        "mod": m.path,
                        "file": m.file.rel}
        for f in m.functions:
            fk = _fkey_free(m.crate, m.path, f.name, f.params)
            out.add_node(fk, "Method", _method_props(f, owner=file_key))
            func_of[(m.crate, m.path, f.name)] = {
                "key": fk, "name": f.name, "params": f.params,
                "results": f.results, "crate": m.crate, "mod": m.path,
                "file": m.file.rel}


# ---- stage 3: use bindings ----

def _resolve_bindings(index: _Index, modules: list[RustModule],
                      out: Graph_out) -> None:
    for m in modules:
        if m.file is None:
            continue
        fk = f"file:{m.file.rel}"
        seen: set[str] = set()
        for u in m.uses:
            desc = _resolve_path(u.segments, m.crate, m.path, index)
            if desc is None:
                continue  # external crate — no edge, no count
            m.bindings[u.local] = desc
            if desc["kind"] == "type":
                out.add_edge(fk, desc["tkey"], "REFERENCES",
                             {"line": u.line})
            dfile = desc.get("file")
            if dfile and dfile != m.file.rel and dfile not in seen:
                if out.add_import(fk, f"file:{dfile}"):
                    seen.add(dfile)


# ---- stage 4a: impl blocks ----

def _emit_impls(index: _Index, modules: list[RustModule],
                out: Graph_out, tables: dict) -> None:
    type_of = tables["type_of"]
    type_ctx = tables["type_ctx"]
    methods_of = tables["methods_of"]

    for m in modules:
        for imp in m.impls:
            type_tkey = _resolve_type_name(imp.type_text, m, index)
            trait_tkey = (_resolve_type_name(imp.trait_text, m, index)
                          if imp.trait_text is not None else None)
            if imp.trait_text is not None:
                if trait_tkey is None or type_tkey is None:
                    out.count_dropped_edge()  # external side — honest drop
                    continue                  # no owner node -> no methods
                out.add_edge(type_tkey, trait_tkey, "IMPLEMENTS",
                             {"kind": "declared"})
            elif type_tkey is None:
                out.count_dropped_edge()
                continue
            tcrate, tmod = type_ctx[type_tkey]
            tname = type_of[type_tkey].name
            for f in imp.methods:
                mk = _fkey_method(tcrate, tmod, tname, f.name, f.params)
                out.add_node(mk, "Method", _method_props(f, owner=type_tkey))
                methods_of.setdefault(type_tkey, {})[f.name] = {
                    "key": mk, "name": f.name, "params": f.params,
                    "results": f.results, "crate": m.crate, "mod": m.path,
                    "file": m.file.rel if m.file else None}


# ---- stage 4b: REFERENCES + CALLS body walks ----

def _bind_bodies(index: _Index, modules: list[RustModule],
                 out: Graph_out, tables: dict) -> None:
    type_of = tables["type_of"]
    type_ctx = tables["type_ctx"]
    methods_of = tables["methods_of"]
    func_of = tables["func_of"]

    # REFERENCES: struct field types (Type->Type)
    for m in modules:
        for t in m.types:
            tk = _tkey(m.crate, m.path, t.name)
            for f in t.fields:
                target = _resolve_type_name(f.type_text, m, index)
                if target is not None and target != tk:
                    out.add_edge(tk, target, "REFERENCES", {"line": f.line})

    for m in modules:
        if m.file is None:
            continue
        source = m.file.source

        def _walk(src_key: str, f: RustFn, owner_tkey: str | None) -> None:
            scope: dict[str, str] = {
                nm: tx for nm, tx in zip(f.names, f.params) if nm}

            def _argc(args) -> int:
                if args is None:
                    return 0
                return sum(1 for c in args.named_children
                           if c.type != "comment")

            def _dispatch(tkey: str, name: str, argc: int, line: int) -> None:
                entry = methods_of.get(tkey, {}).get(name)
                if entry is None or len(entry["params"]) != argc:
                    out.unresolved_refs += 1
                    return
                gt = type_of.get(tkey)
                kind = ("virtual-dispatch"
                        if gt is not None and gt.kind == "trait"
                        else "direct")
                out.add_call(src_key, entry["key"], line, kind)

            def _call_fn(rec: dict, argc: int, line: int) -> None:
                if len(rec["params"]) == argc:
                    out.add_call(src_key, rec["key"], line, "direct")
                else:
                    out.unresolved_refs += 1

            def _recv_type(node) -> str | None:
                if node is None:
                    return None
                t = node.type
                if t == "self":  # `self` is its own node type, not identifier
                    return "self"
                if t == "identifier":
                    nm = _read(node, source)
                    if nm in scope:
                        v = scope[nm]
                        if v.startswith("type:"):
                            return v[5:]
                        return _resolve_type_name(v, m, index)
                    return None
                if t == "field_expression":
                    head = _recv_type(node.child_by_field_name("value"))
                    if head == "self":
                        head = owner_tkey
                    fld = node.child_by_field_name("field")
                    if head is None or fld is None:
                        return None
                    gt = type_of.get(head)
                    if gt is None:
                        return None
                    fname = _read(fld, source)
                    for f2 in gt.fields:
                        if f2.name == fname:
                            return _resolve_type_name(f2.type_text, m, index)
                    return None
                if t == "call_expression":
                    return _call_result(node)
                if t == "reference_expression":
                    return _recv_type(node.child_by_field_name("value"))
                if t == "parenthesized_expression":
                    return _recv_type(next(
                        (c for c in node.children if c.is_named), None))
                return None

            def _info_from_target(target: dict | None) -> tuple | None:
                if target is None:
                    return None
                if target["kind"] == "fn":
                    return ("fn", target["rec"])
                if target["kind"] == "assoc":
                    names = target["names"]
                    e = (methods_of.get(target["tkey"], {}).get(names[-1])
                         if names else None)
                    return ("method", e, target["tkey"]) if e else None
                return None

            def _callee_info(fn) -> tuple | None:
                if fn is None:
                    return None
                t = fn.type
                if t == "identifier":
                    nm = _read(fn, source)
                    b = m.bindings.get(nm)
                    if b is not None and b["kind"] == "fn":
                        return ("fn", b["rec"])
                    rec = func_of.get((m.crate, m.path, nm))
                    if rec is not None:
                        return ("fn", rec)
                    return None
                if t == "scoped_identifier":
                    segs = tuple(_norm(_read(fn, source)).split("::"))
                    if not segs:
                        return None
                    if segs[0] == "Self":
                        if owner_tkey and len(segs) >= 2:
                            e = methods_of.get(owner_tkey, {}).get(segs[-1])
                            return ("method", e, owner_tkey) if e else None
                        return None
                    if segs[0] in m.bindings:
                        b = m.bindings[segs[0]]
                        if b["kind"] == "type" and len(segs) >= 2:
                            e = methods_of.get(b["tkey"], {}).get(segs[-1])
                            return ("method", e, b["tkey"]) if e else None
                        if b["kind"] == "module" and len(segs) >= 2:
                            return _info_from_target(_resolve_path(
                                tuple(segs[1:]), b["crate"], b["mod"],
                                index))
                        return None
                    return _info_from_target(
                        _resolve_path(segs, m.crate, m.path, index))
                if t == "field_expression":
                    r = _recv_type(fn.child_by_field_name("value"))
                    if r == "self":
                        r = owner_tkey
                    if not r or r == "self":
                        return None
                    fld = fn.child_by_field_name("field")
                    if fld is None:
                        return None
                    e = methods_of.get(r, {}).get(_read(fld, source))
                    return ("method", e, r) if e else None
                if t == "generic_function":
                    return _callee_info(fn.child_by_field_name("function"))
                return None

            def _call_result(call) -> str | None:
                info = _callee_info(call.child_by_field_name("function"))
                if info is None:
                    return None
                _, rec, *_rest = info
                if rec is None or len(rec["results"]) != 1:
                    return None
                # result text resolves in the DECLARING module (go lesson)
                tm = index.module_at(rec["crate"], rec["mod"])
                if tm is None:
                    return None
                return _resolve_type_name(rec["results"][0], tm, index)

            def _value_type(v) -> str | None:
                if v is None:
                    return None
                t = v.type
                if t == "struct_expression":
                    nm = v.child_by_field_name("name")
                    if nm is None:
                        return None
                    k = _resolve_type_name(_read(nm, source), m, index)
                    return f"type:{k}" if k else None
                if t == "call_expression":
                    k = _call_result(v)
                    return f"type:{k}" if k else None
                if t == "field_expression":
                    k = _recv_type(v)
                    return f"type:{k}" if k else None
                if t == "reference_expression":
                    return _value_type(v.child_by_field_name("value"))
                if t == "parenthesized_expression":
                    return _value_type(next(
                        (c for c in v.children if c.is_named), None))
                return None

            def _handle_let(n) -> None:
                pat = n.child_by_field_name("pattern")
                tnode = n.child_by_field_name("type")
                vnode = n.child_by_field_name("value")
                if pat is None or pat.type != "identifier":
                    return  # tuple/ref patterns: no binding (conservative)
                nm = _read(pat, source)
                if tnode is not None:
                    scope[nm] = _read(tnode, source)
                elif vnode is not None:
                    v = _value_type(vnode)
                    if v is not None:
                        scope[nm] = v

            def _handle_callee(fn, argc: int, line: int) -> None:
                info = _callee_info(fn)
                if info is None:
                    out.unresolved_refs += 1
                    return
                if info[0] == "fn":
                    _call_fn(info[1], argc, line)
                else:
                    _kind, rec, tkey = info
                    if rec is None:
                        out.unresolved_refs += 1
                        return
                    _dispatch(tkey, rec["name"], argc, line)

            def visit(n) -> None:
                if n is None:
                    return
                t = n.type
                if t == "macro_invocation":
                    out.count_dropped_edge()  # vec![] et al — dropped+count
                    return                   # token_tree is not code
                if t == "call_expression":
                    fn = n.child_by_field_name("function")
                    if fn is not None and fn.type in (
                            "identifier", "scoped_identifier",
                            "field_expression", "generic_function"):
                        _handle_callee(fn, _argc(
                            n.child_by_field_name("arguments")),
                            n.start_point[0] + 1)
                elif t == "struct_expression":
                    nm = n.child_by_field_name("name")
                    if nm is not None:
                        k = _resolve_type_name(_read(nm, source), m, index)
                        if k is not None:
                            out.add_edge(src_key, k, "REFERENCES",
                                         {"line": n.start_point[0] + 1})
                elif t == "let_declaration":
                    _handle_let(n)
                for c in n.children:
                    visit(c)

            if f.node is not None:
                visit(f.node)

        def _sig_refs(src_key: str, f: RustFn) -> None:
            for text in f.params + f.results:
                target = _resolve_type_name(text, m, index)
                if target is not None:
                    out.add_edge(src_key, target, "REFERENCES",
                                 {"line": f.line})

        for f in m.functions:
            src_key = _fkey_free(m.crate, m.path, f.name, f.params)
            _sig_refs(src_key, f)
            _walk(src_key, f, None)

        for t in m.types:
            if t.kind != "trait":
                continue
            tk = _tkey(m.crate, m.path, t.name)
            for f in t.methods:  # default-bodied trait methods
                src_key = _fkey_method(m.crate, m.path, t.name, f.name,
                                       f.params)
                _sig_refs(src_key, f)
                _walk(src_key, f, tk)

        for imp in m.impls:
            type_tkey = _resolve_type_name(imp.type_text, m, index)
            trait_tkey = (_resolve_type_name(imp.trait_text, m, index)
                          if imp.trait_text is not None else None)
            if imp.trait_text is not None:
                if trait_tkey is None or type_tkey is None:
                    continue  # dropped+counted in _emit_impls
            elif type_tkey is None:
                continue
            tcrate, tmod = type_ctx[type_tkey]
            tname = type_of[type_tkey].name
            for f in imp.methods:
                src_key = _fkey_method(tcrate, tmod, tname, f.name, f.params)
                _sig_refs(src_key, f)
                _walk(src_key, f, type_tkey)
