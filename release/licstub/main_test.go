package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/license"
	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
)

func devRoot(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	raw, err := os.ReadFile("../../testkeys/root.key")
	if err != nil {
		t.Fatal(err)
	}
	k, err := sign.ParsePrivateKey(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// fakeGH answers GET /user and GET /repos/acme/member as ghstub does.
func fakeGH(t *testing.T, private, push bool) func() (string, *http.Client, error) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ghu_") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/user":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 7, "login": "acme-dev", "type": "User"})
		case "/repos/acme/member":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1001, "private": private, "owner": map[string]any{"id": 3, "login": "acme", "type": "User"},
				"permissions": map[string]bool{"push": push}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return func() (string, *http.Client, error) { return srv.URL, srv.Client(), nil }
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

// Every key licstub signs verifies with the development root that
// certifies it, binds to what it was asked for, and carries the chunk-3
// fields it was told to.
func TestEveryKeyItSignsVerifies(t *testing.T) {
	roots := []ed25519.PublicKey{devRoot(t).Public().(ed25519.PublicKey)}
	for _, cfg := range []config{
		{},
		{State: "grace", Seats: &license.Seats{Paid: 1, Counted: 3}, CheckoutURL: "https://polar.sh/c/acme"},
		{State: "degraded"},
		{State: "unverified", Plan: "private-repo"},
		{Plan: "organization", Release: &license.Release{Held: []string{"60930.2.0"}, PackIndexSerial: 4, PackKeys: []string{"0123456789abcdef"}}},
	} {
		st, err := newStub(devRoot(t), cfg, fakeGH(t, false, true), time.Now)
		if err != nil {
			t.Fatal(err)
		}
		var keys []string
		// web
		code, out := call(t, st, http.MethodPost, "/_stub/webhook", "", map[string]any{"event_type": "claudinite-key",
			"client_payload": map[string]any{"nonce": "nonce-0123456789abcd", "head": "abc"},
			"sender":         map[string]any{"id": 7, "login": "acme-dev", "type": "User"},
			"repository":     map[string]any{"id": 1001, "private": false, "owner": map[string]any{"id": 3, "login": "acme", "type": "User"}}})
		if code != 200 || out["title"] != "Claudinite key" {
			t.Fatalf("%+v: webhook %d %v", cfg, code, out)
		}
		keys = append(keys, out["text"].(string))
		// desktop
		code, out = call(t, st, http.MethodPost, "/v1/session-key", "ghu_x", map[string]any{"repo": "acme/member", "nonce": "nonce-0123456789abcd"})
		if code != 200 {
			t.Fatalf("%+v: session-key %d %v", cfg, code, out)
		}
		keys = append(keys, out["key"].(string))
		// actions, then a grant from it
		code, out = call(t, st, http.MethodPost, "/v1/actions-key", oidcToken(actionsClaims(nil)), map[string]any{"engine_version": "60930.1.0"})
		if code != 200 {
			t.Fatalf("%+v: actions-key %d %v", cfg, code, out)
		}
		actions := out["key"].(string)
		keys = append(keys, actions)
		code, out = call(t, st, http.MethodPost, "/v1/item-grant", actions, map[string]any{"issue": 12})
		if code != 200 {
			t.Fatalf("%+v: item-grant %d %v", cfg, code, out)
		}
		keys = append(keys, out["grant"].(string))
		for i, raw := range keys {
			k, err := license.VerifyKey([]byte(raw), roots, time.Now())
			if err != nil {
				t.Errorf("%+v key %d: %v", cfg, i, err)
				continue
			}
			if k.RepoID != 1001 || k.OwnerID != 3 {
				t.Errorf("%+v key %d: %+v", cfg, i, k)
			}
			if cfg.State != "" && k.State != cfg.State {
				t.Errorf("%+v key %d: state %s", cfg, i, k.State)
			}
			if cfg.CheckoutURL != "" && (k.CheckoutURL == nil || *k.CheckoutURL != cfg.CheckoutURL || k.Seats == nil) {
				t.Errorf("%+v key %d: chunk-3 fields %+v", cfg, i, k)
			}
			if cfg.Release != nil && k.Release.PackIndexSerial != 4 {
				t.Errorf("%+v key %d: release %+v", cfg, i, k.Release)
			}
			if i < 2 && (k.UserID == nil || *k.UserID != 7 || k.Nonce != "nonce-0123456789abcd") {
				t.Errorf("%+v session key %d: %+v", cfg, i, k)
			}
			if i == 3 && (k.Issue == nil || *k.Issue != 12) {
				t.Errorf("%+v grant: %+v", cfg, k)
			}
		}
	}
}

