package docgen

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	lcmessages "github.com/projanvil/langchain-golang/core/messages"
	"github.com/projanvil/scv/engine/internal/job"
)

// fakeInvoker is an in-test AgentInvoker: respond shapes every call's
// outcome, calls records every delivered message batch in order.
type fakeInvoker struct {
	calls   [][]lcmessages.Message
	respond func(call int) ([]lcmessages.Message, error)
}

func (f *fakeInvoker) Invoke(ctx context.Context, msgs []lcmessages.Message) ([]lcmessages.Message, error) {
	f.calls = append(f.calls, msgs)
	return f.respond(len(f.calls))
}

// agentHarness builds the standard agent-mode setup: test assets,
// English, a responding fake invoker. mutate adjusts the Generator.
func agentHarness(t *testing.T, mutate func(*Generator)) (*harness, *fakeInvoker) {
	t.Helper()
	useTestAssets(t)
	invoker := &fakeInvoker{respond: func(call int) ([]lcmessages.Message, error) {
		return []lcmessages.Message{
			lcmessages.Human("task"),
			lcmessages.AI(fmt.Sprintf("doc body %d", call)),
		}, nil
	}}
	h := &harness{
		repoPath:    t.TempDir(),
		analysisDir: filepath.Join(t.TempDir(), "analysis"),
		summary: map[string]any{
			"status":            "ok",
			"structure_summary": "42 files, 43 types, 100 methods, 777 call edges",
		},
	}
	h.structureJSON = filepath.Join(h.analysisDir, "structure.json")
	if err := os.MkdirAll(h.analysisDir, 0o755); err != nil {
		t.Fatalf("create analysis dir: %v", err)
	}
	if err := os.WriteFile(h.structureJSON, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write structure.json: %v", err)
	}
	h.gen = &Generator{Invoker: invoker, Lang: "en"}
	if mutate != nil {
		mutate(h.gen)
	}
	return h, invoker
}

// TestAgentGenerateHappyPath checks the full agent-mode contract: four
// invocations in the frozen order, each payload headed with the task
// header and carrying the analyzer body, structure summary, and this
// document's template; the last AI message becomes the file content;
// every attempt emits its stage events.
func TestAgentGenerateHappyPath(t *testing.T) {
	h, invoker := agentHarness(t, nil)
	docs, err := h.run(t)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(docs) != 4 || len(invoker.calls) != 4 {
		t.Fatalf("got %d docs, %d invocations; want 4, 4", len(docs), len(invoker.calls))
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
	for i, slug := range docSlugs {
		body, err := os.ReadFile(filepath.Join(h.analysisDir, canonicalNames[slug]))
		if err != nil {
			t.Fatalf("read %s: %v", slug, err)
		}
		if want := fmt.Sprintf("doc body %d", i+1); string(body) != want {
			t.Fatalf("%s content = %q, want %q", slug, body, want)
		}
	}
}

// TestAgentGeneratePayloadShape inspects one delivered message: exactly
// one Human message, headed with the in-process contract block, then
// the analyzer body, the structure summary, and the document template.
func TestAgentGeneratePayloadShape(t *testing.T) {
	h, invoker := agentHarness(t, nil)
	if _, err := h.run(t); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	msgs := invoker.calls[0]
	if len(msgs) != 1 || msgs[0].Role != lcmessages.RoleHuman {
		t.Fatalf("payload = %d messages with roles %v; want one human message", len(msgs), roles(msgs))
	}
	text := lcmessages.Text(msgs[0])
	for _, want := range []string{
		"# SCV document task\n",
		"doc: readme\n",
		"lang: en\n",
		"repository: " + h.repoPath + "\n",
		"analysis_dir: " + h.analysisDir + "\n",
		"structure_json: " + h.structureJSON + "\n",
		"# Project Analyzer (en test stub)",
		"## Structure summary",
		"42 files, 43 types, 100 methods, 777 call edges",
		"<!-- en template: README -->",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("payload missing %q; got:\n%s", want, text)
		}
	}
}

// TestAgentGenerateRetryRecovers fails the first attempt and succeeds
// on the retry: the document is written and both attempts report events.
func TestAgentGenerateRetryRecovers(t *testing.T) {
	h, invoker := agentHarness(t, func(g *Generator) { g.Retry = 1 })
	invoker.respond = func(call int) ([]lcmessages.Message, error) {
		if call == 1 {
			return nil, errors.New("model unavailable")
		}
		return []lcmessages.Message{lcmessages.AI("recovered body")}, nil
	}
	docs, err := h.run(t)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(docs) != 4 || len(invoker.calls) != 5 {
		t.Fatalf("got %d docs, %d invocations; want 4, 5", len(docs), len(invoker.calls))
	}
	assertEvents(t, h.events[:3], []wantEvent{
		{job.EventStageStarted, "readme", 1},
		{job.EventStageStarted, "readme", 2},
		{job.EventStageFinished, "readme", 2},
	})
}

// TestAgentGenerateFailureExhaustsRetries verifies the typed *Failure
// after every attempt fails: DOCGEN_FAILED with the doc, the attempt
// count, and the agent error text.
func TestAgentGenerateFailureExhaustsRetries(t *testing.T) {
	h, invoker := agentHarness(t, func(g *Generator) { g.Retry = 1 })
	invoker.respond = func(int) ([]lcmessages.Message, error) {
		return nil, errors.New("boom")
	}
	_, err := h.run(t)
	var failure *Failure
	if !errors.As(err, &failure) {
		t.Fatalf("error = %v, want *Failure", err)
	}
	if failure.Code != CodeDocGenFailed || failure.Doc != "readme" || failure.Attempt != 2 {
		t.Fatalf("failure = %+v, want DOCGEN_FAILED readme attempt 2", failure)
	}
	if !strings.Contains(failure.Message, "boom") {
		t.Fatalf("failure message %q missing agent error", failure.Message)
	}
	assertEvents(t, h.events, []wantEvent{
		{job.EventStageStarted, "readme", 1},
		{job.EventStageStarted, "readme", 2},
	})
}

// TestAgentGenerateTimeout reports STAGE_TIMEOUT when the agent exceeds
// the per-attempt budget.
func TestAgentGenerateTimeout(t *testing.T) {
	h, invoker := agentHarness(t, func(g *Generator) { g.Timeout = 20 * time.Millisecond })
	invoker.respond = func(int) ([]lcmessages.Message, error) {
		<-time.After(2 * time.Second)
		return []lcmessages.Message{lcmessages.AI("late")}, nil
	}
	_, err := h.run(t)
	var failure *Failure
	if !errors.As(err, &failure) {
		t.Fatalf("error = %v, want *Failure", err)
	}
	if failure.Code != CodeStageTimeout || failure.Doc != "readme" {
		t.Fatalf("failure = %+v, want STAGE_TIMEOUT readme", failure)
	}
}

// TestAgentGenerateRunCancelled surfaces the run's own cancellation as
// a plain error, not a retryable docgen failure.
func TestAgentGenerateRunCancelled(t *testing.T) {
	h, invoker := agentHarness(t, nil)
	invoker.respond = func(int) ([]lcmessages.Message, error) {
		return nil, context.Canceled
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := h.gen.Generate(ctx, h.repoPath, h.analysisDir, h.structureJSON, h.summary, nil)
	if err == nil || errors.As(err, new(*Failure)) {
		t.Fatalf("error = %v, want a plain cancellation error", err)
	}
	if !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("error = %v, want context cancellation", err)
	}
}

// TestAgentGenerateEmptyAndNoAI covers the two shape failures of a
// successful Invoke: an empty assistant message and a reply with no
// assistant message at all.
func TestAgentGenerateEmptyAndNoAI(t *testing.T) {
	for _, tc := range []struct {
		name    string
		respond func(int) ([]lcmessages.Message, error)
		want    string
	}{
		{"empty", func(int) ([]lcmessages.Message, error) {
			return []lcmessages.Message{lcmessages.AI("  \n")}, nil
		}, "empty output"},
		{"no-ai", func(int) ([]lcmessages.Message, error) {
			return []lcmessages.Message{lcmessages.Human("echo")}, nil
		}, "no assistant message"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, invoker := agentHarness(t, nil)
			invoker.respond = tc.respond
			_, err := h.run(t)
			var failure *Failure
			if !errors.As(err, &failure) || !strings.Contains(failure.Message, tc.want) {
				t.Fatalf("error = %v, want failure containing %q", err, tc.want)
			}
		})
	}
}

// TestAgentGenerateExtractLastAI pins the extraction rule: the LAST
// assistant message wins, not the first (agents may stream several).
func TestAgentGenerateExtractLastAI(t *testing.T) {
	h, invoker := agentHarness(t, nil)
	invoker.respond = func(int) ([]lcmessages.Message, error) {
		return []lcmessages.Message{
			lcmessages.AI("draft"),
			lcmessages.Tool("t1", "tool output"),
			lcmessages.AI("final body"),
		}, nil
	}
	if _, err := h.run(t); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(h.analysisDir, canonicalNames["readme"]))
	if err != nil {
		t.Fatalf("read readme: %v", err)
	}
	if string(body) != "final body" {
		t.Fatalf("readme = %q, want the last AI message %q", body, "final body")
	}
}

// TestAgentGenerateCommandIgnored pins the precedence rule: with an
// invoker set, a stale Command field never spawns a subprocess.
func TestAgentGenerateCommandIgnored(t *testing.T) {
	h, _ := agentHarness(t, func(g *Generator) { g.Command = []string{"/definitely/not/a/real/llm"} })
	if _, err := h.run(t); err != nil {
		t.Fatalf("Generate: %v", err)
	}
}

// roles renders message roles for failure messages.
func roles(msgs []lcmessages.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = string(m.Role)
	}
	return out
}
