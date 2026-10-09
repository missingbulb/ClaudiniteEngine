package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/fleet/entitlement"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/licenseapi"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/sign"
)

func devRoot(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	raw, err := os.ReadFile("../../../keys/testkeys/root.key")
	if err != nil {
		t.Fatal(err)
	}
	k, err := sign.ParsePrivateKey(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func call(t *testing.T, h http.Handler, method, path, bearer string, body any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func oidcToken(claims map[string]any) string {
	b64 := base64.RawURLEncoding
	body, _ := json.Marshal(claims)
	return b64.EncodeToString([]byte(`{"alg":"RS256"}`)) + "." + b64.EncodeToString(body) + ".sig"
}

func actionsClaims(edit map[string]any) map[string]any {
	c := map[string]any{"aud": "claudinite", "repository": "acme/member", "repository_owner": "acme", "repository_id": "1001",
		"repository_owner_id": "3", "repository_visibility": "public", "event_name": "schedule",
		"job_workflow_ref": "acme/member/.github/workflows/claudinite-update.yml@refs/heads/main"}
	for k, v := range edit {
		c[k] = v
	}
	return c
}

// Every key licstub signs passes the fleet's own check with the
// development root that certifies it, naming the owner and the plan it was
// told to; a plan that holds no fleet is refused there.
func TestEveryKeyItSignsPassesTheFleetCheck(t *testing.T) {
	roots := []ed25519.PublicKey{devRoot(t).Public().(ed25519.PublicKey)}
	for _, c := range []struct {
		cfg     config
		refused bool
	}{
		{config{Plan: "personal"}, false},
		{config{Plan: "organization", OwnerType: "Organization"}, false},
		{config{}, true},
	} {
		st, err := newStub(devRoot(t), c.cfg, time.Now)
		if err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewTLSServer(st)
		v := entitlement.Check(entitlement.In{Server: &licenseapi.Client{Base: srv.URL, HTTP: srv.Client()},
			OIDC:   func() (string, error) { return oidcToken(actionsClaims(nil)), nil },
			Getenv: func(k string) string { return map[string]string{"GITHUB_REPOSITORY_ID": "1001"}[k] },
			Roots:  roots, Now: time.Now(), Engine: "1.60930.1"})
		srv.Close()
		if v.Refused != c.refused || v.Unverified {
			t.Errorf("%+v: %+v", c.cfg, v)
		}
		if !v.Refused && (v.OwnerID != 3 || v.OwnerLogin != "acme" || v.Plan != c.cfg.Plan) {
			t.Errorf("%+v: entitled as %+v", c.cfg, v)
		}
	}
}

func TestActionsPinRules(t *testing.T) {
	st, err := newStub(devRoot(t), config{}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	for edit, want := range map[string]struct {
		claims map[string]any
		reason string
	}{
		"pull request": {map[string]any{"event_name": "pull_request"}, "pull-request-trigger"},
		"ci workflow":  {map[string]any{"job_workflow_ref": "acme/member/.github/workflows/claudinite-ci.yml@refs/heads/main"}, "workflow-not-pinned"},
		"a branch":     {map[string]any{"job_workflow_ref": "acme/member/.github/workflows/claudinite-update.yml@refs/heads/dev"}, "workflow-not-pinned"},
		"audience":     {map[string]any{"aud": "sts.amazonaws.com"}, "token-audience"},
	} {
		code, out := call(t, st, http.MethodPost, "/v1/actions-key", oidcToken(actionsClaims(want.claims)), map[string]any{})
		if code == 200 || out["refused"] != want.reason {
			t.Errorf("%s: %d %v", edit, code, out)
		}
	}
}
