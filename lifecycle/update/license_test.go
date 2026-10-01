package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/lifecycle/packs"
	"github.com/missingbulb/ClaudiniteEngine/shared/packindex"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
)

func okKey(edit func(k *LicenseKey)) *KeyResult {
	k := &LicenseKey{Plan: "organization", State: "ok", IssuedAt: t0}
	if edit != nil {
		edit(k)
	}
	return &KeyResult{Key: k}
}

func TestNoKeySkipsBeforeAnyNpmRead(t *testing.T) {
	for name, c := range map[string]struct {
		key  KeyResult
		want string
	}{
		"no oidc":     {KeyResult{Cause: "no-oidc"}, "skipped: no OIDC token (id-token: write is missing)"},
		"unreachable": {KeyResult{Cause: "server-unreachable", Detail: "dial tcp"}, "skipped: license server unreachable"},
		"refused":     {KeyResult{Cause: "workflow-not-pinned"}, "skipped: license refused (workflow-not-pinned)"},
		"pr trigger":  {KeyResult{Cause: "pull-request-trigger"}, "skipped: license refused (pull-request-trigger)"},
		"degraded":    {*okKey(func(k *LicenseKey) { k.State, k.Notice = "degraded", "acme's license is degraded" }), "skipped: license degraded (acme's license is degraded)"},
	} {
		for _, run := range []string{"engine", "packs"} {
			w := newWorld(t, settings.YAML)
			w.publish(t, v2, relOpts{})
			w.key = &c.key
			d := w.deps(t)
			var v string
			var err error
			if run == "engine" {
				v, err = Engine(d, Options{})
			} else {
				d.Packs = failingReader{t}
				v, err = Packs(d, Options{})
			}
			if err != nil || v != c.want {
				t.Errorf("%s %s: %q %v, want %q", name, run, v, err, c.want)
			}
			if !IsVerdict(v) {
				t.Errorf("%s: %q is no verdict form", name, v)
			}
			if reqs := w.reg.requests(); len(reqs) != 0 {
				t.Errorf("%s %s: npm read without a key: %v", name, run, reqs)
			}
		}
	}
}

// failingReader fails the test on any pack read.
type failingReader struct{ t *testing.T }

func (r failingReader) VerifiedIndex(id string) (packs.Verified, error) {
	r.t.Errorf("index read for %s without a key", id)
	return packs.Verified{}, os.ErrNotExist
}

func (r failingReader) Archive(id string, _ packindex.Entry) ([]byte, error) {
	r.t.Errorf("archive read for %s without a key", id)
	return nil, os.ErrNotExist
}

func TestRedMainSkipsBeforeTheKey(t *testing.T) {
	w := newWorld(t, settings.YAML)
	w.mainRun(t, "failure")
	if v, _ := Engine(w.deps(t), Options{}); !strings.HasPrefix(v, "skipped: main is not green") || w.keyCalls != 0 {
		t.Errorf("%q, %d key requests", v, w.keyCalls)
	}
}

func TestAppNotInstalledFilesOneInstallIssue(t *testing.T) {
	w := newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{})
	w.key = &KeyResult{Cause: "app-not-installed", Link: "https://github.com/apps/claudinite/installations/new"}
	v, err := Engine(w.deps(t), Options{})
	if err != nil || v != "skipped: the Claudinite App is not installed (#1)" {
		t.Fatalf("%q %v", v, err)
	}
	if got := w.hub.called("create-issue"); len(got) != 1 || got[0] != "create-issue Claudinite needs its GitHub App installed|claudinite-update" {
		t.Fatalf("issues %v", got)
	}
	for _, s := range []string{"https://github.com/apps/claudinite/installations/new", "updates stay off"} {
		if !strings.Contains(w.hub.issues[0].Body, s) {
			t.Errorf("body lacks %q:\n%s", s, w.hub.issues[0].Body)
		}
	}
	if v, err := Engine(w.deps(t), Options{}); err != nil || v != "skipped: the Claudinite App is not installed (#1)" {
		t.Fatalf("second run %q %v", v, err)
	}
	if len(w.hub.called("create-issue")) != 1 {
		t.Errorf("duplicated: %v", w.hub.calls)
	}
}

