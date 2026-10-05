package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/gitcmd"
)

func TestVerifyCommand(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	_ = os.WriteFile(filepath.Join(dir, ".claudinite/settings.yaml"), []byte(strings.Replace(string(raw), "1.60930.1", "1.60930.2", 1)), 0o644)
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

func TestVerifyReadsTheDeclaredChecks(t *testing.T) {
	t.Parallel()
	bin := buildCN(t, "")
	src, _ := filepath.Abs("../../lifecycle/verify/testdata/shapes/v5-settings-checks")
	dir := t.TempDir()
	if out, err := exec.Command("cp", "-R", src+"/.", dir).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	out, _, _ := runCN(t, bin, nil, "", "verify", "--repo", dir)
	if strings.Contains(out, "settings-checks") {
		t.Errorf("a rule the declared pack carries is reported:\n%s", out)
	}
	path := filepath.Join(dir, ".claudinite/settings.yaml")
	raw, _ := os.ReadFile(path)
	_ = os.WriteFile(path, []byte(strings.Replace(string(raw), "    acme-check: \"off\"\n  accept", "    acme-check: \"off\"\n    ghost-check: \"advise\"\n  accept", 1)), 0o644)
	out, _, _ = runCN(t, bin, nil, "", "verify", "--repo", dir)
	if !strings.Contains(out, `deprecation settings-checks .claudinite/settings.yaml: names rule "ghost-check"`) || strings.Contains(out, `"acme-check"`) {
		t.Errorf("a rule no declared check carries:\n%s", out)
	}
	// A coded check is known by either name, as a declared one is.
	_ = os.MkdirAll(filepath.Join(dir, ".claudinite/shared/packs/acme-pack/checks"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, ".claudinite/shared/packs/acme-pack/checks/coded.go"), []byte("package checks\n\nimport \"claudinite.com/checksdk\"\n\nfunc init() {\n\tchecksdk.Register(checksdk.Check{ID: \"acme-coded\", Tags: []string{\"world\"}, Run: func(checksdk.Repo) []checksdk.Finding { return nil }})\n}\n"), 0o644)
	raw, _ = os.ReadFile(path)
	_ = os.WriteFile(path, []byte(strings.Replace(string(raw), "    ghost-check: \"advise\"\n", "    acme-pack/acme-coded: \"off\"\n    acme-pack/acme-check: advise\n    ghost-check: \"advise\"\n", 1)), 0o644)
	// verify never builds: with the coded checks unbuilt it judges no rule
	// key against them, says so once, and leaves the cache empty.
	cache := t.TempDir()
	env := []string{"XDG_CACHE_HOME=" + cache, "CLAUDINITE_CHECKS_NO_FETCH=1"}
	out, errOut, _ := runCN(t, bin, env, "", "verify", "--repo", dir)
	if strings.Contains(out, "settings-checks") || strings.Count(errOut, "coded checks") != 1 {
		t.Errorf("an unbuilt list judged rule keys, or did not say so once:\n%s\n%s", out, errOut)
	}
	if entries, _ := os.ReadDir(cache); len(entries) != 0 {
		t.Errorf("verify built into the cache: %v", entries)
	}
	if out, errOut, code := runCN(t, bin, env, "", "check", "--pack", "acme-pack", "--repo", dir); code != 0 && code != 1 {
		t.Fatalf("check --pack acme-pack: exit %d\n%s\n%s", code, out, errOut)
	}
	out, errOut, _ = runCN(t, bin, env, "", "verify", "--repo", dir)
	if !strings.Contains(out, `names rule "ghost-check"`) || strings.Contains(out, "acme-coded") || strings.Contains(out, `"acme-pack/acme-check"`) || strings.Contains(errOut, "coded checks") {
		t.Errorf("once built, a coded check or a pack-qualified name is reported, or the ghost is not:\n%s\n%s", out, errOut)
	}
	raw, _ = os.ReadFile(path)
	_ = os.WriteFile(path, []byte(strings.Replace(string(raw), "    ghost-check: \"advise\"\n", "", 1)), 0o644)
	_ = os.WriteFile(filepath.Join(dir, ".claudinite/shared/packs/acme-pack/declared-checks.yaml"), []byte("[]\n"), 0o644)
	out, _, _ = runCN(t, bin, nil, "", "verify", "--repo", dir)
	if !strings.Contains(out, "break descriptor-duplicate") {
		t.Errorf("two spellings of the declared checks:\n%s", out)
	}
}

// After a git fault, cn check -v names every check the spent tree kept
// from running.
func TestCheckVerboseNamesChecksSkippedAfterAGitFault(t *testing.T) {
	src, _ := filepath.Abs("../../lifecycle/verify/testdata/shapes/v1-yaml")
	dir := t.TempDir()
	if out, err := exec.Command("cp", "-R", src+"/.", dir).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"commit", "-q", "-m", "base"}} {
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@x"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	gitDir := t.TempDir()
	script := "#!/bin/sh\ncase \"$*\" in *check-attr*) exec sleep 60;; esac\nexec " + real + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(gitDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", gitDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CLAUDINITE_CHECKS_NO_FETCH", "1")
	old := gitcmd.CommandTimeout
	gitcmd.CommandTimeout = 200 * time.Millisecond
	defer func() { gitcmd.CommandTimeout = old }()
	out, errOut, code := runInProc([]string{"check", "--tag", "world", "-v", "--repo", dir}, "")
	if code != 1 || strings.Count(out, "checks-run") != 1 || !strings.Contains(errOut, "[cn] check skipped after a git fault: ") {
		t.Errorf("exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
}
