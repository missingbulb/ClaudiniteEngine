package checks

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/build"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/checksdk"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/findings"
)

func service(t *testing.T) Service {
	t.Helper()
	return Service{Build: build.Config{CacheRoot: filepath.Join(t.TempDir(), "claudinite"), Engine: "1.61001.1", SDK: checksdk.Sources()}, Exe: "/nonexistent/cn"}
}

func helloRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	src := "../../../dev/release/verify/testdata/hello"
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		dst := filepath.Join(repo, ".claudinite/shared/packs/hello", rel)
		_ = os.MkdirAll(filepath.Dir(dst), 0o755)
		raw, _ := os.ReadFile(p)
		return os.WriteFile(dst, raw, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(repo, ".claudinite/settings.yaml"), []byte("engine:\n  version: \"1.1.0\"\npacks:\n  declared:\n    - hello\n"), 0o644)
	return repo
}

func TestRunTheHelloCheckByTag(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go")
	}
	t.Setenv("CLAUDINITE_CHECKS_NO_FETCH", "1")
	s := service(t)
	repo := helloRepo(t)
	coded := func(o Outcome) []string {
		var out []string
		for _, f := range o.Findings {
			if strings.HasPrefix(f.Name(), "hello/hello-") && f.Name() != "hello/hello-declared" && f.Name() != "hello/hello-declared-work" {
				out = append(out, f.Name())
			}
		}
		return out
	}
	o := s.RunAll(repo, "world", declared.Selection{Tags: []string{"world"}}, time.Minute, true, nil)
	if o.Err != nil || len(o.Errors) != 0 || len(coded(o)) != 0 || !strings.HasPrefix(o.Crumb, "[cn] checks world ok ") {
		t.Fatalf("%+v", o)
	}
	_ = os.WriteFile(filepath.Join(repo, "HELLO_FINDING"), nil, 0o644)
	o = s.RunAll(repo, "stop", declared.Selection{Tags: []string{"work"}}, time.Minute, false, nil)
	if got := coded(o); o.Err != nil || len(o.Errors) != 0 || len(got) != 1 || got[0] != "hello/hello-check" {
		t.Fatalf("%v %+v", got, o)
	}
	if o := s.RunAll(repo, "tag", declared.Selection{Tags: []string{"work"}, Pack: "other"}, time.Minute, false, nil); len(coded(o)) != 0 {
		t.Errorf("--pack other ran hello's check: %+v", o)
	}
	_ = os.Remove(filepath.Join(repo, "HELLO_FINDING"))
	_ = os.WriteFile(filepath.Join(repo, ".claudinite/settings.yaml"), []byte("engine:\n  version: \"1.1.0\"\npacks:\n  declared:\n    - id: hello\n      config:\n        probe: true\n"), 0o644)
	o = s.RunAll(repo, "world", declared.Selection{Tags: []string{"world"}}, time.Minute, true, nil)
	if got := coded(o); len(got) != 1 || got[0] != "hello/hello-config" || o.SDKCrumb == "" {
		t.Errorf("hello-config through the SDK: %v %+v", got, o)
	}
	listed, err := s.List(repo, time.Minute)
	var names []string
	for _, l := range listed {
		names = append(names, fmt.Sprintf("%s:%v", l.Check, l.Judge))
	}
	sort.Strings(names)
	if err != nil || strings.Join(names, " ") != "hello/hello-change:false hello/hello-check:false hello/hello-config:false hello/hello-judge:true" {
		t.Errorf("%+v %v", listed, err)
	}
}

func TestRunWithNoGoChecksIsOK(t *testing.T) {
	repo := t.TempDir()
	_ = os.MkdirAll(filepath.Join(repo, ".claudinite"), 0o755)
	_ = os.WriteFile(filepath.Join(repo, ".claudinite/settings.yaml"), []byte("engine:\n  version: \"1.1.0\"\n"), 0o644)
	res, crumb := service(t).Run(repo, "stop", []string{"work"}, "", time.Second, false, nil)
	if res.Err != nil || len(res.Findings) != 0 || !strings.HasPrefix(crumb, "[cn] checks stop ok ") {
		t.Errorf("%+v %q", res, crumb)
	}
}

func TestRunTimesOutWhenTheBuildNeverComes(t *testing.T) {
	s := service(t)
	repo := helloRepo(t)
	s.Exe = "/bin/true"
	res, crumb := s.Run(repo, "stop", []string{"work"}, "", 200*time.Millisecond, false, nil)
	if res.Err == nil || !strings.HasPrefix(crumb, "[cn] checks stop timeout ") {
		t.Errorf("%+v %q", res, crumb)
	}
}

