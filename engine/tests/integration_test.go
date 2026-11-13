//go:build integration

// Package tests holds the opt-in integration golden suite (plan task 13):
// the real scv binary driving the real perception toolchain — perceive.py,
// the uv-run analyzer, the loader/graphmcp dist — over the shipped
// fixture repos, asserting the full §7 event sequences and artifacts.
//
// The suite is excluded from unit runs by the `integration` build tag and
// wired as `make engine-test-integration`. Machine prerequisites, the
// fixture inventory, and runtime expectations are documented in README.md
// next to this file.
//
// Harness shape:
//   - TestMain builds the scv binary and a shared real dist (loader +
//     graphmcp) once; each test gets its own tmp SCV_HOME and dist, so
//     nothing ever writes into the repo checkout or the real ~/.scv.
//   - Fixtures under perception/fixture/repos are NOT git repos; every
//     test copies one into a tmp dir and git-init/commits it with a
//     pinned identity, so preparing/HeadCommit/skip semantics run against
//     a real HEAD. Fixture copies are asserted unmutated (git status)
//     after every case.
//   - Toolchain-shaped cases (go/uv missing) rewrite PATH by filtering
//     out every entry that can produce the tool — the same technique as
//     the perception suite's _path_without.
//   - The cancel case replaces perceive with a deterministic slow shim
//     through the SCV_ENGINE_TEST_PERCEIVE seam instead of racing the
//     real analyzer.
package tests

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// scvRunTimeout bounds one `scv` invocation: a real analyzer run takes
// seconds on a warm venv, but the first-ever uv provisioning of
// perception/analyzer can take well over a minute.
const scvRunTimeout = 300 * time.Second

// buildTimeout bounds the TestMain `go build` calls.
const buildTimeout = 10 * time.Minute

// Shared paths resolved in TestMain: the repo checkout layout this suite
// runs against, the built scv binary, and the shared real dist.
var (
	repoRoot       string
	engineRoot     string
	perceptionRoot string
	fixturesRoot   string
	scvBin         string
	sharedDist     string
	fakeLLM        string
	tmpRoot        string
)

// fixture names this suite consumes (inventory recorded in README.md).
const (
	fixtureGolang = "golang-complex"
	fixtureRuby   = "ruby-plain"
)

func TestMain(m *testing.M) {
	var err error
	engineRoot, err = filepath.Abs("..")
	if err != nil {
		fatal("resolve engine root: %v", err)
	}
	repoRoot = filepath.Dir(engineRoot)
	perceptionRoot = filepath.Join(repoRoot, "perception")
	fixturesRoot = filepath.Join(perceptionRoot, "fixture", "repos")
	fakeLLM = filepath.Join(engineRoot, "tests", "fake-llm.sh")

	// Machine prerequisites (README.md): the suite needs the real
	// toolchain on PATH. Failing here (rather than skipping) keeps the
	// golden suite honest about what it validated.
	for _, tool := range []string{"go", "git", "python3", "uv"} {
		if _, err := exec.LookPath(tool); err != nil {
			fatal("integration suite prerequisite %q not found on PATH: %v", tool, err)
		}
	}
	for _, name := range []string{fixtureGolang, fixtureRuby} {
		info, err := os.Stat(filepath.Join(fixturesRoot, name))
		if err != nil || !info.IsDir() {
			fatal("integration fixture %s missing under %s: %v", name, fixturesRoot, err)
		}
	}
	for _, name := range []string{"readme.md", "summary.md", "architecture.md", "file_index.md"} {
		if _, err := os.Stat(filepath.Join(engineRoot, "tests", "testdata", name)); err != nil {
			fatal("fake-llm.sh fixture output %s missing: %v", name, err)
		}
	}

	tmpRoot, err = os.MkdirTemp("", "scv-integration-")
	if err != nil {
		fatal("create suite tmp dir: %v", err)
	}

	// Build the CLI under test once.
	scvBin = filepath.Join(tmpRoot, "bin", "scv")
	if err := buildBinary(engineRoot, scvBin, "./cmd/scv"); err != nil {
		fatal("build scv: %v", err)
	}

	// Prebuild the real dist binaries once. Their mtimes (now) are newer
	// than the checked-in sources, so every run — including the ones with
	// `go` stripped from PATH — treats them as fresh prebuilts and never
	// rebuilds: the suite stays fast and never writes into the checkout.
	sharedDist = filepath.Join(tmpRoot, "dist")
	if err := buildBinary(filepath.Join(perceptionRoot, "loader"),
		filepath.Join(sharedDist, "loader", "perception-loader"), "."); err != nil {
		fatal("build loader: %v", err)
	}
	if err := buildBinary(filepath.Join(perceptionRoot, "graphmcp"),
		filepath.Join(sharedDist, "graphmcp", "scv-graph-mcp"), "."); err != nil {
		fatal("build graphmcp: %v", err)
	}

	code := m.Run()
	os.RemoveAll(tmpRoot)
	os.Exit(code)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "integration suite setup: "+format+"\n", args...)
	os.Exit(1)
}

// buildBinary runs `go build -o out pkg` in dir.
func buildBinary(dir, out, pkg string) error {
	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	defer cancel()
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "go", "build", "-o", out, pkg)
	cmd.Dir = dir
	outErr, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, outErr)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

