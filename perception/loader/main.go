// Command loader converts a structure.json (SCV Code Perception artifact schema)
// into a vectra snapshot (.vg) and grades bean dependency cycles (INJECTS
// edges only; see grade for the filter rationale).
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/projanvil/vectra"
)

type nodeJ struct {
	Key   string         `json:"key"`
	Label string         `json:"label"`
	Props map[string]any `json:"props"`
}

type edgeJ struct {
	SrcKey string         `json:"srcKey"`
	DstKey string         `json:"dstKey"`
	Label  string         `json:"label"`
	Props  map[string]any `json:"props"`
}

type graphJ struct {
	Nodes []nodeJ `json:"nodes"`
	Edges []edgeJ `json:"edges"`
}

type cycleOut struct {
	Nodes []string `json:"nodes"`
	Grade string   `json:"grade"`
}

type resultOut struct {
	Nodes  int        `json:"nodes"`
	Edges  int        `json:"edges"`
	Cycles []cycleOut `json:"cycles"`
}

// fatalVia edges are the ones Spring cannot satisfy in a cycle
// (constructor wiring; see perception/DEVELOPERS.md artifact schema).
var fatalVia = map[string]bool{
	"constructor": true, "lombok-constructor": true, "beanMethod": true,
}

// loadGraph builds the vectra graph from the artifact JSON exactly as
// emitted (indexes registered before Seal) and seals it. Cycle grading and
// snapshot saving are both read-only on the sealed result.
func loadGraph(jsonPath string) (*vectra.Graph, int) {
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		fatal(err)
	}
	var g graphJ
	if err := json.Unmarshal(raw, &g); err != nil {
		fatal(err)
	}
	gr := vectra.New()
	// No "lang" index declaration needed (spec §9(b), downgraded after the
	// T7 spike): vectra's Cypher falls back to a node scan for unindexed
	// {prop} keys, so MATCH (n {lang:'python'}) hits on snapshots as-is.
	for _, k := range []string{"stereotype", "beanName", "type", "conditions"} {
		gr.IndexProperty(k)
	}
	for _, n := range g.Nodes {
		_, err := gr.AddNode(n.Key, n.Label, n.Props)
		must(err)
	}
	for _, e := range g.Edges {
		_, err := gr.UpsertEdge(e.SrcKey, e.DstKey, e.Label, e.Props)
		must(err)
	}
	must(gr.Seal())
	return gr, len(g.Nodes)
}

// loadAndGrade loads the graph and reports node/edge counts plus every bean
// dependency cycle graded fatal vs tolerated. Grading is INJECTS-only (spec
// 2026-10-14 §7): the graph also carries CALLS edges (method-level mutual
// recursion), and vectra's Cycles() is label-agnostic, so the intra-cycle
// edges are filtered to INJECTS before via-inspection. A cycle whose members
// connect without a single INJECTS edge (e.g. two methods calling each
// other) is NOT reported at all — the user-facing cycle summary is about
// bean dependencies, not call recursion.
func loadAndGrade(jsonPath string) resultOut {
	gr, nodeCount := loadGraph(jsonPath)
	return grade(gr, nodeCount)
}

// grade is a read-only report on a sealed graph. Since vectra v0.5 the Cycle
// value carries the intra-cycle edges with full label/props, so label
// filtering and via-inspection no longer re-join EdgesOf records.
func grade(gr *vectra.Graph, nodeCount int) resultOut {
	out := resultOut{Nodes: nodeCount, Edges: gr.EdgeCount(), Cycles: []cycleOut{}}
	for _, c := range gr.Cycles() {
		injects := 0
		fatal := false
		for _, e := range c.Edges {
			if e.Label != "INJECTS" {
				continue
			}
			injects++
			if via, ok := e.Props["via"].(string); ok && fatalVia[via] {
				fatal = true
			}
		}
		if injects == 0 {
			continue // non-INJECTS cycle (CALLS recursion etc.) — not user-facing
		}
		members := make([]string, 0, len(c.Nodes))
		for _, id := range c.Nodes {
			if n, ok := gr.NodeByID(id); ok {
				members = append(members, n.Key)
			}
		}
		grade := "tolerated"
		if fatal {
			grade = "fatal"
		}
		out.Cycles = append(out.Cycles, cycleOut{Nodes: members, Grade: grade})
	}
	return out
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: loader <structure.json> <structure.vg>")
		os.Exit(2)
	}
	// One sealed graph serves both consumers: the saved .vg contains exactly
	// the JSON graph; grading is report-only and never mutates it.
	gr, nodeCount := loadGraph(os.Args[1])
	res := grade(gr, nodeCount)
	must(gr.SaveFile(os.Args[2]))
	enc := json.NewEncoder(os.Stdout)
	must(enc.Encode(res))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
