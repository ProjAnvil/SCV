package job

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// mustCreate creates a job under a fresh jobs home and fails the test on
// the first error. Every test that needs a job dir goes through here so
// the happy path of Create stays exercised everywhere.
func mustCreate(t *testing.T) (*Job, string) {
	t.Helper()
	j, dir, err := Create(t.TempDir())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return j, dir
}

// reapedPid starts and waits for a sacrificial child process so its pid
// is guaranteed dead and reaped when the helper returns.
func reapedPid(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("sleep", "0")
	if err := cmd.Run(); err != nil {
		t.Fatalf("run sacrificial process: %v", err)
	}
	return cmd.Process.Pid
}

// writeSnapshotFile serializes j directly to <dir>/job.json, bypassing
// Snapshot (which restamps UpdatedAt). Tests use it to forge the on-disk
// state of a crashed job whose snapshot is old.
func writeSnapshotFile(t *testing.T, j *Job, dir string) {
	t.Helper()
	data, err := json.Marshal(j)
	if err != nil {
		t.Fatalf("marshal job: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, snapshotFile), data, 0o644); err != nil {
		t.Fatalf("write job.json: %v", err)
	}
}

func TestCreate(t *testing.T) {
	home := t.TempDir()
	j, dir, err := Create(home)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if matched, _ := regexp.MatchString(`^j_[0-9a-f]{12}$`, j.ID); !matched {
		t.Errorf("job id %q does not match j_ + 12 hex chars", j.ID)
	}
	if filepath.Base(dir) != j.ID {
		t.Errorf("job dir %q is not <jobsHome>/<id>", dir)
	}
	if filepath.Dir(dir) != home {
		t.Errorf("job dir %q is not under jobs home %q", dir, home)
	}
	if j.State != StatePending {
		t.Errorf("State = %q, want %q", j.State, StatePending)
	}
	if j.Pid != 0 {
		t.Errorf("Pid = %d, want 0 (no owner yet)", j.Pid)
	}
	if j.StartedAt.IsZero() || j.UpdatedAt.IsZero() {
		t.Error("StartedAt/UpdatedAt not stamped on create")
	}

	// The initial snapshot is on disk and loadable; a fresh job has no
	// events file yet, which reads as an empty event log.
	loaded, events, err := LoadStatus(dir)
	if err != nil {
		t.Fatalf("LoadStatus after create: %v", err)
	}
	if loaded.ID != j.ID || loaded.State != StatePending {
		t.Errorf("loaded = id %q state %q, want id %q state %q", loaded.ID, loaded.State, j.ID, StatePending)
	}
	if len(events) != 0 {
		t.Errorf("events = %d, want 0", len(events))
	}
	// No pid owner yet, so no pid file.
	if _, err := os.Stat(filepath.Join(dir, pidFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("pid file exists after create: %v", err)
	}
}

func TestCreateErrorPaths(t *testing.T) {
	t.Run("jobs home under a file", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "not-a-dir")
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Create(filepath.Join(file, "jobs")); err == nil {
			t.Error("Create succeeded with a file as jobs home parent")
		}
	})

	home := t.TempDir()
	taken := "j_000000000000"
	if err := os.Mkdir(filepath.Join(home, taken), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("id generator error", func(t *testing.T) {
		boom := func() (string, error) { return "", errors.New("no entropy") }
		if _, _, err := createWith(home, boom, (*Job).Snapshot); err == nil {
			t.Error("createWith succeeded with a failing id generator")
		}
	})

	t.Run("id collision retried", func(t *testing.T) {
		fresh := "j_000000000001"
		calls := 0
		newID := func() (string, error) {
			calls++
			if calls == 1 {
				return taken, nil
			}
			return fresh, nil
		}
		j, dir, err := createWith(home, newID, (*Job).Snapshot)
		if err != nil {
			t.Fatalf("createWith: %v", err)
		}
		if calls != 2 {
			t.Errorf("id generator called %d times, want 2", calls)
		}
		if j.ID != fresh || filepath.Base(dir) != fresh {
			t.Errorf("created id/dir = %q/%q, want %q", j.ID, filepath.Base(dir), fresh)
		}
	})

	t.Run("mkdir error", func(t *testing.T) {
		// An id containing a separator points below a missing parent, so
		// Mkdir fails with something other than ErrExist.
		nested := func() (string, error) { return "missing/id", nil }
		if _, _, err := createWith(home, nested, (*Job).Snapshot); err == nil {
			t.Error("createWith succeeded with an uncreatable job dir")
		}
	})

	t.Run("initial snapshot error", func(t *testing.T) {
		newID := func() (string, error) { return "j_000000000002", nil }
		failing := func(*Job, string) error { return errors.New("snapshot boom") }
		if _, _, err := createWith(home, newID, failing); err == nil {
			t.Error("createWith succeeded with a failing snapshot hook")
		}
	})

	t.Run("id collisions exhausted", func(t *testing.T) {
		always := func() (string, error) { return taken, nil }
		if _, _, err := createWith(home, always, (*Job).Snapshot); err == nil {
			t.Error("createWith succeeded with permanent id collisions")
		}
	})
}

func TestSeqMonotonic(t *testing.T) {
	j, dir := mustCreate(t)

	e1, err := j.Emit(dir, EventStageStarted, "checking", map[string]any{"mode": "full"}, nil)
	if err != nil {
		t.Fatalf("emit 1: %v", err)
	}
	if e1.Seq != 1 {
		t.Errorf("first event seq = %d, want 1", e1.Seq)
	}
	if e1.ID != j.ID {
		t.Errorf("event id = %q, want job id %q", e1.ID, j.ID)
	}
	if e1.TS.IsZero() {
		t.Error("event timestamp not set")
	}

	e2, err := j.Emit(dir, EventStageFinished, "checking", nil, nil)
	if err != nil {
		t.Fatalf("emit 2: %v", err)
	}
	if e2.Seq != 2 {
		t.Errorf("second event seq = %d, want 2", e2.Seq)
	}

	e3, err := j.Emit(dir, EventStageFailed, "analyzing", nil,
		&EventError{Code: "ANALYZER_FAILED", Message: "boom", Retryable: true, LogTail: "tail"})
	if err != nil {
		t.Fatalf("emit 3: %v", err)
	}
	if e3.Seq != 3 {
		t.Errorf("third event seq = %d, want 3", e3.Seq)
	}

	// The log on disk round-trips with sequence numbers and the error
	// payload intact.
	loaded, events, err := LoadStatus(dir)
	if err != nil {
		t.Fatalf("LoadStatus: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3", len(events))
	}
	for i, ev := range events {
		if ev.Seq != i+1 {
			t.Errorf("events[%d].Seq = %d, want %d", i, ev.Seq, i+1)
		}
	}
	if events[2].Error == nil || events[2].Error.Code != "ANALYZER_FAILED" || events[2].Error.LogTail != "tail" {
		t.Errorf("events[2].Error = %+v, want ANALYZER_FAILED with log tail", events[2].Error)
	}

	// A job resumed from disk continues the sequence instead of
	// restarting at 1.
	e4, err := loaded.Emit(dir, EventJobResult, "", nil, nil)
	if err != nil {
		t.Fatalf("emit after load: %v", err)
	}
	if e4.Seq != 4 {
		t.Errorf("seq after reload = %d, want 4", e4.Seq)
	}
	if _, events, _ := LoadStatus(dir); len(events) != 4 {
		t.Errorf("events on disk = %d, want 4", len(events))
	}
}

func TestSnapshotAtomicSwap(t *testing.T) {
	j, dir := mustCreate(t)

	// The snapshot Create wrote is visible.
	first, _, err := LoadStatus(dir)
	if err != nil {
		t.Fatalf("LoadStatus after create: %v", err)
	}
	if first.State != StatePending {
		t.Fatalf("first snapshot state = %q, want pending", first.State)
	}

	// A transition swaps in a new full snapshot: state, pid, pid file,
	// and detail all replace the previous values.
	j.State = StateDone
	j.Pid = os.Getpid()
	j.Detail = map[string]any{"documents": 4}
	before := time.Now().UTC()
	if err := j.Snapshot(dir); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	second, _, err := LoadStatus(dir)
	if err != nil {
		t.Fatalf("LoadStatus after swap: %v", err)
	}
	if second.State != StateDone || second.Pid != os.Getpid() {
		t.Errorf("swapped snapshot = state %q pid %d, want done pid %d", second.State, second.Pid, os.Getpid())
	}
	if second.UpdatedAt.Before(before) {
		t.Errorf("UpdatedAt = %v, want >= %v", second.UpdatedAt, before)
	}
	if docs, ok := second.Detail["documents"].(float64); !ok || docs != 4 {
		t.Errorf("Detail[documents] = %v, want 4", second.Detail["documents"])
	}

	// The pid of the owning process is recorded in the pid file.
	pidData, err := os.ReadFile(filepath.Join(dir, pidFileName))
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	if want := strconv.Itoa(os.Getpid()) + "\n"; string(pidData) != want {
		t.Errorf("pid file = %q, want %q", pidData, want)
	}

	// The tmp file never remains: after a swap only job.json is there.
	leftovers, err := filepath.Glob(filepath.Join(dir, tmpPrefix+"*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Errorf("tmp leftovers after swap: %v", leftovers)
	}

	// A second swap replaces the snapshot again (rename over existing
	// target, not append).
	j.Detail = map[string]any{"documents": 5}
	if err := j.Snapshot(dir); err != nil {
		t.Fatalf("second Snapshot: %v", err)
	}
	third, _, err := LoadStatus(dir)
	if err != nil {
		t.Fatalf("LoadStatus after second swap: %v", err)
	}
	if docs, ok := third.Detail["documents"].(float64); !ok || docs != 5 {
		t.Errorf("Detail[documents] after second swap = %v, want 5", third.Detail["documents"])
	}
}

func TestSnapshotErrorPaths(t *testing.T) {
	t.Run("unmarshalable detail", func(t *testing.T) {
		j, dir := mustCreate(t)
		j.Detail = map[string]any{"f": func() {}}
		if err := j.Snapshot(dir); err == nil {
			t.Error("Snapshot succeeded with an unmarshalable detail map")
		}
	})

	t.Run("missing snapshot dir", func(t *testing.T) {
		j, _ := mustCreate(t)
		if err := j.Snapshot(filepath.Join(t.TempDir(), "missing")); err == nil {
			t.Error("Snapshot succeeded with a nonexistent job dir")
		}
	})

	t.Run("rename blocked by directory", func(t *testing.T) {
		j, dir := mustCreate(t)
		// Replace the snapshot file with a directory so the atomic
		// rename cannot land.
		if err := os.Remove(filepath.Join(dir, snapshotFile)); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(dir, snapshotFile), 0o755); err != nil {
			t.Fatal(err)
		}
		j.State = StateDone
		if err := j.Snapshot(dir); err == nil {
			t.Error("Snapshot succeeded when job.json could not be replaced")
		}
		leftovers, _ := filepath.Glob(filepath.Join(dir, tmpPrefix+"*"))
		if len(leftovers) != 0 {
			t.Errorf("tmp file not cleaned up after failed swap: %v", leftovers)
		}
	})

	t.Run("pid file blocked by directory", func(t *testing.T) {
		j, dir := mustCreate(t)
		if err := os.Mkdir(filepath.Join(dir, pidFileName), 0o755); err != nil {
			t.Fatal(err)
		}
		j.Pid = 1234
		if err := j.Snapshot(dir); err == nil {
			t.Error("Snapshot succeeded when the pid file could not be written")
		}
	})
}

func TestEventsToleratesTruncatedLastLine(t *testing.T) {
	j, dir := mustCreate(t)
	if _, err := j.Emit(dir, EventStageStarted, "checking", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Emit(dir, EventStageFinished, "checking", nil, nil); err != nil {
		t.Fatal(err)
	}

	// Forge the crash artifact: an append torn mid-line leaves a half
	// JSON object without a trailing newline.
	path := filepath.Join(dir, eventsFile)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	torn := `{"id":"` + j.ID + `","seq":3,"ts":"2026-11-13T09:0`
	if _, err := f.WriteString(torn); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	loaded, events, err := LoadStatus(dir)
	if err != nil {
		t.Fatalf("LoadStatus with torn last line: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2 (torn line dropped)", len(events))
	}
	if events[0].Seq != 1 || events[1].Seq != 2 {
		t.Errorf("seqs = [%d %d], want [1 2]", events[0].Seq, events[1].Seq)
	}
	if loaded.lastSeq != 2 {
		t.Errorf("loaded.lastSeq = %d, want 2", loaded.lastSeq)
	}
}

func TestEmitErrorPath(t *testing.T) {
	t.Run("unmarshalable detail", func(t *testing.T) {
		j, dir := mustCreate(t)
		if _, err := j.Emit(dir, EventDegraded, "checking", map[string]any{"f": func() {}}, nil); err == nil {
			t.Error("Emit succeeded with an unmarshalable detail map")
		}
		// A failed emit must not consume a sequence number.
		e, err := j.Emit(dir, EventDegraded, "checking", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if e.Seq != 1 {
			t.Errorf("seq after failed emit = %d, want 1", e.Seq)
		}
	})

	t.Run("events file is a directory", func(t *testing.T) {
		j, dir := mustCreate(t)
		if err := os.Mkdir(filepath.Join(dir, eventsFile), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := j.Emit(dir, EventArtifactProduced, "generating", nil, nil); err == nil {
			t.Error("Emit succeeded with a directory as events file")
		}
	})
}

func TestDeadPidMarkedInterrupted(t *testing.T) {
	pid := reapedPid(t)
	j, dir := mustCreate(t)
	past := time.Now().UTC().Add(-2 * time.Second)

	// Forge the on-disk state of a crashed run: an old snapshot whose
	// recorded pid belongs to a reaped process.
	j.State = StateAnalyzing
	j.Stage = "analyzing"
	j.Pid = pid
	j.Detail = map[string]any{"repo": "demo"}
	j.StartedAt = past
	j.UpdatedAt = past
	writeSnapshotFile(t, j, dir)

	loaded, events, err := LoadStatus(dir)
	if err != nil {
		t.Fatalf("LoadStatus: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events = %d, want 0", len(events))
	}

	if !MarkInterruptedIfDead(loaded) {
		t.Fatal("MarkInterruptedIfDead = false, want true for dead pid in non-terminal state")
	}
	if loaded.State != StateFailed {
		t.Errorf("State = %q, want %q", loaded.State, StateFailed)
	}
	evErr, ok := loaded.Detail["error"].(EventError)
	if !ok {
		t.Fatalf("Detail[error] = %T, want EventError", loaded.Detail["error"])
	}
	if evErr.Code != CodeInterrupted {
		t.Errorf("error code = %q, want %q", evErr.Code, CodeInterrupted)
	}
	if !evErr.Retryable {
		t.Error("INTERRUPTED error not flagged retryable (recovery is re-run)")
	}
	// Pre-existing detail survives the marking.
	if loaded.Detail["repo"] != "demo" {
		t.Errorf("Detail[repo] = %v, want demo", loaded.Detail["repo"])
	}

	// The marking persists through a snapshot swap and is idempotent.
	if err := loaded.Snapshot(dir); err != nil {
		t.Fatalf("Snapshot after marking: %v", err)
	}
	reloaded, _, err := LoadStatus(dir)
	if err != nil {
		t.Fatalf("LoadStatus after marking: %v", err)
	}
	if reloaded.State != StateFailed {
		t.Errorf("reloaded State = %q, want failed", reloaded.State)
	}
	persisted, ok := reloaded.Detail["error"].(map[string]any)
	if !ok || persisted["code"] != CodeInterrupted {
		t.Errorf("reloaded Detail[error] = %#v, want persisted INTERRUPTED", reloaded.Detail["error"])
	}
	if MarkInterruptedIfDead(reloaded) {
		t.Error("MarkInterruptedIfDead marked an already-terminal job")
	}

	t.Run("no prior detail", func(t *testing.T) {
		// A run that crashed before writing any detail still gets the
		// INTERRUPTED error recorded.
		j := &Job{
			ID:        "j_nodetail0000",
			State:     StateQueryTier,
			Pid:       reapedPid(t),
			UpdatedAt: time.Now().UTC().Add(-2 * time.Second),
		}
		if !MarkInterruptedIfDead(j) {
			t.Fatal("MarkInterruptedIfDead = false, want true")
		}
		evErr, ok := j.Detail["error"].(EventError)
		if !ok || evErr.Code != CodeInterrupted {
			t.Errorf("Detail[error] = %#v, want INTERRUPTED EventError", j.Detail["error"])
		}
	})
}

func TestLoadStatusErrorPaths(t *testing.T) {
	t.Run("missing snapshot", func(t *testing.T) {
		if _, _, err := LoadStatus(t.TempDir()); err == nil {
			t.Error("LoadStatus succeeded without a snapshot")
		}
	})

	t.Run("corrupt snapshot", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, snapshotFile), []byte("{not json"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := LoadStatus(dir); err == nil {
			t.Error("LoadStatus succeeded with a corrupt snapshot")
		}
	})

	t.Run("unreadable events log", func(t *testing.T) {
		_, dir := mustCreate(t)
		if err := os.Mkdir(filepath.Join(dir, eventsFile), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, _, err := LoadStatus(dir); err == nil {
			t.Error("LoadStatus succeeded with a directory as events log")
		}
	})
}

func TestTerminalJobNotMarked(t *testing.T) {
	pid := reapedPid(t)
	past := time.Now().UTC().Add(-2 * time.Second)
	for _, state := range []State{StateDone, StateFailed, StateCancelled, StateSkipped} {
		j := &Job{
			ID:        "j_terminal0000",
			State:     state,
			Pid:       pid,
			UpdatedAt: past,
		}
		if MarkInterruptedIfDead(j) {
			t.Errorf("MarkInterruptedIfDead marked terminal state %q", state)
		}
		if j.State != state {
			t.Errorf("State = %q, want unchanged %q", j.State, state)
		}
		if j.Detail != nil {
			t.Errorf("Detail modified for terminal state %q: %v", state, j.Detail)
		}
	}
}

func TestLivePidNotMarked(t *testing.T) {
	t.Run("live owner process", func(t *testing.T) {
		j := &Job{
			ID:        "j_live00000000",
			State:     StateGrading,
			Pid:       os.Getpid(),
			UpdatedAt: time.Now().UTC().Add(-5 * time.Second),
		}
		if MarkInterruptedIfDead(j) {
			t.Error("MarkInterruptedIfDead marked a job owned by a live process")
		}
		if j.State != StateGrading {
			t.Errorf("State = %q, want %q", j.State, StateGrading)
		}
	})

	t.Run("fresh snapshot not trusted as dead yet", func(t *testing.T) {
		// Without /proc (macOS fallback) a dead verdict is trusted only
		// when the snapshot is older than the freshness window.
		j := &Job{
			ID:        "j_fresh0000000",
			State:     StatePreparing,
			Pid:       reapedPid(t),
			UpdatedAt: time.Now().UTC(),
		}
		if MarkInterruptedIfDead(j) {
			t.Error("MarkInterruptedIfDead trusted a dead verdict on a fresh snapshot")
		}
	})

	t.Run("no recorded pid", func(t *testing.T) {
		j := &Job{
			ID:        "j_nopid0000000",
			State:     StatePending,
			Pid:       0,
			UpdatedAt: time.Now().UTC().Add(-5 * time.Second),
		}
		if MarkInterruptedIfDead(j) {
			t.Error("MarkInterruptedIfDead marked a job with no recorded pid")
		}
	})
}

func TestIsTerminal(t *testing.T) {
	nonTerminal := []State{
		StatePending, StateChecking, StatePreparing, StateDetecting,
		StateAnalyzing, StateGrading, StateQueryTier, StateGenerating,
		StateFinalizing,
	}
	for _, s := range nonTerminal {
		if IsTerminal(s) {
			t.Errorf("IsTerminal(%q) = true, want false", s)
		}
	}
	for _, s := range []State{StateDone, StateFailed, StateCancelled, StateSkipped} {
		if !IsTerminal(s) {
			t.Errorf("IsTerminal(%q) = false, want true", s)
		}
	}
}

func TestStateAndEventConstants(t *testing.T) {
	states := map[State]string{
		StatePending: "pending", StateChecking: "checking", StatePreparing: "preparing",
		StateDetecting: "detecting", StateAnalyzing: "analyzing", StateGrading: "grading",
		StateQueryTier: "query_tier", StateGenerating: "generating", StateFinalizing: "finalizing",
		StateDone: "done", StateFailed: "failed", StateCancelled: "cancelled", StateSkipped: "skipped",
	}
	for st, want := range states {
		if string(st) != want {
			t.Errorf("state %q != %q", st, want)
		}
	}
	types := map[string]string{
		EventStageStarted: "stage_started", EventStageFinished: "stage_finished",
		EventStageFailed: "stage_failed", EventDegraded: "degraded",
		EventArtifactProduced: "artifact_produced", EventJobResult: "job_result",
	}
	for got, want := range types {
		if got != want {
			t.Errorf("event type %q != %q", got, want)
		}
	}
}

func TestNewJobID(t *testing.T) {
	id, err := newJobID(rand.Read)
	if err != nil {
		t.Fatalf("newJobID: %v", err)
	}
	if matched, _ := regexp.MatchString(`^j_[0-9a-f]{12}$`, id); !matched {
		t.Errorf("newJobID = %q, want j_ + 12 hex chars", id)
	}
	failing := func([]byte) (int, error) { return 0, errors.New("no entropy") }
	if _, err := newJobID(failing); err == nil {
		t.Error("newJobID succeeded with a failing reader")
	}
}

func TestParseEvents(t *testing.T) {
	line1 := `{"id":"j_1","seq":1,"ts":"2026-11-13T09:00:00Z","type":"stage_started","stage":"checking"}`
	line2 := `{"id":"j_1","seq":2,"ts":"2026-11-13T09:00:01Z","type":"stage_finished","stage":"checking"}`

	tests := []struct {
		name    string
		data    string
		want    []int
		wantErr bool
	}{
		{"complete log", line1 + "\n" + line2 + "\n", []int{1, 2}, false},
		// A writer always terminates a line with '\n', so an
		// unterminated tail is torn even when it happens to parse.
		{"unterminated last line", line1 + "\n" + line2, []int{1}, false},
		{"torn last line", line1 + "\n" + line2 + "\n" + `{"id":"j_1","seq":3,"ts":"2026-`, []int{1, 2}, false},
		{"only a torn line", `{"id":"j_1","seq":1`, nil, false},
		{"empty file", "", nil, false},
		{"corrupt complete line", line1 + "\n" + "not json\n" + line2 + "\n", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, err := parseEvents([]byte(tt.data))
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseEvents error = %v, wantErr %v", err, tt.wantErr)
			}
			if len(events) != len(tt.want) {
				t.Fatalf("events = %d, want %d", len(events), len(tt.want))
			}
			for i, seq := range tt.want {
				if events[i].Seq != seq {
					t.Errorf("events[%d].Seq = %d, want %d", i, events[i].Seq, seq)
				}
			}
		})
	}
}

func TestReadEventsFileHandling(t *testing.T) {
	dir := t.TempDir()

	t.Run("missing file is an empty log", func(t *testing.T) {
		events, err := readEvents(filepath.Join(dir, "absent.jsonl"))
		if err != nil {
			t.Fatalf("readEvents on missing file: %v", err)
		}
		if events != nil {
			t.Errorf("events = %v, want nil", events)
		}
	})

	t.Run("directory is a read error", func(t *testing.T) {
		if err := os.Mkdir(filepath.Join(dir, "adir"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := readEvents(filepath.Join(dir, "adir")); err == nil {
			t.Error("readEvents succeeded on a directory")
		}
	})
}

func TestParseStatStartTicks(t *testing.T) {
	// Build a /proc/<pid>/stat fixture: field 22 (starttime) is the 20th
	// token after the closing paren of the comm field.
	tokens := []string{"R"}
	for field := 4; field <= 25; field++ {
		v := field * 10
		if field == 22 {
			v = 1234567
		}
		tokens = append(tokens, strconv.Itoa(v))
	}
	stat := "42 ((weird) comm) " + strings.Join(tokens, " ")

	tests := []struct {
		name    string
		stat    string
		want    uint64
		wantErr bool
	}{
		{"starttime with parenthesised comm", stat, 1234567, false},
		{"no closing paren", "garbage", 0, true},
		{"too few fields", "42 (comm) R 1", 0, true},
		{"non-numeric starttime", "42 (comm) " + strings.Repeat("0 ", 19) + "soon", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseStatStartTicks(tt.stat)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseStatStartTicks error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("parseStatStartTicks = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestStartTicksFromFile(t *testing.T) {
	dir := t.TempDir()

	valid := filepath.Join(dir, "valid")
	if err := os.WriteFile(valid, []byte("7 (sh) R "+strings.Repeat("1 ", 18)+"42"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ticks, ok := startTicksFromFile(valid); !ok || ticks != 42 {
		t.Errorf("startTicksFromFile(valid) = %d,%v want 42,true", ticks, ok)
	}

	invalid := filepath.Join(dir, "invalid")
	if err := os.WriteFile(invalid, []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ticks, ok := startTicksFromFile(invalid); ok || ticks != 0 {
		t.Errorf("startTicksFromFile(invalid) = %d,%v want 0,false", ticks, ok)
	}

	if ticks, ok := startTicksFromFile(filepath.Join(dir, "missing")); ok || ticks != 0 {
		t.Errorf("startTicksFromFile(missing) = %d,%v want 0,false", ticks, ok)
	}
}

func TestOwnerAlive(t *testing.T) {
	stale := time.Now().UTC().Add(-10 * time.Second)
	matching := func(int) (uint64, bool) { return 77, true }
	reused := func(int) (uint64, bool) { return 9999, true }
	noproc := func(int) (uint64, bool) { return 0, false }

	// /proc verdicts: recorded start time matches, differs (pid reuse),
	// or was never recorded.
	if !ownerAlive(&Job{Pid: 111, PidStart: 77, UpdatedAt: stale}, matching) {
		t.Error("ownerAlive = false for matching start time")
	}
	if ownerAlive(&Job{Pid: 111, PidStart: 77, UpdatedAt: stale}, reused) {
		t.Error("ownerAlive = true for reused pid (start time mismatch)")
	}
	if !ownerAlive(&Job{Pid: 111, PidStart: 0, UpdatedAt: stale}, matching) {
		t.Error("ownerAlive = false with no recorded baseline")
	}

	// Without /proc: a fresh snapshot is assumed alive; a stale snapshot
	// defers to the signal probe.
	if !ownerAlive(&Job{Pid: 111, PidStart: 0, UpdatedAt: time.Now().UTC()}, noproc) {
		t.Error("ownerAlive = false for a fresh snapshot")
	}
	if ownerAlive(&Job{Pid: reapedPid(t), PidStart: 0, UpdatedAt: stale}, noproc) {
		t.Error("ownerAlive = true for a stale snapshot with a dead pid")
	}
	if !ownerAlive(&Job{Pid: os.Getpid(), PidStart: 0, UpdatedAt: stale}, noproc) {
		t.Error("ownerAlive = false for a stale snapshot with a live pid")
	}
}

// fakeSyncWriter exercises the write/sync error paths of the log
// appenders without depending on disk failures.
type fakeSyncWriter struct {
	writeErr error
	syncErr  error
}

func (f *fakeSyncWriter) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(p), nil
}

func (f *fakeSyncWriter) Sync() error { return f.syncErr }

func TestWriteSynced(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name    string
		w       *fakeSyncWriter
		wantErr bool
	}{
		{"clean write", &fakeSyncWriter{}, false},
		{"write failure", &fakeSyncWriter{writeErr: boom}, true},
		{"sync failure", &fakeSyncWriter{syncErr: boom}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := writeSynced(tt.w, []byte("data"))
			if (err != nil) != tt.wantErr {
				t.Fatalf("writeSynced error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// fakeFile stands in for the tmp *os.File in writeSnapshot so the write
// and close failures can be forced deterministically.
type fakeFile struct {
	fakeSyncWriter
	path     string
	closeErr error
}

func (f *fakeFile) Name() string { return f.path }
func (f *fakeFile) Close() error { return f.closeErr }

func TestWriteSnapshot(t *testing.T) {
	t.Run("clean swap", func(t *testing.T) {
		dir := t.TempDir()
		tmp, err := os.CreateTemp(dir, tmpPrefix)
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(dir, snapshotFile)
		if err := writeSnapshot(tmp, []byte("snapshot"), target); err != nil {
			t.Fatalf("writeSnapshot: %v", err)
		}
		data, err := os.ReadFile(target)
		if err != nil || string(data) != "snapshot" {
			t.Errorf("target = %q err %v, want the swapped-in snapshot", data, err)
		}
		leftovers, _ := filepath.Glob(filepath.Join(dir, tmpPrefix+"*"))
		if len(leftovers) != 0 {
			t.Errorf("tmp leftovers: %v", leftovers)
		}
	})

	t.Run("write failure removes tmp", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "victim")
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		f := &fakeFile{fakeSyncWriter{writeErr: errors.New("disk full")}, path, nil}
		if err := writeSnapshot(f, []byte("x"), filepath.Join(dir, snapshotFile)); err == nil {
			t.Error("writeSnapshot succeeded with a failing write")
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("tmp file not removed after write failure: %v", err)
		}
	})

	t.Run("rename failure removes tmp", func(t *testing.T) {
		dir := t.TempDir()
		tmp, err := os.CreateTemp(dir, tmpPrefix)
		if err != nil {
			t.Fatal(err)
		}
		// Blocking the target with a directory makes the rename fail.
		target := filepath.Join(dir, snapshotFile)
		if err := os.Mkdir(target, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := writeSnapshot(tmp, []byte("x"), target); err == nil {
			t.Error("writeSnapshot succeeded with a blocked rename")
		}
		leftovers, _ := filepath.Glob(filepath.Join(dir, tmpPrefix+"*"))
		if len(leftovers) != 0 {
			t.Errorf("tmp leftovers: %v", leftovers)
		}
	})

	t.Run("close failure after rename keeps target", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "renamed")
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(dir, snapshotFile)
		f := &fakeFile{fakeSyncWriter{}, path, errors.New("close boom")}
		if err := writeSnapshot(f, []byte("x"), target); err == nil {
			t.Error("writeSnapshot succeeded with a failing close")
		}
		// The rename already succeeded; the close error must surface
		// without deleting the swapped-in file.
		if _, err := os.Stat(target); err != nil {
			t.Errorf("swapped-in snapshot deleted after close failure: %v", err)
		}
	})
}

// Signal probing must distinguish "no such process" from "exists but not
// ours": pid 1 always exists, so probing it reports alive even when the
// probe is denied (EPERM) — or succeeds outright for a root runner.
func TestSignalAlive(t *testing.T) {
	if !signalAlive(1) {
		t.Error("signalAlive(1) = false, want true (pid 1 always exists)")
	}
	if signalAlive(1 << 30) {
		t.Error("signalAlive(out-of-range pid) = true, want false (ESRCH)")
	}
	if signalAlive(reapedPid(t)) {
		t.Error("signalAlive(reaped) = true, want false")
	}
	if !signalAlive(os.Getpid()) {
		t.Error("signalAlive(self) = false, want true")
	}
	if !signalAlive(syscall.Getpgrp()) {
		t.Error("signalAlive(process group) = false, want true")
	}
}
