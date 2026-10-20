# SCV Code Perception

SCV's code perception capability extracts a structure graph from source code
that `/scv run` uses to deepen the generated architecture documentation — and
that agents can query in later sessions.

**Supported: Java, Python, JavaScript, TypeScript, Go, Rust.** For a Spring
Boot project, `/scv run` performs deep structural analysis automatically: it
maps components (services, controllers, repositories, configuration
components) and their dependency edges, detects circular dependencies, and
enriches ARCHITECTURE.md with a "Component Dependencies" section. Every other
supported language gets the language-agnostic structure layer: files, types,
methods, call edges, and cross-file import edges.

## Language support

| Language | Extracts | CALLS resolution | Special semantics |
|---|---|---|---|
| Java | files, types, methods; component surface for Spring Boot | scope-based receiver resolution (most precise) | Spring Boot extension on top |
| Python | classes, functions, module tree | `self`/`cls` calls resolved through the in-source MRO; module-level functions; `from`-imported symbols | builtin names (`len`, `print`, ...) never counted as calls; method keys carry arity only (annotations erased) |
| JavaScript | ES modules, classes, functions | imported bindings, module-scope functions, `this.x()` in classes | free dynamic calls stay unresolved (see Known limitations); `require()` supported |
| TypeScript | same as JavaScript plus interfaces | annotated receiver types; interface-typed receivers call with virtual dispatch | `implements` clause → IMPLEMENTS edge |
| Go | structs, interfaces, methods | receiver type from declared vars/params/fields; interface-typed receivers call with virtual dispatch | IMPLEMENTS by syntactic method-set matching (see below) |
| Rust | crate/module tree, traits, impls | `use`-resolved paths; `impl` blocks give the owner directly (most reliable of the six) | `impl Trait for Type` is fully explicit → IMPLEMENTS always declared |

### Go method-set IMPLEMENTS

Go satisfaction is structural, so the analyzer matches method sets
syntactically: a type gets an IMPLEMENTS edge to an interface when its method
set (including methods promoted through struct embedding) covers every
interface method with matching signature. Each edge carries:

- `kind: "method-set"` (deduced by coverage; `kind: "declared"` marks
  relations written in the source — Java `implements`, Python bases, TS
  `implements`, Rust `impl Trait for Type`)
- `receiver: "value"` when value-receiver methods alone satisfy the interface,
  `receiver: "pointer"` when pointer-receiver methods are needed (only `*T`
  satisfies the interface)

When an embedded **external** type's promoted methods make coverage
unknowable, no edge is emitted and the case is counted in
`meta.structure.go_unverifiable_satisfaction` — counted, never guessed.

## Runtime requirements

Deep structural analysis needs a **Python 3.11+ runtime** on the default path —
no JDK, no build tools. The first run prepares the analysis environment
automatically (typically under half a minute); afterwards each analysis takes
seconds. The tree-sitter grammar packages for all six languages ship pinned in
the analyzer's lockfile, so the environment prepares them in the same step.
Nothing inside the analyzed repository is ever executed — local and remote
repositories are analyzed identically.

## Optional query tier

When a Go 1.27+ runtime is available, `/scv run` also generates a structure-graph
query file (`structure.vg`) **and builds `scv-graph-mcp`** — a query service over
every analyzed repository. One registration serves all repos; agents query by
repo name plus a graph query language (Cypher subset) and get full node
information back:

```
claude mcp add scv-graph -- ~/.scv/perception/dist/graphmcp/scv-graph-mcp
```

MCP tools: `repos` (list analyzed repositories), `cypher(repo, query)`,
`get_node(repo, key)`, `refresh`.

Two node surfaces live in the same graph:

- **Component surface (Spring Boot extension):** nodes carry the component kind
  (`stereotype`: RestController / Service / Repository / Configuration / ...),
  bean name, type, source file:line, and conditions; edges carry `INJECTS` with
  via/qualifier annotations.
- **Structure surface (always on):** `Type` nodes (`type:<fqn>`, props kind /
  modifiers / lang) and `Method` nodes (`fn:<fqn>#<name>(<params>)`, props name /
  visibility / static / owner / lang); edges `CONTAINS` (File→Type), `IMPLEMENTS`
  and `EXTENDS` (Type→Type, IMPLEMENTS carries `kind` — plus `receiver` for Go
  method-set edges), `CALLS` (Method→Method, direct or virtual-dispatch),
  `REFERENCES` (Method→Type), and `IMPORTS` (File→File, cross-file dependency).

