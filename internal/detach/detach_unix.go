//go:build !windows

package detach

import (
	"os/exec"
	"syscall"
)

// Start puts the agent in its own session: no controlling terminal,
// and signals sent to the Claude session's process group don't reach it.
func Start(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}
