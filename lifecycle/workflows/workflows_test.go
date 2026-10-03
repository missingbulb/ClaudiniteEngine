package workflows

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/taskspec"
)

// jobPermissions reads each job's permissions block: job name to the
// sorted "scope: level" lines.
func jobPermissions(t *testing.T, yml string) (string, map[string][]string) {
	t.Helper()
	top := ""
	jobs := map[string][]string{}
	job, inPerms, inJobs := "", false, false
	for _, l := range strings.Split(yml, "\n") {
		indent := len(l) - len(strings.TrimLeft(l, " "))
		trimmed := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(l, "permissions:"):
			top = strings.TrimSpace(strings.TrimPrefix(l, "permissions:"))
		case l == "jobs:":
			inJobs = true
		case inJobs && indent == 2 && strings.HasSuffix(trimmed, ":"):
			job, inPerms = strings.TrimSuffix(trimmed, ":"), false
			jobs[job] = nil
		case inJobs && indent == 4 && trimmed == "permissions:":
			inPerms = true
		case inPerms && indent == 6 && trimmed != "":
			jobs[job] = append(jobs[job], trimmed)
		case indent <= 4:
			inPerms = false
		}
	}
	for j := range jobs {
		sort.Strings(jobs[j])
	}
	return top, jobs
}

func TestTemplates(t *testing.T) {
	tpl := Templates()
	if len(tpl) != 3 {
		t.Fatalf("%d templates", len(tpl))
	}
	want := map[string]map[string]string{
		"claudinite-ci.yml": {
			"check": "contents: read,pull-requests: read",
			"land":  "actions: write,contents: write,pull-requests: write",
		},
		"claudinite-scheduler.yml": {
			"scheduler-run":  "contents: read,issues: write,pull-requests: read",
			"drain":          "actions: write,contents: read",
			"report-failure": "contents: read,issues: write",
		},
		"claudinite-executor.yml": {
			"execute":            "actions: write,contents: write,id-token: write,issues: write,pull-requests: write",
			"continue-the-chain": "actions: write,contents: read,issues: write",
		},
	}
	pinned := regexp.MustCompile(`^\s*(?:- )?uses:\s*[^@\s]+@[0-9a-f]{40}(?:\s+#.*)?$`)
	for name, body := range tpl {
		disk, err := os.ReadFile("templates/" + name)
		if err != nil || string(disk) != string(body) {
			t.Errorf("%s: embedded template differs from templates/%s", name, name)
		}
		top, jobs := jobPermissions(t, string(body))
		if top != "{}" {
			t.Errorf("%s: top-level permissions %q, want {}", name, top)
		}
		if len(jobs) != len(want[name]) {
			t.Errorf("%s: jobs %v", name, jobs)
		}
		for job, perms := range want[name] {
			if got := strings.Join(jobs[job], ","); got != perms {
				t.Errorf("%s job %s: permissions [%s], want [%s]", name, job, got, perms)
			}
		}
		for i, l := range strings.Split(string(body), "\n") {
			if strings.Contains(l, "uses:") && !pinned.MatchString(l) {
				t.Errorf("%s:%d: not pinned by SHA: %s", name, i+1, l)
			}
			if strings.Contains(l, "secrets.") && !readsItsSecret(name, l) {
				t.Errorf("%s:%d: reads a secret: %s", name, i+1, l)
			}
		}
	}
	if _, ok := tpl[Superseded]; ok {
		t.Errorf("%s is among the templates a member is brought to", Superseded)
	}
	upd := string(SupersededTemplate())
	if disk, err := os.ReadFile("templates/" + Superseded); err != nil || string(disk) != upd {
		t.Errorf("the embedded %s differs from templates/", Superseded)
	}
	for _, w := range []string{"schedule:", "workflow_dispatch:", "sh .claudinite/launch version", ".claudinite/bin/cn update engine", ".claudinite/bin/cn update packs", "GITHUB_STEP_SUMMARY", "persist-credentials: false"} {
		if !strings.Contains(upd, w) {
			t.Errorf("claudinite-update.yml lacks %q", w)
		}
	}
	ci := string(tpl["claudinite-ci.yml"])
	for _, w := range []string{"pull_request:", "workflow_dispatch:", "pr:", "cn check world --pr-author", "--base-ref", "cn update land --pr", "needs: check"} {
		if !strings.Contains(ci, w) {
			t.Errorf("claudinite-ci.yml lacks %q", w)
		}
	}
	sched := string(tpl["claudinite-scheduler.yml"])
	for _, w := range []string{"schedule:", "cron: \"" + CronPlaceholder + "\"", "wake:", "group: claudinite-scheduler-run", "cn schedule run", "cn schedule drain", "cn schedule report-failure",
		"CLAUDINITE_WAKE: ${{ inputs.wake }}", "CLAUDINITE_VARS: ${{ toJSON(vars) }}", "pickable", fleetToken} {
		if !strings.Contains(sched, w) {
			t.Errorf("claudinite-scheduler.yml lacks %q", w)
		}
	}
	if run := sched[strings.Index(sched, "name: cn schedule run"):strings.Index(sched, "drain:")]; !strings.Contains(run, fleetToken) {
		t.Error("cn schedule run, which collects the fleet signal, is not given FLEET_GITHUB_TOKEN")
	}
	exe := string(tpl["claudinite-executor.yml"])
	for _, w := range []string{"types: [labeled]", "continuation_depth:", "timeout-minutes: 350", "CLAUDINITE_VARS: ${{ toJSON(vars) }}",
		"CCR_ROUTINE_TOKEN: ${{ secrets.CCR_ROUTINE_TOKEN }}", SecretsMarker, "cn execute loop", "cn execute continue",
		"CLAUDINITE_CONTINUATION_DEPTH: ${{ inputs.continuation_depth }}", "task:status:waiting-for-executor"} {
		if !strings.Contains(exe, w) {
			t.Errorf("claudinite-executor.yml lacks %q", w)
		}
	}
	if !strings.HasSuffix(strings.TrimRight(exe[:strings.Index(exe, SecretsMarker)+len(SecretsMarker)], " "), SecretsMarker) {
		t.Error("the marker is a line of its own")
	}
}

const fleetToken = "FLEET_GITHUB_TOKEN: ${{ secrets.FLEET_GITHUB_TOKEN }}"

// A template reads only the secret its own steps need: the executor its
// routine token, the scheduler run the fleet token its signal collects with.
func readsItsSecret(name, line string) bool {
	switch name {
	case "claudinite-executor.yml":
		return strings.Contains(line, "CCR_ROUTINE_TOKEN: ${{ secrets.CCR_ROUTINE_TOKEN }}")
	case "claudinite-scheduler.yml":
		return strings.Contains(line, fleetToken)
	}
	return false
}

// The task discovery reads the superseded workflow's path to let the
// engine/update task stand aside; the two spellings are one file.
func TestTheUpdateTaskStandsAsideForThisWorkflow(t *testing.T) {
	if taskspec.UpdateWorkflow != ".github/workflows/"+Superseded {
		t.Errorf("taskspec.UpdateWorkflow %q is not .github/workflows/%s", taskspec.UpdateWorkflow, Superseded)
	}
}
