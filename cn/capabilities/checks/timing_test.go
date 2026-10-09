package checks

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

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

// holdLock makes key's build look under way in another live process.
func holdLock(t *testing.T, s Service, key string) {
	t.Helper()
	dir := filepath.Join(s.Build.CacheRoot, "checks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, key+".lock"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The first run to need the binary compiles it here and reports the
// compile and its wait; a later run finds it built and says nothing.
func TestARunBuildsTheBinaryHereAndReportsIt(t *testing.T) {
	s := service(t)
	repo := helloRepo(t)
	lines := collect(&s)
	if res, _ := s.Run(repo, "stop", []string{"work"}, "", time.Minute, nil); res.Err != nil {
		t.Fatal(res.Err)
	}
	if !hasPrefix(*lines, "[cn] buildwait stop ok ") || !hasPrefix(*lines, "[cn] build compiled ok ") || len(*lines) != 2 {
		t.Fatalf("first stop: %q", *lines)
	}
	*lines = nil
	if res, _ := s.Run(repo, "stop", []string{"work"}, "", time.Minute, nil); res.Err != nil || len(*lines) != 0 {
		t.Errorf("a built binary: %v %q", res.Err, *lines)
	}
}

func TestAFailedBuildReportsItsFailureAndTheWait(t *testing.T) {
	s := service(t)
	repo := helloRepo(t)
	s.Build.Go = filepath.Join(t.TempDir(), "no-go-here")
	lines := collect(&s)
	res, crumb := s.Run(repo, "stop", []string{"work"}, "", time.Minute, nil)
	if res.Err == nil || !strings.HasPrefix(crumb, "[cn] checks stop error ") {
		t.Errorf("%+v %q", res, crumb)
	}
	if !hasPrefix(*lines, "[cn] buildwait stop error ") || !hasPrefix(*lines, "[cn] build compiled error ") {
		t.Errorf("%q", *lines)
	}
}

// A build another live process holds is waited for, and a wait that runs
// out is a timeout.
func TestAWaitForAnotherBuildThatRunsOutReportsTimeout(t *testing.T) {
	s := service(t)
	repo := helloRepo(t)
	key, _, _ := s.Key(repo)
	holdLock(t, s, key)
	lines := collect(&s)
	res, crumb := s.Run(repo, "stop", []string{"work"}, "", 200*time.Millisecond, nil)
	if res.Err == nil || !strings.HasPrefix(crumb, "[cn] checks stop timeout ") {
		t.Errorf("%+v %q", res, crumb)
	}
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
	if !hasPrefix(*lines, "[cn] buildwait check error ") {
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
	lines := collect(&s)
	s.Run(repo, "stop", []string{"work"}, "", time.Second, nil)
	if len(*lines) != 0 {
		t.Errorf("%q", *lines)
	}
}
