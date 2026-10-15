"""tree-sitter-java structure extraction (spec 2026-10-14 §3/§4).

Emits File/Type/Method nodes plus CONTAINS/IMPLEMENTS/EXTENDS/CALLS/
REFERENCES edges into a :class:`~structure.model.Graph_out`. FQN resolution
is purely syntactic: explicit single-type imports, then wildcard imports
matched against known source simple names, then the same package — anything
else is dropped and counted, never dangled.

CALLS/REFERENCES come from a second pass over the retained parse trees:
receiver resolution is scope-based (params, locals from declarators, fields
of the owner's in-source supertype closure, implicit ``this``), the target
method is matched by receiver-declared-type FQN + name + arity (multi-match
→ drop+count), and the dispatch kind follows the receiver's declared type
(``virtual-dispatch`` on interfaces, else ``direct``).
"""

from __future__ import annotations

import os
from collections import deque
from dataclasses import dataclass, field
from pathlib import Path

import tree_sitter_java
from tree_sitter import Language, Parser

_LANG = Language(tree_sitter_java.language())
_PARSER = Parser(_LANG)

# ---- source-root discovery (spec §3, mirrors perceive.py _iter_source_roots) ----

# module directories count within depth 3 of the repo root, so a source root
# lives at most at depth 6 (3 module levels + src/main/java)
_MAX_MODULE_DEPTH = 3
_SOURCE_SUFFIX = ("src", "main", "java")
# never descend into build outputs, caches, or dot-directories
_EXCLUDED_DIRS = {"build", ".gradle", "target"}


def _has_src_test(parts: tuple[str, ...]) -> bool:
    return any(parts[i] == "src" and parts[i + 1] == "test"
               for i in range(len(parts) - 1))


def discover_source_roots(repo: Path) -> list[Path]:
    """Every ``src/main/java`` directory of a module at depth <= 3.

    Excludes ``build/``, ``.gradle/``, ``target/``, ``src/test/`` trees and
    dot-directories (independent re-implementation of perceive.py's walk).
    """
    roots: list[Path] = []
    for dirpath, dirnames, _filenames in os.walk(repo):
        parts = Path(dirpath).relative_to(repo).parts
        if len(parts) >= _MAX_MODULE_DEPTH + len(_SOURCE_SUFFIX):
            dirnames[:] = []
        else:
            dirnames[:] = [d for d in dirnames
                           if d not in _EXCLUDED_DIRS and not d.startswith(".")]
        if (len(parts) >= len(_SOURCE_SUFFIX)
                and parts[-3:] == _SOURCE_SUFFIX
                and not _has_src_test(parts)):
            roots.append(Path(dirpath))
            dirnames[:] = []  # do not look for nested roots inside a root
    return roots


# ---- parse model ----

@dataclass
class ImportDecl:
    name: str        # dotted target; package prefix for wildcard imports
    wildcard: bool
    static: bool


@dataclass
class MethodDecl:
    name: str
    params: list[str] = field(default_factory=list)   # normalized type names
    returns: str = ""
    visibility: str = "package"
    static: bool = False
    line: int = 0


@dataclass
class FieldDecl:
    name: str
    type_name: str   # as written, type arguments erased (declared_type_name)
    line: int = 0


@dataclass
class TypeDecl:
    name: str                 # simple name
    kind: str                 # class/interface/record/enum/annotation
    modifiers: list[str] = field(default_factory=list)
    line: int = 0
    extends_names: list[str] = field(default_factory=list)  # as written -> EXTENDS
    interfaces: list[str] = field(default_factory=list)     # as written -> IMPLEMENTS
    methods: list[MethodDecl] = field(default_factory=list)
    fields: list[FieldDecl] = field(default_factory=list)
    nested: list["TypeDecl"] = field(default_factory=list)


@dataclass
class JavaFile:
    path: Path
    package: str
    imports: list[ImportDecl] = field(default_factory=list)
    types: list[TypeDecl] = field(default_factory=list)
    loc: int = 0
    tree: object | None = None   # retained parse tree (second pass bodies)


