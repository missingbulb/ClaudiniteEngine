package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/proc"
)

// One cn call should finish in as few processes as it can: every process
// it starts is counted by kind, and each scenario here is held to the
// starts it makes today, exactly. A start more fails as a regression; a
// start fewer fails too, so the budget comes down with the improvement
// that earned it. These run in process and not in parallel, since the
// counts are the whole test binary's.
var spawnBudget = map[string]string{
	"hook pre-tool-use":         "",
	"verify":                    "",
	"hook session-start":        "",
	"hook stop":                 "git cat-file×1, git check-attr×1, git log×1, git ls-files×1, git merge-base×1, git rev-parse×1",
	"check world":               "git cat-file×1, git check-attr×1, git diff×1, git ls-files×1, git merge-base×1, git remote×1",
	"check world, many changes": "git cat-file×1, git check-attr×1, git diff×1, git ls-files×1, git merge-base×1, git remote×1",
}

func TestSpawnBudget(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("CLAUDINITE_CHECKS_NO_FETCH", "1")
	member := memberAt(t, "v5-settings-checks")
	many := memberAt(t, "v5-settings-checks")
	for i := 0; i < 40; i++ {
		writeRepoFile(t, many, fmt.Sprintf("docs/note-%02d.md", i), "a note\n")
	}
	gitIn(t, many, "add", "-A")
	gitIn(t, many, "commit", "-q", "-m", "many")

	scenarios := []struct {
		name  string
		args  []string
		stdin string
		dir   string
	}{
		{"hook pre-tool-use", []string{"hook", "pre-tool-use"}, `{"session_id":"s","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"ls"}}`, member},
		{"hook session-start", []string{"hook", "session-start"}, `{"session_id":"s","cwd":"` + member + `","hook_event_name":"SessionStart","source":"startup"}`, member},
		{"hook stop", []string{"hook", "stop"}, `{"session_id":"s","cwd":"` + member + `","hook_event_name":"Stop","transcript_path":"` + member + `/none.jsonl"}`, member},
		{"verify", []string{"verify", "--repo", member}, "", member},
		{"check world", []string{"check", "world", "--pr-author", "someone", "--base-ref", "main", "--repo", member}, "", member},
		{"check world, many changes", []string{"check", "world", "--pr-author", "someone", "--base-ref", "main", "--repo", many}, "", many},
	}
	for _, s := range scenarios {
		t.Setenv("CLAUDE_PROJECT_DIR", s.dir)
		proc.Reset()
		out, errOut, code := runInProc(s.args, s.stdin)
		if code != 0 && code != 1 {
			t.Errorf("%s: exit %d\n%s\n%s", s.name, code, out, errOut)
			continue
		}
		if got := proc.Format(proc.Counts()); got != spawnBudget[s.name] {
			t.Errorf("%s starts %q, its budget is %q", s.name, got, spawnBudget[s.name])
		}
	}
}

// memberAt is a git repo holding the named shape corpus, committed on main
// and checked out on a change branch.
func memberAt(t *testing.T, shape string) string {
	t.Helper()
	src, _ := filepath.Abs("../packaging/verify/testdata/shapes/" + shape)
	dir := t.TempDir()
	if out, err := exec.Command("cp", "-R", src+"/.", dir).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "base")
	gitIn(t, dir, "checkout", "-q", "-b", "change")
	return dir
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@x"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v %s", strings.Join(args, " "), err, out)
	}
}
