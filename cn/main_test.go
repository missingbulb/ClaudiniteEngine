package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/checksdk"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/version"
)

var buildRoot string

func TestMain(m *testing.M) {
	var err error
	if buildRoot, err = os.MkdirTemp("", "cn-test-builds-"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// Tests point XDG_CACHE_HOME at a temporary folder, which would move the Go build cache
	// there too and make every checks build start cold.
	if err := pinGoCache(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(buildRoot)
	os.Exit(code)
}

var (
	buildsMu sync.Mutex
	builds   = map[string]*cnBuild{}
)

type cnBuild struct {
	once sync.Once
	bin  string
	err  error
}

// buildCN builds the real binary with extra ldflags, once per ldflags for the
// whole package, and returns its path; tests only run it.
func buildCN(t *testing.T, ldflags string) string {
	t.Helper()
	buildsMu.Lock()
	b := builds[ldflags]
	if b == nil {
		b = &cnBuild{}
		builds[ldflags] = b
	}
	buildsMu.Unlock()
	b.once.Do(func() {
		dir, err := os.MkdirTemp(buildRoot, "cn-")
		if err != nil {
			b.err = err
			return
		}
		b.bin = filepath.Join(dir, "cn")
		cmd := exec.Command("go", "build", "-ldflags", ldflags, "-o", b.bin, ".")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			b.err = fmt.Errorf("build: %v\n%s", err, out)
		}
	})
	if b.err != nil {
		t.Fatal(b.err)
	}
	return b.bin
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
	t.Parallel()
	bin := buildCN(t, "-X github.com/missingbulb/ClaudiniteEngine/cn/shared/version.version=1.60928.3 -X github.com/missingbulb/ClaudiniteEngine/cn/shared/version.commit=abc1234")
	out, _, code := runCN(t, bin, nil, "", "version")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if lines[0] != "1.60928.3" {
		t.Fatalf("first line %q", lines[0])
	}
	if len(lines) < 2 || !strings.Contains(lines[1], "abc1234") {
		t.Fatalf("second line must carry the commit: %q", out)
	}
}

func TestVersionDayPrintsTodaysDayNumber(t *testing.T) {
	t.Parallel()
	before := version.Today(time.Now())
	out, errOut, code := runInProc([]string{"version", "--day"}, "")
	after := version.Today(time.Now())
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	got := strings.TrimSpace(out)
	if got != strconv.Itoa(before) && got != strconv.Itoa(after) {
		t.Fatalf("version --day printed %q, want %d", out, before)
	}
}

func TestVersionFloorPrintsTheSDKsEngineFloor(t *testing.T) {
	t.Parallel()
	out, errOut, code := runInProc([]string{"version", "--floor"}, "")
	if code != 0 || out != checksdk.EngineFloor+"\n" {
		t.Fatalf("exit %d, %q %s", code, out, errOut)
	}
}

func TestUnknownSubcommandExitsUsage(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

func pinGoCache() error {
	if os.Getenv("GOCACHE") != "" {
		return nil
	}
	out, err := exec.Command("go", "env", "GOCACHE").Output()
	if err != nil {
		return fmt.Errorf("go env GOCACHE: %w", err)
	}
	return os.Setenv("GOCACHE", strings.TrimSpace(string(out)))
}
