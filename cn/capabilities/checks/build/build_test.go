package build

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/packset"
)

// sdkSource is the real SDK, read from the tree as cn embeds it: every
// file of the package but its tests and the embedding.
func sdkSource(t *testing.T) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir("../checksdk")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") || n == "embed.go" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("../checksdk", n))
		if err != nil {
			t.Fatal(err)
		}
		out[n] = raw
	}
	return out
}

// canon is the canon packs ids as the pack set loads them in repo.
func canon(repo string, ids ...string) []packset.Pack {
	var out []packset.Pack
	for _, id := range ids {
		out = append(out, packset.Pack{ID: id, Kind: packset.Canon, Dir: packset.Tree(repo, id)})
	}
	return out
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
// dev/release/verify/testdata/hello.
func helloRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	copyDir(t, "../../../../dev/release/verify/testdata/hello", filepath.Join(repo, ".claudinite/shared/packs/hello"))
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
	srcs, err := Sources(canon(repo, "hello", "absent"))
	if err != nil {
		t.Fatal(err)
	}
	if len(srcs) != 4 || srcs[0].Pack != "hello" || srcs[0].Path != "change.go" || srcs[3].Path != "judge.go" {
		t.Fatalf("%d sources: %s %s", len(srcs), srcs[0].Path, srcs[len(srcs)-1].Path)
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
	srcs2, _ := Sources(canon(repo, "hello"))
	if Key(c, srcs2) == k1 {
		t.Error("a new source does not move the key")
	}
	if none, _ := Sources(canon(t.TempDir(), "hello")); len(none) != 0 {
		t.Errorf("sources in an empty repo: %v", none)
	}
}

func TestBuildRunsOnceAndRebuildsNothing(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go on PATH")
	}
	repo := helloRepo(t)
	c := cfg(t)
	srcs, _ := Sources(canon(repo, "hello"))
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
	// The judges manifest beside the binary names each hook event's
	// judges, so a hook knows without starting the child.
	if j, err := Judges(c, key); err != nil || strings.Join(j["pre-tool-use"], ",") != "hello/hello-judge" || len(j) != 1 {
		t.Errorf("judges %v %v", j, err)
	}
	log, _ := os.ReadFile(filepath.Join(c.Dir(key), "build.log"))
	if !strings.HasPrefix(string(log), "go version go1.") {
		t.Errorf("build.log starts %q", log)
	}
	sdk, err := os.ReadFile(filepath.Join(c.CacheRoot, "1.1.0", "checksdk", "checksdk.go"))
	if len(c.SDK) < 5 {
		t.Errorf("the SDK has %d files", len(c.SDK))
	}
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
	if rec, ok := ReadRecord(c, key); !ok || !rec.OK || rec.Took <= 0 {
		t.Errorf("record of a successful build: %+v %v", rec, ok)
	}
}

func TestBuildRecordsACompileError(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go on PATH")
	}
	repo := helloRepo(t)
	_ = os.WriteFile(filepath.Join(repo, ".claudinite/shared/packs/hello/checks/broken.go"), []byte("package checks\nfunc broken() { return 1 }\n"), 0o644)
	c := cfg(t)
	srcs, _ := Sources(canon(repo, "hello"))
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
	srcs, _ := Sources(canon(repo, "hello"))
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

// A local pack's checks build as local/<name>; a temp pack's never do.
func TestSourcesTakeLocalPacksAndSkipTemp(t *testing.T) {
	repo := helloRepo(t)
	local := filepath.Join(repo, ".claudinite/local/packs/probe")
	temp := filepath.Join(repo, ".claudinite/temp/packs/copied")
	for _, d := range []string{local, temp} {
		_ = os.MkdirAll(filepath.Join(d, "checks"), 0o755)
		_ = os.WriteFile(filepath.Join(d, "checks", "c.go"), []byte("package checks\n"), 0o644)
	}
	packs := append(canon(repo, "hello"), packset.Pack{ID: "probe", Kind: packset.Local, Dir: local}, packset.Pack{ID: "copied", Kind: packset.Temp, Dir: temp})
	srcs, err := Sources(packs)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range srcs {
		names = append(names, s.Pack+":"+s.Path)
	}
	if strings.Join(names, " ") != "hello:change.go hello:config.go hello:hello.go hello:judge.go local/probe:c.go" {
		t.Errorf("sources %v", names)
	}
	k := Key(cfg(t), srcs)
	_ = os.WriteFile(filepath.Join(local, "checks", "c.go"), []byte("package checks\n\n"), 0o644)
	srcs2, _ := Sources(packs)
	if Key(cfg(t), srcs2) == k {
		t.Error("a local pack's Go file does not move the key")
	}
}

