//go:build windows

package detach

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// Start gives the agent no console and its own process group, and
// tries to leave the parent's job object so it isn't killed with the Claude
// session. A job that forbids breakaway makes CreateProcess fail, so retry
// without it (the agent then lives only as long as that job).
func Start(cmd *exec.Cmd) error {
	flags := uint32(windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: flags | windows.CREATE_BREAKAWAY_FROM_JOB}
	if err := cmd.Start(); err == nil {
		return nil
	}
	retry := exec.Command(cmd.Path, cmd.Args[1:]...)
	retry.Dir, retry.Env, retry.Stdout, retry.Stderr = cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr
	retry.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: flags}
	*cmd = *retry
	return cmd.Start()
}
