package declared

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/findings"
	"github.com/missingbulb/ClaudiniteEngine/dev/test/testgit"
)

const testSettings = `engine:
  version: "0.0.0"
  manifest: "sha512-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="
packs:
  declared:
    - local/acme-pack
`

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@x", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func put(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// member is a committed repo declaring one local pack whose declared
// checks are checksJSON, with files on top.
func member(t *testing.T, settingsYAML, checksJSON string, files map[string]string) string {
	t.Helper()
	t.Setenv("CLAUDINITE_CHECKS_NO_FETCH", "1")
	dir := t.TempDir()
	put(t, dir, map[string]string{
		".claudinite/settings.yaml":                              settingsYAML,
		".claudinite/local/packs/acme-pack/pack.json":            "{}\n",
		".claudinite/local/packs/acme-pack/declared-checks.json": checksJSON,
	})
	put(t, dir, files)
	testgit.Init(t, dir, "main")
	testgit.Commit(t, dir, "main", "base")
	testgit.Index(t, dir)
	return dir
}

func runSet(t *testing.T, dir string, sel Selection, now time.Time) ([]findings.Finding, string) {
	t.Helper()
	s, err := LoadSet(dir, "0.0.0")
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	fs, _ := s.Run(sel, now, &stderr)
	return ApplyConfig(fs, s.Config), stderr.String()
}

func render(fs []findings.Finding) string {
	var b bytes.Buffer
	findings.Print(&b, fs)
	return b.String()
}

const markerCheck = `[{"id":"acme-check","on_fail":"block","failureMessage":"Markers are placeholders.","fix":"Replace it.",
  "scanFiles":"/\\.md$/","matchLines":[{"match":"/ACME_MARKER/","what":"an acme marker"}]}]`

func TestWorldFindingPrintsWhyAndFix(t *testing.T) {
	dir := member(t, testSettings, markerCheck, map[string]string{"docs/a.md": "fine\nACME_MARKER here\n", "docs/b.txt": "ACME_MARKER\n"})
	fs, _ := runSet(t, dir, Selection{Tags: []string{"world"}}, time.Now())
	want := "finding acme-pack/acme-check docs/a.md:2: an acme marker\n  why: Markers are placeholders.\n  fix: Replace it.\n"
	if got := render(fs); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if fs, _ := runSet(t, dir, Selection{Tags: []string{"work"}}, time.Now()); len(fs) != 0 {
		t.Errorf("a world check ran under the work tag: %v", fs)
	}
	if fs, _ := runSet(t, dir, Selection{Pack: "other"}, time.Now()); len(fs) != 0 {
		t.Errorf("another pack's selection ran it: %v", fs)
	}
}

func TestGraceWindow(t *testing.T) {
	check := strings.Replace(markerCheck, `"on_fail":"block"`, `"on_fail":"block","since":"2026-09-20"`, 1)
	dir := member(t, testSettings, check, map[string]string{"a.md": "ACME_MARKER\n"})
	day := func(s string) time.Time { d, _ := time.Parse("2006-01-02", s); return d.Add(12 * time.Hour) }
	fs, _ := runSet(t, dir, Selection{}, day("2026-09-25"))
	if len(fs) != 1 || fs[0].Class != findings.Advisory || !strings.Contains(fs[0].Fix, "advisory until 2026-10-04") {
		t.Errorf("inside the window: %+v", fs)
	}
	if fs, _ := runSet(t, dir, Selection{}, day("2026-10-04")); len(fs) != 1 || fs[0].Class != findings.Coded {
		t.Errorf("after the window: %+v", fs)
	}
	if fs, _ := runSet(t, dir, Selection{}, day("2026-09-01")); len(fs) != 1 || fs[0].Class != findings.Coded {
		t.Errorf("a future since grants nothing: %+v", fs)
	}
}