Every node in a multi-language repository carries a `lang` property
(`java`, `python`, `javascript`, `typescript`, `go`, `rust`), so queries can
scope to one language.

Example queries:

```
MATCH (n {stereotype:'RestController'}) RETURN n                      -- all controllers with full info
MATCH (s {stereotype:'Service'})-[:INJECTS]->(m) RETURN s.key, m.key  -- service dependency pairs
MATCH (n {lang:'python'}) RETURN n.key                                -- census of the python part of a repo
MATCH (f:File)-[:IMPORTS]->(g) RETURN f.key, g.key                    -- file-level dependency graph (any language)
MATCH (t:Type {lang:'go'})-[e:IMPLEMENTS]->(i {kind:'interface'})
  RETURN t.key, i.key, e                                              -- Go method-set implements; edge props
                                                                      -- show kind (method-set) and receiver (value/pointer)
MATCH (m:Method)-[:CALLS]->(n:Method {name:'greet'}) RETURN m.owner, m.name  -- callers of a method
```

Fallback: without a Go runtime, a separately installed
`vectra-mcp` (`go install github.com/projanvil/vectra/mcp/cmd/vectra-mcp@latest`)
can serve one repository's snapshot per registration (`scv-structure`).

Both tiers are optional enhancements: when their runtime is missing, `/scv run`
notes the skip in its completion report and finishes normally.

## Configuration (`~/.scv/config.json`)

| Key | Default | Meaning |
|---|---|---|
| `structure_analysis_enabled` | `true` | enable/disable deep structural analysis |
| `structure_extensions` | `["springboot"]` | language extensions to enrich the structure graph (see below) |
| `structure_languages` | all six languages | languages the structure tier analyzes |

`structure_extensions` toggles framework enrichment on top of the
language-agnostic structure layer. With `["springboot"]` (default) the component
surface is extracted; with `[]` (structure mode) only types, methods, and call
edges are extracted — useful for plain Java projects, and the summary line then
reports `{types} types, {methods} methods, {call_edges} call edges` instead of
component counts. Unknown values are ignored with a note in `perception.log`.

`structure_languages` selects the languages the structure tier runs on
(default: `["java", "python", "javascript", "typescript", "go", "rust"]`).
The tier runs when ANY enabled language has at least one source file in the
repository, and the summary line lists the languages actually found. Unknown
values are warned into `perception.log` and ignored; an empty list turns the
structure tier off entirely (the run finishes with a skip note, like the
extensions-off case). `structure_extensions` is orthogonal: the Spring Boot
extension still applies only to Java repositories.

## Known limitations

- **Go**: interface method signatures are compared by simple type name —
  `pkg.Foo` and a local `Foo` collide (documented approximation). Types
  embedding an external type cannot have their satisfaction verified: no edge,
  counted in `go_unverifiable_satisfaction`.
- **JavaScript**: free dynamic calls (no resolvable receiver or binding) stay
  unresolved and are counted in `unresolved_refs`. Real JS codebases produce
  high counts there — that is the honest floor of syntactic analysis, not a
  failure.
- **Python**: method keys carry the parameter count only
  (`fn:<module>#<name>(<n>)`) — annotations are erased because overloading is
  not a Python semantic.
- **Java**: wildcard (on-demand) imports hide a class's stereotype — a
  `@RestController` reached through `import ...*;` is extracted into the
  structure layer but never becomes a bean.

## Reliability

The analyzer's extraction is pinned by a frozen fixture corpus with
ground-truth assertions (node and edge multisets per language, including the Go
IMPLEMENTS multiset with its pointer-receiver, embedding-promotion, and
honest-miss cases), and the Spring Boot component extraction is
cross-validated release-over-release against a reference analysis engine over
the same corpus (a developer-side check; see DEVELOPERS.md).

## Files

- `~/.scv/analysis/{project}/structure.json` — structure graph data (always, when analysis ran)
- `~/.scv/analysis/{project}/structure.vg` — query-file for the MCP tier (optional tier)
- `~/.scv/analysis/{project}/perception.log` — tier decisions and skip reasons

Deleting the analysis directory removes these files; the MCP query service stops
returning data for the project until the next `/scv run`.
