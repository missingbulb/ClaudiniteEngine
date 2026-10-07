//go:build !windows

package runner

import (
	"os/exec"
	"syscall"
)

// groupAttr starts the child as the leader of its own process group, so
// the kill reaches everything it spawned: under the shell the direct
// child is sh, and the worker is its child.
func groupAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }

func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
