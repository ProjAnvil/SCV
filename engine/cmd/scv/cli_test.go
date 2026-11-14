package main

// CLI tests exec the BUILT scv binary (assembled once in TestMain): they
// exercise the user-facing surface end to end — the `--version`
// handshake output, the perceive version-handshake matrix, the NDJSON
// run contract, the jobs reads (`--since`, status, cancel refusal), the
// repo verbs via the CLI, remove's --confirm gate, and the SIGTERM relay
// that an external `scv jobs cancel` depends on (Task 9 review
// carry-over). Perceive is stubbed through the pipeline's own
// SCV_ENGINE_TEST_PERCEIVE seam.

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// scvBin is the CLI binary built once per test run.
var scvBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "scv-cli-bin")
	if err != nil {
		fmt.Fprintf(os.Stderr, "mktemp: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)
	scvBin = filepath.Join(dir, "scv")
	out, err := exec.Command("go", "build", "-o", scvBin, ".").CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "build scv: %v\n%s", err, out)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// scvRun executes the built CLI with extra KEY=VALUE environment entries
// and returns stdout, stderr, and the exit code.
func scvRun(t *testing.T, env []string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(scvBin, args...)
	cmd.Env = mergeEnv(env)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return stdout.String(), stderr.String(), 0
	case errors.As(err, &ee):
		return stdout.String(), stderr.String(), ee.ExitCode()
	default:
		t.Fatalf("run scv %v: %v", args, err)
		return "", "", -1
	}
}

// mergeEnv overlays extra KEY=VALUE entries onto the process environment,
// replacing existing assignments: a duplicated key would otherwise leave
// the first (host) assignment winning on unix systems.
func mergeEnv(extra []string) []string {
	keys := make(map[string]bool, len(extra))
	for _, kv := range extra {
		k, _, _ := strings.Cut(kv, "=")
		keys[k] = true
	}
	out := make([]string, 0, len(os.Environ())+len(extra))
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if !keys[k] {
			out = append(out, kv)
		}
	}
	return append(out, extra...)
}

// cliStubPerceive is the CLI-level stub of perceive.py. The handshake
// (`--version`) behavior is selected by STUB_VERSION (match,
// major-mismatch, absent); the run behavior by STUB_MODE (full, spawner).
// The run contract mirrors the internal run-package stub: tier lines to
// <analysis_dir>/perception.log, artifacts on disk, one final JSON line
// on stdout.
const cliStubPerceive = `#!/usr/bin/env python3
import json, os, subprocess, sys, time

if "--version" in sys.argv[1:]:
    mode = os.environ.get("STUB_VERSION", "match")
    if mode == "absent":
        # Legacy perceive: argparse rejects --version and exits 2.
        sys.stderr.write("unrecognized arguments: --version\n")
        sys.exit(2)
    contract = "2.0" if mode == "major-mismatch" else "1.0"
    print(json.dumps({"scv_version": "v9.9.9", "contract": contract}))
    sys.exit(0)

repo, ad = sys.argv[1], sys.argv[2]
os.makedirs(ad, exist_ok=True)
mode = os.environ.get("STUB_MODE", "full")

def log(msg):
    with open(os.path.join(ad, "perception.log"), "a", encoding="utf-8") as fh:
        fh.write(msg + "\n")

if mode == "spawner":
    # A grandchild inside the perceive process group: it survives a
    # child-only kill and dies only when the engine relays cancellation
    # into the run context (the SIGTERM relay under test).
    child = subprocess.Popen(["sleep", "60"])
    with open(os.path.join(ad, ".stub-pids.json"), "w", encoding="utf-8") as fh:
        json.dump({"perceive": os.getpid(), "child": child.pid}, fh)
    time.sleep(30)
    print("{}")
    sys.exit(0)

log("tier1 start")
log("tier1 detect: structure, source=local, extensions=[], languages=['go']")
log("tier2 start")
log("tier2 uv probe: /fake/uv")
log("tier3 start")
log("tier3 analyzer rc=0 duration=0.1s fake-analyzer")
with open(os.path.join(ad, "structure.json"), "w", encoding="utf-8") as fh:
    json.dump({"nodes": [], "edges": []}, fh)
log("tier6 start")
with open(os.path.join(ad, "structure.vg"), "w", encoding="utf-8") as fh:
    fh.write("VG\n")
log("tier8 start")
log("tiers done: {'json': True, 'vg': True, 'mcp_hint': True} reasons=[]")
print(json.dumps({"status": "ok", "reason": None,
                  "tiers": {"json": True, "vg": True, "mcp_hint": True},
                  "artifacts": {"structure_json": os.path.join(ad, "structure.json"),
                                "structure_vg": os.path.join(ad, "structure.vg")}}))
`

