# SCV Code Perception

SCV's code perception capability recognizes supported frameworks in a codebase and
extracts a structure graph that `/scv run` uses to deepen the generated
architecture documentation — and that agents can query in later sessions.

**Supported: Spring Boot.** For a Spring Boot project, `/scv run` performs
deep structural analysis automatically: it maps components (services, controllers,
repositories, configuration components) and their dependency edges, detects circular
dependencies, and enriches ARCHITECTURE.md with a "Component Dependencies" section.

## Runtime requirements

Deep structural analysis needs a **Python 3.11+ runtime** on the default path —
no JDK, no build tools. The first run prepares the analysis environment
automatically (typically under half a minute); afterwards each analysis takes
seconds. Nothing inside the analyzed repository is ever executed — local and
remote repositories are analyzed identically.

Non-Spring Java repositories are also supported: with all extensions turned off
(see below), the language-agnostic structure layer still maps files, types, and
methods.

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
modifiers) and `Method` nodes (`fn:<fqn>#<name>(<params>)`, props name /
visibility / static / owner); edges `CONTAINS` (File→Type), `IMPLEMENTS` and
`EXTENDS` (Type→Type), `CALLS` (Method→Method, direct or virtual-dispatch), and
`REFERENCES` (Method→Type).

Example queries:

```
MATCH (n {stereotype:'RestController'}) RETURN n                      -- all controllers with full info
MATCH (s {stereotype:'Service'})-[:INJECTS]->(m) RETURN s.key, m.key  -- service dependency pairs
MATCH (a {key:'bean:orderController'})-[:INJECTS*1..2]->(b) RETURN DISTINCT b.key   -- transitive impact
MATCH (t:Type)-[:IMPLEMENTS]->(i {kind:'interface'}) RETURN t.key, i.key           -- implementations of an interface
MATCH (m:Method)-[:CALLS]->(n:Method {name:'greet'}) RETURN m.owner, m.name        -- callers of a method
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

`structure_extensions` toggles framework enrichment on top of the
language-agnostic structure layer. With `["springboot"]` (default) the component
surface is extracted; with `[]` (structure mode) only types, methods, and call
edges are extracted — useful for plain Java projects, and the summary line then
reports `{types} types, {methods} methods, {call_edges} call edges` instead of
component counts. Unknown values are ignored with a note in `perception.log`.

## Reliability

The analyzer's component extraction is pinned by a frozen fixture corpus with
ground-truth assertions, and is cross-validated release-over-release against a
reference analysis engine over the same corpus (a developer-side check; see
DEVELOPERS.md).

## Files

- `~/.scv/analysis/{project}/structure.json` — structure graph data (always, when analysis ran)
- `~/.scv/analysis/{project}/structure.vg` — query-file for the MCP tier (optional tier)
- `~/.scv/analysis/{project}/perception.log` — tier decisions and skip reasons

Deleting the analysis directory removes these files; the MCP query service stops
returning data for the project until the next `/scv run`.
