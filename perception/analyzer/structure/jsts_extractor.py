"""tree-sitter JavaScript/TypeScript structure extraction (spec 2026-10-20
§3.1/§4/§5/§6).

One module, two grammars: tree-sitter-typescript ships two Languages
(``typescript`` + ``tsx``); tree-sitter-javascript one. Emits File/Type/Method
nodes (``lang`` prop = the FILE's language: typescript | javascript) plus
CONTAINS / EXTENDS / IMPLEMENTS / CALLS / REFERENCES / IMPORTS edges into the
shared :class:`~structure.model.Graph_out`.

Graphify-aligned two-pass design (spec §3.1):

- pass 1 — per-file parse: import/require tables (re-export names captured
  syntactically, order-independent), type tables (class/interface/type_alias,
  heritage), method tables; nodes and CONTAINS edges emitted immediately;
- pass 2 — cross-file binding: specifier resolution for IMPORTS (one hop;
  symbol binding through a re-export is beyond one hop → drop+count, spec
  §12), heritage resolution for EXTENDS/IMPLEMENTS (``kind: declared``),
  annotated-receiver CALLS (interface receiver → ``virtual-dispatch``), and
  REFERENCES from ``new`` expressions + type-identifier annotations.

Honesty rules (v2 discipline + the 2026-10-20 rev3 IMPORTS counting
ruling): unresolvable callee/receiver → ``unresolved_refs``; a relative
specifier resolving to an in-repo file whose export table lacks the imported
name → ``unresolved_refs`` (a name-resolution failure inside the repo; the
module-level IMPORTS edge itself stays — the file dependency is real);
constructible-but-unreachable (bare npm specifier, re-export chain beyond
one hop, unresolvable heritage) → ``dropped_edges``; builtin bare calls and
builtin member receivers are filtered silently BEFORE counting (spec
§3.1.3, language-local frozensets, never a shared table). ``import type``
creates type bindings (REFERENCES / receivers) but never an IMPORTS edge.
Default imports (``import X from``) never name an export-table entry: bound
opportunistically, never counted.

Grammar node-name pitfalls (probed; reference for the Rust/Go extractors):

- JS class name field is ``identifier``; TS is ``type_identifier`` —
  ``child_by_field_name("name")`` covers both, but any explicit type check
  must not assume one.
- ``require`` is NOT a dedicated node: a plain ``call_expression`` whose
  function field is ``identifier "require"`` with a ``string`` argument.
  Destructured require lives in ``variable_declarator.value`` with an
  ``object_pattern`` name — whose properties are
  ``shorthand_property_identifier_pattern`` in the JS grammar vs
  ``shorthand_property_identifier`` in TS (different node names!).
- TS ``import type`` is a bare ``type`` token DIRECT child of
  import_statement; inline ``{ type X }`` keeps the token inside the
  import_specifier — only direct children may be inspected.
- Import/export specifiers: field ``source`` → ``string`` → ``string_fragment``
  (never parse the quoted text from the raw slice).
- TS params: ``required_parameter``/``optional_parameter`` with fields
  ``pattern`` + ``type`` (a ``type_annotation``); rest params are a
  ``required_parameter`` wrapping ``rest_pattern``. JS params are bare
  ``identifier`` / ``assignment_pattern`` / ``rest_pattern`` children of
  ``formal_parameters`` (commas and parens are anonymous).
- **The params FIELD is ``parameters``** — ``formal_parameters`` is the NODE
  TYPE only, ``child_by_field_name("formal_parameters")`` silently returns
  None (same trap as python_extractor's ``parameters``).
- ``class_heritage`` is NOT a field of class_declaration/abstract_class_
  declaration (fields: ``name``, ``body`` only) — it is a NAMED CHILD node
  that must be found by iterating children.
- ``export default class X`` nests the ``class_declaration`` INSIDE the
  ``export_statement``; anonymous default classes have no name field.
- ``abstract_class_declaration`` is a separate node type from
  ``class_declaration``; both carry ``class_heritage`` →
  ``extends_clause`` (field ``value``) / ``implements_clause``.
"""
from __future__ import annotations

import posixpath
from dataclasses import dataclass, field
from pathlib import Path

import tree_sitter_javascript
import tree_sitter_typescript
from tree_sitter import Language, Parser

