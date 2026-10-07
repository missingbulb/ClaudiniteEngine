package checks

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeBuilder is a cn whose `check build` takes a moment, then leaves
// the key's record and a binary, as a real detached build does.
func fakeBuilder(t *testing.T, s Service, key string, ok bool) string {
	t.Helper()
	dir := s.Build.Dir(key)
	okJSON, failed := "true", ""
	if !ok {
		okJSON, failed = "false", "touch "+filepath.Join(dir, "build.failed")+"\nexit 1\n"
	}
	tmp := t.TempDir()
	script := filepath.Join(tmp, "fake-cn")
	started, ended := filepath.Join(tmp, "started"), filepath.Join(tmp, "ended")
	// A run can start a second build beside the test's own; the temp dirs go
	// only once every one of them has exited.
	t.Cleanup(func() {
		for i := 0; i < 400 && lineCount(started) != lineCount(ended); i++ {
			time.Sleep(10 * time.Millisecond)
		}
	})
	body := "#!/bin/sh\necho >> " + started + "\ntrap 'echo >> " + ended + "' EXIT\nsleep 0.3\nmkdir -p " + dir + "\n" +
		"printf '{\"atMs\": %s, \"ms\": 1234, \"ok\": " + okJSON + "}' $(date +%s%3N) > " + filepath.Join(dir, "build.json") + "\n" +
		failed +
		"printf '#!/bin/sh\\nexit 1\\n' > " + s.Build.Binary(key) + "\nchmod 555 " + s.Build.Binary(key) + "\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

func lineCount(path string) int {
	raw, _ := os.ReadFile(path)
	return strings.Count(string(raw), "\n")
}

func collect(s *Service) *[]string {
	var lines []string
	s.Timing = func(l string) { lines = append(lines, l) }
	return &lines
}

func hasPrefix(lines []string, p string) bool {
	for _, l := range lines {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	return false
}

// SessionStart says whether it found the binary or started a build; the
// session's first run that finds the build done reports it once, and a
// run reports a wait only when the binary was not ready on arrival.
func TestTheBuildAndItsWaitsReportPerSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	s := service(t)
	repo := helloRepo(t)
	key, _, err := s.Key(repo)
	if err != nil || key == "" {
		t.Fatal(key, err)
	}
	s.Exe = fakeBuilder(t, s, key, true)
	s.Session = "s1"
	lines := collect(&s)
	line, err := s.Start(repo)
	if err != nil || !strings.HasPrefix(line, "[cn] build started ok ") {
		t.Fatalf("start: %q %v", line, err)
	}
	s.Run(repo, "stop", []string{"work"}, "", 10*time.Second, false, nil)
	if !hasPrefix(*lines, "[cn] buildwait stop ok ") || !hasPrefix(*lines, "[cn] build compiled ok 1234ms") || len(*lines) != 2 {
		t.Fatalf("first stop: %q", *lines)
	}
	*lines = nil
	s.Run(repo, "stop", []string{"work"}, "", 10*time.Second, false, nil)
	if len(*lines) != 0 {
		t.Errorf("a second stop reported again: %q", *lines)
	}
	s.Session = "s2"
	if line, err := s.Start(repo); err != nil || !strings.HasPrefix(line, "[cn] build cached ok ") {
		t.Errorf("a later session: %q %v", line, err)
	}
	s.Run(repo, "stop", []string{"work"}, "", 10*time.Second, false, nil)
	if len(*lines) != 0 {
		t.Errorf("a cached session reported: %q", *lines)
	}
}

func TestAFailedBuildReportsItsFailureAndTheWait(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	s := service(t)
	repo := helloRepo(t)
	key, _, _ := s.Key(repo)
	s.Exe = fakeBuilder(t, s, key, false)
	_ = os.Remove(s.Build.Binary(key))
	s.Session = "s1"
	lines := collect(&s)
	if _, err := s.Start(repo); err != nil {
		t.Fatal(err)
	}
	// The fake leaves a binary after failing; a real failed build leaves
	// none, so remove it the moment it lands.
	go func() {
		for i := 0; i < 200; i++ {
			if err := os.Remove(s.Build.Binary(key)); err == nil {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	s.Run(repo, "stop", []string{"work"}, "", 5*time.Second, false, nil)
	if !hasPrefix(*lines, "[cn] buildwait stop error ") || !hasPrefix(*lines, "[cn] build compiled error 1234ms") {
		t.Errorf("%q", *lines)
	}
}

func TestAWaitThatRunsOutReportsTimeout(t *testing.T) {
	s := service(t)
	repo := helloRepo(t)
	s.Exe = "/bin/true"
	s.Session = "s1"
	lines := collect(&s)
	s.Run(repo, "stop", []string{"work"}, "", 200*time.Millisecond, false, nil)
	if !hasPrefix(*lines, "[cn] buildwait stop timeout ") || hasPrefix(*lines, "[cn] build compiled") {
		t.Errorf("%q", *lines)
	}
}

// A command that builds in the foreground reports what it waited, named
// by its caller, and nothing once the binary is there.
func TestAForegroundBuildReportsItsWait(t *testing.T) {
	s := service(t)
	repo := helloRepo(t)
	s.Build.Go = filepath.Join(t.TempDir(), "no-go-here")
	s.Caller = "check"
	lines := collect(&s)
	if _, err := s.BuildNow(repo, "", true, time.Second); err == nil {
		t.Fatal("built without go")
	}
	if !hasPrefix(*lines, "[cn] buildwait check error ") || len(*lines) != 1 {
		t.Errorf("%q", *lines)
	}
	*lines = nil
	key, _, _ := s.Key(repo)
	_ = os.MkdirAll(s.Build.Dir(key), 0o755)
	_ = os.WriteFile(s.Build.Binary(key), []byte("#!/bin/sh\n"), 0o555)
	if _, err := s.BuildNow(repo, "", true, time.Second); err != nil || len(*lines) != 0 {
		t.Errorf("a built binary: %v %q", err, *lines)
	}
}

// A repo with no Go checks has nothing to build and says nothing.
func TestNothingToBuildSaysNothing(t *testing.T) {
	repo := t.TempDir()
	_ = os.MkdirAll(filepath.Join(repo, ".claudinite"), 0o755)
	_ = os.WriteFile(filepath.Join(repo, ".claudinite/settings.yaml"), []byte("engine:\n  version: \"1.1.0\"\n"), 0o644)
	s := service(t)
	s.Session = "s1"
	lines := collect(&s)
	if line, err := s.Start(repo); line != "" || err != nil {
		t.Errorf("%q %v", line, err)
	}
	s.Run(repo, "stop", []string{"work"}, "", time.Second, false, nil)
	if len(*lines) != 0 {
		t.Errorf("%q", *lines)
	}
}
