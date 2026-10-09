//go:build !windows

package build

import (
	"errors"
	"syscall"
)

// alive says process pid exists; one owned by another user still does.
func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
