package checks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/checks/build"
)

func service(t *testing.T) Service {
	t.Helper()
	raw, err := os.ReadFile("../checksdk/checksdk.go")
	if err != nil {
		t.Fatal(err)
	}
	return Service{Build: build.Config{CacheRoot: filepath.Join(t.TempDir(), "claudinite"), Engine: "61001.1.0", SDK: map[string][]byte{"checksdk.go": raw}}, Exe: "/nonexistent/cn"}
}

func helloRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	src := "../release/testdata/hello"
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
	s := service(t)
	repo := helloRepo(t)
	res, crumb := s.Run(repo, "world", []string{"world"}, "", time.Minute, true)
	if res.Err != nil || len(res.Findings) != 0 || !strings.HasPrefix(crumb, "[cn] checks world ok ") {
		t.Fatalf("%+v %q", res, crumb)
	}
	_ = os.WriteFile(filepath.Join(repo, "HELLO_FINDING"), nil, 0o644)
	res, _ = s.Run(repo, "stop", []string{"work"}, "", time.Minute, false)
	if res.Err != nil || len(res.Findings) != 1 || res.Findings[0].Check != "hello/hello-check" || !res.Blocking() {
		t.Fatalf("%+v", res)
	}
	if res, _ := s.Run(repo, "tag", []string{"work"}, "other", time.Minute, false); len(res.Findings) != 0 {
		t.Errorf("--pack other ran hello's check: %+v", res)
	}
	listed, err := s.List(repo, time.Minute)
	if err != nil || len(listed) != 2 || listed[0].Check != "hello/hello-check" || listed[1].Check != "hello/hello-judge" || !listed[1].Judge {
		t.Errorf("%+v %v", listed, err)
	}
}

func TestRunWithNoGoChecksIsOK(t *testing.T) {
	repo := t.TempDir()
	_ = os.MkdirAll(filepath.Join(repo, ".claudinite"), 0o755)
	_ = os.WriteFile(filepath.Join(repo, ".claudinite/settings.yaml"), []byte("engine:\n  version: \"1.1.0\"\n"), 0o644)
	res, crumb := service(t).Run(repo, "stop", []string{"work"}, "", time.Second, false)
	if res.Err != nil || len(res.Findings) != 0 || !strings.HasPrefix(crumb, "[cn] checks stop ok ") {
		t.Errorf("%+v %q", res, crumb)
	}
}

func TestRunTimesOutWhenTheBuildNeverComes(t *testing.T) {
	s := service(t)
	repo := helloRepo(t)
	s.Exe = "/bin/true"
	res, crumb := s.Run(repo, "stop", []string{"work"}, "", 200*time.Millisecond, false)
	if res.Err == nil || !strings.HasPrefix(crumb, "[cn] checks stop timeout ") {
		t.Errorf("%+v %q", res, crumb)
	}
}

func TestARepoWithoutSettingsRunsNothing(t *testing.T) {
	res, crumb := service(t).Run(t.TempDir(), "check", []string{"world"}, "", time.Second, true)
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
