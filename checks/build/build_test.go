package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// sdkSource is the real SDK, read from the tree as cn embeds it.
func sdkSource(t *testing.T) map[string][]byte {
	t.Helper()
	raw, err := os.ReadFile("../../checksdk/checksdk.go")
	if err != nil {
		t.Fatal(err)
	}
	return map[string][]byte{"checksdk.go": raw}
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), raw, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// helloRepo is a member declaring the hello pack, vendored from
// release/testdata/hello.
func helloRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	copyDir(t, "../../release/testdata/hello", filepath.Join(repo, ".claudinite/shared/packs/hello"))
	_ = os.WriteFile(filepath.Join(repo, ".claudinite/settings.yaml"), []byte("engine:\n  version: \"1.1.0\"\npacks:\n  declared:\n    - hello\n"), 0o644)
	return repo
}

func cfg(t *testing.T) Config {
	t.Helper()
	cache := filepath.Join(t.TempDir(), "claudinite")
	return Config{CacheRoot: cache, Engine: "1.1.0", SDK: sdkSource(t)}
}

func TestSourcesAndKey(t *testing.T) {
	repo := helloRepo(t)
	_ = os.WriteFile(filepath.Join(repo, ".claudinite/shared/packs/hello/checks/hello_test.go"), []byte("package checks\n"), 0o644)
	srcs, err := Sources(repo, []string{"hello", "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if len(srcs) != 1 || srcs[0].Pack != "hello" || srcs[0].Path != "hello.go" {
		t.Fatalf("%+v", srcs)
	}
	c := cfg(t)
	k1 := Key(c, srcs)
	if len(k1) != 64 || Key(c, srcs) != k1 {
		t.Fatalf("key %q", k1)
	}
	c2 := c
	c2.Engine = "1.2.0"
	if Key(c2, srcs) == k1 {
		t.Error("the engine version does not move the key")
	}
	c3 := c
	c3.SDK = map[string][]byte{"checksdk.go": append(append([]byte{}, c.SDK["checksdk.go"]...), '\n')}
	if Key(c3, srcs) == k1 {
		t.Error("the SDK does not move the key")
	}
	_ = os.WriteFile(filepath.Join(repo, ".claudinite/shared/packs/hello/checks/more.go"), []byte("package checks\n"), 0o644)
	srcs2, _ := Sources(repo, []string{"hello"})
	if Key(c, srcs2) == k1 {
		t.Error("a new source does not move the key")
	}
	if none, _ := Sources(t.TempDir(), []string{"hello"}); len(none) != 0 {
		t.Errorf("sources in an empty repo: %v", none)
	}
}

func TestBuildRunsOnceAndRebuildsNothing(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go on PATH")
	}
	repo := helloRepo(t)
	c := cfg(t)
	srcs, _ := Sources(repo, []string{"hello"})
	key := Key(c, srcs)
	start := time.Now()
	if err := Build(c, key, srcs); err != nil {
		log, _ := os.ReadFile(filepath.Join(c.Dir(key), "build.log"))
		t.Fatalf("%v\n%s", err, log)
	}
	t.Logf("build: %v", time.Since(start))
	bin := c.Binary(key)
	st, err := os.Stat(bin)
	if err != nil || st.Mode().Perm() != 0o555 {
		t.Fatalf("binary %v %v", st, err)
	}
	log, _ := os.ReadFile(filepath.Join(c.Dir(key), "build.log"))
	if !strings.HasPrefix(string(log), "go version go1.") {
		t.Errorf("build.log starts %q", log)
	}
	sdk, err := os.ReadFile(filepath.Join(c.CacheRoot, "1.1.0", "checksdk", "checksdk.go"))
	if err != nil || string(sdk) != string(c.SDK["checksdk.go"]) {
		t.Errorf("unpacked SDK: %v", err)
	}
	info, _ := os.Stat(filepath.Join(c.Dir(key), "build.log"))
	time.Sleep(20 * time.Millisecond)
	if err := Build(c, key, srcs); err != nil {
		t.Fatal(err)
	}
	again, _ := os.Stat(filepath.Join(c.Dir(key), "build.log"))
	if !again.ModTime().Equal(info.ModTime()) {
		t.Error("a second Build rewrote build.log")
	}
	// A warm rebuild of the same sources under a new key is fast: the
	// compile cache is the user's.
	c2 := c
	c2.Engine = "1.1.1"
	k2 := Key(c2, srcs)
	start = time.Now()
	if err := Build(c2, k2, srcs); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("warm rebuild took %v", d)
	} else {
		t.Logf("warm rebuild: %v", d)
	}
	if got, err := Wait(c, key, time.Second); err != nil || got != bin {
		t.Errorf("Wait: %q %v", got, err)
	}
}

