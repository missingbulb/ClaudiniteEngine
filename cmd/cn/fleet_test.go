package main

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
)

// fleetManager is a manager checkout with a GitHub origin and an API that
// counts the calls it is asked.
func fleetManager(t *testing.T) (string, *atomic.Int64) {
	t.Helper()
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", "https://github.com/acme/manager.git"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("CLAUDINITE_GITHUB_API", srv.URL)
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", "")
	t.Setenv("GITHUB_REPOSITORY", "")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	return repo, &calls
}

func TestAFleetSweepUnderAnOffKeyParksActionAndReadsNoMember(t *testing.T) {
	repo, calls := fleetManager(t)
	t.Setenv("FLEET_GITHUB_TOKEN", "t")
	// A session whose key request found no GitHub origin is degraded.
	gl := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", "https://gitlab.example/acme/m.git"}} {
		if out, err := exec.Command("git", append([]string{"-C", gl}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if _, errOut, code := runInProc([]string{"license", "request", "--session", "s1", "--repo", gl}, ""); code != 0 {
		t.Fatalf("license request: %d %s", code, errOut)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "s1")
	for _, cmd := range [][]string{{"fleet", "roster"}, {"fleet", "update"}, {"fleet", "judge", "acme/m"}} {
		_, errOut, code := runInProc(append(cmd, "--repo", repo), "")
		if code != 1 || !strings.Contains(errOut, "[cn] fleet: off under this key (degraded: ") ||
			!strings.Contains(errOut, "claudinite-needs-human: action — ") {
			t.Errorf("%v: exit %d, err %q", cmd, code, errOut)
		}
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("an off key made %d API calls", n)
	}
}

func TestAFleetSweepWithoutTheTokenSaysWhatToGrant(t *testing.T) {
	repo, calls := fleetManager(t)
	t.Setenv("FLEET_GITHUB_TOKEN", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	_, errOut, code := runInProc([]string{"fleet", "roster", "--repo", repo}, "")
	if code != 1 || !strings.Contains(errOut, "FLEET_GITHUB_TOKEN") || !strings.Contains(errOut, "[cn] fleet roster error 0/0 ") {
		t.Fatalf("exit %d, err %q", code, errOut)
	}
	if calls.Load() != 0 {
		t.Error("a missing token made API calls")
	}
}

func TestFleetTokenPrintsTheGrant(t *testing.T) {
	out, _, code := runInProc([]string{"fleet", "token", "--sweep", "fleet-update"}, "")
	if code != 0 || !strings.Contains(out, "FLEET_GITHUB_TOKEN must be granted: ") || !strings.Contains(out, "fleet-update uses: ") {
		t.Fatalf("exit %d, out %q", code, out)
	}
}
