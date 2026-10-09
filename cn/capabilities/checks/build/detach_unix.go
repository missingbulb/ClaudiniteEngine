//go:build !windows

package build

import (
	"errors"
	"syscall"
)

// Detached starts a child in its own session, so it outlives the hook.
func Detached() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }

// alive says process pid exists; one owned by another user still does.
func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
