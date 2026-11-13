// Package job implements the SCV engine job state model (spec section
// 7). A job is a directory under the jobs home holding three files:
//
//	job.json    — the current snapshot, replaced atomically on every
//	              transition (tmp + fsync + rename)
//	events.jsonl — the append-only event log, fsynced on every append
//	pid          — the process group id of the owning run, while running
//
// Crash tolerance: a snapshot swap never leaves a partial job.json, a
// torn event append loses only the torn line, and a reader that finds a
// dead pid in a non-terminal snapshot marks the job failed with error
// code INTERRUPTED. Recovery from an interrupted run is to run again.
package job

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// State is one state of the job lifecycle. The string values are the
// exact wire format of job.json and of every event's stage context, so
// they must never be renamed (spec section 7).
type State string

// Job lifecycle states (spec section 7). A run moves pending → checking
// → preparing → detecting → analyzing → grading → query_tier →
// generating → finalizing → done; any running state may instead move to
// failed or cancelled, and a pending job may move directly to skipped
// (--skip-unchanged hit). done, failed, cancelled, and skipped are
// terminal.
const (
	StatePending    State = "pending"
	StateChecking   State = "checking"
	StatePreparing  State = "preparing"
	StateDetecting  State = "detecting"
	StateAnalyzing  State = "analyzing"
	StateGrading    State = "grading"
	StateQueryTier  State = "query_tier"
	StateGenerating State = "generating"
	StateFinalizing State = "finalizing"
	StateDone       State = "done"
	StateFailed     State = "failed"
	StateCancelled  State = "cancelled"
	StateSkipped    State = "skipped"
)

// Job home file names (spec section 7). The tmp prefix used for the
// atomic snapshot swap lives in the same directory so the rename stays
// within one filesystem.
const (
	snapshotFile = "job.json"
	eventsFile   = "events.jsonl"
	pidFileName  = "pid"
	tmpPrefix    = ".job.json.tmp."
)

// CodeInterrupted is the error code recorded when a job's owning
// process is found dead before the job reached a terminal state.
const CodeInterrupted = "INTERRUPTED"

// snapshotFreshWindow is how stale a snapshot must be before a dead-pid
// verdict is trusted on hosts without /proc (the macOS fallback for the
// pid-reuse check): a snapshot written within the last second is treated
// as still owned, which avoids racing a run that has barely started.
const snapshotFreshWindow = time.Second

