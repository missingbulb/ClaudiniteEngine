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
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1]); last != "plan: public" {
		t.Errorf("last line %q\n%s", last, out)
	}
	if strings.Contains(out.String(), "arrives with a later engine") {
		t.Error("the checklist still defers the App link")
	}
}

func TestInitWithNoKeyEndsOnTheInstallLink(t *testing.T) {
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
	lines := strings.Split(s, "\n")
	if !strings.Contains(lines[len(lines)-1]+lines[len(lines)-2], "installations/new") {
		t.Errorf("the link is not at the end:\n%s", s)
	}
}
