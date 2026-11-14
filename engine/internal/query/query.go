// Package query serves structure-graph queries (repo listing, Cypher,
// node lookup, blast-radius impact) in-process over vectra snapshots.
//
// The store aggregates every analysis root: the dirs registered in the
// analysis-dirs registry (engine internal/run AppendAnalysisDir, spec
// section 7) plus the default root (<scvHome>/analysis), which is always
// scanned even on fresh installs where the registry does not exist yet.
// Each root holds snapshots at <root>/<repo>/structure.vg.
//
// The vectra call surface (LoadFile, Cypher, NodeByKey, Reachable, Stats)
// and the mutex-guarded lazy per-repo load are a direct port of
// perception/graphmcp's store and tool handlers — no new graph logic.
package query

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/projanvil/vectra"
)

// defaultRowLimit caps Cypher rows and impact nodes when the caller passes
// limit <= 0 (graphmcp's default).
const defaultRowLimit = 1000

// snapshotName is the per-repo file every analysis root produces.
const snapshotName = "structure.vg"

// Store is the query surface over all registered analysis roots. The zero
// value is not usable; call Open.
type Store struct {
	registryPath string
	defaultRoot  string

	mu     sync.Mutex
	files  map[string]string // repo name -> structure.vg path
	graphs map[string]*vectra.Graph
}

// Open reads the analysis-dirs registry at registryPath and returns a store
// scanning the union of the registry dirs and defaultRoot. A missing
// registry is a fresh install (empty dir list); a registry that exists but
// does not parse is corruption and an error, matching AppendAnalysisDir's
// semantics. Roots that do not exist yet are skipped, not errors.
func Open(registryPath, defaultRoot string) (*Store, error) {
	s := &Store{
		registryPath: registryPath,
		defaultRoot:  defaultRoot,
		files:        map[string]string{},
		graphs:       map[string]*vectra.Graph{},
	}
	if _, err := s.roots(); err != nil {
		return nil, err
	}
	s.rescan()
	return s, nil
}

// roots lists the scan roots: registry dirs in append order, deduped, with
// the default root always last (it wins on repo-name collisions).
func (s *Store) roots() ([]string, error) {
	dirs, err := readRegistry(s.registryPath)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(dirs)+1)
	roots := make([]string, 0, len(dirs)+1)
	for _, d := range append(dirs, s.defaultRoot) {
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		roots = append(roots, d)
	}
	return roots, nil
}

// readRegistry parses {"dirs": [...]} at path. A missing file is an empty
// list (fresh install); unparseable content is corruption.
func readRegistry(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read registry %s: %w", path, err)
	}
	var reg struct {
		Dirs []string `json:"dirs"`
	}
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parse registry %s: %w", path, err)
	}
	return reg.Dirs, nil
}

// rescan refreshes the repo -> snapshot mapping across all roots (cheap:
// directory walks only) and drops cached graphs whose snapshot disappeared.
// Unreadable roots are skipped; on a registry read error the previous
// mapping is kept (Refresh cannot surface errors).
func (s *Store) rescan() {
	roots, err := s.roots()
	if err != nil {
		return
	}
	files := map[string]string{}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue // root absent (fresh install) or unreadable: skip
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			vg := filepath.Join(root, e.Name(), snapshotName)
			if st, err := os.Stat(vg); err == nil && !st.IsDir() {
				files[e.Name()] = vg
			}
		}
	}
	s.mu.Lock()
	s.files = files
	for name := range s.graphs {
		if _, ok := files[name]; !ok {
			delete(s.graphs, name)
		}
	}
	s.mu.Unlock()
}

