package main

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/license"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/adopt"
)

func TestLicenseStatusWithNoSessionSaysSo(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	out, errOut, code := runInProc([]string{"license", "status", "--repo", t.TempDir()}, "")
	if code != 0 || strings.TrimSpace(out) != "no session state for this repo" {
		t.Fatalf("exit %d, out %q, err %q", code, out, errOut)
	}
}

func TestForegroundRequestWithoutAGitHubOriginDegrades(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("GITHUB_ACTIONS", "")
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", "https://gitlab.example/acme/member.git"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	out, errOut, code := runInProc([]string{"license", "request", "--session", "s1", "--repo", repo}, "")
	if code != 0 || !strings.Contains(out, "state: degraded: no-github-remote") || !strings.Contains(out, "off: ") {
		t.Fatalf("exit %d, out %q, err %q", code, out, errOut)
	}
	out, _, code = runInProc([]string{"license", "status", "--repo", repo}, "")
	if code != 0 || !strings.Contains(out, "session: s1") || !strings.Contains(out, "cause: no-github-remote") {
		t.Fatalf("status: exit %d, out %q", code, out)
	}
}

func TestLicenseRequestNeedsASession(t *testing.T) {
	_, _, code := runInProc([]string{"license", "request"}, "")
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

// adopt cannot import license, so it carries the App's install link too;
// this keeps lifecycle/adopt's InstallURL equal to license.InstallURL.
func TestInstallLinksAgree(t *testing.T) {
	if adopt.InstallURL != license.InstallURL {
		t.Errorf("adopt.InstallURL %q, license.InstallURL %q", adopt.InstallURL, license.InstallURL)
	}
}
