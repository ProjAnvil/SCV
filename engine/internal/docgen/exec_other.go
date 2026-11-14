//go:build !unix

package docgen

import "os/exec"

// setProcessGroup is the non-unix fallback: no separate process group,
// so CommandContext's default kill reaches only the direct child. SCV
// targets unix hosts; this stub keeps the package portable.
func setProcessGroup(cmd *exec.Cmd) {}