func TestOverridesAndAcceptances(t *testing.T) {
	files := map[string]string{"a.md": "ACME_MARKER\n", "docs/b.md": "ACME_MARKER\n"}
	cases := []struct{ name, checks, want string }{
		{"off", "checks:\n  rules:\n    acme-check: \"off\"\n", ""},
		{"advise", "checks:\n  rules:\n    acme-check: \"advise\"\n", "advisory acme-pack/acme-check a.md:1"},
		{"accepted with a reason", "checks:\n  accept:\n    - rule: acme-check\n      path: docs/\n      reason: \"docs quote it\"\n", "finding acme-pack/acme-check a.md:1"},
		{"accepted with none", "checks:\n  accept:\n    - rule: acme-check\n      path: a.md\n", "finding config .claudinite/settings.yaml: acceptance for acme-check on a.md has no reason"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := member(t, testSettings+c.checks, markerCheck, files)
			fs, _ := runSet(t, dir, Selection{Tags: []string{"world"}}, time.Now())
			out := render(fs)
			if c.want == "" && out != "" || !strings.Contains(out, c.want) {
				t.Errorf("got\n%s", out)
			}
			if c.name == "accepted with a reason" && strings.Contains(out, "docs/b.md") {
				t.Errorf("an accepted path still reported:\n%s", out)
			}
			if c.name == "accepted with none" && strings.Count(out, "acme-pack/acme-check a.md") != 1 {
				t.Errorf("a reasonless acceptance keeps the finding:\n%s", out)
			}
		})
	}
}

func TestWorkChecksReadTheChange(t *testing.T) {
	check := `[{"id":"acme-work","on_fail":"block","scope":"work","failureMessage":"w","fix":"f",
	  "flagUntrackedFilesMatching":[{"match":"/^ACME_UNTRACKED$/","what":"an untracked marker"}]},
	 {"id":"acme-gated","on_fail":"block","scope":"work","failureMessage":"w","fix":"f",
	  "whenReplyClassIncludes":["feature"],"flagUntrackedFilesMatching":[{"match":"/./","what":"x"}]}]`
	dir := member(t, testSettings, check, nil)
	put(t, dir, map[string]string{"ACME_UNTRACKED": "x\n"})
	fs, stderr := runSet(t, dir, Selection{Tags: []string{"work"}}, time.Now())
	if len(fs) != 1 || fs[0].ID != "acme-work" || fs[0].Path != "ACME_UNTRACKED" {
		t.Errorf("untracked: %+v", fs)
	}
	if !strings.Contains(stderr, "1 check(s) read the session") {
		t.Errorf("the gated check is not named on stderr: %q", stderr)
	}
	gitIn(t, dir, "add", "ACME_UNTRACKED")
	if fs, _ := runSet(t, dir, Selection{Tags: []string{"work"}}, time.Now()); len(fs) != 0 {
		t.Errorf("tracked: %+v", fs)
	}
}

func TestATimeoutIsABreakNamingTheCheck(t *testing.T) {
	old := MatchTimeout
	MatchTimeout = 50 * time.Millisecond
	t.Cleanup(func() { MatchTimeout = old })
	check := `[{"id":"acme-slow","on_fail":"block","failureMessage":"w","fix":"f","scanFiles":"/\\.txt$/",
	  "matchLines":[{"match":"/^(a+)+$/","what":"slow"}]},
	 {"id":"acme-check","on_fail":"block","failureMessage":"w","fix":"f","scanFiles":"/\\.txt$/",
	  "matchLines":[{"match":"/^a/","what":"fast"}]}]`
	dir := member(t, testSettings, check, map[string]string{"x.txt": strings.Repeat("a", 40) + "b\n"})
	fs, _ := runSet(t, dir, Selection{}, time.Now())
	out := render(fs)
	if !strings.Contains(out, "break checks-run .claudinite/local/packs/acme-pack/declared-checks.json: the declared check acme-pack/acme-slow could not run") {
		t.Errorf("no break for the slow check:\n%s", out)
	}
	if !strings.Contains(out, "finding acme-pack/acme-check x.txt:1: fast") {
		t.Errorf("the other check lost its finding:\n%s", out)
	}
	if !strings.Contains(out, "acme-slow could not run: in x.txt line 1: the pattern /^(a+)+$/") {
		t.Errorf("the break does not name the file and line in flight:\n%s", out)
	}
}