func TestGraceAndUnverifiedUpdateAsOk(t *testing.T) {
	for _, state := range []string{"grace", "unverified"} {
		w := newWorld(t, settings.YAML)
		w.publish(t, v2, relOpts{})
		w.key = okKey(func(k *LicenseKey) { k.State = state })
		if v, err := Engine(w.deps(t), Options{}); err != nil || v != "opened #1 for "+v2 {
			t.Errorf("%s: %q %v", state, v, err)
		}
	}
}

func TestAVersionHeldByTheKeyIsSkipped(t *testing.T) {
	w := newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{})
	w.key = okKey(func(k *LicenseKey) { k.Held = []string{v2} })
	v, err := Engine(w.deps(t), Options{})
	if err != nil || v != "up to date" || !strings.Contains(w.out.String(), v2+" skipped: held (license key)") {
		t.Fatalf("%q %v\n%s", v, err, w.out)
	}
}

func TestAPinRevokedByTheKeyFilesTheIssue(t *testing.T) {
	w := newWorld(t, settings.YAML)
	w.publish(t, v1, relOpts{})
	w.publish(t, v2, relOpts{})
	w.key = okKey(func(k *LicenseKey) { k.Revoked = []string{v1} })
	if v, err := Engine(w.deps(t), Options{}); err != nil || v != "opened #1 for "+v2 {
		t.Fatalf("%q %v\n%s", v, err, w.out)
	}
	if got := w.hub.called("create-issue"); len(got) != 1 || got[0] != "create-issue Claudinite engine "+v1+" is revoked|claudinite-update" {
		t.Fatalf("issues %v", got)
	}
	if !strings.Contains(w.hub.issues[0].Body, "license key") {
		t.Errorf("body %s", w.hub.issues[0].Body)
	}
}

func TestStatesFromKeyComeFirst(t *testing.T) {
	s := StatesFromKey(&LicenseKey{Held: []string{v2}, Revoked: []string{v3}})
	s = s.Union(States{Held: map[string]string{v1: "npm reason"}, Revoked: map[string]string{v2: "npm says revoked"}})
	if k, r := s.Of(v2); k != "revoked" || r != "npm says revoked" {
		t.Errorf("revoked still wins over held: %s %s", k, r)
	}
	if k, r := s.Of(v3); k != "revoked" || r != KeyReason {
		t.Errorf("%s %s", k, r)
	}
	if k, r := s.Of(v1); k != "held" || r != "npm reason" {
		t.Errorf("%s %s", k, r)
	}
}

func TestOneKeyPerRun(t *testing.T) {
	w := newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{})
	if _, err := Engine(w.deps(t), Options{}); err != nil || w.keyCalls != 1 {
		t.Errorf("%v, %d key requests", err, w.keyCalls)
	}
}

func (w *world) planOf(t *testing.T, ref string) string {
	t.Helper()
	raw := []byte(gitRun(t, w.repo, "show", ref+":"+settings.RelPath(w.f)) + "\n")
	l, err := settings.ReadLicense(raw, w.f)
	if err != nil {
		t.Fatal(err)
	}
	return l.Plan
}