from .langs import walk as walk_lang
from .model import Graph_out

_TS_LANG = Language(tree_sitter_typescript.language_typescript())
_TSX_LANG = Language(tree_sitter_typescript.language_tsx())
_JS_LANG = Language(tree_sitter_javascript.language())
_TS_PARSER = Parser(_TS_LANG)
_TSX_PARSER = Parser(_TSX_LANG)
_JS_PARSER = Parser(_JS_LANG)

_TS_SUFFIXES = (".ts", ".tsx", ".mts", ".cts")
_PARSER_BY_SUFFIX = {
    ".ts": _TS_PARSER, ".mts": _TS_PARSER, ".cts": _TS_PARSER,
    ".tsx": _TSX_PARSER,
    ".js": _JS_PARSER, ".jsx": _JS_PARSER, ".mjs": _JS_PARSER,
    ".cjs": _JS_PARSER,
}

# Import-specifier candidate order (spec §5: x.ts/tsx/js/jsx/index.*).
_EXT_ORDER = (".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs")

# JS builtins — language-local, bare-identifier-scoped (spec §3.1.3); the
# graphify ``append`` lesson: a shared table once absorbed 330 phantom call
# edges. ``require`` is NOT here: it is import syntax, intercepted before
# call resolution. Member receivers (``console.log``) get their own table.
_JS_BUILTINS = frozenset({
    "AggregateError", "Array", "ArrayBuffer", "BigInt", "Boolean", "DataView",
    "Date", "Error", "EvalError", "Float32Array", "Float64Array", "Function",
    "Intl", "Map", "Number", "Object", "Promise", "Proxy", "RangeError",
    "ReferenceError", "Reflect", "RegExp", "Set", "String", "Symbol",
    "SyntaxError", "TypeError", "URIError", "WeakMap", "WeakSet",
    "alert", "atob", "btoa", "clearInterval", "clearTimeout", "decodeURI",
    "decodeURIComponent", "encodeURI", "encodeURIComponent", "eval", "fetch",
    "isFinite", "isNaN", "parseFloat", "parseInt", "queueMicrotask",
    "setInterval", "setTimeout", "structuredClone",
})
_JS_MEMBER_BUILTINS = frozenset({
    "Array", "BigInt", "Buffer", "JSON", "Math", "Number", "Object",
    "Promise", "Reflect", "String", "Symbol", "console", "document",
    "exports", "globalThis", "history", "Intl", "localStorage", "location",
    "module", "navigator", "process", "sessionStorage", "window",
})

# TS keeps its own tables (spec §3.1.3: per-language, never shared) — same
# universe, deliberately re-declared: a JS-only addition must never leak a
# silent filter into TS parsing or the reverse.
_TS_BUILTINS = frozenset({
    "AggregateError", "Array", "ArrayBuffer", "BigInt", "Boolean",
    "DataView", "Date", "Error", "EvalError", "Float32Array",
    "Float64Array", "Function", "Intl", "Map", "Number", "Object",
    "Promise", "Proxy", "RangeError", "ReferenceError", "Reflect", "RegExp",
    "Set", "String", "Symbol", "SyntaxError", "TypeError", "URIError",
    "WeakMap", "WeakSet", "alert", "atob", "btoa", "clearInterval",
    "clearTimeout", "decodeURI", "decodeURIComponent", "encodeURI",
    "encodeURIComponent", "eval", "fetch", "isFinite", "isNaN", "parseFloat",
    "parseInt", "queueMicrotask", "setInterval", "setTimeout",
    "structuredClone",
})
_TS_MEMBER_BUILTINS = frozenset({
    "Array", "BigInt", "Buffer", "JSON", "Math", "Number", "Object",
    "Promise", "Reflect", "String", "Symbol", "console", "document",
    "exports", "globalThis", "history", "Intl", "localStorage", "location",
    "module", "navigator", "process", "sessionStorage", "window",
})


def _read(node, source: bytes) -> str:
    return source[node.start_byte:node.end_byte].decode("utf-8", "replace")


def _simple_name(text: str) -> str:
    return text.split(".")[-1].strip()


