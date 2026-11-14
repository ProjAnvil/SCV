package docgen

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/projanvil/scv/engine/internal/job"
)

// Generator's fit with the run pipeline's doc-generation seam
// (run.DocGenerator, Task 6) is proven end-to-end by the run package's
// tests, which pass a *Generator straight into the pipeline; run imports
// this package, so the assertion cannot live here (import cycle in test).

// useTestAssets points asset resolution at the committed testdata mirror
// of the packed docgen/ layout (absolute, so child-process env values are
// cwd-independent).
func useTestAssets(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("testdata", "docgen"))
	if err != nil {
		t.Fatalf("resolve testdata assets: %v", err)
	}
	t.Setenv("SCV_DOCGEN_ASSETS", root)
	return root
}

// repoRoot walks up from the package dir to the repository checkout root:
// the first ancestor containing perception/.
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

// fakeLLM returns the absolute path of the fake command fixture and
// asserts it is executable, the way production commands are invoked.
func fakeLLM(t *testing.T) string {
	t.Helper()
	p := filepath.Join(repoRoot(t), "engine", "tests", "fake-llm.sh")
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("fake-llm.sh fixture: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("fake-llm.sh is not executable: %v", info.Mode())
	}
	return p
}

// harness carries one Generate run's inputs and captured outputs.
type harness struct {
	gen           *Generator
	repoPath      string
	analysisDir   string
	structureJSON string
	summary       map[string]any
	events        []job.Event
}

// newHarness builds the standard setup: fake-llm in the given mode,
// testdata assets, English docs. mutate adjusts the Generator. Tests set
// SCV_CAPTURE_DIR and asset overrides AFTER this call, never before.
func newHarness(t *testing.T, mode string, mutate func(*Generator)) *harness {
	t.Helper()
	useTestAssets(t)
	t.Setenv("SCV_FAKE_MODE", mode)
	h := &harness{
		repoPath:    t.TempDir(),
		analysisDir: filepath.Join(t.TempDir(), "analysis"),
		summary: map[string]any{
			"status":            "ok",
			"structure_summary": "42 files, 43 types, 100 methods, 777 call edges",
			"counts":            map[string]any{"components": 7},
		},
	}
	h.structureJSON = filepath.Join(h.analysisDir, "structure.json")
	if err := os.MkdirAll(h.analysisDir, 0o755); err != nil {
		t.Fatalf("create analysis dir: %v", err)
	}
	if err := os.WriteFile(h.structureJSON, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write structure.json: %v", err)
	}
	h.gen = &Generator{Command: []string{fakeLLM(t)}, Lang: "en"}
	if mutate != nil {
		mutate(h.gen)
	}
	return h
}

// run executes Generate with a collecting sink.
func (h *harness) run(t *testing.T) ([]string, error) {
	t.Helper()
	return h.gen.Generate(context.Background(), h.repoPath, h.analysisDir,
		h.structureJSON, h.summary, func(ev job.Event) { h.events = append(h.events, ev) })
}

// wantEvent describes one expected per-doc generating sub-event.
type wantEvent struct {
	typ     string
	doc     string
	attempt int
}

// assertEvents checks the exact event sequence: type, stage, doc slug,
// and attempt number on every event.
func assertEvents(t *testing.T, events []job.Event, want []wantEvent) {
	t.Helper()
	if len(events) != len(want) {
		t.Fatalf("got %d events, want %d: %+v", len(events), len(want), events)
	}
	for i, w := range want {
		ev := events[i]
		if ev.Type != w.typ {
			t.Errorf("event %d type = %q, want %q", i, ev.Type, w.typ)
		}
		if ev.Stage != string(job.StateGenerating) {
			t.Errorf("event %d stage = %q, want %q", i, ev.Stage, job.StateGenerating)
		}
		if ev.Detail["doc"] != w.doc {
			t.Errorf("event %d detail.doc = %v, want %q", i, ev.Detail["doc"], w.doc)
		}
		if ev.Detail["attempt"] != w.attempt {
			t.Errorf("event %d detail.attempt = %v, want %d", i, ev.Detail["attempt"], w.attempt)
		}
	}
}