// harness carries one test's isolated scv home and work tree.
type harness struct {
	t    *testing.T
	home string // SCV_HOME: jobs, repos, analysis, config.json
	dist string // SCV_PERCEPTION_DIST (shared real dist by default)
	work string // fixture copies and scratch files
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	base := t.TempDir()
	h := &harness{
		t:    t,
		home: filepath.Join(base, "scv-home"),
		dist: sharedDist,
		work: filepath.Join(base, "work"),
	}
	for _, dir := range []string{h.home, h.work} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create harness dir: %v", err)
		}
	}
	return h
}

// env builds the child environment: the inherited environment overlaid
// with the isolated SCV roots. Tests pass extra overrides (PATH, HOME,
// SCV_ENGINE_TEST_PERCEIVE, SCV_CAPTURE_DIR).
func (h *harness) env(extra map[string]string) []string {
	base := map[string]string{}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		base[k] = v
	}
	base["SCV_HOME"] = h.home
	base["SCV_PERCEPTION_ROOT"] = perceptionRoot
	base["SCV_PERCEPTION_DIST"] = h.dist
	for k, v := range extra {
		base[k] = v
	}
	out := make([]string, 0, len(base))
	for k, v := range base {
		out = append(out, k+"="+v)
	}
	return out
}

// runResult is one scv invocation's outcome. events is populated by
// eventsOf for run invocations only.
type runResult struct {
	code   int
	stdout string
	stderr string
}

// runSCV executes the scv binary with the harness environment and extra
// overrides, asserting the process ran and capturing both streams.
func (h *harness) runSCV(extra map[string]string, args ...string) runResult {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), scvRunTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, scvBin, args...)
	cmd.Env = h.env(extra)
	cmd.Dir = h.work
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := runResult{code: exitCode(err), stdout: stdout.String(), stderr: stderr.String()}
	if res.code == -1 {
		h.t.Fatalf("scv %v did not exit cleanly (timeout or signal): %v\nstderr:\n%s", args, err, res.stderr)
	}
	return res
}

// runPipeline runs `scv run`, asserts exit 0, and returns the parsed
// NDJSON event stream.
func (h *harness) runPipeline(extra map[string]string, args ...string) []evt {
	h.t.Helper()
	res := h.runSCV(extra, args...)
	if res.code != 0 {
		h.t.Fatalf("scv %v exited %d:\n%s", args, res.code, res.stderr)
	}
	return h.eventsOf(res)
}

// runJSON runs a synchronous verb with --json and decodes its one-line
// stdout JSON.
func (h *harness) runJSON(extra map[string]string, args ...string) any {
	h.t.Helper()
	full := append([]string{"--scv-home", h.home, "--json"}, args...)
	res := h.runSCV(extra, full...)
	if res.code != 0 {
		h.t.Fatalf("scv %v exited %d:\n%s", full, res.code, res.stderr)
	}
	var v any
	if err := json.Unmarshal([]byte(res.stdout), &v); err != nil {
		h.t.Fatalf("scv %v stdout is not one JSON value: %v\nstdout:\n%s", full, err, res.stdout)
	}
	return v
}

// writeConfig installs a config.json body in the scv home.
func (h *harness) writeConfig(body string) {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(h.home, "config.json"), []byte(body), 0o644); err != nil {
		h.t.Fatalf("write config: %v", err)
	}
}

// llmConfig renders a command-mode llm section invoking fake-llm.sh.
func llmConfig(timeoutS, retry int) string {
	return fmt.Sprintf(`{"llm":{"mode":"command","command":["/bin/sh",%q],"timeout_s":%d,"retry":%d}}`,
		fakeLLM, timeoutS, retry)
}

// copyFixture copies a fixture repo into the work tree and turns it into
// a real git worktree with one commit (the fixtures ship without .git).
// The returned path is the run source; the copy is never mutated by the
// suite or the engine (asserted via assertGitClean).
func (h *harness) copyFixture(name, project string) string {
	h.t.Helper()
	dst := filepath.Join(h.work, "repo-"+project)
	copyTree(h.t, filepath.Join(fixturesRoot, name), dst)
	gitRun(h.t, dst, "init", "-b", "main")
	// Pinned identity, like the engine repo tests: no dependency on the
	// invoking user's git config.
	gitRun(h.t, dst, "config", "user.email", "test@scv.local")
	gitRun(h.t, dst, "config", "user.name", "SCV Test")
	gitRun(h.t, dst, "add", ".")
	gitRun(h.t, dst, "commit", "-q", "-m", "fixture snapshot")
	return dst
}

// assertGitClean fails when a fixture copy was mutated by a run.
func (h *harness) assertGitClean(dir string) {
	h.t.Helper()
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		h.t.Fatalf("git status in %s: %v", dir, err)
	}
	if len(bytes.TrimSpace(out)) != 0 {
		h.t.Errorf("fixture copy %s was mutated by the run:\n%s", dir, out)
	}
}

// analysisDir is <scv-home>/analysis/<project>.
func (h *harness) analysisDir(project string) string {
	return filepath.Join(h.home, "analysis", project)
}

// ---------------------------------------------------------------------------
// Event stream decoding and assertions
// ---------------------------------------------------------------------------

// evt mirrors the NDJSON wire schema of `scv run` (internal/job events).
type evt struct {
	ID     string         `json:"id"`
	Seq    int            `json:"seq"`
	Type   string         `json:"type"`
	Stage  string         `json:"stage"`
	Detail map[string]any `json:"detail"`
	Error  *struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		Retryable bool   `json:"retryable"`
		LogTail   string `json:"log_tail"`
	} `json:"error"`
}

