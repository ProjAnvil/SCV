// Command scv-graph-mcp serves every analyzed repository's structure graph
// (~/.scv/analysis/*/structure.vg) over MCP: one registration, repo-scoped
// Cypher queries, node lookup, and a repo listing.
//
// Transport modes:
//
//	scv-graph-mcp                       # stdio (Claude Code / Claude Desktop)
//	scv-graph-mcp -http :8080           # Streamable HTTP at /mcp
//	scv-graph-mcp -analysis-dir <dir>   # default ~/.scv/analysis
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/projanvil/vectra"
)

const defaultRowLimit = 1000

// ---- repo store: scan + lazy load ----

type repoInfo struct {
	Name  string `json:"repo"`
	Nodes int    `json:"nodes"`
	Edges int    `json:"edges"`
	File  string `json:"file"`
}

type store struct {
	dir    string
	mu     sync.Mutex
	files  map[string]string // repo name -> structure.vg path
	graphs map[string]*vectra.Graph
}

func newStore(dir string) *store {
	return &store{dir: dir, files: map[string]string{}, graphs: map[string]*vectra.Graph{}}
}

// rescan refreshes the repo -> snapshot mapping (cheap: directory walk only).
func (s *store) rescan() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	files := map[string]string{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		vg := filepath.Join(s.dir, e.Name(), "structure.vg")
		if st, err := os.Stat(vg); err == nil && !st.IsDir() {
			files[e.Name()] = vg
		}
	}
	s.mu.Lock()
	s.files = files
	// drop cached graphs whose snapshot disappeared
	for name := range s.graphs {
		if _, ok := files[name]; !ok {
			delete(s.graphs, name)
		}
	}
	s.mu.Unlock()
	return nil
}

// graph returns the sealed graph for a repo, loading (and caching) on first use.
func (s *store) graph(name string) (*vectra.Graph, error) {
	s.mu.Lock()
	path, known := s.files[name]
	cached, loaded := s.graphs[name]
	s.mu.Unlock()
	if loaded && known {
		return cached, nil
	}
	if !known {
		// one rescan covers analyze-after-startup
		if err := s.rescan(); err != nil {
			return nil, err
		}
		s.mu.Lock()
		path, known = s.files[name]
		s.mu.Unlock()
		if !known {
			return nil, fmt.Errorf("unknown repo %q — call repos() to list available", name)
		}
	}
	g, err := vectra.LoadFile(path)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.graphs[name] = g
	s.mu.Unlock()
	return g, nil
}

func (s *store) list() ([]repoInfo, error) {
	if err := s.rescan(); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(s.files))
	for n := range s.files {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]repoInfo, 0, len(names))
	for _, n := range names {
		info := repoInfo{Name: n, File: s.files[n]}
		if g, err := s.graph(n); err == nil {
			st := g.Stats()
			info.Nodes, info.Edges = st.Nodes, st.Edges
		}
		out = append(out, info)
	}
	return out, nil
}

// ---- MCP wiring ----

type service struct {
	store *store
}

type reposOut struct {
	Repos []repoInfo `json:"repos"`
}

func (svc *service) repos(_ context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, reposOut, error) {
	repos, err := svc.store.list()
	if err != nil {
		return nil, reposOut{}, err
	}
	return nil, reposOut{Repos: repos}, nil
}

type cypherIn struct {
	Repo  string `json:"repo" jsonschema:"repository name from repos()"`
	Query string `json:"query" jsonschema:"Cypher-subset statement, e.g. MATCH (n {stereotype:'RestController'}) RETURN n"`
	Limit int    `json:"limit,omitempty" jsonschema:"max rows returned, default 1000"`
}

func (svc *service) cypher(_ context.Context, _ *mcp.CallToolRequest, in cypherIn) (*mcp.CallToolResult, any, error) {
	g, err := svc.store.graph(in.Repo)
	if err != nil {
		return nil, nil, err
	}
	res, err := g.Cypher(in.Query)
	if err != nil {
		return nil, nil, err
	}
	limit := in.Limit
	if limit <= 0 || limit > defaultRowLimit {
		limit = defaultRowLimit
	}
	rows := res.Rows
	total := len(rows)
	if len(rows) > limit {
		rows = rows[:limit]
	}
	// Node/Edge cells become plain maps so JSON clients see stable shapes.
	clean := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		m := make(map[string]any, len(r))
		for k, v := range r {
			m[k] = jsonable(v)
		}
		clean = append(clean, m)
	}
	return nil, map[string]any{
		"columns": res.Columns, "rows": clean,
		"count": len(clean), "total": total,
	}, nil
}

