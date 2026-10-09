package verify

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildInto builds linux-x64 cn exactly as a release does into dir/bin/linux-x64,
// with extra ldflags, and returns dir/bin.
func buildInto(t *testing.T, extra string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "bin")
	out := filepath.Join(bin, "linux-x64", "cn")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "dev/build/gobuild.sh", "linux-x64", out)
	cmd.Dir = "../../.."
	cmd.Env = append(os.Environ(), "VERSION=1.61001.1", "EXTRA_LDFLAGS="+extra)
	if o, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, o)
	}
	return bin
}

func scan(t *testing.T, dir string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", "dev/release/verify/secretscan.sh", dir)
	cmd.Dir = "../../.."
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestSecretScan(t *testing.T) {
	t.Parallel()
	devKey, err := os.ReadFile("../keys/testkeys/release.key")
	if err != nil {
		t.Fatal(err)
	}
	plant := "-X main.secretScanPlant="
	cases := map[string]string{
		"github token":    plant + "ghp_" + strings.Repeat("A1b2", 9),
		"npm token":       plant + "npm_" + strings.Repeat("Zz09", 9),
		"aws key":         plant + "AKIAABCDEFGHIJKLMNOP",
		"dev private key": plant + strings.TrimSpace(string(devKey)),
		// encoding/pem's own "-----BEGIN " is in every binary that speaks
		// TLS; a private key block is not.
		"pem private key": "-X 'main.secretScanPlant=-----BEGIN EC PRIVATE KEY-----'",
	}
	built := map[string]string{}
	for name, flags := range cases {
		built[name] = buildInto(t, flags)
		out, err := scan(t, built[name])
		if err == nil {
			t.Errorf("%s: scan passed a planted secret\n%s", name, out)
		}
	}
	spaced := filepath.Join(t.TempDir(), "a folder with spaces", "bin")
	if err := os.CopyFS(spaced, os.DirFS(built["npm token"])); err != nil {
		t.Fatal(err)
	}
	if out, err := scan(t, spaced); err == nil || !strings.Contains(out, "a folder with spaces/bin/linux-x64/cn carries a string matching npm_") {
		t.Errorf("scan passed a planted secret under a path with spaces\n%s", out)
	}
	if out, err := scan(t, buildInto(t, "")); err != nil {
		t.Fatalf("clean build fails the scan: %v\n%s", err, out)
	}
}
