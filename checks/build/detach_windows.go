//go:build windows

package build

import "syscall"

// CREATE_NEW_PROCESS_GROUP, so the build outlives the hook's console.
func detached() *syscall.SysProcAttr { return &syscall.SysProcAttr{CreationFlags: 0x00000200} }
