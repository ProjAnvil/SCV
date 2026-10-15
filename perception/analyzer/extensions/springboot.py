"""Spring Boot bean + INJECTS extension (spec 2026-10-14 §5).

Runs on top of the tree-sitter structure layer's parse results (the
``JavaFile`` list) and enriches the :class:`~structure.model.Graph_out` with
``Bean`` nodes and ``INJECTS`` edges, reproducing the Spoon BeanProbe
semantics syntactically:

- beans: stereotype classes (transitive meta-annotation walk — in-source
  ``@interface`` chains first, then the jar-carried table), ``@Bean`` methods
  (incl. static), and ``@ConfigurationProperties`` classes registered via
  ``@EnableConfigurationProperties(X.class)`` (syntactic ``.class`` match
  against source types); names: explicit stereotype value > decapitalized
  simple name, two leading uppercase letters preserved;
- injection points: constructor params (multi-ctor: ``@Autowired`` wins, else
  the sole non-default ctor; ``@Value`` params skipped; record components are
  the canonical constructor), Lombok ``@RequiredArgsConstructor``
  final/``@NonNull`` fields, ``@Autowired`` fields (own + superclass walk in
  source), setters, and ``@Bean`` method params — a class whose type is
  @Bean-produced contributes NO class-level points (its dependencies are the
  method params);
- resolution: candidates = beans whose type FQN equals the injected type FQN
  or transitively implements/extends it via in-source edges; qualifier ->
  bean-name match; ``@Primary`` collapse; by-name fallback; List/Set/
  Collection/Iterable take ALL candidates (never ambiguous);
  Optional/ObjectProvider unwrap; ambiguity flagged honestly;
- conditions recorded as props with the v1 wording; unresolved injections
  counted in the top-level ``unresolved`` (java./jdk. wants are noise).

Annotation names resolve by import, same package, or the fully-qualified
inline form (``@org.springframework.stereotype.Component``). Tree-walking
reuses the structure layer's tree-sitter-java helpers.
"""

from __future__ import annotations

import re
from collections import deque
from dataclasses import dataclass, field

from structure import java_extractor as jx

# ---- annotation name table (spec §5; FQNs as in the Spoon engine) ----

STEREOTYPES = {
    "org.springframework.stereotype.Component",
    "org.springframework.stereotype.Service",
    "org.springframework.stereotype.Repository",
    "org.springframework.stereotype.Controller",
    "org.springframework.web.bind.annotation.RestController",
    "org.springframework.web.bind.annotation.ControllerAdvice",
    "org.springframework.web.bind.annotation.RestControllerAdvice",
    "org.springframework.context.annotation.Configuration",
}
# jar-carried meta chains (in-source @interface chains are walked directly):
# annotation FQN -> annotations it is itself annotated with.
META_TABLE = {
    "org.springframework.boot.autoconfigure.SpringBootApplication": (
        "org.springframework.boot.SpringBootConfiguration",
        "org.springframework.boot.autoconfigure.EnableAutoConfiguration"),
    "org.springframework.boot.SpringBootConfiguration": (
        "org.springframework.context.annotation.Configuration",),
}
AUTOWIRED = "org.springframework.beans.factory.annotation.Autowired"
QUALIFIER = "org.springframework.beans.factory.annotation.Qualifier"
BEAN_METHOD = "org.springframework.context.annotation.Bean"
PRIMARY = "org.springframework.context.annotation.Primary"
VALUE_ANNO = "org.springframework.beans.factory.annotation.Value"
LOMBOK_RCA = "lombok.RequiredArgsConstructor"
LOMBOK_NONNULL = "lombok.NonNull"
ENABLE_CONFIG_PROPS = (
    "org.springframework.boot.context.properties.EnableConfigurationProperties")
CONDITION_PREFIX = "org.springframework.boot.autoconfigure.condition.ConditionalOn"
PROFILE = "org.springframework.context.annotation.Profile"

WRAPPER_FQNS = {
    "java.util.List", "java.util.Set", "java.util.Collection",
    "java.lang.Iterable", "java.util.Optional",
    "org.springframework.beans.factory.ObjectProvider",
}
COLLECTION_FQNS = WRAPPER_FQNS - {
    "java.util.Optional", "org.springframework.beans.factory.ObjectProvider"}
DATA_REPO_FAMILY = {
    "org.springframework.data.repository.Repository",
    "org.springframework.data.repository.CrudRepository",
    "org.springframework.data.repository.PagingAndSortingRepository",
    "org.springframework.data.jpa.repository.JpaRepository",
}

_ECP_CLASS_RE = re.compile(r"([\w.$]+)\.class")
_META_DEPTH = 6
_SUPER_WALK_LIMIT = 8
_ANNOTATION_NODES = ("marker_annotation", "annotation")
_PUNCT = {"(", ")", ",", "=", "<", ">"}


@dataclass
class Bean:
    name: str
    type_fqn: str
    stereotype: str
    conditions: list[str] = field(default_factory=list)
    file: str = ""
    line: int = 0
    primary: bool = False


@dataclass
class Point:
    want_fqn: str | None   # resolved FQN of the (unwrapped) injected type
    name: str              # field/param simple name (by-name disambiguation)
    via: str               # constructor | lombok-constructor | field |
    #                       # inherited-field | setter | beanMethod
    qualifier: str | None
    wraps: str | None      # simple name of the unwrapped wrapper, else None
    collection: bool       # multi-candidate is BY DESIGN, never ambiguous
    at: str


# ---- small parse helpers (annotation surface of declarations) ----

def _annotations_of(decl, jf, resolve) -> list[tuple]:
    """(resolved FQN or None, annotation node) of a declaration's modifiers."""
    out = []
    mods = jx._modifiers_of(decl)
    if mods is None:
        return out
    for child in mods.children:
        if child.type in _ANNOTATION_NODES:
            name = child.child_by_field_name("name")
            fqn = resolve(jx._text(name), jf) if name is not None else None
            out.append((fqn, child))
    return out


def _anno_fqns(annos: list[tuple]) -> set[str]:
    return {fqn for fqn, _node in annos if fqn is not None}


def _value_text(node) -> str | None:
    t = node.type
    if t == "string_literal":
        s = jx._text(node)
        return s[1:-1] if len(s) >= 2 else s
    if t == "element_value_array_initializer":
        # Spoon concatenates array attribute values without a separator
        parts = (_value_text(c) for c in node.children if c.type not in _PUNCT)
        return "".join(p for p in parts if p) or None
    return jx._text(node)


def _anno_value(anno_node, key: str) -> str | None:
    args = anno_node.child_by_field_name("arguments")
    if args is None:
        return None
    for child in args.children:
        if child.type == "element_value_pair":
            if not child.children or jx._text(child.children[0]) != key:
                continue
            vals = [c for c in child.children
                    if c.type not in ("identifier", "=")]
            return _value_text(vals[0]) if vals else None
        elif child.type not in _PUNCT and key == "value":
            return _value_text(child)
    return None


def _qualifier_of(annos: list[tuple]) -> str | None:
    for fqn, node in annos:
        if fqn == QUALIFIER:
            value = _anno_value(node, "value")
            if value is not None:
                return value
    return None


def _explicit_bean_name(annos: list[tuple]) -> str | None:
    """Explicit value of a DIRECT stereotype annotation (Spoon explicitName)."""
    for fqn, node in annos:
        if fqn in STEREOTYPES:
            value = _anno_value(node, "value")
            if value:
                return value
    return None


def _collect_conditions(annos: list[tuple]) -> list[str]:
    out = []
    for fqn, node in annos:
        if fqn is None or not (fqn.startswith(CONDITION_PREFIX) or fqn == PROFILE):
            continue
        simple = fqn.rsplit(".", 1)[-1]
        name = _anno_value(node, "name")
        value = _anno_value(node, "value")
        having = _anno_value(node, "havingValue")
        detail = name if name is not None else value
        if detail is not None:
            out.append(f"{simple}({detail}{'=' + having if having else ''})")
        else:
            out.append(simple)
    return out