func TestARepoWithoutSettingsRunsNothing(t *testing.T) {
	res, crumb := service(t).Run(t.TempDir(), "check", []string{"world"}, "", time.Second, true, nil)
	if res.Err != nil || len(res.Findings) != 0 || !strings.HasPrefix(crumb, "[cn] checks check ok ") {
		t.Errorf("%+v %q", res, crumb)
	}
}

// Judge runs the declared guards in this process and the coded judges in
// the checks binary, the latter only once it is built and only for an
// event the judges manifest names; a judge that cannot run lets the call
// through with an error line.
func TestJudgeRunsGuardsAndCodedJudges(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go")
	}
	s := service(t)
	repo := helloRepo(t)
	bash := func(cmd string) Call { return Call{Tool: "Bash", Input: []byte(`{"command":"` + cmd + `"}`)} }
	soon := func() time.Time { return time.Now().Add(5 * time.Second) }
	v := s.Judge(repo, "pre-tool-use", bash("echo HELLO_JUDGE"), nil, soon())
	if len(v.Blocks) != 0 || len(v.Errors) != 1 || !strings.Contains(v.Errors[0], "not built") {
		t.Errorf("before the build: %+v", v)
	}
	if _, err := s.BuildNow(repo, "", true, time.Minute); err != nil {
		t.Fatal(err)
	}
	v = s.Judge(repo, "pre-tool-use", bash("echo HELLO_JUDGE"), nil, soon())
	if len(v.Errors) != 0 || len(v.Blocks) != 1 || !strings.HasPrefix(v.Blocks[0], "Blocked by hello-judge: the command names HELLO_JUDGE") {
		t.Errorf("judge: %+v", v)
	}
	v = s.Judge(repo, "pre-tool-use", bash("echo HELLO_GUARD"), nil, soon())
	if len(v.Blocks) != 1 || !strings.HasPrefix(v.Blocks[0], "Blocked by hello-guard: the command names HELLO_GUARD. ") {
		t.Errorf("guard: %+v", v)
	}
	if v := s.Judge(repo, "pre-tool-use", bash("ls"), nil, soon()); len(v.Blocks)+len(v.Advice)+len(v.Errors) != 0 {
		t.Errorf("ls: %+v", v)
	}
	if v := s.Judge(repo, "post-tool-use", bash("echo HELLO_JUDGE"), nil, soon()); len(v.Blocks)+len(v.Advice)+len(v.Errors) != 0 {
		t.Errorf("an event with no judge: %+v", v)
	}
	if v := s.Judge(repo, "pre-tool-use", bash("echo HELLO_JUDGE"), nil, time.Now().Add(-time.Second)); len(v.Blocks) != 0 || len(v.Errors) != 1 {
		t.Errorf("past the deadline: %+v", v)
	}
	settings := filepath.Join(repo, ".claudinite/settings.yaml")
	raw, _ := os.ReadFile(settings)
	_ = os.WriteFile(settings, append(raw, []byte("checks:\n  rules:\n    hello-judge: advise\n")...), 0o644)
	v = s.Judge(repo, "pre-tool-use", bash("echo HELLO_JUDGE"), nil, soon())
	if len(v.Blocks) != 0 || len(v.Advice) != 1 || !strings.HasPrefix(v.Advice[0], "[claudinite hello-judge] the command names HELLO_JUDGE") {
		t.Errorf("advise: %+v", v)
	}
	_ = os.WriteFile(settings, append(raw, []byte("checks:\n  rules:\n    hello-judge: \"off\"\n")...), 0o644)
	if v := s.Judge(repo, "pre-tool-use", bash("echo HELLO_JUDGE"), nil, soon()); len(v.Blocks)+len(v.Advice) != 0 {
		t.Errorf("off: %+v", v)
	}
}

const acmeChecks = `package checks

import (
	"encoding/json"
	"strings"

	"claudinite.com/checksdk"
)

func init() {
	checksdk.Register(checksdk.Check{
		ID: "acme-grace", Tags: []string{"world"}, Since: "@@TODAY@@", Why: "a new check",
		Run: func(repo checksdk.Repo) []checksdk.Finding {
			return []checksdk.Finding{{Path: "README.md", Line: 1, Sentence: "graced", Fix: "fix it"}}
		},
	})
	checksdk.Register(checksdk.Check{
		ID: "acme-sdk", Tags: []string{"world"}, OnFail: "advise",
		Run: func(repo checksdk.Repo) []checksdk.Finding {
			var cfg struct{ Probe bool }
			_ = json.Unmarshal(repo.PackConfig("acme-pack"), &cfg)
			var out []checksdk.Finding
			for _, f := range repo.Files() {
				if strings.HasPrefix(f, "ACME_") && cfg.Probe {
					out = append(out, checksdk.Finding{Path: f, Line: 2, Sentence: "an acme file", Fix: "remove it"})
				}
			}
			return out
		},
	})
}
`