// eventsOf parses a run's stdout as strict NDJSON events.
func (h *harness) eventsOf(res runResult) []evt {
	h.t.Helper()
	var evs []evt
	for i, line := range strings.Split(res.stdout, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e evt
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			h.t.Fatalf("stdout line %d is not an event (%v): %s", i+1, err, line)
		}
		evs = append(evs, e)
	}
	if len(evs) == 0 {
		h.t.Fatalf("run produced no events\nstdout:\n%s", res.stdout)
	}
	return evs
}

// stageSeq renders the ordered "type:stage" view of a stream.
func stageSeq(evs []evt) []string {
	out := make([]string, 0, len(evs))
	for _, e := range evs {
		out = append(out, e.Type+":"+e.Stage)
	}
	return out
}

// assertStages asserts the exact stage sequence.
func assertStages(t *testing.T, evs []evt, want []string) {
	t.Helper()
	got := stageSeq(evs)
	if !slices.Equal(got, want) {
		t.Errorf("stage sequence mismatch:\n got: %s\nwant: %s",
			strings.Join(got, " "), strings.Join(want, " "))
	}
}

// pairs renders started/finished pairs for stages.
func pairs(stages ...string) []string {
	var out []string
	for _, s := range stages {
		out = append(out, "stage_started:"+s, "stage_finished:"+s)
	}
	return out
}

// degradedItems flattens every degraded event's missing[].item.
func degradedItems(evs []evt) []string {
	var items []string
	for _, e := range evs {
		if e.Type != "degraded" {
			continue
		}
		missing, _ := e.Detail["missing"].([]any)
		for _, m := range missing {
			if mm, ok := m.(map[string]any); ok {
				if it, _ := mm["item"].(string); it != "" {
					items = append(items, it)
				}
			}
		}
	}
	return items
}

// detailOf returns the detail of the first event of type:stage.
func detailOf(evs []evt, typ, stage string) map[string]any {
	for _, e := range evs {
		if e.Type == typ && e.Stage == stage {
			return e.Detail
		}
	}
	return nil
}

// jobResultEv asserts the last event is the job_result and returns it.
func jobResultEv(t *testing.T, evs []evt) evt {
	t.Helper()
	last := evs[len(evs)-1]
	if last.Type != "job_result" {
		t.Fatalf("last event is %s:%s, want job_result", last.Type, last.Stage)
	}
	return last
}

// detailString fetches a string detail key.
func detailString(t *testing.T, detail map[string]any, key, context string) string {
	t.Helper()
	if detail == nil {
		t.Fatalf("no detail for %s", context)
	}
	v, _ := detail[key].(string)
	if v == "" {
		t.Fatalf("detail %s.%s missing or empty: %v", context, key, detail)
	}
	return v
}

// docEvents collects the per-document generating sub-events (detail.doc
// set) of one type, in order.
func docEvents(evs []evt, typ string) []evt {
	var out []evt
	for _, e := range evs {
		if e.Type == typ && e.Stage == "generating" && e.Detail != nil {
			if _, ok := e.Detail["doc"]; ok {
				out = append(out, e)
			}
		}
	}
	return out
}

// docSlugs maps doc events to their slug list.
func docSlugs(evs []evt) []string {
	var out []string
	for _, e := range evs {
		out = append(out, fmt.Sprint(e.Detail["doc"]))
	}
	return out
}

// outerGeneratingFinish returns the generating stage's outer close event
// — the stage_finished:generating without a per-doc detail.doc key (the
// per-doc sub-events carry the same type:stage pair).
func outerGeneratingFinish(t *testing.T, evs []evt) evt {
	t.Helper()
	for _, e := range evs {
		if e.Type == "stage_finished" && e.Stage == "generating" && e.Detail != nil {
			if _, ok := e.Detail["doc"]; !ok {
				return e
			}
		}
	}
	t.Fatal("no outer stage_finished:generating event")
	return evt{}
}

var wantDocSlugs = []string{"readme", "summary", "architecture", "file_index"}

// wantDocFiles maps slugs to their canonical output names.
var wantDocFiles = []string{"README.md", "SUMMARY.md", "ARCHITECTURE.md", "FILE_INDEX.md"}

// ---------------------------------------------------------------------------
// Small utilities
// ---------------------------------------------------------------------------

// pathWithout filters every PATH entry that can produce any of the named
// tools (the perception suite's _path_without technique: dir-content
// checks catch symlinked brew-style installs too). Everything else stays.
func pathWithout(tools ...string) string {
	var kept []string
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		drop := false
		for _, tool := range tools {
			if isExecFile(filepath.Join(dir, tool)) {
				drop = true
				break
			}
		}
		if !drop {
			kept = append(kept, dir)
		}
	}
	return strings.Join(kept, string(filepath.ListSeparator))
}

func isExecFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}

// copyTree copies a fixture directory recursively, preserving file modes.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
	if err != nil {
		t.Fatalf("copy fixture %s: %v", src, err)
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

// gitHead returns the HEAD commit of a worktree.
func gitHead(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse in %s: %v", dir, err)
	}
	return strings.TrimSpace(string(out))
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// structureNodeCount reads the node count out of an analysis dir's
// structure.json (the analyzer graph the loader consumes).
func structureNodeCount(t *testing.T, analysisDir string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(analysisDir, "structure.json"))
	if err != nil {
		t.Fatalf("read structure.json: %v", err)
	}
	var g struct {
		Nodes []map[string]any `json:"nodes"`
	}
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatalf("parse structure.json: %v", err)
	}
	return len(g.Nodes)
}

