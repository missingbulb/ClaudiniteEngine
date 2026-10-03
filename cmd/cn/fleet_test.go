package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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
	for _, cmd := range [][]string{{"fleet", "roster"}, {"fleet", "update"}, {"fleet", "judge", "acme/m"}, {"fleet", "add-packs"}, {"fleet", "pack-seeds"}} {
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

func TestTheWriteSweepsWithoutTheTokenSayWhatToGrant(t *testing.T) {
	repo, calls := fleetManager(t)
	t.Setenv("FLEET_GITHUB_TOKEN", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	for _, sweep := range []string{"add-packs", "pack-seeds"} {
		_, errOut, code := runInProc([]string{"fleet", sweep, "--repo", repo}, "")
		if code != 1 || !strings.Contains(errOut, "FLEET_GITHUB_TOKEN") || !strings.Contains(errOut, "[cn] fleet "+sweep+" error 0/0 ") {
			t.Errorf("%s: exit %d, err %q", sweep, code, errOut)
		}
	}
	if calls.Load() != 0 {
		t.Error("a missing token made API calls")
	}
}

// The member half's protocol test reads the constants as JSON.
func TestFleetProtocolPrintsTheWorkListConstants(t *testing.T) {
	out, errOut, code := runInProc([]string{"fleet", "protocol", "--json"}, "")
	var p map[string]string
	if code != 0 || json.Unmarshal([]byte(out), &p) != nil {
		t.Fatalf("exit %d, out %q, err %q", code, out, errOut)
	}
	if p["label"] == "" || p["memberTaskId"] == "" || p["requestedTitle"] == "" {
		t.Errorf("protocol %v", p)
	}
}

func TestFleetTokenPrintsTheGrant(t *testing.T) {
	out, _, code := runInProc([]string{"fleet", "token", "--sweep", "fleet-update"}, "")
	if code != 0 || !strings.Contains(out, "FLEET_GITHUB_TOKEN must be granted: ") || !strings.Contains(out, "fleet-update uses: ") {
		t.Fatalf("exit %d, out %q", code, out)
	}
}

// The per-repo half answers only for the fleet's own owner: a repository
// anywhere else is refused before anything of it is read.
func TestFleetJudgeRefusesARepositoryOutsideTheOwner(t *testing.T) {
	repo, calls := fleetManager(t)
	t.Setenv("FLEET_GITHUB_TOKEN", "t")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	writeFile(t, repo, ".claudinite/settings.yaml", "packs:\n  declared:\n    - id: claudinite-fleet-sheepdog\n      config:\n        owner: acme\n")
	_, errOut, code := runInProc([]string{"fleet", "judge", "Other/x", "--repo", repo}, "")
	if code != 1 || !strings.Contains(errOut, "other/x is not under the fleet's owner acme") || !strings.Contains(errOut, "[cn] fleet judge refused 0/1 ") {
		t.Fatalf("exit %d, err %q", code, errOut)
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("judging a repository outside the owner made %d API calls", n)
	}
}

func writeFile(t *testing.T, root, rel, text string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The executor carries a declared secret in its CLAUDINITE_SECRETS bag
// as well as stamped into its env, so the fleet reader takes either.
func TestTheFleetSignalReadsTheTokenFromTheSecretsBag(t *testing.T) {
	t.Setenv("FLEET_GITHUB_TOKEN", "")
	t.Setenv("CLAUDINITE_SECRETS", "")
	if fleetSignal("acme/manager") != nil {
		t.Fatal("a reader with no token anywhere")
	}
	t.Setenv("CLAUDINITE_SECRETS", `{"FLEET_GITHUB_TOKEN": "t"}`)
	if fleetSignal("acme/manager") == nil {
		t.Fatal("no reader with the token in the secrets bag")
	}
}