// graph returns the loaded graph for a repo, loading (and caching) on
// first use. One rescan covers repos analyzed after Open.
func (s *Store) graph(name string) (*vectra.Graph, error) {
	s.mu.Lock()
	path, known := s.files[name]
	cached, loaded := s.graphs[name]
	s.mu.Unlock()
	if loaded && known {
		return cached, nil
	}
	if !known {
		s.rescan()
		s.mu.Lock()
		path, known = s.files[name]
		s.mu.Unlock()
		if !known {
			return nil, fmt.Errorf("unknown repo %q", name)
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

// Repos rescans every root and lists the available repositories with their
// snapshot paths and node/edge counts, sorted by repo name.
func (s *Store) Repos() ([]map[string]any, error) {
	s.rescan()
	s.mu.Lock()
	names := make([]string, 0, len(s.files))
	for n := range s.files {
		names = append(names, n)
	}
	s.mu.Unlock()
	sort.Strings(names)
	out := make([]map[string]any, 0, len(names))
	for _, n := range names {
		info := map[string]any{"repo": n, "file": s.filePath(n)}
		if g, err := s.graph(n); err == nil {
			st := g.Stats()
			info["nodes"], info["edges"] = st.Nodes, st.Edges
		}
		out = append(out, info)
	}
	return out, nil
}

func (s *Store) filePath(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.files[name]
}

// Cypher runs a Cypher-subset statement against one repo's snapshot and
// returns {"columns", "rows", "count", "total"}; limit <= 0 (or above the
// default cap) means the default row limit, and total always carries the
// untruncated row count. Node and Edge cells become plain maps so callers
// get stable JSON shapes.
func (s *Store) Cypher(repo, q string, limit int) (any, error) {
	g, err := s.graph(repo)
	if err != nil {
		return nil, err
	}
	res, err := g.Cypher(q)
	if err != nil {
		return nil, err
	}
	limit = clampLimit(limit)
	rows := res.Rows
	total := len(rows)
	if len(rows) > limit {
		rows = rows[:limit]
	}
	clean := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		m := make(map[string]any, len(r))
		for k, v := range r {
			m[k] = jsonable(v)
		}
		clean = append(clean, m)
	}
	return map[string]any{
		"columns": res.Columns, "rows": clean,
		"count": len(clean), "total": total,
	}, nil
}

// Node returns the full node (key, label, props) for repo + key.
func (s *Store) Node(repo, key string) (map[string]any, error) {
	g, err := s.graph(repo)
	if err != nil {
		return nil, err
	}
	n, ok := g.NodeByKey(key)
	if !ok {
		return nil, fmt.Errorf("node %q not found in repo %q", key, repo)
	}
	return map[string]any{"key": n.Key, "label": n.Label, "props": n.Props}, nil
}

// Impact is blast-radius analysis: every node transitively reachable from
// key, excluding the start node itself, as {"start", "direction",
// "nodes", "count", "total"}. Direction is "out" (default; what changing
// this node affects), "in" (what it depends on) or "both" (union — vectra
// has no DirBoth, so both is the union of the two traversals). labels
// optionally restricts traversal to those edge labels; limit <= 0 (or
// above the default cap) means the default node limit, and total always
// carries the untruncated count.
func (s *Store) Impact(repo, key, dir string, labels []string, limit int) (any, error) {
	g, err := s.graph(repo)
	if err != nil {
		return nil, err
	}
	if _, ok := g.NodeByKey(key); !ok {
		return nil, fmt.Errorf("node %q not found in repo %q", key, repo)
	}
	switch dir {
	case "", "out", "in", "both":
	default:
		return nil, fmt.Errorf("invalid direction %q (want out, in or both)", dir)
	}

	keys := map[string]bool{}
	collect := func(d vectra.Direction) error {
		opts := []vectra.ReachOption{vectra.ReachWithDirection(d)}
		if len(labels) > 0 {
			opts = append(opts, vectra.ReachWithLabels(labels...))
		}
		reached, err := g.Reachable(key, opts...)
		if err != nil {
			return err
		}
		// Reachable includes the start node; the blast radius does not.
		for _, k := range reached {
			if k != key {
				keys[k] = true
			}
		}
		return nil
	}
	if dir == "" || dir == "out" || dir == "both" {
		if err := collect(vectra.DirOut); err != nil {
			return nil, err
		}
	}
	if dir == "in" || dir == "both" {
		if err := collect(vectra.DirIn); err != nil {
			return nil, err
		}
	}

	filtered := make([]string, 0, len(keys))
	for k := range keys {
		filtered = append(filtered, k)
	}
	sort.Strings(filtered) // deterministic output regardless of traversal order

	limit = clampLimit(limit)
	total := len(filtered)
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	nodes := make([]map[string]any, 0, len(filtered))
	for _, k := range filtered {
		if n, ok := g.NodeByKey(k); ok {
			nodes = append(nodes, map[string]any{"key": n.Key, "label": n.Label})
		}
	}
	return map[string]any{
		"start": key, "direction": dir, "nodes": nodes,
		"count": len(nodes), "total": total,
	}, nil
}

// Refresh re-reads the registry and rescans every root for new or removed
// structure.vg files (after a new run). Errors are not reportable through
// this signature; unreadable roots are skipped and a corrupt registry
// keeps the previous snapshot set.
func (s *Store) Refresh() {
	s.rescan()
}

// clampLimit mirrors graphmcp: limit <= 0 or above the default cap falls
// back to the default cap.
func clampLimit(limit int) int {
	if limit <= 0 || limit > defaultRowLimit {
		return defaultRowLimit
	}
	return limit
}

// jsonable converts vectra Node/Edge cells into plain maps so callers see
// stable shapes (same conversion as graphmcp's handlers).
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