def _string_text(node, source: bytes) -> str:
    """Raw fragment text of a ``string`` node (no quotes)."""
    if node is None:
        return ""
    stack = [node]
    while stack:
        c = stack.pop()
        if c.type == "string_fragment":
            return _read(c, source)
        stack.extend(c.children)
    return ""


def _module_spec(rel: str) -> str:
    """Module-spec: repo-relative posix path minus extension (spec §4)."""
    return rel.rsplit(".", 1)[0] if "." in rel else rel


def _ann_type_node(ann):
    """The type node inside a ``type_annotation`` (after the ``:``)."""
    if ann is None:
        return None
    for c in ann.children:
        if c.is_named:
            return c
    return None


def _ann_simple_name(ann, source: bytes) -> str:
    """Simple name of an annotation type (spec §4 TS param normalization).

    type_identifier/predefined_type verbatim; generic_type → its name child
    (``Array<string>`` → ``Array``); anything else → the first
    type-identifier descendant (``boolean[]`` → ``boolean``), else ``?``.
    """
    t = _ann_type_node(ann)
    if t is None:
        return "?"
    if t.type in ("type_identifier", "predefined_type"):
        return _read(t, source)
    nm = t.child_by_field_name("name")
    if nm is not None:
        return _read(nm, source)
    stack = [t]
    while stack:
        c = stack.pop(0)
        if c.type in ("type_identifier", "predefined_type"):
            return _read(c, source)
        stack.extend(c.children)
    return "?"


def _resolve_spec(src_rel: str, spec: str, rel_set: set[str]) -> str | None:
    """Resolve a relative specifier to an in-repo (parsed) file path.

    Tries the spec as-is (require usually keeps the extension), then the
    extension candidates, then ``index.*`` (spec §5).
    """
    if not spec.startswith("."):
        return None
    base = posixpath.normpath(posixpath.join(posixpath.dirname(src_rel),
                                              spec))
    candidates = [base]
    candidates.extend(base + e for e in _EXT_ORDER)
    candidates.extend(posixpath.join(base, "index") + e for e in _EXT_ORDER)
    for cand in candidates:
        if cand in rel_set:
            return cand
    return None


# ---- pass-1 parse model ----

@dataclass
class JsImport:
    spec: str                     # raw specifier ('' when unresolvable)
    kind: str                     # import | require | reexport | reexport_wildcard
    type_only: bool = False       # TS `import type` — bindings, never an edge
    unresolvable: bool = False    # require(<non-literal>) — counted, no binding
    names: list[tuple[str, str]] = field(default_factory=list)
    # names: (source-side name, local/exposed name)
    namespace: str | None = None  # `import * as ns` / `const ns = require()`
    default_local: str | None = None  # `import X from` — the default import's
    # local name; a default never names an export-table entry, so it is
    # bound opportunistically and NEVER counted as unresolved (rev3 ruling
    # applies to named imports).


@dataclass
class JsMethod:
    name: str
    params: str                   # TS: "Shape,number" / "?"-joined; JS: arity
    line: int
    visibility: str = "public"
    static: bool = False
    node: object = None


@dataclass
class JsType:
    name: str
    kind: str                     # class | interface | type_alias
    line: int
    extends: list[str] = field(default_factory=list)
    implements: list[str] = field(default_factory=list)
    methods: list[JsMethod] = field(default_factory=list)


@dataclass
class JsFile:
    rel: str                      # repo-relative posix path
    lang: str                     # "typescript" | "javascript" (by FILE suffix)
    module: str                   # module-spec (path form, no extension)
    source: bytes = b""
    tree: object = None
    imports: list[JsImport] = field(default_factory=list)
    types: list[JsType] = field(default_factory=list)
    functions: list[JsMethod] = field(default_factory=list)
    reexports: set[str] = field(default_factory=set)   # syntactic (parse-time)
    wildcard_reexport: bool = False


def _normalize_params(params_node, source: bytes, is_ts: bool) -> str:
    """Spec §4: TS = annotated simple names (``?`` when unannotated), JS = arity."""
    if params_node is None:
        return ""
    children = [c for c in params_node.children if c.is_named]
    if not is_ts:
        return str(len(children))
    parts: list[str] = []
    for c in children:
        ann = None
        if c.type in ("required_parameter", "optional_parameter"):
            ann = c.child_by_field_name("type")
        parts.append(_ann_simple_name(ann, source) if ann is not None else "?")
    return ",".join(parts)


