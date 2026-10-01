//go:build !windows

package paths

import (
	"os"
	"syscall"
)

func ownedByCurrentUser(st os.FileInfo) bool {
	s, ok := st.Sys().(*syscall.Stat_t)
	return ok && int(s.Uid) == os.Getuid()
}