_TYPE_KINDS = {
    "class_declaration": "class",
    "interface_declaration": "interface",
    "record_declaration": "record",
    "enum_declaration": "enum",
    "annotation_type_declaration": "annotation",
}

_ANNOTATION_NODES = {"marker_annotation", "annotation"}


def _text(node) -> str:
    return node.text.decode("utf-8", errors="replace")


def _line(node) -> int:
    return node.start_point[0] + 1


def _modifiers_of(decl):
    """tree-sitter-java attaches ``modifiers`` as a bare child, not a field."""
    for child in decl.children:
        if child.type == "modifiers":
            return child
    return None


def _modifier_keywords(modifiers_node) -> list[str]:
    if modifiers_node is None:
        return []
    kws = []
    for child in modifiers_node.children:
        if child.type in _ANNOTATION_NODES:
            continue  # annotations are not modifiers (T3 reads them itself)
        kws.append(_text(child))
    return kws


def norm_type(node) -> str:
    """Normalize a type node to the key form: generics erased to the raw
    simple name, arrays/varargs as ``[]`` per dimension (spec §4)."""
    if node is None:
        return ""
    t = node.type
    if t == "array_type":
        element = node.child_by_field_name("element")
        dims = node.child_by_field_name("dimensions")
        count = _text(dims).count("[") if dims is not None else 1
        return norm_type(element) + "[]" * count
    if t == "generic_type":
        # first child is the raw base (type_identifier / scoped_type_identifier)
        return norm_type(node.children[0]) if node.children else _text(node)
    if t == "scoped_type_identifier":
        return _text(node).rsplit(".", 1)[-1]
    return _text(node)  # type_identifier, void_type, integral_type, boolean_type...


def declared_type_name(node) -> str:
    """Declared-type text for name RESOLUTION (supertypes, receivers): type
    arguments erased like :func:`norm_type`, but qualification preserved —
    ``a.b.Bar<X>`` stays ``a.b.Bar`` so ``resolve_type_name`` can take the
    fully-qualified form as written."""
    if node is None:
        return ""
    if node.type == "generic_type":
        base = node.children[0] if node.children else node
        return _text(base)
    return _text(node)


def _norm_param(param_node) -> str:
    if param_node.type == "spread_parameter":  # varargs -> T[]
        for child in param_node.children:
            if child.type.endswith("_type") or child.type in (
                    "type_identifier", "scoped_type_identifier", "generic_type"):
                return norm_type(child) + "[]"
        return ""
    return norm_type(param_node.child_by_field_name("type"))


def _params(formal_parameters) -> list[str]:
    if formal_parameters is None:
        return []
    return [_norm_param(c) for c in formal_parameters.children
            if c.type in ("formal_parameter", "spread_parameter")]


def _visibility(kws: list[str]) -> str:
    for vis in ("public", "private", "protected"):
        if vis in kws:
            return vis
    return "package"


def _method(decl) -> MethodDecl:
    kws = _modifier_keywords(_modifiers_of(decl))
    if decl.type == "annotation_type_element_declaration":
        # @interface members: bare children, no parameter field
        name, returns = "", ""
        for child in decl.children:
            if child.type == "identifier" and not name:
                name = _text(child)
            elif child.type.endswith("_type") or child.type in (
                    "type_identifier", "scoped_type_identifier", "generic_type"):
                returns = returns or norm_type(child)
        return MethodDecl(name=name, params=[], returns=returns,
                          visibility=_visibility(kws), static="static" in kws,
                          line=_line(decl))
    params = _params(decl.child_by_field_name("parameters"))
    if decl.type in ("constructor_declaration", "compact_constructor_declaration"):
        name = _text(decl.child_by_field_name("name"))
        returns = name  # constructor: returns the owner simple name
    else:
        name = _text(decl.child_by_field_name("name"))
        returns = norm_type(decl.child_by_field_name("type"))
    return MethodDecl(name=name, params=params, returns=returns,
                      visibility=_visibility(kws), static="static" in kws,
                      line=_line(decl))


