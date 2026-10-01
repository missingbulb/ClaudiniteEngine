package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyCommand(t *testing.T) {
	bin := buildCN(t, "")
	out, _, code := runCN(t, bin, nil, "", "verify", "--repo", t.TempDir())
	if code != 1 || !strings.HasPrefix(out, "break settings-file .claudinite: ") || strings.Count(out, "break ") != 1 {
		t.Errorf("empty repo: exit %d\n%s", code, out)
	}
	corpus, _ := filepath.Abs("../../lifecycle/verify/testdata/shapes/v1-yaml")
	out, _, code = runCN(t, bin, nil, "", "verify", "--repo", corpus)
	if code != 0 || !strings.Contains(out, "deprecation bin-ignore") || strings.Contains(out, "break") {
		t.Errorf("v1 shape: exit %d\n%s", code, out)
	}
	if _, _, code := runCN(t, bin, nil, "", "verify", "--bogus"); code != 2 {
		t.Errorf("bad flag: exit %d", code)
	}
}

func TestCheckWorldCommand(t *testing.T) {
	bin := buildCN(t, "")
	src, _ := filepath.Abs("../../lifecycle/verify/testdata/shapes/v1-yaml")
	dir := t.TempDir()
	if out, err := exec.Command("cp", "-R", src+"/.", dir).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"commit", "-q", "-m", "base"}, {"checkout", "-q", "-b", "change"}} {
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@x"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	out, _, code := runCN(t, bin, nil, "", "check", "world", "--pr-author", "someone", "--base-ref", "main", "--repo", dir)
	if code != 0 || !strings.Contains(out, "deprecation bin-ignore") {
		t.Errorf("unchanged pin: exit %d\n%s", code, out)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".claudinite/settings.yaml"))
	_ = os.WriteFile(filepath.Join(dir, ".claudinite/settings.yaml"), []byte(strings.Replace(string(raw), "60930.1.0", "60930.2.0", 1)), 0o644)
	cmd := exec.Command("git", "-c", "user.name=t", "-c", "user.email=t@x", "commit", "-q", "-am", "move")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	out, _, code = runCN(t, bin, nil, "", "check", "world", "--pr-author", "someone", "--base-ref", "main", "--repo", dir)
	if code != 1 || !strings.Contains(out, "pin-guard") {
		t.Errorf("a person moved the pin: exit %d\n%s", code, out)
	}
	if out, _, code := runCN(t, bin, nil, "", "check", "world", "--repo", dir); code != 0 || strings.Contains(out, "pin-guard") {
		t.Errorf("no pull request to judge: the guard runs, or the findings break: exit %d\n%s", code, out)
	}
	if out, _, code := runCN(t, bin, nil, "", "check", "world", "--repo", t.TempDir()); code != 0 || out != "" {
		t.Errorf("a repo that is not a member: exit %d\n%s", code, out)
	}
}
