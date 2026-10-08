package update

import (
	"bytes"
	"os"
	"path/filepath"
)

// LauncherPath is the member's launcher, which only an engine update PR
// and cn init write.
const LauncherPath = ".claudinite/launch"

// writeLauncher replaces the member's launcher with shipped, the launcher
// of the engine the update moves to, and reports whether it changed. A
// release that ships none, or a repo holding none, is left alone.
func writeLauncher(repo string, shipped []byte) (bool, error) {
	if len(shipped) == 0 {
		return false, nil
	}
	p := filepath.Join(repo, filepath.FromSlash(LauncherPath))
	have, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if bytes.Equal(have, shipped) {
		return false, nil
	}
	if err := os.WriteFile(p, shipped, 0o755); err != nil {
		return false, err
	}
	return true, nil
}
