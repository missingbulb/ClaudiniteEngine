package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildCN builds the real binary with extra ldflags and returns its path.
func buildCN(t *testing.T, ldflags string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "cn")
	cmd := exec.Command("go", "build", "-ldflags", ldflags, "-o", bin, ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

func runCN(t *testing.T, bin string, env []string, stdin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return out.String(), errb.String(), code
}

func TestVersionPrintsInjectedVersion(t *testing.T) {
	bin := buildCN(t, "-X github.com/missingbulb/ClaudiniteEngine/shared/version.version=60928.3.0 -X github.com/missingbulb/ClaudiniteEngine/shared/version.commit=abc1234")
	out, _, code := runCN(t, bin, nil, "", "version")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if lines[0] != "60928.3.0" {
		t.Fatalf("first line %q", lines[0])
	}
	if len(lines) < 2 || !strings.Contains(lines[1], "abc1234") {
		t.Fatalf("second line must carry the commit: %q", out)
	}
}

func TestUnknownSubcommandExitsUsage(t *testing.T) {
	out, errOut, code := runInProc([]string{"frobnicate"}, "")
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if out != "" {
		t.Fatalf("stdout must stay empty, got %q", out)
	}
	if !strings.Contains(errOut, "usage:") {
		t.Fatalf("stderr lacks usage: %q", errOut)
	}
}

func TestNoSubcommandExitsUsage(t *testing.T) {
	_, errOut, code := runInProc(nil, "")
	if code != 2 || !strings.Contains(errOut, "usage:") {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
}

func runInProc(args []string, stdin string) (string, string, int) {
	var out, errb bytes.Buffer
	code := run(args, strings.NewReader(stdin), &out, &errb)
	return out.String(), errb.String(), code
}
