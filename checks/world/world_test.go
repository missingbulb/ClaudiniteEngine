package world

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
)

const (
	bot  = "github-actions[bot]"
	pin1 = "sha512-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="
	pin2 = "sha512-BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=="
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@x"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func settingsBody(ver, pin string) string {
	return "engine:\n  package: \"@claudinite/cli-rc\"\n  version: \"" + ver + "\"\n  manifest: \"" + pin + "\"\n"
}

// member is a repo whose main pins 1.1.0, checked out on a branch that the
// change function edits and commits.
func member(t *testing.T, change func(dir string)) string {
	t.Helper()
	return memberOn(t, settingsBody("1.1.0", pin1), change)
}

// memberOn is member with main's settings file given.
func memberOn(t *testing.T, base string, change func(dir string)) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	write(t, dir, ".claudinite/settings.yaml", base)
	write(t, dir, ".claudinite/launch", "#!/bin/sh\n")
	write(t, dir, "RULES.md", "- a rule\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "base")
	git(t, dir, "checkout", "-q", "-b", "change")
	change(dir)
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "change")
	return dir
}

func movePin(t *testing.T) func(string) {
	return func(dir string) { write(t, dir, ".claudinite/settings.yaml", settingsBody("1.2.0", pin2)) }
}

type pinCheck struct {
	called []settings.Engine
	err    error
}

func (p *pinCheck) check(e settings.Engine) error {
	p.called = append(p.called, e)
	return p.err
}

func runWorld(t *testing.T, dir, author string, pc *pinCheck, fs []findings.Finding) (int, string) {
	t.Helper()
	var out bytes.Buffer
	code := Run(&out, Input{Repo: dir, PRAuthor: author, BaseRef: "main", Git: gitcmd.Repo{Dir: dir}, CheckPin: pc.check, Findings: fs})
	return code, out.String()
}

func TestPinGuard(t *testing.T) {
	cases := []struct {
		name    string
		change  func(t *testing.T) func(string)
		author  string
		pinErr  error
		code    int
		want    string
		checked bool
	}{
		{"a person moves the pin", movePin, "someone", nil, 1, "pin-guard", false},
		{"the bot moves the pin to a verified version", movePin, bot, nil, 0, "", true},
		{"the bot moves the pin and a rule file", func(t *testing.T) func(string) {
			return func(dir string) { movePin(t)(dir); write(t, dir, "RULES.md", "- a changed rule\n") }
		}, bot, nil, 1, "RULES.md", false},
		{"the bot moves the pin and restates it in the member file", func(t *testing.T) func(string) {
			return func(dir string) { movePin(t)(dir); write(t, dir, ".claudinite/flat/member.GENERATED.json", "{}\n") }
		}, bot, nil, 0, "", true},
		{"the bot moves the pin and the task flat file", func(t *testing.T) func(string) {
			return func(dir string) { movePin(t)(dir); write(t, dir, ".claudinite/flat/tasks.GENERATED.json", "{}\n") }
		}, bot, nil, 1, "tasks.GENERATED.json", false},
		{"the bot moves the pin to a held version", movePin, bot, errors.New("1.2.0 is held: canary red"), 1, "held", true},
		{"the bot edits the launcher", func(t *testing.T) func(string) {
			return func(dir string) { write(t, dir, ".claudinite/launch", "#!/bin/sh\n# edited\n") }
		}, bot, nil, 1, "launcher", false},
		{"the bot changes the package", func(t *testing.T) func(string) {
			return func(dir string) {
				write(t, dir, ".claudinite/settings.yaml", strings.Replace(settingsBody("1.2.0", pin2), "cli-rc", "cli", 1))
			}
		}, bot, nil, 1, "engine.package", false},
		{"the bot's pack PR changes only the vendored packs", func(t *testing.T) func(string) {
			return func(dir string) { write(t, dir, ".claudinite/shared/packs/hello/RULES.md", "# hello 1.1\n") }
		}, bot, nil, 0, "", false},
		{"a person changes the vendored packs", func(t *testing.T) func(string) {
			return func(dir string) { write(t, dir, ".claudinite/shared/packs/hello/pack.json", "{}") }
		}, "someone", nil, 0, "", false},
		{"the bot moves the pin and a pack together", func(t *testing.T) func(string) {
			return func(dir string) { movePin(t)(dir); write(t, dir, ".claudinite/shared/packs/hello/RULES.md", "x") }
		}, bot, nil, 1, ".claudinite/shared/packs/hello/RULES.md", false},
		{"a person changes only a rule file", func(t *testing.T) func(string) {
			return func(dir string) { write(t, dir, "RULES.md", "- another rule\n") }
		}, "someone", nil, 0, "", false},
	}
	for _, c := range cases {
		dir := member(t, c.change(t))
		pc := &pinCheck{err: c.pinErr}
		code, out := runWorld(t, dir, c.author, pc, nil)
		if code != c.code || !strings.Contains(out, c.want) {
			t.Errorf("%s: exit %d, want %d; output lacks %q:\n%s", c.name, code, c.code, c.want, out)
		}
		if (len(pc.called) > 0) != c.checked {
			t.Errorf("%s: pin verified %d times", c.name, len(pc.called))
		}
		if c.checked && pc.called[0].Version != "1.2.0" {
			t.Errorf("%s: verified %+v", c.name, pc.called[0])
		}
	}
}

