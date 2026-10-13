# SCV Code Perception

SCV's code perception capability recognizes supported frameworks in a codebase and
extracts component dependency structure that `/scv run` uses to deepen the generated
architecture documentation — and that agents can query in later sessions.

**Supported: Spring Boot.** For a Spring Boot project, `/scv run` performs
deep structural analysis automatically: it maps components (services, controllers,
repositories, configuration beans) and their dependency edges, detects circular
dependencies, and enriches ARCHITECTURE.md with a "Component Dependencies" section.

## Optional query tier

When a Go 1.27+ runtime and the `vectra-mcp` tool are available, `/scv run` also
generates a structure-graph query file (`structure.vg`) and prints the command to
register it as an MCP service (`scv-structure`), enabling dependency queries
(multi-hop reachability, impact radius, cycles) in future sessions. Install the
tool with: `go install github.com/projanvil/vectra/mcp/cmd/vectra-mcp@latest`

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
