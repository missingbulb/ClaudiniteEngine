package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gobuild(t *testing.T, out string, env ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", "release/gobuild.sh", "linux-x64", out)
	cmd.Dir = ".."
	cmd.Env = append(append(os.Environ(), "VERSION=1.1.0", "REHEARSAL=", "BUILD_TAGS="), env...)
	o, err := cmd.CombinedOutput()
	return string(o), err
}

// The rehearsal_break and devroots tags exist for the rehearsal only: a
// release build refuses them, and any other tag.
func TestTheRehearsalBreakTagIsRehearsalOnly(t *testing.T) {
	out := filepath.Join(t.TempDir(), "cn")
	for _, env := range [][]string{{"BUILD_TAGS=rehearsal_break"}, {"BUILD_TAGS=devroots"}, {"BUILD_TAGS=devroots,rehearsal_break"}, {"REHEARSAL=1", "BUILD_TAGS=other"}, {"REHEARSAL=1", "BUILD_TAGS=devroots,other"}, {"REHEARSAL=1", "BUILD_TAGS=rehearsal_break,other"}} {
		if o, err := gobuild(t, out, env...); err == nil || !strings.Contains(o, "BUILD_TAGS") {
			t.Errorf("%v: built\n%s", env, o)
		}
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("a refused build left a binary")
	}
	if o, err := gobuild(t, out, "REHEARSAL=1", "BUILD_TAGS=devroots,rehearsal_break"); err != nil {
		t.Fatalf("%v\n%s", err, o)
	}
	corpus, _ := filepath.Abs("../lifecycle/verify/testdata/shapes/v1-yaml")
	got, err := exec.Command(out, "verify", "--repo", corpus).CombinedOutput()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 || strings.Count("\n"+string(got), "\nbreak ") != 1 || !strings.Contains(string(got), "break rehearsal ") {
		t.Errorf("rehearsal_break verify: %v\n%s", err, got)
	}
	plain := filepath.Join(t.TempDir(), "cn")
	if o, err := gobuild(t, plain); err != nil {
		t.Fatalf("%v\n%s", err, o)
	}
	if got, err := exec.Command(plain, "verify", "--repo", corpus).CombinedOutput(); err != nil || strings.Contains(string(got), "rehearsal") {
		t.Errorf("a plain build carries the rehearsal rule: %v\n%s", err, got)
	}
}
