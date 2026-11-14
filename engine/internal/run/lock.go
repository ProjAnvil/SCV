// Per-repo mutual exclusion (spec section 10): one run per analysis
// dir, enforced by an exclusive flock on <analysisDir>/.scv-lock. A
// second run on the same analysis dir fails fast with the typed
// WORKFLOW_LOCKED error; --output-dir is the deliberate escape hatch
// for parallel re-analysis.
package run

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/projanvil/scv/engine/internal/job"
)

// CodeWorkflowLocked is the wire error code of a second run refused by
// the per-analysis-dir lock (spec section 10).
const CodeWorkflowLocked = "WORKFLOW_LOCKED"

// ErrWorkflowLocked is the sentinel behind CodeWorkflowLocked; the CLI
// maps it with errors.Is.
var ErrWorkflowLocked = errors.New(CodeWorkflowLocked)

// repoLockFile is the per-analysis-dir lock file name (spec section 10).
const repoLockFile = ".scv-lock"

// AcquireRepoLock enforces per-repo mutual exclusion (spec section 10):
// an exclusive non-blocking flock on <analysisDir>/.scv-lock, held
// until the returned release func runs. A second acquirer — including
// another run in the same process — fails fast with the typed
// WORKFLOW_LOCKED error.
func AcquireRepoLock(analysisDir string) (release func(), err error) {
	if err := os.MkdirAll(analysisDir, 0o755); err != nil {
		return nil, fmt.Errorf("create analysis dir: %w", err)
	}
	path := filepath.Join(analysisDir, repoLockFile)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := lockFileExclusive(f); err != nil {
		_ = f.Close()
		if isWouldBlock(err) {
			return nil, fmt.Errorf("%w: another run holds %s", ErrWorkflowLocked, path)
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return func() {
		// Unchecked errors: the lock is best released on close whatever
		// the unlock reports, and a failed close cannot un-hold it.
		_ = unlockFile(f)
		_ = f.Close()
	}, nil
}

// lockFailureEvent maps an AcquireRepoLock failure to its
// preparing-stage event error: the typed WORKFLOW_LOCKED refusal, or
// the preparing stage's generic retryable code for lock infrastructure
// errors (no separate wire spelling exists for those).
func lockFailureEvent(analysisDir string, err error) job.EventError {
	if errors.Is(err, ErrWorkflowLocked) {
		return job.EventError{
			Code:      CodeWorkflowLocked,
			Message:   fmt.Sprintf("another run is already analyzing %s", analysisDir),
			Retryable: true, // a re-run after the holder finishes recovers
		}
	}
	return job.EventError{
		Code:      CodeCloneFailed,
		Message:   fmt.Sprintf("lock analysis dir: %v", err),
		Retryable: true,
	}
}
