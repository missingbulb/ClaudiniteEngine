package world

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/gitcmd"
)

// expectedFrom stands in for the engine's workflows.Expected: the
// workflow each managed name should hold, given the base's copy.
func expectedFrom(name string, base []byte) ([]byte, error) {
	if strings.Contains(string(base), "unreadable") {
		return nil, errors.New("no name to hash")
	}
	return []byte("expected " + name + " over " + string(base)), nil
}

// workflowMember is a member whose main holds a scheduler workflow, on a
// branch where the bot moved the pin and change edited the rest.
func workflowMember(t *testing.T, base string, change func(dir string)) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	write(t, dir, ".claudinite/settings.yaml", settingsBody("1.1.0", pin1))
	write(t, dir, ".claudinite/launch", "#!/bin/sh\n")
	write(t, dir, ".github/workflows/claudinite-scheduler.yml", base)
	write(t, dir, ".github/workflows/deploy.yml", "deploy\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "base")
	git(t, dir, "checkout", "-q", "-b", "change")
	write(t, dir, ".claudinite/settings.yaml", settingsBody("1.2.0", pin2))
	write(t, dir, ".claudinite/cache/member.GENERATED.json", "{}\n")
	change(dir)
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "change")
	return dir
}

func runWorkflowWorld(t *testing.T, dir string, expected func(string, []byte) ([]byte, error)) (int, string) {
	t.Helper()
	var out bytes.Buffer
	pc := &pinCheck{}
	code := Run(&out, Input{Repo: dir, PRAuthor: bot, BaseRef: "main", Git: gitcmd.Repo{Dir: dir}, CheckPin: pc.check,
		Workflows: []string{"claudinite-ci.yml", "claudinite-scheduler.yml"}, ExpectedWorkflow: expected})
	return code, out.String()
}

// The engine update PR may carry, beside the pin, exactly the workflows
// the new engine expects, as its agent stage moved them in; CI's pin
// guard accepts that and nothing else under .github/workflows/.
func TestPinGuardAcceptsExactlyTheExpectedWorkflows(t *testing.T) {
	t.Parallel()
	const sched = ".github/workflows/claudinite-scheduler.yml"
	moved := func(dir string) {
		write(t, dir, sched, "expected claudinite-scheduler.yml over old\n")
		write(t, dir, ".github/workflows/claudinite-ci.yml", "expected claudinite-ci.yml over ")
	}
	if code, out := runWorkflowWorld(t, workflowMember(t, "old\n", moved), expectedFrom); code != 0 {
		t.Errorf("the expected workflows were refused: %s", out)
	}
	cases := []struct {
		name     string
		base     string
		change   func(string)
		expected func(string, []byte) ([]byte, error)
		want     string
	}{
		{"an edited move", "old\n", func(dir string) { write(t, dir, sched, "expected claudinite-scheduler.yml over old\n# and more\n") }, expectedFrom, "claudinite-scheduler.yml"},
		{"a workflow the engine does not manage", "old\n", func(dir string) { moved(dir); write(t, dir, ".github/workflows/deploy.yml", "deploy elsewhere\n") }, expectedFrom, "deploy.yml"},
		{"a deleted workflow", "old\n", func(dir string) { _ = os.Remove(filepath.Join(dir, filepath.FromSlash(sched))) }, expectedFrom, "claudinite-scheduler.yml"},
		{"files still staged", "old\n", func(dir string) {
			moved(dir)
			write(t, dir, ".claudinite/cache/pending-workflows/claudinite-scheduler.yml", "expected claudinite-scheduler.yml over old\n")
		}, expectedFrom, "pending-workflows"},
		{"no expected workflows to compare with", "old\n", moved, nil, "claudinite-scheduler.yml"},
		{"an expected workflow that cannot be computed", "unreadable\n", func(dir string) { write(t, dir, sched, "anything\n") }, expectedFrom, "no name to hash"},
	}
	for _, c := range cases {
		code, out := runWorkflowWorld(t, workflowMember(t, c.base, c.change), c.expected)
		if code != 1 || !strings.Contains(out, "pin-guard") || !strings.Contains(out, c.want) {
			t.Errorf("%s: exit %d\n%s", c.name, code, out)
		}
	}
}
