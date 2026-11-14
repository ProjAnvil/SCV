//go:build unix

package run

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// The unix process-control primitives behind spec section 10 (locking,
// process groups, cancellation). They live in build-tagged files — the
// same split docgen uses — so the package stays portable; the !unix
// stubs degrade to errors/no-ops.

// setpgidSelf moves the calling process into its own process group
// (pgid == own pid).
func setpgidSelf() error { return syscall.Setpgid(0, 0) }

// termGroup sends SIGTERM to every process in group pgid.
func termGroup(pgid int) error { return syscall.Kill(-pgid, syscall.SIGTERM) }

// killGroupHard sends SIGKILL to every process in group pgid.
func killGroupHard(pgid int) error { return syscall.Kill(-pgid, syscall.SIGKILL) }

// groupGone reports whether no process remains in group pgid, probed
// with signal 0 (a delivery check only). EPERM — a member owned by
// another user — counts as present; only ESRCH means empty.
func groupGone(pgid int) bool {
	return errors.Is(syscall.Kill(-pgid, syscall.Signal(0)), syscall.ESRCH)
}

// lockFileExclusive takes a non-blocking exclusive flock on f.
func lockFileExclusive(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

// unlockFile releases the flock held by f.
func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}

// isWouldBlock reports whether err is the flock contention errno.
func isWouldBlock(err error) bool { return errors.Is(err, syscall.EWOULDBLOCK) }

// setChildProcessGroup puts the command in its own process group and
// replaces CommandContext's default child-only kill with a group-wide
// SIGKILL, so a cancelled run reaches grandchildren too — the pattern
// proven by docgen.setProcessGroup (spec section 10). Cancel must be
// set before Start; it returns nil so a kill race with an already-dead
// group never fabricates a Wait error.
func setChildProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			_ = killGroupHard(cmd.Process.Pid)
		}
		return nil
	}
}