def decapitalize(name: str) -> str:
    """Two leading uppercase letters are preserved (URLValidator stays)."""
    if len(name) > 1 and name[0].isupper() and name[1].isupper():
        return name
    return name[0].lower() + name[1:]


def _simple_type_name(fqn: str) -> str:
    """Simple name of a possibly-nested ($-chained) FQN."""
    return fqn.rsplit("$", 1)[-1].rsplit(".", 1)[-1]


# ---- stereotype detection (in-source meta walk first, then the table) ----

def _walk_meta(fqn, types_by_fqn, resolve, visited: set, depth: int):
    if fqn is None or depth > _META_DEPTH or fqn in visited:
        return None
    visited.add(fqn)
    if fqn in STEREOTYPES:
        return fqn
    entry = types_by_fqn.get(fqn)
    if entry is not None:  # in-source declaration: walk ITS annotations
        jf, node = entry
        for sub_fqn, _node in _annotations_of(node, jf, resolve):
            hit = _walk_meta(sub_fqn, types_by_fqn, resolve, visited, depth + 1)
            if hit is not None:
                return hit
        return None
    for nxt in META_TABLE.get(fqn, ()):
        hit = _walk_meta(nxt, types_by_fqn, resolve, visited, depth + 1)
        if hit is not None:
            return hit
    return None


def _detect_stereotype(annos, types_by_fqn, resolve):
    for fqn, _node in annos:  # direct hit first (Spoon ordering)
        if fqn in STEREOTYPES:
            return fqn
    for fqn, _node in annos:  # then the meta walk, in annotation order
        hit = _walk_meta(fqn, types_by_fqn, resolve, set(), 0)
        if hit is not None:
            return hit
    return None


def _super_names(node) -> list[str]:
    """extends + implements names as written (candidate closure input)."""
    names: list[str] = []
    sup = node.child_by_field_name("superclass")
    if sup is not None:
        for child in sup.children:
            if child.type in ("type_identifier", "scoped_type_identifier",
                              "generic_type"):
                names.append(jx.declared_type_name(child))
                break
    ifaces = node.child_by_field_name("interfaces")
    if ifaces is not None:
        names.extend(jx._type_list_texts(ifaces))
    else:  # interface_declaration: extends_interfaces is a bare child
        for child in node.children:
            if child.type == "extends_interfaces":
                names.extend(jx._type_list_texts(child))
                break
    return names


def _superclass_name(node) -> str | None:
    sup = node.child_by_field_name("superclass")
    if sup is None:
        return None
    for child in sup.children:
        if child.type in ("type_identifier", "scoped_type_identifier",
                          "generic_type"):
            return jx.declared_type_name(child)
    return None


def _is_data_repository(node, jf, resolve, types_by_fqn) -> bool:
    """Interface whose (transitive, resolvable) extends clause reaches the
    Spring Data family. Deviation-allowed rule; no fixture coverage."""
    work = deque([resolve(n, jf) for n in _super_names(node)])
    seen: set[str] = set()
    while work:
        cur = work.popleft()
        if cur is None or cur in seen:
            continue
        seen.add(cur)
        if cur in DATA_REPO_FAMILY:
            return True
        entry = types_by_fqn.get(cur)
        if entry is None:
            continue
        sjf, snode = entry
        work.extend(resolve(n, sjf) for n in _super_names(snode))
    return False


# ---- injection points ----

