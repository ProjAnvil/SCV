package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
