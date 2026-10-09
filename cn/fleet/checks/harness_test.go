package checks

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/builtin"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/findings"
)

// gone, as a file's content in a change, deletes the file.
const gone = "\x00gone"

const settingsYAML = `engine:
  version: "0.0.0"
  manifest: "sha512-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="
packs:
  declared:
    - claudinite-lifecycle
    - claudinite-growth
    - local/mypack
`

// repo is a member declaring the two folded packs and local/mypack, with
// base committed on main and, when change is not nil, change committed on
// branch change (or branch, when set) under message.
type repo struct {
	settings     string
	base, change map[string]string
	message      string
	untracked    map[string]string
	branch       string
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@x", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if body == gone {
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// Every test's repo is offline, so no check fetches; set once, the tests can run in parallel.
func TestMain(m *testing.M) {
	if err := os.Setenv("CLAUDINITE_CHECKS_NO_FETCH", "1"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func (r repo) build(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	s := r.settings
	if s == "" {
		s = settingsYAML
	}
	write(t, dir, map[string]string{
		".claudinite/settings.yaml":                               s,
		".claudinite/shared/packs/claudinite-lifecycle/pack.json": "{\"version\": \"1\"}\n",
		".claudinite/shared/packs/claudinite-growth/pack.json":    "{\"version\": \"1\", \"requires\": [\"claudinite-lifecycle\"]}\n",
		".claudinite/local/packs/mypack/pack.json":                "{}\n",
	})
	write(t, dir, r.base)
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "base")
	if r.change != nil {
		branch := r.branch
		if branch == "" {
			branch = "change"
		}
		git(t, dir, "checkout", "-q", "-b", branch)
		write(t, dir, r.change)
		msg := r.message
		if msg == "" {
			msg = "change"
		}
		git(t, dir, "add", "-A")
		git(t, dir, "commit", "-q", "--allow-empty", "-m", msg)
	}
	write(t, dir, r.untracked)
	return dir
}

// pastGrace is a clock past every built-in's grace window, so a finding
// carries the class its check gives it.
var pastGrace = time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

// run runs check id at its scope over the repo, the member's
// configuration applied, and returns its findings sorted by place.
func (r repo) run(t *testing.T, id string) []findings.Finding {
	t.Helper()
	dir := r.build(t)
	set, err := declared.LoadSet(dir, "0.0.0", builtin.All()...)
	if err != nil {
		t.Fatal(err)
	}
	var b declared.Builtin
	for _, x := range set.Builtins {
		if x.ID == id {
			b = x
		}
	}
	if b.ID == "" {
		t.Fatalf("%s is not active", id)
	}
	fs, _ := set.Run(declared.Selection{Tags: b.Tags[:1], Pack: b.Pack}, pastGrace, nil)
	var out []findings.Finding
	for _, f := range declared.ApplyConfig(fs, set.Config) {
		if f.ID == id || f.ID == "checks-run" {
			out = append(out, f)
		}
	}
	sort.SliceStable(out, func(i, k int) bool {
		if out[i].Path != out[k].Path {
			return out[i].Path < out[k].Path
		}
		return out[i].Line < out[k].Line
	})
	return out
}

// want is one expected finding: its place, and patterns its sentence and
// fix match; advise marks an advisory.
type want struct {
	path      string
	line      int
	what, fix string
	advise    bool
}

func expect(t *testing.T, got []findings.Finding, wants ...want) {
	t.Helper()
	if len(got) != len(wants) {
		var b strings.Builder
		findings.Print(&b, got)
		t.Fatalf("%d findings, want %d:\n%s", len(got), len(wants), b.String())
	}
	for i, w := range wants {
		f := got[i]
		if f.Path != w.path || f.Line != w.line {
			t.Errorf("finding %d at %s:%d, want %s:%d (%s)", i, f.Path, f.Line, w.path, w.line, f.Sentence)
		}
		if w.what != "" && !regexp.MustCompile(w.what).MatchString(f.Sentence) {
			t.Errorf("finding %d says %q, want /%s/", i, f.Sentence, w.what)
		}
		if w.fix != "" && !regexp.MustCompile(w.fix).MatchString(f.Fix) {
			t.Errorf("finding %d fixes %q, want /%s/", i, f.Fix, w.fix)
		}
		if (f.Class == findings.Advisory) != w.advise {
			t.Errorf("finding %d is %s, want advise=%v", i, f.Class, w.advise)
		}
	}
}

