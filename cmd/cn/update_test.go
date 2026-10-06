package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/lifecycle/workflows"
)

func TestUpdateCommandArguments(t *testing.T) {
	t.Parallel()
	bin := buildCN(t, "")
	noToken := []string{"GITHUB_TOKEN="}
	for _, c := range []struct {
		args []string
		code int
		in   string
	}{
		{[]string{"update"}, 2, "update takes engine, packs or land"},
		{[]string{"update", "bogus"}, 2, "update takes engine, packs or land"},
		{[]string{"update", "land"}, 2, "--pr"},
		{[]string{"update", "land", "--pr", "3"}, 2, "--sha"},
		{[]string{"update", "engine", "--bogus"}, 2, ""},
		{[]string{"update", "engine", "--repo", t.TempDir()}, 1, "GITHUB_TOKEN"},
		{[]string{"update", "packs", "--repo", t.TempDir()}, 1, "GITHUB_TOKEN"},
		{[]string{"update", "land", "--pr", "3", "--sha", "abc", "--repo", t.TempDir()}, 1, "GITHUB_TOKEN"},
	} {
		_, errOut, code := runCN(t, bin, noToken, "", c.args...)
		if code != c.code || !strings.Contains(errOut, c.in) {
			t.Errorf("%v: exit %d %q", c.args, code, errOut)
		}
	}
	if _, errOut, _ := runCN(t, bin, nil, "", "bogus"); !strings.Contains(errOut, "update engine [--force]") {
		t.Errorf("usage lacks update: %s", errOut)
	}
}

func TestWorkflowsDiffCommand(t *testing.T) {
	t.Parallel()
	bin := buildCN(t, "")
	out, _, code := runCN(t, bin, nil, "", "workflows", "diff", "--repo", t.TempDir(), "--name", "o/r")
	if code != 0 || !strings.Contains(out, "+++ b/.github/workflows/claudinite-executor.yml") || !strings.Contains(out, `+    - cron: "20 5,17 * * *"`) {
		t.Errorf("exit %d\n%s", code, out)
	}
	dir := gcecMember(t, "https://github.com/missingbulb/GoogleCalendarEventCreator")
	out, errOut, code := runCN(t, bin, []string{"GITHUB_REPOSITORY="}, "", "workflows", "stage", "--repo", dir)
	if code != 0 || out != workflows.StagedPath("claudinite-scheduler.yml")+"\n" {
		t.Errorf("stage: exit %d %s\n%s", code, errOut, out)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(workflows.StagedPath("claudinite-scheduler.yml")))); !strings.Contains(string(b), `cron: "24 4,16 * * *"`) {
		t.Errorf("staged:\n%s", b)
	}
	if _, _, code := runCN(t, bin, nil, "", "workflows"); code != 2 {
		t.Errorf("no subcommand: exit %d", code)
	}
}

// gcecMember is a checkout whose origin is GoogleCalendarEventCreator and
// whose workflows are the templates, but for the scheduler cron the Node
// engine wrote: one tick a day, not the engine's hashed pair.
func gcecMember(t *testing.T, origin string) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if origin != "" {
		if out, err := exec.Command("git", "-C", dir, "remote", "add", "origin", origin).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	}
	for n, b := range workflows.Templates() {
		if n == "claudinite-scheduler.yml" {
			b = []byte(strings.Replace(string(b), workflows.CronPlaceholder, "24 4 * * *", 1))
		}
		p := filepath.Join(dir, ".github", "workflows", n)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const gcecCron = "+    - cron: \"24 4,16 * * *\"\n"

// GoogleCalendarEventCreator#1441: a member whose cron is not the hashed
// pair is patched to its own hashed cron, never to the template's
// placeholder, whichever way the name is learned.
func TestWorkflowsDiffGivesAMemberItsOwnHashedCron(t *testing.T) {
	t.Parallel()
	bin := buildCN(t, "")
	noRepo := []string{"GITHUB_REPOSITORY="}
	for _, origin := range []string{"https://github.com/missingbulb/GoogleCalendarEventCreator", "git@github.com:missingbulb/GoogleCalendarEventCreator.git"} {
		out, errOut, code := runCN(t, bin, noRepo, "", "workflows", "diff", "--repo", gcecMember(t, origin))
		if code != 0 || !strings.Contains(out, gcecCron) || !strings.Contains(out, "-    - cron: \"24 4 * * *\"\n") || strings.Contains(out, workflows.CronPlaceholder) {
			t.Errorf("origin %s: exit %d %s\n%s", origin, code, errOut, out)
		}
	}
	out, errOut, code := runCN(t, bin, noRepo, "", "workflows", "diff", "--repo", gcecMember(t, ""), "--name", "missingbulb/GoogleCalendarEventCreator")
	if code != 0 || !strings.Contains(out, gcecCron) || strings.Contains(out, workflows.CronPlaceholder) {
		t.Errorf("--name: exit %d %s\n%s", code, errOut, out)
	}
	out, errOut, code = runCN(t, bin, []string{"GITHUB_REPOSITORY=missingbulb/GoogleCalendarEventCreator"}, "", "workflows", "diff", "--repo", gcecMember(t, ""))
	if code != 0 || !strings.Contains(out, gcecCron) || strings.Contains(out, workflows.CronPlaceholder) {
		t.Errorf("GITHUB_REPOSITORY: exit %d %s\n%s", code, errOut, out)
	}
}

// With the name nowhere to be read, a cron the hash did not write cannot
// be given its replacement: the diff fails, naming the flag that supplies
// it, rather than writing the placeholder.
func TestWorkflowsDiffRefusesWithoutTheRepoName(t *testing.T) {
	t.Parallel()
	bin := buildCN(t, "")
	out, errOut, code := runCN(t, bin, []string{"GITHUB_REPOSITORY="}, "", "workflows", "diff", "--repo", gcecMember(t, ""))
	if code == 0 || strings.Contains(out, workflows.CronPlaceholder) || !strings.Contains(errOut, "--name") {
		t.Errorf("exit %d %s\n%s", code, errOut, out)
	}
}