def _inj_type_info(type_node, jf, resolve) -> tuple[str, str | None, bool]:
    """(want type text, wrapper simple name | None, is collection) of a
    declared type: generic wrappers with exactly one argument unwrap."""
    if type_node is None:
        return "", None, False
    base_node, args_node = type_node, None
    if type_node.type == "generic_type" and type_node.children:
        base_node = type_node.children[0]
        if (len(type_node.children) > 1
                and type_node.children[1].type == "type_arguments"):
            args_node = type_node.children[1]
    base_text = jx.declared_type_name(base_node)
    base_fqn = resolve(base_text, jf)
    if args_node is not None and base_fqn in WRAPPER_FQNS:
        elems = [c for c in args_node.children if c.type not in _PUNCT]
        if len(elems) == 1:
            elem = elems[0]
            elem_text = (jx.declared_type_name(elem.children[0])
                         if elem.type == "generic_type" and elem.children
                         else jx.declared_type_name(elem))
            return elem_text, base_fqn.rsplit(".", 1)[-1], base_fqn in COLLECTION_FQNS
    return base_text, None, False


def _param_point(param, jf, resolve, via: str) -> Point | None:
    annos = _annotations_of(param, jf, resolve)
    if VALUE_ANNO in _anno_fqns(annos):
        return None
    type_node = param.child_by_field_name("type")
    if type_node is None:
        type_node = next((c for c in param.children if jx._is_type_node(c)), None)
    want_text, wraps, collection = _inj_type_info(type_node, jf, resolve)
    name_node = param.child_by_field_name("name")
    return Point(
        want_fqn=resolve(want_text, jf) if want_text else None,
        name=jx._text(name_node) if name_node is not None else "",
        via=via, qualifier=_qualifier_of(annos), wraps=wraps,
        collection=collection, at=f"{jf.path.name}:{jx._line(param)}")


def _params_of(decl) -> list:
    params = decl.child_by_field_name("parameters")
    if params is None:
        return []
    return [c for c in params.children
            if c.type in ("formal_parameter", "spread_parameter")]


def _method_decls(node) -> list:
    return [child for body in jx._member_bodies(node)
            for child in body.children if child.type == "method_declaration"]


def _ctor_decls(node) -> list:
    return [child for body in jx._member_bodies(node) for child in body.children
            if child.type in ("constructor_declaration",
                              "compact_constructor_declaration")]


def _field_decls(node) -> list:
    return [child for body in jx._member_bodies(node)
            for child in body.children if child.type == "field_declaration"]


def _field_info(field_decl, jf, resolve) -> tuple[list[str], set[str], list[tuple]]:
    """(keywords, annotation FQNs, annotations) of one field declaration."""
    mods = jx._modifiers_of(field_decl)
    kws = jx._modifier_keywords(mods)
    annos = _annotations_of(field_decl, jf, resolve)
    return kws, _anno_fqns(annos), annos


def _declarators(field_decl) -> list:
    out = []
    for child in field_decl.children:
        if child.type == "variable_declarator":
            name_node = child.child_by_field_name("name")
            if name_node is not None:
                out.append((jx._text(name_node), jx._line(child),
                            child.child_by_field_name("value")))
    return out


