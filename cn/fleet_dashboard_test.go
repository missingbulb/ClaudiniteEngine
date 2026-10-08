package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/fleet/dashboard"
)

// A repo whose settings carry no fleet block is not a fleet manager, and
// gets no dashboard: nothing is written.
func TestCreateDashboardArtifactRefusesARepoWithNoFleetBlock(t *testing.T) {
	repo, _ := fleetManager(t)
	entitleAcme(t)
	writeFile(t, repo, ".claudinite/settings.yaml", "checks: {}\n")
	out := filepath.Join(t.TempDir(), "_site")
	_, errOut, code := runInProc([]string{"fleet", "create-dashboard-artifact", "--repo", repo, "--out", out}, "")
	if code == 0 || !strings.Contains(errOut, "declares no fleet block") {
		t.Fatalf("exit %d, err %q", code, errOut)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("a refused build left %s behind (%v)", out, err)
	}
}

// The site lands in --out whole: the redirect at the root, the page and
// its modules at its home, and a config carrying the fleet block's owner
// and exclude list, the manager as the deployment repo and the sign-in
// pair from the repository variables. Nothing is left in a temp dir or
// beside --out, and what --out held before is gone.
func TestCreateDashboardArtifactWritesTheSiteFromTheFleetBlock(t *testing.T) {
	repo, _ := fleetManager(t)
	entitleAcme(t)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv(dashboard.ClientIDVar, "Iv1.acme")
	t.Setenv(dashboard.ExchangeURLVar, "")
	writeFile(t, repo, ".claudinite/settings.yaml", "fleet:\n  owner: Acme\n  exclude: [acme/Sandbox]\n  staleDays: 14\n")
	parent := t.TempDir()
	out := filepath.Join(parent, "_site")
	writeFile(t, out, "stale.txt", "from the last build")

	stdout, errOut, code := runInProc([]string{"fleet", "create-dashboard-artifact", "--repo", repo, "--out", out}, "")
	if code != 0 {
		t.Fatalf("exit %d, err %q", code, errOut)
	}
	if !strings.Contains(errOut, "[cn] fleet create-dashboard-artifact ok ") || !strings.Contains(stdout, "acme") {
		t.Errorf("out %q, err %q", stdout, errOut)
	}
	home := filepath.Join(out, filepath.FromSlash(dashboard.Home))
	for _, rel := range []string{
		filepath.Join(out, "index.html"), filepath.Join(out, ".nojekyll"),
		filepath.Join(home, "index.html"), filepath.Join(home, "favicon.svg"),
		filepath.Join(home, "src", "app.mjs"), filepath.Join(home, "src", "views", "view-fleet.mjs"),
		filepath.Join(home, "src", "read", "roster.mjs"),
	} {
		if _, err := os.Stat(rel); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}
	for _, gone := range []string{filepath.Join(home, "src", "index.html"), filepath.Join(out, "stale.txt")} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("%s should not be in the site (%v)", gone, err)
		}
	}
	root, _ := os.ReadFile(filepath.Join(out, "index.html"))
	if !strings.Contains(string(root), "url=./"+dashboard.Home+"/") {
		t.Errorf("the root does not redirect to the page: %s", root)
	}

	raw, err := os.ReadFile(filepath.Join(home, "dashboard.config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"mode": "fleet", "owner": "acme", "exclude": []any{"acme/sandbox"}, "deploymentRepo": "acme/manager",
		"clientId": "Iv1.acme", "exchangeUrl": nil, "defaultRepo": nil, "rates": nil,
	}
	for k, v := range want {
		got, _ := json.Marshal(cfg[k])
		w, _ := json.Marshal(v)
		if _, ok := cfg[k]; !ok || string(got) != string(w) {
			t.Errorf("config %s = %s, want %s", k, got, w)
		}
	}

	if left, _ := os.ReadDir(tmp); len(left) != 0 {
		t.Errorf("the build left %v in the temp dir", names(left))
	}
	if beside, _ := os.ReadDir(parent); len(beside) != 1 || beside[0].Name() != "_site" {
		t.Errorf("beside --out: %v", names(beside))
	}
}

// Without --out the site lands in _site at the manager's root.
func TestCreateDashboardArtifactDefaultsToSiteAtTheRoot(t *testing.T) {
	repo, _ := fleetManager(t)
	entitleAcme(t)
	t.Setenv("TMPDIR", t.TempDir())
	writeFile(t, repo, ".claudinite/settings.yaml", "fleet:\n  owner: acme\n")
	if _, errOut, code := runInProc([]string{"fleet", "create-dashboard-artifact", "--repo", repo}, ""); code != 0 {
		t.Fatalf("exit %d, err %q", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(repo, "_site", filepath.FromSlash(dashboard.Home), "index.html")); err != nil {
		t.Fatal(err)
	}
}

func names(es []os.DirEntry) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}