// writeStubPerceive writes the stub script into a temp dir and returns
// its path (to be handed to the engine via SCV_ENGINE_TEST_PERCEIVE).
func writeStubPerceive(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "perceive.py")
	if err := os.WriteFile(path, []byte(cliStubPerceive), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// runEnv builds the hermetic environment for CLI runs against the stub
// perceive: HOME pinned to a temp dir (discovery resolves relative to
// it), every SCV_* override neutralized so the --scv-home flag is the
// only input, and the stub seam variables set.
func runEnv(t *testing.T, stub, mode string) []string {
	t.Helper()
	return []string{
		"SCV_ENGINE_TEST_PERCEIVE=" + stub,
		"STUB_MODE=" + mode,
		"STUB_VERSION=match",
		"HOME=" + t.TempDir(),
		"SCV_HOME=",
		"SCV_UV=",
		"SCV_PERCEPTION_ROOT=",
		"SCV_PERCEPTION_DIST=",
	}
}

// setEnv replaces one KEY=VALUE entry within an env slice.
func setEnv(env []string, key, value string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if k, _, _ := strings.Cut(kv, "="); k == key {
			continue
		}
		out = append(out, kv)
	}
	return append(out, key+"="+value)
}

// newTestHome creates a hermetic scv home: the perception/ root the
// checking gate requires, and a config that disables the structure tier
// so runs need no uv on the host.
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
	return home
}

// makeGitRepo creates a real git worktree with one commit; it is the run
// source the pipeline links under the scv home.
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

// ndjsonLines parses every line of an NDJSON stream as a JSON object and
// fails on any blank or unparseable line — the exact `scv run` stdout
// contract under test.
func ndjsonLines(t *testing.T, s string) []map[string]any {
	t.Helper()
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		t.Fatal("no NDJSON output")
	}
	raw := strings.Split(s, "\n")
	out := make([]map[string]any, 0, len(raw))
	for i, line := range raw {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("NDJSON line %d is not a JSON object: %q", i+1, line)
		}
		out = append(out, m)
	}
	return out
}

// assertJobResultLast asserts the NDJSON contract's terminal rule: the
// last line is a job_result event.
func assertJobResultLast(t *testing.T, s string) map[string]any {
	t.Helper()
	all := ndjsonLines(t, s)
	last := all[len(all)-1]
	if last["type"] != "job_result" {
		t.Fatalf("last NDJSON line type = %v, want job_result: %v", last["type"], last)
	}
	return last
}

// onlyJobID returns the single job id recorded under the home.
func onlyJobID(t *testing.T, home string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(home, "jobs"))
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want exactly 1 job under %s, got %d", home, len(entries))
	}
	return entries[0].Name()
}

