package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runScript runs a release script from the repo root with extra environment.
func runScript(t *testing.T, env []string, script string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", append([]string{script}, args...)...)
	cmd.Dir = ".."
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// With no development keys on disk, signing needs both key paths named.
func TestBuildNeedsExplicitKeysWithoutDevKeys(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "no-dev-keys")
	dist := filepath.Join(t.TempDir(), "dist")
	out, err := runScript(t, []string{"DEV_KEYS=" + gone, "RELEASE_KEY=", "RELEASE_CERT=", "DIST=" + dist}, "release/build.sh")
	if err == nil {
		t.Fatalf("build.sh signed with no keys\n%s", out)
	}
	if !strings.Contains(out, "RELEASE_KEY") || !strings.Contains(out, "RELEASE_CERT") {
		t.Errorf("message does not name RELEASE_KEY and RELEASE_CERT:\n%s", out)
	}

	root, _ := filepath.Abs("..")
	out, err = runScript(t, []string{
		"DEV_KEYS=" + gone, "DIST=" + dist, "VERSION=1.1.0",
		"RELEASE_KEY=" + filepath.Join(root, "keys/dev/release.key"),
		"RELEASE_CERT=" + filepath.Join(root, "keys/dev/release.cert.json"),
	}, "release/build.sh")
	if err != nil {
		t.Fatalf("build.sh with explicit keys: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dist, "manifest.sig.json")); err != nil {
		t.Errorf("no manifest.sig.json: %v", err)
	}
}