def _class_injection_points(node, jf, fqn, resolve, types_by_fqn) -> list[Point]:
    points: list[Point] = []

    # constructors: @Autowired-annotated wins; else the sole non-default
    ctors = _ctor_decls(node)
    non_default = [c for c in ctors if _params_of(c)]
    autowired = []
    for c in non_default:
        if AUTOWIRED in _anno_fqns(_annotations_of(c, jf, resolve)):
            autowired.append(c)
    selected = autowired if autowired else (non_default if len(non_default) == 1 else [])
    for c in selected:
        for p in _params_of(c):
            point = _param_point(p, jf, resolve, "constructor")
            if point is not None:
                points.append(point)

    # records: the canonical constructor IS the component list
    has_explicit_ctor = bool(non_default)
    if node.type == "record_declaration":
        comps = _params_of(node)
        if comps:
            has_explicit_ctor = True
            if not any(_params_of(c) for c in ctors):
                for p in comps:
                    point = _param_point(p, jf, resolve, "constructor")
                    if point is not None:
                        points.append(point)

    # Lombok @RequiredArgsConstructor: final/@NonNull fields of THIS class
    if not has_explicit_ctor and LOMBOK_RCA in _anno_fqns(
            _annotations_of(node, jf, resolve)):
        for fd in _field_decls(node):
            kws, fqns, annos = _field_info(fd, jf, resolve)
            if "static" in kws:
                continue
            if "final" in kws or LOMBOK_NONNULL in fqns:
                qualifier = _qualifier_of(annos)
                for name, line, _init in _declarators(fd):
                    type_node = next((c for c in fd.children
                                      if jx._is_type_node(c)), None)
                    want_text, wraps, coll = _inj_type_info(type_node, jf, resolve)
                    points.append(Point(
                        want_fqn=resolve(want_text, jf) if want_text else None,
                        name=name, via="lombok-constructor", qualifier=qualifier,
                        wraps=wraps, collection=coll,
                        at=f"{jf.path.name}:{line}"))

    # field injection: own fields, then inherited from in-source superclasses
    seen_names: set[str] = set()
    for fd in _field_decls(node):
        kws, fqns, annos = _field_info(fd, jf, resolve)
        for name, _line, _init in _declarators(fd):
            seen_names.add(name)
        if "static" in kws or AUTOWIRED not in fqns:
            continue
        qualifier = _qualifier_of(annos)
        for name, line, _init in _declarators(fd):
            type_node = next((c for c in fd.children if jx._is_type_node(c)), None)
            want_text, wraps, coll = _inj_type_info(type_node, jf, resolve)
            points.append(Point(
                want_fqn=resolve(want_text, jf) if want_text else None,
                name=name, via="field", qualifier=qualifier, wraps=wraps,
                collection=coll, at=f"{jf.path.name}:{line}"))
    sup_name = _superclass_name(node)
    sup_fqn = resolve(sup_name, jf) if sup_name else None
    guard = 0
    while sup_fqn is not None and sup_fqn in types_by_fqn and guard < _SUPER_WALK_LIMIT:
        guard += 1
        sjf, snode = types_by_fqn[sup_fqn]
        for fd in _field_decls(snode):
            kws, fqns, annos = _field_info(fd, sjf, resolve)
            if "static" in kws or AUTOWIRED not in fqns:
                continue
            qualifier = _qualifier_of(annos)
            for name, line, _init in _declarators(fd):
                if name in seen_names:  # overridden/shadowed
                    continue
                seen_names.add(name)
                type_node = next((c for c in fd.children
                                  if jx._is_type_node(c)), None)
                want_text, wraps, coll = _inj_type_info(type_node, sjf, resolve)
                points.append(Point(
                    want_fqn=resolve(want_text, sjf) if want_text else None,
                    name=name, via="inherited-field", qualifier=qualifier,
                    wraps=wraps, collection=coll,
                    at=f"{sjf.path.name}:{line}"))
        nxt = _superclass_name(snode)
        sup_fqn = resolve(nxt, sjf) if nxt else None

    # setter injection
    for m in _method_decls(node):
        kws = jx._modifier_keywords(jx._modifiers_of(m))
        if "static" in kws:
            continue
        params = _params_of(m)
        if len(params) != 1:
            continue
        if AUTOWIRED not in _anno_fqns(_annotations_of(m, jf, resolve)):
            continue
        point = _param_point(params[0], jf, resolve, "setter")
        if point is not None:
            points.append(point)
    return points


# ---- resolution ----

def _supertype_closure(fqn: str, supers_map: dict, cache: dict) -> set[str]:
    """The type plus its transitive IN-SOURCE supertypes (BFS)."""
    if fqn in cache:
        return cache[fqn]
    seen: set[str] = set()
    queue = deque([fqn])
    while queue:
        cur = queue.popleft()
        if cur in seen:
            continue
        seen.add(cur)
        queue.extend(supers_map.get(cur, ()))
    cache[fqn] = seen
    return seen


