// External run cancellation (spec section 10): the engine leads its
// own process group (recorded on the job at Run start), and Cancel
// terminates that group — SIGTERM first, SIGKILL after a bounded grace
// — before moving a still non-terminal job to cancelled.
package run

import (
	"errors"
	"fmt"
	"time"

	"github.com/projanvil/scv/engine/internal/job"
)

// CodeWorkflowNotCancellable is the refusal code of cancelling a job
// that already ended (spec section 10).
const CodeWorkflowNotCancellable = "WORKFLOW_NOT_CANCELLABLE"

// ErrWorkflowNotCancellable is the sentinel behind
// CodeWorkflowNotCancellable; the CLI maps it with errors.Is.
var ErrWorkflowNotCancellable = errors.New(CodeWorkflowNotCancellable)

// cancelGracePeriod bounds the wait between the SIGTERM and the SIGKILL
// of a cancelled run's process group (spec section 10: at most 10s). It
// is a package variable so tests can shorten the window.
var cancelGracePeriod = 10 * time.Second

// cancelPollInterval is how often Cancel polls the group and the job
// state file while waiting out its grace period.
var cancelPollInterval = 50 * time.Millisecond

// SetProcessGroup moves the calling engine process into its own process
// group (pgid == own pid, spec section 10) so a `jobs cancel` can
// address the whole run tree — the engine and every perceive/docgen
// descendant — with one kill(-pgid). Run calls it once at start, before
// the job pid is recorded. The call is best effort: a process that
// cannot lead a group (a session leader) keeps its inherited group, and
// external cancel then degrades to signaling what shares that group.
func SetProcessGroup() {
	// Best effort by contract: the common failure (already a session
	// leader) leaves the engine running fine, only group-wide cancel
	// degrades, and the recorded pid still addresses the engine itself.
	_ = setpgidSelf()
}

// Cancel terminates the run that owns job j recorded in the job
// directory dir (spec section 10): SIGTERM to the recorded process
// group, a bounded wait for a graceful exit (the state file flipping
// terminal or the group dying), then SIGKILL; once the group is gone a
// still non-terminal job is moved to cancelled with the T6 cancel
// shape — stage_failed CANCELLED on the open stage, then the cancelled
// job_result. Cancelling a terminal job is refused with the typed
// WORKFLOW_NOT_CANCELLABLE error and no side effects.
func Cancel(dir string, j *job.Job) error {
	if job.IsTerminal(j.State) {
		return fmt.Errorf("%w: job %s already ended as %s",
			ErrWorkflowNotCancellable, j.ID, j.State)
	}
	if j.Pid <= 0 {
		return fmt.Errorf("%w: job %s records no owning process group",
			ErrWorkflowNotCancellable, j.ID)
	}
	pgid := j.Pid

	// Graceful phase: TERM the group, then wait for the run to end on
	// its own terms — its state file flips terminal (it wrote its own
	// result) or the group empties.
	_ = termGroup(pgid)
	ended := waitUntil(time.Now().Add(cancelGracePeriod), func() bool {
		return groupGone(pgid) || snapshotTerminal(dir)
	})
	if !ended && !groupGone(pgid) {
		// Backstop: the group ignored (or never saw) the TERM.
		_ = killGroupHard(pgid)
		_ = waitUntil(time.Now().Add(cancelGracePeriod), func() bool { return groupGone(pgid) })
	}

	fresh, _, err := job.LoadStatus(dir)
	if err != nil {
		return fmt.Errorf("cancel job %s: reload state: %w", j.ID, err)
	}
	if job.IsTerminal(fresh.State) {
		// The run recorded its own ending (cancel lost the race to a
		// finishing run); that result stands.
		return nil
	}
	return markCancelled(dir, fresh, pgid)
}

// waitUntil polls cond every cancelPollInterval until it holds or the
// deadline passes; it reports whether cond held.
func waitUntil(deadline time.Time, cond func() bool) bool {
	for !cond() {
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(cancelPollInterval)
	}
	return true
}

// snapshotTerminal reports whether the job's persisted state is
// terminal — a cancelled run having written its own result.
func snapshotTerminal(dir string) bool {
	j, _, err := job.LoadStatus(dir)
	return err == nil && job.IsTerminal(j.State)
}

// markCancelled moves a dead-but-non-terminal job to cancelled: the T6
// cancel shape (stage_failed CANCELLED on the open stage, then the
// cancelled job_result), made durable by Snapshot like every state
// transition.
func markCancelled(dir string, j *job.Job, pgid int) error {
	if j.Stage != "" { // a stage was entered but never finished
		if _, err := j.Emit(dir, job.EventStageFailed, j.Stage, nil, &job.EventError{
			Code:      CodeCancelled,
			Message:   "cancelled before the stage finished",
			Retryable: true,
		}); err != nil {
			return fmt.Errorf("cancel job %s: emit stage_failed: %w", j.ID, err)
		}
	}
	resultErr := &job.EventError{
		Code:      CodeCancelled,
		Message:   fmt.Sprintf("cancelled by signal to process group %d", pgid),
		Retryable: true,
	}
	j.State, j.Stage = job.StateCancelled, string(job.StateCancelled)
	j.Detail = map[string]any{"error": *resultErr}
	if err := j.Snapshot(dir); err != nil {
		return fmt.Errorf("cancel job %s: snapshot cancelled: %w", j.ID, err)
	}
	if _, err := j.Emit(dir, job.EventJobResult, string(job.StateCancelled), nil, resultErr); err != nil {
		return fmt.Errorf("cancel job %s: emit job_result: %w", j.ID, err)
	}
	return nil
}
