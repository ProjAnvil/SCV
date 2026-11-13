//go:build unix

package run

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/projanvil/scv/engine/internal/job"
)

// groupLeaderScript runs as a standalone process-group leader for the
// Cancel tests: it spawns a sleeper, records both pids to the file named
// by argv[1], and sleeps. LEADER_IGNORE_TERM=1 installs a SIGTERM ignore
// so only the SIGKILL backstop can end the leader.
const groupLeaderScript = `import json, os, subprocess, sys, time
if os.environ.get("LEADER_IGNORE_TERM") == "1":
    import signal
    signal.signal(signal.SIGTERM, signal.SIG_IGN)
child = subprocess.Popen(["sleep", "60"])
with open(sys.argv[1], "w") as fh:
    json.dump({"self": os.getpid(), "child": child.pid}, fh)
time.sleep(30)
`

// spawnGroupLeader starts the script as its own process-group leader
// (pgid == leader pid), reaps it from a goroutine so a zombie never
// keeps the group alive, and waits for the pid record. It returns the
// leader pid (== pgid) and the spawned child pid.
func spawnGroupLeader(t *testing.T, ignoreTERM bool) (leader, child int) {
	t.Helper()
	pidsFile := filepath.Join(t.TempDir(), "pids.json")
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Fatalf("python3 not found: %v", err)
	}
	ignore := "0"
	if ignoreTERM {
		ignore = "1"
	}
	cmd := exec.Command(py, "-c", groupLeaderScript, pidsFile)
	cmd.Env = append(os.Environ(), "LEADER_IGNORE_TERM="+ignore)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start group leader: %v", err)
	}
	go func() { _ = cmd.Wait() }()
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
	waitForFile(t, pidsFile, 10*time.Second)
	var pids struct {
		Self  int `json:"self"`
		Child int `json:"child"`
	}
	readJSON(t, pidsFile, &pids)
	return pids.Self, pids.Child
}

// waitGone polls until cond holds, failing the test on timeout — death
// must be observed as an ESRCH probe result, not inferred from elapsed
// time (the reaping of a reparented grandchild is asynchronous).
func waitGone(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s still present after cancel", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestCancelKillsProcessGroupTree pins the T6 review closure: a stub
// perceive that spawned a sleeper grandchild must lose the WHOLE group
// when the run is cancelled. A child-only kill leaves the grandchild
// holding the stdout pipe, so Wait's copy loop never drains and the
// tree survives — the group kill has to close this.
func TestCancelKillsProcessGroupTree(t *testing.T) {
	f := newFixture(t, false)
	stubMode(t, "spawner")
	analysisDir := f.analysisDirOf("tree")
	pidsFile := filepath.Join(analysisDir, ".stub-pids.json")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := f.run(ctx, RunRequest{Source: makeGitRepo(t), Project: "tree"}, nil)
		done <- err
	}()
	waitForFile(t, pidsFile, 10*time.Second) // perceive mid-run, grandchild spawned
	var pids struct {
		Self  int `json:"self"`
		Child int `json:"child"`
	}
	readJSON(t, pidsFile, &pids)

	cancel()
	select {
	case <-done: // Wait returned instead of draining the grandchild's pipe
	case <-time.After(10 * time.Second):
		t.Fatal("perceive Wait did not return after cancel: the grandchild kept the pipe open")
	}

	// The tree is actually gone: no process remains in the perceive
	// group (kill(-pgid, 0) reports ESRCH) and the grandchild pid is
	// dead — not merely a bounded elapsed time.
	waitGone(t, "perceive process group", func() bool {
		return errors.Is(syscall.Kill(-pids.Self, syscall.Signal(0)), syscall.ESRCH)
	})
	waitGone(t, "sleeper grandchild", func() bool {
		return errors.Is(syscall.Kill(pids.Child, syscall.Signal(0)), syscall.ESRCH)
	})
}