func jsonable(v any) any {
	switch t := v.(type) {
	case vectra.Node:
		return map[string]any{"key": t.Key, "label": t.Label, "props": t.Props}
	case vectra.Edge:
		return map[string]any{
			"src": t.Src, "dst": t.Dst, "label": t.Label, "props": t.Props,
		}
	default:
		return v
	}
}

type nodeIn struct {
	Repo string `json:"repo"`
	Key  string `json:"key" jsonschema:"node key, e.g. bean:orderController"`
}

func (svc *service) getNode(_ context.Context, _ *mcp.CallToolRequest, in nodeIn) (*mcp.CallToolResult, any, error) {
	g, err := svc.store.graph(in.Repo)
	if err != nil {
		return nil, nil, err
	}
	n, ok := g.NodeByKey(in.Key)
	if !ok {
		return nil, nil, fmt.Errorf("node %q not found in repo %q", in.Key, in.Repo)
	}
	return nil, map[string]any{"key": n.Key, "label": n.Label, "props": n.Props}, nil
}

func (svc *service) refresh(_ context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	if err := svc.store.rescan(); err != nil {
		return nil, nil, err
	}
	return nil, map[string]any{"repos": len(svc.store.files)}, nil
}

func newServer(svc *service) *mcp.Server {
	s := mcp.NewServer(
		&mcp.Implementation{Name: "scv-graph-mcp", Version: "0.1.0"},
		nil,
	)
	mcp.AddTool(s, &mcp.Tool{
		Name: "repos", Description: "List analyzed repositories with structure graphs (repo name, node/edge counts).",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, svc.repos)
	mcp.AddTool(s, &mcp.Tool{
		Name: "cypher",
		Description: "Run a Cypher-subset query against one repository's structure graph. " +
			"Node labels and key shapes: " +
			"Bean (key 'bean:<name>', props: beanName, type, stereotype (RestController/Service/Repository/Configuration/...), conditions, source); " +
			"Type (key 'type:<fqn>', props: kind (class/interface/record/enum/annotation), modifiers, loc); " +
			"Method (key 'fn:<fqn>#<name>(<params>)', props: name, visibility, static, params, returns, owner (Type key), loc); " +
			"File (key 'file:<repo-relative path>', props: path, language, loc). " +
			"Edge labels: INJECTS (Bean→Bean, props via/qualifier/wraps/ambiguous); " +
			"CALLS (Method→Method, props line, kind direct|virtual-dispatch); " +
			"IMPLEMENTS and EXTENDS (Type→Type); CONTAINS (File→Type); REFERENCES (Method→Type, prop line). " +
			"Examples: " +
			"MATCH (n {stereotype:'Service'})-[:INJECTS]->(m) RETURN n.key, m.key; " +
			"MATCH (t:Type)-[:IMPLEMENTS]->(i {kind:'interface'}) RETURN t.key, i.key; " +
			"MATCH (m:Method)-[:CALLS]->(n:Method {name:'greet'}) RETURN m.owner, m.name",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, svc.cypher)
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_node", Description: "Full node info (key, label, props) by repo + key.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, svc.getNode)
	mcp.AddTool(s, &mcp.Tool{
		Name: "refresh", Description: "Rescan the analysis directory for new structure.vg files (after a new /scv run).",
	}, svc.refresh)
	return s
}

func main() {
	httpAddr := flag.String("http", "", "HTTP listen address (empty = stdio mode)")
	dir := flag.String("analysis-dir", defaultAnalysisDir(), "directory containing <repo>/structure.vg")
	flag.Parse()

	svc := &service{store: newStore(*dir)}
	if err := svc.store.rescan(); err != nil {
		fmt.Fprintf(os.Stderr, "scv-graph-mcp: analysis dir: %v\n", err)
	}
	s := newServer(svc)
	if *httpAddr == "" {
		if err := s.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return newServer(svc)
	}, nil))
	fmt.Fprintf(os.Stderr, "scv-graph-mcp listening on http://%s/mcp (analysis dir: %s, %d repo(s) at %s)\n",
		*httpAddr, *dir, len(svc.store.files), time.Now().Format(time.RFC3339))
	if err := http.ListenAndServe(*httpAddr, mux); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func defaultAnalysisDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".scv/analysis"
	}
	return filepath.Join(home, ".scv", "analysis")
}