def _type_list_texts(node) -> list[str]:
    """Names from a super_interfaces / extends_interfaces clause: type
    arguments erased so the simple name resolves (T1 review fix)."""
    if node is None:
        return []
    for child in node.children:
        if child.type == "type_list":
            node = child
            break
    return [declared_type_name(c) for c in node.children if c.type in (
        "type_identifier", "scoped_type_identifier", "generic_type")]


def _is_type_node(node) -> bool:
    """True for any node usable in a declared-type position."""
    return (node.type.endswith("_type")
            or node.type in ("type_identifier", "scoped_type_identifier",
                             "generic_type"))


def _field_decls(member) -> list[FieldDecl]:
    type_name = ""
    for child in member.children:
        if _is_type_node(child):
            type_name = declared_type_name(child)
            break
    decls: list[FieldDecl] = []
    for child in member.children:
        if child.type == "variable_declarator":
            name_node = child.child_by_field_name("name")
            if name_node is not None:
                decls.append(FieldDecl(name=_text(name_node), type_name=type_name,
                                       line=_line(child)))
    return decls


_BODY_NODES = ("class_body", "interface_body", "enum_body", "annotation_type_body",
               "enum_body_declarations")


def _member_bodies(decl):
    """Every body container of a type declaration — enum members live one
    level deeper under ``enum_body_declarations``."""
    for child in decl.children:
        if child.type in _BODY_NODES:
            yield child
            yield from _member_bodies(child)


_METHOD_MEMBER_TYPES = ("method_declaration", "constructor_declaration",
                        "compact_constructor_declaration",
                        "annotation_type_element_declaration")


def _parse_type(decl) -> TypeDecl:
    kind = _TYPE_KINDS[decl.type]
    name = _text(decl.child_by_field_name("name"))
    td = TypeDecl(name=name, kind=kind,
                  modifiers=_modifier_keywords(_modifiers_of(decl)),
                  line=_line(decl))
    # class extends -> EXTENDS; class/record/enum implements -> IMPLEMENTS;
    # interfaces extend via the bare extends_interfaces child -> EXTENDS.
    sup = decl.child_by_field_name("superclass")
    if sup is not None:
        for child in sup.children:
            if child.type in ("type_identifier", "scoped_type_identifier", "generic_type"):
                td.extends_names.append(declared_type_name(child))
                break
    iface_field = decl.child_by_field_name("interfaces")
    if iface_field is not None:
        td.interfaces = _type_list_texts(iface_field)
    else:
        for child in decl.children:  # interface_declaration: bare child
            if child.type == "extends_interfaces":
                td.extends_names.extend(_type_list_texts(child))
                break
    if decl.type == "record_declaration":  # components are implicit fields
        params = decl.child_by_field_name("parameters")
        if params is not None:
            for child in params.children:
                if child.type != "formal_parameter":
                    continue
                type_node = child.child_by_field_name("type")
                name_node = child.child_by_field_name("name")
                if type_node is not None and name_node is not None:
                    td.fields.append(FieldDecl(
                        name=_text(name_node),
                        type_name=declared_type_name(type_node), line=_line(child)))
    for body in _member_bodies(decl):
        _parse_members(body, td)
    return td


def _parse_members(body, owner: TypeDecl) -> None:
    """Direct declarations only: anonymous-class bodies are skipped (their
    members have no own FQN in the syntactic model)."""
    for child in body.children:
        if child.type in _TYPE_KINDS:
            owner.nested.append(_parse_type(child))
        elif child.type in _METHOD_MEMBER_TYPES:
            owner.methods.append(_method(child))
        elif child.type == "field_declaration":
            owner.fields.extend(_field_decls(child))
        elif child.type == "enum_constant":
            # an enum constant is an implicit static field of the enum type
            name_node = child.child_by_field_name("name")
            if name_node is not None:
                owner.fields.append(FieldDecl(name=_text(name_node),
                                              type_name=owner.name,
                                              line=_line(child)))


