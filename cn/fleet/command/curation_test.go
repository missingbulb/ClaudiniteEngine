package command

import (
	"os/exec"
	"strings"
	"testing"
)

// gitAt runs git in dir as a fixed committer.
func gitAt(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@x", "-c", "commit.gpgsign=false"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// A promote branch passes with every path under the corpus roots, fails
// naming each stray one, and is refused with no merge base.
func TestFleetPromoteScope(t *testing.T) {
	repo, _ := fleetManager(t)
	entitleAcme(t)
	writeFile(t, repo, ".claudinite/settings.yaml", "fleet:\n  owner: acme\n  writePaths:\n    - skills\n")
	writeFile(t, repo, "packs/p/RULES.md", "a\n")
	gitAt(t, repo, "add", "-A")
	gitAt(t, repo, "commit", "-q", "-m", "base")
	gitAt(t, repo, "branch", "-M", "main")
	gitAt(t, repo, "checkout", "-q", "-b", "claudinite/growth-promote-1")
	writeFile(t, repo, "packs/p/RULES.md", "b\n")
	writeFile(t, repo, "skills/s.md", "s\n")
	out, errOut, code := runInProc([]string{"fleet", "promote-scope", "--base", "main", "--repo", repo}, "")
	if code != 0 || !strings.Contains(out, "promote-scope: OK — every changed path is under packs/, skills/.") {
		t.Fatalf("inside the roots: exit %d, out %q, err %q", code, out, errOut)
	}
	writeFile(t, repo, "engine/e.mjs", "e\n")
	_, errOut, code = runInProc([]string{"fleet", "promote-scope", "--base", "main", "--repo", repo}, "")
	if code != 1 || !strings.Contains(errOut, "touches 1 path(s):\n  - engine/e.mjs\n") {
		t.Errorf("a stray path: exit %d, err %q", code, errOut)
	}
	if _, errOut, code = runInProc([]string{"fleet", "promote-scope", "--repo", repo}, ""); code != 2 || !strings.Contains(errOut, "needs --base REF") {
		t.Errorf("no --base: exit %d, err %q", code, errOut)
	}
	gitAt(t, repo, "checkout", "-q", "--orphan", "lonely")
	gitAt(t, repo, "commit", "-q", "-m", "alone")
	if _, errOut, code = runInProc([]string{"fleet", "promote-scope", "--base", "main", "--repo", repo}, ""); code != 2 || !strings.Contains(errOut, "no merge-base") {
		t.Errorf("no merge base: exit %d, err %q", code, errOut)
	}
}
