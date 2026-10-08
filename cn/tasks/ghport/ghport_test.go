package ghport

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/land"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/world"
)

type recorded struct{ method, path, body string }

func stubGitHub(t *testing.T, answer func(r recorded) (int, string)) (World, *[]recorded) {
	var seen []recorded
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		rec := recorded{r.Method, r.URL.RequestURI(), string(raw)}
		seen = append(seen, rec)
		code, body := answer(rec)
		w.WriteHeader(code)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return World{&githubapi.Client{Base: srv.URL, Repo: "o/r", Token: "t", HTTP: srv.Client()}}, &seen
}

func TestThePortSpeaksTheIssuesAPI(t *testing.T) {
	g, seen := stubGitHub(t, func(r recorded) (int, string) {
		switch {
		case strings.HasPrefix(r.path, "/repos/o/r/issues?"):
			return 200, `[{"number":3,"title":"[claudinite-work] a/b","body":"x","state":"open","labels":[{"name":"task:status:blocked"}],"user":{"login":"me"}},{"number":4,"pull_request":{"merged_at":"2026-01-01T00:00:00Z"}}]`
		case r.path == "/repos/o/r/issues/9":
			return 404, `{"message":"Not Found"}`
		case r.method == "DELETE":
			return 404, `{"message":"Label does not exist"}`
		case r.path == "/repos/o/r/labels" && strings.Contains(r.body, `"task:status:done"`):
			return 422, `{"message":"already_exists"}`
		case r.path == "/repos/o/r/collaborators/stranger/permission":
			return 404, `{}`
		case r.path == "/repos/o/r/collaborators/me/permission":
			return 200, `{"permission":"write","role_name":"maintain"}`
		}
		return 200, `{}`
	})
	got, err := g.IssuesPage(world.Query{State: "closed", Since: "2026-01-01T00:00:00.000Z", Label: workitem.OriginAdHoc}, 2)
	if err != nil || len(got) != 2 || got[0].Author != "me" || !got[0].Is(workitem.StatusBlocked) || !got[1].PullRequest || got[1].MergedAt == "" {
		t.Fatalf("page %+v %v", got, err)
	}
	q := (*seen)[0].path
	for _, want := range []string{"state=closed", "sort=updated", "direction=desc", "per_page=100", "page=2", "labels=task%3Aorigin%3Aad-hoc", "since=2026-01-01"} {
		if !strings.Contains(q, want) {
			t.Errorf("query %s lacks %s", q, want)
		}
	}
	if _, err := g.Issue(9); !errors.Is(err, world.ErrGone) {
		t.Errorf("a 404 read is gone: %v", err)
	}
	if err := g.RemoveLabel(3, "task:status:blocked"); err != nil {
		t.Errorf("removing an absent label is the end state asked for: %v", err)
	}
	if err := g.EnsureLabels([]workitem.Label{{Name: workitem.StatusDone, Color: "fff"}}); err != nil {
		t.Errorf("ensure: %v", err)
	}
	last := (*seen)[len(*seen)-1]
	if last.method != "PATCH" || last.path != "/repos/o/r/labels/task:status:done" {
		t.Errorf("a label that exists is reconciled, got %+v", last)
	}
	if _, err := g.Permission("stranger"); !errors.Is(err, world.ErrGone) {
		t.Errorf("a stranger is no collaborator: %v", err)
	}
	if role, _ := g.Permission("me"); role != "maintain" {
		t.Errorf("role %q", role)
	}
	if err := g.CloseIssue(3, "not_planned"); err != nil {
		t.Fatal(err)
	}
	var body map[string]string
	_ = json.Unmarshal([]byte((*seen)[len(*seen)-1].body), &body)
	if body["state"] != "closed" || body["state_reason"] != "not_planned" {
		t.Errorf("close %v", body)
	}
}

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
			return 200, `{"workflow_runs":[{"id":41,"name":"CI","event":"pull_request","status":"completed","conclusion":"success"}]}`
		case r.path == "/repos/o/r/actions/runs/42/approve":
			return 403, `{"message":"Resource not accessible by integration"}`
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
	if runs, err := g.RunsForSHA("abc"); err != nil || len(runs) != 1 || runs[0].Conclusion != "success" || runs[0].ID != 41 || runs[0].Event != "pull_request" {
		t.Error(runs, err)
	}
	if err := g.ApproveRun(41); err != nil {
		t.Error(err)
	}
	if err := g.ApproveRun(42); !errors.As(err, &se) || se.Status != 403 {
		t.Error("the lane reads a refused approval's status:", err)
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

func TestThePortRefusesALabelOffTheApprovedList(t *testing.T) {
	g, seen := stubGitHub(t, func(recorded) (int, string) { return 200, `{"number":1}` })
	if err := g.AddLabel(3, "made-up"); err == nil {
		t.Error("adding an unapproved label succeeded")
	}
	if _, err := g.CreateIssue("t", "b", []string{workitem.OriginManual, "made-up"}); err == nil {
		t.Error("filing an issue under an unapproved label succeeded")
	}
	if err := g.EnsureLabels([]workitem.Label{{Name: "made-up"}}); err == nil {
		t.Error("defining an unapproved label succeeded")
	}
	if len(*seen) != 0 {
		t.Errorf("a refused label still reached GitHub: %+v", *seen)
	}
}
