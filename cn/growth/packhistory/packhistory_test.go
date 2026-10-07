package packhistory

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/gitcmd"
)

func TestIsShippingIsTheVendoredSetsRule(t *testing.T) {
	for p, want := range map[string]bool{
		"packs/acme/RULES.md":                   true,
		"packs/acme/skills/a/SKILL.md":          true,
		"packs/acme/checks/acme.go":             true,
		"packs/acme/checks/acme_test.go":        false,
		"packs/acme/checks/sub/acme_test.go":    true,
		"packs/acme/test/x.mjs":                 false,
		"packs/acme/docs/why.md":                false,
		"packs/acme/provenance/VERSIONS.md":     false,
		"packs/acme/skills/a/test/fixture.txt":  true,
		"packs/directory.GENERATED.md":          false,
		".claudinite/local/packs/acme/RULES.md": false,
	} {
		if got := IsShipping(p); got != want {
			t.Errorf("IsShipping(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestDeclaredVersionReadsEveryPackVersion(t *testing.T) {
	for text, want := range map[string]string{
		`{"version": "61001.2"}`: "61001.2",
		`{"version": "1.4"}`:     "1.4",
		`{"version": "1.2.3"}`:   "1.2.3",
		`{"minEngineVersion": "1.61001.1", "version": "61001.3"}`: "61001.3",
		`{"minEngineVersion": "1.61001.1"}`:                       "",
	} {
		if got := DeclaredVersion(text); got != want {
			t.Errorf("DeclaredVersion(%s) = %q, want %q", text, got, want)
		}
	}
}

type fixture struct {
	t   *testing.T
	dir string
	n   int
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{t: t, dir: t.TempDir()}
	f.git("init", "-q")
	return f
}

func (f *fixture) git(args ...string) string {
	f.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = f.dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t",
		"GIT_COMMITTER_EMAIL=t@example.com", "GIT_AUTHOR_DATE=2026-07-0"+string(rune('1'+f.n))+"T12:00:00Z",
		"GIT_COMMITTER_DATE=2026-07-0"+string(rune('1'+f.n))+"T12:00:00Z")
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func (f *fixture) commit(msg string, files map[string]string) {
	f.t.Helper()
	for rel, body := range files {
		p := filepath.Join(f.dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			f.t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			f.t.Fatal(err)
		}
	}
	f.git("add", "-A")
	f.git("commit", "-q", "-m", msg)
	f.n++
}

func TestHistoryDropsWhatTheVendoredSetDrops(t *testing.T) {
	f := newFixture(t)
	f.commit("born (#1)", map[string]string{"packs/acme/pack.json": `{"version": "1.2.3"}`, "packs/acme/RULES.md": "# a\n"})
	f.commit("tests and docs (#2)", map[string]string{"packs/acme/test/x.mjs": "t\n", "packs/acme/docs/d.md": "d\n", "packs/acme/checks/a_test.go": "package a\n"})
	f.commit("1.2.4 (#3)", map[string]string{"packs/acme/pack.json": `{"version": "1.2.4"}`})
	f.commit("docs again (#4)", map[string]string{"packs/acme/docs/d.md": "d2\n"})
	packs, err := Walker{Git: gitcmd.Repo{Dir: f.dir}}.History("HEAD", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) != 1 {
		t.Fatalf("packs = %+v", packs)
	}
	p := packs[0]
	if p.Version != "1.2.4" || p.LastBump == nil || p.LastBump.Version != "1.2.4" || len(p.ShippingSince) != 0 {
		t.Errorf("a docs change since the move ships nothing, got %+v", p)
	}
	if len(p.Versions) != 2 || len(p.Versions[1].Commits) != 1 || p.Versions[1].Commits[0].Subject != "1.2.4 (#3)" {
		t.Errorf("1.2.4 carried only its own commit, the tests-and-docs one shipped nothing: %+v", p.Versions)
	}
	if strings.Join(p.Missing, ",") != "1.2.3,1.2.4" {
		t.Errorf("missing = %v", p.Missing)
	}
	lines := Lines(packs)
	if !strings.HasPrefix(lines[0], "acme 1.2.4: moved at ") || !strings.HasSuffix(lines[0], ", nothing shipping changed since") {
		t.Errorf("head line = %q", lines[0])
	}
	f.commit("a rule (#5)", map[string]string{"packs/acme/RULES.md": "# b\n"})
	packs, err = Walker{Git: gitcmd.Repo{Dir: f.dir}}.History("HEAD", []string{"acme"})
	if err != nil {
		t.Fatal(err)
	}
	if got := Lines(packs); !strings.Contains(got[0], "1 shipping file changed since") || got[1] != "  changed: packs/acme/RULES.md" {
		t.Errorf("a shipping change since the move is named: %q", got)
	}
}

func TestHistoryRefusesAShallowClone(t *testing.T) {
	f := newFixture(t)
	f.commit("born", map[string]string{"packs/acme/pack.json": `{"version": "61001.1"}`})
	f.commit("next", map[string]string{"packs/acme/pack.json": `{"version": "61001.2"}`})
	clone := filepath.Join(t.TempDir(), "clone")
	if out, err := exec.Command("git", "clone", "-q", "--depth=1", "file://"+f.dir, clone).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	if _, err := (Walker{Git: gitcmd.Repo{Dir: clone}}).History("HEAD", nil); !errors.Is(err, ErrShallow) {
		t.Errorf("a shallow clone is refused, got %v", err)
	}
	if _, err := (Walker{Git: gitcmd.Repo{Dir: f.dir}}).History("HEAD", []string{"nope"}); err == nil || !strings.Contains(err.Error(), `no manifest for pack "nope"`) {
		t.Errorf("an unknown id names itself, got %v", err)
	}
}
