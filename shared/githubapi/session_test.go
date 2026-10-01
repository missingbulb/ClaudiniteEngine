package githubapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sessionServer answers the session calls for acme/member and records
// each request's method, path and Authorization header.
func sessionServer(t *testing.T, seen *[]string) (*httptest.Server, *Client) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*seen = append(*seen, r.Method+" "+r.URL.RequestURI()+" auth="+r.Header.Get("Authorization")+" "+string(body))
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/user":
			_, _ = io.WriteString(w, `{"id": 7, "login": "acme-user", "type": "User"}`)
		case r.URL.Path == "/repos/acme/member":
			_, _ = io.WriteString(w, `{"id": 11, "name": "member", "full_name": "acme/member", "private": true, "default_branch": "trunk", "owner": {"id": 3, "login": "acme", "type": "Organization"}, "permissions": {"push": true}}`)
		case r.URL.Path == "/repos/acme/member/commits/trunk":
			_, _ = io.WriteString(w, `{"sha": "`+strings.Repeat("a", 40)+`"}`)
		case r.URL.Path == "/repos/acme/member/dispatches":
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/check-runs"):
			_, _ = io.WriteString(w, `{"total_count": 1, "check_runs": [{"id": 1, "name": "Claudinite key", "external_id": "n1", "status": "completed", "conclusion": "neutral", "output": {"title": "Claudinite key", "summary": "s", "text": "KEY"}, "app": {"slug": "claudinite"}}]}`)
		case r.URL.Path == "/repos/acme/locked/dispatches":
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"message": "Resource not accessible by integration"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &Client{Base: srv.URL, Repo: "acme/member", HTTP: srv.Client()}
}

func TestSessionCalls(t *testing.T) {
	var seen []string
	_, c := sessionServer(t, &seen)
	u, err := c.User()
	if err != nil || u.ID != 7 || u.Login != "acme-user" || u.Type != "User" {
		t.Fatalf("user %+v %v", u, err)
	}
	ri, err := c.RepoInfo()
	if err != nil || ri.ID != 11 || !ri.Private || ri.DefaultBranch != "trunk" || ri.Owner.ID != 3 || ri.Owner.Type != "Organization" || !ri.Permissions.Push {
		t.Fatalf("repo %+v %v", ri, err)
	}
	head, err := c.BranchHead("trunk")
	if err != nil || head != strings.Repeat("a", 40) {
		t.Fatalf("head %q %v", head, err)
	}
	if err := c.RepositoryDispatch("claudinite-key", map[string]string{"nonce": "n1"}); err != nil {
		t.Fatal(err)
	}
	runs, err := c.CheckRuns(head, "Claudinite key")
	if err != nil || len(runs) != 1 || runs[0].ExternalID != "n1" || runs[0].Output.Text != "KEY" || runs[0].App.Slug != "claudinite" {
		t.Fatalf("runs %+v %v", runs, err)
	}
	for _, s := range seen {
		if !strings.Contains(s, "auth= ") {
			t.Errorf("a tokenless client sent a credential: %s", s)
		}
	}
	if !strings.Contains(seen[3], `{"client_payload":{"nonce":"n1"},"event_type":"claudinite-key"}`) {
		t.Errorf("dispatch body: %s", seen[3])
	}
	if !strings.Contains(seen[4], "check_name=Claudinite+key") && !strings.Contains(seen[4], "check_name=Claudinite%20key") {
		t.Errorf("check-runs query: %s", seen[4])
	}
}

func TestSessionCallsSendATokenWhenGiven(t *testing.T) {
	var seen []string
	_, c := sessionServer(t, &seen)
	c.Token = "tok"
	if _, err := c.User(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(seen[0], "auth=Bearer tok") {
		t.Errorf("%s", seen[0])
	}
}

func TestARefusedDispatchCarriesItsStatus(t *testing.T) {
	var seen []string
	_, c := sessionServer(t, &seen)
	c.Repo = "acme/locked"
	err := c.RepositoryDispatch("claudinite-key", map[string]string{})
	if StatusOf(err) != http.StatusForbidden {
		t.Fatalf("%v", err)
	}
	c.Base = "https://127.0.0.1:1"
	if err := c.RepositoryDispatch("claudinite-key", map[string]string{}); err == nil || StatusOf(err) != 0 {
		t.Fatalf("an unreachable host: %v", err)
	}
}

func TestParseRemote(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/acme/member.git":      "acme/member",
		"https://github.com/acme/member":          "acme/member",
		"https://x-access-token:t@github.com/a/b": "a/b",
		"git@github.com:acme/member.git":          "acme/member",
		"ssh://git@github.com/acme/member.git":    "acme/member",
		"git://github.com/acme/member.git":        "acme/member",
		"https://gitlab.com/acme/member.git":      "",
		"/tmp/origin.git":                         "",
		"https://github.com/acme":                 "",
		"https://github.com/acme/member/extra":    "",
	} {
		got, ok := ParseRemote(in)
		if got != want || ok != (want != "") {
			t.Errorf("%s: %q %v, want %q", in, got, ok, want)
		}
	}
}

