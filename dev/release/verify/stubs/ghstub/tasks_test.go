package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/execute"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/ghport"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/land"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/world"
)

// The task runner's port, driven against the stub: every call the
// scheduler and the executor make answers as GitHub's does.
func TestThePortsIssueCallsAgainstTheStub(t *testing.T) {
	t.Parallel()
	bare, _ := fixture(t)
	c, _ := start(t, bare)
	g := ghport.New(c)
	if err := g.EnsureLabels([]workitem.Label{{Name: workitem.StatusDone, Color: "fff"}}); err != nil {
		t.Fatal(err)
	}
	if err := g.EnsureLabels([]workitem.Label{{Name: workitem.StatusDone, Color: "000"}}); err != nil {
		t.Error("a label that exists is reconciled:", err)
	}
	n, err := g.CreateIssue("[claudinite-work] hello/hello-fold", "packs/hello/tasks/hello-fold/task.md\n", []string{workitem.StatusReady})
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if err := g.AddLabel(n, workitem.StatusRunningExecutor); err != nil {
		t.Fatal(err)
	}
	if err := g.RemoveLabel(n, workitem.StatusReady); err != nil {
		t.Fatal(err)
	}
	if err := g.RemoveLabel(n, "absent"); err != nil {
		t.Error("removing an absent label is the end state:", err)
	}
	id, err := g.Comment(n, "claim")
	if err != nil || id == 0 {
		t.Fatal(id, err)
	}
	second, _ := g.Comment(n, "second")
	if second <= id {
		t.Error("comment ids rise", id, second)
	}
	if err := g.EditComment(id, "claim, struck"); err != nil {
		t.Fatal(err)
	}
	cs, err := g.Comments(n)
	if err != nil || len(cs) != 2 || cs[0].Body != "claim, struck" || cs[0].CreatedAt == "" || cs[0].Author == "" {
		t.Fatalf("%+v %v", cs, err)
	}
	i, err := g.Issue(n)
	if err != nil || !i.Is(workitem.StatusRunningExecutor) || i.Is(workitem.StatusReady) || i.PullRequest {
		t.Fatalf("%+v %v", i, err)
	}
	if _, err := g.Issue(99); !errors.Is(err, world.ErrGone) {
		t.Error(err)
	}
	if err := g.SetIssueBody(n, "new body"); err != nil {
		t.Fatal(err)
	}
	if err := g.CloseIssue(n, "completed"); err != nil {
		t.Fatal(err)
	}
	open, _ := g.IssuesPage(world.Query{State: "open"}, 1)
	closed, _ := g.IssuesPage(world.Query{State: "closed", Label: workitem.StatusRunningExecutor}, 1)
	if len(open) != 0 || len(closed) != 1 || closed[0].StateReason != "completed" || closed[0].Body != "new body" {
		t.Fatalf("open %+v closed %+v", open, closed)
	}
	if page2, _ := g.IssuesPage(world.Query{State: "all"}, 2); len(page2) != 0 {
		t.Error("a second page past the end is empty")
	}
	if err := g.ReopenIssue(n); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Permission("stranger"); !errors.Is(err, world.ErrGone) {
		t.Error(err)
	}
	if p, err := g.Permission("acme-dev"); err != nil || p != "admin" {
		t.Error("the session's person administers the repo:", p, err)
	}
}

