// Package query tests run against the committed prebuilt snapshot
// testdata/demo.vg (8 nodes / 5 edges), generated once by the perception
// loader — never rebuilt during tests. Regenerate with (from the worktree
// root):
//
//	cd perception/loader && go run . \
//	  ../../engine/internal/query/testdata/demo-structure.json \
//	  ../../engine/internal/query/testdata/demo.vg
//
// demo.vg graph shape (INJECTS edges point dependency-first):
//
//	bean:orderController -INJECTS-> bean:orderService -INJECTS-> bean:orderRepository
//	bean:orderController -INJECTS-> bean:notificationService
//	type:com/demo/MemoryOrderStore -IMPLEMENTS-> type:com/demo/OrderStore
//	fn:com/demo/OrderController#place -CALLS-> fn:com/demo/OrderStore#save
package query

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const demoVG = "testdata/demo.vg"

// seedRepo copies the committed demo.vg snapshot into <root>/<name>/structure.vg,
// the layout every analysis root uses.
func seedRepo(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(demoVG)
	if err != nil {
		t.Fatalf("read fixture %s: %v", demoVG, err)
	}
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	vg := filepath.Join(dir, "structure.vg")
	if err := os.WriteFile(vg, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", vg, err)
	}
	return vg
}

// writeRegistry writes an analysis-dirs registry {"dirs": [...]}.
func writeRegistry(t *testing.T, path string, dirs ...string) {
	t.Helper()
	out, err := json.Marshal(map[string]any{"dirs": dirs})
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		t.Fatalf("write registry %s: %v", path, err)
	}
}

func openDemo(t *testing.T, registryPath, defaultRoot string) *Store {
	t.Helper()
	s, err := Open(registryPath, defaultRoot)
	if err != nil {
		t.Fatalf("Open(%q, %q): %v", registryPath, defaultRoot, err)
	}
	return s
}

