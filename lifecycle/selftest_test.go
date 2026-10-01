package lifecycle

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSelftestReportsAndPasses(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "claudinite")
	crashes := filepath.Join(cache, "crashes")
	if err := os.MkdirAll(crashes, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i, age := range []time.Duration{time.Hour, 6 * 24 * time.Hour, 8 * 24 * time.Hour} {
		p := filepath.Join(crashes, "x-"+string(rune('1'+i))+".txt")
		_ = os.WriteFile(p, []byte("x"), 0o600)
		_ = os.Chtimes(p, now.Add(-age), now.Add(-age))
	}
	var out bytes.Buffer
	code := Selftest(&out, SelftestInput{CacheRoot: cache, RootIDs: []string{"aaaa000011112222", "bbbb000011112222"}, Now: now})
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out.String())
	}
	s := out.String()
	for _, want := range []string{"version 0.0.0", "platform ", "cache " + cache, "writable", "aaaa000011112222", "bbbb000011112222", "crashes (7 days) 2"} {
		if !strings.Contains(s, want) {
			t.Errorf("selftest output lacks %q:\n%s", want, s)
		}
	}
}

func TestSelftestFailsOnUnwritableCache(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(file, nil, 0o600)
	var out bytes.Buffer
	code := Selftest(&out, SelftestInput{CacheRoot: filepath.Join(file, "claudinite"), RootIDs: []string{"a", "b"}, Now: time.Now()})
	if code != 1 || !strings.Contains(out.String(), "not writable") {
		t.Fatalf("exit %d\n%s", code, out.String())
	}
}

func TestSelftestFailsOnBadRoots(t *testing.T) {
	var out bytes.Buffer
	code := Selftest(&out, SelftestInput{CacheRoot: filepath.Join(t.TempDir(), "c"), RootsErr: errors.New("bad embed"), Now: time.Now()})
	if code != 1 || !strings.Contains(out.String(), "bad embed") {
		t.Fatalf("exit %d\n%s", code, out.String())
	}
}