// pollUntil polls cond every 50ms until it holds or the deadline passes.
func pollUntil(t *testing.T, d time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout: %s", msg)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// runOnceForTest runs the CLI once against the stub perceive (full mode)
// and returns the home, the job id, and the run's stdout.
func runOnceForTest(t *testing.T, project string) (home, id, stdout string) {
	t.Helper()
	stub := writeStubPerceive(t)
	src := makeGitRepo(t)
	home = newTestHome(t)
	env := runEnv(t, stub, "full")
	stdout, stderr, code := scvRun(t, env, "run", src, "--project", project, "--scv-home", home)
	if code != 0 {
		t.Fatalf("scv run exit = %d, want 0; stderr:\n%s", code, stderr)
	}
	assertJobResultLast(t, stdout)
	return home, onlyJobID(t, home), stdout
}

func TestVersionHandshakeOutput(t *testing.T) {
	stdout, _, code := scvRun(t, nil, "--version")
	if code != 0 {
		t.Fatalf("scv --version exit = %d, want 0", code)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &v); err != nil {
		t.Fatalf("scv --version output is not one JSON object: %q: %v", stdout, err)
	}
	for _, key := range []string{"engine", "contract_min", "contract_max"} {
		if _, ok := v[key]; !ok {
			t.Errorf("scv --version output missing key %q: %s", key, stdout)
		}
	}
}

func TestVersionHandshakeMatrix(t *testing.T) {
	stub := writeStubPerceive(t)
	src := makeGitRepo(t)

	t.Run("contract match proceeds", func(t *testing.T) {
		home := newTestHome(t)
		env := runEnv(t, stub, "full") // STUB_VERSION=match
		stdout, stderr, code := scvRun(t, env, "run", src, "--project", "proj", "--scv-home", home)
		if code != 0 {
			t.Fatalf("exit = %d, want 0; stderr:\n%s", code, stderr)
		}
		for _, line := range ndjsonLines(t, stdout) {
			if line["type"] == "degraded" {
				if detail, _ := line["detail"].(map[string]any); detail["reason"] == "contract-unknown" {
					t.Errorf("matched contract still emitted contract-unknown: %v", line)
				}
			}
		}
		assertJobResultLast(t, stdout)
	})

	t.Run("major mismatch fails fast", func(t *testing.T) {
		home := newTestHome(t)
		env := setEnv(runEnv(t, stub, "full"), "STUB_VERSION", "major-mismatch")
		stdout, stderr, code := scvRun(t, env, "run", src, "--project", "proj", "--scv-home", home)
		if code == 0 {
			t.Fatalf("exit = 0, want non-0 for a major contract mismatch")
		}
		if !strings.Contains(stderr, "VERSION_INCOMPATIBLE") {
			t.Errorf("stderr lacks VERSION_INCOMPATIBLE: %q", stderr)
		}
		if strings.TrimSpace(stdout) != "" {
			t.Errorf("stdout should carry no events on a failed handshake: %q", stdout)
		}
		if _, err := os.Stat(filepath.Join(home, "jobs")); !os.IsNotExist(err) {
			t.Errorf("no job should be created on a failed handshake (stat err = %v)", err)
		}
	})

	t.Run("major mismatch with force proceeds", func(t *testing.T) {
		home := newTestHome(t)
		env := setEnv(runEnv(t, stub, "full"), "STUB_VERSION", "major-mismatch")
		stdout, stderr, code := scvRun(t, env, "run", src, "--project", "proj", "--force", "--scv-home", home)
		if code != 0 {
			t.Fatalf("exit = %d, want 0 with --force; stderr:\n%s", code, stderr)
		}
		assertJobResultLast(t, stdout)
	})

	t.Run("absent version degrades to contract-unknown", func(t *testing.T) {
		home := newTestHome(t)
		env := setEnv(runEnv(t, stub, "full"), "STUB_VERSION", "absent")
		stdout, stderr, code := scvRun(t, env, "run", src, "--project", "proj", "--scv-home", home)
		if code != 0 {
			t.Fatalf("exit = %d, want 0 (legacy perceive proceeds degraded); stderr:\n%s", code, stderr)
		}
		var degraded map[string]any
		for _, line := range ndjsonLines(t, stdout) {
			if line["type"] == "degraded" {
				if detail, _ := line["detail"].(map[string]any); detail["reason"] == "contract-unknown" {
					degraded = line
				}
			}
		}
		if degraded == nil {
			t.Fatalf("no degraded contract-unknown event in stdout: %s", stdout)
		}
		assertJobResultLast(t, stdout)
	})
}

func TestRunNDJSONContract(t *testing.T) {
	stub := writeStubPerceive(t)
	src := makeGitRepo(t)
	home := newTestHome(t)
	env := runEnv(t, stub, "full")
	stdout, stderr, code := scvRun(t, env, "run", src, "--project", "proj", "--scv-home", home)
	if code != 0 {
		t.Fatalf("scv run exit = %d, want 0; stderr:\n%s", code, stderr)
	}

	all := ndjsonLines(t, stdout)
	for i, ev := range all {
		if seq, _ := ev["seq"].(float64); int(seq) != i+1 {
			t.Errorf("line %d has seq %v, want %d (monotonic from 1)", i+1, ev["seq"], i+1)
		}
		for _, key := range []string{"id", "ts", "type", "stage"} {
			if _, ok := ev[key]; !ok {
				t.Errorf("line %d missing key %q: %v", i+1, key, ev)
			}
		}
	}
	last := assertJobResultLast(t, stdout)
	if last["stage"] != "done" {
		t.Errorf("job_result stage = %v, want done", last["stage"])
	}

	// The stdout stream and the durable events.jsonl are the same
	// events: one schema, one sequence.
	id, _ := all[0]["id"].(string)
	data, err := os.ReadFile(filepath.Join(home, "jobs", id, "events.jsonl"))
	if err != nil {
		t.Fatalf("read events.jsonl: %v", err)
	}
	disk := ndjsonLines(t, string(data))
	if len(disk) != len(all) {
		t.Fatalf("stdout has %d events, events.jsonl has %d", len(all), len(disk))
	}
	for i := range all {
		if all[i]["seq"] != disk[i]["seq"] || all[i]["type"] != disk[i]["type"] || all[i]["id"] != disk[i]["id"] {
			t.Errorf("event %d differs between stdout and events.jsonl:\n stdout: %v\n disk:   %v", i+1, all[i], disk[i])
		}
	}
}

func TestJobsEventsSince(t *testing.T) {
	home, id, _ := runOnceForTest(t, "proj")

	t.Run("full log", func(t *testing.T) {
		stdout, stderr, code := scvRun(t, nil, "jobs", "events", id, "--scv-home", home)
		if code != 0 {
			t.Fatalf("exit = %d, want 0; stderr:\n%s", code, stderr)
		}
		if got := len(ndjsonLines(t, stdout)); got == 0 {
			t.Fatal("jobs events printed no events")
		}
	})

	t.Run("since filters", func(t *testing.T) {
		fullOut, _, _ := scvRun(t, nil, "jobs", "events", id, "--scv-home", home)
		full := ndjsonLines(t, fullOut)

		stdout, stderr, code := scvRun(t, nil, "jobs", "events", id, "--since", "3", "--scv-home", home)
		if code != 0 {
			t.Fatalf("exit = %d, want 0; stderr:\n%s", code, stderr)
		}
		got := ndjsonLines(t, stdout)
		if len(got) != len(full)-3 {
			t.Fatalf("--since 3 returned %d events, want %d (all after seq 3)", len(got), len(full)-3)
		}
		for _, ev := range got {
			if seq, _ := ev["seq"].(float64); int(seq) <= 3 {
				t.Errorf("--since 3 returned seq %v, want > 3", ev["seq"])
			}
		}
	})

	t.Run("status shows the recorded state", func(t *testing.T) {
		stdout, stderr, code := scvRun(t, nil, "jobs", "status", id, "--scv-home", home, "--json")
		if code != 0 {
			t.Fatalf("exit = %d, want 0; stderr:\n%s", code, stderr)
		}
		var j map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &j); err != nil {
			t.Fatalf("jobs status --json is not one JSON object: %q", stdout)
		}
		if j["state"] != "done" {
			t.Errorf("job state = %v, want done", j["state"])
		}
	})

	t.Run("list shows the job", func(t *testing.T) {
		stdout, stderr, code := scvRun(t, nil, "jobs", "list", "--scv-home", home, "--json")
		if code != 0 {
			t.Fatalf("exit = %d, want 0; stderr:\n%s", code, stderr)
		}
		var jobs []map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &jobs); err != nil {
			t.Fatalf("jobs list --json is not a JSON array: %q", stdout)
		}
		if len(jobs) != 1 || jobs[0]["id"] != id {
			t.Errorf("jobs list = %v, want the single job %s", jobs, id)
		}
	})
}

