package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// run accepts the exit code it is told to expect and refuses any other;
// samples records times measured elsewhere; report takes its title and
// command line.
func TestRunExitSamplesAndTitle(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "log.jsonl")
	if err := runCmd([]string{"--name", "two", "--runs", "2", "--log", log, "--exit", "2", "--", "sh", "-c", "exit 2"}); err != nil {
		t.Fatal(err)
	}
	if err := runCmd([]string{"--name", "zero", "--runs", "1", "--log", log, "--exit", "2", "--", "true"}); err == nil {
		t.Error("an exit 0 passed where 2 was expected")
	}
	if err := samplesCmd([]string{"--name", "derive", "--log", log}, strings.NewReader("1.5\n2.5\n3.5\n")); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	if err := reportCmd([]string{"--log", log, "--out", out, "--runs", "2", "--title", "Hook latency", "--command", "sh probe/hook-latency/run.sh"}); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(out, "*.md"))
	if len(files) != 1 {
		t.Fatalf("%v", files)
	}
	md, _ := os.ReadFile(files[0])
	for _, want := range []string{"# Hook latency: ", "`sh probe/hook-latency/run.sh`", "| two | 2 |", "| derive | 3 | 2.5 ms |"} {
		if !strings.Contains(string(md), want) {
			t.Errorf("no %q in\n%s", want, md)
		}
	}
}
