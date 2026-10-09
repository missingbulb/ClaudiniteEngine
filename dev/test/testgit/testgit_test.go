package testgit

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
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func put(t *testing.T, dir, rel, body string, mode os.FileMode) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

// A history written here is one git reads as its own: fsck finds nothing
// wrong (tree order and modes included), the refs and HEAD are where the
// calls left them, the index matches HEAD, and the tree is the one git
// would have staged.
func TestGitReadsTheHistoryAsItsOwn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	Init(t, dir, "main")
	put(t, dir, "a.b", "dot\n", 0o644)
	put(t, dir, "a/x", "in a\n", 0o644)
	put(t, dir, "a-b", "dash\n", 0o644)
	put(t, dir, "run.sh", "#!/bin/sh\n", 0o755)
	if err := os.MkdirAll(filepath.Join(dir, "empty", "deeper"), 0o755); err != nil {
		t.Fatal(err)
	}
	base := Commit(t, dir, "main", "base")
	put(t, dir, "a/x", "changed\n", 0o644)
	if err := os.Remove(filepath.Join(dir, "a-b")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.b", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	change := Commit(t, dir, "change", "the change")
	Index(t, dir)

	git(t, dir, "fsck", "--strict", "--no-dangling")
	if got := git(t, dir, "rev-parse", "main", "HEAD", "HEAD^", "--symbolic-full-name", "HEAD"); got != base+"\n"+change+"\n"+base+"\nrefs/heads/change" {
		t.Errorf("refs: %q", got)
	}
	if got := git(t, dir, "status", "--porcelain"); got != "" {
		t.Errorf("the worktree differs from HEAD: %q", got)
	}
	if got := git(t, dir, "write-tree"); got != git(t, dir, "rev-parse", "HEAD^{tree}") {
		t.Errorf("the index's tree %s is not HEAD's", got)
	}
	if got := git(t, dir, "ls-tree", "-r", "--format=%(objectmode) %(path)", "HEAD"); got != "100644 a.b\n100644 a/x\n120000 link\n100755 run.sh" {
		t.Errorf("tree: %q", got)
	}
	if got := git(t, dir, "log", "-1", "--format=%an <%ae>|%s"); got != Author+"|the change" {
		t.Errorf("commit: %q", got)
	}
	for _, ref := range []string{"HEAD", "refs/heads/main"} {
		want := git(t, dir, "rev-parse", ref)
		if got, err := Resolve(filepath.Join(dir, ".git"), ref); err != nil || got != want {
			t.Errorf("Resolve(%s) = %q %v, want %s", ref, got, err, want)
		}
	}
	git(t, dir, "pack-refs", "--all")
	if got, err := Resolve(filepath.Join(dir, ".git"), "refs/heads/main"); err != nil || got != base {
		t.Errorf("a packed ref: %q %v", got, err)
	}
	if _, err := Resolve(filepath.Join(dir, ".git"), "refs/heads/none"); err == nil {
		t.Error("a missing ref resolved")
	}
}

// A bare repository written here takes a push and serves a fetch.
func TestABareRepositoryTakesAPush(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	bare, dir := filepath.Join(root, "origin.git"), filepath.Join(root, "member")
	InitBare(t, bare, "main")
	Init(t, dir, "main")
	put(t, dir, "f", "x\n", 0o644)
	sha := Commit(t, dir, "main", "base")
	Index(t, dir)
	git(t, dir, "push", "-q", bare, "main")
	if got, err := Resolve(bare, "refs/heads/main"); err != nil || got != sha {
		t.Errorf("pushed main: %q %v", got, err)
	}
	git(t, bare, "fsck", "--strict", "--no-dangling")
}
