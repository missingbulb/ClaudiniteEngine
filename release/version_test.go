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

func TestVersionNext(t *testing.T) {
	day := version.Today(time.Now())
	d := strconv.Itoa(day)
	out, code := versionSh(t, gitRepo(t), "next")
	if code != 0 || strings.TrimSpace(out) != d+".1.0" {
		t.Fatalf("no tags: exit %d, %q", code, out)
	}
	repo := gitRepo(t, "v"+d+".1.0", "v"+d+".2.0", "v"+strconv.Itoa(day-1)+".7.0", "unrelated")
	out, code = versionSh(t, repo, "next")
	if code != 0 || strings.TrimSpace(out) != d+".3.0" {
		t.Fatalf("two tags today: exit %d, %q", code, out)
	}
}

func TestVersionCheck(t *testing.T) {
	repo := gitRepo(t, "v60928.2.0")
	if out, code := versionSh(t, repo, "check", "60928.3.0"); code != 0 {
		t.Errorf("free version: exit %d %s", code, out)
	}
	if out, code := versionSh(t, repo, "check", "60928.2.0"); code != 1 {
		t.Errorf("taken version: exit %d %s", code, out)
	}
	if out, code := versionSh(t, repo, "check", "60928.2"); code == 0 {
		t.Errorf("malformed version accepted: %s", out)
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