// fixturePath returns the fake-llm output fixture for a doc slug.
func fixturePath(t *testing.T, slug string) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "engine", "tests", "testdata", slug+".md")
}

// readCapture reads one capture file written by fake-llm.sh.
func readCapture(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read capture %s: %v", name, err)
	}
	return string(data)
}

// templateNameFor returns the template file name of a doc slug (for
// asserting the SCV_TEMPLATE env value).
func templateNameFor(slug string) string {
	return strings.TrimSuffix(canonicalNames[slug], ".md") + ".template.md"
}

// assertNoDocsWritten fails when any canonical doc file exists in dir.
func assertNoDocsWritten(t *testing.T, dir string) {
	t.Helper()
	for _, name := range canonicalNames {
		if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("document %s exists in %s despite failure", name, dir)
		}
	}
}

func TestFourDocsGenerated(t *testing.T) {
	h := newHarness(t, "", nil)
	docs, err := h.run(t)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	wantNames := []string{"README.md", "SUMMARY.md", "ARCHITECTURE.md", "FILE_INDEX.md"}
	if len(docs) != len(wantNames) {
		t.Fatalf("docs = %v, want %d entries", docs, len(wantNames))
	}
	for i, name := range wantNames {
		if want := filepath.Join(h.analysisDir, name); docs[i] != want {
			t.Errorf("docs[%d] = %q, want %q", i, docs[i], want)
		}
		fixture, rerr := os.ReadFile(fixturePath(t, docSlugs[i]))
		if rerr != nil {
			t.Fatalf("read fixture: %v", rerr)
		}
		got, rerr := os.ReadFile(docs[i])
		if rerr != nil {
			t.Fatalf("read %s: %v", name, rerr)
		}
		if string(got) != string(fixture) {
			t.Errorf("%s content = %q, want fixture %q", name, got, fixture)
		}
	}
	assertEvents(t, h.events, []wantEvent{
		{job.EventStageStarted, "readme", 1},
		{job.EventStageFinished, "readme", 1},
		{job.EventStageStarted, "summary", 1},
		{job.EventStageFinished, "summary", 1},
		{job.EventStageStarted, "architecture", 1},
		{job.EventStageFinished, "architecture", 1},
		{job.EventStageStarted, "file_index", 1},
		{job.EventStageFinished, "file_index", 1},
	})
}

func TestEnvContractAndStdin(t *testing.T) {
	h := newHarness(t, "", func(g *Generator) { g.Lang = "en" })
	capture := t.TempDir()
	t.Setenv("SCV_CAPTURE_DIR", capture)
	assets := useTestAssets(t)
	if _, err := h.run(t); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	for _, slug := range docSlugs {
		envText := readCapture(t, capture, "env."+slug)
		wantTemplate := filepath.Join(assets, "en", templateNameFor(slug))
		for _, want := range []string{
			"SCV_DOC=" + slug + "\n",
			"SCV_REPOSITORY=" + h.repoPath + "\n",
			"SCV_ANALYSIS_DIR=" + h.analysisDir + "\n",
			"SCV_STRUCTURE_JSON=" + h.structureJSON + "\n",
			"SCV_TEMPLATE=" + wantTemplate + "\n",
			"SCV_LANG=en\n",
		} {
			if !strings.Contains(envText, want) {
				t.Errorf("doc %s: env missing %q", slug, want)
			}
		}
		// argv carries nothing beyond the configured command.
		if got := readCapture(t, capture, "argc."+slug); got != "0\n" {
			t.Errorf("doc %s: command received %s extra argv items, want 0", slug, strings.TrimSpace(got))
		}

		stdin := readCapture(t, capture, "stdin."+slug)
		analyzer, rerr := os.ReadFile(filepath.Join(assets, "en", "project-analyzer.md"))
		if rerr != nil {
			t.Fatal(rerr)
		}
		if !strings.Contains(stdin, string(analyzer)) {
			t.Errorf("doc %s: stdin lacks the project-analyzer body", slug)
		}
		if !strings.Contains(stdin, h.summary["structure_summary"].(string)) {
			t.Errorf("doc %s: stdin lacks the structure summary rendering", slug)
		}
		template, rerr := os.ReadFile(wantTemplate)
		if rerr != nil {
			t.Fatal(rerr)
		}
		if !strings.Contains(stdin, string(template)) {
			t.Errorf("doc %s: stdin lacks the template content", slug)
		}
	}
}

