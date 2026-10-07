// Package scripttest is what the tests under dev/ share: the repository
// root, running a dev script from it, the one release build the slow tests
// copy, golden files, and a workflow job's text.
package scripttest

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// Root is the repository root: the nearest directory above the test's
// working directory that holds go.mod.
func Root(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		up := filepath.Dir(dir)
		if up == dir {
			t.Fatal("no go.mod above the test's directory")
		}
		dir = up
	}
}

// Path is rel, a slash path from the repository root, made absolute.
func Path(t *testing.T, rel string) string {
	t.Helper()
	return filepath.Join(Root(t), filepath.FromSlash(rel))
}

// Run runs a script, named from the repository root, from there with extra
// environment.
func Run(t *testing.T, env []string, script string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", append([]string{script}, args...)...)
	cmd.Dir = Root(t)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

var (
	builtOnce sync.Once
	builtDist string
	builtOut  string
	builtErr  error
)

// UnsignedDist is a copy of one dev/release/create/build.sh run
// (VERSION=1.61001.1, the development roots, as the rehearsal needs), as a
// folder named dist under a fresh parent, and that run's output.
func UnsignedDist(t *testing.T) (string, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("builds a release; the full run covers it")
	}
	root := Root(t)
	builtOnce.Do(func() {
		var dir string
		if dir, builtErr = os.MkdirTemp("", "build-once-"); builtErr != nil {
			return
		}
		builtDist = filepath.Join(dir, "dist")
		cmd := exec.Command("sh", "dev/release/create/build.sh")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "VERSION=1.61001.1", "DIST="+builtDist, "REHEARSAL=1", "BUILD_TAGS=devroots")
		var out []byte
		out, builtErr = cmd.CombinedOutput()
		builtOut = string(out)
	})
	if builtErr != nil {
		t.Fatalf("build.sh: %v\n%s", builtErr, builtOut)
	}
	dist := filepath.Join(t.TempDir(), "dist")
	if out, err := exec.Command("cp", "-R", builtDist, dist).CombinedOutput(); err != nil {
		t.Fatalf("copy: %v %s", err, out)
	}
	return dist, builtOut
}

// DevKeyEnv is the environment dev/release/create/sign.sh signs with the
// development release key under.
func DevKeyEnv(t *testing.T) []string {
	t.Helper()
	return []string{"RELEASE_KEY=" + Path(t, "dev/keys/testkeys/release.key"), "RELEASE_CERT=" + Path(t, "dev/keys/testkeys/release.cert.json"), "ROOTS=" + Path(t, "cn/shared/trust/devroots")}
}

var update = flag.Bool("update", false, "rewrite golden files")

// Golden compares got with testdata/<name>, rewriting it under -update.
func Golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("%s differs from the golden file (go test -update to accept):\n%s", name, got)
	}
}

// JobBlock is one job's text in a workflow, named from the repository root:
// from "  <name>:" to the next job at the same indentation.
func JobBlock(t *testing.T, wf, name string) string {
	t.Helper()
	raw, err := os.ReadFile(Path(t, wf))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	i := strings.Index(s, "\n  "+name+":\n")
	if i < 0 {
		return ""
	}
	rest := s[i+1:]
	if j := regexp.MustCompile(`\n  [a-z][a-z0-9-]*:\n`).FindStringIndex(rest[1:]); j != nil {
		rest = rest[:j[0]+1]
	}
	return rest
}

// NoNpmPath is PATH with a fake npm first that fails if called: a dry run
// never runs npm.
func NoNpmPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "npm"), []byte("#!/bin/sh\necho \"npm was called: $*\" >&2\nexit 99\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir + string(os.PathListSeparator) + os.Getenv("PATH")
}