def parse_java_file(path: Path) -> JavaFile:
    """Parse one .java file into package/imports/type declarations."""
    source = path.read_bytes()
    jf = JavaFile(path=path, package="",
                  loc=len(source.decode("utf-8", errors="replace").splitlines()))
    tree = _PARSER.parse(source)
    jf.tree = tree.root_node
    for node in tree.root_node.children:
        if node.type == "package_declaration":
            for child in node.children:
                if child.type in ("scoped_identifier", "identifier"):
                    jf.package = _text(child)
        elif node.type == "import_declaration":
            name, wildcard, static = "", False, False
            for child in node.children:
                if child.type == "scoped_identifier":
                    name = _text(child)
                elif child.type == "static":
                    static = True
                elif child.type == "asterisk":
                    wildcard = True
            if name:
                jf.imports.append(ImportDecl(name=name, wildcard=wildcard, static=static))
        elif node.type in _TYPE_KINDS:
            jf.types.append(_parse_type(node))
    return jf


def _iter_types(types: list[TypeDecl], package: str):
    """Yield (fqn, TypeDecl) for top-level (package-joined) types and their
    nested declarations ($-chained, v1-compatible)."""
    for td in types:
        fqn = f"{package}.{td.name}" if package else td.name
        yield fqn, td
        yield from _iter_nested(td.nested, fqn)


def _iter_nested(types: list[TypeDecl], parent_fqn: str):
    for td in types:
        fqn = f"{parent_fqn}${td.name}"
        yield fqn, td
        yield from _iter_nested(td.nested, fqn)


def fqn_index(files: list[JavaFile]) -> dict[str, str]:
    """Map every source simple name to its FQN; ambiguous names are removed
    (they must never resolve through wildcard/same-package guessing)."""
    index: dict[str, str] = {}
    ambiguous: set[str] = set()
    for jf in files:
        for fqn, _td in _iter_types(jf.types, jf.package):
            simple = fqn.rsplit("$", 1)[-1].rsplit(".", 1)[-1]
            if simple in index and index[simple] != fqn:
                ambiguous.add(simple)
            else:
                index[simple] = fqn
    for simple in ambiguous:
        del index[simple]
    return index


# ---- FQN resolution (spec §4) ----

def resolve_type_name(name: str, package: str, imports: list[ImportDecl],
                      source_fqns: set[str]) -> str | None:
    """Syntactic FQN resolution (spec §4): fully-qualified as written,
    explicit single-type import, wildcard import vs known source types
    (package-scoped), same package (source types only). None when unknown."""
    if "." in name:
        return name
    for imp in imports:
        if not imp.wildcard and not imp.static and imp.name.rsplit(".", 1)[-1] == name:
            return imp.name
    for imp in imports:
        if imp.wildcard and not imp.static:
            candidate = f"{imp.name}.{name}"
            if candidate in source_fqns:
                return candidate
    same_package = f"{package}.{name}" if package else name
    if same_package in source_fqns:
        return same_package
    return None


# ---- extraction ----