def _resolve_candidates(point: Point, beans: dict[str, Bean], supers_map, cache):
    if point.want_fqn is None:
        return []
    candidates = [b for b in beans.values()
                  if b.type_fqn == point.want_fqn
                  or point.want_fqn in _supertype_closure(
                      b.type_fqn, supers_map, cache)]
    if point.qualifier is not None:
        return [b for b in candidates if b.name == point.qualifier]
    if len(candidates) > 1 and not point.collection:
        primaries = [b for b in candidates if b.primary]
        if len(primaries) == 1:
            return primaries
        by_name = [b for b in candidates if b.name == point.name]
        if len(by_name) == 1:  # Spring byType -> byName fallback
            return by_name
    return candidates


# ---- main entry ----

def enrich(java_files: list, out) -> None:
    """Add Bean nodes + INJECTS edges (spec §5) for the parsed sources."""
    source_fqns = {fqn for jf in java_files
                   for fqn, _node in jx._iter_decl_nodes(jf.tree, jf.package)}

    def resolve(name: str, jf):
        return jx.resolve_type_name(name, jf.package, jf.imports, source_fqns)

    types_by_fqn: dict[str, tuple] = {}
    for jf in java_files:
        for fqn, node in jx._iter_decl_nodes(jf.tree, jf.package):
            types_by_fqn[fqn] = (jf, node)

    supers_map: dict[str, list[str]] = {}
    for fqn, (jf, node) in types_by_fqn.items():
        supers = []
        for name in _super_names(node):
            target = resolve(name, jf)
            if target is not None and target in types_by_fqn:
                supers.append(target)
        supers_map[fqn] = supers
    closure_cache: dict[str, set[str]] = {}

    # phase 1: collect beans (stereotype classes, @Bean methods, ECP records)
    beans: dict[str, Bean] = {}
    owner_by_type: dict[str, str] = {}   # type FQN -> first bean key
    bean_produced_types: set[str] = set()

    def _add_bean(bean: Bean) -> None:
        key = f"bean:{bean.name}"
        beans[key] = bean
        owner_by_type.setdefault(bean.type_fqn, key)

    for jf in java_files:
        for fqn, node in jx._iter_decl_nodes(jf.tree, jf.package):
            if node.type == "annotation_type_declaration":
                continue  # annotation types are not beans
            annos = _annotations_of(node, jf, resolve)
            stereo_fqn = _detect_stereotype(annos, types_by_fqn, resolve)
            data_repo = False
            if stereo_fqn is not None:
                stereotype = stereo_fqn.rsplit(".", 1)[-1]
            elif (node.type == "interface_declaration"
                  and _is_data_repository(node, jf, resolve, types_by_fqn)):
                stereotype, data_repo = "data-repository", True
            else:
                continue
            _add_bean(Bean(
                name=_explicit_bean_name(annos)
                    or decapitalize(_simple_type_name(fqn)),
                type_fqn=fqn, stereotype=stereotype,
                conditions=_collect_conditions(annos),
                file=jf.path.name, line=jx._line(node),
                primary=PRIMARY in _anno_fqns(annos)))
            if data_repo:
                continue
            for m in _method_decls(node):
                m_annos = _annotations_of(m, jf, resolve)
                if BEAN_METHOD not in _anno_fqns(m_annos):
                    continue
                ret_node = m.child_by_field_name("type")
                ret_text = (jx.declared_type_name(ret_node)
                            if ret_node is not None else "")
                ret_fqn = resolve(ret_text, jf) if ret_text else None
                name_node = m.child_by_field_name("name")
                _add_bean(Bean(
                    name=jx._text(name_node) if name_node is not None else "",
                    type_fqn=ret_fqn or ret_text, stereotype="@Bean",
                    conditions=_collect_conditions(m_annos),
                    file=jf.path.name, line=jx._line(m),
                    primary=PRIMARY in _anno_fqns(m_annos)))
                if ret_fqn is not None:
                    bean_produced_types.add(ret_fqn)

    # @EnableConfigurationProperties(X.class) registrations
    for fqn, (jf, node) in types_by_fqn.items():
        for anno_fqn, anno_node in _annotations_of(node, jf, resolve):
            if anno_fqn != ENABLE_CONFIG_PROPS:
                continue
            args = anno_node.child_by_field_name("arguments")
            raw = jx._text(args) if args is not None else ""
            for match in _ECP_CLASS_RE.finditer(raw):
                target = resolve(match.group(1), jf)
                if target is None or target not in types_by_fqn:
                    continue
                if any(b.type_fqn == target for b in beans.values()):
                    continue
                t_jf, t_node = types_by_fqn[target]
                t_name_node = t_node.child_by_field_name("name")
                t_simple = (jx._text(t_name_node) if t_name_node is not None
                            else _simple_type_name(target))
                _add_bean(Bean(
                    name=decapitalize(t_simple), type_fqn=target,
                    stereotype="ConfigurationProperties",
                    conditions=[f"EnableConfigurationProperties("
                                f"{_simple_type_name(fqn)})"],
                    file=jf.path.name, line=jx._line(node)))

    # phase 2: injection points per bean
    injections: dict[str, list[Point]] = {}
    for fqn, (jf, node) in types_by_fqn.items():
        owner = owner_by_type.get(fqn)
        if owner is None:
            continue
        if fqn not in bean_produced_types:  # @Bean params already cover it
            injections.setdefault(owner, []).extend(
                _class_injection_points(node, jf, fqn, resolve, types_by_fqn))
        for m in _method_decls(node):
            if BEAN_METHOD not in _anno_fqns(_annotations_of(m, jf, resolve)):
                continue
            name_node = m.child_by_field_name("name")
            m_name = jx._text(name_node) if name_node is not None else ""
            bean_key = f"bean:{m_name}" if f"bean:{m_name}" in beans else owner
            for p in _params_of(m):
                point = _param_point(p, jf, resolve, "beanMethod")
                if point is not None:
                    injections.setdefault(bean_key, []).append(point)

    # resolve + emit
    unresolved: list[str] = []
    edges: list[tuple] = []
    edge_keys: set[str] = set()
    for src_key, points in injections.items():
        for point in points:
            candidates = _resolve_candidates(point, beans, supers_map,
                                             closure_cache)
            if not candidates:
                want = point.want_fqn
                framework_noise = (want is None or want.startswith("java.")
                                   or want.startswith("jdk."))
                if not framework_noise:
                    detail = (f"{point.via}"
                              f"{f'<{point.wraps}>' if point.wraps else ''}"
                              f" @ {point.at}")
                    unresolved.append(
                        f"{src_key} needs {want} "
                        + (f"@{point.qualifier} " if point.qualifier else "")
                        + f"({detail})")
                continue
            ambiguous = not point.collection and len(candidates) > 1
            for bean in candidates:
                dst_key = f"bean:{bean.name}"
                if dst_key == src_key:
                    continue
                dedup = f"{src_key}->{dst_key}:{point.via}:{point.name}"
                if dedup in edge_keys:
                    continue
                edge_keys.add(dedup)
                edges.append((src_key, dst_key, point.via, point.qualifier,
                              ambiguous, point.wraps, point.at))

    # output: nodes first so INJECTS endpoints never drop
    for key, bean in beans.items():
        props = {
            "beanName": bean.name,
            "type": bean.type_fqn,
            "stereotype": bean.stereotype,
            "primary": bean.primary,
        }
        if bean.conditions:
            props["conditions"] = "; ".join(bean.conditions)
        props["source"] = f"{bean.file}:{bean.line}"
        out.add_node(key, "Bean", props)
    for src, dst, via, qualifier, ambiguous, wraps, at in edges:
        props = {"via": via}
        if qualifier is not None:
            props["qualifier"] = qualifier
        if wraps is not None:
            props["wraps"] = wraps
        if ambiguous:
            props["ambiguous"] = True
        props["at"] = at
        out.add_edge(src, dst, "INJECTS", props)
    out.unresolved = len(unresolved)
