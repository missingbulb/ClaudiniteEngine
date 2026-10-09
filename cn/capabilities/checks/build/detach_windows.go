//go:build windows

package build

import "syscall"

// Detached is CREATE_NEW_PROCESS_GROUP, so a child outlives the hook's console.
func Detached() *syscall.SysProcAttr { return &syscall.SysProcAttr{CreationFlags: 0x00000200} }
