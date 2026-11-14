package supervisor_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	lcmessages "github.com/projanvil/langchain-golang/core/messages"
	"github.com/projanvil/scv/engine/internal/job"
	"github.com/projanvil/scv/engine/internal/run"
	"github.com/projanvil/scv/engine/supervisor"
)

// stubAgent stands in for the host's initialized langchain-golang
// agent on the AgentInvoker surface: every document task gets one
// invocation, answered with one assistant message.
type stubAgent struct {
	calls int
}

func (a *stubAgent) Invoke(ctx context.Context, msgs []lcmessages.Message) ([]lcmessages.Message, error) {
	a.calls++
	return []lcmessages.Message{lcmessages.AI(fmt.Sprintf("agent doc %d", a.calls))}, nil
}

// TestSupervisorRunWithAgentDocgen pins the agent-mode embedding path:
// AgentDocGenerator adapts a host agent into a run.DocGenerator,
// RunWithDocs overrides the config's llm wiring (here a command that
// would fail if ever executed), and the four documents come from the
// agent's assistant messages. Asset resolution is pinned to the
// committed testdata mirror like TestSupervisorRunDocgen.
func TestSupervisorRunWithAgentDocgen(t *testing.T) {
	home := newTestHome(t)
	stubPerceiveEnv(t)
	root := repoRoot(t)
	// The llm section must lose to the injected generator: its command
	// does not exist, so any command-mode execution would fail the run.
	cfgBody := `{"structure_analysis_enabled": false, "llm": {"mode": "command", "command": ["/definitely/not/a/real/llm"], "timeout_s": 60, "retry": 0}}` + "\n"
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

	agent := &stubAgent{}
	docsGen, err := sup.AgentDocGenerator(agent, "en")
	if err != nil {
		t.Fatalf("AgentDocGenerator: %v", err)
	}
	h, err := sup.RunWithDocs(ctx, run.RunRequest{Source: t.TempDir(), Lang: "en"}, docsGen)
	if err != nil {
		t.Fatalf("RunWithDocs: %v", err)
	}
	res, err := h.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}

	if agent.calls != 4 {
		t.Fatalf("agent invocations = %d, want 4 (one per document)", agent.calls)
	}
	wantDocs := []string{"README.md", "SUMMARY.md", "ARCHITECTURE.md", "FILE_INDEX.md"}
	if len(res.Docs) != len(wantDocs) {
		t.Fatalf("Wait result docs = %v, want %d entries", res.Docs, len(wantDocs))
	}
	for i, name := range wantDocs {
		data, rerr := os.ReadFile(filepath.Join(res.OutputDir, name))
		if rerr != nil {
			t.Fatalf("document %s missing: %v", name, rerr)
		}
		if want := fmt.Sprintf("agent doc %d", i+1); string(data) != want {
			t.Errorf("document %s = %q, want %q", name, data, want)
		}
	}

	// The generating stage is durable in the job log, like command mode.
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

// TestSupervisorAgentDocGeneratorNil invoker is an explicit error: a
// host asking for agent docgen never gets a silent structure-only run.
func TestSupervisorAgentDocGeneratorNil(t *testing.T) {
	home := newTestHome(t)
	cfg := loadTestConfig(t, home)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sup, err := supervisor.New(ctx, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := sup.AgentDocGenerator(nil, "en"); err == nil || !strings.Contains(err.Error(), "nil") {
		t.Fatalf("AgentDocGenerator(nil) error = %v, want a nil-invoker error", err)
	}
}
