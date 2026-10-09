package update

import (
	"bytes"
	"os"
	"path/filepath"
)

// LauncherPath is the member's launcher, which only an engine update PR
// and cn init write.
const LauncherPath = ".claudinite/launch"

// EnvSetupScript is the line a Claude Code web environment's Setup script
// holds. The script starts above the checkout, one checkout or several, so
// it pre-warms the engine cache in each checkout holding a launcher and
// never fails the session.
const EnvSetupScript = `for d in . *; do [ -f "$d/.claudinite/launch" ] && (cd "$d" && sh .claudinite/launch env install); done; true`

// writeLauncher replaces the member's launcher with the launcher of got,
// the engine the update moves to, and reports whether it changed. A
// release whose signed manifest hashes no launcher, or a repo holding
// none, is left alone.
func writeLauncher(repo string, got Fetched) (bool, error) {
	if !got.SignedLauncher {
		return false, nil
	}
	shipped := got.Launcher
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
