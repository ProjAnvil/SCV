package run

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/projanvil/scv/engine/internal/config"
	"github.com/projanvil/scv/engine/internal/docgen"
	"github.com/projanvil/scv/engine/internal/env"
	"github.com/projanvil/scv/engine/internal/job"
	"github.com/projanvil/scv/engine/internal/repo"
)

// stubPerceiveScript is a faithful stub of perceive.py's observable
// contract for the run pipeline: it appends tier log lines (timestamp
// prefix included) to <analysis_dir>/perception.log, writes artifacts,
// records its invocation (argv + SCV_PERCEPTION_CONFIG) to
// <analysis_dir>/.stub-call.json, and prints exactly one final JSON line.
// STUB_MODE selects the scenario (see stub doc below). The engine runs it
// through the real python3, via SCV_ENGINE_TEST_PERCEIVE.
const stubPerceiveScript = `#!/usr/bin/env python3
"""Stub perceive.py driven by STUB_MODE:
  full            - all tiers log start + completion, structure.json/.vg, status ok
  not_framework   - only "tier1 detect: not-framework", status not-framework
  go_missing      - tiers 1-3 run; tier6/tier8 log start only (real perceive
                    logs the starts unconditionally when go+dist are absent)
  uv_missing      - tier1 runs, tier2 uv probe fails, status skipped/uv-missing
  analyzer_failed - analyzer tier fails in-band (exit 0, reason analyzer-failed)
  crash_open      - tier1 starts, process exits 3 (stage open at crash)
  crash           - tier1 completes, process exits 3 (stage closed at crash)
  exit3           - no logs at all, process exits 3
  garbage         - junk log lines (no tier lines), non-JSON stdout
  sleep           - logs nothing, sleeps 30s (cancellation target)
"""
import json, os, sys, time

def log(ad, msg):
    with open(os.path.join(ad, "perception.log"), "a", encoding="utf-8") as fh:
        fh.write(time.strftime("%Y-%m-%dT%H:%M:%S") + " " + msg + "\n")

def done(status, reason=None, vg=False):
    out = {"status": status, "reason": reason,
           "tiers": {"json": status == "ok", "vg": vg, "mcp_hint": vg},
           "artifacts": {"structure_json": os.path.join(ad, "structure.json") if status == "ok" else None,
                         "structure_vg": os.path.join(ad, "structure.vg") if vg else None}}
    print(json.dumps(out))
    return 0

repo_arg, ad = sys.argv[1], sys.argv[2]
os.makedirs(ad, exist_ok=True)
with open(os.path.join(ad, ".stub-call.json"), "w", encoding="utf-8") as fh:
    json.dump({"argv": sys.argv, "config_env": os.environ.get("SCV_PERCEPTION_CONFIG", "")}, fh)
mode = os.environ.get("STUB_MODE", "full")

if mode == "garbage":
    log(ad, "junk " + "x" * 3000)
    print("this is not json {")
    sys.exit(0)
if mode == "sleep":
    time.sleep(30)
    print("{}")
    sys.exit(0)
if mode == "exit3":
    sys.exit(3)
if mode == "not_framework":
    log(ad, "tier1 detect: not-framework")
    sys.exit(done("not-framework"))
if mode == "uv_missing":
    log(ad, "tier1 start")
    log(ad, "tier1 detect: structure, source=local, extensions=[], languages=['go']")
    log(ad, "tier2 start")
    log(ad, "tier2 uv probe: not found")
    sys.exit(done("skipped", "uv-missing"))
if mode == "analyzer_failed":
    log(ad, "tier1 start")
    log(ad, "tier1 detect: structure, source=local, extensions=[], languages=['go']")
    log(ad, "tier2 start")
    log(ad, "tier2 uv probe: /fake/uv")
    log(ad, "tier3 start")
    log(ad, "tier3 analyzer rc=1 duration=0.1s fake-analyzer blew up")
    sys.exit(done("skipped", "analyzer-failed"))
if mode == "crash_open":
    log(ad, "tier1 start")
    sys.exit(3)
if mode == "crash":
    log(ad, "tier1 start")
    log(ad, "tier1 detect: structure, source=local, extensions=[], languages=['go']")
    sys.exit(3)

# full and go_missing share tiers 1-3. Sleep mid-run so tests can observe
# the engine's live tailer emitting events before the process exits.
log(ad, "tier1 start")
log(ad, "tier1 detect: structure, source=local, extensions=[], languages=['go']")
time.sleep(0.5)
with open(os.path.join(ad, ".stub-phase2"), "w", encoding="utf-8") as fh:
    fh.write("tiers 2+ ran")
log(ad, "tier2 start")
log(ad, "tier2 uv probe: /fake/uv")
log(ad, "tier3 start")
log(ad, "tier3 analyzer rc=0 duration=0.1s fake-analyzer")
with open(os.path.join(ad, "structure.json"), "w", encoding="utf-8") as fh:
    json.dump({"nodes": [], "edges": []}, fh)
if mode == "go_missing":
    log(ad, "tier6 start")
    log(ad, "tier8 start")
    log(ad, "tiers done: {'json': True, 'vg': False, 'mcp_hint': False} reasons=['go-missing']")
    sys.exit(done("ok", ["go-missing"]))
log(ad, "tier6 start")
with open(os.path.join(ad, "structure.vg"), "w", encoding="utf-8") as fh:
    fh.write("VG")
log(ad, "tier8 start")
log(ad, "tiers done: {'json': True, 'vg': True, 'mcp_hint': True} reasons=[]")
sys.exit(done("ok", None, vg=True))
`

// stubPerceive writes the stub script into a temp dir and points
// SCV_ENGINE_TEST_PERCEIVE at it.
func stubPerceive(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "perceive.py")
	if err := os.WriteFile(path, []byte(stubPerceiveScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SCV_ENGINE_TEST_PERCEIVE", path)
}

func stubMode(t *testing.T, mode string) { t.Setenv("STUB_MODE", mode) }

// makeGitRepo creates a real git worktree with one commit; its HEAD is
// the commit the skip-check compares against.
func makeGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@scv.local")
	run("config", "user.name", "SCV Test")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "init")
	return dir
}

// fixture wires a full pipeline run against a fake env snapshot and an
// in-memory event sink, with the stub perceive installed.
type fixture struct {
	scvHome  string
	cfgPath  string
	cfg      *config.Config
	env      *env.Env
	events   []job.Event
	sink     func(job.Event)
	discover discoverFunc
}

func newFixture(t *testing.T, llm bool) *fixture {
	t.Helper()
	stubPerceive(t)
	stubMode(t, "full")
	home := t.TempDir()
	root := filepath.Join(home, "perception")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "{}\n"
	if llm {
		body = `{"llm":{"mode":"command","command":["fake-llm"],"timeout_s":600,"retry":1}}` + "\n"
	}
	cfgPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath, home)
	if err != nil {
		t.Fatal(err)
	}
	py3, err := exec.LookPath("python3")
	if err != nil {
		t.Fatalf("python3 not found: %v", err)
	}
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("git not found: %v", err)
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("go not found: %v", err)
	}
	f := &fixture{
		scvHome: home,
		cfgPath: cfgPath,
		cfg:     cfg,
		env: &env.Env{
			ScvHome:        home,
			PerceptionRoot: root,
			DistRoot:       filepath.Join(root, "dist"),
			Uv:             &env.Tool{Path: "/fake/uv", OK: true},
			Python3:        &env.Tool{Path: py3, OK: true},
			Git:            &env.Tool{Path: gitBin, OK: true},
			Go:             &env.Tool{Path: goBin, OK: true},
		},
	}
	f.sink = func(ev job.Event) { f.events = append(f.events, ev) }
	f.discover = func(env.Options) (*env.Env, error) { return f.env, nil }
	return f
}