func TestActionsPinRules(t *testing.T) {
	st, err := newStub(devRoot(t), config{}, fakeGH(t, false, true), time.Now)
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

func TestDesktopRefusals(t *testing.T) {
	st, err := newStub(devRoot(t), config{RejectToken: "ghu_old"}, fakeGH(t, false, false), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if code, out := call(t, st, http.MethodPost, "/v1/session-key", "ghu_old", map[string]any{"repo": "acme/member", "nonce": "n"}); code != 401 || out["refused"] != "token-invalid" {
		t.Errorf("rejected token: %d %v", code, out)
	}
	if code, out := call(t, st, http.MethodPost, "/v1/session-key", "ghu_new", map[string]any{"repo": "acme/member", "nonce": "n"}); code != 403 || out["refused"] != "no-push-access" {
		t.Errorf("no push: %d %v", code, out)
	}
	if code, out := call(t, st, http.MethodPost, "/v1/login/refresh", "", map[string]any{"refresh_token": "ghr_1"}); code != 200 || out["access_token"] != "ghu_refreshed-1" {
		t.Errorf("refresh: %d %v", code, out)
	}
}

// A desktop refusal licstub is told to make carries the checkout link it
// was given, as a no-plan refusal names where a plan is picked.
func TestDesktopRefusalCarriesTheCheckout(t *testing.T) {
	st, err := newStub(devRoot(t), config{Refuse: map[string]string{"desktop": "no-plan"}, CheckoutURL: "https://polar.sh/c/acme"}, fakeGH(t, false, true), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	code, out := call(t, st, http.MethodPost, "/v1/session-key", "ghu_x", map[string]any{"repo": "acme/member", "nonce": "nonce-0123456789abcd"})
	if code != 403 || out["refused"] != "no-plan" || out["checkout_url"] != "https://polar.sh/c/acme" {
		t.Errorf("%d %v", code, out)
	}
}

// The refusal vocabulary is the Worker README's. CLAUDINITE_LICENSES names
// a ClaudiniteLicenses checkout; without it the comparison is skipped.
func TestRefusalsAreTheWorkersVocabulary(t *testing.T) {
	dir := os.Getenv("CLAUDINITE_LICENSES")
	if dir == "" {
		t.Skip("CLAUDINITE_LICENSES names no ClaudiniteLicenses checkout")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "workers", "key", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	// The reasons are the backticked hyphenated words of the Paths and
	// login-contract sections, less the workflow and event names, plus the
	// grant path's key-invalid, which the README spells inside its JSON.
	start, end := strings.Index(doc, "## Paths"), strings.Index(doc, "## Secrets")
	if start < 0 || end < start {
		t.Fatal("the README's sections moved")
	}
	re := regexp.MustCompile("`([a-z][a-z0-9]*(?:-[a-z0-9]+)+)`")
	found := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(doc[start:end], -1) {
		if !strings.HasPrefix(m[1], "claudinite-") {
			found[m[1]] = true
		}
	}
	if strings.Contains(doc[start:end], `"refused": "key-invalid"`) {
		found["key-invalid"] = true
	}
	var got []string
	for r := range found {
		got = append(got, r)
	}
	sort.Strings(got)
	want := slices.Clone(Refusals)
	sort.Strings(want)
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("README reasons %v\nlicstub reasons %v", got, want)
	}
}
