package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/license"
	"github.com/missingbulb/ClaudiniteEngine/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/tasks/land"
	"github.com/missingbulb/ClaudiniteEngine/tasks/world"
)

func TestThePortSpeaksThePullsAndLaneAPIs(t *testing.T) {
	wf := base64.StdEncoding.EncodeToString([]byte("on:\n  pull_request:\n  workflow_dispatch:\n"))
	g, seen := stubGitHub(t, func(r recorded) (int, string) {
		switch {
		case r.path == "/repos/o/r/pulls/5" && r.method == "GET":
			return 200, `{"number":5,"node_id":"PR_5","state":"open","mergeable":true,"head":{"ref":"claudinite/a/x","sha":"abc"},"base":{"ref":"main"}}`
		case r.path == "/repos/o/r/pulls/6":
			return 200, `{"number":6,"mergeable":null}`
		case r.path == "/repos/o/r/pulls/7":
			return 404, `{"message":"Not Found"}`
		case r.path == "/repos/o/r/pulls" && r.method == "POST":
			return 201, `{"number":8,"node_id":"PR_8","head":{"ref":"b","sha":"s8"}}`
		case r.path == "/repos/o/r/contents/.github/workflows?ref=claudinite%2Fa%2Fx":
			return 200, `[{"name":"ci.yml"},{"name":"notes.md"}]`
		case r.path == "/repos/o/r/contents/.github/workflows/ci.yml?ref=claudinite%2Fa%2Fx":
			return 200, `{"content":"` + wf + `"}`
		case r.path == "/repos/o/r/contents/README.md?ref=main":
			return 200, `{"content":"` + base64.StdEncoding.EncodeToString([]byte("hello")) + `"}`
		case strings.HasPrefix(r.path, "/repos/o/r/contents/"):
			return 404, `{}`
		case r.path == "/repos/o/r/actions/workflows/ci.yml/dispatches":
			return 403, `{"message":"Resource not accessible by integration"}`
		case r.path == "/repos/o/r/branches/main":
			return 200, `{"name":"main","protected":false}`
		case r.path == "/repos/o/r/rules/branches/main":
			return 200, `[{"type":"deletion"},{"type":"pull_request"}]`
		case strings.HasPrefix(r.path, "/repos/o/r/actions/runs?head_sha=abc"):
			return 200, `{"workflow_runs":[{"name":"CI","status":"completed","conclusion":"success"}]}`
		case r.path == "/graphql":
			if strings.Contains(r.body, "PR_bad") {
				return 200, `{"errors":[{"message":"Pull request is in clean status"}]}`
			}
			return 200, `{"data":{}}`
		case r.path == "/repos/o/r/pulls/5/merge":
			return 409, `{"message":"Head branch was modified"}`
		}
		return 200, `{}`
	})
	p, err := g.Pull(5)
	if err != nil || p.NodeID != "PR_5" || p.HeadRef != "claudinite/a/x" || p.BaseRef != "main" {
		t.Fatalf("%+v %v", p, err)
	}
	if m, err := g.Mergeable(5); err != nil || m == nil || !*m {
		t.Error(m, err)
	}
	if m, err := g.Mergeable(6); err != nil || m != nil {
		t.Error("not yet computed is unknown:", m, err)
	}
	if _, err := g.Pull(7); !errors.Is(err, world.ErrGone) {
		t.Error(err)
	}
	if p, err := g.CreatePull("t", "b", "b", "main"); err != nil || p.Number != 8 || p.NodeID != "PR_8" {
		t.Error(p, err)
	}
	files, err := g.WorkflowFiles("claudinite/a/x")
	if err != nil || len(files) != 1 || files[0].Name != "ci.yml" || !strings.Contains(files[0].Content, "workflow_dispatch") {
		t.Error(files, err)
	}
	if s, err := g.FileAt("README.md", "main"); err != nil || s != "hello" {
		t.Error(s, err)
	}
	if _, err := g.FileAt("absent", "main"); !errors.Is(err, world.ErrGone) {
		t.Error(err)
	}
	var se *land.StatusError
	if err := g.DispatchWorkflow("ci.yml", "b"); !errors.As(err, &se) || se.Status != 403 {
		t.Error("the lane reads a refused dispatch's status:", err)
	}
	if prot, err := g.BranchProtected("main"); err != nil || prot == nil || *prot {
		t.Error(prot, err)
	}
	if rules, err := g.BranchRules("main"); err != nil || strings.Join(rules, ",") != "deletion,pull_request" {
		t.Error(rules, err)
	}
	if runs, err := g.RunsForSHA("abc"); err != nil || len(runs) != 1 || runs[0].Conclusion != "success" {
		t.Error(runs, err)
	}
	if err := g.EnableAutoMerge("PR_5"); err != nil {
		t.Error(err)
	}
	if err := g.EnableAutoMerge("PR_bad"); err == nil || !strings.Contains(err.Error(), "clean status") {
		t.Error("a GraphQL error is an error:", err)
	}
	err = g.MergePull(land.Merge{Number: 5, SHA: "abc", Message: "Claudinite-Task: a/x"})
	if !errors.As(err, &se) || se.Status != 409 {
		t.Error(err)
	}
	var merge map[string]string
	for _, r := range *seen {
		if r.path == "/repos/o/r/pulls/5/merge" {
			_ = json.Unmarshal([]byte(r.body), &merge)
		}
	}
	if merge["merge_method"] != "squash" || merge["sha"] != "abc" || merge["commit_message"] != "Claudinite-Task: a/x" {
		t.Error(merge)
	}
	if _, set := merge["commit_title"]; set {
		t.Error("an unset title leaves GitHub's own")
	}
	if err := g.DeleteBranch("claudinite/a/x"); err != nil {
		t.Error(err)
	}
	if last := (*seen)[len(*seen)-1]; last.method != "DELETE" || last.path != "/repos/o/r/git/refs/heads/claudinite/a/x" {
		t.Error(last)
	}
}