def _parse_method(defn, source: bytes, is_ts: bool) -> JsMethod:
    name_node = defn.child_by_field_name("name")
    params = defn.child_by_field_name("parameters")  # NOT "formal_parameters"
    name = _read(name_node, source) if name_node is not None else ""
    visibility = "public"
    static = False
    for c in defn.children:
        if c.type == "accessibility_modifier":
            visibility = _read(c, source)
        elif c.type == "static":
            static = True
    if visibility == "public" and name.startswith("_"):
        visibility = "private"  # underscore convention, both languages
    return JsMethod(name=name,
                    params=_normalize_params(params, source, is_ts),
                    line=defn.start_point[0] + 1, visibility=visibility,
                    static=static, node=defn)


def _parse_file(path: Path, repo: Path) -> JsFile:
    source = path.read_bytes()
    rel = path.relative_to(repo).as_posix()
    suffix = rel[rel.rfind("."):]
    lang = "typescript" if suffix in _TS_SUFFIXES else "javascript"
    is_ts = lang == "typescript"
    root = _PARSER_BY_SUFFIX[suffix].parse(source).root_node
    pf = JsFile(rel=rel, lang=lang, module=_module_spec(rel), source=source,
                tree=root)

    def _class(defn) -> JsType | None:
        name_node = defn.child_by_field_name("name")
        if name_node is None:
            return None  # anonymous default class — no nameable Type node
        t = JsType(name=_read(name_node, source), kind="class",
                   line=defn.start_point[0] + 1)
        heritage = next((c for c in defn.children
                         if c.type == "class_heritage"), None)
        if heritage is not None:
            for c in heritage.children:
                if c.type == "extends_clause":
                    v = c.child_by_field_name("value")
                    if v is not None:
                        t.extends.append(_simple_name(_read(v, source)))
                elif c.type == "implements_clause":
                    for ty in c.children:
                        if ty.is_named:
                            t.implements.append(
                                _simple_name(_read(ty, source)))
        body = defn.child_by_field_name("body")
        if body is not None:
            for stmt in body.children:
                if stmt.type == "method_definition":
                    t.methods.append(_parse_method(stmt, source, is_ts))
        return t

    def _interface(defn) -> JsType | None:
        name_node = defn.child_by_field_name("name")
        if name_node is None:
            return None
        t = JsType(name=_read(name_node, source), kind="interface",
                   line=defn.start_point[0] + 1)
        body = defn.child_by_field_name("body")
        if body is not None:
            for stmt in body.children:
                if stmt.type == "method_signature":
                    t.methods.append(_parse_method(stmt, source, is_ts))
        return t

    def _import(stmt) -> None:
        src_node = stmt.child_by_field_name("source")
        if src_node is None:
            return
        spec = _string_text(src_node, source)
        # `import type`: the `type` token is a DIRECT child of the statement;
        # inline `{ type X }` keeps it inside import_specifier.
        type_only = any(c.type == "type" for c in stmt.children)
        names: list[tuple[str, str]] = []
        namespace = None
        default_local = None
        for c in stmt.children:
            if c.type != "import_clause":
                continue
            for d in c.children:
                if d.type == "identifier":
                    default_local = _read(d, source)  # default import
                elif d.type == "named_imports":
                    for es in d.children:
                        if es.type != "import_specifier":
                            continue
                        nm = es.child_by_field_name("name")
                        al = es.child_by_field_name("alias")
                        if nm is not None:
                            names.append(
                                (_read(nm, source),
                                 _read(al, source) if al is not None
                                 else _read(nm, source)))
                elif d.type == "namespace_import":
                    ids = [x for x in d.children if x.type == "identifier"]
                    if ids:
                        namespace = _read(ids[0], source)
        pf.imports.append(JsImport(spec=spec, kind="import",
                                   type_only=type_only, names=names,
                                   namespace=namespace,
                                   default_local=default_local))

    def _reexport(stmt, src_node) -> None:
        spec = _string_text(src_node, source)
        names: list[tuple[str, str]] = []
        wildcard = False
        namespace = None
        for c in stmt.children:
            if c.type == "export_clause":
                for es in c.children:
                    if es.type != "export_specifier":
                        continue
                    nm = es.child_by_field_name("name")
                    al = es.child_by_field_name("alias")
                    if nm is not None:
                        names.append(
                            (_read(nm, source),
                             _read(al, source) if al is not None
                             else _read(nm, source)))
            elif c.type == "*":
                wildcard = True
            elif c.type == "identifier":
                namespace = _read(c, source)  # `export * as ns from`
        if wildcard:
            pf.imports.append(JsImport(spec=spec, kind="reexport_wildcard",
                                       namespace=namespace))
            pf.wildcard_reexport = True
            if namespace:
                pf.reexports.add(namespace)
        else:
            pf.imports.append(JsImport(spec=spec, kind="reexport",
                                       names=names))
            pf.reexports.update(exposed for _, exposed in names)

    def _top(stmt) -> None:
        if stmt.type == "export_statement":
            src_node = stmt.child_by_field_name("source")
            if src_node is not None:
                _reexport(stmt, src_node)
                return
            for c in stmt.children:
                if c.type in ("class_declaration", "abstract_class_declaration",
                              "interface_declaration", "type_alias_declaration",
                              "function_declaration"):
                    _top(c)
            return
        if stmt.type in ("class_declaration", "abstract_class_declaration"):
            t = _class(stmt)
            if t is not None:
                pf.types.append(t)
        elif stmt.type == "interface_declaration":
            t = _interface(stmt)
            if t is not None:
                pf.types.append(t)
        elif stmt.type == "type_alias_declaration":
            name_node = stmt.child_by_field_name("name")
            if name_node is not None:
                pf.types.append(JsType(name=_read(name_node, source),
                                       kind="type_alias",
                                       line=stmt.start_point[0] + 1))
        elif stmt.type == "function_declaration":
            pf.functions.append(_parse_method(stmt, source, is_ts))
        elif stmt.type == "import_statement":
            _import(stmt)

    for stmt in root.children:
        _top(stmt)

    # require() scan — CommonJS import syntax, anywhere in the file (lazy
    # requires included). ``require`` is a plain call_expression (see module
    # docstring); the CALLS walker later skips it via the same recognition.
    def _visit(n) -> None:
        if n.type == "call_expression":
            fn = n.child_by_field_name("function")
            if (fn is not None and fn.type == "identifier"
                    and _read(fn, source) == "require"):
                args = n.child_by_field_name("arguments")
                first = next((c for c in args.children if c.is_named), None) \
                    if args is not None else None
                if first is None or first.type != "string":
                    pf.imports.append(JsImport(spec="", kind="require",
                                               unresolvable=True))
                else:
                    spec = _string_text(first, source)
                    names: list[tuple[str, str]] = []
                    namespace = None
                    parent = n.parent
                    if parent is not None and parent.type == "variable_declarator":
                        idn = parent.child_by_field_name("name")
                        if idn is not None:
                            if idn.type == "object_pattern":
                                for p in idn.children:
                                    if p.type in ("shorthand_property_identifier",
                                                  "shorthand_property_identifier_pattern"):
                                        text = _read(p, source)
                                        names.append((text, text))
                                    elif p.type == "pair":
                                        k = p.child_by_field_name("key")
                                        v = p.child_by_field_name("value")
                                        if (k is not None and v is not None
                                                and v.type == "identifier"):
                                            names.append((_read(k, source),
                                                          _read(v, source)))
                            elif idn.type == "identifier":
                                namespace = _read(idn, source)
                    pf.imports.append(JsImport(spec=spec, kind="require",
                                               names=names,
                                               namespace=namespace))
        for c in n.children:
            _visit(c)

    _visit(root)
    return pf


