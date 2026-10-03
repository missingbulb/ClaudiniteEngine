package adopt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
)

func TestInitWritesThePlanTheKeyNames(t *testing.T) {
	repo := t.TempDir()
	in, out := input(t, repo, "hello")
	asked := ""
	in.Key = func(dir string) KeyGrant { asked = dir; return KeyGrant{Plan: "public"} }
	if err := Init(in); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if asked != repo {
		t.Errorf("key asked for %q", asked)
	}
	raw, _ := os.ReadFile(filepath.Join(repo, ".claudinite/settings.yaml"))
	if l, err := settings.ReadLicense(raw, settings.YAML); err != nil || l.Plan != "public" {
		t.Errorf("plan %+v %v\n%s", l, err, raw)
	}
	if !strings.Contains(out.String(), "\nplan: public\n") || strings.Contains(out.String(), "Install the Claudinite GitHub App") {
		t.Errorf("want the plan line and no install row:\n%s", out)
	}
	if strings.Contains(out.String(), "arrives with a later engine") {
		t.Error("the checklist still defers the App link")
	}
}

func TestInitWithNoKeyHandsOverTheInstall(t *testing.T) {
	repo := t.TempDir()
	in, out := input(t, repo, "hello")
	in.Key = func(string) KeyGrant {
		return KeyGrant{Reason: "app-not-installed", Link: "https://github.com/apps/claudinite/installations/new"}
	}
	if err := Init(in); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	raw, _ := os.ReadFile(filepath.Join(repo, ".claudinite/settings.yaml"))
	if l, _ := settings.ReadLicense(raw, settings.YAML); l.Present {
		t.Errorf("a plan block with no key:\n%s", raw)
	}
	s := strings.TrimSpace(out.String())
	if !strings.Contains(s, "https://github.com/apps/claudinite/installations/new") || !strings.Contains(s, "sessions run degraded") {
		t.Errorf("output:\n%s", s)
	}
	if !strings.Contains(s, "  [ ] (cn) Install the Claudinite GitHub App on this account: https://github.com/apps/claudinite/installations/new\n") {
		t.Errorf("no install row in HANDOVER:\n%s", s)
	}
}

func TestInitWithNoPlanHandsOverTheCheckout(t *testing.T) {
	repo := t.TempDir()
	in, out := input(t, repo, "hello")
	in.Key = func(string) KeyGrant {
		return KeyGrant{Reason: "no-plan", Link: InstallURL, Checkout: "https://checkout.example/c"}
	}
	if err := Init(in); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	s := out.String()
	if !strings.Contains(s, "  [ ] (cn) Pick a plan for this private repo: https://checkout.example/c\n") || strings.Contains(s, "Install the Claudinite") {
		t.Errorf("want the plan row in place of the install row:\n%s", s)
	}
}
