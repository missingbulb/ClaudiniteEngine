package main

import (
	"encoding/json"
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

// The version walk answers each pack's versions as text, or as the JSON
// the pack-version-history task reads.
func TestFleetPackHistory(t *testing.T) {
	repo, _ := fleetManager(t)
	entitleAcme(t)
	writeFile(t, repo, "packs/acme/pack.json", `{"version": "1.61001.1"}`)
	writeFile(t, repo, "packs/acme/RULES.md", "# a\n")
	gitAt(t, repo, "add", "-A")
	gitAt(t, repo, "commit", "-q", "-m", "born (#1)")
	writeFile(t, repo, "packs/acme/pack.json", `{"version": "1.61002.1"}`)
	gitAt(t, repo, "commit", "-q", "-am", "next (#2)")
	out, errOut, code := runInProc([]string{"fleet", "pack-history", "acme", "--json", "--repo", repo}, "")
	if code != 0 {
		t.Fatalf("exit %d, err %q", code, errOut)
	}
	var packs []struct {
		ID       string   `json:"id"`
		Version  string   `json:"version"`
		Missing  []string `json:"missing"`
		Versions []struct {
			Version string `json:"version"`
			Commits []struct {
				PR *int `json:"pr"`
			} `json:"commits"`
		} `json:"versions"`
	}
	if err := json.Unmarshal([]byte(out), &packs); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if len(packs) != 1 || packs[0].ID != "acme" || packs[0].Version != "1.61002.1" || strings.Join(packs[0].Missing, ",") != "1.61001.1,1.61002.1" ||
		len(packs[0].Versions) != 2 || len(packs[0].Versions[1].Commits) != 1 || *packs[0].Versions[1].Commits[0].PR != 2 {
		t.Errorf("history = %s", out)
	}
	out, _, code = runInProc([]string{"fleet", "pack-history", "--repo", repo}, "")
	if code != 0 || !strings.HasPrefix(out, "acme 1.61002.1: moved at ") || !strings.Contains(out, "  1.61002.1 ") {
		t.Errorf("text: exit %d, out %q", code, out)
	}
	if _, errOut, code = runInProc([]string{"fleet", "pack-history", "--ref"}, ""); code != 2 || !strings.Contains(errOut, "--ref needs a value") {
		t.Errorf("a flag with no value: exit %d, err %q", code, errOut)
	}
}