func TestAPlanMismatchOpensAPlanPR(t *testing.T) {
	for _, f := range settings.Formats {
		w := newWorld(t, f)
		w.publish(t, v2, relOpts{})
		w.key = okKey(func(k *LicenseKey) { k.Plan = "public" })
		v, err := Engine(w.deps(t), Options{})
		if err != nil || v != "opened #1 for plan public" || !IsVerdict(v) {
			t.Fatalf("%s: %q %v\n%s", f, v, err, w.out)
		}
		pr := w.hub.pulls[0]
		if cp := w.hub.called("create-pull"); !strings.HasPrefix(pr.HeadRef, PlanBranchPrefix) || len(cp) != 1 || !strings.Contains(cp[0], "names the **public** plan") {
			t.Fatalf("%s: %+v", f, pr)
		}
		gitRun(t, w.repo, "fetch", "-q", "origin", pr.HeadRef)
		if got := w.planOf(t, "FETCH_HEAD"); got != "public" {
			t.Errorf("%s: branch plan %q", f, got)
		}
		if files := gitRun(t, w.repo, "diff", "--name-only", "main", "FETCH_HEAD"); files != settings.RelPath(f) {
			t.Errorf("%s: changed %q", f, files)
		}
		if gitRun(t, w.repo, "rev-parse", "--abbrev-ref", "HEAD") != "main" {
			t.Errorf("%s: left the checkout off main", f)
		}
		// Land accepts the plan-only shape.
		sha := gitRun(t, w.repo, "rev-parse", "FETCH_HEAD")
		w.hub.pulls[0].HeadSHA = sha
		v, err = Land(w.deps(t), pr.Number, sha)
		if err != nil || v != "landed plan public" || !IsVerdict(v) {
			t.Errorf("%s: land %q %v", f, v, err)
		}
	}
}

func TestAPaidKeyWithNoPlanBlockOpensNoPlanPR(t *testing.T) {
	w := newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{})
	if v, err := Engine(w.deps(t), Options{}); err != nil || v != "opened #1 for "+v2 {
		t.Fatalf("%q %v", v, err)
	}
}

func TestAStillDisagreeingKeySupersedesThePlanPR(t *testing.T) {
	w := newWorld(t, settings.YAML)
	p := filepath.Join(w.repo, settings.RelPath(w.f))
	raw, _ := os.ReadFile(p)
	raw, err := settings.SetPlan(raw, w.f, "organization")
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(p, raw, 0o644)
	gitRun(t, w.repo, "commit", "-q", "-am", "plan")
	gitRun(t, w.repo, "push", "-q", "origin", "main")
	w.mainRun(t, "success")
	w.key = okKey(func(k *LicenseKey) { k.Plan = "public" })
	if v, _ := Engine(w.deps(t), Options{}); v != "opened #1 for plan public" {
		t.Fatal(v)
	}
	// Same plan, CI not yet run: the engine update goes ahead beside it.
	w.publish(t, v2, relOpts{})
	if v, err := Engine(w.deps(t), Options{}); err != nil || v != "opened #2 for "+v2 {
		t.Fatalf("%q %v\n%v", v, err, w.hub.calls)
	}
	w.hub.pulls = w.hub.pulls[:1]
	w.key = okKey(func(k *LicenseKey) { k.Plan = "personal" })
	v, err := Engine(w.deps(t), Options{})
	if err != nil || v != "opened #3 for plan personal" {
		t.Fatalf("%q %v", v, err)
	}
	if got := w.hub.called("close-pull"); len(got) != 1 || got[0] != "close-pull 1" {
		t.Errorf("closed %v", got)
	}
}

func TestLandRefusesAPlanPRChangingMore(t *testing.T) {
	w := newWorld(t, settings.YAML)
	w.key = okKey(func(k *LicenseKey) { k.Plan = "public" })
	if v, _ := Engine(w.deps(t), Options{}); v != "opened #1 for plan public" {
		t.Fatal(v)
	}
	pr := w.hub.pulls[0]
	gitRun(t, w.repo, "fetch", "-q", "origin", pr.HeadRef)
	gitRun(t, w.repo, "checkout", "-q", "FETCH_HEAD")
	p := filepath.Join(w.repo, settings.RelPath(w.f))
	b, _ := os.ReadFile(p)
	_ = os.WriteFile(p, []byte(strings.Replace(string(b), v1, v2, 1)), 0o644)
	gitRun(t, w.repo, "commit", "-q", "-am", "more")
	gitRun(t, w.repo, "push", "-q", "-f", "origin", "HEAD:refs/heads/"+pr.HeadRef)
	sha := w.head(t)
	gitRun(t, w.repo, "checkout", "-q", "main")
	w.hub.pulls[0].HeadSHA = sha
	if _, err := Land(w.deps(t), 1, sha); err == nil {
		t.Error("landed a plan PR that moves the pin")
	}
}
