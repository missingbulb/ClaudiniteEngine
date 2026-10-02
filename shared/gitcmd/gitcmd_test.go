package gitcmd

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// The token reaches only the children that talk to the remote, as an
// http.extraheader in their environment, never in .git/config; every other
// child, and a Repo with no token, sees none.
func TestOnlyRemoteChildrenCarryTheToken(t *testing.T) {
	r, _ := clone(t)
	r.Token = "ghs_secret"
	key := "http.https://github.com/.extraheader"
	want := "AUTHORIZATION: basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:ghs_secret"))
	if got, err := r.remoteLine("config", "--get", key); err != nil || got != want {
		t.Errorf("remote child: %q %v", got, err)
	}
	if got, _ := r.line("config", "--get", key); got != "" {
		t.Errorf("a local child sees %q", got)
	}
	if raw, _ := os.ReadFile(filepath.Join(r.Dir, ".git", "config")); strings.Contains(string(raw), "ghs_secret") || strings.Contains(string(raw), base64.StdEncoding.EncodeToString([]byte("x-access-token:ghs_secret"))) {
		t.Error(".git/config holds the token")
	}
	r.Token = ""
	if got, _ := r.remoteLine("config", "--get", key); got != "" {
		t.Errorf("no token: %q", got)
	}
	// A push still works with the header set (the remote here is a path,
	// which ignores it).
	r.Token = "ghs_secret"
	if err := r.CreateBranch("authed", "HEAD"); err != nil {
		t.Fatal(err)
	}
	if err := r.Push("origin", "authed"); err != nil {
		t.Fatal(err)
	}
}

// A checkout from the old template persists actions/checkout's header in
// .git/config. http.extraheader values accumulate and an empty value
// clears the list, so a remote child must send the env's header alone:
// the entries after the last empty one are exactly the token's.
func TestTheTokenReplacesAPersistedHeader(t *testing.T) {
	r, _ := clone(t)
	key := "http.https://github.com/.extraheader"
	if _, err := r.run("config", "--add", key, "AUTHORIZATION: basic cGVyc2lzdGVk"); err != nil {
		t.Fatal(err)
	}
	r.Token = "ghs_secret"
	want := "AUTHORIZATION: basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:ghs_secret"))
	out, err := r.remote("config", "--get-all", key)
	if err != nil {
		t.Fatal(err)
	}
	values := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	effective := values
	for i, v := range values {
		if v == "" {
			effective = values[i+1:]
		}
	}
	if len(effective) != 1 || effective[0] != want {
		t.Errorf("a remote child sends %q (all values %q)", effective, values)
	}
	if got, _ := r.line("config", "--get-all", key); got != "AUTHORIZATION: basic cGVyc2lzdGVk" {
		t.Errorf("a local child sees %q", got)
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

// hangingGit puts a git on PATH that never answers.
func hangingGit(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	old := CommandTimeout
	CommandTimeout = 100 * time.Millisecond
	t.Cleanup(func() { CommandTimeout = old })
}

// A git that never answers cannot hang the engine: the call fails at
// CommandTimeout naming its arguments, and the repo's Faults keep the
// timeout for a caller whose read only reports ok.
func TestAHungGitTimesOut(t *testing.T) {
	hangingGit(t)
	r := Repo{Dir: t.TempDir(), Faults: &Faults{}}
	_, err := r.line("rev-parse", "HEAD")
	if err == nil || !strings.Contains(err.Error(), "git rev-parse HEAD timed out after 100ms") {
		t.Errorf("err %v", err)
	}
	got := r.Faults.Take()
	if len(got) != 1 || !strings.Contains(got[0], "git rev-parse HEAD timed out after 100ms") {
		t.Errorf("faults %q", got)
	}
	if again := r.Faults.Take(); len(again) != 0 {
		t.Errorf("Take does not clear: %q", again)
	}
	if _, ok := (Repo{Dir: t.TempDir()}).ShowText("HEAD", "a.txt"); ok {
		t.Error("a timed-out show read as ok")
	}
	if _, ok := (Repo{Dir: t.TempDir()}).input("x", "hash-object", "--stdin"); ok {
		t.Error("a timed-out input read as ok")
	}
	_, _ = (Repo{Dir: t.TempDir()}).try("status")
}

// One timeout spends a run's git: every later command sharing its Faults
// fails at once without running, so a hook's git cost is bounded by one
// CommandTimeout however many reads follow.
func TestAfterOneTimeoutLaterGitFailsAtOnce(t *testing.T) {
	hangingGit(t)
	r := Repo{Dir: t.TempDir(), Faults: &Faults{}}
	_, _ = r.line("rev-parse", "HEAD")
	start := time.Now()
	_, err := r.line("status")
	if err == nil || !strings.Contains(err.Error(), "git status not run: git rev-parse HEAD timed out after 100ms") {
		t.Errorf("err %v", err)
	}
	if _, ok := r.ShowText("HEAD", "a.txt"); ok {
		t.Error("a skipped show read as ok")
	}
	if _, ok := r.input("x", "hash-object", "--stdin"); ok {
		t.Error("a skipped input read as ok")
	}
	if d := time.Since(start); d >= 100*time.Millisecond {
		t.Errorf("three commands after a timeout took %v; they ran", d)
	}
	if got := r.Faults.Take(); len(got) != 1 {
		t.Errorf("faults %q: a skip is not a second fault", got)
	}
}