def extract(repo: Path, out: Graph_out,
            languages=("javascript", "typescript")) -> list[JsFile]:
    """Two-pass extraction: nodes + CONTAINS now, everything else after.

    ``languages`` selects which file sets walk (analyzer passes the requested
    subset); node ``lang`` props always follow the FILE's own language.
    """
    files: set[Path] = set()
    for lname in languages:
        files.update(walk_lang(repo, lname))

    parsed: list[JsFile] = []
    for path in sorted(files):
        try:
            parsed.append(_parse_file(path, repo))
        except OSError:
            continue

    # ---- pass 1: nodes ----
    for pf in parsed:
        loc = pf.source.count(b"\n") + (0 if pf.source.endswith(b"\n")
                                        or not pf.source else 1)
        file_key = f"file:{pf.rel}"
        out.add_node(file_key, "File",
                     {"path": pf.rel, "language": pf.lang, "lang": pf.lang,
                      "loc": loc})
        for t in pf.types:
            tkey = f"type:{pf.module}::{t.name}"
            out.add_node(tkey, "Type",
                         {"lang": pf.lang, "kind": t.kind, "modifiers": "",
                          "loc": 0, "line": t.line})
            out.add_edge(file_key, tkey, "CONTAINS")
            for m in t.methods:
                out.add_node(f"fn:{pf.module}::{t.name}#{m.name}({m.params})",
                             "Method",
                             {"lang": pf.lang, "name": m.name,
                              "visibility": m.visibility, "static": m.static,
                              "params": m.params, "returns": "", "loc": 0,
                              "line": m.line, "owner": tkey})
        for m in pf.functions:
            out.add_node(f"fn:{pf.module}#{m.name}({m.params})", "Method",
                         {"lang": pf.lang, "name": m.name,
                          "visibility": m.visibility, "static": m.static,
                          "params": m.params, "returns": "", "loc": 0,
                          "line": m.line, "owner": file_key})

    _bind(parsed, out)
    return parsed