func TestJobsCancelTerminalRefuses(t *testing.T) {
	home, id, _ := runOnceForTest(t, "proj")
	_, stderr, code := scvRun(t, nil, "jobs", "cancel", id, "--scv-home", home)
	if code == 0 {
		t.Fatal("cancelling a terminal job exited 0, want non-0")
	}
	if !strings.Contains(stderr, "WORKFLOW_NOT_CANCELLABLE") {
		t.Errorf("stderr lacks WORKFLOW_NOT_CANCELLABLE: %q", stderr)
	}
}

// TestRunSignalRelayOnExternalCancel pins the Task 9 review carry-over:
// `scv run` must install a SIGTERM handler that cancels its run context,
// so an external `scv jobs cancel` (SIGTERM to the engine's process
// group) unwinds the pipeline and kills the perceive subtree instead of
// orphaning it. The spawner stub's grandchild shares the perceive child's
// own process group and survives anything but the engine relaying the
// cancel into its run context.
func TestRunSignalRelayOnExternalCancel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group signal semantics are unix-only")
	}
	stub := writeStubPerceive(t)
	src := makeGitRepo(t)
	home := newTestHome(t)
	pidsPath := filepath.Join(home, "analysis", "proj", ".stub-pids.json")

	run := exec.Command(scvBin, "run", src, "--project", "proj", "--scv-home", home)
	run.Env = mergeEnv(runEnv(t, stub, "spawner"))
	stdout, err := run.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var runStderr strings.Builder
	run.Stderr = &runStderr
	if err := run.Start(); err != nil {
		t.Fatal(err)
	}

	// The first NDJSON line carries the job id (the pid file is written
	// before any event, so the group is already addressable).
	reader := bufio.NewReader(stdout)
	firstLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("no first event from scv run: %v; stderr:\n%s", err, runStderr.String())
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(firstLine), &first); err != nil {
		t.Fatalf("first stdout line is not JSON: %q", firstLine)
	}
	id, _ := first["id"].(string)
	if id == "" {
		t.Fatalf("first event carries no job id: %q", firstLine)
	}

	// Wait until perceive spawned its grandchild, then cancel externally.
	pollUntil(t, 15*time.Second, func() bool {
		_, err := os.Stat(pidsPath)
		return err == nil
	}, "perceive never spawned its subtree")
	var pids struct {
		Perceive int `json:"perceive"`
		Child    int `json:"child"`
	}
	if data, err := os.ReadFile(pidsPath); err != nil {
		t.Fatalf("read stub pids: %v", err)
	} else if err := json.Unmarshal(data, &pids); err != nil {
		t.Fatalf("parse stub pids: %v", err)
	}

	_, cancelStderr, code := scvRun(t, runEnv(t, stub, "spawner"), "jobs", "cancel", id, "--scv-home", home)
	if code != 0 {
		t.Fatalf("scv jobs cancel exit = %d, want 0; stderr:\n%s", code, cancelStderr)
	}

	rest, _ := io.ReadAll(reader)
	runErr := run.Wait()
	var ee *exec.ExitError
	if !errors.As(runErr, &ee) {
		t.Fatalf("scv run did not exit with a failure code: %v; stderr:\n%s", runErr, runStderr.String())
	}
	if ee.ExitCode() != exitCancelled {
		t.Errorf("scv run exit = %d, want %d (cancelled); stderr:\n%s", ee.ExitCode(), exitCancelled, runStderr.String())
	}

	// The engine recorded its own cancelled job_result as the last line.
	last := assertJobResultLast(t, firstLine+string(rest))
	if last["stage"] != "cancelled" {
		t.Errorf("job_result stage = %v, want cancelled", last["stage"])
	}
	data, err := os.ReadFile(filepath.Join(home, "jobs", id, "job.json"))
	if err != nil {
		t.Fatalf("read job.json: %v", err)
	}
	var j map[string]any
	if err := json.Unmarshal(data, &j); err != nil {
		t.Fatalf("parse job.json: %v", err)
	}
	if j["state"] != "cancelled" {
		t.Errorf("job.json state = %v, want cancelled", j["state"])
	}

	// The whole perceive subtree died with the run — the relay worked.
	// A missing relay kills only the engine (default TERM disposition)
	// and leaves this child running for another minute.
	pollUntil(t, 5*time.Second, func() bool {
		return syscall.Kill(pids.Child, 0) == syscall.ESRCH
	}, fmt.Sprintf("perceive grandchild pid %d survived the external cancel (signal relay broken)", pids.Child))
}

