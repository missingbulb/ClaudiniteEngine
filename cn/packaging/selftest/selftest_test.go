package selftest

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/workflows"
)

var events = []string{"session-start", "pre-tool-use", "post-tool-use", "user-prompt-submit", "stop", "session-end"}

func machine(t *testing.T) Input {
	t.Helper()
	return Input{Version: "0.0.0", Platform: "linux-amd64", CacheRoot: filepath.Join(t.TempDir(), "claudinite"),
		RootIDs: []string{"aaaa000011112222", "bbbb000011112222"}, Now: time.Now(), HookEvents: events}
}

func TestSelftestReportsAndPasses(t *testing.T) {
	in := machine(t)
	crashes := filepath.Join(in.CacheRoot, "crashes")
	if err := os.MkdirAll(crashes, 0o700); err != nil {
		t.Fatal(err)
	}
	for i, age := range []time.Duration{time.Hour, 6 * 24 * time.Hour, 8 * 24 * time.Hour} {
		p := filepath.Join(crashes, "x-"+string(rune('1'+i))+".txt")
		_ = os.WriteFile(p, []byte("x"), 0o600)
		_ = os.Chtimes(p, in.Now.Add(-age), in.Now.Add(-age))
	}
	var out bytes.Buffer
	if code := Selftest(&out, in); code != 0 {
		t.Fatalf("exit %d\n%s", code, out.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if lines[0] != "version 0.0.0" || len(lines) != 1+len(Names) {
		t.Fatalf("want the version line and one line per probe:\n%s", out.String())
	}
	for i, name := range Names {
		if f := strings.Fields(lines[i+1]); len(f) < 2 || strings.TrimSuffix(f[1], ":") != name {
			t.Errorf("line %d is not the %s probe: %s", i+2, name, lines[i+1])
		}
	}
	for _, want := range []string{"ok binary: linux-amd64", "ok cache: " + in.CacheRoot + " writable", "ok roots: aaaa000011112222 bbbb000011112222",
		"skip mount: no --repo", "ok crashes: 2 in 7 days"} {
		if !strings.Contains(out.String(), want+"\n") {
			t.Errorf("selftest output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestSelftestFailsOnUnwritableCache(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(file, nil, 0o600)
	in := machine(t)
	in.CacheRoot = filepath.Join(file, "claudinite")
	var out bytes.Buffer
	if code := Selftest(&out, in); code != 1 || !reflect.DeepEqual(Failed(out.String()), []string{"cache"}) {
		t.Fatalf("exit %d\n%s", code, out.String())
	}
}

func TestSelftestFailsOnBadRoots(t *testing.T) {
	in := machine(t)
	in.RootIDs, in.RootsErr = nil, errors.New("bad embed")
	var out bytes.Buffer
	if code := Selftest(&out, in); code != 1 || !strings.Contains(out.String(), "fail roots: invalid: bad embed\n") {
		t.Fatalf("exit %d\n%s", code, out.String())
	}
}

// memberLines is the member probes' answers as "<probe>=<status>".
func memberLines(in Input) []string {
	var out []string
	for _, p := range Run(in) {
		switch p.Name {
		case "mount", "packs", "hooks", "skills", "scheduler", "rules":
			out = append(out, p.Name+"="+string(p.Status))
		}
	}
	return out
}

// Every shape an earlier release accepted passes, and selftest.txt names
// each one's member probe answers.
func TestEveryShapePasses(t *testing.T) {
	const shapes = "../verify/testdata/shapes"
	raw, err := os.ReadFile(filepath.Join(shapes, "selftest.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for _, l := range strings.Split(string(raw), "\n") {
		if l = strings.TrimSpace(l); l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		name, rest, _ := strings.Cut(l, " ")
		want[name] = rest
	}
	dirs, _ := filepath.Glob(filepath.Join(shapes, "v*-*"))
	if len(dirs) == 0 {
		t.Fatal("no shapes")
	}
	for _, d := range dirs {
		name := filepath.Base(d)
		in := machine(t)
		in.Repo = d
		var out bytes.Buffer
		if code := Selftest(&out, in); code != 0 {
			t.Errorf("%s fails selftest:\n%s", name, out.String())
		}
		got := strings.Join(memberLines(in), " ")
		if want[name] != got {
			t.Errorf("selftest.txt: %s %s, the shape answers %s", name, want[name], got)
		}
		delete(want, name)
	}
	for name := range want {
		t.Errorf("selftest.txt names %s, which is no shape", name)
	}
}

func copyShape(t *testing.T, name string) string {
	t.Helper()
	dst := t.TempDir()
	src := filepath.Join("../verify/testdata/shapes", name)
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), raw, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func probe(t *testing.T, repo, name string) Probe {
	t.Helper()
	in := machine(t)
	in.Repo = repo
	for _, p := range Run(in) {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("no probe %s", name)
	return Probe{}
}

func edit(t *testing.T, path, old, new string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), old) {
		t.Fatalf("%s holds no %q", path, old)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(raw), old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEachMemberProbeFails(t *testing.T) {
	const shape = "v7-update-as-task"
	cases := []struct {
		probe, detail string
		break_        func(t *testing.T, repo string)
	}{
		{"mount", ".claudinite/shared/packs lacks basics", func(t *testing.T, repo string) {
			if err := os.RemoveAll(filepath.Join(repo, ".claudinite/shared/packs/basics")); err != nil {
				t.Fatal(err)
			}
		}},
		{"packs", "basics: ", func(t *testing.T, repo string) {
			edit(t, filepath.Join(repo, ".claudinite/shared/packs/basics/pack.json"), "{", `{"skills": ["absent"],`)
		}},
		{"packs", "local/mine: pack.mjs is a module manifest", func(t *testing.T, repo string) {
			edit(t, filepath.Join(repo, ".claudinite/settings.yaml"), "  declared:\n", "  declared:\n    - local/mine\n")
			write(t, filepath.Join(repo, ".claudinite/local/packs/mine/pack.mjs"), "export default {};\n")
		}},
		{"hooks", "PreToolUse → cn hook pre-tool-uses, an event this binary does not answer", func(t *testing.T, repo string) {
			edit(t, filepath.Join(repo, ".claude/settings.json"), "cn hook pre-tool-use", "cn hook pre-tool-uses")
		}},
		{"skills", "basics/probe: link → ../absent/SKILL.md missing", func(t *testing.T, repo string) {
			write(t, filepath.Join(repo, ".claudinite/shared/packs/basics/skills/probe/SKILL.md"),
				"See [the sibling](../sibling/SKILL.md#top), [the web](https://x.test/a) and [a gone one](../absent/SKILL.md).\n")
			write(t, filepath.Join(repo, ".claudinite/shared/packs/basics/skills/sibling/SKILL.md"), "here\n")
		}},
		{"scheduler", "scheduler without executor", func(t *testing.T, repo string) {
			if err := os.Remove(filepath.Join(repo, ".github/workflows/claudinite-executor.yml")); err != nil {
				t.Fatal(err)
			}
		}},
		{"rules", "is stale", func(t *testing.T, repo string) {
			write(t, filepath.Join(repo, ".claudinite/cache/claudinite-rules.GENERATED.md"), "stale\n")
		}},
	}
	for _, c := range cases {
		t.Run(c.probe+" "+c.detail, func(t *testing.T) {
			repo := copyShape(t, shape)
			if p := probe(t, repo, c.probe); p.Status == Fail {
				t.Fatalf("%s fails before the break: %s", c.probe, p.Detail)
			}
			c.break_(t, repo)
			p := probe(t, repo, c.probe)
			if p.Status != Fail || !strings.Contains(p.Detail, c.detail) {
				t.Errorf("%s answers %s %q, want fail containing %q", c.probe, p.Status, p.Detail, c.detail)
			}
		})
	}
}

// A skill showing link syntax as an example, in an inline code span or a
// fenced block, links to nothing; a real link beside it still does.
func TestTheSkillsProbeSkipsLinksInCode(t *testing.T) {
	repo := copyShape(t, "v7-update-as-task")
	file := filepath.Join(repo, ".claudinite/shared/packs/basics/skills/probe/SKILL.md")
	write(t, file, "Write `![Figure 1](fig1.png)` for an image.\n\n```md\n[the report](report.md)\n```\n")
	if p := probe(t, repo, "skills"); p.Status != OK {
		t.Fatalf("example links: %s %q", p.Status, p.Detail)
	}
	write(t, file, "Write `![Figure 1](fig1.png)` for an image, or see [the guide](guide.md).\n")
	if p := probe(t, repo, "skills"); p.Status != Fail || !strings.Contains(p.Detail, "basics/probe: link → guide.md missing") || strings.Contains(p.Detail, "fig1") {
		t.Fatalf("a real link beside an example: %s %q", p.Status, p.Detail)
	}
}

func TestTheSchedulerProbeNotesThePlaceholderCron(t *testing.T) {
	repo := copyShape(t, "v7-update-as-task")
	if p := probe(t, repo, "scheduler"); p.Status != OK || strings.Contains(p.Detail, "placeholder") {
		t.Fatalf("hashed cron: %s %q", p.Status, p.Detail)
	}
	write(t, filepath.Join(repo, ".github/workflows/claudinite-scheduler.yml"), string(workflows.Templates()["claudinite-scheduler.yml"]))
	if p := probe(t, repo, "scheduler"); p.Status != OK || !strings.Contains(p.Detail, "placeholder") {
		t.Fatalf("placeholder cron: %s %q", p.Status, p.Detail)
	}
}

func TestFailedReadsTheReport(t *testing.T) {
	report := "version 1.2.0\nok binary: x\nfail hooks: a\nskip rules: b\nfail scheduler: c\n"
	if got := Failed(report); !reflect.DeepEqual(got, []string{"hooks", "scheduler"}) {
		t.Fatal(got)
	}
	if got := Failed("version 1.2.0\nok binary: x\n"); got != nil {
		t.Fatal(got)
	}
}