# ---- pass 2: cross-file binding ----

def _bind(parsed: list[JsFile], out: Graph_out) -> None:
    rel_set = {f.rel for f in parsed}
    fkey = {f.rel: f"file:{f.rel}" for f in parsed}
    types_of: dict[str, dict[str, str]] = {}
    fns_of: dict[str, dict[str, str]] = {}
    kind_of: dict[str, str] = {}
    methods_of: dict[str, dict[str, str]] = {}
    for f in parsed:
        tt: dict[str, str] = {}
        ft: dict[str, str] = {}
        for t in f.types:
            tk = f"type:{f.module}::{t.name}"
            tt[t.name] = tk
            kind_of[tk] = t.kind
            methods_of[tk] = {
                m.name: f"fn:{f.module}::{t.name}#{m.name}({m.params})"
                for m in t.methods}
        for m in f.functions:
            ft[m.name] = f"fn:{f.module}#{m.name}({m.params})"
        types_of[f.rel] = tt
        fns_of[f.rel] = ft

    # imports + bindings; re-export names are parse-time (order-independent)
    bindings_of: dict[str, dict[str, tuple]] = {}
    for f in parsed:
        bindings: dict[str, tuple] = {}
        seen: set[str] = set()
        fk = fkey[f.rel]
        for imp in f.imports:
            if imp.unresolvable:
                out.count_dropped_edge()  # require(<non-literal>) — honest
                continue
            if not imp.spec.startswith("."):
                out.count_dropped_edge()  # bare npm specifier (spec §5)
                continue
            tgt = _resolve_spec(f.rel, imp.spec, rel_set)
            if tgt is None or tgt == f.rel:
                continue  # relative but no in-repo target (python precedent)
            if not imp.type_only and tgt not in seen:
                if out.add_import(fk, fkey[tgt]):
                    seen.add(tgt)
            if imp.kind == "reexport":
                for imported, _exposed in imp.names:
                    if (imported in types_of[tgt]
                            or imported in fns_of[tgt]):
                        continue  # one-hop re-export: resolvable
                    other = next((g for g in parsed if g.rel == tgt), None)
                    if (other is not None
                            and (imported in other.reexports
                                 or other.wildcard_reexport)):
                        out.count_dropped_edge()  # chain beyond one hop
                    else:
                        # in-repo target whose export table lacks the name
                        out.unresolved_refs += 1  # (rev3 counting ruling)
                continue
            if imp.kind == "reexport_wildcard":
                continue
            for imported, local in imp.names:
                if imported in fns_of[tgt]:
                    bindings[local] = ("fn", fns_of[tgt][imported])
                    continue
                if imported in types_of[tgt]:
                    bindings[local] = ("cls", types_of[tgt][imported])
                    continue
                other = next((g for g in parsed if g.rel == tgt), None)
                if (other is not None
                        and (imported in other.reexports
                             or other.wildcard_reexport)):
                    out.count_dropped_edge()  # symbol beyond one hop (§12)
                else:
                    # in-repo file, name absent from its export table
                    out.unresolved_refs += 1  # (rev3 counting ruling)
            if imp.default_local is not None:
                # default import: bound opportunistically, never counted
                # (the default export's identity is not in the symbol table)
                dl = imp.default_local
                if dl in fns_of[tgt]:
                    bindings[dl] = ("fn", fns_of[tgt][dl])
                elif dl in types_of[tgt]:
                    bindings[dl] = ("cls", types_of[tgt][dl])
            if imp.namespace:
                bindings[imp.namespace] = ("ns", tgt)
        bindings_of[f.rel] = bindings

    # heritage resolution + parent chains
    parents: dict[str, list[str]] = {}

    def _rt(f: JsFile, name: str, bindings: dict) -> str | None:
        own = types_of[f.rel].get(name)
        if own is not None:
            return own
        b = bindings.get(name)
        return b[1] if b is not None and b[0] == "cls" else None

    for f in parsed:
        bindings = bindings_of[f.rel]
        for t in f.types:
            tk = types_of[f.rel][t.name]
            ps: list[str] = []
            for base in t.extends:
                r = _rt(f, base, bindings)
                if r is None:
                    out.count_dropped_edge()  # base not in-source (spec §4)
                    continue
                out.add_edge(tk, r, "EXTENDS", {"kind": "declared"})
                ps.append(r)
            for impl in t.implements:
                r = _rt(f, impl, bindings)
                if r is None:
                    out.count_dropped_edge()
                    continue
                out.add_edge(tk, r, "IMPLEMENTS", {"kind": "declared"})
                ps.append(r)
            parents[tk] = ps

    chain_cache: dict[str, list[str]] = {}

    def _chain(tk: str, seen: set[str] | None = None) -> list[str]:
        if tk in chain_cache:
            return chain_cache[tk]
        seen = seen or set()
        if tk in seen:
            return [tk]
        seen.add(tk)
        result = [tk]
        for p in parents.get(tk, ()):
            for q in _chain(p, seen):
                if q not in result:
                    result.append(q)
        chain_cache[tk] = result
        return result

    # CALLS / REFERENCES — walk every method + module-function body
    for f in parsed:
        bindings = bindings_of[f.rel]
        own_fns = fns_of[f.rel]
        is_ts = f.lang == "typescript"
        builtins = _TS_BUILTINS if is_ts else _JS_BUILTINS
        member_builtins = (_TS_MEMBER_BUILTINS if is_ts
                           else _JS_MEMBER_BUILTINS)

        def _method_call(src: str, tks: list[str], attr: str, line: int,
                         ) -> None:
            # dispatch kind follows the RECEIVER's declared type (tks[0]),
            # not the declaring ancestor (spec §6: interface → virtual)
            kind = ("virtual-dispatch"
                    if kind_of.get(tks[0]) == "interface" else "direct")
            for tk in tks:
                mk = methods_of.get(tk, {}).get(attr)
                if mk is not None:
                    out.add_call(src, mk, line, kind)
                    return
            out.unresolved_refs += 1

        def _walk(m: JsMethod, owner: str | None) -> None:
            if owner is not None:
                src = f"fn:{f.module}::{owner}#{m.name}({m.params})"
                chain = _chain(types_of[f.rel][owner])
            else:
                src = f"fn:{f.module}#{m.name}({m.params})"
                chain = []

            # TS annotated params: receiver types + REFERENCES (spec §6)
            param_t: dict[str, str] = {}
            local_t: dict[str, str] = {}
            if is_ts:
                params = m.node.child_by_field_name("parameters")
                if params is not None:
                    for c in params.children:
                        if c.type not in ("required_parameter",
                                          "optional_parameter"):
                            continue
                        pat = c.child_by_field_name("pattern")
                        ann = c.child_by_field_name("type")
                        if (pat is None or ann is None
                                or pat.type != "identifier"):
                            continue
                        tnode = _ann_type_node(ann)
                        if (tnode is None
                                or tnode.type != "type_identifier"):
                            continue
                        pname = _read(pat, f.source)
                        tname = _read(tnode, f.source)
                        param_t[pname] = tname
                        tk = _rt(f, tname, bindings)
                        if tk is not None:
                            out.add_edge(src, tk, "REFERENCES",
                                         {"line": c.start_point[0] + 1})
                        else:
                            out.unresolved_refs += 1

            def _resolve_call(call) -> None:
                fn = call.child_by_field_name("function")
                if fn is None:
                    return
                line = call.start_point[0] + 1
                if fn.type == "identifier":
                    name = _read(fn, f.source)
                    if name == "require":
                        return  # import syntax — handled above
                    if name in builtins:
                        return  # not a repo reference (spec §3.1.3)
                    if name in own_fns:
                        out.add_call(src, own_fns[name], line, "direct")
                        return
                    b = bindings.get(name)
                    if b is not None:
                        if b[0] == "fn":
                            out.add_call(src, b[1], line, "direct")
                        else:  # imported class used as a value
                            out.add_edge(src, b[1], "REFERENCES",
                                         {"line": line})
                        return
                    tk = types_of[f.rel].get(name)
                    if tk is not None:
                        out.add_edge(src, tk, "REFERENCES", {"line": line})
                        return
                    out.unresolved_refs += 1
                    return
                if fn.type != "member_expression":
                    return  # computed callee — not nameable (python precedent)
                obj = fn.child_by_field_name("object")
                prop = fn.child_by_field_name("property")
                if obj is None or prop is None:
                    return
                attr = _read(prop, f.source)
                if obj.type == "this":
                    _method_call(src, chain, attr, line)
                    return
                if obj.type == "super":
                    _method_call(src, chain[1:], attr, line)
                    return
                if obj.type != "identifier":
                    out.unresolved_refs += 1  # a.b.c(), f().x() — free dynamic
                    return
                oname = _read(obj, f.source)
                if oname in member_builtins:
                    return  # console.log & friends — silent (spec §3.1.3)
                tname = param_t.get(oname) or local_t.get(oname)
                if tname is not None:
                    tk = _rt(f, tname, bindings)
                    if tk is None:
                        out.unresolved_refs += 1
                        return
                    _method_call(src, _chain(tk), attr, line)
                    return
                tk = _rt(f, oname, bindings)
                if tk is not None:  # ClassName.method()
                    _method_call(src, _chain(tk), attr, line)
                    return
                b = bindings.get(oname)
                if b is not None and b[0] == "ns":
                    tf = fns_of.get(b[1], {})
                    mk = tf.get(attr)
                    if mk is not None:
                        out.add_call(src, mk, line, "direct")
                        return
                    out.unresolved_refs += 1
                    return
                out.unresolved_refs += 1

            def _resolve_new(new) -> None:
                ctor = new.child_by_field_name("constructor")
                if ctor is None or ctor.type != "identifier":
                    return  # member/computed new — not nameable, skipped
                name = _read(ctor, f.source)
                if name in builtins:
                    return
                line = new.start_point[0] + 1
                tk = _rt(f, name, bindings)
                if tk is not None:
                    out.add_edge(src, tk, "REFERENCES", {"line": line})
                    return
                out.unresolved_refs += 1

            def visit(n) -> None:
                if n is None:
                    return
                if n.type == "variable_declarator" and is_ts:
                    nm = n.child_by_field_name("name")
                    ann = n.child_by_field_name("type")
                    if (nm is not None and nm.type == "identifier"
                            and ann is not None):
                        tnode = _ann_type_node(ann)
                        if tnode is not None and tnode.type == "type_identifier":
                            tname = _read(tnode, f.source)
                            local_t[_read(nm, f.source)] = tname
                            tk = _rt(f, tname, bindings)
                            if tk is not None:
                                out.add_edge(src, tk, "REFERENCES",
                                             {"line": n.start_point[0] + 1})
                            else:
                                out.unresolved_refs += 1
                elif n.type == "call_expression":
                    _resolve_call(n)
                elif n.type == "new_expression":
                    _resolve_new(n)
                for c in n.children:
                    visit(c)

            visit(m.node)

        for t in f.types:
            for m in t.methods:
                _walk(m, t.name)
        for m in f.functions:
            _walk(m, None)