func (f *fixture) run(ctx context.Context, req RunRequest, docs DocGenerator) (*RunResult, error) {
	return runPipeline(ctx, f.cfg, req, f.sink, docs, f.discover)
}

// analysisDirOf returns the analysis dir a run with project name p used.
func (f *fixture) analysisDirOf(p string) string {
	return filepath.Join(f.scvHome, "analysis", p)
}

// seq renders the event stream as "type:stage" pairs for golden asserts.
func seq(events []job.Event) []string {
	out := make([]string, 0, len(events))
	for _, ev := range events {
		out = append(out, ev.Type+":"+ev.Stage)
	}
	return out
}

func assertSeq(t *testing.T, events []job.Event, want []string) {
	t.Helper()
	got := seq(events)
	if len(got) != len(want) {
		t.Fatalf("event sequence mismatch:\n got: %v\nwant: %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event sequence mismatch at %d:\n got: %v\nwant: %v", i, got, want)
		}
	}
}

func findEvent(t *testing.T, events []job.Event, typ, stage string) job.Event {
	t.Helper()
	for _, ev := range events {
		if ev.Type == typ && ev.Stage == stage {
			return ev
		}
	}
	t.Fatalf("no %s:%s event in %v", typ, stage, seq(events))
	return job.Event{}
}

// missingItemNames extracts the item names from a degraded/stage_failed
// detail.missing payload.
func missingItemNames(t *testing.T, ev job.Event) []string {
	t.Helper()
	raw, ok := ev.Detail["missing"].([]map[string]any)
	if !ok {
		t.Fatalf("event %s:%s has no detail.missing list: %v", ev.Type, ev.Stage, ev.Detail)
	}
	names := make([]string, 0, len(raw))
	for _, m := range raw {
		item, _ := m["item"].(string)
		names = append(names, item)
	}
	return names
}

func readJSON(t *testing.T, path string, into any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
}

// loadJobFromDisk verifies the durable trail: job.json state and the
// events.jsonl log must match what the sink saw.
func loadJobFromDisk(t *testing.T, f *fixture, wantState job.State, wantEvents int) {
	t.Helper()
	if len(f.events) == 0 {
		t.Fatal("no events captured")
	}
	dir := filepath.Join(f.scvHome, "jobs", f.events[0].ID)
	j, events, err := job.LoadStatus(dir)
	if err != nil {
		t.Fatalf("load job: %v", err)
	}
	if j.State != wantState {
		t.Fatalf("job.json state = %q, want %q", j.State, wantState)
	}
	if len(events) != wantEvents {
		t.Fatalf("events.jsonl has %d events, sink saw %d", len(events), wantEvents)
	}
}

func TestGateEnvIncompleteListsAll(t *testing.T) {
	f := newFixture(t, false)
	f.env.Uv = nil
	f.env.PerceptionRoot = filepath.Join(f.scvHome, "perception-missing")

	res, err := f.run(context.Background(), RunRequest{Source: "/definitely/not/here"}, nil)
	if !errors.Is(err, ErrEnvIncomplete) {
		t.Fatalf("error = %v, want ErrEnvIncomplete", err)
	}
	if res != nil && res.Perception != nil {
		t.Fatalf("result should carry no perception, got %v", res.Perception)
	}
	assertSeq(t, f.events, []string{
		"stage_started:checking",
		"stage_failed:checking",
		"job_result:failed",
	})
	failed := findEvent(t, f.events, job.EventStageFailed, string(job.StateChecking))
	if failed.Error == nil || failed.Error.Code != CodeEnvIncomplete {
		t.Fatalf("stage_failed error = %+v, want code %s", failed.Error, CodeEnvIncomplete)
	}
	got := missingItemNames(t, failed)
	want := []string{"perception-root", "uv"}
	if len(got) != len(want) {
		t.Fatalf("missing items = %v, want %v (all blocking items in one event)", got, want)
	}
	for _, w := range want {
		if !strings.Contains(strings.Join(got, ","), w) {
			t.Fatalf("missing items = %v, want %v contained", got, want)
		}
	}
	loadJobFromDisk(t, f, job.StateFailed, 3)
}