// mustExist / mustNotExist assert artifact presence.
func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected artifact %s: %v", path, err)
	}
}

func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Errorf("unexpected artifact %s", path)
	}
}

// copyFile copies one file, refreshing its mtime (the fresh-prebuilt
// technique: a now-dated binary is never considered stale).
func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("create %s dir: %v", dst, err)
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}

// waitNoOrphans polls pgrep until no process whose command line contains
// marker remains — the cancel case's no-orphans guarantee.
func waitNoOrphans(t *testing.T, marker string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		err := exec.Command("pgrep", "-f", marker).Run()
		if err == nil {
			if time.Now().After(deadline) {
				out, _ := exec.Command("pgrep", "-fl", marker).CombinedOutput()
				t.Fatalf("orphaned processes still running for %s:\n%s", marker, out)
			}
			time.Sleep(300 * time.Millisecond)
			continue
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 {
			return // pgrep exit 1: no matches — clean
		}
		t.Fatalf("pgrep %s: %v", marker, err)
	}
}

// ---------------------------------------------------------------------------
// Subtests (plan §14): one per §7 path
// ---------------------------------------------------------------------------

// TestIntegrationFullRun drives the complete happy path over the
// golang-complex fixture: the full frozen stage sequence, structure.json
// AND structure.vg produced, the skip metadata recorded, the auto-linked
// repos entry, and `scv query cypher` agreeing with structure.json.
func TestIntegrationFullRun(t *testing.T) {
	h := newHarness(t)
	src := h.copyFixture(fixtureGolang, "gocomplex")
	head := gitHead(t, src)

	evs := h.runPipeline(nil, "run", src, "--project", "gocomplex", "--scv-home", h.home)

	want := append([]string{"stage_started:checking", "degraded:checking", "stage_finished:checking"},
		pairs("preparing", "detecting", "analyzing", "grading", "query_tier", "finalizing")...)
	want = append(want, "job_result:done")
	assertStages(t, evs, want)

	// Structure-only config: the one degraded item is docgen-unconfigured.
	if items := degradedItems(evs); !slices.Equal(items, []string{"docgen-unconfigured"}) {
		t.Errorf("degraded items = %v, want [docgen-unconfigured]", items)
	}

	// Preparing linked the local git worktree; HEAD is real.
	prep := detailOf(evs, "stage_finished", "preparing")
	if got := detailString(t, prep, "source_kind", "preparing"); got != "local_git" {
		t.Errorf("source_kind = %q, want local_git", got)
	}
	if got := detailString(t, prep, "head_commit", "preparing"); got != head {
		t.Errorf("head_commit = %q, want %q", got, head)
	}
	if got := detailString(t, prep, "repo_path", "preparing"); got != filepath.Join(h.home, "repos", "gocomplex") {
		t.Errorf("repo_path = %q", got)
	}

	// Tier details: detect verdict, analyzer rc, loader artifact.
	if got := detailString(t, detailOf(evs, "stage_finished", "detecting"), "perception_status", "detecting"); got != "structure" {
		t.Errorf("detecting perception_status = %q, want structure", got)
	}
	analyzing := detailOf(evs, "stage_finished", "analyzing")
	if got := detailString(t, analyzing, "analyzer_rc", "analyzing"); got != "0" {
		t.Errorf("analyzing analyzer_rc = %q, want 0", got)
	}
	detailString(t, analyzing, "uv", "analyzing") // uv probe recorded
	grading := detailOf(evs, "stage_finished", "grading")
	if ok, _ := grading["structure_vg"].(bool); !ok {
		t.Errorf("grading detail structure_vg = %v, want true", grading["structure_vg"])
	}

	// Artifacts and skip metadata.
	dir := h.analysisDir("gocomplex")
	mustExist(t, filepath.Join(dir, "structure.json"))
	mustExist(t, filepath.Join(dir, "structure.vg"))
	meta, err := os.ReadFile(filepath.Join(dir, ".scv_metadata.json"))
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	if !strings.Contains(string(meta), `"last_analyzed_commit": "`+head+`"`) {
		t.Errorf("metadata last_analyzed_commit missing HEAD %s:\n%s", head, meta)
	}

	// Query tier: the golden cross-check — the store loads the run's
	// structure.vg and its node total matches the analyzer graph.
	result, _ := h.runJSON(nil, "query", "cypher", "gocomplex", "MATCH (n) RETURN n").(map[string]any)
	if result == nil {
		t.Fatalf("query cypher output not an object")
	}
	total, _ := result["total"].(float64)
	if want := float64(structureNodeCount(t, dir)); total != want {
		t.Errorf("query cypher total = %v, want structure.json nodes %v", total, want)
	}

	// The run auto-linked the fixture; the repos entry exists.
	entries, _ := h.runJSON(nil, "repo", "list").([]any)
	found := false
	for _, e := range entries {
		m, _ := e.(map[string]any)
		if m["name"] == "gocomplex" && m["type"] == "linked" {
			found = true
		}
	}
	if !found {
		t.Errorf("repos entry gocomplex (linked) missing from repo list: %v", entries)
	}

	h.assertGitClean(src)
}