func TestReposListsFixtureVg(t *testing.T) {
	home := t.TempDir()
	defaultRoot := filepath.Join(home, "analysis")
	vg := seedRepo(t, defaultRoot, "demo")
	// Registry absent: fresh install, default root only — not an error.
	s := openDemo(t, filepath.Join(home, "analysis-dirs.json"), defaultRoot)

	repos, err := s.Repos()
	if err != nil {
		t.Fatalf("Repos(): %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("Repos() = %d entries, want 1: %v", len(repos), repos)
	}
	r := repos[0]
	if r["repo"] != "demo" {
		t.Errorf("repo = %v, want demo", r["repo"])
	}
	if r["nodes"] != 8 {
		t.Errorf("nodes = %v, want 8", r["nodes"])
	}
	if r["edges"] != 5 {
		t.Errorf("edges = %v, want 5", r["edges"])
	}
	if file, _ := r["file"].(string); file != vg {
		t.Errorf("file = %v, want %s", r["file"], vg)
	}
}

func TestCypherCountMatchesNodes(t *testing.T) {
	home := t.TempDir()
	defaultRoot := filepath.Join(home, "analysis")
	seedRepo(t, defaultRoot, "demo")
	s := openDemo(t, filepath.Join(home, "analysis-dirs.json"), defaultRoot)

	// All nodes: count must match the snapshot's node count.
	res, err := s.Cypher("demo", "MATCH (n) RETURN n", 0)
	if err != nil {
		t.Fatalf("Cypher all nodes: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("Cypher result type %T, want map[string]any", res)
	}
	if m["count"] != 8 || m["total"] != 8 {
		t.Errorf("count/total = %v/%v, want 8/8", m["count"], m["total"])
	}
	if cols, _ := m["columns"].([]string); len(cols) != 1 || cols[0] != "n" {
		t.Errorf("columns = %v, want [n]", m["columns"])
	}
	rows, _ := m["rows"].([]map[string]any)
	if len(rows) != 8 {
		t.Fatalf("rows = %d, want 8", len(rows))
	}
	node, _ := rows[0]["n"].(map[string]any)
	if node == nil || node["key"] == nil || node["label"] == nil || node["props"] == nil {
		t.Errorf("node cell = %#v, want map with key/label/props", rows[0]["n"])
	}

	// Property filter through the real snapshot.
	res, err = s.Cypher("demo", "MATCH (n {stereotype:'Service'}) RETURN n", 0)
	if err != nil {
		t.Fatalf("Cypher stereotype filter: %v", err)
	}
	m = res.(map[string]any)
	if m["count"] != 2 {
		t.Errorf("Service count = %v, want 2 (orderService, notificationService)", m["count"])
	}

	// Edge cells come back as plain maps too.
	res, err = s.Cypher("demo", "MATCH (a)-[r:INJECTS]->(b) RETURN r", 0)
	if err != nil {
		t.Fatalf("Cypher edge return: %v", err)
	}
	m = res.(map[string]any)
	rows = m["rows"].([]map[string]any)
	if m["count"] != 3 || len(rows) != 3 {
		t.Fatalf("INJECTS count = %v (%d rows), want 3", m["count"], len(rows))
	}
	edge, _ := rows[0]["r"].(map[string]any)
	if edge == nil || edge["label"] != "INJECTS" || edge["src"] == nil || edge["dst"] == nil {
		t.Errorf("edge cell = %#v, want map with src/dst/label", rows[0]["r"])
	}

	// Plain scalar cells pass through unchanged.
	res, err = s.Cypher("demo", "MATCH (n {stereotype:'Service'}) RETURN n.beanName", 0)
	if err != nil {
		t.Fatalf("Cypher scalar return: %v", err)
	}
	if rows = res.(map[string]any)["rows"].([]map[string]any); len(rows) != 2 {
		t.Fatalf("scalar rows = %d, want 2", len(rows))
	}
	if v, ok := rows[0]["n.beanName"].(string); !ok || v == "" {
		t.Errorf("scalar cell = %#v, want non-empty string", rows[0]["n.beanName"])
	}

	// Limit is forwarded: truncated rows, total keeps the full count.
	res, err = s.Cypher("demo", "MATCH (n) RETURN n", 3)
	if err != nil {
		t.Fatalf("Cypher limited: %v", err)
	}
	m = res.(map[string]any)
	if m["count"] != 3 || m["total"] != 8 {
		t.Errorf("limited count/total = %v/%v, want 3/8", m["count"], m["total"])
	}

	// Malformed query surfaces vectra's error.
	if _, err := s.Cypher("demo", "MATCH (", 0); err == nil {
		t.Error("Cypher malformed query: want error, got nil")
	}
}

func TestImpactExcludesStart(t *testing.T) {
	home := t.TempDir()
	defaultRoot := filepath.Join(home, "analysis")
	seedRepo(t, defaultRoot, "demo")
	s := openDemo(t, filepath.Join(home, "analysis-dirs.json"), defaultRoot)

	keys := func(t *testing.T, res any) map[string]bool {
		t.Helper()
		m, ok := res.(map[string]any)
		if !ok {
			t.Fatalf("Impact result type %T, want map[string]any", res)
		}
		out := map[string]bool{}
		for _, n := range m["nodes"].([]map[string]any) {
			out[n["key"].(string)] = true
		}
		return out
	}

	// out: transitive INJECTS blast radius, start node never included.
	res, err := s.Impact("demo", "bean:orderController", "out", nil, 0)
	if err != nil {
		t.Fatalf("Impact out: %v", err)
	}
	got := keys(t, res)
	want := map[string]bool{
		"bean:orderService": true, "bean:orderRepository": true, "bean:notificationService": true,
	}
	if len(got) != len(want) {
		t.Errorf("Impact out = %v, want %v", got, want)
	}
	for k := range want {
		if !got[k] {
			t.Errorf("Impact out missing %q (got %v)", k, got)
		}
	}
	if got["bean:orderController"] {
		t.Error("Impact out includes the start node")
	}

	// Empty direction defaults to out (graphmcp semantics).
	res, err = s.Impact("demo", "bean:orderService", "", nil, 0)
	if err != nil {
		t.Fatalf("Impact default dir: %v", err)
	}
	if got = keys(t, res); len(got) != 1 || !got["bean:orderRepository"] {
		t.Errorf("Impact default dir = %v, want [bean:orderRepository]", got)
	}

	// in: everything the node depends on.
	res, err = s.Impact("demo", "bean:orderRepository", "in", nil, 0)
	if err != nil {
		t.Fatalf("Impact in: %v", err)
	}
	if got = keys(t, res); len(got) != 2 || !got["bean:orderService"] || !got["bean:orderController"] {
		t.Errorf("Impact in = %v, want [bean:orderService bean:orderController]", got)
	}

	// both: union of in and out, still excluding the start node.
	res, err = s.Impact("demo", "bean:orderService", "both", nil, 0)
	if err != nil {
		t.Fatalf("Impact both: %v", err)
	}
	if got = keys(t, res); len(got) != 2 || !got["bean:orderRepository"] || !got["bean:orderController"] {
		t.Errorf("Impact both = %v, want [bean:orderRepository bean:orderController]", got)
	}

	// Labels filter edge families: no CALLS edge leaves the controller bean.
	res, err = s.Impact("demo", "bean:orderController", "out", []string{"CALLS"}, 0)
	if err != nil {
		t.Fatalf("Impact labels CALLS: %v", err)
	}
	if m := res.(map[string]any); m["count"] != 0 {
		t.Errorf("Impact labels CALLS count = %v, want 0", m["count"])
	}

	// Limit caps the node list while total keeps the full blast radius.
	res, err = s.Impact("demo", "bean:orderController", "out", []string{"INJECTS"}, 2)
	if err != nil {
		t.Fatalf("Impact limited: %v", err)
	}
	if m := res.(map[string]any); m["count"] != 2 || m["total"] != 3 {
		t.Errorf("Impact limited count/total = %v/%v, want 2/3", m["count"], m["total"])
	}

	// Unknown direction and unknown start node are errors.
	if _, err := s.Impact("demo", "bean:orderController", "sideways", nil, 0); err == nil {
		t.Error("Impact invalid direction: want error, got nil")
	}
	if _, err := s.Impact("demo", "bean:missing", "out", nil, 0); err == nil {
		t.Error("Impact unknown key: want error, got nil")
	}
}

func TestRegistryUnionTwoRoots(t *testing.T) {
	home := t.TempDir()
	defaultRoot := filepath.Join(home, "analysis")
	seedRepo(t, defaultRoot, "demo")
	extra := filepath.Join(home, "elsewhere")
	seedRepo(t, extra, "alpha")
	registry := filepath.Join(home, "analysis-dirs.json")
	writeRegistry(t, registry, extra)

	s := openDemo(t, registry, defaultRoot)
	repos, err := s.Repos()
	if err != nil {
		t.Fatalf("Repos(): %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("Repos() = %d entries, want 2 (union of registry root + default root): %v", len(repos), repos)
	}
	if repos[0]["repo"] != "alpha" || repos[1]["repo"] != "demo" {
		t.Errorf("repos = %v, want sorted [alpha demo]", repos)
	}

	// Both roots are queryable through the same store.
	if _, err := s.Node("alpha", "bean:orderService"); err != nil {
		t.Errorf("Node in registry-root repo: %v", err)
	}
}

func TestUnknownRepoErrors(t *testing.T) {
	home := t.TempDir()
	defaultRoot := filepath.Join(home, "analysis")
	seedRepo(t, defaultRoot, "demo")
	s := openDemo(t, filepath.Join(home, "analysis-dirs.json"), defaultRoot)

	if _, err := s.Cypher("nope", "MATCH (n) RETURN n", 0); err == nil {
		t.Error("Cypher unknown repo: want error, got nil")
	}
	if _, err := s.Node("nope", "bean:orderService"); err == nil {
		t.Error("Node unknown repo: want error, got nil")
	}
	if _, err := s.Impact("nope", "bean:orderService", "out", nil, 0); err == nil {
		t.Error("Impact unknown repo: want error, got nil")
	}
}

func TestNodeReturnsKeyLabelProps(t *testing.T) {
	home := t.TempDir()
	defaultRoot := filepath.Join(home, "analysis")
	seedRepo(t, defaultRoot, "demo")
	s := openDemo(t, filepath.Join(home, "analysis-dirs.json"), defaultRoot)

	n, err := s.Node("demo", "bean:orderService")
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if n["key"] != "bean:orderService" || n["label"] != "Bean" {
		t.Errorf("node = %v, want key bean:orderService label Bean", n)
	}
	props, _ := n["props"].(map[string]any)
	if props["stereotype"] != "Service" {
		t.Errorf("props = %v, want stereotype Service", n["props"])
	}

	if _, err := s.Node("demo", "bean:missing"); err == nil {
		t.Error("Node unknown key: want error, got nil")
	} else if !strings.Contains(err.Error(), "bean:missing") {
		t.Errorf("Node unknown key error = %v, want it to name the key", err)
	}
}

func TestOpenCorruptRegistryErrors(t *testing.T) {
	home := t.TempDir()
	registry := filepath.Join(home, "analysis-dirs.json")
	if err := os.WriteFile(registry, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write corrupt registry: %v", err)
	}
	if _, err := Open(registry, filepath.Join(home, "analysis")); err == nil {
		t.Error("Open with corrupt registry: want error, got nil")
	}
}

func TestRefreshPicksUpNewRepos(t *testing.T) {
	home := t.TempDir()
	defaultRoot := filepath.Join(home, "analysis")
	seedRepo(t, defaultRoot, "demo")
	registry := filepath.Join(home, "analysis-dirs.json")
	s := openDemo(t, registry, defaultRoot)

	// A new run registers another root after the store was opened.
	extra := filepath.Join(home, "run-42")
	seedRepo(t, extra, "beta")
	writeRegistry(t, registry, extra)

	s.Refresh()
	repos, err := s.Repos()
	if err != nil {
		t.Fatalf("Repos(): %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("Repos() after Refresh = %d entries, want 2 (demo + beta): %v", len(repos), repos)
	}
	if _, err := s.Node("beta", "bean:orderService"); err != nil {
		t.Errorf("Node in post-Refresh repo: %v", err)
	}

	// A vanished snapshot disappears from the listing on refresh.
	if err := os.RemoveAll(filepath.Join(defaultRoot, "demo")); err != nil {
		t.Fatalf("remove demo: %v", err)
	}
	s.Refresh()
	if repos, err = s.Repos(); err != nil {
		t.Fatalf("Repos() after removal: %v", err)
	}
	if len(repos) != 1 || repos[0]["repo"] != "beta" {
		t.Errorf("Repos() after removal = %v, want [beta]", repos)
	}
}
