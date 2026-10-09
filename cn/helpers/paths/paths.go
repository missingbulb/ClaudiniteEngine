// Package paths resolves the engine's cache folder, the one place outside a
// member repo it writes, and keeps it private: 0700 and owned by the user,
// with files placed atomically.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
)

// CacheRoot is ${XDG_CACHE_HOME:-$HOME/.cache}/claudinite, the same folder
// the launcher uses.
func CacheRoot() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home := os.Getenv("HOME")
		if home == "" {
			home, _ = os.UserHomeDir()
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "claudinite")
}

// EnsurePrivateDir creates dir 0700 if missing, then refuses it unless it is
// a directory with mode 0700 owned by the current user.
func EnsurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return CheckPrivateDir(dir)
}

// CheckPrivateDir refuses dir unless it is a 0700 directory the current
// user owns. Windows has no POSIX modes; there only the type is checked.
func CheckPrivateDir(dir string) error {
	st, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	if st.Mode().Perm() != 0o700 {
		return fmt.Errorf("%s has mode %04o; the cache must be 0700", dir, st.Mode().Perm())
	}
	if !ownedByCurrentUser(st) {
		return fmt.Errorf("%s is not owned by the current user", dir)
	}
	return nil
}

// PlaceReadOnly writes data to dir/name with mode by rename from a temporary
// file in the same folder, so readers see the old file or the new, never a
// partial one. No fork happens while the file is open for writing: a child
// forked then would hold it open until its exec, and running the placed
// binary would fail with "text file busy".
func PlaceReadOnly(dir, name string, data []byte, mode os.FileMode) error {
	syscall.ForkLock.Lock()
	tmp, err := writeTemp(dir, name, data)
	syscall.ForkLock.Unlock()
	if tmp != "" {
		defer func() { _ = os.Remove(tmp) }()
	}
	if err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, name))
}

func writeTemp(dir, name string, data []byte) (string, error) {
	f, err := os.CreateTemp(dir, "."+name+".tmp-*")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return f.Name(), err
	}
	return f.Name(), f.Close()
}