// One clock per check over the whole sweep: a pattern that stays under the
// per-match bound on every line still stops at the check's deadline, as a
// break naming how far it got, while the other checks keep their hits.
func TestACheckPastItsDeadlineIsABreak(t *testing.T) {
	old := CheckDeadline
	CheckDeadline = 150 * time.Millisecond
	t.Cleanup(func() { CheckDeadline = old })
	check := `[{"id":"acme-slow","on_fail":"block","failureMessage":"w","fix":"f","scanFiles":"/\\.txt$/",
	  "matchLines":[{"match":"/^(a+)+$/","what":"slow"}]},
	 {"id":"acme-check","on_fail":"block","failureMessage":"w","fix":"f","scanFiles":"/\\.txt$/",
	  "matchLines":[{"match":"/^a/","what":"fast"}]}]`
	line := strings.Repeat("a", 20) + "b\n"
	dir := member(t, testSettings, check, map[string]string{"x.txt": strings.Repeat(line, 3000)})
	start := time.Now()
	fs, _ := runSet(t, dir, Selection{}, time.Now())
	took := time.Since(start)
	out := render(fs)
	if !regexp.MustCompile(`acme-slow could not run: in x\.txt line [0-9]+: ran past its 150ms deadline`).MatchString(out) {
		t.Errorf("no deadline break naming the line in flight:\n%.2000s", out)
	}
	if !strings.Contains(out, "finding acme-pack/acme-check x.txt:1: fast") {
		t.Errorf("the other check lost its finding:\n%.2000s", out)
	}
	if took > 5*time.Second {
		t.Errorf("the sweep took %v past a 150ms deadline", took)
	}
}

func TestLoadFaultIsABreak(t *testing.T) {
	dir := member(t, testSettings, `[{"id":"acme-check","on_fail":"sometimes","failureMessage":"w","fix":"f"}]`, nil)
	fs, _ := runSet(t, dir, Selection{}, time.Now())
	if out := render(fs); !strings.Contains(out, "break checks-run .claudinite/local/packs/acme-pack: the declared checks of pack acme-pack failed to load") {
		t.Errorf("got\n%s", out)
	}
}

func TestNonMemberRunsNothing(t *testing.T) {
	s, err := LoadSet(t.TempDir(), "0.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if fs, n := s.Run(Selection{}, time.Now(), nil); len(fs) != 0 || n != 0 || len(s.List()) != 0 {
		t.Errorf("non-member: %v %d", fs, n)
	}
}

// A rule set to off drops its findings whatever produced them, a coded
// check's included, so the member's `checks.rules.<id>: off` keeps Stop
// from blocking on it.
func TestApplyConfigDropsARuleSetOff(t *testing.T) {
	fs := []findings.Finding{
		{Class: findings.Coded, ID: "hello/hello-check", Path: "HELLO_FINDING"},
		{Class: findings.Advisory, ID: "hello/other", Path: "a"},
	}
	got := ApplyConfig(fs, Config{Rules: map[string]string{"hello-check": "off"}})
	if len(got) != 1 || got[0].ID != "hello/other" {
		t.Errorf("%+v", got)
	}
}

func TestCheckParsedFilesReadsEveryDescriptorFormat(t *testing.T) {
	check := `[{"id":"acme-check","on_fail":"block","failureMessage":"m","fix":"f",
  "checkParsedFiles":[{"filesMatching":"/^conf\\.(yaml|toml|json)$/","requireValueInArray":{"atField":"list.names","value":"acme","matchingEntryObjectsByField":"id"},"what":"{path} lacks acme"}]}]`
	cases := map[string][2]string{
		"conf.yaml": {"list:\n  names: [other, {id: acme}]\n", "list:\n  names: [other]\n"},
		"conf.toml": {"[list]\nnames = [\"other\", {id = \"acme\"}]\n", "[list]\nnames = [\"other\"]\n"},
		"conf.json": {`{"list": {"names": ["other", "acme"]}}`, `{"list": {"names": ["other"]}}`},
	}
	for name, bodies := range cases {
		for i, body := range bodies {
			dir := member(t, testSettings, check, map[string]string{name: body})
			fs, _ := runSet(t, dir, Selection{}, time.Now())
			fired := strings.Contains(render(fs), name+" lacks acme")
			if fired != (i == 1) {
				t.Errorf("%s holding %q: fired %v\n%s", name, body, fired, render(fs))
			}
		}
	}
}
