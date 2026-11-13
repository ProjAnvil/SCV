//go:build !unix

package run

import (
	"errors"
	"os"
	"os/exec"
)

// Non-unix fallbacks: SCV targets unix hosts (spec section 12); these
// stubs keep the package portable, the same split docgen uses. Locking
// surfaces an error and process-group operations are no-ops.
func setpgidSelf() error { return errors.New("setpgid: unsupported platform") }

func termGroup(int) error { return errors.New("kill: unsupported platform") }

func killGroupHard(int) error { return errors.New("kill: unsupported platform") }

func groupGone(int) bool { return true }

func lockFileExclusive(*os.File) error { return errors.New("flock: unsupported platform") }

func unlockFile(*os.File) error { return nil }

func isWouldBlock(error) bool { return false }

func setChildProcessGroup(*exec.Cmd) {}
