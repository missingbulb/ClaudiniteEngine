//go:build !windows

package build

import "syscall"

// Detached starts a child in its own session, so it outlives the hook.
func Detached() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }
