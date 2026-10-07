package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestHookSessionStartThroughCN(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	out, _, code := runInProc([]string{"hook", "session-start"}, `{"session_id":"s","cwd":"/r","hook_event_name":"SessionStart","source":"startup"}`)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var v struct {
		HookSpecificOutput struct{ AdditionalContext string } `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil || !strings.Contains(v.HookSpecificOutput.AdditionalContext, "Hello from cn") {
		t.Fatalf("stdout %q (%v)", out, err)
	}
}

func TestHookUsageErrors(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, args := range [][]string{{"hook"}, {"hook", "nonsense"}} {
		_, errOut, code := runInProc(args, "")
		if code != 2 || !strings.Contains(errOut, "cn: usage: ") {
			t.Errorf("%v: exit %d, stderr %q", args, code, errOut)
		}
	}
}

func TestSelftestThroughCN(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	out, _, code := runInProc([]string{"selftest"}, "")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !regexp.MustCompile(`(?m)^ok roots:(?: [0-9a-f]{16})+$`).MatchString(out) {
		t.Fatalf("no root ids: %s", out)
	}
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".claudinite"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(repo, ".claudinite", "settings.yaml"), []byte("engine:\n  version: \"1.0.0\"\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(repo, ".claude"), 0o755)
	_ = os.WriteFile(filepath.Join(repo, ".claude", "settings.json"), []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":".claudinite/bin/cn hook stopped"}]}]}}`), 0o644)
	out, _, code = runInProc([]string{"selftest", "--repo", repo}, "")
	if code != 1 || !strings.Contains(out, "fail hooks: Stop → cn hook stopped, an event this binary does not answer\n") {
		t.Fatalf("exit %d: %s", code, out)
	}
}

var crashLine = regexp.MustCompile(`^cn: crash: report written to (.+)$`)

func crashFileFrom(t *testing.T, stderr, wantCrumb string) string {
	t.Helper()
	var file string
	crumb := false
	for _, l := range strings.Split(strings.TrimRight(stderr, "\n"), "\n") {
		if m := crashLine.FindStringSubmatch(l); m != nil {
			if file != "" {
				t.Fatal("more than one crash line")
			}
			file = m[1]
		}
		if regexp.MustCompile(`^` + regexp.QuoteMeta(wantCrumb) + ` [0-9]+ms$`).MatchString(l) {
			crumb = true
		}
	}
	if file == "" || !crumb {
		t.Fatalf("stderr lacks the crash line or %q breadcrumb:\n%s", wantCrumb, stderr)
	}
	return file
}

func TestSelftestPanicWritesCrashFile(t *testing.T) {
	t.Parallel()
	bin := buildCN(t, "-X github.com/missingbulb/ClaudiniteEngine/cn/shared/version.version=1.1.0")
	cache := t.TempDir()
	_, errOut, code := runCN(t, bin, []string{"XDG_CACHE_HOME=" + cache, "CN_TEST_SECRET=hunter2-env"}, "stdin-secret-xyz", "selftest", "--panic")
	if code != 1 {
		t.Fatalf("exit %d, want 1\n%s", code, errOut)
	}
	file := crashFileFrom(t, errOut, "[cn] lifecycle selftest crash")
	if filepath.Dir(file) != filepath.Join(cache, "claudinite", "crashes") {
		t.Fatalf("crash file %s outside the cache's crashes folder", file)
	}
	if !regexp.MustCompile(`^[0-9]{8}T[0-9]{6}\.[0-9]{3}Z-[0-9]+\.txt$`).MatchString(filepath.Base(file)) {
		t.Fatalf("crash file name %s", filepath.Base(file))
	}
	st, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
	raw, _ := os.ReadFile(file)
	for _, want := range []string{"1.1.0", "selftest --panic", "goroutine"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("crash file lacks %q", want)
		}
	}
	for _, leak := range []string{"hunter2", "stdin-secret"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("crash file carries %q", leak)
		}
	}
}

func TestHookPanicExitsZero(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	saved := runHook
	runHook = func(string, io.Reader, io.Writer, io.Writer, time.Time) error { panic("hook boom") }
	defer func() { runHook = saved }()
	_, errOut, code := runInProc([]string{"hook", "stop"}, "{}")
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	file := crashFileFrom(t, errOut, "[cn] hooks stop crash")
	raw, _ := os.ReadFile(file)
	if !strings.Contains(string(raw), "hook boom") {
		t.Fatal("crash file lacks the panic value")
	}
}

func TestEveryRunPrunesOldCrashes(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	dir := filepath.Join(cache, "claudinite", "crashes")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().AddDate(0, 0, -40)
	p := filepath.Join(dir, fmt.Sprintf("20200101T000000.000Z-%d.txt", 1))
	_ = os.WriteFile(p, []byte("x"), 0o600)
	_ = os.Chtimes(p, old, old)
	if _, _, code := runInProc([]string{"version"}, ""); code != 0 {
		t.Fatal("version failed")
	}
	if _, err := os.Stat(p); err == nil {
		t.Fatal("a 40-day-old crash file survived a run")
	}
}