// TestIntegrationNotFramework runs the ruby-plain fixture: ruby is not
// among the six shipped structure languages, so detect returns nothing,
// the run reduces to detecting→finalizing, and no structure artifacts
// appear — a clean not-framework done, exit 0.
func TestIntegrationNotFramework(t *testing.T) {
	h := newHarness(t)
	src := h.copyFixture(fixtureRuby, "rubyplain")

	evs := h.runPipeline(nil, "run", src, "--project", "rubyplain", "--scv-home", h.home)

	want := append([]string{"stage_started:checking", "degraded:checking", "stage_finished:checking"},
		pairs("preparing", "detecting", "finalizing")...)
	want = append(want, "job_result:done")
	assertStages(t, evs, want)

	detect := detailOf(evs, "stage_finished", "detecting")
	if got := detailString(t, detect, "perception_status", "detecting"); got != "not-framework" {
		t.Errorf("detecting perception_status = %q, want not-framework", got)
	}
	done := jobResultEv(t, evs)
	// perceive reports not-framework in the status field with a null
	// reason (perceive.py: emit["status"] = "not-framework").
	if got := detailString(t, done.Detail, "perception_status", "job_result"); got != "not-framework" {
		t.Errorf("job_result perception_status = %q, want not-framework", got)
	}
	if reason, ok := done.Detail["perception_reason"]; ok && reason != nil {
		t.Errorf("job_result perception_reason = %v, want none", reason)
	}

	dir := h.analysisDir("rubyplain")
	mustNotExist(t, filepath.Join(dir, "structure.json"))
	mustNotExist(t, filepath.Join(dir, "structure.vg"))

	h.assertGitClean(src)
}

// TestIntegrationGoMissingReduced strips go from PATH and points the
// engine at an empty dist: the checking gate reports the upfront degraded
// go item, the query-tier stages drop from the sequence, the analyzer
// still produces structure.json, but no structure.vg.
func TestIntegrationGoMissingReduced(t *testing.T) {
	h := newHarness(t)
	emptyDist := filepath.Join(h.work, "empty-dist")
	if err := os.MkdirAll(emptyDist, 0o755); err != nil {
		t.Fatal(err)
	}
	h.dist = emptyDist
	src := h.copyFixture(fixtureGolang, "gomissing")

	evs := h.runPipeline(map[string]string{"PATH": pathWithout("go")},
		"run", src, "--project", "gomissing", "--scv-home", h.home)

	want := append([]string{"stage_started:checking", "degraded:checking", "stage_finished:checking"},
		pairs("preparing", "detecting", "analyzing", "finalizing")...)
	want = append(want, "job_result:done")
	assertStages(t, evs, want)

	if items := degradedItems(evs); !slices.Equal(items, []string{"go", "docgen-unconfigured"}) {
		t.Errorf("degraded items = %v, want [go docgen-unconfigured]", items)
	}

	dir := h.analysisDir("gomissing")
	mustExist(t, filepath.Join(dir, "structure.json"))
	mustNotExist(t, filepath.Join(dir, "structure.vg"))

	// perceive folds go-missing (and mcp-missing, with no graphmcp
	// prebuilt either) into its reason list on the done event.
	done := jobResultEv(t, evs)
	reasons, _ := done.Detail["perception_reason"].([]any)
	found := false
	for _, r := range reasons {
		if r == "go-missing" {
			found = true
		}
	}
	if !found {
		t.Errorf("job_result perception_reason %v lacks go-missing (detail %v)", reasons, done.Detail)
	}

	h.assertGitClean(src)
}

// TestIntegrationAllowDegradedUvMissing strips uv from PATH and pins HOME
// to an isolated dir (hiding the ~/.local/bin and homebrew discovery
// fallbacks), then runs with --allow-degraded: the gate demotes uv to
// degrading, the structure stages drop entirely, and perceive's own
// uv-missing skip folds into a reduced done, exit 0.
func TestIntegrationAllowDegradedUvMissing(t *testing.T) {
	h := newHarness(t)
	isoHome := filepath.Join(h.work, "isolated-home")
	if err := os.MkdirAll(isoHome, 0o755); err != nil {
		t.Fatal(err)
	}
	src := h.copyFixture(fixtureGolang, "uvmissing")

	res := h.runSCV(map[string]string{"PATH": pathWithout("uv"), "HOME": isoHome},
		"run", src, "--project", "uvmissing", "--scv-home", h.home, "--allow-degraded")
	if res.code != 0 {
		t.Fatalf("allow-degraded run exited %d:\n%s", res.code, res.stderr)
	}
	evs := h.eventsOf(res)

	want := append([]string{"stage_started:checking", "degraded:checking", "stage_finished:checking"},
		pairs("preparing", "detecting", "finalizing")...)
	want = append(want, "job_result:done")
	assertStages(t, evs, want)

	// The gate reports docgen-unconfigured first; --allow-degraded then
	// appends the demoted uv item.
	if items := degradedItems(evs); !slices.Equal(items, []string{"docgen-unconfigured", "uv"}) {
		t.Errorf("degraded items = %v, want [docgen-unconfigured uv]", items)
	}

	done := jobResultEv(t, evs)
	if got := detailString(t, done.Detail, "perception_status", "job_result"); got != "skipped" {
		t.Errorf("job_result perception_status = %q, want skipped", got)
	}
	if got := detailString(t, done.Detail, "perception_reason", "job_result"); got != "uv-missing" {
		t.Errorf("job_result perception_reason = %q, want uv-missing", got)
	}
	if !strings.Contains(res.stderr, "skipped: uv-missing") {
		t.Errorf("stderr lacks the skipped diagnostic; stderr:\n%s", res.stderr)
	}

	dir := h.analysisDir("uvmissing")
	mustNotExist(t, filepath.Join(dir, "structure.json"))
	mustNotExist(t, filepath.Join(dir, "structure.vg"))

	h.assertGitClean(src)
}