func TestFailureModeDocgenFailed(t *testing.T) {
	t.Run("nonzero exit", func(t *testing.T) {
		h := newHarness(t, "fail", nil)
		docs, err := h.run(t)
		if err == nil {
			t.Fatal("Generate succeeded, want failure")
		}
		if docs != nil {
			t.Errorf("docs = %v, want nil on failure", docs)
		}
		var failure *Failure
		if !errors.As(err, &failure) {
			t.Fatalf("error type %T, want *Failure: %v", err, err)
		}
		if failure.Code != CodeDocGenFailed {
			t.Errorf("failure code = %q, want %q", failure.Code, CodeDocGenFailed)
		}
		if failure.Doc != "readme" || failure.Attempt != 1 {
			t.Errorf("failure doc/attempt = %q/%d, want readme/1", failure.Doc, failure.Attempt)
		}
		if !strings.Contains(failure.LogTail, "simulated failure for readme") {
			t.Errorf("LogTail = %q, want the command stderr tail", failure.LogTail)
		}
		if len(failure.LogTail) > logTailLimit {
			t.Errorf("LogTail length = %d, want <= %d", len(failure.LogTail), logTailLimit)
		}
		assertNoDocsWritten(t, h.analysisDir)
		assertEvents(t, h.events, []wantEvent{{job.EventStageStarted, "readme", 1}})
	})

	t.Run("empty output is a failure", func(t *testing.T) {
		h := newHarness(t, "empty", nil)
		_, err := h.run(t)
		var failure *Failure
		if !errors.As(err, &failure) {
			t.Fatalf("error type %T, want *Failure: %v", err, err)
		}
		if failure.Code != CodeDocGenFailed {
			t.Errorf("failure code = %q, want %q", failure.Code, CodeDocGenFailed)
		}
		if !strings.Contains(failure.Message, "empty") {
			t.Errorf("failure message = %q, want the empty-output reason", failure.Message)
		}
		assertNoDocsWritten(t, h.analysisDir)
	})

	t.Run("negative retry means a single attempt", func(t *testing.T) {
		h := newHarness(t, "fail", func(g *Generator) { g.Retry = -3 })
		if _, err := h.run(t); err == nil {
			t.Fatal("Generate succeeded, want failure")
		}
		assertEvents(t, h.events, []wantEvent{{job.EventStageStarted, "readme", 1}})
	})
}

func TestTimeoutKillsAndReports(t *testing.T) {
	h := newHarness(t, "hang", func(g *Generator) { g.Timeout = 300 * time.Millisecond })
	start := time.Now()
	docs, err := h.run(t)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("Generate succeeded, want timeout failure")
	}
	var failure *Failure
	if !errors.As(err, &failure) {
		t.Fatalf("error type %T, want *Failure: %v", err, err)
	}
	if failure.Code != CodeStageTimeout {
		t.Errorf("failure code = %q, want %q", failure.Code, CodeStageTimeout)
	}
	if failure.Doc != "readme" {
		t.Errorf("failure doc = %q, want readme", failure.Doc)
	}
	if !strings.Contains(failure.Message, "timed out") {
		t.Errorf("failure message = %q, want the timeout reason", failure.Message)
	}
	if elapsed > 10*time.Second {
		t.Errorf("Generate took %s; the process-group kill did not land", elapsed)
	}
	if docs != nil {
		t.Errorf("docs = %v, want nil on failure", docs)
	}
	assertNoDocsWritten(t, h.analysisDir)
	assertEvents(t, h.events, []wantEvent{{job.EventStageStarted, "readme", 1}})
}

