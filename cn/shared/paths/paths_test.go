package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCacheRootFollowsXDG(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/x/cache")
	if got := CacheRoot(); got != filepath.Join("/x/cache", "claudinite") {
		t.Fatalf("got %s", got)
	}
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "/home/me")
	if got := CacheRoot(); got != filepath.Join("/home/me", ".cache", "claudinite") {
		t.Fatalf("got %s", got)
	}
}

func TestEnsurePrivateDir(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "claudinite", "1.1.0")
	if err := EnsurePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(dir)
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o700 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
	if err := EnsurePrivateDir(dir); err != nil {
		t.Fatalf("second call: %v", err)
	}
}

func TestEnsurePrivateDirRefusesOpenDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no POSIX modes")
	}
	dir := filepath.Join(t.TempDir(), "open")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	err := EnsurePrivateDir(dir)
	if err == nil || !strings.Contains(err.Error(), dir) {
		t.Fatalf("want a refusal naming %s, got %v", dir, err)
	}
}

func TestPlaceReadOnly(t *testing.T) {
	dir := t.TempDir()
	if err := PlaceReadOnly(dir, "cn", []byte("binary"), 0o555); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, "cn"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o555 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
	if err := PlaceReadOnly(dir, "cn", []byte("binary2"), 0o555); err != nil {
		t.Fatalf("replacing a read-only file: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "cn"))
	if string(got) != "binary2" {
		t.Fatalf("got %q", got)
	}
}