func TestThePortsRepoAndLaneCallsAgainstTheStub(t *testing.T) {
	t.Parallel()
	bare, sha := fixture(t)
	c, _ := start(t, bare)
	g := ghport.New(c)
	branches, err := g.BranchesPage(1)
	if err != nil || len(branches) != 2 {
		t.Fatal(branches, err)
	}
	if b, err := g.Branch("claudinite/engine-2.0.0"); err != nil || b.SHA != sha {
		t.Error(b, err)
	}
	commits, err := g.CommitsPage("main", "", 1)
	if err != nil || len(commits) != 1 || commits[0].Message != "base" {
		t.Fatal(commits, err)
	}
	if more, _ := g.CommitsPage("main", "", 2); len(more) != 0 {
		t.Error("one page")
	}
	cm, err := g.Commit(sha)
	if err != nil || cm.Message != "pin" || cm.Date == "" {
		t.Fatal(cm, err)
	}
	if _, err := g.FileAt("absent.txt", "main"); !errors.Is(err, world.ErrGone) {
		t.Error(err)
	}
	p, err := g.CreatePull("t", "b", "claudinite/engine-2.0.0", "main")
	if err != nil || p.Number != 1 || p.NodeID == "" || p.HeadSHA != sha {
		t.Fatal(p, err)
	}
	if m, err := g.Mergeable(1); err != nil || m == nil || !*m {
		t.Error(m, err)
	}
	pulls, err := world.OpenPulls(g)
	if err != nil || len(pulls) != 1 {
		t.Fatal(pulls, err)
	}
	if issues, _ := g.IssuesPage(world.Query{State: "open"}, 1); len(issues) != 1 || !issues[0].PullRequest {
		t.Error("a pull request lists among the issues, marked:", issues)
	}
	if prot, err := g.BranchProtected("main"); err != nil || prot == nil || *prot {
		t.Error(prot, err)
	}
	if rules, err := g.BranchRules("main"); err != nil || len(rules) != 0 {
		t.Error(rules, err)
	}
	if err := g.EnableAutoMerge(p.NodeID); err != nil {
		t.Error(err)
	}
	runs, err := g.RunsForSHA(sha)
	if err != nil || len(runs) != 1 || runs[0].Event != "pull_request" || runs[0].Conclusion != "action_required" {
		t.Fatal("the job token's PR got no held run:", runs, err)
	}
	if err := g.ApproveRun(runs[0].ID); err != nil {
		t.Error(err)
	}
	if runs, err := g.RunsForSHA(sha); err != nil || len(runs) != 1 || runs[0].Conclusion != "success" {
		t.Error(runs, err)
	}
	if err := g.MergePull(land.Merge{Number: 1, SHA: "0000000000000000000000000000000000000000"}); err == nil {
		t.Error("merged at a stale head")
	}
	if err := g.MergePull(land.Merge{Number: 1, SHA: sha, Message: workitem.TaskTrailer + ": hello/hello-fold"}); err != nil {
		t.Fatal(err)
	}
	if got := git(t, bare, "log", "-1", "--format=%B", "main"); !strings.Contains(got, workitem.TaskTrailer+": hello/hello-fold") {
		t.Errorf("the squash carries the trailer: %q", got)
	}
	if err := g.DeleteBranch("claudinite/engine-2.0.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Branch("claudinite/engine-2.0.0"); !errors.Is(err, world.ErrGone) {
		t.Error(err)
	}
	if i, _ := g.Issue(1); i.State != "closed" || i.MergedAt == "" {
		t.Error("the merged pull request reads merged:", i)
	}
}

// A fire reaches the routine route once, with the bearer the member's
// secret holds; the route down answers 503.
func TestTheRoutineRouteTakesTheFire(t *testing.T) {
	t.Parallel()
	bare, _ := fixture(t)
	c, srv := start(t, bare)
	g := ghport.New(c)
	n, _ := g.CreateIssue("[claudinite-work] hello/hello-agent", "packs/hello/tasks/hello-agent/task.md\n", []string{workitem.StatusRunningAgent})
	inv := execute.Invoker{Repo: "acme/member", HTTP: srv.Client(), Timeout: 5 * time.Second,
		Endpoints: map[string]any{"default": map[string]any{"url": srv.URL + "/routines/trig_hello"}},
		Env:       map[string]string{"CCR_ROUTINE_TOKEN": routineToken}}
	got := inv.Invoke(helloAgent(), workitem.Issue{Number: n}, "1-abc")
	if !got.OK || got.SessionID == "" {
		t.Fatalf("%+v", got)
	}
	s := state(t, srv)
	if len(s.Fires) != 1 || s.Fires[0].Trigger != "trig_hello" || !strings.Contains(s.Fires[0].Text, "acme/member#1. Invocation nonce: 1-abc.") {
		t.Fatalf("%+v", s.Fires)
	}
	bad := inv
	bad.Env = map[string]string{"CCR_ROUTINE_TOKEN": "wrong"}
	if got := bad.Invoke(helloAgent(), workitem.Issue{Number: n}, "1-x"); got.OK || !got.Answered || !strings.Contains(got.Error, "401") {
		t.Errorf("%+v", got)
	}
	control(t, srv, "/_stub/routine", map[string]any{"down": true})
	if got := inv.Invoke(helloAgent(), workitem.Issue{Number: n}, "1-y"); got.OK || !got.Answered || !strings.Contains(got.Error, "503") {
		t.Errorf("%+v", got)
	}
}

