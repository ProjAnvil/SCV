package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/projanvil/vectra"
)

// buildVG writes a small structure graph as a .vg snapshot the way the
// perception loader does (string indexes registered before Seal).
func buildVG(t *testing.T, path string) {
	t.Helper()
	g := vectra.New()
	for _, k := range []string{"stereotype", "beanName", "type"} {
		g.IndexProperty(k)
	}
	g.AddNode("bean:orderController", "Bean", map[string]any{"stereotype": "RestController", "beanName": "orderController"})
	g.AddNode("bean:orderService", "Bean", map[string]any{"stereotype": "Service", "beanName": "orderService"})
	g.AddNode("bean:appConfig", "Bean", map[string]any{"stereotype": "Configuration", "beanName": "appConfig"})
	g.UpsertEdge("bean:orderController", "bean:orderService", "INJECTS", map[string]any{"via": "constructor"})
	if err := g.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := g.SaveFile(path); err != nil {
		t.Fatal(err)
	}
}

// buildBetaVG writes a DIFFERENT graph so cross-repo isolation is assertable.
func buildBetaVG(t *testing.T, path string) {
	t.Helper()
	g := vectra.New()
	g.IndexProperty("stereotype")
	g.AddNode("bean:nightScheduler", "Bean", map[string]any{"stereotype": "Scheduler", "beanName": "nightScheduler"})
	if err := g.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := g.SaveFile(path); err != nil {
		t.Fatal(err)
	}
}

func TestStoreListAndCypher(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "alpha"), 0o755)
	os.MkdirAll(filepath.Join(dir, "beta"), 0o755)
	buildVG(t, filepath.Join(dir, "alpha", "structure.vg"))
	buildBetaVG(t, filepath.Join(dir, "beta", "structure.vg"))
	// a repo without a graph must be ignored
	os.MkdirAll(filepath.Join(dir, "empty"), 0o755)

	s := newStore(dir)
	if err := s.rescan(); err != nil {
		t.Fatal(err)
	}
	repos, err := s.list()
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 || repos[0].Name != "alpha" || repos[1].Name != "beta" {
		t.Fatalf("repos: %+v", repos)
	}
	if repos[0].Nodes != 3 || repos[0].Edges != 1 {
		t.Fatalf("alpha stats: %+v", repos[0])
	}
	if repos[1].Nodes != 1 || repos[1].Edges != 0 {
		t.Fatalf("beta stats: %+v", repos[1])
	}

	g, err := s.graph("alpha")
	if err != nil {
		t.Fatal(err)
	}
	res, err := g.Cypher(`MATCH (n {stereotype:'RestController'}) RETURN n`)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("rows: %+v", res.Rows)
	}
	node := res.Rows[0]["n"].(vectra.Node)
	if node.Key != "bean:orderController" {
		t.Fatalf("node: %+v", node)
	}

	// cross-repo isolation: beta's Scheduler does not leak into alpha
	alpha, err := s.graph("alpha")
	if err != nil {
		t.Fatal(err)
	}
	beta, err := s.graph("beta")
	if err != nil {
		t.Fatal(err)
	}
	alphaSched, err := alpha.Cypher(`MATCH (n {stereotype:'Scheduler'}) RETURN n`)
	if err != nil {
		t.Fatal(err)
	}
	if len(alphaSched.Rows) != 0 {
		t.Fatalf("beta node leaked into alpha: %+v", alphaSched.Rows)
	}
	betaSched, err := beta.Cypher(`MATCH (n {stereotype:'Scheduler'}) RETURN n`)
	if err != nil {
		t.Fatal(err)
	}
	if len(betaSched.Rows) != 1 {
		t.Fatalf("beta Scheduler missing: %+v", betaSched.Rows)
	}
}

func TestUnknownRepoError(t *testing.T) {
	s := newStore(t.TempDir())
	if _, err := s.graph("nope"); err == nil {
		t.Fatal("expected error for unknown repo")
	}
}

func TestCypherServesFromSnapshotIndexes(t *testing.T) {
	// v3 snapshots persist index declarations: a freshly loaded snapshot must
	// answer property-anchored Cypher without any re-registration (the bug we
	// reported upstream, fixed in vectra v0.5).
	dir := t.TempDir()
	vg := filepath.Join(dir, "structure.vg")
	buildVG(t, vg)
	g, err := vectra.LoadFile(vg)
	if err != nil {
		t.Fatal(err)
	}
	res, err := g.Cypher(`MATCH (n {stereotype:'Configuration'}) RETURN n.key`)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 1 || res.Rows[0]["n.key"] != "bean:appConfig" {
		t.Fatalf("rows: %+v", res.Rows)
	}
}