// TestCancelSetsCancelledState drives Cancel against a real process
// group: after the group exits, the job lands in cancelled with the T6
// cancel shape (stage_failed CANCELLED on the open stage, then the
// cancelled job_result). The backstop case proves the SIGKILL: the
// leader ignores SIGTERM, so only the hard kill can empty the group.
func TestCancelSetsCancelledState(t *testing.T) {
	oldGrace := cancelGracePeriod
	cancelGracePeriod = 400 * time.Millisecond
	t.Cleanup(func() { cancelGracePeriod = oldGrace })

	for _, tc := range []struct {
		name       string
		ignoreTERM bool
	}{
		{"graceful SIGTERM", false},
		{"SIGKILL backstop", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			leader, child := spawnGroupLeader(t, tc.ignoreTERM)
			j, dir, err := job.Create(filepath.Join(t.TempDir(), "jobs"))
			if err != nil {
				t.Fatal(err)
			}
			j.State, j.Stage, j.Pid = job.StateDetecting, string(job.StateDetecting), leader
			if err := j.Snapshot(dir); err != nil {
				t.Fatal(err)
			}

			if err := Cancel(dir, j); err != nil {
				t.Fatalf("Cancel: %v", err)
			}
			waitGone(t, "leader process group", func() bool {
				return errors.Is(syscall.Kill(-leader, syscall.Signal(0)), syscall.ESRCH)
			})
			waitGone(t, "sleeper child", func() bool {
				return errors.Is(syscall.Kill(child, syscall.Signal(0)), syscall.ESRCH)
			})

			got, events, err := job.LoadStatus(dir)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != job.StateCancelled {
				t.Fatalf("job state = %q, want cancelled", got.State)
			}
			assertSeq(t, events, []string{
				"stage_failed:detecting",
				"job_result:cancelled",
			})
			failed := findEvent(t, events, job.EventStageFailed, string(job.StateDetecting))
			if failed.Error == nil || failed.Error.Code != CodeCancelled {
				t.Fatalf("stage_failed error = %+v, want code %s", failed.Error, CodeCancelled)
			}
			result := findEvent(t, events, job.EventJobResult, string(job.StateCancelled))
			if result.Error == nil || result.Error.Code != CodeCancelled {
				t.Fatalf("job_result error = %+v, want code %s", result.Error, CodeCancelled)
			}
		})
	}
}

// jobHomeSnapshot captures the files a refused Cancel must not touch.
func jobHomeSnapshot(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, name := range []string{"job.json", "events.jsonl", "pid"} {
		if data, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			out[name] = data
		}
	}
	return out
}

// TestCancelTerminalJobRefuses pins the no-side-effects refusal: a job
// in any terminal state (or one with no recorded process group) yields
// the typed WORKFLOW_NOT_CANCELLABLE error, leaves the job home
// untouched, and never signals a live process group.
func TestCancelTerminalJobRefuses(t *testing.T) {
	for _, state := range []job.State{job.StateDone, job.StateFailed, job.StateCancelled, job.StateSkipped} {
		t.Run(string(state), func(t *testing.T) {
			j, dir, err := job.Create(filepath.Join(t.TempDir(), "jobs"))
			if err != nil {
				t.Fatal(err)
			}
			j.State = state
			if err := j.Snapshot(dir); err != nil {
				t.Fatal(err)
			}
			before := jobHomeSnapshot(t, dir)

			err = Cancel(dir, j)
			if !errors.Is(err, ErrWorkflowNotCancellable) {
				t.Fatalf("Cancel error = %v, want WORKFLOW_NOT_CANCELLABLE", err)
			}
			if !strings.Contains(err.Error(), string(state)) {
				t.Fatalf("error %q should name the terminal state", err)
			}
			if after := jobHomeSnapshot(t, dir); !reflect.DeepEqual(before, after) {
				t.Fatalf("refused cancel changed the job home: %v -> %v", before, after)
			}
		})
	}

	// No recorded process group: nothing to signal, same refusal.
	j, dir, err := job.Create(filepath.Join(t.TempDir(), "jobs"))
	if err != nil {
		t.Fatal(err)
	}
	j.State = job.StateDetecting
	if err := j.Snapshot(dir); err != nil {
		t.Fatal(err)
	}
	if err := Cancel(dir, j); !errors.Is(err, ErrWorkflowNotCancellable) {
		t.Fatalf("pid-less Cancel error = %v, want WORKFLOW_NOT_CANCELLABLE", err)
	}

	// A live process group behind a terminal job is never signaled.
	leader, _ := spawnGroupLeader(t, false)
	j2, dir2, err := job.Create(filepath.Join(t.TempDir(), "jobs"))
	if err != nil {
		t.Fatal(err)
	}
	j2.State, j2.Pid = job.StateDone, leader
	if err := j2.Snapshot(dir2); err != nil {
		t.Fatal(err)
	}
	if err := Cancel(dir2, j2); !errors.Is(err, ErrWorkflowNotCancellable) {
		t.Fatalf("terminal live-group Cancel error = %v, want WORKFLOW_NOT_CANCELLABLE", err)
	}
	if errors.Is(syscall.Kill(-leader, syscall.Signal(0)), syscall.ESRCH) {
		t.Fatal("a refused cancel must not signal a live process group")
	}
}