func TestBuildRecordsACompileError(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go on PATH")
	}
	repo := helloRepo(t)
	_ = os.WriteFile(filepath.Join(repo, ".claudinite/shared/packs/hello/checks/broken.go"), []byte("package checks\nfunc broken() { return 1 }\n"), 0o644)
	c := cfg(t)
	srcs, _ := Sources(repo, []string{"hello"})
	key := Key(c, srcs)
	if err := Build(c, key, srcs); err == nil {
		t.Fatal("a broken check built")
	}
	log, _ := os.ReadFile(filepath.Join(c.Dir(key), "build.log"))
	if !strings.Contains(string(log), "broken.go") {
		t.Errorf("build.log: %s", log)
	}
	if _, err := Wait(c, key, 5*time.Second); err == nil || !strings.Contains(err.Error(), "failed") {
		t.Errorf("Wait on a failed build: %v", err)
	}
}

func TestBuildWithoutGoIsARecordedFailure(t *testing.T) {
	repo := helloRepo(t)
	c := cfg(t)
	c.Go = filepath.Join(t.TempDir(), "no-go-here")
	srcs, _ := Sources(repo, []string{"hello"})
	key := Key(c, srcs)
	err := Build(c, key, srcs)
	if err == nil {
		t.Fatal("built without go")
	}
	log, _ := os.ReadFile(filepath.Join(c.Dir(key), "build.log"))
	if !strings.Contains(string(log), "go") {
		t.Errorf("build.log: %q", log)
	}
}

func TestGoVersionFloor(t *testing.T) {
	for out, ok := range map[string]bool{
		"go version go1.24.7 linux/amd64":   true,
		"go version go1.25 darwin/arm64":    true,
		"go version go1.23.4 linux/amd64":   false,
		"go version devel go1.26-abc linux": true,
		"nonsense":                          false,
	} {
		if err := goFloor(out); (err == nil) != ok {
			t.Errorf("%q: %v", out, err)
		}
	}
}

func TestLockIsTakenOnceAndGoesStale(t *testing.T) {
	c := cfg(t)
	if err := os.MkdirAll(c.checksRoot(), 0o700); err != nil {
		t.Fatal(err)
	}
	release, err := lock(c, "k")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lock(c, "k"); err != ErrBuilding {
		t.Errorf("second lock: %v", err)
	}
	release()
	release2, err := lock(c, "k")
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-11 * time.Minute)
	_ = os.Chtimes(c.lockPath("k"), old, old)
	if r, err := lock(c, "k"); err != nil {
		t.Errorf("a stale lock was not taken over: %v", err)
	} else {
		r()
	}
	release2()
}

func TestStartRunsDetached(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-cn")
	marker := filepath.Join(dir, "ran")
	_ = os.WriteFile(script, []byte("#!/bin/sh\necho \"$@\" > "+marker+"\n"), 0o755)
	start := time.Now()
	if err := Start(script, "/repo", "abc"); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Error("Start waited")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(marker); err == nil && strings.TrimSpace(string(raw)) == "check build --repo /repo --key abc" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("the detached build never ran")
}
