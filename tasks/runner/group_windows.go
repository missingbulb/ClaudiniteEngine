//go:build windows

package runner

import (
	"os/exec"
	"syscall"
)

// groupAttr starts the child in a new process group.
func groupAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{CreationFlags: 0x00000200} }

func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
