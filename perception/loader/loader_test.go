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

func TestLoadAndGradeShape(t *testing.T) {
	res := loadAndGrade(writeFixture(t, fatalDoc))
	if res.Nodes != 2 || res.Edges != 2 {
		t.Fatalf("counts: %+v", res)
	}
	if _, err := json.Marshal(res); err != nil {
		t.Fatal(err)
	}
}
