package gitcmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@x", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@x")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// clone is a working repo whose origin is a fresh bare remote with one
// commit on main.
func clone(t *testing.T) (Repo, string) {
	t.Helper()
	root := t.TempDir()
	bare := filepath.Join(root, "origin.git")
	work := filepath.Join(root, "work")
	git(t, root, "init", "-q", "--bare", "-b", "main", bare)
	git(t, root, "init", "-q", "-b", "main", work)
	_ = os.WriteFile(filepath.Join(work, "a.txt"), []byte("one\n"), 0o644)
	git(t, work, "add", "a.txt")
	git(t, work, "commit", "-q", "-m", "first")
	git(t, work, "remote", "add", "origin", bare)
	git(t, work, "push", "-q", "origin", "main")
	return Repo{Dir: work}, bare
}

func TestBranchCommitPushDelete(t *testing.T) {
	r, bare := clone(t)
	mainSHA, err := r.Head()
	if err != nil || len(mainSHA) != 40 {
		t.Fatalf("%q %v", mainSHA, err)
	}
	if b, err := r.CurrentBranch(); err != nil || b != "main" {
		t.Fatalf("current branch %q %v", b, err)
	}
	if err := r.CreateBranch("claudinite/engine-1.2.0", "HEAD"); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(r.Dir, "a.txt"), []byte("two\n"), 0o644)
	if err := r.Commit("Move the pin", "a.txt"); err != nil {
		t.Fatal(err)
	}
	if err := r.Push("origin", "claudinite/engine-1.2.0"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, bare, "log", "-1", "--format=%s %an", "claudinite/engine-1.2.0"); got != "Move the pin github-actions[bot]" {
		t.Errorf("remote branch head: %s", got)
	}
	names, err := r.ChangedFiles("main", "claudinite/engine-1.2.0")
	if err != nil || strings.Join(names, ",") != "a.txt" {
		t.Errorf("%v %v", names, err)
	}
	old, ok, err := r.Show("main", "a.txt")
	if err != nil || !ok || string(old) != "one\n" {
		t.Errorf("show: %q %v %v", old, ok, err)
	}
	if _, ok, err := r.Show("main", "missing.txt"); ok || err != nil {
		t.Errorf("show of a missing path: %v %v", ok, err)
	}
	if err := r.Checkout("main"); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteRemoteBranch("origin", "claudinite/engine-1.2.0"); err != nil {
		t.Fatal(err)
	}
	if out := git(t, bare, "branch", "--list", "claudinite/*"); out != "" {
		t.Errorf("branch still on the remote: %s", out)
	}
}

// The job token stays in the engine's memory: no git child sees it.
func TestChildEnvironmentHoldsNoToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghs_secret")
	t.Setenv("GH_TOKEN", "ghs_secret")
	for _, kv := range childEnv() {
		if strings.Contains(kv, "ghs_secret") {
			t.Errorf("child environment carries %s", kv)
		}
	}
}

func TestCloneBranchAndReadTree(t *testing.T) {
	r, bare := clone(t)
	if err := r.CreateBranch("vendored", "HEAD"); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Join(r.Dir, "p", "bin"), 0o755)
	_ = os.WriteFile(filepath.Join(r.Dir, "p", "x.txt"), []byte("x\n"), 0o644)
	_ = os.WriteFile(filepath.Join(r.Dir, "p", "bin", "run"), []byte("#!/bin/sh\n"), 0o755)
	if err := r.Commit("vendor", "p"); err != nil {
		t.Fatal(err)
	}
	if err := r.Push("origin", "vendored"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "c")
	if err := Clone(bare, "vendored", dir); err != nil {
		t.Fatal(err)
	}
	c := Repo{Dir: dir}
	got, ok, err := c.Show("HEAD", "p/x.txt")
	if err != nil || !ok || string(got) != "x\n" {
		t.Fatalf("%q %v %v", got, ok, err)
	}
	files, err := r.Tree("HEAD", "p")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || string(files["p/x.txt"].Data) != "x\n" || files["p/x.txt"].Executable || !files["p/bin/run"].Executable {
		t.Errorf("%+v", files)
	}
	if err := Clone(filepath.Join(t.TempDir(), "nothing"), "vendored", filepath.Join(t.TempDir(), "d")); err == nil {
		t.Error("cloned a repo that does not exist")
	}
}

func TestDeleteBranch(t *testing.T) {
	r, _ := clone(t)
	if err := r.CreateBranch("side", "HEAD"); err != nil {
		t.Fatal(err)
	}
	if err := r.Checkout("main"); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteBranch("side"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.RevParse("side"); err == nil {
		t.Error("side still exists")
	}
}