// The stub agent is handed the item and its comments as the session's
// tools read them, and its converge lands on the item as those tools
// would write it.
func TestTheStubAgentConvergesTheItem(t *testing.T) {
	t.Parallel()
	bare, _ := fixture(t)
	dir := t.TempDir()
	log := filepath.Join(dir, "agent.log")
	script := filepath.Join(dir, "agent.sh")
	_ = os.WriteFile(script, []byte("#!/bin/sh\nset -eu\nprintf '%s %s\\n' \"$CLAUDINITE_AGENT_ISSUE\" \"$CLAUDINITE_AGENT_NONCE\" >> "+log+"\n"+
		"grep -q hand-off \"$CLAUDINITE_AGENT_COMMENTS\"\ngrep -q '\"number\":' \"$CLAUDINITE_AGENT_ITEM\"\n"+
		"curl -sS --fail -k -X POST -d '{\"issue\":'\"$CLAUDINITE_AGENT_ISSUE\"',\"outcome\":\"done\",\"summary\":\"stub agent ran\"}' \"$CLAUDINITE_AGENT_STUB/_stub/converge\" > /dev/null\n"), 0o755)
	st := newStub(bare, "acme/member", "tok")
	st.agent = script
	srv := httptestTLS(t, st)
	c := clientFor(srv)
	g := ghport.New(c)
	n, _ := g.CreateIssue("[claudinite-work] hello/hello-agent", "packs/hello/tasks/hello-agent/task.md\n", []string{workitem.StatusRunningAgent})
	_, _ = g.Comment(n, workitem.HandoffMarker+"\nhand-off nonce `1-abc`")
	inv := execute.Invoker{Repo: "acme/member", HTTP: srv.Client(), Timeout: 5 * time.Second,
		Endpoints: map[string]any{"default": map[string]any{"url": srv.URL + "/routines/trig_hello"}},
		Env:       map[string]string{"CCR_ROUTINE_TOKEN": routineToken}}
	if got := inv.Invoke(helloAgent(), workitem.Issue{Number: n}, "1-abc"); !got.OK {
		t.Fatalf("%+v", got)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		i, _ := g.Issue(n)
		if i.State == "closed" {
			if !i.Is(workitem.StatusDone) || i.Is(workitem.StatusRunningAgent) || i.StateReason != "completed" {
				t.Errorf("%+v", i)
			}
			break
		}
		if time.Now().After(deadline) {
			s := state(t, srv)
			raw, _ := json.Marshal(s.Agent)
			t.Fatalf("the item never closed: %s", raw)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got, _ := os.ReadFile(log); string(got) != "1 1-abc\n" {
		t.Errorf("agent log %q", got)
	}
	s := state(t, srv)
	for len(s.Agent) == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		s = state(t, srv)
	}
	if len(s.Agent) != 1 || s.Agent[0].Exit != 0 {
		t.Errorf("%+v", s.Agent)
	}
}

func helloAgent() taskspec.Task {
	return taskspec.Task{Pack: "hello", ID: "hello-agent", Decl: taskspec.Decl{"id": "hello-agent", "agent_model": "sonnet"}}
}

func httptestTLS(t *testing.T, h http.Handler) *httptest.Server {
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func clientFor(srv *httptest.Server) *githubapi.Client {
	return &githubapi.Client{Base: srv.URL, Repo: "acme/member", Token: "tok", HTTP: srv.Client()}
}
