package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/githubapi"
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	dir := t.TempDir()
	write(t, dir, ".github/workflows/claudinite-scheduler.yml", "on: {}\n")
	c, srv := startFleet(t, "acme/a="+dir)
	control(t, srv, "/_stub/advance", map[string]any{"repo": "acme/a", "run": false})
	_, _, _ = c.Raw("POST", "/repos/acme/a/actions/workflows/claudinite-scheduler.yml/dispatches", map[string]any{"ref": "main"})
	if _, raw, _ := c.Raw("GET", "/repos/acme/a/actions/workflows/claudinite-scheduler.yml/runs", nil); !strings.Contains(string(raw), `"total_count":0`) {
		t.Errorf("runs %s", raw)
	}
	control(t, srv, "/_stub/deny", map[string]any{"repo": "acme/a", "path": ".claudinite"})
	if status, _, _ := c.Raw("GET", "/repos/acme/a/contents/.claudinite", nil); status != 403 {
		t.Errorf("denied read answered %d", status)
	}
	if status, _, _ := c.Raw("GET", "/repos/acme/a/contents/.github", nil); status != 200 {
		t.Errorf("a read outside the denied path answered %d", status)
	}
	control(t, srv, "/_stub/deny", map[string]any{"repo": "acme/a"})
	if status, _, _ := c.Raw("GET", "/repos/acme/a/contents/.github", nil); status != 403 {
		t.Errorf("a whole-member deny answered %d", status)
	}
	control(t, srv, "/_stub/deny", map[string]any{"repo": "acme/a", "deny": false})
	if status, _, _ := c.Raw("GET", "/repos/acme/a/contents/.github", nil); status != 200 {
		t.Errorf("a lifted deny answered %d", status)
	}
	if n := strings.Count(strings.Join(state(t, srv).Calls, "\n"), "member acme/a GET /contents/.github"); n != 3 {
		t.Errorf("member calls logged %d times", n)
	}
	if status, _, _ := (&githubapi.Client{Base: c.Base, Token: "wrong", HTTP: c.HTTP}).Raw("GET", "/user/repos", nil); status != 401 {
		t.Errorf("a wrong token listed repos: %d", status)
	}
}

// A member's issues are its own: an issue filed there, labelled and
// edited, is not the manager's, and the state lists it under the member.
func TestAFleetMemberKeepsItsOwnIssues(t *testing.T) {
	t.Parallel()
	c, srv := startFleet(t, "acme/a="+t.TempDir())
	if status, _, err := c.Raw("POST", "/repos/acme/a/labels", map[string]string{"name": "add-packs", "color": "0E8A16"}); err != nil || status != 201 {
		t.Fatalf("label: %d %v", status, err)
	}
	status, raw, err := c.Raw("POST", "/repos/acme/a/issues", map[string]any{"title": "t", "body": "b", "labels": []string{"add-packs"}})
	if err != nil || status != 201 {
		t.Fatalf("create: %d %s %v", status, raw, err)
	}
	var created struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	if status, raw, _ := c.Raw("PATCH", fmt.Sprintf("/repos/acme/a/issues/%d", created.Number), map[string]string{"body": "b2"}); status != 200 {
		t.Fatalf("edit: %d %s", status, raw)
	}
	status, raw, _ = c.Raw("GET", "/repos/acme/a/issues?labels=add-packs&state=all&per_page=100&page=1", nil)
	if status != 200 || !strings.Contains(string(raw), `"b2"`) {
		t.Fatalf("list: %d %s", status, raw)
	}
	resp, err := srv.Client().Get(srv.URL + "/_stub/state")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var st stubState
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if len(st.Issues) != 0 || len(st.Fleet["acme/a"]) != 1 || st.Fleet["acme/a"][0].Body != "b2" {
		t.Fatalf("manager %v, member %v", st.Issues, st.Fleet)
	}
}

// A Contents PUT lands in the member's directory when its sha is the
// file's as it stands, and is refused 409 when the file moved.
func TestAFleetMemberTakesAShaGuardedWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, dir, ".claudinite/settings.yaml", "engine: {}\n")
	c, _ := startFleet(t, "acme/a="+dir)
	_, raw, _ := c.Raw("GET", "/repos/acme/a/contents/.claudinite/settings.yaml", nil)
	var f struct{ SHA string }
	_ = json.Unmarshal(raw, &f)
	put := func(sha string) int {
		status, _, _ := c.Raw("PUT", "/repos/acme/a/contents/.claudinite/settings.yaml",
			map[string]string{"message": "m", "content": "cGFja3M6IHt9Cg==", "sha": sha})
		return status
	}
	if got := put("stale"); got != 409 {
		t.Errorf("a stale sha answered %d", got)
	}
	if got := put(f.SHA); got != 200 {
		t.Errorf("the read's sha answered %d", got)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, ".claudinite/settings.yaml")); string(b) != "packs: {}\n" {
		t.Errorf("file reads %q", b)
	}
	status, raw, _ := c.Raw("GET", "/repos/acme/a/git/trees/main?recursive=1", nil)
	if status != 200 || !strings.Contains(string(raw), `".claudinite/settings.yaml"`) {
		t.Errorf("tree: %d %s", status, raw)
	}
}