// Job is the on-disk snapshot of a run (job.json). Pid is the process
// group id of the owning run; PidStart is the /proc start-time ticks
// recorded at the same moment, used to tolerate pid reuse when deciding
// whether the owner is still alive (0 where /proc is unavailable).
type Job struct {
	ID        string         `json:"id"`
	State     State          `json:"state"`
	Stage     string         `json:"stage"`
	Pid       int            `json:"pid"`
	PidStart  uint64         `json:"pid_start,omitempty"`
	StartedAt time.Time      `json:"started_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	Detail    map[string]any `json:"detail,omitempty"`

	// lastSeq is the sequence number of the last emitted event. It is
	// runtime state, not serialized: LoadStatus restores it from the log
	// so a job resumed from disk continues the sequence instead of
	// restarting at 1.
	lastSeq int
}

// IsTerminal reports whether state is one a job never leaves: done,
// failed, cancelled, or skipped.
func IsTerminal(state State) bool {
	switch state {
	case StateDone, StateFailed, StateCancelled, StateSkipped:
		return true
	default:
		return false
	}
}

// Create creates a new job directory under jobsHome and writes its
// initial pending snapshot. It returns the job and its home directory
// path (<jobsHome>/<id>). The id is "j_" followed by 12 hex characters.
func Create(jobsHome string) (*Job, string, error) {
	return createWith(jobsHome, func() (string, error) { return newJobID(rand.Read) }, (*Job).Snapshot)
}

// createWith is Create with the id generator and snapshot hook injected
// so tests can force collisions and failures deterministically.
func createWith(jobsHome string, newID func() (string, error), snap func(*Job, string) error) (*Job, string, error) {
	if err := os.MkdirAll(jobsHome, 0o755); err != nil {
		return nil, "", fmt.Errorf("create jobs home: %w", err)
	}
	for attempt := 0; attempt < 3; attempt++ {
		id, err := newID()
		if err != nil {
			return nil, "", fmt.Errorf("generate job id: %w", err)
		}
		dir := filepath.Join(jobsHome, id)
		if err := os.Mkdir(dir, 0o755); err != nil {
			if errors.Is(err, fs.ErrExist) {
				continue // random-id collision with a live job dir; try a new id
			}
			return nil, "", fmt.Errorf("create job dir: %w", err)
		}
		now := time.Now().UTC()
		j := &Job{ID: id, State: StatePending, StartedAt: now, UpdatedAt: now}
		if err := snap(j, dir); err != nil {
			return nil, "", fmt.Errorf("initial snapshot: %w", err)
		}
		return j, dir, nil
	}
	return nil, "", fmt.Errorf("job id collision in %s: giving up after 3 attempts", jobsHome)
}

// newJobID returns a fresh job id, "j_" followed by 12 hex characters
// (6 crypto-random bytes). The reader is injected so the entropy-failure
// path is testable.
func newJobID(read func([]byte) (int, error)) (string, error) {
	var buf [6]byte
	if _, err := read(buf[:]); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return "j_" + hex.EncodeToString(buf[:]), nil
}

// Snapshot writes the current job state to <dir>/job.json atomically:
// the JSON is written to a tmp file in the same directory, fsynced, and
// renamed over the old snapshot, so a crash mid-swap can never leave a
// partial job.json. UpdatedAt is restamped, the owning process id is
// recorded in the pid file, and on /proc hosts the process start time
// is recorded for the pid-reuse check of MarkInterruptedIfDead.
func (j *Job) Snapshot(dir string) error {
	j.UpdatedAt = time.Now().UTC()
	if j.Pid != 0 {
		if err := writePidFile(dir, j.Pid); err != nil {
			return err
		}
		if ticks, ok := readStartTicks(j.Pid); ok {
			j.PidStart = ticks
		} else {
			j.PidStart = 0
		}
	}
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	tmp, err := os.CreateTemp(dir, tmpPrefix)
	if err != nil {
		return fmt.Errorf("create snapshot tmp file: %w", err)
	}
	if err := writeSnapshot(tmp, append(data, '\n'), filepath.Join(dir, snapshotFile)); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}
	return nil
}

// writePidFile records the owning process (group) id in <dir>/pid. The
// fsynced job.json carries the same id; this file exists for humans and
// tooling per the job home layout, so it is a plain write.
func writePidFile(dir string, pid int) error {
	if err := os.WriteFile(filepath.Join(dir, pidFileName), []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	return nil
}

// LoadStatus reads a job directory: it returns the job.json snapshot
// and the events.jsonl log, and restores the job's event sequence
// position from the log. LoadStatus itself is read-only; the read paths
// that must reflect crash semantics call MarkInterruptedIfDead on the
// returned job and Snapshot the result (spec section 7: on any jobs
// read, a dead-pid non-terminal job is marked failed/INTERRUPTED).
func LoadStatus(dir string) (*Job, []Event, error) {
	data, err := os.ReadFile(filepath.Join(dir, snapshotFile))
	if err != nil {
		return nil, nil, fmt.Errorf("read snapshot: %w", err)
	}
	j := &Job{}
	if err := json.Unmarshal(data, j); err != nil {
		return nil, nil, fmt.Errorf("parse snapshot: %w", err)
	}
	events, err := readEvents(filepath.Join(dir, eventsFile))
	if err != nil {
		return nil, nil, err
	}
	if n := len(events); n > 0 {
		j.lastSeq = events[n-1].Seq
	}
	return j, events, nil
}

// MarkInterruptedIfDead applies the crash rule of spec section 7: if
// the job is non-terminal and its recorded owning process is dead, the
// job is marked failed with error code INTERRUPTED (recorded in
// Detail["error"], retryable — recovery is to run again) and true is
// returned. Terminal jobs and jobs without a recorded pid are left
// untouched. The pid-reuse check compares the /proc start time recorded
// in the snapshot against the current one; without /proc (macOS) a dead
// verdict from the signal probe is trusted only for snapshots older
// than snapshotFreshWindow.
func MarkInterruptedIfDead(j *Job) bool {
	if IsTerminal(j.State) || j.Pid <= 0 {
		return false
	}
	if ownerAlive(j, readStartTicks) {
		return false
	}
	j.State = StateFailed
	if j.Detail == nil {
		j.Detail = make(map[string]any)
	}
	j.Detail["error"] = EventError{
		Code:      CodeInterrupted,
		Message:   "owning process died before the job reached a terminal state; run again to recover",
		Retryable: true,
	}
	return true
}

// ownerAlive reports whether the process recorded in the snapshot still
// owns the job, with startTicks injected for tests. On /proc hosts the
// verdict is exact: the pid's start-time ticks must match the recorded
// ones, so a reused pid counts as dead and a pid with no recorded
// baseline counts as alive. Without /proc the start-time check is
// impossible, so the decision defers to the signal probe and a dead
// verdict is trusted only when the snapshot is stale.
func ownerAlive(j *Job, startTicks func(pid int) (uint64, bool)) bool {
	if current, ok := startTicks(j.Pid); ok {
		return j.PidStart == 0 || j.PidStart == current
	}
	if time.Since(j.UpdatedAt) < snapshotFreshWindow {
		return true
	}
	return signalAlive(j.Pid)
}

// signalAlive probes pid with signal 0: a permission check only, no
// signal is delivered. EPERM means the process exists but belongs to
// another user, i.e. it is alive; ESRCH means no such process.
func signalAlive(pid int) bool {
	err := syscall.Kill(pid, syscall.Signal(0))
	return err == nil || !errors.Is(err, syscall.ESRCH)
}

// readStartTicks reads the start-time ticks of pid from
// /proc/<pid>/stat. ok is false on hosts without /proc (macOS) and for
// pids that no longer exist there.
func readStartTicks(pid int) (uint64, bool) {
	return startTicksFromFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
}

// startTicksFromFile parses the stat file at path and returns its
// starttime field, or ok=false when the file cannot be read or parsed.
func startTicksFromFile(path string) (uint64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	ticks, err := parseStatStartTicks(string(data))
	if err != nil {
		return 0, false
	}
	return ticks, true
}

// parseStatStartTicks extracts field 22 (starttime, clock ticks since
// boot) from a /proc/<pid>/stat line. The comm field (field 2) is
// parenthesized and may itself contain parentheses or spaces, so
// parsing starts after the last ')' in the line.
func parseStatStartTicks(stat string) (uint64, error) {
	end := strings.LastIndex(stat, ")")
	if end < 0 {
		return 0, fmt.Errorf("parse /proc stat: no comm field in %q", stat)
	}
	fields := strings.Fields(stat[end+1:])
	// fields[0] is field 3 (state); field 22 is fields[22-3].
	const starttime = 22 - 3
	if len(fields) <= starttime {
		return 0, fmt.Errorf("parse /proc stat: got %d fields after comm, want at least %d", len(fields), starttime+1)
	}
	ticks, err := strconv.ParseUint(fields[starttime], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse /proc stat starttime: %w", err)
	}
	return ticks, nil
}
