package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/githubapi"
)

func startFleet(t *testing.T, members ...string) (*githubapi.Client, *httptest.Server) {
	t.Helper()
	bare, _ := fixture(t)
	rs := &repos{home: "acme/manager"}
	for _, m := range members {
		if err := rs.Set(m); err != nil {
			t.Fatal(err)
		}
	}
	st := newStub(bare, rs.home, "tok")
	st.fleet = rs.members
	srv := httptest.NewTLSServer(st)
	t.Cleanup(srv.Close)
	return &githubapi.Client{Base: srv.URL, Token: "tok", HTTP: srv.Client()}, srv
}

func write(t *testing.T, dir, rel, text string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTheRepoFlagNamesTheManagerAndEachMember(t *testing.T) {
	rs := &repos{home: "acme/member"}
	for _, v := range []string{"acme/manager", "acme/a=/tmp/a", "acme/old=/tmp/o;archived;fork"} {
		if err := rs.Set(v); err != nil {
			t.Fatal(err)
		}
	}
	if rs.home != "acme/manager" || len(rs.members) != 2 || !rs.members[1].archived || !rs.members[1].fork || rs.members[0].dir != "/tmp/a" {
		t.Fatalf("%+v %+v", rs, rs.members)
	}
	if err := rs.Set("acme/x=/d;sideways"); err == nil {
		t.Error("an unknown member flag was taken")
	}
}

func TestAFleetMemberIsServedFromItsDirectory(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".claudinite/settings.yaml", "engine: {}\n")
	c, _ := startFleet(t, "acme/a="+dir, "acme/old="+t.TempDir()+";archived")
	status, raw, err := c.Raw("GET", "/user/repos?affiliation=owner&per_page=100&page=1", nil)
	if err != nil || status != 200 {
		t.Fatalf("%d %v", status, err)
	}
	var list []struct {
		FullName string `json:"full_name"`
		Archived bool   `json:"archived"`
		Owner    struct{ Login string }
	}
	_ = json.Unmarshal(raw, &list)
	if len(list) != 3 || list[0].FullName != "acme/manager" || list[1].FullName != "acme/a" || !list[2].Archived || list[1].Owner.Login != "acme" {
		t.Fatalf("%s", raw)
	}
	if status, raw, _ = c.Raw("GET", "/user/repos?affiliation=owner&per_page=100&page=2", nil); string(raw) != "[]\n" {
		t.Fatalf("page 2: %d %s", status, raw)
	}
	status, raw, _ = c.Raw("GET", "/repos/acme/a/contents/.claudinite", nil)
	if status != 200 || !strings.Contains(string(raw), `"name":"settings.yaml"`) {
		t.Fatalf("listing: %d %s", status, raw)
	}
	status, raw, _ = c.Raw("GET", "/repos/acme/a/contents/.claudinite/settings.yaml", nil)
	if status != 200 || !strings.Contains(string(raw), `"content":"ZW5naW5lOiB7fQo="`) {
		t.Fatalf("file: %d %s", status, raw)
	}
	if status, _, _ = c.Raw("GET", "/repos/acme/a/contents/missing", nil); status != 404 {
		t.Fatalf("missing: %d", status)
	}
	if status, _, _ = c.Raw("GET", "/repos/acme/a/contents/../../etc/passwd", nil); status != 404 {
		t.Fatalf("escape: %d", status)
	}
}

func TestADispatchLandsTheAdvanceAndStartsARun(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".github/workflows/claudinite-scheduler.yml", "on: {}\n")
	c, srv := startFleet(t, "acme/a="+dir, "acme/b="+t.TempDir())
	control(t, srv, "/_stub/advance", map[string]any{"repo": "acme/a", "files": map[string]string{".claudinite/settings.yaml": "moved\n"}})
	status, _, _ := c.Raw("POST", "/repos/acme/a/actions/workflows/claudinite-scheduler.yml/dispatches", map[string]any{"ref": "main", "inputs": map[string]string{"wake": "update"}})
	if status != 204 {
		t.Fatalf("dispatch %d", status)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, ".claudinite", "settings.yaml")); string(got) != "moved\n" {
		t.Errorf("the advance did not land: %q", got)
	}
	_, raw, _ := c.Raw("GET", "/repos/acme/a/actions/workflows/claudinite-scheduler.yml/runs?event=workflow_dispatch&per_page=20", nil)
	if !strings.Contains(string(raw), `"total_count":1`) {
		t.Errorf("runs %s", raw)
	}
	if status, _, _ = c.Raw("POST", "/repos/acme/b/actions/workflows/claudinite-scheduler.yml/dispatches", map[string]any{"ref": "main"}); status != 404 {
		t.Errorf("a member with no scheduler answered %d", status)
	}
	st := state(t, srv)
	if len(st.Dispatches) != 1 || st.Dispatches[0].Repo != "acme/a" || st.Dispatches[0].Inputs["wake"] != "update" {
		t.Errorf("%+v", st.Dispatches)
	}
}

func TestADispatchCanStartNoRunAndADeniedMemberAnswers403(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".github/workflows/claudinite-scheduler.yml", "on: {}\n")
	c, srv := startFleet(t, "acme/a="+dir)
	control(t, srv, "/_stub/advance", map[string]any{"repo": "acme/a", "run": false})
	_, _, _ = c.Raw("POST", "/repos/acme/a/actions/workflows/claudinite-scheduler.yml/dispatches", map[string]any{"ref": "main"})
	if _, raw, _ := c.Raw("GET", "/repos/acme/a/actions/workflows/claudinite-scheduler.yml/runs", nil); !strings.Contains(string(raw), `"total_count":0`) {
		t.Errorf("runs %s", raw)
	}
	control(t, srv, "/_stub/deny", map[string]any{"repo": "acme/a"})
	if status, _, _ := c.Raw("GET", "/repos/acme/a/contents/.github", nil); status != 403 {
		t.Errorf("denied read answered %d", status)
	}
	if status, _, _ := (&githubapi.Client{Base: c.Base, Token: "wrong", HTTP: c.HTTP}).Raw("GET", "/user/repos", nil); status != 401 {
		t.Errorf("a wrong token listed repos: %d", status)
	}
}
