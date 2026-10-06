package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/lifecycle/update"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/workflows"
	"github.com/missingbulb/ClaudiniteEngine/tasks/execute"
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
		{[]string{"update", "land", "--check"}, 2, "--pr"},
		{[]string{"update", "engine", "--check"}, 2, "--check is update land's"},
		{[]string{"update", "land", "--check", "--pr", "3", "--sha", "abc", "--repo", t.TempDir()}, 1, "GITHUB_TOKEN"},
	} {
		_, errOut, code := runCN(t, bin, noToken, "", c.args...)
		if code != c.code || !strings.Contains(errOut, c.in) {
			t.Errorf("%v: exit %d %q", c.args, code, errOut)
		}
	}
	if _, errOut, _ := runCN(t, bin, nil, "", "bogus"); !strings.Contains(errOut, "update engine [--force]") || !strings.Contains(errOut, "update land --pr N --sha SHA [--check]") {
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
	dir := gcecMember(t, "https://github.com/missingbulb/GoogleCalendarEventCreator", "")
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

// The engine/update task asks for its agent stage only when the engine PR
// carries staged workflows, and then on that PR.
func TestTheUpdateTaskHandsOffOnlyWhatIsStaged(t *testing.T) {
	t.Parallel()
	said := []string{"cn update engine: opened #5 for 1.61006.1", "cn update packs: skipped: engine PR #5 is open"}
	plain := updateTaskResult(said, update.EngineResult{Verdict: "opened #5 for 1.61006.1"})
	if !plain.OK || plain.AgentRequested || plain.HandOff != nil || !reflect.DeepEqual(plain.Said, said) {
		t.Errorf("nothing staged: %+v", plain)
	}
	staged := updateTaskResult(said, update.EngineResult{Verdict: "opened #5 for 1.61006.1", PR: 5, Branch: "claudinite/engine-1.61006.1",
		Staged: []string{workflows.StagedPath("claudinite-scheduler.yml")}})
	if !staged.OK || !staged.AgentRequested || staged.DeliveredPR != 5 || staged.Branch != "claudinite/engine-1.61006.1" ||
		staged.HandOff == nil || staged.HandOff.Mode != execute.ModeAmend || staged.HandOff.PR != 5 || staged.HandOff.Branch != "claudinite/engine-1.61006.1" ||
		!strings.Contains(staged.Reason, workflows.StagedPath("claudinite-scheduler.yml")) || !reflect.DeepEqual(staged.Said, said) {
		t.Errorf("staged: %+v", staged)
	}
}

// gcecMember is a checkout whose origin is GoogleCalendarEventCreator and
// whose workflows are the templates, its scheduler running on cron, or on
// none when cron is "".
func gcecMember(t *testing.T, origin, cron string) string {
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
			line := "    - cron: \"" + workflows.CronPlaceholder + "\"\n"
			if !strings.Contains(string(b), line) {
				t.Fatalf("the scheduler template lacks %q", line)
			}
			if cron != "" {
				b = []byte(strings.Replace(string(b), line, "    - cron: \""+cron+"\"\n", 1))
			} else {
				b = []byte(strings.Replace(string(b), line, "", 1))
			}
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

// GoogleCalendarEventCreator#1441: the Node-era single daily tick 24 4 is
// the member's own cron and is kept, with no name needed and never the
// template's placeholder.
func TestWorkflowsDiffKeepsAMembersOwnCron(t *testing.T) {
	t.Parallel()
	bin := buildCN(t, "")
	out, errOut, code := runCN(t, bin, []string{"GITHUB_REPOSITORY="}, "", "workflows", "diff", "--repo", gcecMember(t, "", "24 4 * * *"))
	if code != 0 || out != "" {
		t.Errorf("exit %d %s\n%s", code, errOut, out)
	}
}

// A scheduler with no cron of its own is patched to the repo's hashed cron,
// never to the template's placeholder, whichever way the name is learned.
func TestWorkflowsDiffGivesACronlessMemberItsOwnHashedCron(t *testing.T) {
	t.Parallel()
	bin := buildCN(t, "")
	noRepo := []string{"GITHUB_REPOSITORY="}
	for _, origin := range []string{"https://github.com/missingbulb/GoogleCalendarEventCreator", "git@github.com:missingbulb/GoogleCalendarEventCreator.git"} {
		out, errOut, code := runCN(t, bin, noRepo, "", "workflows", "diff", "--repo", gcecMember(t, origin, ""))
		if code != 0 || !strings.Contains(out, gcecCron) || strings.Contains(out, workflows.CronPlaceholder) {
			t.Errorf("origin %s: exit %d %s\n%s", origin, code, errOut, out)
		}
	}
	out, errOut, code := runCN(t, bin, noRepo, "", "workflows", "diff", "--repo", gcecMember(t, "", ""), "--name", "missingbulb/GoogleCalendarEventCreator")
	if code != 0 || !strings.Contains(out, gcecCron) || strings.Contains(out, workflows.CronPlaceholder) {
		t.Errorf("--name: exit %d %s\n%s", code, errOut, out)
	}
	out, errOut, code = runCN(t, bin, []string{"GITHUB_REPOSITORY=missingbulb/GoogleCalendarEventCreator"}, "", "workflows", "diff", "--repo", gcecMember(t, "", ""))
	if code != 0 || !strings.Contains(out, gcecCron) || strings.Contains(out, workflows.CronPlaceholder) {
		t.Errorf("GITHUB_REPOSITORY: exit %d %s\n%s", code, errOut, out)
	}
}

// With the name nowhere to be read, a cronless scheduler cannot be given
// its cron: the diff fails, naming the flag that supplies it, rather than
// writing the placeholder.
func TestWorkflowsDiffRefusesWithoutTheRepoName(t *testing.T) {
	t.Parallel()
	bin := buildCN(t, "")
	out, errOut, code := runCN(t, bin, []string{"GITHUB_REPOSITORY="}, "", "workflows", "diff", "--repo", gcecMember(t, "", ""))
	if code == 0 || strings.Contains(out, workflows.CronPlaceholder) || !strings.Contains(errOut, "--name") {
		t.Errorf("exit %d %s\n%s", code, errOut, out)
	}
}