// TestIntegrationDocgenFakeLLM configures the fake LLM command and runs
// with --lang en: the full chain proceeds to generating, all four
// documents are produced in the frozen order with per-doc events, and
// the fake-llm capture records the §8 environment contract.
func TestIntegrationDocgenFakeLLM(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(llmConfig(60, 0))
	capture := filepath.Join(h.work, "llm-capture")
	if err := os.MkdirAll(capture, 0o755); err != nil {
		t.Fatal(err)
	}
	src := h.copyFixture(fixtureGolang, "docs")

	evs := h.runPipeline(map[string]string{"SCV_CAPTURE_DIR": capture},
		"run", src, "--project", "docs", "--scv-home", h.home, "--lang", "en")

	// llm configured: no degraded items at all.
	if items := degradedItems(evs); len(items) != 0 {
		t.Errorf("degraded items = %v, want none", items)
	}

	// Full sequence plus the generating stage with its per-doc sub-events.
	want := append([]string{"stage_started:checking", "stage_finished:checking"},
		pairs("preparing", "detecting", "analyzing", "grading", "query_tier")...)
	want = append(want, "stage_started:generating")
	for range wantDocSlugs {
		want = append(want, "stage_started:generating", "stage_finished:generating")
	}
	want = append(want, "stage_finished:generating")
	want = append(want, pairs("finalizing")...)
	want = append(want, "job_result:done")
	assertStages(t, evs, want)

	// Per-doc events: frozen order, first attempt.
	if got := docSlugs(docEvents(evs, "stage_started")); !slices.Equal(got, wantDocSlugs) {
		t.Errorf("doc stage_started order = %v, want %v", got, wantDocSlugs)
	}
	if got := docSlugs(docEvents(evs, "stage_finished")); !slices.Equal(got, wantDocSlugs) {
		t.Errorf("doc stage_finished order = %v, want %v", got, wantDocSlugs)
	}
	for _, e := range docEvents(evs, "stage_started") {
		if attempt, _ := e.Detail["attempt"].(float64); attempt != 1 {
			t.Errorf("doc %v attempt = %v, want 1", e.Detail["doc"], e.Detail["attempt"])
		}
	}
	generating := outerGeneratingFinish(t, evs)
	if docs, _ := generating.Detail["documents"].(float64); docs != 4 {
		t.Errorf("generating documents = %v, want 4", generating.Detail["documents"])
	}

	// All four files exist with the fake-llm fixture bodies.
	dir := h.analysisDir("docs")
	for i, slug := range wantDocSlugs {
		path := filepath.Join(dir, wantDocFiles[i])
		mustExist(t, path)
		wantBody, err := os.ReadFile(filepath.Join(engineRoot, "tests", "testdata", slug+".md"))
		if err != nil {
			t.Fatal(err)
		}
		gotBody, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(gotBody, wantBody) {
			t.Errorf("document %s content mismatch:\ngot:  %q\nwant: %q", wantDocFiles[i], gotBody, wantBody)
		}
	}

	// The command-mode environment contract reached the fake LLM.
	for _, slug := range wantDocSlugs {
		mustExist(t, filepath.Join(capture, "doc."+slug))
		envDump, err := os.ReadFile(filepath.Join(capture, "env."+slug))
		if err != nil {
			t.Fatalf("read env capture for %s: %v", slug, err)
		}
		for _, needle := range []string{"SCV_DOC=" + slug, "SCV_LANG=en", "SCV_REPOSITORY=", "SCV_STRUCTURE_JSON="} {
			if !strings.Contains(string(envDump), needle) {
				t.Errorf("env capture for %s lacks %s:\n%s", slug, needle, envDump)
			}
		}
	}

	h.assertGitClean(src)
}

// slowPerceiveShim is the deterministic cancellation vehicle: a perceive
// stand-in that replays the tier log lines up to tier3 and then sleeps,
// so the run is provably mid-analyzing when the test sends SIGTERM.
const slowPerceiveShim = `#!/usr/bin/env python3
# Slow perceive shim for the integration cancel case (SCV_ENGINE_TEST_PERCEIVE).
# Contract: --version prints the handshake JSON; a run writes the tier
# lines up to "tier3 start" and then sleeps far past any test timeout.
import json, os, sys, time

if "--version" in sys.argv:
    print(json.dumps({"scv_version": "slow-shim", "contract": "1.0"}))
    raise SystemExit(0)

analysis_dir = sys.argv[2]
log_path = os.path.join(analysis_dir, "perception.log")

def emit(msg):
    with open(log_path, "a") as fh:
        print(msg, file=fh)

emit("tier1 start")
emit("tier1 detect: structure, source=local, extensions=[], languages=['go']")
emit("tier2 start")
emit("tier2 uv probe: /shimmed/uv")
emit("tier3 start")
time.sleep(600)
print(json.dumps({"status": "ok"}))
`