func TestFullSequenceGolden(t *testing.T) {
	f := newFixture(t, false) // no llm section: docgen-unconfigured degradation
	stubMode(t, "full")
	src := makeGitRepo(t)
	analysisDir := f.analysisDirOf("demo")

	// Liveness pin: when the tailer emits the detecting finish, the stub
	// process must still be mid-run (the .stub-phase2 marker appears only
	// after its 0.5s pause), proving events stream live, not at exit.
	inner := f.sink
	f.sink = func(ev job.Event) {
		if ev.Type == job.EventStageFinished && ev.Stage == string(job.StateDetecting) {
			if _, err := os.Stat(filepath.Join(analysisDir, ".stub-phase2")); err == nil {
				t.Error("detecting finished arrived after the stub continued; tailer is not live")
			}
		}
		inner(ev)
	}

	res, err := f.run(context.Background(), RunRequest{Source: src, Project: "demo", Lang: "en"}, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	assertSeq(t, f.events, []string{
		"stage_started:checking",
		"degraded:checking",
		"stage_finished:checking",
		"stage_started:preparing",
		"stage_finished:preparing",
		"stage_started:detecting",
		"stage_finished:detecting",
		"stage_started:analyzing",
		"stage_finished:analyzing",
		"stage_started:grading",
		"stage_finished:grading",
		"stage_started:query_tier",
		"stage_finished:query_tier",
		"stage_started:finalizing",
		"stage_finished:finalizing",
		"job_result:done",
	})

	// Degraded event lists every degrading item in one shot.
	deg := findEvent(t, f.events, job.EventDegraded, string(job.StateChecking))
	if got := missingItemNames(t, deg); len(got) != 1 || got[0] != "docgen-unconfigured" {
		t.Fatalf("degraded missing = %v, want [docgen-unconfigured]", got)
	}

	// Stage details: detect result, tier notes, belt-and-braces vg check.
	detFin := findEvent(t, f.events, job.EventStageFinished, string(job.StateDetecting))
	if detFin.Detail["perception_status"] != "structure" {
		t.Fatalf("detecting finish detail = %v, want perception_status=structure", detFin.Detail)
	}
	anaFin := findEvent(t, f.events, job.EventStageFinished, string(job.StateAnalyzing))
	if anaFin.Detail["uv"] != "/fake/uv" || anaFin.Detail["analyzer_rc"] != "0" {
		t.Fatalf("analyzing finish detail = %v, want uv and analyzer_rc notes", anaFin.Detail)
	}
	graFin := findEvent(t, f.events, job.EventStageFinished, string(job.StateGrading))
	if graFin.Detail["structure_vg"] != true {
		t.Fatalf("grading finish detail = %v, want structure_vg=true (belt-and-braces)", graFin.Detail)
	}
	prepFin := findEvent(t, f.events, job.EventStageFinished, string(job.StatePreparing))
	if prepFin.Detail["source_kind"] != string(repo.KindLocalGit) {
		t.Fatalf("preparing finish detail = %v, want source_kind=local_git", prepFin.Detail)
	}
	head, err := repo.HeadCommit(src)
	if err != nil {
		t.Fatal(err)
	}
	if prepFin.Detail["head_commit"] != head {
		t.Fatalf("preparing finish head_commit = %v, want %s", prepFin.Detail["head_commit"], head)
	}

	// Result and side effects.
	if status, _ := res.Perception["status"].(string); status != "ok" {
		t.Fatalf("perception status = %v, want ok", res.Perception["status"])
	}
	if res.OutputDir != analysisDir {
		t.Fatalf("result output dir = %q, want %q", res.OutputDir, analysisDir)
	}
	if res.SkippedReason != "" || len(res.Docs) != 0 {
		t.Fatalf("result = %+v, want no skip and no docs", res)
	}
	for _, name := range []string{"structure.json", "structure.vg", ".scv_metadata.json", "perception.log"} {
		if _, err := os.Stat(filepath.Join(analysisDir, name)); err != nil {
			t.Fatalf("artifact %s missing: %v", name, err)
		}
	}
	// Project overrides the linked repos entry name.
	link, err := os.Readlink(filepath.Join(f.scvHome, "repos", "demo"))
	if err != nil {
		t.Fatalf("repos/demo symlink: %v", err)
	}
	if abs, _ := filepath.Abs(src); link != abs {
		t.Fatalf("repos/demo -> %q, want %q", link, abs)
	}
	// Metadata carries the head commit.
	var meta map[string]any
	readJSON(t, filepath.Join(analysisDir, ".scv_metadata.json"), &meta)
	if meta["last_analyzed_commit"] != head {
		t.Fatalf("metadata last_analyzed_commit = %v, want %s", meta["last_analyzed_commit"], head)
	}
	// The analysis dir is registered for the query surface.
	var reg struct {
		Dirs []string `json:"dirs"`
	}
	readJSON(t, filepath.Join(f.scvHome, "analysis-dirs.json"), &reg)
	if len(reg.Dirs) != 1 || reg.Dirs[0] != analysisDir {
		t.Fatalf("analysis-dirs.json = %v, want [%s]", reg.Dirs, analysisDir)
	}
	// Exec contract: --source local for a git source, --lang passthrough,
	// SCV_PERCEPTION_CONFIG pointing at the loaded config.
	var call struct {
		Argv      []string `json:"argv"`
		ConfigEnv string   `json:"config_env"`
	}
	readJSON(t, filepath.Join(analysisDir, ".stub-call.json"), &call)
	argv := strings.Join(call.Argv, " ")
	if !strings.Contains(argv, "--source local") {
		t.Fatalf("stub argv = %v, want --source local", call.Argv)
	}
	if !strings.Contains(argv, "--lang en") {
		t.Fatalf("stub argv = %v, want --lang en", call.Argv)
	}
	if call.ConfigEnv != f.cfgPath {
		t.Fatalf("stub SCV_PERCEPTION_CONFIG = %q, want %q", call.ConfigEnv, f.cfgPath)
	}
	loadJobFromDisk(t, f, job.StateDone, 16)
}

func TestNotFrameworkSequence(t *testing.T) {
	f := newFixture(t, false)
	stubMode(t, "not_framework")

	res, err := f.run(context.Background(), RunRequest{Source: makeGitRepo(t), Project: "nf"}, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	assertSeq(t, f.events, []string{
		"stage_started:checking",
		"degraded:checking",
		"stage_finished:checking",
		"stage_started:preparing",
		"stage_finished:preparing",
		"stage_started:detecting",
		"stage_finished:detecting",
		"stage_started:finalizing",
		"stage_finished:finalizing",
		"job_result:done",
	})
	detFin := findEvent(t, f.events, job.EventStageFinished, string(job.StateDetecting))
	if detFin.Detail["perception_status"] != "not-framework" {
		t.Fatalf("detecting finish detail = %v, want perception_status=not-framework", detFin.Detail)
	}
	if status, _ := res.Perception["status"].(string); status != "not-framework" {
		t.Fatalf("perception status = %v, want not-framework", res.Perception["status"])
	}
	loadJobFromDisk(t, f, job.StateDone, 10)
}

func TestGoMissingReducedSequence(t *testing.T) {
	f := newFixture(t, false)
	f.env.Go = nil // no toolchain and no prebuilt binaries: query capability degraded
	f.env.PrebuiltLoader = false
	f.env.PrebuiltGraphMCP = false
	stubMode(t, "go_missing") // logs tier6/tier8 starts unconditionally, like real perceive

	_, err := f.run(context.Background(), RunRequest{Source: makeGitRepo(t), Project: "gom"}, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	assertSeq(t, f.events, []string{
		"stage_started:checking",
		"degraded:checking",
		"stage_finished:checking",
		"stage_started:preparing",
		"stage_finished:preparing",
		"stage_started:detecting",
		"stage_finished:detecting",
		"stage_started:analyzing",
		"stage_finished:analyzing",
		"stage_started:finalizing",
		"stage_finished:finalizing",
		"job_result:done",
	})
	deg := findEvent(t, f.events, job.EventDegraded, string(job.StateChecking))
	got := strings.Join(missingItemNames(t, deg), ",")
	for _, want := range []string{"go", "docgen-unconfigured"} {
		if !strings.Contains(got, want) {
			t.Fatalf("degraded missing = %v, want %s listed", got, want)
		}
	}
	// grading/query_tier must be absent even though the log carried
	// tier6/tier8 start lines.
	for _, ev := range f.events {
		if ev.Stage == string(job.StateGrading) || ev.Stage == string(job.StateQueryTier) {
			t.Fatalf("query-degraded run must not emit %s:%s", ev.Type, ev.Stage)
		}
	}
	loadJobFromDisk(t, f, job.StateDone, 12)
}

func TestUvMissingAllowDegradedSequence(t *testing.T) {
	f := newFixture(t, false)
	f.env.Uv = nil
	stubMode(t, "uv_missing")

	res, err := f.run(context.Background(), RunRequest{
		Source: makeGitRepo(t), Project: "uvm", AllowDegraded: true,
	}, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	assertSeq(t, f.events, []string{
		"stage_started:checking",
		"degraded:checking",
		"stage_finished:checking",
		"stage_started:preparing",
		"stage_finished:preparing",
		"stage_started:detecting",
		"stage_finished:detecting",
		"stage_started:finalizing",
		"stage_finished:finalizing",
		"job_result:done",
	})
	// uv was demoted from blocking to one degraded item, folded with the
	// docgen item; perceive's in-band uv-missing is not a second event.
	deg := findEvent(t, f.events, job.EventDegraded, string(job.StateChecking))
	got := strings.Join(missingItemNames(t, deg), ",")
	if !strings.Contains(got, "uv") {
		t.Fatalf("degraded missing = %v, want uv demoted into it", got)
	}
	for _, ev := range f.events {
		if ev.Stage == string(job.StateAnalyzing) {
			t.Fatalf("structure-degraded run must not emit %s:%s", ev.Type, ev.Stage)
		}
	}
	if reason, _ := res.Perception["reason"].(string); reason != "uv-missing" {
		t.Fatalf("perception reason = %v, want uv-missing", res.Perception["reason"])
	}
	if res.SkippedReason != "uv-missing" {
		t.Fatalf("SkippedReason = %q, want uv-missing", res.SkippedReason)
	}
	loadJobFromDisk(t, f, job.StateDone, 10)
}

func TestSkipUnchangedWithMissingDocs(t *testing.T) {
	f := newFixture(t, false)
	src := makeGitRepo(t)
	req := RunRequest{Source: src, Project: "skip", SkipUnchanged: true}

	// Phase 1: structure-only run completes and records the commit.
	if _, err := f.run(context.Background(), req, nil); err != nil {
		t.Fatalf("phase 1 run: %v", err)
	}
	analysisDir := f.analysisDirOf("skip")

	// Phase 2: llm-configured re-run on the same commit. The four
	// documents are missing, so the run must NOT be trapped in skipped.
	llm := `{"llm":{"mode":"command","command":["fake-llm"],"timeout_s":600,"retry":1}}` + "\n"
	if err := os.WriteFile(f.cfgPath, []byte(llm), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(f.cfgPath, f.scvHome)
	if err != nil {
		t.Fatal(err)
	}
	f.cfg = cfg
	mark := len(f.events)
	res, err := f.run(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("phase 2 run: %v", err)
	}
	if res.SkippedReason != "" {
		t.Fatalf("phase 2 SkippedReason = %q, want re-run (docs missing)", res.SkippedReason)
	}
	phase2 := f.events[mark:]
	if findEventQuiet(phase2, job.EventStageStarted, string(job.StateDetecting)) == nil {
		t.Fatal("phase 2 must re-run perceive (docs missing means not skippable)")
	}
	// The linked entry already exists: preparing reuses it instead of failing.
	link, err := os.Readlink(filepath.Join(f.scvHome, "repos", "skip"))
	if err != nil {
		t.Fatalf("repos/skip symlink after re-run: %v", err)
	}
	if abs, _ := filepath.Abs(src); link != abs {
		t.Fatalf("repos/skip -> %q, want %q", link, abs)
	}

	// Phase 3: with all four documents present, the same commit skips.
	for _, doc := range []string{"README.md", "SUMMARY.md", "ARCHITECTURE.md", "FILE_INDEX.md"} {
		if err := os.WriteFile(filepath.Join(analysisDir, doc), []byte("doc\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mark = len(f.events)
	res, err = f.run(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("phase 3 run: %v", err)
	}
	if res.SkippedReason != "commit-unchanged" {
		t.Fatalf("phase 3 SkippedReason = %q, want commit-unchanged", res.SkippedReason)
	}
	assertSeq(t, f.events[mark:], []string{
		"stage_started:checking",
		"stage_finished:checking",
		"stage_started:preparing",
		"stage_finished:preparing",
		"job_result:skipped",
	})
	skipped := findEvent(t, f.events[mark:], job.EventJobResult, string(job.StateSkipped))
	if skipped.Detail["reason"] != "commit-unchanged" {
		t.Fatalf("job_result detail = %v, want reason=commit-unchanged", skipped.Detail)
	}
	if findEventQuiet(f.events[mark:], job.EventStageStarted, string(job.StateDetecting)) != nil {
		t.Fatal("skipped run must not run perceive")
	}
}

func TestPerceiveGarbageStdoutFailsAnalyzerFailed(t *testing.T) {
	f := newFixture(t, false)
	stubMode(t, "garbage")

	res, err := f.run(context.Background(), RunRequest{Source: makeGitRepo(t), Project: "gar"}, nil)
	if !errors.Is(err, ErrAnalyzerFailed) {
		t.Fatalf("error = %v, want ErrAnalyzerFailed", err)
	}
	if res == nil || res.Perception != nil {
		t.Fatalf("result = %+v, want no perception map", res)
	}
	failed := findEvent(t, f.events, job.EventStageFailed, string(job.StateDetecting))
	if failed.Error == nil || failed.Error.Code != CodeAnalyzerFailed {
		t.Fatalf("stage_failed error = %+v, want code %s", failed.Error, CodeAnalyzerFailed)
	}
	// The log tail is capped at 2000 chars and carries the failing log's
	// end (the stub logged 3000+ chars of junk).
	if len(failed.Error.LogTail) != logTailLimit {
		t.Fatalf("log_tail length = %d, want %d", len(failed.Error.LogTail), logTailLimit)
	}
	if strings.Trim(failed.Error.LogTail, "x\n ") != "" {
		t.Fatalf("log_tail = %q, want the tail of the junk log line", failed.Error.LogTail)
	}
	loadJobFromDisk(t, f, job.StateFailed, len(f.events))
}

func TestPerceiveScriptMissingFails(t *testing.T) {
	f := newFixture(t, false)
	// Unset the override so the production resolution runs: the fake
	// perception root exists but carries no perceive.py.
	t.Setenv("SCV_ENGINE_TEST_PERCEIVE", "")

	_, err := f.run(context.Background(), RunRequest{Source: makeGitRepo(t), Project: "gone"}, nil)
	if !errors.Is(err, ErrAnalyzerFailed) {
		t.Fatalf("error = %v, want ErrAnalyzerFailed", err)
	}
	failed := findEvent(t, f.events, job.EventStageFailed, string(job.StateDetecting))
	if failed.Error == nil || !strings.Contains(failed.Error.Message, "perceive.py") {
		t.Fatalf("stage_failed error = %+v, want message naming the missing script", failed.Error)
	}
}

func TestURLCloneAndRerun(t *testing.T) {
	f := newFixture(t, false)
	stubMode(t, "full")
	origin := makeGitRepo(t)
	url := "file://" + origin
	req := RunRequest{Source: url} // entry name derives from the URL

	res, err := f.run(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	name := filepath.Base(origin)
	entryPath := filepath.Join(f.scvHome, "repos", name)
	if info, err := os.Stat(entryPath); err != nil || !info.IsDir() {
		t.Fatalf("repos entry %s not cloned: %v", entryPath, err)
	}
	if res.OutputDir != f.analysisDirOf(name) {
		t.Fatalf("analysis dir = %q, want URL-derived %q", res.OutputDir, f.analysisDirOf(name))
	}
	// URL sources run perceive with --source remote.
	var call struct {
		Argv []string `json:"argv"`
	}
	readJSON(t, filepath.Join(f.analysisDirOf(name), ".stub-call.json"), &call)
	if !strings.Contains(strings.Join(call.Argv, " "), "--source remote") {
		t.Fatalf("stub argv = %v, want --source remote", call.Argv)
	}

	// Re-run: the existing clone is pulled, not refused.
	mark := len(f.events)
	if _, err := f.run(context.Background(), req, nil); err != nil {
		t.Fatalf("second run: %v", err)
	}
	last := f.events[len(f.events)-1]
	if last.Type != job.EventJobResult || last.Stage != string(job.StateDone) {
		t.Fatalf("last event = %s:%s, want job_result:done", last.Type, last.Stage)
	}
	for _, ev := range f.events[mark:] {
		if ev.Type == job.EventStageFailed {
			t.Fatalf("re-run failed: %+v", ev)
		}
	}
}

func TestPreparingFailuresMapCodes(t *testing.T) {
	f := newFixture(t, false)

	// Missing local path: PATH_NOT_FOUND.
	_, err := f.run(context.Background(),
		RunRequest{Source: filepath.Join(t.TempDir(), "missing")}, nil)
	if !errors.Is(err, repo.ErrPathNotFound) {
		t.Fatalf("error = %v, want repo.ErrPathNotFound", err)
	}
	failed := findEvent(t, f.events, job.EventStageFailed, string(job.StatePreparing))
	if failed.Error == nil || failed.Error.Code != CodePathNotFound {
		t.Fatalf("stage_failed error = %+v, want code %s", failed.Error, CodePathNotFound)
	}

	// Same project name linked to a different worktree: the first link
	// succeeds, the second is refused with NAME_EXISTS.
	f2 := newFixture(t, false)
	for i, src := range []string{makeGitRepo(t), makeGitRepo(t)} {
		f2.events = nil
		_, err := f2.run(context.Background(), RunRequest{Source: src, Project: "clash"}, nil)
		if i == 0 && err != nil {
			t.Fatalf("first clash run: %v", err)
		}
		if i == 1 && err == nil {
			t.Fatal("conflicting link must fail")
		}
	}
	failed = findEvent(t, f2.events, job.EventStageFailed, string(job.StatePreparing))
	if failed.Error == nil || failed.Error.Code != CodeNameExists {
		t.Fatalf("stage_failed error = %+v, want code %s", failed.Error, CodeNameExists)
	}
}

func TestRunCancelledMidPerceive(t *testing.T) {
	f := newFixture(t, false)
	stubMode(t, "sleep")

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	_, err := f.run(ctx, RunRequest{Source: makeGitRepo(t), Project: "cxl"}, nil)
	if err == nil {
		t.Fatal("cancelled run must return an error")
	}
	last := f.events[len(f.events)-1]
	if last.Type != job.EventJobResult || last.Stage != string(job.StateCancelled) {
		t.Fatalf("last event = %s:%s, want job_result:cancelled", last.Type, last.Stage)
	}
	if last.Error == nil || last.Error.Code != CodeCancelled {
		t.Fatalf("job_result error = %+v, want code %s", last.Error, CodeCancelled)
	}
	loadJobFromDisk(t, f, job.StateCancelled, len(f.events))
}

// fakeGen pins the Task 7 hook seam: it records its arguments and emits
// one artifact event through the pipeline's event sink.
type fakeGen struct {
	repoPath      string
	analysisDir   string
	structureJSON string
	summary       map[string]any
}

func (g *fakeGen) Generate(_ context.Context, repoPath, analysisDir, structureJSONPath string, summary map[string]any, sink func(job.Event)) ([]string, error) {
	g.repoPath, g.analysisDir, g.structureJSON, g.summary = repoPath, analysisDir, structureJSONPath, summary
	sink(job.Event{Type: job.EventArtifactProduced, Stage: string(job.StateGenerating),
		Detail: map[string]any{"doc": "README.md"}})
	return []string{"README.md"}, nil
}

func TestDocGenHookInvoked(t *testing.T) {
	f := newFixture(t, true) // llm configured: no docgen degradation
	stubMode(t, "full")
	gen := &fakeGen{}

	res, err := f.run(context.Background(), RunRequest{Source: makeGitRepo(t), Project: "gen"}, gen)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	assertSeq(t, f.events, []string{
		"stage_started:checking",
		"stage_finished:checking",
		"stage_started:preparing",
		"stage_finished:preparing",
		"stage_started:detecting",
		"stage_finished:detecting",
		"stage_started:analyzing",
		"stage_finished:analyzing",
		"stage_started:grading",
		"stage_finished:grading",
		"stage_started:query_tier",
		"stage_finished:query_tier",
		"stage_started:generating",
		"artifact_produced:generating",
		"stage_finished:generating",
		"stage_started:finalizing",
		"stage_finished:finalizing",
		"job_result:done",
	})
	analysisDir := f.analysisDirOf("gen")
	if gen.analysisDir != analysisDir {
		t.Fatalf("generator analysis dir = %q, want %q", gen.analysisDir, analysisDir)
	}
	if gen.structureJSON != filepath.Join(analysisDir, "structure.json") {
		t.Fatalf("generator structure json = %q", gen.structureJSON)
	}
	if status, _ := gen.summary["status"].(string); status != "ok" {
		t.Fatalf("generator summary = %v, want the perceive JSON", gen.summary)
	}
	if len(res.Docs) != 1 || res.Docs[0] != "README.md" {
		t.Fatalf("result docs = %v, want [README.md]", res.Docs)
	}
}

func TestOutputDirOverrideRegistered(t *testing.T) {
	f := newFixture(t, false)
	stubMode(t, "full")
	out := filepath.Join(t.TempDir(), "custom-out")

	res, err := f.run(context.Background(), RunRequest{Source: makeGitRepo(t), Project: "od", OutputDir: out}, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.OutputDir != out {
		t.Fatalf("result output dir = %q, want %q", res.OutputDir, out)
	}
	if _, err := os.Stat(filepath.Join(out, "structure.json")); err != nil {
		t.Fatalf("structure.json not written to output dir: %v", err)
	}
	var reg struct {
		Dirs []string `json:"dirs"`
	}
	readJSON(t, filepath.Join(f.scvHome, "analysis-dirs.json"), &reg)
	found := false
	for _, d := range reg.Dirs {
		if d == out {
			found = true
		}
	}
	if !found {
		t.Fatalf("output dir %s not registered: %v", out, reg.Dirs)
	}
}

func TestRunSmokeRealDiscovery(t *testing.T) {
	stubPerceive(t)
	stubMode(t, "full")
	home := t.TempDir()
	root := filepath.Join(home, "perception")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	// Deterministic environment: fake uv executable, minimal PATH (no go),
	// explicit roots. Real env.Discover runs against it via Run.
	uvBin := filepath.Join(t.TempDir(), "uv")
	if err := os.WriteFile(uvBin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("SCV_HOME", home)
	t.Setenv("SCV_PERCEPTION_ROOT", root)
	t.Setenv("SCV_UV", uvBin)

	cfgPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath, home)
	if err != nil {
		t.Fatal(err)
	}
	var events []job.Event
	res, err := Run(context.Background(), cfg, RunRequest{Source: makeGitRepo(t), Project: "smoke"},
		func(ev job.Event) { events = append(events, ev) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if status, _ := res.Perception["status"].(string); status != "ok" {
		t.Fatalf("perception status = %v, want ok", res.Perception["status"])
	}
	// go is absent from PATH and dist is empty: query tier degraded, so
	// the stub's tier6/tier8 starts must not produce grading events.
	for _, ev := range events {
		if ev.Stage == string(job.StateGrading) || ev.Stage == string(job.StateQueryTier) {
			t.Fatalf("query-degraded run must not emit %s:%s", ev.Type, ev.Stage)
		}
	}
	last := events[len(events)-1]
	if last.Type != job.EventJobResult || last.Stage != string(job.StateDone) {
		t.Fatalf("last event = %s:%s, want job_result:done", last.Type, last.Stage)
	}

	// RunWith with a nil generator behaves like Run (the Task 7 seam).
	events = nil
	res, err = RunWith(context.Background(), cfg, RunRequest{Source: makeGitRepo(t), Project: "smoke2"},
		func(ev job.Event) { events = append(events, ev) }, nil)
	if err != nil {
		t.Fatalf("run with docs: %v", err)
	}
	if status, _ := res.Perception["status"].(string); status != "ok" {
		t.Fatalf("perception status = %v, want ok", res.Perception["status"])
	}
	last = events[len(events)-1]
	if last.Type != job.EventJobResult || last.Stage != string(job.StateDone) {
		t.Fatalf("last event = %s:%s, want job_result:done", last.Type, last.Stage)
	}
}

func TestAppendAnalysisDirDedup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "analysis-dirs.json")
	a, b := "/tmp/a", "/tmp/b"
	if err := AppendAnalysisDir(path, a); err != nil {
		t.Fatal(err)
	}
	if err := AppendAnalysisDir(path, a); err != nil { // dedup
		t.Fatal(err)
	}
	if err := AppendAnalysisDir(path, b); err != nil {
		t.Fatal(err)
	}
	var reg struct {
		Dirs []string `json:"dirs"`
	}
	readJSON(t, path, &reg)
	want := []string{a, b}
	if len(reg.Dirs) != len(want) || reg.Dirs[0] != want[0] || reg.Dirs[1] != want[1] {
		t.Fatalf("registry = %v, want %v (append order, deduped)", reg.Dirs, want)
	}
	if err := AppendAnalysisDir(path, b); err != nil {
		t.Fatal(err)
	}
	readJSON(t, path, &reg)
	if len(reg.Dirs) != 2 {
		t.Fatalf("registry = %v, want still 2 entries", reg.Dirs)
	}
	// A corrupt registry surfaces instead of being silently replaced.
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AppendAnalysisDir(path, a); err == nil {
		t.Fatal("corrupt registry must be an error")
	}
}

func TestParsePerceiveJSONLenient(t *testing.T) {
	m, err := ParsePerceiveJSON(`{"status":"ok","tiers":{"json":true},"future_field":{"nested":[1,2]}}`)
	if err != nil {
		t.Fatalf("valid line: %v", err)
	}
	if m["status"] != "ok" {
		t.Fatalf("status = %v, want ok", m["status"])
	}
	if _, err := ParsePerceiveJSON("this is not json {"); err == nil {
		t.Fatal("garbage line must error")
	}
	if _, err := ParsePerceiveJSON(""); err == nil {
		t.Fatal("empty line must error")
	}
	if _, err := ParsePerceiveJSON(`[1,2,3]`); err == nil {
		t.Fatal("non-object JSON must error")
	}
}

func TestShouldSkipSemantics(t *testing.T) {
	// layout builds an analysis dir with metadata at commit c1 and an
	// optional structure.json, returning its path.
	layout := func(withStructure bool, docs ...string) string {
		dir := filepath.Join(t.TempDir(), "analysis")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if withStructure {
			if err := os.WriteFile(filepath.Join(dir, "structure.json"), []byte("{}"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		for _, d := range docs {
			if err := os.WriteFile(filepath.Join(dir, d), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := WriteMetadata(dir, "/repo", "c1"); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	allDocs := []string{"README.md", "SUMMARY.md", "ARCHITECTURE.md", "FILE_INDEX.md"}

	tests := []struct {
		name   string
		dir    string
		commit string
		docGen bool
		want   bool
	}{
		{"no metadata never skips", t.TempDir(), "c1", false, false},
		{"structure artifact missing", layout(false), "c1", false, false},
		{"commit mismatch", layout(true), "c2", false, false},
		{"no commit identity never skips", layout(true), "", false, false},
		{"commit match structure only", layout(true), "c1", false, true},
		{"docgen with missing docs", layout(true), "c1", true, false},
		{"docgen with all docs", layout(true, allDocs...), "c1", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldSkip(tt.dir, tt.commit, tt.docGen); got != tt.want {
				t.Fatalf("ShouldSkip = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMetadataKeysPort(t *testing.T) {
	dir := t.TempDir()

	// Fresh write: exactly the five scv_util.py keys, scv_version ported.
	if err := WriteMetadata(dir, "/repos/demo", "abc123"); err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	readJSON(t, filepath.Join(dir, ".scv_metadata.json"), &meta)
	for _, key := range []string{"last_analyzed_commit", "last_analyzed_at", "created_at", "repo_path", "scv_version"} {
		if _, ok := meta[key]; !ok {
			t.Fatalf("metadata missing key %q: %v", key, meta)
		}
	}
	if meta["last_analyzed_commit"] != "abc123" || meta["repo_path"] != "/repos/demo" {
		t.Fatalf("metadata = %v", meta)
	}
	if meta["scv_version"] != "2.0" {
		t.Fatalf("scv_version = %v, want the scv_util.py constant 2.0", meta["scv_version"])
	}
	firstCreated, _ := meta["created_at"].(string)
	firstAnalyzed, _ := meta["last_analyzed_at"].(string)
	if firstCreated == "" || firstAnalyzed == "" {
		t.Fatalf("metadata timestamps empty: %v", meta)
	}

	// Rewrite on a new commit: created_at is preserved, the rest refresh.
	if err := WriteMetadata(dir, "/repos/demo", "def456"); err != nil {
		t.Fatal(err)
	}
	readJSON(t, filepath.Join(dir, ".scv_metadata.json"), &meta)
	if meta["last_analyzed_commit"] != "def456" {
		t.Fatalf("rewrite did not update last_analyzed_commit: %v", meta)
	}
	if meta["created_at"] != firstCreated {
		t.Fatalf("created_at changed on rewrite: %v", meta)
	}

	// Additive: keys the engine does not own survive a rewrite.
	if err := os.WriteFile(filepath.Join(dir, ".scv_metadata.json"),
		[]byte(`{"created_at":"2020-01-01T00:00:00Z","custom":"keep"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteMetadata(dir, "/repos/demo", "ghi789"); err != nil {
		t.Fatal(err)
	}
	readJSON(t, filepath.Join(dir, ".scv_metadata.json"), &meta)
	if meta["custom"] != "keep" {
		t.Fatalf("additive key lost: %v", meta)
	}
	if meta["created_at"] != "2020-01-01T00:00:00Z" {
		t.Fatalf("existing created_at not preserved: %v", meta)
	}
}

func TestTierLineParsing(t *testing.T) {
	tests := []struct {
		line   string
		tier   int
		rest   string
		done   bool
		isTier bool
	}{
		{line: "tier1 start", tier: 1, rest: "start", isTier: true},
		{line: "2026-11-13T09:00:00 tier1 detect: structure, source=local", tier: 1, rest: "detect: structure, source=local", isTier: true},
		{line: "2026-11-13T09:00:00 tier2 uv probe: not found", tier: 2, rest: "uv probe: not found", isTier: true},
		{line: "tier3 analyzer rc=1 duration=0.1s x", tier: 3, rest: "analyzer rc=1 duration=0.1s x", isTier: true},
		{line: "tier6 start", tier: 6, rest: "start", isTier: true},
		{line: "tiers done: {'json': True} reasons=[]", done: true, isTier: true},
		{line: "tier1"},              // no payload
		{line: "tiers are done"},     // "tiers are..." is not the done line
		{line: "some human message"}, // not a tier line
		{line: "2026-11-13T09:00:00 note: tier stuff happened"}, // second token is not tierN
		{line: "tierX start"}, // non-numeric tier
	}
	for _, tt := range tests {
		got, ok := parseTierLine(tt.line)
		if ok != tt.isTier {
			t.Fatalf("parseTierLine(%q) ok = %v, want %v", tt.line, ok, tt.isTier)
		}
		if !tt.isTier {
			continue
		}
		if got.tier != tt.tier || got.rest != tt.rest || got.done != tt.done {
			t.Fatalf("parseTierLine(%q) = %+v, want tier=%d rest=%q done=%v",
				tt.line, got, tt.tier, tt.rest, tt.done)
		}
	}

	// Source flag mapping (controller ruling: perceive accepts local|remote only).
	if got := sourceFlag(repo.KindURL); got != "remote" {
		t.Fatalf("sourceFlag(url) = %q, want remote", got)
	}
	for _, kind := range []repo.SourceKind{repo.KindLocalGit, repo.KindLocalDir} {
		if got := sourceFlag(kind); got != "local" {
			t.Fatalf("sourceFlag(%s) = %q, want local", kind, got)
		}
	}
}

func findEventQuiet(events []job.Event, typ, stage string) *job.Event {
	for i, ev := range events {
		if ev.Type == typ && ev.Stage == stage {
			return &events[i]
		}
	}
	return nil
}

func TestPerceiveInBandAnalyzerFailed(t *testing.T) {
	f := newFixture(t, false)
	stubMode(t, "analyzer_failed")

	_, err := f.run(context.Background(), RunRequest{Source: makeGitRepo(t), Project: "af"}, nil)
	if !errors.Is(err, ErrAnalyzerFailed) {
		t.Fatalf("error = %v, want ErrAnalyzerFailed", err)
	}
	// The analyzing tier was open when perceive reported failure in-band.
	failed := findEvent(t, f.events, job.EventStageFailed, string(job.StateAnalyzing))
	if failed.Error == nil || failed.Error.Code != CodeAnalyzerFailed {
		t.Fatalf("stage_failed error = %+v, want code %s", failed.Error, CodeAnalyzerFailed)
	}
	if !strings.Contains(failed.Error.LogTail, "rc=1") {
		t.Fatalf("log_tail = %q, want the analyzer failure line", failed.Error.LogTail)
	}
}

func TestPerceiveNonZeroExitFails(t *testing.T) {
	tests := []struct {
		mode      string
		wantStage string
		wantTail  bool
	}{
		// Stage open at crash, stage closed at crash (last-stage
		// attribution), and no stage ever started (entry-stage default).
		{"crash_open", string(job.StateDetecting), true},
		{"crash", string(job.StateDetecting), true},
		{"exit3", string(job.StateDetecting), false},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			f := newFixture(t, false)
			stubMode(t, tt.mode)

			_, err := f.run(context.Background(), RunRequest{Source: makeGitRepo(t), Project: "crash"}, nil)
			if !errors.Is(err, ErrAnalyzerFailed) {
				t.Fatalf("error = %v, want ErrAnalyzerFailed", err)
			}
			failed := findEvent(t, f.events, job.EventStageFailed, tt.wantStage)
			if failed.Error == nil || failed.Error.Code != CodeAnalyzerFailed {
				t.Fatalf("stage_failed error = %+v, want code %s", failed.Error, CodeAnalyzerFailed)
			}
			if tt.wantTail && failed.Error.LogTail == "" {
				t.Fatal("log_tail empty, want the perception log tail")
			}
		})
	}
}

func TestPipelineGuardErrors(t *testing.T) {
	// Nil config and a config without an scv home are rejected before a
	// job is created.
	if _, err := runPipeline(context.Background(), nil, RunRequest{}, nil, nil, nil); err == nil {
		t.Fatal("nil config must be an error")
	}
	cfg := &config.Config{ScvHome: ""}
	if _, err := runPipeline(context.Background(), cfg, RunRequest{}, nil, nil, nil); err == nil {
		t.Fatal("empty scv home must be an error")
	}

	// A nil sink is replaced with a no-op, and a discovery failure maps
	// to ENV_INCOMPLETE at checking.
	f := newFixture(t, false)
	f.discover = func(env.Options) (*env.Env, error) {
		return nil, errors.New("HOME is empty")
	}
	_, err := f.run(context.Background(), RunRequest{Source: makeGitRepo(t)}, nil)
	if !errors.Is(err, ErrEnvIncomplete) {
		t.Fatalf("error = %v, want ErrEnvIncomplete", err)
	}
	failed := findEvent(t, f.events, job.EventStageFailed, string(job.StateChecking))
	if failed.Error == nil || failed.Error.Code != CodeEnvIncomplete {
		t.Fatalf("stage_failed error = %+v, want code %s", failed.Error, CodeEnvIncomplete)
	}
}

// failingGen exercises the DOCGEN_FAILED mapping of the Task 7 seam.
type failingGen struct{}

func (failingGen) Generate(context.Context, string, string, string, map[string]any, func(job.Event)) ([]string, error) {
	return nil, errors.New("llm command exited 1")
}

func TestDocGenFailureFails(t *testing.T) {
	f := newFixture(t, true)
	stubMode(t, "full")

	_, err := f.run(context.Background(), RunRequest{Source: makeGitRepo(t), Project: "dgf"}, failingGen{})
	if err == nil {
		t.Fatal("docgen failure must fail the run")
	}
	failed := findEvent(t, f.events, job.EventStageFailed, string(job.StateGenerating))
	if failed.Error == nil || failed.Error.Code != CodeDocGenFailed {
		t.Fatalf("stage_failed error = %+v, want code %s", failed.Error, CodeDocGenFailed)
	}
}

// realDocGen builds the production Generator wired to the engine test
// fixtures: engine/tests/fake-llm.sh (SCV_FAKE_MODE selects its scenario)
// and the docgen package's packed-assets testdata mirror. Passing the
// *docgen.Generator straight into the pipeline is also the compile-time
// proof that Generator satisfies the DocGenerator seam. mutate adjusts
// the Generator (e.g. a short Timeout).
func realDocGen(t *testing.T, mode string, mutate func(*docgen.Generator)) *docgen.Generator {
	t.Helper()
	assets, err := filepath.Abs(filepath.Join("..", "docgen", "testdata", "docgen"))
	if err != nil {
		t.Fatalf("resolve docgen testdata: %v", err)
	}
	t.Setenv("SCV_DOCGEN_ASSETS", assets)
	t.Setenv("SCV_FAKE_MODE", mode)
	fake, err := filepath.Abs(filepath.Join("..", "..", "tests", "fake-llm.sh"))
	if err != nil {
		t.Fatalf("resolve fake-llm.sh: %v", err)
	}
	info, err := os.Stat(fake)
	if err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("fake-llm.sh fixture unusable: %v", err)
	}
	g := &docgen.Generator{Command: []string{fake}, Lang: "en"}
	if mutate != nil {
		mutate(g)
	}
	return g
}

// TestDocGenTimeoutMapsStageTimeout proves a docgen attempt timeout
// reaches the pipeline events as STAGE_TIMEOUT (not the generic
// DOCGEN_FAILED) when the real Generator runs through the pipeline.
func TestDocGenTimeoutMapsStageTimeout(t *testing.T) {
	f := newFixture(t, true)
	stubMode(t, "full")
	gen := realDocGen(t, "hang", func(g *docgen.Generator) { g.Timeout = 300 * time.Millisecond })

	start := time.Now()
	_, err := f.run(context.Background(), RunRequest{Source: makeGitRepo(t), Project: "dgt"}, gen)
	if err == nil {
		t.Fatal("docgen timeout must fail the run")
	}
	failed := findEvent(t, f.events, job.EventStageFailed, string(job.StateGenerating))
	if failed.Error == nil || failed.Error.Code != docgen.CodeStageTimeout {
		t.Fatalf("stage_failed error = %+v, want code %s", failed.Error, docgen.CodeStageTimeout)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("hang mode ran %v; the process-group kill did not fire", elapsed)
	}
}

// TestDocGenFailureCarriesLogTail proves a failing LLM command surfaces
// DOCGEN_FAILED with its stderr tail as the stage event's structured
// log_tail field, not only inside the message text.
func TestDocGenFailureCarriesLogTail(t *testing.T) {
	f := newFixture(t, true)
	stubMode(t, "full")
	gen := realDocGen(t, "fail", nil)

	_, err := f.run(context.Background(), RunRequest{Source: makeGitRepo(t), Project: "dgf2"}, gen)
	if err == nil {
		t.Fatal("docgen failure must fail the run")
	}
	failed := findEvent(t, f.events, job.EventStageFailed, string(job.StateGenerating))
	if failed.Error == nil || failed.Error.Code != CodeDocGenFailed {
		t.Fatalf("stage_failed error = %+v, want code %s", failed.Error, CodeDocGenFailed)
	}
	if !strings.Contains(failed.Error.LogTail, "fake-llm: simulated failure") {
		t.Fatalf("stage_failed log_tail = %q, want the fake-llm stderr text", failed.Error.LogTail)
	}
}

func TestFinalizingFailures(t *testing.T) {
	src := makeGitRepo(t)

	// .scv_metadata.json existing as a directory breaks WriteMetadata.
	f := newFixture(t, false)
	out := filepath.Join(t.TempDir(), "out")
	if err := os.MkdirAll(filepath.Join(out, ".scv_metadata.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := f.run(context.Background(), RunRequest{Source: src, Project: "m1", OutputDir: out}, nil); err == nil {
		t.Fatal("metadata write failure must fail the run")
	}
	failed := findEvent(t, f.events, job.EventStageFailed, string(job.StateFinalizing))
	if failed.Error == nil || failed.Error.Code != CodeFinalizeFailed ||
		!strings.Contains(failed.Error.Message, "metadata") {
		t.Fatalf("stage_failed error = %+v, want FINALIZE_FAILED naming metadata", failed.Error)
	}

	// analysis-dirs.json existing as a directory breaks the registry.
	f2 := newFixture(t, false)
	if err := os.Mkdir(filepath.Join(f2.scvHome, "analysis-dirs.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := f2.run(context.Background(), RunRequest{Source: src, Project: "m2"}, nil); err == nil {
		t.Fatal("registry failure must fail the run")
	}
	failed = findEvent(t, f2.events, job.EventStageFailed, string(job.StateFinalizing))
	if failed.Error == nil || failed.Error.Code != CodeFinalizeFailed ||
		!strings.Contains(failed.Error.Message, "register analysis dir") {
		t.Fatalf("stage_failed error = %+v, want FINALIZE_FAILED naming the registry", failed.Error)
	}
}

// lockOnEvent wraps the fixture sink to make the job's event log (or its
// whole directory, when lockDir) unwritable right after the trigger
// event is emitted, exercising the emitter's write-failure branches.
func lockOnEvent(t *testing.T, f *fixture, trigger job.Event, lockDir bool) {
	t.Helper()
	inner := f.sink
	fired := false
	f.sink = func(ev job.Event) {
		inner(ev)
		if fired || ev.Type != trigger.Type || ev.Stage != trigger.Stage {
			return
		}
		fired = true
		jobDir := filepath.Join(f.scvHome, "jobs", ev.ID)
		target := filepath.Join(jobDir, "events.jsonl")
		mode := os.FileMode(0o400)
		if lockDir {
			target, mode = jobDir, 0o500
		}
		if err := os.Chmod(target, mode); err != nil {
			t.Fatalf("lock %s: %v", target, err)
		}
		t.Cleanup(func() { _ = os.Chmod(target, 0o755) })
	}
}

func TestEventLogWriteFailuresSurface(t *testing.T) {
	src := makeGitRepo(t)

	// Events file unwritable before stage_failed: the emit error wins
	// over the semantic error.
	f := newFixture(t, false)
	f.env.Uv = nil
	f.env.PerceptionRoot = filepath.Join(f.scvHome, "no-root")
	lockOnEvent(t, f, job.Event{Type: job.EventStageStarted, Stage: string(job.StateChecking)}, false)
	_, err := f.run(context.Background(), RunRequest{Source: src}, nil)
	if err == nil || !strings.Contains(err.Error(), "emit") {
		t.Fatalf("error = %v, want an emit failure", err)
	}

	// Events file locked once preparing finishes: the next enterStage
	// (detecting, opened implicitly by the not-framework detect line)
	// fails to emit.
	f2 := newFixture(t, false)
	stubMode(t, "not_framework")
	lockOnEvent(t, f2, job.Event{Type: job.EventStageFinished, Stage: string(job.StatePreparing)}, false)
	_, err = f2.run(context.Background(), RunRequest{Source: src, Project: "l2"}, nil)
	if err == nil || !strings.Contains(err.Error(), "emit") {
		t.Fatalf("error = %v, want an emit failure at detecting start", err)
	}

	// Events file locked after finalizing finished: the job_result emit
	// fails (snapshot already succeeded).
	f3 := newFixture(t, false)
	stubMode(t, "not_framework")
	lockOnEvent(t, f3, job.Event{Type: job.EventStageFinished, Stage: string(job.StateFinalizing)}, false)
	_, err = f3.run(context.Background(), RunRequest{Source: src, Project: "l3"}, nil)
	if err == nil || !strings.Contains(err.Error(), "emit") {
		t.Fatalf("error = %v, want a job_result emit failure", err)
	}
}

func TestSnapshotWriteFailuresSurface(t *testing.T) {
	src := makeGitRepo(t)

	// Job dir unwritable when finalizing starts: enterStage's snapshot
	// fails after the stage_started event was emitted.
	f := newFixture(t, false)
	stubMode(t, "not_framework")
	lockOnEvent(t, f, job.Event{Type: job.EventStageFinished, Stage: string(job.StateDetecting)}, true)
	_, err := f.run(context.Background(), RunRequest{Source: src, Project: "s1"}, nil)
	if err == nil || !strings.Contains(err.Error(), "snapshot") {
		t.Fatalf("error = %v, want a snapshot failure", err)
	}

	// Job dir locked between stage_failed and terminate: the terminal
	// snapshot inside fail fails.
	f2 := newFixture(t, false)
	f2.env.Uv = nil
	f2.env.PerceptionRoot = filepath.Join(f2.scvHome, "no-root")
	lockOnEvent(t, f2, job.Event{Type: job.EventStageFailed, Stage: string(job.StateChecking)}, true)
	_, err = f2.run(context.Background(), RunRequest{Source: src}, nil)
	if err == nil || !strings.Contains(err.Error(), "snapshot") {
		t.Fatalf("error = %v, want a terminal snapshot failure", err)
	}
}

func TestForwardToleratesLogFailure(t *testing.T) {
	j, dir, err := job.Create(filepath.Join(t.TempDir(), "jobs"))
	if err != nil {
		t.Fatal(err)
	}
	var sunk []job.Event
	em := &emitter{j: j, dir: dir, sink: func(ev job.Event) { sunk = append(sunk, ev) }}
	// Create the log with one good event, then make it unwritable.
	if _, err := j.Emit(dir, job.EventStageStarted, string(job.StateGenerating), nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "events.jsonl"), 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "events.jsonl"), 0o644) })
	sunk = nil
	// Must not panic and must not deliver the event.
	em.forward(job.Event{Type: job.EventArtifactProduced, Stage: string(job.StateGenerating)})
	if len(sunk) != 0 {
		t.Fatalf("forward delivered %d events despite log failure", len(sunk))
	}
}

func TestReuseLinkRefusesNonSymlink(t *testing.T) {
	f := newFixture(t, false)
	// A regular directory squatting on the entry name: not adoptable.
	if err := os.MkdirAll(filepath.Join(f.scvHome, "repos", "squat"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := f.run(context.Background(), RunRequest{Source: makeGitRepo(t), Project: "squat"}, nil)
	if err == nil {
		t.Fatal("non-symlink entry with the same name must be refused")
	}
	failed := findEvent(t, f.events, job.EventStageFailed, string(job.StatePreparing))
	if failed.Error == nil || failed.Error.Code != CodeNameExists {
		t.Fatalf("stage_failed error = %+v, want code %s", failed.Error, CodeNameExists)
	}
}

func TestRefreshCloneNoURLMatchRefused(t *testing.T) {
	f := newFixture(t, false)
	stubMode(t, "full")
	origin := makeGitRepo(t)
	base := t.TempDir()
	var urls []string
	for _, sub := range []string{"one", "two"} {
		bare := filepath.Join(base, sub, "same-name.git")
		if err := os.MkdirAll(filepath.Dir(bare), 0o755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("git", "clone", "--bare", origin, bare)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git clone --bare: %v\n%s", err, out)
		}
		urls = append(urls, "file://"+bare)
	}

	// First URL clones under the shared derived name and succeeds.
	if _, err := f.run(context.Background(), RunRequest{Source: urls[0]}, nil); err != nil {
		t.Fatalf("first clone run: %v", err)
	}
	// Second URL derives the same name but matches no entry URL: refused
	// instead of silently analyzing the other remote.
	f.events = nil
	if _, err := f.run(context.Background(), RunRequest{Source: urls[1]}, nil); err == nil {
		t.Fatal("same-name different-URL clone must be refused")
	}
	failed := findEvent(t, f.events, job.EventStageFailed, string(job.StatePreparing))
	if failed.Error == nil || failed.Error.Code != CodeNameExists {
		t.Fatalf("stage_failed error = %+v, want code %s", failed.Error, CodeNameExists)
	}
}
