package workflows

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
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
	if len(tpl) != 2 {
		t.Fatalf("%d templates", len(tpl))
	}
	want := map[string]map[string]string{
		"claudinite-update.yml": {"update": "actions: write,contents: write,issues: write,pull-requests: write"},
		"claudinite-ci.yml": {
			"check": "contents: read,pull-requests: read",
			"land":  "actions: write,contents: write,pull-requests: write",
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
			if strings.Contains(l, "secrets.") {
				t.Errorf("%s:%d: reads a secret: %s", name, i+1, l)
			}
		}
	}
	upd := string(tpl["claudinite-update.yml"])
	for _, w := range []string{"schedule:", "workflow_dispatch:", "sh .claudinite/launch version", ".claudinite/bin/cn update engine", ".claudinite/bin/cn update packs", "GITHUB_STEP_SUMMARY"} {
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
}