def extract(repo: Path, out) -> list[JavaFile]:
    """Emit the structure graph of ``repo`` into ``out`` (Graph_out) and
    return the parsed files (consumed by the language extensions).

    Three phases: every node is declared first, then type-level edges are
    resolved (cross-file edges must not drop merely because the target's file
    sorts later), then a second pass over the retained trees emits CALLS and
    REFERENCES from method bodies (spec §4).
    """
    repo = Path(repo)
    java_files: list[JavaFile] = []
    for root in discover_source_roots(repo):
        for path in sorted(root.rglob("*.java")):
            java_files.append(parse_java_file(path))
    source_fqns = {fqn for jf in java_files
                   for fqn, _td in _iter_types(jf.types, jf.package)}

    declared: list[tuple[str, JavaFile, str, TypeDecl]] = []
    for jf in java_files:
        rel = jf.path.relative_to(repo).as_posix()
        out.add_node(f"file:{rel}", "File",
                     {"path": rel, "language": "java", "loc": jf.loc})
        for fqn, td in _iter_types(jf.types, jf.package):
            _emit_type_nodes(out, fqn, td)
            declared.append((rel, jf, fqn, td))

    for rel, jf, fqn, td in declared:
        _emit_type_edges(out, rel, jf, fqn, td, source_fqns)

    types_by_fqn: dict[str, tuple[JavaFile, TypeDecl]] = {
        fqn: (jf, td) for _rel, jf, fqn, td in declared}
    supers_map = {fqn: _resolved_supers(jf, td, source_fqns)
                  for fqn, (jf, td) in types_by_fqn.items()}
    field_cache: dict[str, dict[str, tuple[str, JavaFile]]] = {}
    for jf in java_files:
        for fqn, decl_node in _iter_decl_nodes(jf.tree, jf.package):
            for body in _member_bodies(decl_node):
                for member in body.children:
                    if member.type not in _METHOD_MEMBER_TYPES:
                        continue
                    body_node = member.child_by_field_name("body")
                    if body_node is None:  # abstract/interface members
                        continue
                    m = _method(member)
                    src_key = f"fn:{fqn}#{m.name}({','.join(m.params)})"
                    _scan_body(out, member, body_node, src_key, jf, fqn,
                               types_by_fqn, supers_map, source_fqns,
                               field_cache)
    return java_files


def _emit_type_nodes(out, fqn: str, td: TypeDecl) -> None:
    type_key = f"type:{fqn}"
    out.add_node(type_key, "Type",
                 {"kind": td.kind, "modifiers": td.modifiers, "loc": td.line})
    for m in td.methods:
        out.add_node(f"fn:{fqn}#{m.name}({','.join(m.params)})", "Method", {
            "name": m.name, "visibility": m.visibility, "static": m.static,
            "params": m.params, "returns": m.returns, "loc": m.line,
            "owner": type_key,
        })


def _emit_type_edges(out, rel_path: str, jf: JavaFile, fqn: str, td: TypeDecl,
                     source_fqns: set[str]) -> None:
    type_key = f"type:{fqn}"
    out.add_edge(f"file:{rel_path}", type_key, "CONTAINS")

    def _supers_to_edges(names: list[str], label: str) -> None:
        for ref in names:
            target = resolve_type_name(ref, jf.package, jf.imports, source_fqns)
            if target is None:
                out.count_dropped_edge()  # unresolvable name
            else:
                # resolved but not source-declared -> add_edge drops + counts
                out.add_edge(type_key, f"type:{target}", label)

    _supers_to_edges(td.extends_names, "EXTENDS")
    _supers_to_edges(td.interfaces, "IMPLEMENTS")


# ---- second pass: CALLS + REFERENCES from method bodies (spec §4) ----

def _resolved_supers(jf: JavaFile, td: TypeDecl,
                     source_fqns: set[str]) -> list[str]:
    """In-source supertype FQNs of a declared type (extends + implements)."""
    supers: list[str] = []
    for name in [*td.extends_names, *td.interfaces]:
        fqn = resolve_type_name(name, jf.package, jf.imports, source_fqns)
        if fqn is not None and fqn in source_fqns:
            supers.append(fqn)
    return supers


def _closure(fqn: str, supers_map: dict[str, list[str]]) -> list[str]:
    """A type plus its transitive in-source supertypes, near-first (BFS)."""
    seen: list[str] = []
    queue = deque([fqn])
    while queue:
        cur = queue.popleft()
        if cur in seen:
            continue
        seen.append(cur)
        queue.extend(supers_map.get(cur, ()))
    return seen


def _candidates(receiver_fqn: str, name: str, arity: int,
                types_by_fqn: dict[str, tuple[JavaFile, TypeDecl]],
                supers_map: dict[str, list[str]]) -> list[str]:
    """Method keys matching name+arity on the receiver's declared type or its
    in-source supertypes (spec §4 mapping rule)."""
    found: set[str] = set()
    for t_fqn in _closure(receiver_fqn, supers_map):
        entry = types_by_fqn.get(t_fqn)
        if entry is None:
            continue
        for m in entry[1].methods:
            if m.name == name and len(m.params) == arity:
                found.add(f"fn:{t_fqn}#{m.name}({','.join(m.params)})")
    return sorted(found)