// TestIntegrationCancelMidAnalyzing starts a run on the slow shim,
// SIGTERMs the engine once the analyzing stage is observably open, and
// asserts the cancelled terminal state (exit 10) with no orphaned shim.
func TestIntegrationCancelMidAnalyzing(t *testing.T) {
	h := newHarness(t)
	shim := filepath.Join(h.work, "slow-perceive.py")
	if err := os.WriteFile(shim, []byte(slowPerceiveShim), 0o755); err != nil {
		t.Fatal(err)
	}
	src := h.copyFixture(fixtureGolang, "cancelfx")

	cmd := exec.Command(scvBin, "run", src, "--project", "cancelfx", "--scv-home", h.home)
	cmd.Env = h.env(map[string]string{"SCV_ENGINE_TEST_PERCEIVE": shim})
	cmd.Dir = h.work
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()

	var got []string
	analyzingSeen := false
	waitStage:
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				break waitStage
			}
			got = append(got, line)
			if strings.Contains(line, `"stage_started"`) && strings.Contains(line, `"analyzing"`) {
				analyzingSeen = true
				break waitStage
			}
		case <-time.After(90 * time.Second):
			cmd.Process.Kill()
			t.Fatalf("timed out waiting for the analyzing stage; events:\n%s", strings.Join(got, "\n"))
		}
	}
	if !analyzingSeen {
		cmd.Process.Kill()
		t.Fatalf("stream ended before the analyzing stage; events:\n%s", strings.Join(got, "\n"))
	}

	// Cancel: an external SIGTERM (what jobs cancel / Ctrl-C deliver).
	if err := syscall.Kill(cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	drainDone := false
	for !drainDone {
		select {
		case line, ok := <-lines:
			if !ok {
				drainDone = true
				break
			}
			got = append(got, line)
		case <-time.After(90 * time.Second):
			cmd.Process.Kill()
			t.Fatalf("scv did not exit after SIGTERM; events:\n%s", strings.Join(got, "\n"))
		}
	}
	waitErr := cmd.Wait()
	if code := exitCode(waitErr); code != 10 {
		t.Fatalf("exit code after SIGTERM = %d (err %v), want 10 (CANCELLED)\nstderr:\n%s\nevents:\n%s",
			code, waitErr, stderr.String(), strings.Join(got, "\n"))
	}

	var evs []evt
	for _, line := range got {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e evt
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("unparseable event line %q: %v", line, err)
		}
		evs = append(evs, e)
	}
	last := jobResultEv(t, evs)
	if last.Stage != "cancelled" {
		t.Errorf("job_result stage = %q, want cancelled", last.Stage)
	}
	if last.Error == nil || last.Error.Code != "CANCELLED" {
		t.Errorf("job_result error = %+v, want code CANCELLED", last.Error)
	}
	// The open analyzing stage failed with CANCELLED before the terminal
	// event.
	var failedAnalyzing bool
	for _, e := range evs {
		if e.Type == "stage_failed" && e.Stage == "analyzing" && e.Error != nil && e.Error.Code == "CANCELLED" {
			failedAnalyzing = true
		}
	}
	if !failedAnalyzing {
		t.Errorf("no stage_failed analyzing CANCELLED event; stream:\n%s", strings.Join(stageSeq(evs), " "))
	}

	// No perceive subtree survived the engine: the shim is gone.
	waitNoOrphans(t, shim)

	dir := h.analysisDir("cancelfx")
	mustNotExist(t, filepath.Join(dir, "structure.json"))
	mustNotExist(t, filepath.Join(dir, "structure.vg"))

	h.assertGitClean(src)
}

// TestIntegrationSkipSemantics pins the --skip-unchanged behavior over
// one analysis dir: a structure-only run never traps a later
// docgen-configured run (the four documents are missing, so it
// proceeds), and only a fully-artifacted unchanged commit skips.
func TestIntegrationSkipSemantics(t *testing.T) {
	h := newHarness(t)
	src := h.copyFixture(fixtureGolang, "skipfx")
	head := gitHead(t, src)

	t.Run("structure-only-run", func(t *testing.T) {
		evs := h.runPipeline(nil, "run", src, "--project", "skipfx", "--scv-home", h.home)
		if last := jobResultEv(t, evs); last.Stage != "done" {
			t.Errorf("first run stage = %q, want done", last.Stage)
		}
		mustExist(t, filepath.Join(h.analysisDir("skipfx"), "structure.json"))
	})

	t.Run("docgen-rerun-proceeds", func(t *testing.T) {
		h.writeConfig(llmConfig(60, 0))
		evs := h.runPipeline(nil, "run", src, "--project", "skipfx", "--scv-home", h.home, "--skip-unchanged")
		last := jobResultEv(t, evs)
		if last.Stage != "done" {
			t.Fatalf("re-run stage = %q, want done (must not skip); detail %v", last.Stage, last.Detail)
		}
		if got := docSlugs(docEvents(evs, "stage_finished")); !slices.Equal(got, wantDocSlugs) {
			t.Errorf("re-run did not generate docs: %v", got)
		}
		dir := h.analysisDir("skipfx")
		for _, name := range wantDocFiles {
			mustExist(t, filepath.Join(dir, name))
		}
	})

	t.Run("unchanged-commit-skips", func(t *testing.T) {
		evs := h.runPipeline(nil, "run", src, "--project", "skipfx", "--scv-home", h.home, "--skip-unchanged")
		want := append([]string{"stage_started:checking", "stage_finished:checking"},
			pairs("preparing")...)
		want = append(want, "job_result:skipped")
		assertStages(t, evs, want)
		last := jobResultEv(t, evs)
		if got := detailString(t, last.Detail, "reason", "job_result"); got != "commit-unchanged" {
			t.Errorf("skip reason = %q, want commit-unchanged (head %s)", got, head)
		}
	})

	h.assertGitClean(src)
}