func TestOIDCToken(t *testing.T) {
	var got *http.Request
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		_, _ = io.WriteString(w, `{"value": "jwt"}`)
	}))
	defer srv.Close()
	env := map[string]string{"ACTIONS_ID_TOKEN_REQUEST_URL": srv.URL + "/token?api-version=2.0", "ACTIONS_ID_TOKEN_REQUEST_TOKEN": "req"}
	tok, err := OIDCToken(srv.Client(), func(k string) string { return env[k] }, "claudinite")
	if err != nil || tok != "jwt" {
		t.Fatalf("%q %v", tok, err)
	}
	if got.URL.Query().Get("audience") != "claudinite" || got.URL.Query().Get("api-version") != "2.0" || got.Header.Get("Authorization") != "bearer req" {
		t.Errorf("request %s %v", got.URL, got.Header)
	}
	if _, err := OIDCToken(srv.Client(), func(string) string { return "" }, "claudinite"); !errors.Is(err, ErrNoOIDC) {
		t.Errorf("no variables: %v", err)
	}
}

func TestReadEvent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "event.json")
	raw, _ := json.Marshal(map[string]any{"repository": map[string]any{"id": 11, "private": true, "owner": map[string]any{"id": 3, "login": "acme", "type": "Organization"}}})
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	e, err := ReadEvent(p)
	if err != nil || e.ID != 11 || !e.Private || e.Owner.ID != 3 || e.Owner.Type != "Organization" || e.Owner.Login != "acme" {
		t.Fatalf("%+v %v", e, err)
	}
	if _, err := ReadEvent(filepath.Join(t.TempDir(), "none.json")); err == nil {
		t.Error("a missing event file read")
	}
}

func TestDeviceFlow(t *testing.T) {
	polls := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept %q", r.Header.Get("Accept"))
		}
		switch r.URL.Path {
		case "/login/device/code":
			if r.URL.Query().Get("client_id") != "cid" {
				t.Errorf("device code query %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"device_code": "dc", "user_code": "ABCD-1234", "verification_uri": "https://github.com/login/device", "expires_in": 900, "interval": 5}`)
		case "/login/oauth/access_token":
			q := r.URL.Query()
			if q.Get("device_code") != "dc" || q.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
				t.Errorf("token query %s", r.URL.RawQuery)
			}
			polls++
			if polls == 1 {
				_, _ = io.WriteString(w, `{"error": "authorization_pending"}`)
				return
			}
			_, _ = io.WriteString(w, `{"access_token": "ghu_x", "expires_in": 28800, "refresh_token": "ghr_y", "refresh_token_expires_in": 15897600, "token_type": "bearer"}`)
		}
	}))
	defer srv.Close()
	d, err := DeviceCode(srv.Client(), srv.URL+"/login/device/code", "cid")
	if err != nil || d.UserCode != "ABCD-1234" || d.Interval != 5 || d.DeviceCode != "dc" {
		t.Fatalf("%+v %v", d, err)
	}
	tok, err := DeviceToken(srv.Client(), srv.URL+"/login/oauth/access_token", "cid", "dc")
	if err == nil || tok.Error != "authorization_pending" {
		t.Fatalf("first poll %+v %v", tok, err)
	}
	tok, err = DeviceToken(srv.Client(), srv.URL+"/login/oauth/access_token", "cid", "dc")
	if err != nil || tok.AccessToken != "ghu_x" || tok.RefreshToken != "ghr_y" || tok.ExpiresIn != 28800 {
		t.Fatalf("second poll %+v %v", tok, err)
	}
	if _, err := DeviceCode(srv.Client(), "http://github.example/login/device/code", "cid"); err == nil || !strings.Contains(err.Error(), "HTTPS only") {
		t.Errorf("device flow over plain HTTP: %v", err)
	}
}
