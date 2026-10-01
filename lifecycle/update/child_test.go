package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func script(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cn")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSelftest(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghs_secret")
	ok := script(t, `[ "$1" = selftest ] || exit 9
[ -z "$GITHUB_TOKEN" ] || { echo "token leaked"; exit 1; }
echo "version 60930.2.0"; echo "platform linux-x64"`)
	if out, err := Selftest(ok, "60930.2.0", 5*time.Second); err != nil || !strings.Contains(out, "platform") {
		t.Fatalf("%q %v", out, err)
	}
	if _, err := Selftest(ok, "60930.3.0", 5*time.Second); err == nil || !strings.Contains(err.Error(), "60930.3.0") {
		t.Errorf("wrong version accepted: %v", err)
	}
	fails := script(t, `echo "version 60930.2.0"; echo "cache not writable"; exit 1`)
	if _, err := Selftest(fails, "60930.2.0", 5*time.Second); err == nil || !strings.Contains(err.Error(), "not writable") {
		t.Errorf("failing selftest accepted: %v", err)
	}
	hangs := script(t, `sleep 30`)
	start := time.Now()
	if _, err := Selftest(hangs, "60930.2.0", 300*time.Millisecond); err == nil || time.Since(start) > 10*time.Second {
		t.Errorf("hanging selftest: %v after %v", err, time.Since(start))
	}
}

func TestRunVerify(t *testing.T) {
	repo := t.TempDir()
	clean := script(t, `[ "$1 $2 $3" = "verify --repo `+repo+`" ] || exit 9; exit 0`)
	if out, breaks, err := RunVerify(clean, repo, 5*time.Second); err != nil || breaks || out != "" {
		t.Errorf("clean: %q %v %v", out, breaks, err)
	}
	broken := script(t, `echo "break rehearsal .: deliberately broken"; exit 1`)
	if out, breaks, err := RunVerify(broken, repo, 5*time.Second); err != nil || !breaks || !strings.Contains(out, "break rehearsal") {
		t.Errorf("broken: %q %v %v", out, breaks, err)
	}
	crashed := script(t, `echo boom >&2; exit 3`)
	if _, _, err := RunVerify(crashed, repo, 5*time.Second); err == nil {
		t.Error("an exit 3 read as a verdict")
	}
}