func TestRepoLinkViaCLI(t *testing.T) {
	src := makeGitRepo(t)
	home := newTestHome(t)
	name := filepath.Base(src)

	stdout, stderr, code := scvRun(t, nil, "repo", "link", src, "--scv-home", home, "--json")
	if code != 0 {
		t.Fatalf("repo link exit = %d, want 0; stderr:\n%s", code, stderr)
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &entry); err != nil {
		t.Fatalf("repo link --json is not one JSON object: %q", stdout)
	}
	if entry["name"] != name || entry["type"] != "linked" {
		t.Errorf("repo link entry = %v, want name %q type linked", entry, name)
	}
	info, err := os.Lstat(filepath.Join(home, "repos", name))
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("repos entry %s is not a symlink (err = %v)", name, err)
	}

	listOut, _, code := scvRun(t, nil, "repo", "list", "--scv-home", home, "--json")
	if code != 0 {
		t.Fatalf("repo list exit = %d, want 0", code)
	}
	var entries []map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(listOut)), &entries); err != nil {
		t.Fatalf("repo list --json is not a JSON array: %q", listOut)
	}
	if len(entries) != 1 || entries[0]["name"] != name || entries[0]["type"] != "linked" {
		t.Errorf("repo list = %v, want the single linked entry %q", entries, name)
	}

	// A second link of the same source is refused with the typed code.
	_, stderr, code = scvRun(t, nil, "repo", "link", src, "--scv-home", home)
	if code != exitNameExists {
		t.Errorf("second repo link exit = %d, want %d (NAME_EXISTS); stderr:\n%s", code, exitNameExists, stderr)
	}
}