// TestCancelStateWriteFailuresSurface proves the write failures of the
// cancelled-state transition surface as Cancel errors instead of being
// swallowed: the stage_failed append, then the terminal snapshot.
func TestCancelStateWriteFailuresSurface(t *testing.T) {
	newJob := func(t *testing.T) (*job.Job, string) {
		j, dir, err := job.Create(filepath.Join(t.TempDir(), "jobs"))
		if err != nil {
			t.Fatal(err)
		}
		j.State, j.Stage = job.StateDetecting, string(job.StateDetecting)
		if _, err := j.Emit(dir, job.EventStageStarted, string(job.StateDetecting), nil, nil); err != nil {
			t.Fatal(err)
		}
		if err := j.Snapshot(dir); err != nil {
			t.Fatal(err)
		}
		return j, dir
	}
	lock := func(t *testing.T, target string, mode os.FileMode) {
		if err := os.Chmod(target, mode); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(target, 0o755) })
	}

	// events.jsonl unwritable: the stage_failed append fails.
	j, dir := newJob(t)
	lock(t, filepath.Join(dir, "events.jsonl"), 0o400)
	lock(t, dir, 0o500)
	if err := markCancelled(dir, j, 42); err == nil || !strings.Contains(err.Error(), "stage_failed") {
		t.Fatalf("error = %v, want the stage_failed emit failure", err)
	}

	// events.jsonl still appendable but the directory read-only: the
	// snapshot swap (a tmp file in the dir) fails.
	j2, dir2 := newJob(t)
	lock(t, dir2, 0o500)
	if err := markCancelled(dir2, j2, 42); err == nil || !strings.Contains(err.Error(), "snapshot") {
		t.Fatalf("error = %v, want the snapshot failure", err)
	}
}

// TestSetProcessGroupLeadsOwnGroup pins the engine-self half of spec
// section 10: after SetProcessGroup, the calling process leads its own
// group, which is what Run records as the job's cancellable pgid.
func TestSetProcessGroupLeadsOwnGroup(t *testing.T) {
	SetProcessGroup()
	if pgid := syscall.Getpgrp(); pgid != os.Getpid() {
		t.Fatalf("process group = %d, want own pid %d", pgid, os.Getpid())
	}
}

// TestCancelRunWiringRecordsPid pins the run-side wiring: a pipeline run
// records its process group on the job (job.json + pid file), so Cancel
// has a target.
func TestCancelRunWiringRecordsPid(t *testing.T) {
	f := newFixture(t, false)
	stubMode(t, "full")
	if _, err := f.run(context.Background(), RunRequest{Source: makeGitRepo(t), Project: "pid"}, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	dir := filepath.Join(f.scvHome, "jobs", f.events[0].ID)
	j, _, err := job.LoadStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if j.Pid != os.Getpid() {
		t.Fatalf("job pid = %d, want the running process's group id %d", j.Pid, os.Getpid())
	}
	pidData, err := os.ReadFile(filepath.Join(dir, "pid"))
	if err != nil {
		t.Fatalf("pid file: %v", err)
	}
	if got := strings.TrimSpace(string(pidData)); got != strconv.Itoa(j.Pid) {
		t.Fatalf("pid file = %q, want %d", got, j.Pid)
	}
}