def _fields_of(owner_fqn: str,
               types_by_fqn: dict[str, tuple[JavaFile, TypeDecl]],
               supers_map: dict[str, list[str]],
               cache: dict[str, dict[str, tuple[str, JavaFile]]]
               ) -> dict[str, tuple[str, JavaFile]]:
    """name -> (declared type text, declaring file) over the owner's in-source
    supertype closure; nearest declaration wins."""
    if owner_fqn in cache:
        return cache[owner_fqn]
    fields: dict[str, tuple[str, JavaFile]] = {}
    for t_fqn in _closure(owner_fqn, supers_map):
        entry = types_by_fqn.get(t_fqn)
        if entry is None:
            continue
        jf, td = entry
        for f in td.fields:
            fields.setdefault(f.name, (f.type_name, jf))
    cache[owner_fqn] = fields
    return fields


def _iter_decl_nodes(root_node, package: str):
    """Yield (fqn, type-declaration node) mirroring _iter_types: top-level
    types are package-joined, nested ones $-chained."""
    for node in root_node.children:
        if node.type in _TYPE_KINDS:
            name = _text(node.child_by_field_name("name"))
            fqn = f"{package}.{name}" if package else name
            yield fqn, node
            yield from _nested_decl_nodes(node, fqn)


def _nested_decl_nodes(decl, parent_fqn: str):
    for body in _member_bodies(decl):
        for child in body.children:
            if child.type in _TYPE_KINDS:
                fqn = f"{parent_fqn}${_text(child.child_by_field_name('name'))}"
                yield fqn, child
                yield from _nested_decl_nodes(child, fqn)


def _arity(arguments) -> int:
    if arguments is None:
        return 0
    return sum(1 for a in arguments.named_children
               if a.type not in ("line_comment", "block_comment"))