// The SDK a pack repo's tests resolve and the one the build compiles
// against are written by one function: the same files, byte for byte.
func TestWriteSDKIsTheBuildsSDK(t *testing.T) {
	c := cfg(t)
	built, err := UnpackSDK(c)
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := WriteSDK(out, c.SDK); err != nil {
		t.Fatal(err)
	}
	for n := range c.SDK {
		a, _ := os.ReadFile(filepath.Join(built, n))
		b, _ := os.ReadFile(filepath.Join(out, n))
		if len(a) == 0 || !bytes.Equal(a, b) {
			t.Errorf("%s differs", n)
		}
	}
	a, _ := os.ReadFile(filepath.Join(built, "go.mod"))
	b, _ := os.ReadFile(filepath.Join(out, "go.mod"))
	if !bytes.Equal(a, b) || !strings.HasPrefix(string(a), "module claudinite.com/checksdk") {
		t.Errorf("go.mod %q %q", a, b)
	}
	stanza, _ := os.ReadFile(filepath.Join(out, "go.mod.stanza"))
	if want := "require claudinite.com/checksdk v0.0.0\n\nreplace claudinite.com/checksdk => " + filepath.ToSlash(out) + "\n"; string(stanza) != want {
		t.Errorf("stanza %q, want %q", stanza, want)
	}
}

// Every compile leaves its duration and result beside the binary, so the
// session that started a detached build can report it later; a key never
// compiled has no record, which is not a build of zero.
func TestBuildRecordsItsTimingAndOutcome(t *testing.T) {
	repo := helloRepo(t)
	c := cfg(t)
	c.Go = filepath.Join(t.TempDir(), "no-go-here")
	srcs, _ := Sources(canon(repo, "hello"))
	key := Key(c, srcs)
	if _, ok := ReadRecord(c, key); ok {
		t.Fatal("a record before any build")
	}
	if err := Build(c, key, srcs); err == nil {
		t.Fatal("built without go")
	}
	rec, ok := ReadRecord(c, key)
	if !ok || rec.OK || rec.Took < 0 {
		t.Errorf("record of a failed build: %+v %v", rec, ok)
	}
}

// A lock whose holder has exited is a build nobody is running: a session
// can kill a detached build, and the next one must not wait out staleLock
// behind it.
func TestADeadBuildersLockIsTakenOver(t *testing.T) {
	c := cfg(t)
	if err := os.MkdirAll(c.checksRoot(), 0o700); err != nil {
		t.Fatal(err)
	}
	gone := exec.Command("true")
	if err := gone.Run(); err != nil {
		t.Skip("no true on PATH")
	}
	if err := os.WriteFile(c.lockPath("k"), []byte(strconv.Itoa(gone.Process.Pid)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if Building(c, "k") {
		t.Error("a dead holder's lock reads as a build under way")
	}
	r, err := lock(c, "k")
	if err != nil {
		t.Fatalf("a dead holder's lock was not taken over: %v", err)
	}
	if !Building(c, "k") {
		t.Error("a live holder's lock does not read as a build under way")
	}
	if _, err := lock(c, "k"); err != ErrBuilding {
		t.Errorf("a live holder's lock: %v", err)
	}
	r()
}

func TestFailedNamesAFailedBuildNobodyRetries(t *testing.T) {
	c := cfg(t)
	if err := os.MkdirAll(c.Dir("k"), 0o755); err != nil {
		t.Fatal(err)
	}
	if Failed(c, "k") {
		t.Error("a key with no build reads as failed")
	}
	if err := os.WriteFile(c.failedPath("k"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !Failed(c, "k") {
		t.Error("a failed build does not read as failed")
	}
	r, err := lock(c, "k")
	if err != nil {
		t.Fatal(err)
	}
	if Failed(c, "k") {
		t.Error("a failed build being retried reads as failed")
	}
	r()
}