const localChecks = `package checks

import "claudinite.com/checksdk"

func init() {
	checksdk.Register(checksdk.Check{
		ID: "local-check", Tags: []string{"world"},
		Run: func(repo checksdk.Repo) []checksdk.Finding {
			if !repo.Exists("HELLO_LOCAL") {
				return nil
			}
			return []checksdk.Finding{{Path: "HELLO_LOCAL", Sentence: "the local probe"}}
		},
	})
}
`

func acmeRepo(t *testing.T, rules string) string {
	t.Helper()
	repo := t.TempDir()
	files := map[string]string{
		".claudinite/shared/packs/acme-pack/pack.json":      `{"version":"1.0","minEngineVersion":"1.61001.1"}`,
		".claudinite/shared/packs/acme-pack/checks/acme.go": strings.ReplaceAll(acmeChecks, "@@TODAY@@", time.Now().UTC().Format("2006-01-02")),
		".claudinite/local/packs/probe/pack.json":           `{}`,
		".claudinite/local/packs/probe/checks/local.go":     localChecks,
		".claudinite/temp/packs/copied/pack.json":           `{}`,
		".claudinite/temp/packs/copied/checks/broken.go":    "package checks\n\nthis does not compile\n",
		".claudinite/settings.yaml":                         "engine:\n  version: \"1.61001.1\"\npacks:\n  declared:\n    - id: acme-pack\n      config:\n        probe: true\n    - local/probe\n" + rules,
		"README.md":                                         "hi\n",
		"ACME_ONE":                                          "x\n",
		"HELLO_LOCAL":                                       "x\n",
	}
	for rel, text := range files {
		p := filepath.Join(repo, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "base"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	return repo
}

func byName(fs []findings.Finding) map[string]findings.Finding {
	out := map[string]findings.Finding{}
	for _, f := range fs {
		out[f.Name()] = f
	}
	return out
}

// Coded findings carry their line, why and fix; a new blocking check only
// advises inside its grace window; the member's overrides name a check by
// <pack>/<id> or its bare id; a local pack's checks build and are named
// local/<name>/<id>; a temp pack's are never built; the coded checks' SDK
// calls are answered from the run's own walk and config.
func TestRunAllOverCodedFindings(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go")
	}
	t.Setenv("CLAUDINITE_CHECKS_NO_FETCH", "1")
	s := service(t)
	repo := acmeRepo(t, "checks:\n  rules:\n    acme-pack/acme-sdk: block\n")
	o := s.RunAll(repo, "world", declared.Selection{Tags: []string{"world"}}, time.Minute, true, nil)
	if o.Err != nil || len(o.Errors) != 0 {
		t.Fatalf("%v %v", o.Err, o.Errors)
	}
	got := byName(o.Findings)
	grace := got["acme-pack/acme-grace"]
	if grace.Class != findings.Advisory || grace.Line != 1 || grace.Why != "a new check" || !strings.Contains(grace.Fix, "fix it (grace: added ") {
		t.Errorf("grace %+v", grace)
	}
	if sdk := got["acme-pack/acme-sdk"]; sdk.Class != findings.Coded || sdk.Path != "ACME_ONE" || sdk.Line != 2 || sdk.Fix != "remove it" {
		t.Errorf("sdk (advise, set to block by its pack-qualified name) %+v", sdk)
	}
	if local := got["local/probe/local-check"]; local.Class != findings.Coded || local.Path != "HELLO_LOCAL" {
		t.Errorf("local %+v", local)
	}
	if !strings.HasPrefix(o.SDKCrumb, "[cn] sdk world ok ") || o.Calls["config.pack"] != 1 || o.Calls["tree.files"] != 1 {
		t.Errorf("sdk crumb %q calls %v", o.SDKCrumb, o.Calls)
	}
	listed, err := s.ListAll(repo, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var rows []string
	for _, l := range listed {
		rows = append(rows, l.Name()+" "+l.Kind+" "+l.OnFail+" "+l.Since)
	}
	today := time.Now().UTC().Format("2006-01-02")
	joined := "|" + strings.Join(rows, "|") + "|"
	for _, want := range []string{"acme-pack/acme-grace coded block " + today, "acme-pack/acme-sdk coded advise ", "local/probe/local-check coded block "} {
		if !strings.Contains(joined, "|"+want+"|") {
			t.Errorf("list lacks %q: %v", want, rows)
		}
	}
	if strings.Contains(joined, "copied") {
		t.Errorf("a temp pack's checks were built: %v", rows)
	}

	bare := acmeRepo(t, "checks:\n  rules:\n    acme-sdk: \"off\"\n    local-check: advise\n")
	o = s.RunAll(bare, "world", declared.Selection{Tags: []string{"world"}}, time.Minute, true, nil)
	got = byName(o.Findings)
	if _, ok := got["acme-pack/acme-sdk"]; ok {
		t.Errorf("a bare-id off left the finding: %v", o.Findings)
	}
	if got["local/probe/local-check"].Class != findings.Advisory {
		t.Errorf("a bare-id advise: %+v", got["local/probe/local-check"])
	}
	if o := s.RunAll(bare, "pack", declared.Selection{Pack: "local/probe"}, time.Minute, true, nil); len(o.Findings) != 1 || o.Findings[0].Name() != "local/probe/local-check" {
		t.Errorf("--pack local/probe: %v", o.Findings)
	}
	for _, key := range []string{"local/probe/local-check", "local-check"} {
		off := acmeRepo(t, "checks:\n  rules:\n    "+key+": \"off\"\n")
		o := s.RunAll(off, "world", declared.Selection{Tags: []string{"world"}}, time.Minute, true, nil)
		if _, ok := byName(o.Findings)["local/probe/local-check"]; ok {
			t.Errorf("%s: off left the local finding: %v", key, o.Findings)
		}
	}
}

// The pack-owned built-ins list beside the coded and declared checks,
// under their pack and since, and --pack <canon pack> runs them.
func TestBuiltinsListAndRunUnderTheirPack(t *testing.T) {
	t.Setenv("CLAUDINITE_CHECKS_NO_FETCH", "1")
	s := service(t)
	repo := t.TempDir()
	for rel, body := range map[string]string{
		".claudinite/settings.yaml":                               "engine:\n  version: \"1.1.0\"\npacks:\n  declared:\n    - claudinite-lifecycle\n    - claudinite-growth\n",
		".claudinite/shared/packs/claudinite-lifecycle/pack.json": "{\"version\": \"1\", \"minEngineVersion\": \"1.61001.1\"}\n",
		".claudinite/shared/packs/claudinite-growth/pack.json":    "{\"version\": \"1\", \"minEngineVersion\": \"1.61001.1\", \"requires\": [\"claudinite-lifecycle\"]}\n",
		".claudinite/local/packs/mine/pack.json":                  "{}\n",
		".claudinite/local/packs/mine/tasks/nightly/task.md":      "Run `bash gather.sh`.\n",
	} {
		p := filepath.Join(repo, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	listed, err := s.ListAll(repo, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var rows []string
	for _, l := range listed {
		rows = append(rows, l.Name()+" "+l.Kind+" "+strings.Join(l.Tags, ",")+" "+l.OnFail+" "+l.Since)
	}
	joined := "|" + strings.Join(rows, "|") + "|"
	for _, want := range []string{
		"claudinite-growth/provenance-integrity builtin world,builtin,claudinite-growth block 2026-09-20",
		"claudinite-growth/provenance-change-recorded builtin work,builtin,claudinite-growth block 2026-09-20",
		"claudinite-lifecycle/shared-tree-immutable builtin work,builtin,claudinite-lifecycle advise 2026-09-06",
	} {
		if !strings.Contains(joined, "|"+want+"|") {
			t.Errorf("list lacks %q: %v", want, rows)
		}
	}
	o := s.RunAll(repo, "pack", declared.Selection{Pack: "claudinite-growth"}, time.Minute, true, nil)
	if _, ok := byName(o.Findings)["claudinite-growth/routine-structure"]; !ok || o.Err != nil {
		t.Errorf("--pack claudinite-growth did not run routine-structure: %v %v", o.Err, o.Findings)
	}
	o = s.RunAll(repo, "pack", declared.Selection{Pack: "claudinite-lifecycle"}, time.Minute, true, nil)
	if _, ok := byName(o.Findings)["claudinite-growth/routine-structure"]; ok {
		t.Errorf("--pack claudinite-lifecycle ran a growth built-in: %v", o.Findings)
	}
}

// ListBuilt never builds: before a build it is ErrNotBuilt, and
// ListAllBuilt still names the declared checks; after one it lists.
func TestListBuiltNeverBuilds(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go")
	}
	t.Setenv("CLAUDINITE_CHECKS_NO_FETCH", "1")
	s := service(t)
	repo := helloRepo(t)
	if _, err := s.ListBuilt(repo); err != ErrNotBuilt {
		t.Fatalf("before a build: %v", err)
	}
	if entries, _ := os.ReadDir(s.Build.CacheRoot); len(entries) != 0 {
		t.Errorf("ListBuilt built: %v", entries)
	}
	rows, err := s.ListAllBuilt(repo)
	if err != ErrNotBuilt || len(rows) == 0 {
		t.Errorf("ListAllBuilt before a build: %d rows, %v", len(rows), err)
	}
	if _, err := s.List(repo, time.Minute); err != nil {
		t.Fatal(err)
	}
	if listed, err := s.ListBuilt(repo); err != nil || len(listed) == 0 {
		t.Errorf("after a build: %v %v", listed, err)
	}
}
