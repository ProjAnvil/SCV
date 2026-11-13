package run

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

// waitForFile polls until path exists or the timeout hits, failing the
// test on timeout.
func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestSecondRunWorkflowsLocked pins per-repo mutual exclusion (spec
// section 10): the second acquirer of the same analysis dir lock fails
// fast with the typed WORKFLOW_LOCKED error, and a second concurrent
// run on the same analysis dir fails at preparing with that code.
func TestSecondRunWorkflowsLocked(t *testing.T) {
	// Unit level: second acquisition refused while held, freed by
	// release, other analysis dirs independent.
	dir := t.TempDir()
	other := t.TempDir()
	release, err := AcquireRepoLock(dir)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if _, err := AcquireRepoLock(other); err != nil {
		t.Fatalf("a different analysis dir must not be locked: %v", err)
	}
	second, err := AcquireRepoLock(dir)
	if !errors.Is(err, ErrWorkflowLocked) {
		t.Fatalf("second acquire error = %v, want WORKFLOW_LOCKED", err)
	}
	if second != nil {
		t.Fatal("a refused acquisition must not return a release func")
	}
	if !strings.Contains(err.Error(), repoLockFile) {
		t.Fatalf("error %q must name the lock file", err)
	}
	release()
	again, err := AcquireRepoLock(dir)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	again()

	// Pipeline level: run 1 holds the lock mid-perceive (spawner stub
	// hangs); run 2 on the same project fails fast at preparing. Run 1
	// gets its own no-op sink driven straight through runPipeline, so
	// the fixture's shared sink stays single-goroutine (race-free).
	f := newFixture(t, false)
	stubMode(t, "spawner")
	src := makeGitRepo(t)
	analysisDir := f.analysisDirOf("lock")
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	done1 := make(chan error, 1)
	go func() {
		_, err := runPipeline(ctx1, f.cfg, RunRequest{Source: src, Project: "lock"},
			func(job.Event) {}, nil, f.discover)
		done1 <- err
	}()
	// The stub's pid record proves perceive is mid-run, so the lock is
	// necessarily held.
	waitForFile(t, filepath.Join(analysisDir, ".stub-pids.json"), 10*time.Second)

	mark := len(f.events)
	// A timeout only as a RED-phase guard: a green run 2 fails in
	// milliseconds, long before the context ever expires.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	_, err = f.run(ctx2, RunRequest{Source: src, Project: "lock"}, nil)
	if !errors.Is(err, ErrWorkflowLocked) {
		t.Fatalf("second run error = %v, want WORKFLOW_LOCKED", err)
	}
	run2 := f.events[mark:]
	assertSeq(t, run2, []string{
		"stage_started:checking",
		"degraded:checking", // the fixture's docgen-unconfigured item
		"stage_finished:checking",
		"stage_started:preparing",
		"stage_failed:preparing",
		"job_result:failed",
	})
	failed := findEvent(t, run2, job.EventStageFailed, string(job.StatePreparing))
	if failed.Error == nil || failed.Error.Code != CodeWorkflowLocked {
		t.Fatalf("stage_failed error = %+v, want code %s", failed.Error, CodeWorkflowLocked)
	}

	// Ending run 1 releases the lock for the next run.
	cancel1()
	select {
	case <-done1:
	case <-time.After(10 * time.Second):
		t.Fatal("run 1 did not return after cancel")
	}
	released, err := AcquireRepoLock(analysisDir)
	if err != nil {
		t.Fatalf("lock must be released once the run ended: %v", err)
	}
	released()
}

// TestLockInfrastructureFailureFailsPreparing proves a lock acquisition
// error that is not contention still fails the run at preparing, with
// the preparing stage's generic retryable code.
func TestLockInfrastructureFailureFailsPreparing(t *testing.T) {
	f := newFixture(t, false)
	stubMode(t, "full")
	// A file squatting on the analysis root makes the lock's directory
	// uncreatable.
	if err := os.WriteFile(filepath.Join(f.scvHome, "analysis"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := f.run(context.Background(), RunRequest{Source: makeGitRepo(t), Project: "infra"}, nil)
	if err == nil {
		t.Fatal("lock infrastructure failure must fail the run")
	}
	failed := findEvent(t, f.events, job.EventStageFailed, string(job.StatePreparing))
	if failed.Error == nil || failed.Error.Code != CodeCloneFailed ||
		!strings.Contains(failed.Error.Message, "lock analysis dir") {
		t.Fatalf("stage_failed error = %+v, want %s naming the lock", failed.Error, CodeCloneFailed)
	}
}
