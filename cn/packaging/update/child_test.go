package update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func script(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cn")
	writeExecutable(t, p, "#!/bin/sh\n"+body)
	return p
}

// writeExecutable holds off forks while the file is open, since the tests run
// in parallel and a child forked meanwhile makes exec fail with "text file busy".
func writeExecutable(t *testing.T, p, body string) {
	t.Helper()
	syscall.ForkLock.Lock()
	err := os.WriteFile(p, []byte(body), 0o755)
	syscall.ForkLock.Unlock()
	if err != nil {
		t.Fatal(err)
	}
}

func TestSelftest(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghs_secret")
	repo := t.TempDir()
	ok := script(t, `[ "$*" = "selftest --repo `+repo+`" ] || [ "$*" = selftest ] || exit 9
[ -z "$GITHUB_TOKEN" ] || { echo "token leaked"; exit 1; }
echo "version 1.60930.2"; echo "ok binary: linux-x64"`)
	for _, r := range []string{repo, ""} {
		if out, err := Selftest(ok, "1.60930.2", r, 5*time.Second); err != nil || !strings.Contains(out, "ok binary") {
			t.Fatalf("repo %q: %q %v", r, out, err)
		}
	}
	if _, err := Selftest(ok, "1.60930.3", repo, 5*time.Second); err == nil || !strings.Contains(err.Error(), "1.60930.3") {
		t.Errorf("wrong version accepted: %v", err)
	}
	probes := script(t, `echo "version 1.60930.2"; echo "fail hooks: PreToolUse"; echo "ok rules: x"; echo "fail rules: y"; exit 1`)
	var failed *SelftestFailed
	if _, err := Selftest(probes, "1.60930.2", repo, 5*time.Second); !errors.As(err, &failed) || strings.Join(failed.Probes, ",") != "hooks,rules" {
		t.Errorf("failed probes: %v", err)
	}
	crashed := script(t, `echo "version 1.60930.2"; echo boom >&2; exit 3`)
	if _, err := Selftest(crashed, "1.60930.2", repo, 5*time.Second); err == nil || errors.As(err, &failed) || !strings.Contains(err.Error(), "boom") {
		t.Errorf("a crash read as failed probes: %v", err)
	}
	hangs := script(t, `sleep 30`)
	start := time.Now()
	if _, err := Selftest(hangs, "1.60930.2", repo, 300*time.Millisecond); err == nil || time.Since(start) > 10*time.Second {
		t.Errorf("hanging selftest: %v after %v", err, time.Since(start))
	}
}

func TestRunVerify(t *testing.T) {
	t.Parallel()
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