// TestIntegrationLinkedRepo covers the explicit repos-entry path:
// `scv repo link` first, then a run over the same worktree adopts the
// existing entry (never re-creating it) and analyzes through the
// symlink, leaving the target untouched.
func TestIntegrationLinkedRepo(t *testing.T) {
	h := newHarness(t)
	src := h.copyFixture(fixtureGolang, "linkedfx")
	head := gitHead(t, src)

	entry, _ := h.runJSON(nil, "repo", "link", src, "--name", "linkedfx").(map[string]any)
	if entry == nil {
		t.Fatalf("repo link output not an object")
	}
	if entry["type"] != "linked" || entry["head_commit"] != head {
		t.Errorf("repo link entry = %v, want linked @ %s", entry, head)
	}

	evs := h.runPipeline(nil, "run", src, "--project", "linkedfx", "--scv-home", h.home)

	want := append([]string{"stage_started:checking", "degraded:checking", "stage_finished:checking"},
		pairs("preparing", "detecting", "analyzing", "grading", "query_tier", "finalizing")...)
	want = append(want, "job_result:done")
	assertStages(t, evs, want)

	prep := detailOf(evs, "stage_finished", "preparing")
	if got := detailString(t, prep, "source_kind", "preparing"); got != "local_git" {
		t.Errorf("source_kind = %q, want local_git", got)
	}
	if got := detailString(t, prep, "repo_path", "preparing"); got != filepath.Join(h.home, "repos", "linkedfx") {
		t.Errorf("repo_path = %q, want the linked entry", got)
	}
	if got := detailString(t, prep, "head_commit", "preparing"); got != head {
		t.Errorf("head_commit = %q, want %q", got, head)
	}

	// The entry is one symlink to the fixture copy, and the run produced
	// the full artifact set through it.
	linkPath := filepath.Join(h.home, "repos", "linkedfx")
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("repos entry is not a symlink: %v", err)
	}
	if target != src {
		t.Errorf("symlink target = %q, want %q", target, src)
	}
	mustExist(t, filepath.Join(h.analysisDir("linkedfx"), "structure.vg"))

	h.assertGitClean(src)
}

// TestIntegrationPrebuiltDistNoGo validates the binary-OR-toolchain gate
// end to end: go stripped from PATH, but the dist carries fresh prebuilt
// loader and graphmcp binaries (real ones, freshly copied — the
// perception suite's fake-loader fresh-mtime technique, with the real
// binary so the produced structure.vg is genuinely queryable). No go
// degradation, the full chain runs to .vg, and no build is attempted.
func TestIntegrationPrebuiltDistNoGo(t *testing.T) {
	h := newHarness(t)
	prebuiltDist := filepath.Join(h.work, "prebuilt-dist")
	copyFile(t, filepath.Join(sharedDist, "loader", "perception-loader"),
		filepath.Join(prebuiltDist, "loader", "perception-loader"))
	copyFile(t, filepath.Join(sharedDist, "graphmcp", "scv-graph-mcp"),
		filepath.Join(prebuiltDist, "graphmcp", "scv-graph-mcp"))
	h.dist = prebuiltDist
	src := h.copyFixture(fixtureGolang, "prebuiltnogo")

	evs := h.runPipeline(map[string]string{"PATH": pathWithout("go")},
		"run", src, "--project", "prebuiltnogo", "--scv-home", h.home)

	want := append([]string{"stage_started:checking", "degraded:checking", "stage_finished:checking"},
		pairs("preparing", "detecting", "analyzing", "grading", "query_tier", "finalizing")...)
	want = append(want, "job_result:done")
	assertStages(t, evs, want)

	// The gate saw the prebuilt loader: no go item, only the
	// structure-only docgen note.
	if items := degradedItems(evs); !slices.Equal(items, []string{"docgen-unconfigured"}) {
		t.Errorf("degraded items = %v, want [docgen-unconfigured] only", items)
	}

	grading := detailOf(evs, "stage_finished", "grading")
	if ok, _ := grading["structure_vg"].(bool); !ok {
		t.Errorf("grading detail structure_vg = %v, want true", grading["structure_vg"])
	}
	dir := h.analysisDir("prebuiltnogo")
	mustExist(t, filepath.Join(dir, "structure.vg"))

	// The prebuilt ran as-is: no build (which would have failed — there
	// is no toolchain) and therefore no build-failure line in the log.
	logBytes, err := os.ReadFile(filepath.Join(dir, "perception.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(logBytes), "build failed") {
		t.Errorf("perception.log records a build attempt without a toolchain:\n%s", logBytes)
	}

	// The vg is a real, queryable snapshot.
	result, _ := h.runJSON(nil, "query", "cypher", "prebuiltnogo", "MATCH (n) RETURN n").(map[string]any)
	if result == nil {
		t.Fatalf("query cypher output not an object")
	}
	total, _ := result["total"].(float64)
	if want := float64(structureNodeCount(t, dir)); total != want {
		t.Errorf("query cypher total = %v, want structure.json nodes %v", total, want)
	}

	h.assertGitClean(src)
}
