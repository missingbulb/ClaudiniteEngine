package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

// gitRepo makes a repository with one commit and the given tags.
func gitRepo(t *testing.T, tags ...string) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	git(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "one")
	for _, tag := range tags {
		git(t, dir, "tag", tag)
	}
	return dir
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// versionSh runs release/version.sh with the given repository as its
// working directory.
func versionSh(t *testing.T, dir string, args ...string) (string, int) {
	t.Helper()
	script, _ := filepath.Abs("version.sh")
	cmd := exec.Command("sh", append([]string{script}, args...)...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return string(out), code
}

// major is the hand-edited release line release/major holds.
func major(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("major")
	if err != nil {
		t.Fatal(err)
	}
	m := strings.TrimSpace(string(raw))
	if _, err := strconv.Atoi(m); err != nil || m+"\n" != string(raw) {
		t.Fatalf("release/major holds %q, want one number on one line", raw)
	}
	return m
}

func TestVersionNext(t *testing.T) {
	m := major(t)
	day := version.Today(time.Now())
	d := strconv.Itoa(day)
	out, code := versionSh(t, gitRepo(t), "next")
	if code != 0 || strings.TrimSpace(out) != m+"."+d+".1" {
		t.Fatalf("no tags: exit %d, %q", code, out)
	}
	// Today's builds of this major count; another day's, another major's
	// and the retired <day>.<n>.0 tags do not.
	other := strconv.Itoa(mustAtoi(t, m) + 1)
	repo := gitRepo(t, "v"+m+"."+d+".1", "v"+m+"."+d+".2", "v"+m+"."+strconv.Itoa(day-1)+".7",
		"v"+other+"."+d+".9", "v"+d+".5.0", "v61003.1.0", "unrelated")
	out, code = versionSh(t, repo, "next")
	if code != 0 || strings.TrimSpace(out) != m+"."+d+".3" {
		t.Fatalf("two tags today: exit %d, %q", code, out)
	}
	if _, err := version.Parse(strings.TrimSpace(out)); err != nil {
		t.Fatalf("next printed a version Parse refuses: %v", err)
	}
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestVersionCheck(t *testing.T) {
	repo := gitRepo(t, "v1.61002.2")
	for _, free := range []string{"1.61002.3", "1.61001.1", "2.60101.1"} {
		if out, code := versionSh(t, repo, "check", free); code != 0 {
			t.Errorf("free version %s: exit %d %s", free, code, out)
		}
	}
	if out, code := versionSh(t, repo, "check", "1.61002.2"); code != 1 {
		t.Errorf("taken version: exit %d %s", code, out)
	}
	for _, bad := range []string{"1.61002", "61002.3.0", "1.61002.0", "01.61002.1", "1.61002.1.0"} {
		if out, code := versionSh(t, repo, "check", bad); code == 0 || !strings.Contains(out, "not a <major>.<day>.<n> version") {
			t.Errorf("malformed version %s: exit %d %s", bad, code, out)
		}
	}
	// A version below the SDK's engine floor would ship an engine the
	// floor says cannot answer SDK calls.
	for _, below := range []string{"1.60930.4", "1.60915.9", "0.61005.1"} {
		if out, code := versionSh(t, repo, "check", below); code == 0 || !strings.Contains(out, "below the engine floor 1.61001.1") {
			t.Errorf("%s below the floor: exit %d %s", below, code, out)
		}
	}
}

func TestVersionRefusesAShallowClone(t *testing.T) {
	src := gitRepo(t)
	git(t, src, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "two")
	shallow := filepath.Join(t.TempDir(), "shallow")
	git(t, ".", "clone", "-q", "--depth", "1", "file://"+src, shallow)
	out, code := versionSh(t, shallow, "next")
	if code == 0 || !strings.Contains(out, "fetch-depth: 0") {
		t.Fatalf("shallow clone: exit %d, %q", code, out)
	}
}

// check reads the floor from the file the SDK embeds, so it runs with no
// Go toolchain on PATH.
func TestVersionCheckRunsWithoutGo(t *testing.T) {
	repo := gitRepo(t, "v1.61002.2")
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.Symlink(gitBin, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	script, _ := filepath.Abs("version.sh")
	cmd := exec.Command("/bin/sh", script, "check", "1.61002.9")
	cmd.Dir = repo
	cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin", "HOME=" + t.TempDir()}
	if _, err := exec.LookPath("go"); err == nil {
		for _, d := range []string{"/usr/bin", "/bin"} {
			if _, err := os.Stat(filepath.Join(d, "go")); err == nil {
				t.Skipf("a go binary in %s is on every PATH this test can build", d)
			}
		}
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("check with no go on PATH: %v\n%s", err, out)
	}
}
