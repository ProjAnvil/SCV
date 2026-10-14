package main

import (
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

func TestStoreListAndCypher(t *testing.T) {
	dir := t.TempDir()
	for _, repo := range []string{"alpha", "beta"} {
		os.MkdirAll(filepath.Join(dir, repo), 0o755)
		buildVG(t, filepath.Join(dir, repo, "structure.vg"))
	}
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
		t.Fatalf("stats: %+v", repos[0])
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
