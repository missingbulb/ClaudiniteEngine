package report

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestExitCodesFollowTheClass(t *testing.T) {
	cases := []struct {
		err  error
		hook bool
		want int
	}{
		{nil, false, 0},
		{nil, true, 0},
		{New(Usage, "bad flag"), false, 2},
		{New(Usage, "unknown event"), true, 2},
		{New(IO, "disk full"), false, 1},
		{New(IO, "disk full"), true, 0},
		{New(Verify, "hash mismatch"), false, 1},
		{New(Internal, "bug"), true, 0},
		{errors.New("plain"), false, 1},
		{fmt.Errorf("wrapped: %w", New(Usage, "x")), false, 2},
		{New(Block, "denied"), true, 2},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		if got := Exit(&buf, c.err, c.hook); got != c.want {
			t.Errorf("Exit(%v, hook=%v) = %d, want %d", c.err, c.hook, got, c.want)
		}
	}
}

// A hook's block is its verdict, already written: exit 2 and nothing more.
func TestABlockExitsTwoSilently(t *testing.T) {
	var buf bytes.Buffer
	if got := Exit(&buf, New(Block, "denied"), true); got != 2 || buf.Len() != 0 {
		t.Errorf("exit %d, stderr %q", got, buf.String())
	}
}

func TestExitPrintsOneLine(t *testing.T) {
	var buf bytes.Buffer
	Exit(&buf, Wrap(IO, "write cache", errors.New("disk\nfull")), false)
	if got := buf.String(); got != "cn: io: write cache: disk full\n" {
		t.Fatalf("got %q", got)
	}
	buf.Reset()
	Exit(&buf, errors.New("plain"), false)
	if got := buf.String(); got != "cn: internal: plain\n" {
		t.Fatalf("got %q", got)
	}
	buf.Reset()
	Exit(&buf, nil, false)
	if buf.Len() != 0 {
		t.Fatalf("nil error printed %q", buf.String())
	}
}

func TestWriteCrash(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "crashes")
	t.Setenv("CN_SECRET_FOR_TEST", "sekrit-value")
	at := time.Date(2026, 10, 1, 13, 4, 5, 0, time.UTC)
	path, err := WriteCrash(dir, Crash{
		Version: "1.1.0", Platform: "linux-x64", Args: []string{"selftest", "--panic"},
		Value: "boom", Stack: []byte("goroutine 1 [running]:\nmain.main()"), At: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "20261001T130405") || !strings.HasSuffix(base, fmt.Sprintf("-%d.txt", os.Getpid())) {
		t.Fatalf("name %s", base)
	}
	st, _ := os.Stat(path)
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
	raw, _ := os.ReadFile(path)
	for _, want := range []string{"1.1.0", "linux-x64", "selftest --panic", "boom", "goroutine 1"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("crash file lacks %q", want)
		}
	}
	if strings.Contains(string(raw), "sekrit") {
		t.Error("crash file carries the environment")
	}
}

func touch(t *testing.T, dir, name string, mtime time.Time) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func TestPruneDropsOldAndKeepsFifty(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	touch(t, dir, "20260801T000000.000Z-1.txt", now.AddDate(0, 0, -31))
	touch(t, dir, "20260830T000000.000Z-2.txt", now.AddDate(0, 0, -29))
	for i := 0; i < 55; i++ {
		touch(t, dir, fmt.Sprintf("20260930T00%04d.000Z-%d.txt", i, 100+i), now.Add(-time.Duration(55-i)*time.Minute))
	}
	touch(t, dir, "notes.md", now.AddDate(-1, 0, 0))
	if err := Prune(dir, now); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	var crashes []string
	for _, e := range entries {
		if e.Name() != "notes.md" {
			crashes = append(crashes, e.Name())
		}
	}
	if len(crashes) != 50 {
		t.Fatalf("%d crash files kept, want 50", len(crashes))
	}
	for _, gone := range []string{"20260801T000000.000Z-1.txt", "20260830T000000.000Z-2.txt", "20260930T000000.000Z-100.txt"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); err == nil {
			t.Errorf("%s survived", gone)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.md")); err != nil {
		t.Error("prune removed a file that is not a crash file")
	}
	if err := Prune(filepath.Join(dir, "missing"), now); err != nil {
		t.Errorf("a missing crash folder is not an error: %v", err)
	}
}

func TestCountRecent(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	touch(t, dir, "a-1.txt", now.AddDate(0, 0, -8))
	touch(t, dir, "b-2.txt", now.AddDate(0, 0, -6))
	touch(t, dir, "c-3.txt", now.Add(-time.Hour))
	if got := CountRecent(dir, now, 7*24*time.Hour); got != 2 {
		t.Fatalf("got %d", got)
	}
	if got := CountRecent(filepath.Join(dir, "missing"), now, time.Hour); got != 0 {
		t.Fatalf("missing folder counted %d", got)
	}
}
