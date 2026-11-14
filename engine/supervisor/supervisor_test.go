package supervisor_test

// External-package tests: the supervisor is imported the way an
// embedding host would import it, and the engine seams it exposes are
// exercised for real — the pipeline runs in-process against the
// SCV_ENGINE_TEST_PERCEIVE stub, the same seam the run-package and CLI
// tests use.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/projanvil/scv/engine/internal/config"
	"github.com/projanvil/scv/engine/internal/job"
	"github.com/projanvil/scv/engine/internal/run"
	"github.com/projanvil/scv/engine/supervisor"
)

// stubPerceive is the minimal faithful perceive stub for a successful
// structure-off run: it writes no tier lines (so the event sequence is
// exactly the pipeline's own stage events) and prints the final
// status-ok JSON line on stdout.
const stubPerceive = `#!/usr/bin/env python3
import json, os, sys
repo, ad = sys.argv[1], sys.argv[2]
os.makedirs(ad, exist_ok=True)
print(json.dumps({"status": "ok", "reason": None,
                  "tiers": {"json": True, "vg": False, "mcp_hint": False},
                  "artifacts": {}}))
`

// newTestHome builds a hermetic scv home: the perception/ root the
// checking gate requires and a config that disables the structure tier,
// so runs need no uv on the host (the same shape as the CLI tests'
// newTestHome).
func newTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "perception"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `{"structure_analysis_enabled": false}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SCV_PERCEPTION_ROOT", filepath.Join(home, "perception"))
	return home
}

// stubPerceiveEnv points the pipeline at the stub script through its
// own test seam.
func stubPerceiveEnv(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "perceive.py")
	if err := os.WriteFile(path, []byte(stubPerceive), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SCV_ENGINE_TEST_PERCEIVE", path)
}

// repoRoot walks up from the package dir to the repository checkout
// root: the first ancestor containing perception/ — the same locator the
// docgen package tests use.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if info, err := os.Stat(filepath.Join(dir, "perception")); err == nil && info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository root (directory containing perception/) not found")
		}
		dir = parent
	}
}

// TestSupervisorRunDocgen pins the supervisor's docgen wiring: a config
// with an llm section (command mode, the shared fake-llm.sh fixture)
// must produce the four documents from a supervised run — the embedding
// host gets the same generating stage the scv CLI wires, not a silent
// structure-only run. Asset resolution is pinned to the committed
// testdata mirror via SCV_DOCGEN_ASSETS, the docgen package's test
// override seam, so the tmp perception root needs no pack.
func TestSupervisorRunDocgen(t *testing.T) {
	home := newTestHome(t)
	stubPerceiveEnv(t)
	root := repoRoot(t)
	fakeLLM := filepath.Join(root, "engine", "tests", "fake-llm.sh")
	if info, err := os.Stat(fakeLLM); err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("fake-llm.sh fixture unusable: %v (mode %v)", err, info.Mode())
	}
	cfgBody := fmt.Sprintf(`{"structure_analysis_enabled": false, "llm": {"mode": "command", "command": ["/bin/sh", %q], "timeout_s": 60, "retry": 0}}`+"\n", fakeLLM)
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(cfgBody), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SCV_DOCGEN_ASSETS", filepath.Join(root, "engine", "internal", "docgen", "testdata", "docgen"))
	cfg := loadTestConfig(t, home)
	if cfg.LLM == nil {
		t.Fatal("test config lost its llm section")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	sup, err := supervisor.New(ctx, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	h, err := sup.Run(ctx, run.RunRequest{Source: t.TempDir(), Lang: "en"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	res, err := h.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}

	wantDocs := []string{"README.md", "SUMMARY.md", "ARCHITECTURE.md", "FILE_INDEX.md"}
	if len(res.Docs) != len(wantDocs) {
		t.Fatalf("Wait result docs = %v, want %d entries", res.Docs, len(wantDocs))
	}
	for i, name := range wantDocs {
		if res.Docs[i] != filepath.Join(res.OutputDir, name) {
			t.Errorf("docs[%d] = %q, want %q", i, res.Docs[i], filepath.Join(res.OutputDir, name))
		}
		data, rerr := os.ReadFile(filepath.Join(res.OutputDir, name))
		if rerr != nil {
			t.Errorf("document %s missing: %v", name, rerr)
			continue
		}
		fixture, ferr := os.ReadFile(filepath.Join(root, "engine", "tests", "testdata",
			strings.TrimSuffix(strings.ToLower(name), ".md")+".md"))
		if ferr != nil {
			t.Fatalf("read fixture for %s: %v", name, ferr)
		}
		if !bytes.Equal(data, fixture) {
			t.Errorf("document %s content mismatch:\ngot:  %q\nwant: %q", name, data, fixture)
		}
	}

	// The generating stage landed in the durable event log with its
	// per-doc sub-events.
	events, err := h.Events(ctx, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	var generating bool
	for _, ev := range events {
		if ev.Stage == string(job.StateGenerating) {
			generating = true
		}
	}
	if !generating {
		t.Errorf("no generating-stage events in the job log; stages: %+v", events)
	}
}

// loadTestConfig loads the hermetic home's config the way a host would.
func loadTestConfig(t *testing.T, home string) config.Config {
	t.Helper()
	cfg, err := config.Load(filepath.Join(home, "config.json"), home)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return *cfg
}

// TestSupervisorRunEndToEnd drives the whole façade: Run returns a
// handle whose id exists immediately, Status polls the durable job
// snapshot to a terminal state, Wait delivers the pipeline result, and
// Events replays the log with the since cursor.
func TestSupervisorRunEndToEnd(t *testing.T) {
	home := newTestHome(t)
	stubPerceiveEnv(t)
	cfg := loadTestConfig(t, home)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup, err := supervisor.New(ctx, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	h, err := sup.Run(ctx, run.RunRequest{Source: t.TempDir()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if h.ID == "" {
		t.Fatal("Run returned an empty job id")
	}

	// The handle is pollable the moment Run returns.
	j, err := h.Status(ctx)
	if err != nil {
		t.Fatalf("immediate Status: %v", err)
	}
	if j.ID != h.ID {
		t.Fatalf("immediate status id %q != handle id %q", j.ID, h.ID)
	}

	// Poll Status to a terminal state; the stub run finishes in well
	// under a second, the budget only guards a hung host.
	deadline := time.Now().Add(30 * time.Second)
	for {
		if j, err = h.Status(ctx); err != nil {
			t.Fatalf("Status: %v", err)
		}
		if job.IsTerminal(j.State) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job never reached a terminal state; last state %s", j.State)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if j.State != job.StateDone {
		t.Fatalf("terminal state = %s, want %s (detail %v)", j.State, job.StateDone, j.Detail)
	}

	res, err := h.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if res == nil || res.OutputDir == "" {
		t.Fatalf("Wait returned no analysis dir: %+v", res)
	}
	if status, _ := res.Perception["status"].(string); status != "ok" {
		t.Fatalf("perception status = %v, want ok", res.Perception["status"])
	}

	events, err := h.Events(ctx, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) < 5 { // checking, preparing, finalizing pairs + result
		t.Fatalf("got %d events, want at least the pipeline stage events: %+v", len(events), events)
	}
	first, last := events[0], events[len(events)-1]
	if first.Type != job.EventStageStarted || first.Stage != string(job.StateChecking) {
		t.Fatalf("first event = %s/%s, want stage_started/checking", first.Type, first.Stage)
	}
	if last.Type != job.EventJobResult || last.Stage != string(job.StateDone) {
		t.Fatalf("last event = %s/%s, want job_result/done", last.Type, last.Stage)
	}
	for i, ev := range events {
		if ev.ID != h.ID {
			t.Fatalf("event %d carries id %q, want %q", i, ev.ID, h.ID)
		}
		if ev.Seq != i+1 {
			t.Fatalf("event %d has seq %d, want %d (monotonic from 1)", i, ev.Seq, i+1)
		}
	}

	tail, err := h.Events(ctx, 1)
	if err != nil {
		t.Fatalf("Events since 1: %v", err)
	}
	if len(tail) != len(events)-1 {
		t.Fatalf("Events since 1 returned %d events, want %d", len(tail), len(events)-1)
	}
}

// TestSupervisorEnvAndQuery covers the two thin constructors: Env
// discovers the environment snapshot for the supervisor's scv home,
// and Query opens the store over the analysis-dir registry.
func TestSupervisorEnvAndQuery(t *testing.T) {
	home := newTestHome(t)
	cfg := loadTestConfig(t, home)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup, err := supervisor.New(ctx, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	e, err := sup.Env(ctx)
	if err != nil {
		t.Fatalf("Env: %v", err)
	}
	if e.ScvHome != home {
		t.Fatalf("env scv home = %q, want %q", e.ScvHome, home)
	}
	if e.PerceptionRoot != filepath.Join(home, "perception") {
		t.Fatalf("env perception root = %q, want %q", e.PerceptionRoot, filepath.Join(home, "perception"))
	}

	// A fresh home has no registry: the store opens over zero roots.
	store, err := sup.Query()
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if repos, err := store.Repos(); err != nil || len(repos) != 0 {
		t.Fatalf("fresh Query Repos = %v, %v; want empty, nil", repos, err)
	}

	// A registered analysis dir with one snapshot shows up after a
	// fresh open (the run pipeline's finalizing writes the registry).
	ad := t.TempDir()
	repoDir := filepath.Join(ad, "demo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "structure.vg"), []byte("VG\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := json.Marshal(map[string]any{"dirs": []string{ad}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "analysis-dirs.json"), reg, 0o644); err != nil {
		t.Fatal(err)
	}

	store, err = sup.Query()
	if err != nil {
		t.Fatalf("Query after registry: %v", err)
	}
	repos, err := store.Repos()
	if err != nil {
		t.Fatalf("Repos: %v", err)
	}
	if len(repos) != 1 || repos[0]["repo"] != "demo" {
		t.Fatalf("Repos = %+v, want one entry for demo", repos)
	}
}
