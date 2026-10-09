package githubapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type recorded struct{ method, path, body, auth string }

func server(t *testing.T, answers map[string]string) (*Client, *[]recorded) {
	t.Helper()
	var mu sync.Mutex
	var log []recorded
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		log = append(log, recorded{r.Method, r.URL.RequestURI(), string(b), r.Header.Get("Authorization")})
		mu.Unlock()
		key := r.Method + " " + r.URL.Path
		a, ok := answers[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if a == "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = w.Write([]byte(a))
	}))
	t.Cleanup(srv.Close)
	return &Client{Base: srv.URL, Repo: "o/r", Token: "ghs_x", HTTP: srv.Client()}, &log
}

func TestCalls(t *testing.T) {
	c, log := server(t, map[string]string{
		"GET /repos/o/r/actions/workflows/claudinite-ci.yml/runs": `{"workflow_runs":[{"id":2,"head_sha":"abc","event":"workflow_dispatch","status":"completed","conclusion":"success","created_at":"2026-10-01T00:00:02Z"}]}`,
		"GET /repos/o/r/pulls":                                           `[{"number":7,"title":"t","state":"open","user":{"login":"github-actions[bot]"},"labels":[{"name":"claudinite-update"}],"head":{"ref":"claudinite/engine-1.2.0","sha":"abc"},"base":{"ref":"main"}}]`,
		"GET /repos/o/r/pulls/7":                                         `{"number":7,"state":"open","user":{"login":"github-actions[bot]"},"labels":[],"head":{"ref":"b","sha":"abc"},"base":{"ref":"main"}}`,
		"POST /repos/o/r/pulls":                                          `{"number":8,"head":{"ref":"b","sha":"def"}}`,
		"PATCH /repos/o/r/pulls/7":                                       `{"number":7,"state":"closed"}`,
		"PUT /repos/o/r/pulls/7/merge":                                   `{"merged":true,"sha":"fff"}`,
		"POST /repos/o/r/issues/7/comments":                              `{}`,
		"POST /repos/o/r/actions/workflows/claudinite-ci.yml/dispatches": "",
		"GET /repos/o/r/actions/runs":                                    `{"workflow_runs":[{"id":5,"name":"claudinite-ci","head_sha":"abc","event":"pull_request","status":"completed","conclusion":"action_required"}]}`,
		"POST /repos/o/r/actions/runs/5/approve":                         `{}`,
		"GET /repos/o/r/issues":                                          `[{"number":3,"title":"Claudinite engine 1.2.0 needs a workflow change","body":"b","labels":[{"name":"claudinite-update"}]},{"number":7,"title":"pr","pull_request":{}}]`,
		"POST /repos/o/r/issues":                                         `{"number":9,"title":"x"}`,
		"PATCH /repos/o/r/issues/3":                                      `{"number":3}`,
	})
	runs, err := c.WorkflowRuns("claudinite-ci.yml", "abc")
	if err != nil || len(runs) != 1 || runs[0].Conclusion != "success" || runs[0].Event != "workflow_dispatch" {
		t.Fatalf("%+v %v", runs, err)
	}
	held, err := c.HeadRuns("abc")
	if err != nil || len(held) != 1 || held[0].ID != 5 || held[0].Name != "claudinite-ci" || held[0].Conclusion != "action_required" {
		t.Fatalf("%+v %v", held, err)
	}
	if err := c.ApproveRun(5); err != nil {
		t.Fatal(err)
	}
	prs, err := c.OpenPulls()
	if err != nil || len(prs) != 1 || prs[0].Author != "github-actions[bot]" || !prs[0].HasLabel("claudinite-update") || prs[0].HeadRef != "claudinite/engine-1.2.0" {
		t.Fatalf("%+v %v", prs, err)
	}
	if pr, err := c.Pull(7); err != nil || pr.HeadSHA != "abc" {
		t.Fatalf("%+v %v", pr, err)
	}
	if pr, err := c.CreatePull("Claudinite engine 1.2.0", "body", "b", "main"); err != nil || pr.Number != 8 {
		t.Fatalf("%+v %v", pr, err)
	}
	if err := c.ClosePull(7); err != nil {
		t.Fatal(err)
	}
	if err := c.MergePull(7, "abc", "Claudinite engine 1.2.0"); err != nil {
		t.Fatal(err)
	}
	if err := c.Comment(7, "superseded"); err != nil {
		t.Fatal(err)
	}
	if err := c.Dispatch("claudinite-ci.yml", "main", map[string]string{"pr": "8"}); err != nil {
		t.Fatal(err)
	}
	issues, err := c.OpenIssues()
	if err != nil || len(issues) != 1 || issues[0].Number != 3 {
		t.Fatalf("issues %+v %v", issues, err)
	}
	if n, err := c.CreateIssue("t", "b"); err != nil || n != 9 {
		t.Fatalf("%d %v", n, err)
	}
	if err := c.UpdateIssueBody(3, "new"); err != nil {
		t.Fatal(err)
	}
	for _, r := range *log {
		if r.auth != "Bearer ghs_x" {
			t.Errorf("%s %s without the token", r.method, r.path)
		}
	}
	want := map[string]string{
		"GET /repos/o/r/actions/workflows/claudinite-ci.yml/runs?head_sha=abc&per_page=100": "",
		"GET /repos/o/r/actions/runs?head_sha=abc&per_page=100":                             "",
		"POST /repos/o/r/actions/runs/5/approve":                                            "",
		"PATCH /repos/o/r/pulls/7":                                                          `{"state":"closed"}`,
		"PUT /repos/o/r/pulls/7/merge":                                                      `{"commit_title":"Claudinite engine 1.2.0","merge_method":"squash","sha":"abc"}`,
		"POST /repos/o/r/actions/workflows/claudinite-ci.yml/dispatches":                    `{"inputs":{"pr":"8"},"ref":"main"}`,
		"POST /repos/o/r/issues":                                                            `{"body":"b","title":"t"}`,
		"GET /repos/o/r/issues?state=open&per_page=100&page=1":                              "",
	}
	for k, body := range want {
		found := false
		for _, r := range *log {
			if r.method+" "+r.path == k {
				found = true
				if body != "" && canon(t, r.body) != body {
					t.Errorf("%s body %s, want %s", k, r.body, body)
				}
			}
		}
		if !found {
			t.Errorf("no request %s", k)
		}
	}
}

func canon(t *testing.T, s string) string {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("%q: %v", s, err)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func TestErrorsNameTheCall(t *testing.T) {
	c, _ := server(t, map[string]string{})
	if err := c.MergePull(7, "abc", "t"); err == nil || !strings.Contains(err.Error(), "PUT /repos/o/r/pulls/7/merge") || !strings.Contains(err.Error(), "404") {
		t.Errorf("%v", err)
	}
	c.Base = "http://127.0.0.1:1"
	if _, err := c.OpenPulls(); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Errorf("plain http base: %v", err)
	}
}

func TestARefusedCallCarriesItsStatus(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message": "Resource not accessible by integration"}`)
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, Repo: "acme/locked", HTTP: srv.Client()}
	if err := c.do(http.MethodPost, "/repos/acme/locked/dispatches", map[string]any{}, nil); StatusOf(err) != http.StatusForbidden {
		t.Fatalf("%v", err)
	}
	c.Base = "https://127.0.0.1:1"
	if err := c.do(http.MethodPost, "/repos/acme/locked/dispatches", map[string]any{}, nil); err == nil || StatusOf(err) != 0 {
		t.Fatalf("an unreachable host: %v", err)
	}
}
