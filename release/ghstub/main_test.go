package main

import (
	"encoding/json"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/githubapi"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@x"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// fixture is a bare origin with main and one branch changing a file.
func fixture(t *testing.T) (bare, branchSHA string) {
	root := t.TempDir()
	bare, work := filepath.Join(root, "o.git"), filepath.Join(root, "w")
	git(t, root, "init", "-q", "--bare", "-b", "main", bare)
	git(t, root, "init", "-q", "-b", "main", work)
	git(t, work, "commit", "-q", "--allow-empty", "-m", "base")
	git(t, work, "remote", "add", "origin", bare)
	git(t, work, "push", "-q", "origin", "main")
	git(t, work, "checkout", "-q", "-b", "claudinite/engine-2.0.0")
	git(t, work, "commit", "-q", "--allow-empty", "-m", "pin")
	git(t, work, "push", "-q", "origin", "claudinite/engine-2.0.0")
	return bare, git(t, work, "rev-parse", "HEAD")
}

func start(t *testing.T, bare string) (*githubapi.Client, *httptest.Server) {
	srv := httptest.NewTLSServer(newStub(bare, "acme/member", "tok"))
	t.Cleanup(srv.Close)
	return &githubapi.Client{Base: srv.URL, Repo: "acme/member", Token: "tok", HTTP: srv.Client()}, srv
}

func control(t *testing.T, srv *httptest.Server, path string, body any) {
	raw, _ := json.Marshal(body)
	resp, err := srv.Client().Post(srv.URL+path, "application/json", strings.NewReader(string(raw)))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("%s: %v %v", path, err, resp)
	}
	_ = resp.Body.Close()
}

func state(t *testing.T, srv *httptest.Server) stubState {
	resp, err := srv.Client().Get(srv.URL + "/_stub/state")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var s stubState
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTheUpdatersCallsAgainstTheStub(t *testing.T) {
	bare, sha := fixture(t)
	c, srv := start(t, bare)
	pr, err := c.CreatePull("Claudinite engine 2.0.0", "body", "claudinite/engine-2.0.0", "main")
	if err != nil || pr.Number != 1 || pr.HeadSHA != sha || pr.Author != "github-actions[bot]" {
		t.Fatalf("%+v %v", pr, err)
	}
	if err := c.AddLabel(1, "claudinite-update"); err != nil {
		t.Fatal(err)
	}
	open, err := c.OpenPulls()
	if err != nil || len(open) != 1 || !open[0].HasLabel("claudinite-update") {
		t.Fatalf("%+v %v", open, err)
	}

	control(t, srv, "/_stub/dispatch", map[string]string{"conclusion": "success"})
	if err := c.Dispatch("claudinite-ci.yml", "claudinite/engine-2.0.0", map[string]string{"pr": "1"}); err != nil {
		t.Fatal(err)
	}
	runs, err := c.WorkflowRuns("claudinite-ci.yml", sha)
	if err != nil || len(runs) != 1 || runs[0].Event != "workflow_dispatch" || runs[0].Conclusion != "success" {
		t.Fatalf("%+v %v", runs, err)
	}

	if err := c.MergePull(1, "0000000000000000000000000000000000000000", "x"); err == nil {
		t.Error("merged at a stale sha")
	}
	if err := c.MergePull(1, sha, "Claudinite engine 2.0.0"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, bare, "log", "-1", "--format=%s", "main"); got != "Claudinite engine 2.0.0" {
		t.Errorf("main's head is %q", got)
	}
	if p, _ := c.Pull(1); p.State != "closed" {
		t.Errorf("merged PR state %q", p.State)
	}

	n, err := c.CreateIssue("Claudinite engine 1.0.0 is revoked", "b", "claudinite-update")
	if err != nil || n != 2 {
		t.Fatalf("%d %v", n, err)
	}
	if err := c.UpdateIssueBody(n, "b2"); err != nil {
		t.Fatal(err)
	}
	issues, err := c.OpenIssues("claudinite-update")
	if err != nil || len(issues) != 1 || issues[0].Body != "b2" {
		t.Fatalf("%+v %v", issues, err)
	}
	if err := c.Comment(1, "hello"); err != nil {
		t.Fatal(err)
	}
	s := state(t, srv)
	want := []string{"create-pull 1 claudinite/engine-2.0.0", "label 1 claudinite-update", "dispatch claudinite-ci.yml claudinite/engine-2.0.0 pr=1", "merge 1 " + sha, "create-issue 2 Claudinite engine 1.0.0 is revoked", "update-issue 2", "comment 1"}
	if strings.Join(s.Calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls:\n%s", strings.Join(s.Calls, "\n"))
	}
	if len(s.Issues) != 1 || len(s.Dispatches) != 1 {
		t.Errorf("%+v", s)
	}
}

func TestStubRunsAndClose(t *testing.T) {
	bare, sha := fixture(t)
	c, srv := start(t, bare)
	control(t, srv, "/_stub/run", map[string]string{"ref": "main", "conclusion": "failure"})
	main := git(t, bare, "rev-parse", "main")
	if runs, _ := c.WorkflowRuns("claudinite-ci.yml", main); len(runs) != 1 || runs[0].Conclusion != "failure" || runs[0].Event != "push" {
		t.Errorf("%+v", runs)
	}
	if _, err := c.CreatePull("t", "b", "claudinite/engine-2.0.0", "main"); err != nil {
		t.Fatal(err)
	}
	if err := c.ClosePull(1); err != nil {
		t.Fatal(err)
	}
	if open, _ := c.OpenPulls(); len(open) != 0 {
		t.Errorf("%+v", open)
	}
	if err := c.MergePull(1, sha, "t"); err == nil {
		t.Error("merged a closed PR")
	}
}

func TestStubRefusesAWrongTokenOrRepo(t *testing.T) {
	bare, _ := fixture(t)
	c, _ := start(t, bare)
	bad := *c
	bad.Token = "other"
	if _, err := bad.OpenPulls(); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("%v", err)
	}
	bad = *c
	bad.Repo = "acme/other"
	if _, err := bad.OpenPulls(); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("%v", err)
	}
}