func TestRetryAttemptsEmitted(t *testing.T) {
	h := newHarness(t, "fail-once", func(g *Generator) { g.Retry = 1 })
	docs, err := h.run(t)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(docs) != 4 {
		t.Fatalf("docs = %v, want 4 documents", docs)
	}
	// The first call fails: readme burns attempt 1, succeeds on attempt 2;
	// every later doc succeeds on its first attempt.
	assertEvents(t, h.events, []wantEvent{
		{job.EventStageStarted, "readme", 1},
		{job.EventStageStarted, "readme", 2},
		{job.EventStageFinished, "readme", 2},
		{job.EventStageStarted, "summary", 1},
		{job.EventStageFinished, "summary", 1},
		{job.EventStageStarted, "architecture", 1},
		{job.EventStageFinished, "architecture", 1},
		{job.EventStageStarted, "file_index", 1},
		{job.EventStageFinished, "file_index", 1},
	})
}

func TestAssetsLangMapping(t *testing.T) {
	t.Run("en resolves the en dir", func(t *testing.T) {
		h := newHarness(t, "", func(g *Generator) { g.Lang = "en" })
		capture := t.TempDir()
		t.Setenv("SCV_CAPTURE_DIR", capture)
		assets := useTestAssets(t)
		if _, err := h.run(t); err != nil {
			t.Fatalf("Generate: %v", err)
		}
		envText := readCapture(t, capture, "env.readme")
		want := "SCV_TEMPLATE=" + filepath.Join(assets, "en", "README.template.md") + "\n"
		if !strings.Contains(envText, want) {
			t.Errorf("env missing %q", want)
		}
		if !strings.Contains(readCapture(t, capture, "stdin.readme"), "en template: README") {
			t.Errorf("stdin lacks the en template content")
		}
	})

	t.Run("zh maps to zh-cn", func(t *testing.T) {
		h := newHarness(t, "", func(g *Generator) { g.Lang = "zh" })
		capture := t.TempDir()
		t.Setenv("SCV_CAPTURE_DIR", capture)
		assets := useTestAssets(t)
		if _, err := h.run(t); err != nil {
			t.Fatalf("Generate: %v", err)
		}
		envText := readCapture(t, capture, "env.readme")
		want := "SCV_TEMPLATE=" + filepath.Join(assets, "zh-cn", "README.template.md") + "\n"
		if !strings.Contains(envText, want) {
			t.Errorf("env missing %q", want)
		}
		if !strings.Contains(readCapture(t, capture, "stdin.readme"), "zh-cn template: README") {
			t.Errorf("stdin lacks the zh-cn template content")
		}
		if !strings.Contains(envText, "SCV_LANG=zh\n") {
			t.Errorf("SCV_LANG should carry the configured lang zh, not the dir name")
		}
	})

	t.Run("repo-checkout fallback resolves the anchored checkout", func(t *testing.T) {
		// Hermetic anchor (final-review F1): synthesize a minimal repo
		// checkout in a tmp dir — perception/, agents/<lang>/, skills
		// templates — and anchor the fallback there, so the subtest never
		// depends on whether the real checkout (or a pack-generated
		// perception/docgen/ mirror, which would win as the packed layout)
		// exists on the machine running the tests.
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "perception"), 0o755); err != nil {
			t.Fatal(err)
		}
		analyzerMarker := "hermetic-analyzer: en project-analyzer body"
		analyzerPath := filepath.Join(root, "agents", "en", "project-analyzer.md")
		if err := os.MkdirAll(filepath.Dir(analyzerPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(analyzerPath, []byte(analyzerMarker+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, slug := range docSlugs {
			tmpl := filepath.Join(root, "skills", "en", "references", "templates", templateName(slug))
			if err := os.MkdirAll(filepath.Dir(tmpl), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(tmpl, []byte("hermetic template: "+slug+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}

		h := newHarness(t, "", func(g *Generator) {
			g.AssetsRoot = filepath.Join(root, "perception", "docgen") // packed layout absent by construction
		})
		capture := t.TempDir()
		t.Setenv("SCV_CAPTURE_DIR", capture)
		t.Setenv("SCV_DOCGEN_ASSETS", "") // env override off; force the fallback
		if _, err := h.run(t); err != nil {
			t.Fatalf("Generate: %v", err)
		}
		wantTemplate := filepath.Join(root, "skills", "en", "references", "templates", "README.template.md")
		envText := readCapture(t, capture, "env.readme")
		if !strings.Contains(envText, "SCV_TEMPLATE="+wantTemplate+"\n") {
			t.Errorf("SCV_TEMPLATE did not resolve to the anchored checkout template %s", wantTemplate)
		}
		if !strings.Contains(readCapture(t, capture, "stdin.readme"), analyzerMarker) {
			t.Errorf("stdin lacks the anchored agents/en/project-analyzer.md body (marker %q)", analyzerMarker)
		}
		if !strings.Contains(readCapture(t, capture, "stdin.readme"), "hermetic template: readme") {
			t.Errorf("stdin lacks the anchored template body")
		}
	})

	t.Run("env override is strict", func(t *testing.T) {
		h := newHarness(t, "", nil)
		t.Setenv("SCV_DOCGEN_ASSETS", filepath.Join(t.TempDir(), "nope"))
		_, err := h.run(t)
		if err == nil {
			t.Fatal("Generate succeeded against a bogus SCV_DOCGEN_ASSETS, want an assets error")
		}
		if !strings.Contains(err.Error(), "docgen") || !strings.Contains(err.Error(), "assets") {
			t.Errorf("error = %v, want an assets-resolution error", err)
		}
		if len(h.events) != 0 {
			t.Errorf("events = %d, want none before assets resolve", len(h.events))
		}
	})
}

func TestGeneratorRequiresCommand(t *testing.T) {
	useTestAssets(t)
	g := &Generator{Lang: "en"}
	_, err := g.Generate(context.Background(), t.TempDir(), t.TempDir(), "structure.json", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "command") {
		t.Errorf("Generate with no command: err = %v, want a command-required error", err)
	}
}

func TestFailureErrorMessage(t *testing.T) {
	f := &Failure{Code: CodeDocGenFailed, Doc: "readme", Attempt: 2, Message: "boom", LogTail: "stderr tail"}
	got := f.Error()
	for _, want := range []string{"readme", "attempt 2", "boom", "stderr tail"} {
		if !strings.Contains(got, want) {
			t.Errorf("Failure.Error() = %q, want it to contain %q", got, want)
		}
	}
}

func TestClip(t *testing.T) {
	if got := clip("short", logTailLimit); got != "short" {
		t.Errorf("clip(short) = %q, want unchanged", got)
	}
	long := strings.Repeat("x", logTailLimit+500)
	if got := clip(long, logTailLimit); len(got) != logTailLimit || got != long[500:] {
		t.Errorf("clip(long) kept %d chars, want the final %d", len(got), logTailLimit)
	}
}

func TestResolveAssetsRequiresRoot(t *testing.T) {
	t.Setenv("SCV_DOCGEN_ASSETS", "")
	g := &Generator{}
	if _, err := g.resolveAssets("en"); err == nil || !strings.Contains(err.Error(), "no assets root") {
		t.Errorf("resolveAssets with no roots: err = %v, want the no-assets-root error", err)
	}
}

func TestResolveAssetsFallbackMiss(t *testing.T) {
	t.Setenv("SCV_DOCGEN_ASSETS", "")
	g := &Generator{AssetsRoot: filepath.Join(t.TempDir(), "nope")}
	_, err := g.resolveAssets("en")
	if err == nil || !strings.Contains(err.Error(), "repo-checkout fallback") {
		t.Errorf("resolveAssets with unusable roots: err = %v, want the fallback-miss error", err)
	}
}

func TestRenderSummary(t *testing.T) {
	tests := []struct {
		name    string
		summary map[string]any
		want    string
	}{
		{"nil map", nil, "(no structure summary)"},
		{"empty map", map[string]any{}, "(no structure summary)"},
		{"structure summary preferred", map[string]any{
			"structure_summary": "7 files, 8 types",
			"counts":            map[string]any{"components": 2},
		}, "7 files, 8 types"},
		{"counts fallback", map[string]any{
			"counts": map[string]any{"components": 2},
		}, `"components": 2`},
		{"whole map fallback", map[string]any{"status": "ok"}, `"status": "ok"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renderSummary(tt.summary); !strings.Contains(got, tt.want) {
				t.Errorf("renderSummary() = %q, want it to contain %q", got, tt.want)
			}
		})
	}
}
