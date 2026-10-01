//go:build windows

package paths

import "os"

func ownedByCurrentUser(os.FileInfo) bool { return true }