def _scan_body(out, method_decl, body, src_key: str, jf: JavaFile,
               owner_fqn: str, types_by_fqn, supers_map, source_fqns,
               field_cache) -> None:
    """Walk one method body: resolve receivers, emit CALLS/REFERENCES, count
    the rest honestly (scope = params + locals + owner-chain fields; ``this``
    is implied for unqualified calls)."""
    static_members = {i.name.rsplit(".", 1)[-1] for i in jf.imports
                      if i.static and not i.wildcard}
    static_classes = {i.name.rsplit(".", 1)[0].rsplit(".", 1)[-1] for i in jf.imports
                      if i.static and i.name.rsplit(".", 1)[0]}
    fields = _fields_of(owner_fqn, types_by_fqn, supers_map, field_cache)

    scope: dict[str, str] = {}  # params + locals: name -> declared type text
    params = method_decl.child_by_field_name("parameters")
    if params is not None:
        for p in params.children:
            if p.type not in ("formal_parameter", "spread_parameter"):
                continue
            name_node = p.child_by_field_name("name")
            type_node = p.child_by_field_name("type")
            if type_node is None:
                type_node = next((c for c in p.children if _is_type_node(c)), None)
            if name_node is not None and type_node is not None:
                scope[_text(name_node)] = declared_type_name(type_node)

    def call_on_type(type_text: str, ctx_jf: JavaFile, name: str,
                     arity: int, line: int, kind: str | None = None) -> None:
        fqn = resolve_type_name(type_text, ctx_jf.package, ctx_jf.imports,
                                source_fqns)
        if fqn is None:
            out.unresolved_refs += 1  # receiver-FQN resolution failure
            return
        entry = types_by_fqn.get(fqn)
        if entry is None:
            out.count_dropped_edge()  # resolved FQN, but not source-declared
            return
        if kind is None:
            kind = ("virtual-dispatch" if entry[1].kind == "interface"
                    else "direct")
        cands = _candidates(fqn, name, arity, types_by_fqn, supers_map)
        if len(cands) == 1:
            out.add_call(src_key, cands[0], line, kind)
        else:  # no such member in source, or name+arity overload ambiguity
            out.count_dropped_edge()

    def field_receiver(field_name: str, name: str, arity: int, line: int) -> None:
        if field_name not in fields:
            out.unresolved_refs += 1
            return
        type_text, decl_jf = fields[field_name]
        call_on_type(type_text, decl_jf, name, arity, line)

    def reference(type_node, line: int) -> None:
        if type_node is None:
            return
        fqn = resolve_type_name(declared_type_name(type_node), jf.package,
                                jf.imports, source_fqns)
        if fqn is not None and fqn in source_fqns:
            out.add_edge(src_key, f"type:{fqn}", "REFERENCES", {"line": line})
        else:
            out.count_dropped_edge()

    def invocation(inv) -> None:
        name_node = inv.child_by_field_name("name")
        if name_node is None:
            return
        name = _text(name_node)
        arity = _arity(inv.child_by_field_name("arguments"))
        line = _line(inv)
        obj = inv.child_by_field_name("object")
        if obj is None:  # unqualified: static import, else implicit this
            if name in static_members:
                out.count_dropped_edge()
            else:
                call_on_type(owner_fqn, jf, name, arity, line)
        elif obj.type == "identifier":
            ident = _text(obj)
            if ident in scope:
                call_on_type(scope[ident], jf, name, arity, line)
            elif ident in fields:
                field_receiver(ident, name, arity, line)
            elif ident in static_classes:  # Class.member via static import
                out.count_dropped_edge()
            else:
                fqn = resolve_type_name(ident, jf.package, jf.imports,
                                        source_fqns)
                if fqn is not None:
                    # receiver written as a type name: static call, direct;
                    # a resolved-but-out-of-source FQN drops like any
                    # non-source endpoint (not an unresolved_refs case)
                    call_on_type(fqn, jf, name, arity, line, kind="direct")
                else:
                    out.unresolved_refs += 1
        elif obj.type == "this":
            call_on_type(owner_fqn, jf, name, arity, line)
        elif obj.type == "field_access":
            inner = obj.child_by_field_name("object")
            fld = obj.child_by_field_name("field")
            if (inner is not None and inner.type == "this"
                    and fld is not None):
                field_receiver(_text(fld), name, arity, line)
            else:  # System.out.x(), chained a.b.c receivers, super.x()
                out.unresolved_refs += 1
        else:  # expression receiver: chained call, lambda, array...
            out.unresolved_refs += 1

    # sub-pass 1: collect the flat method scope (params + every local
    # declarator) BEFORE any invocation is resolved — the scope is
    # position-independent by design (syntactic approximation).
    stack = [body]
    while stack:
        node = stack.pop()
        t = node.type
        if t == "local_variable_declaration":
            type_text = next((declared_type_name(c) for c in node.children
                              if _is_type_node(c)), "")
            if type_text:
                for d in node.children:
                    if d.type == "variable_declarator":
                        name_node = d.child_by_field_name("name")
                        if name_node is not None:
                            scope[_text(name_node)] = type_text
        elif t == "enhanced_for_statement":
            type_node = node.child_by_field_name("type")
            name_node = node.child_by_field_name("name")
            if type_node is not None and name_node is not None:
                scope[_text(name_node)] = declared_type_name(type_node)
        elif t == "catch_formal_parameter":
            name_node = node.child_by_field_name("name")
            type_node = next((c for c in node.children if _is_type_node(c)), None)
            if name_node is not None and type_node is not None:
                scope[_text(name_node)] = declared_type_name(type_node)
        stack.extend(node.children)

    # sub-pass 2: emit CALLS / REFERENCES
    stack = [body]
    while stack:
        node = stack.pop()
        t = node.type
        if t == "method_invocation":
            invocation(node)
        elif t == "object_creation_expression":
            reference(node.child_by_field_name("type"), _line(node))
        elif t == "cast_expression":
            reference(next((c for c in node.children if _is_type_node(c)), None),
                      _line(node))
        stack.extend(node.children)
