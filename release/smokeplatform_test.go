package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

// serveDist starts regstub over the given dist folders and returns its URL
// and CA file.
func serveDist(t *testing.T, dists ...string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	stub := filepath.Join(dir, "regstub")
	build := exec.Command("go", "build", "-o", stub, "./release/regstub")
	build.Dir = ".."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build regstub: %v\n%s", err, out)
	}
	ready, ca := filepath.Join(dir, "ready"), filepath.Join(dir, "ca.pem")
	args := []string{"--ready", ready, "--ca-out", ca}
	for _, d := range dists {
		args = append(args, "--dist", d)
	}
	cmd := exec.Command(stub, args...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if raw, err := os.ReadFile(ready); err == nil {
			return strings.TrimSpace(string(raw)), ca
		}
		if time.Now().After(deadline) {
			t.Fatal("regstub did not start")
		}
	}
}

func integrityOf(t *testing.T, dist string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dist, "manifest.integrity"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(raw))
}

func TestSmokePlatformRunsTheHostLeg(t *testing.T) {
	if version.Platform() != "linux-x64" {
		t.Skip("the fixture dist is built on linux-x64")
	}
	dist, _ := unsignedDist(t)
	url, ca := serveDist(t, dist)
	out, err := runScript(t, []string{"CURL_CA_BUNDLE=" + ca, "NO_PROXY=127.0.0.1,localhost"}, "release/smoke-platform.sh",
		"--registry", url, "--channel", "canary", "--version", "1.61001.1", "--pin", integrityOf(t, dist), "--dist", dist)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"ok binary: linux-x64", "ok hooks: ", "version 1.61001.1", "pre-tool-use", "pin change refused"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	out, err = runScript(t, []string{"CURL_CA_BUNDLE=" + ca, "NO_PROXY=127.0.0.1,localhost"}, "release/smoke-platform.sh",
		"--registry", url, "--channel", "canary", "--version", "1.61001.1", "--pin", integrityOf(t, dist), "--dist", dist, "--keep")
	if err != nil {
		t.Fatalf("--keep: %v\n%s", err, out)
	}
	var kept string
	for _, l := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(l, "smoke-platform: kept "); ok {
			kept = v
		}
	}
	if kept == "" {
		t.Fatalf("--keep printed no folder:\n%s", out)
	}
	defer func() { _ = exec.Command("chmod", "-R", "u+w", kept).Run(); _ = os.RemoveAll(kept) }()
	if _, err := os.Stat(filepath.Join(kept, "member", ".claudinite", "launch")); err != nil {
		t.Errorf("kept folder has no member: %v", err)
	}
}

func TestSmokePlatformRefusesAMissingBinary(t *testing.T) {
	dist, _ := unsignedDist(t)
	if err := os.RemoveAll(filepath.Join(dist, "bin", "darwin-arm64")); err != nil {
		t.Fatal(err)
	}
	out, err := runScript(t, nil, "release/smoke-platform.sh",
		"--registry", "https://127.0.0.1:9", "--channel", "canary", "--version", "1.61001.1", "--pin", integrityOf(t, dist), "--dist", dist, "--platform", "darwin-arm64")
	if err == nil || !strings.Contains(out, "darwin-arm64") || !strings.Contains(out, "no ") {
		t.Fatalf("err %v\n%s", err, out)
	}
}
