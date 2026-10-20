package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/projanvil/vectra"
)

func writeFixture(t *testing.T, doc string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "structure.json")
	if err := os.WriteFile(p, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const fatalDoc = `{"nodes":[
 {"key":"bean:a","label":"Bean","props":{}},
 {"key":"bean:b","label":"Bean","props":{}}],
 "edges":[
 {"srcKey":"bean:a","dstKey":"bean:b","label":"INJECTS","props":{"via":"constructor"}},
 {"srcKey":"bean:b","dstKey":"bean:a","label":"INJECTS","props":{"via":"beanMethod"}}]}`

const toleratedDoc = `{"nodes":[
 {"key":"bean:x","label":"Bean","props":{}},
 {"key":"bean:y","label":"Bean","props":{}}],
 "edges":[
 {"srcKey":"bean:x","dstKey":"bean:y","label":"INJECTS","props":{"via":"field"}},
 {"srcKey":"bean:y","dstKey":"bean:x","label":"INJECTS","props":{"via":"setter"}}]}`

// callsOnlyDoc: two methods in mutual recursion — a real cycle in the raw
// graph, but with zero INJECTS edges it must not reach the user-facing
// cycles[] (spec 2026-10-14 §7: CALLS mutual recursion is not a bean cycle).
const callsOnlyDoc = `{"nodes":[
 {"key":"fn:com.a.A#rec()" ,"label":"Method","props":{}},
 {"key":"fn:com.b.B#back()","label":"Method","props":{}}],
 "edges":[
 {"srcKey":"fn:com.a.A#rec()","dstKey":"fn:com.b.B#back()","label":"CALLS","props":{"kind":"direct"}},
 {"srcKey":"fn:com.b.B#back()","dstKey":"fn:com.a.A#rec()","label":"CALLS","props":{"kind":"virtual-dispatch"}}]}`

// mixedFatalDoc: one SCC carrying both labels — an INJECTS constructor edge
// (fatal via) plus a CALLS back edge. Graded from the INJECTS edge only.
const mixedFatalDoc = `{"nodes":[
 {"key":"bean:p","label":"Bean","props":{}},
 {"key":"bean:q","label":"Bean","props":{}}],
 "edges":[
 {"srcKey":"bean:p","dstKey":"bean:q","label":"INJECTS","props":{"via":"constructor"}},
 {"srcKey":"bean:q","dstKey":"bean:p","label":"CALLS","props":{"kind":"direct"}}]}`

// mixedToleratedDoc: INJECTS edges are field/setter (tolerated) while the
// CALLS back edge carries a fatal-looking via prop — the label filter must
// ignore it, proving grading reads INJECTS edges only.
const mixedToleratedDoc = `{"nodes":[
 {"key":"bean:u","label":"Bean","props":{}},
 {"key":"bean:v","label":"Bean","props":{}}],
 "edges":[
 {"srcKey":"bean:u","dstKey":"bean:v","label":"INJECTS","props":{"via":"field"}},
 {"srcKey":"bean:v","dstKey":"bean:u","label":"CALLS","props":{"via":"constructor","kind":"direct"}}]}`

func TestGradeCycles(t *testing.T) {
	res := loadAndGrade(writeFixture(t, fatalDoc))
	if len(res.Cycles) != 1 || res.Cycles[0].Grade != "fatal" {
		t.Fatalf("want 1 fatal cycle, got %+v", res.Cycles)
	}
	res = loadAndGrade(writeFixture(t, toleratedDoc))
	if len(res.Cycles) != 1 || res.Cycles[0].Grade != "tolerated" {
		t.Fatalf("want 1 tolerated cycle, got %+v", res.Cycles)
	}
}

// TestGradeCyclesINJECTSFilter covers the v2 rule (spec 2026-10-14 §7):
// vectra Cycles() is label-agnostic, so the loader must (a) drop CALLS-only
// cycles from the report entirely and (b) grade mixed cycles from their
// INJECTS edges only.
func TestGradeCyclesINJECTSFilter(t *testing.T) {
	res := loadAndGrade(writeFixture(t, callsOnlyDoc))
	if len(res.Cycles) != 0 {
		t.Fatalf("CALLS-only mutual recursion must not appear in cycles[], got %+v", res.Cycles)
	}
	if res.Nodes != 2 || res.Edges != 2 {
		t.Fatalf("counts must still report the raw graph: %+v", res)
	}

	res = loadAndGrade(writeFixture(t, mixedFatalDoc))
	if len(res.Cycles) != 1 || res.Cycles[0].Grade != "fatal" {
		t.Fatalf("mixed cycle with an INJECTS constructor edge must be reported fatal, got %+v", res.Cycles)
	}
	if got := res.Cycles[0].Nodes; len(got) != 2 {
		t.Fatalf("cycle members: %v", got)
	}

	res = loadAndGrade(writeFixture(t, mixedToleratedDoc))
	if len(res.Cycles) != 1 || res.Cycles[0].Grade != "tolerated" {
		t.Fatalf("CALLS-edge via props must not grade a cycle; INJECTS field/setter is tolerated, got %+v", res.Cycles)
	}
}

func TestLoadAndGradeShape(t *testing.T) {
	res := loadAndGrade(writeFixture(t, fatalDoc))
	if res.Nodes != 2 || res.Edges != 2 {
		t.Fatalf("counts: %+v", res)
	}
	if _, err := json.Marshal(res); err != nil {
		t.Fatal(err)
	}
}

// multiLangDoc: minimal multi-language artifact (spec §8) — two File nodes
// carrying lang, one IMPORTS (File→File) edge, one CALLS edge with props.
const multiLangDoc = `{"nodes":[
 {"key":"file:src/app.ts","label":"File","props":{"path":"src/app.ts","language":"typescript","lang":"typescript"}},
 {"key":"file:lib/api.py","label":"File","props":{"path":"lib/api.py","language":"python","lang":"python"}},
 {"key":"type:lib.api::API","label":"Type","props":{"lang":"python","kind":"class"}},
 {"key":"fn:lib.api::API#get(n)","label":"Method","props":{"lang":"python","name":"get"}},
 {"key":"fn:lib.api::API#load(n)","label":"Method","props":{"lang":"python","name":"load"}}],
 "edges":[
 {"srcKey":"file:src/app.ts","dstKey":"file:lib/api.py","label":"IMPORTS","props":{}},
 {"srcKey":"fn:lib.api::API#get(n)","dstKey":"fn:lib.api::API#load(n)","label":"CALLS","props":{"kind":"direct","line":10}}]}`

// importsCycleDoc: two files importing each other — a real cycle in the raw
// graph, but IMPORTS-only, so it must not reach cycles[] (INJECTS-only rule,
// spec §9(c)).
const importsCycleDoc = `{"nodes":[
 {"key":"file:a.py","label":"File","props":{"lang":"python"}},
 {"key":"file:b.py","label":"File","props":{"lang":"python"}}],
 "edges":[
 {"srcKey":"file:a.py","dstKey":"file:b.py","label":"IMPORTS","props":{}},
 {"srcKey":"file:b.py","dstKey":"file:a.py","label":"IMPORTS","props":{}}]}`

// TestLoadMultiLangSnapshot covers the v3 multi-language artifact shape:
// lang props survive the SaveFile→LoadFile roundtrip and answer {lang}
// Cypher WITHOUT any lang index (spec §9(b) downgrade — unindexed {prop}
// keys fall back to a node scan), and the IMPORTS edge keeps its label and
// endpoints while CALLS props ride along.
func TestLoadMultiLangSnapshot(t *testing.T) {
	gr, _ := loadGraph(writeFixture(t, multiLangDoc))
	vg := filepath.Join(t.TempDir(), "structure.vg")
	if err := gr.SaveFile(vg); err != nil {
		t.Fatal(err)
	}
	loaded, err := vectra.LoadFile(vg)
	if err != nil {
		t.Fatal(err)
	}
	if st := loaded.Stats(); st.Nodes != 5 || st.Edges != 2 {
		t.Fatalf("snapshot stats: %+v", st)
	}

	py, err := loaded.Cypher(`MATCH (n {lang:'python'}) RETURN n`)
	if err != nil {
		t.Fatal(err)
	}
	// 4 python nodes: File + Type + 2 Methods (subset has no count() — count rows)
	if len(py.Rows) != 4 {
		t.Fatalf("python nodes: %d rows, want 4", len(py.Rows))
	}
	ts, err := loaded.Cypher(`MATCH (n {lang:'typescript'}) RETURN n`)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts.Rows) != 1 {
		t.Fatalf("typescript nodes: %d rows, want 1", len(ts.Rows))
	}

	imp, err := loaded.Cypher(`MATCH (a)-[e:IMPORTS]->(b) RETURN a.key, b.key, e`)
	if err != nil {
		t.Fatal(err)
	}
	if len(imp.Rows) != 1 {
		t.Fatalf("IMPORTS rows: %+v", imp.Rows)
	}
	row := imp.Rows[0]
	if e, ok := row["e"].(vectra.Edge); !ok || e.Label != "IMPORTS" {
		t.Fatalf("IMPORTS label lost: %+v", row["e"])
	}
	if row["a.key"] != "file:src/app.ts" || row["b.key"] != "file:lib/api.py" {
		t.Fatalf("IMPORTS endpoints: %+v", row)
	}

	call, err := loaded.Cypher(`MATCH ()-[e:CALLS]->() RETURN e`)
	if err != nil {
		t.Fatal(err)
	}
	if len(call.Rows) != 1 {
		t.Fatalf("CALLS rows: %+v", call.Rows)
	}
	if e, ok := call.Rows[0]["e"].(vectra.Edge); !ok || e.Props["kind"] != "direct" || fmt.Sprint(e.Props["line"]) != "10" {
		t.Fatalf("CALLS props lost: %+v", call.Rows[0]["e"])
	}
}

// TestImportsOnlyCycleNotGraded: an IMPORTS-only cycle (mutual file imports)
// is not a bean dependency cycle — it must not reach the user-facing
// cycles[] even though vectra's Cycles() is label-agnostic.
func TestImportsOnlyCycleNotGraded(t *testing.T) {
	res := loadAndGrade(writeFixture(t, importsCycleDoc))
	if len(res.Cycles) != 0 {
		t.Fatalf("IMPORTS-only cycle must not appear in cycles[], got %+v", res.Cycles)
	}
	if res.Nodes != 2 || res.Edges != 2 {
		t.Fatalf("counts must still report the raw graph: %+v", res)
	}
}