func TestRemoveRequiresConfirm(t *testing.T) {
	src := makeGitRepo(t)
	home := newTestHome(t)
	name := filepath.Base(src)
	entry := filepath.Join(home, "repos", name)
	analysis := filepath.Join(home, "analysis", name)

	link := func() {
		_, stderr, code := scvRun(t, nil, "repo", "link", src, "--scv-home", home)
		if code != 0 {
			t.Fatalf("repo link exit = %d, want 0; stderr:\n%s", code, stderr)
		}
	}
	analysisMarker := func() {
		if err := os.MkdirAll(analysis, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(analysis, "structure.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	link()
	analysisMarker()

	// Without --confirm the remove is refused and nothing is deleted.
	_, stderr, code := scvRun(t, nil, "repo", "remove", name, "--scv-home", home)
	if code == 0 {
		t.Fatal("repo remove without --confirm exited 0, want non-0")
	}
	if !strings.Contains(stderr, "confirm") {
		t.Errorf("stderr should explain the --confirm requirement: %q", stderr)
	}
	if _, err := os.Lstat(entry); err != nil {
		t.Fatalf("unconfirmed remove deleted the entry anyway: %v", err)
	}

	// With --confirm the entry goes, the analysis dir stays, and the
	// output says so.
	stdout, stderr, code := scvRun(t, nil, "repo", "remove", name, "--confirm", "--scv-home", home, "--json")
	if code != 0 {
		t.Fatalf("repo remove --confirm exit = %d, want 0; stderr:\n%s", code, stderr)
	}
	if _, err := os.Lstat(entry); !os.IsNotExist(err) {
		t.Fatalf("entry still exists after confirmed remove (err = %v)", err)
	}
	if _, err := os.Stat(analysis); err != nil {
		t.Fatalf("analysis dir must survive a plain remove: %v", err)
	}
	var removed map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &removed); err != nil {
		t.Fatalf("repo remove --json is not one JSON object: %q", stdout)
	}
	if removed["analysis_removed"] != false {
		t.Errorf("repo remove --json analysis_removed = %v, want false", removed["analysis_removed"])
	}

	// --with-analysis removes the analysis dir too.
	link()
	analysisMarker()
	_, stderr, code = scvRun(t, nil, "repo", "remove", name, "--confirm", "--with-analysis", "--scv-home", home)
	if code != 0 {
		t.Fatalf("repo remove --confirm --with-analysis exit = %d, want 0; stderr:\n%s", code, stderr)
	}
	if _, err := os.Stat(analysis); !os.IsNotExist(err) {
		t.Errorf("analysis dir still exists after --with-analysis remove (err = %v)", err)
	}
}

func TestQueryReposEmpty(t *testing.T) {
	home := newTestHome(t)
	stdout, stderr, code := scvRun(t, nil, "query", "repos", "--scv-home", home, "--json")
	if code != 0 {
		t.Fatalf("query repos exit = %d, want 0 on a fresh install; stderr:\n%s", code, stderr)
	}
	var repos []any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &repos); err != nil {
		t.Fatalf("query repos --json is not a JSON array: %q", stdout)
	}
	if len(repos) != 0 {
		t.Errorf("query repos = %v, want empty on a fresh install", repos)
	}
}

func TestEnvJSONOutput(t *testing.T) {
	home := newTestHome(t)
	stdout, stderr, code := scvRun(t, nil, "env", "--scv-home", home, "--json")
	if code != 0 {
		t.Fatalf("scv env exit = %d, want 0 (always); stderr:\n%s", code, stderr)
	}
	var snap map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &snap); err != nil {
		t.Fatalf("scv env --json is not one JSON object: %q", stdout)
	}
	if snap["scv_home"] != home {
		t.Errorf("scv_home = %v, want %s", snap["scv_home"], home)
	}
	if _, ok := snap["diagnostics"]; !ok {
		t.Errorf("scv env output lacks diagnostics: %s", stdout)
	}
}
