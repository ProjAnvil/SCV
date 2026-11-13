//go:build unix

package docgen

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts the command in its own process group and replaces
// CommandContext's default child-only kill with a group-wide SIGKILL, so
// a timeout reaches grandchildren too (spec section 8: kill process
// group). Cancel must be set before Start; it returns nil so a kill race
// with an already-dead group never fabricates a Wait error.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
}
