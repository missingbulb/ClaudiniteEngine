package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/tasks/world"
)

func envOf(m map[string]string) world.Env { return func(k string) string { return m[k] } }

func TestTheHoldExitsBeforeAnyRead(t *testing.T) {
	var out bytes.Buffer
	env := envOf(map[string]string{world.VarsBagEnv: `{"` + world.SuspendAllVar + `": "true"}`})
	if err := cmdScheduleRun([]string{"--repo", filepath.Join(t.TempDir(), "absent")}, &out, env); err != nil {
		t.Fatalf("held run: %v", err)
	}
	if !strings.Contains(out.String(), "the queue is held") || !strings.Contains(out.String(), "[cn] tasks schedule ok ") {
		t.Fatalf("output %q", out.String())
	}
}

func TestADormantProjectIsNotScheduled(t *testing.T) {
	repo := t.TempDir()
	p := filepath.Join(repo, ".claudinite/settings.yaml")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte("packs:\n  declared:\n    - id: claudinite-tasks\n      config:\n        dormant: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := cmdScheduleRun([]string{"--repo", repo}, &out, envOf(nil)); err != nil {
		t.Fatalf("dormant run: %v", err)
	}
	if !strings.Contains(out.String(), "declares its scheduler dormant") {
		t.Fatalf("output %q", out.String())
	}
	_ = os.WriteFile(p, []byte("packs:\n  declared:\n    - id: claudinite-tasks\n      config:\n        dormant: \"yes\"\n"), 0o644)
	out.Reset()
	err := cmdScheduleRun([]string{"--repo", repo}, &out, envOf(nil))
	if !strings.Contains(out.String(), `"dormant" on the "claudinite-tasks" pack entry must be true or false`) || err == nil {
		t.Fatalf("a mistyped dormancy reads as awake and is named: %v %q", err, out.String())
	}
}

type recorded struct{ method, path, body string }

func stubGitHub(t *testing.T, answer func(r recorded) (int, string)) (ghWorld, *[]recorded) {
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
	return ghWorld{&githubapi.Client{Base: srv.URL, Repo: "o/r", Token: "t", HTTP: srv.Client()}}, &seen
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
		case r.path == "/repos/o/r/labels" && strings.Contains(r.body, `"exists"`):
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
	if err := g.EnsureLabels([]workitem.Label{{Name: "exists", Color: "fff"}}); err != nil {
		t.Errorf("ensure: %v", err)
	}
	last := (*seen)[len(*seen)-1]
	if last.method != "PATCH" || last.path != "/repos/o/r/labels/exists" {
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