// buildPolyglotVG writes a multi-language snapshot (java + python nodes in
// one graph, spec §8) with an IMPORTS edge — the v3 shape graphmcp serves.
// Note: no "lang" index is registered (spec §9(b) downgrade — unindexed
// {prop} queries fall back to a node scan on the loaded snapshot).
func buildPolyglotVG(t *testing.T, path string) {
	t.Helper()
	g := vectra.New()
	g.AddNode("type:com.example.User", "Type", map[string]any{"lang": "java", "kind": "class"})
	g.AddNode("type:models.user.User", "Type", map[string]any{"lang": "python", "kind": "class"})
	g.AddNode("file:src/main/java/com/example/User.java", "File",
		map[string]any{"lang": "java", "language": "java", "path": "src/main/java/com/example/User.java"})
	g.AddNode("file:models/user.py", "File",
		map[string]any{"lang": "python", "language": "python", "path": "models/user.py"})
	g.AddNode("file:app.py", "File",
		map[string]any{"lang": "python", "language": "python", "path": "app.py"})
	g.UpsertEdge("file:app.py", "file:models/user.py", "IMPORTS", map[string]any{})
	if err := g.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := g.SaveFile(path); err != nil {
		t.Fatal(err)
	}
}

// TestMultiLangQueryMatrix covers the v3 multi-language surface (spec §9):
// lang-anchored Cypher on a loaded snapshot, IMPORTS traversal, get_node on
// a python Type node, and cross-repo isolation with the polyglot graph
// alongside the java-only beta graph.
func TestMultiLangQueryMatrix(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "alpha"), 0o755)
	os.MkdirAll(filepath.Join(dir, "beta"), 0o755)
	buildPolyglotVG(t, filepath.Join(dir, "alpha", "structure.vg"))
	buildBetaVG(t, filepath.Join(dir, "beta", "structure.vg"))

	svc := &service{store: newStore(dir)}
	if err := svc.store.rescan(); err != nil {
		t.Fatal(err)
	}

	g, err := svc.store.graph("alpha")
	if err != nil {
		t.Fatal(err)
	}

	// lang-anchored query returns ONLY python nodes (client-side count —
	// the Cypher subset has no count()/DISTINCT)
	py, err := g.Cypher(`MATCH (n {lang:'python'}) RETURN n`)
	if err != nil {
		t.Fatal(err)
	}
	if len(py.Rows) != 3 { // type:models.user.User + file:models/user.py + file:app.py
		t.Fatalf("python rows: %d, want 3", len(py.Rows))
	}
	for _, r := range py.Rows {
		if n, ok := r["n"].(vectra.Node); !ok || n.Props["lang"] != "python" {
			t.Fatalf("non-python node in result: %+v", r["n"])
		}
	}
	java, err := g.Cypher(`MATCH (n {lang:'java'}) RETURN n`)
	if err != nil {
		t.Fatal(err)
	}
	if len(java.Rows) != 2 { // type:com.example.User + its File
		t.Fatalf("java rows: %d, want 2", len(java.Rows))
	}

	// IMPORTS traversal
	imp, err := g.Cypher(`MATCH (f:File)-[:IMPORTS]->(g) RETURN f.key, g.key`)
	if err != nil {
		t.Fatal(err)
	}
	if len(imp.Rows) != 1 || imp.Rows[0]["f.key"] != "file:app.py" || imp.Rows[0]["g.key"] != "file:models/user.py" {
		t.Fatalf("IMPORTS traversal: %+v", imp.Rows)
	}

	// get_node on a python Type node
	_, out, err := svc.getNode(context.Background(), nil, nodeIn{Repo: "alpha", Key: "type:models.user.User"})
	if err != nil {
		t.Fatal(err)
	}
	node, ok := out.(map[string]any)
	if !ok || node["key"] != "type:models.user.User" {
		t.Fatalf("get_node: %+v", out)
	}
	if node["props"].(map[string]any)["lang"] != "python" {
		t.Fatalf("get_node props.lang: %+v", node["props"])
	}

	// cross-repo isolation: alpha's polyglot graph does not leak into beta
	beta, err := svc.store.graph("beta")
	if err != nil {
		t.Fatal(err)
	}
	betaPy, err := beta.Cypher(`MATCH (n {lang:'python'}) RETURN n`)
	if err != nil {
		t.Fatal(err)
	}
	if len(betaPy.Rows) != 0 {
		t.Fatalf("alpha python nodes leaked into beta: %+v", betaPy.Rows)
	}
}