func keyed(state string, features ...string) *license.ActionsOnce {
	k := &license.KeyPayload{Typ: "actions", Plan: license.PlanPublic, State: state, Features: features}
	return &license.ActionsOnce{Request: func() license.ActionsResult { return license.ActionsResult{Key: k, Wire: "W"} }}
}

func TestOnlyAnItemNeedingTheKeyParksWithoutOne(t *testing.T) {
	member, agentic, growth := sessionTaskOf("acme-pack", "none"), sessionTaskOf("acme-pack", "sonnet"), sessionTaskOf("claudinite-growth", "none")
	asked := 0
	none := &license.ActionsOnce{Request: func() license.ActionsResult {
		asked++
		return license.ActionsResult{Cause: license.CauseNoOIDC, Detail: "id-token: write is missing"}
	}}
	if n := taskLicense(none, member); n != "" || asked != 0 {
		t.Error("a member's agentless task runs keyless, and asks for nothing:", n, asked)
	}
	if n := taskLicense(none, agentic); n == "" {
		t.Error("an agentic task without a key parks")
	}
	if n := taskLicense(keyed("degraded", "claudinite-tasks"), agentic); n == "" {
		t.Error("a degraded key parks the agentic item")
	}
	if n := taskLicense(keyed("ok"), growth); !strings.Contains(n, "claudinite-tasks") {
		t.Error("an engine pack's task needs the claudinite-tasks row:", n)
	}
	if n := taskLicense(keyed("ok", "claudinite-tasks"), growth); n != "" {
		t.Error(n)
	}
	if n := taskLicense(keyed("ok"), agentic); n != "" {
		t.Error("a member's agentic task needs a sound key, not the engine's row:", n)
	}
}

func TestOnlyAPolicyThatAuthorizesALandingEntersTheLane(t *testing.T) {
	for _, c := range []struct {
		policy any
		want   bool
	}{{nil, false}, {"nothing", false}, {"anything", true}, {[]any{"layout"}, true}, {"reject:layout", false}} {
		if got := mayLand(c.policy); got != c.want {
			t.Errorf("%v: %v", c.policy, got)
		}
	}
}

func sessionTaskOf(pack, model string) taskspec.Task {
	return taskspec.Task{Pack: pack, ID: "a", Decl: taskspec.Decl{"id": "a", "agent_model": model}}
}