func TestFindingsDecideTheExit(t *testing.T) {
	dir := member(t, func(string) {})
	dep := findings.Finding{Class: findings.Deprecation, ID: "bin-ignore", Path: ".gitignore", Sentence: "move it"}
	if code, out := runWorld(t, dir, "someone", &pinCheck{}, []findings.Finding{dep}); code != 0 || !strings.Contains(out, "deprecation bin-ignore") {
		t.Errorf("deprecation only: %d\n%s", code, out)
	}
	brk := findings.Finding{Class: findings.Break, ID: "settings-file", Path: ".claudinite", Sentence: "add one"}
	if code, out := runWorld(t, dir, "someone", &pinCheck{}, []findings.Finding{dep, brk}); code != 1 || !strings.Contains(out, "break settings-file") {
		t.Errorf("with a break: %d\n%s", code, out)
	}
}

// retired is main's settings file still carrying the license block.
var retired = settingsBody("1.1.0", pin1) + "license:\n  plan: \"public\"\n"

func TestPinGuardLetsTheUpdateDropTheRetiredLicenseBlock(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		author string
		code   int
		want   string
	}{
		{"the bot moves the pin and drops the license block", settingsBody("1.2.0", pin2), bot, 0, ""},
		{"the bot moves the pin and keeps the license block", settingsBody("1.2.0", pin2) + "license:\n  plan: \"public\"\n", bot, 0, ""},
		{"the bot drops only the license block", settingsBody("1.1.0", pin1), bot, 0, ""},
		{"the bot moves the pin and changes the plan", settingsBody("1.2.0", pin2) + "license:\n  plan: \"private-repo\"\n", bot, 1, "pin-guard"},
		{"a person drops the license block, leaving the engine block alone", settingsBody("1.1.0", pin1), "someone", 0, ""},
		{"a person drops the license block and moves the pin", settingsBody("1.2.0", pin2), "someone", 1, "pin-guard"},
	}
	for _, c := range cases {
		dir := memberOn(t, retired, func(dir string) { write(t, dir, ".claudinite/settings.yaml", c.body) })
		code, out := runWorld(t, dir, c.author, &pinCheck{}, nil)
		if code != c.code || !strings.Contains(out, c.want) {
			t.Errorf("%s: exit %d, want %d; output lacks %q:\n%s", c.name, code, c.code, c.want, out)
		}
	}
}

// unadopted is a repo whose main holds no settings file and no launcher,
// checked out on a branch that the change function edits and commits.
func unadopted(t *testing.T, change func(dir string)) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	write(t, dir, ".claudinite-settings.json", "{}\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "base")
	git(t, dir, "checkout", "-q", "-b", "change")
	change(dir)
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "change")
	return dir
}

func TestPinGuardLetsAPersonAdoptAndEditTheirSettings(t *testing.T) {
	adopt := func(t *testing.T) func(string) {
		return func(dir string) {
			write(t, dir, ".claudinite/settings.yaml", settingsBody("1.2.0", pin2)+"packs:\n  declared:\n    - \"basics\"\n")
			write(t, dir, ".claudinite/launch", "#!/bin/sh\n")
		}
	}
	declare := func(t *testing.T) func(string) {
		return func(dir string) {
			write(t, dir, ".claudinite/settings.yaml", settingsBody("1.1.0", pin1)+"packs:\n  declared:\n    - \"basics\"\n")
		}
	}
	cases := []struct {
		name    string
		repo    func(t *testing.T) string
		pinErr  error
		code    int
		want    string
		checked bool
	}{
		{"a person's adoption PR writes the first pin and the launcher", func(t *testing.T) string { return unadopted(t, adopt(t)) }, nil, 0, "", true},
		{"a person's adoption PR whose first pin does not verify", func(t *testing.T) string { return unadopted(t, adopt(t)) }, errors.New("1.2.0 is revoked"), 1, "does not verify", true},
		{"a person declares a pack and leaves the engine block alone", func(t *testing.T) string { return member(t, declare(t)) }, nil, 0, "", false},
		{"a person declares a pack and moves the pin", func(t *testing.T) string {
			return member(t, func(dir string) {
				write(t, dir, ".claudinite/settings.yaml", settingsBody("1.2.0", pin2)+"packs:\n  declared:\n    - \"basics\"\n")
			})
		}, nil, 1, "pin-guard", false},
		{"a person edits the launcher of an adopted repo", func(t *testing.T) string {
			return member(t, func(dir string) { write(t, dir, ".claudinite/launch", "#!/bin/sh\n# edited\n") })
		}, nil, 1, "pin-guard", false},
	}
	for _, c := range cases {
		pc := &pinCheck{err: c.pinErr}
		code, out := runWorld(t, c.repo(t), "someone", pc, nil)
		if code != c.code || !strings.Contains(out, c.want) {
			t.Errorf("%s: exit %d, want %d; output lacks %q:\n%s", c.name, code, c.code, c.want, out)
		}
		if (len(pc.called) > 0) != c.checked {
			t.Errorf("%s: pin verified %d times", c.name, len(pc.called))
		}
	}
}
