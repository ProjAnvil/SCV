# SCV Code Perception

SCV's code perception capability recognizes supported frameworks in a codebase and
extracts component dependency structure that `/scv run` uses to deepen the generated
architecture documentation — and that agents can query in later sessions.

**Supported: Spring Boot.** For a Spring Boot project, `/scv run` performs
deep structural analysis automatically: it maps components (services, controllers,
repositories, configuration components) and their dependency edges, detects circular
dependencies, and enriches ARCHITECTURE.md with a "Component Dependencies" section.

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
`get_node(repo, key)`, `refresh`. Node properties include the component kind
(`stereotype`: RestController / Service / Repository / Configuration / ...),
bean name, type, source file:line, and conditions; edges carry `INJECTS` with
via/qualifier annotations. Example queries:

```
MATCH (n {stereotype:'RestController'}) RETURN n                      -- all controllers with full info
MATCH (s {stereotype:'Service'})-[:INJECTS]->(m) RETURN s.key, m.key  -- service dependency pairs
MATCH (a {key:'bean:orderController'})-[:INJECTS*1..2]->(b) RETURN DISTINCT b.key   -- transitive impact
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
| `structure_build_execution` | `"local-only"` | build-tool execution policy: `local-only` (remote repos are analyzed in a no-build degraded mode), `always`, `never` |

## Files

- `~/.scv/analysis/{project}/structure.json` — component dependency data (always, when analysis ran)
- `~/.scv/analysis/{project}/structure.vg` — query-file for the MCP tier (optional tier)
- `~/.scv/analysis/{project}/perception.log` — tier decisions and skip reasons

Deleting the analysis directory removes these files; the MCP query service stops
returning data for the project until the next `/scv run`.
