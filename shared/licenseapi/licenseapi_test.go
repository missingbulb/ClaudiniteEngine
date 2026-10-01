package licenseapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type seen struct {
	method, path, auth string
	body               map[string]any
}

func server(t *testing.T, log *[]seen, answer func(w http.ResponseWriter, r *http.Request)) *Client {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		*log = append(*log, seen{r.Method, r.URL.Path, r.Header.Get("Authorization"), b})
		w.Header().Set("Content-Type", "application/json")
		answer(w, r)
	}))
	t.Cleanup(srv.Close)
	return &Client{Base: srv.URL, HTTP: srv.Client()}
}

func TestTheSixRoutes(t *testing.T) {
	var log []seen
	c := server(t, &log, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/login/config":
			_, _ = io.WriteString(w, `{"client_id": "cid", "device_code_url": "https://github.com/login/device/code", "token_url": "https://github.com/login/oauth/access_token"}`)
		case "/v1/login/refresh":
			_, _ = io.WriteString(w, `{"access_token": "ghu_2", "expires_in": 28800, "refresh_token": "ghr_2", "refresh_token_expires_in": 100}`)
		default:
			_, _ = io.WriteString(w, `{"key": "{\"k\":1}", "plan": "public", "state": "ok"}`)
		}
	})
	cfg, err := c.LoginConfig()
	if err != nil || cfg.ClientID != "cid" || cfg.TokenURL == "" || cfg.DeviceCodeURL == "" {
		t.Fatalf("config %+v %v", cfg, err)
	}
	tok, err := c.Refresh("ghr_1")
	if err != nil || tok.AccessToken != "ghu_2" || tok.RefreshToken != "ghr_2" {
		t.Fatalf("refresh %+v %v", tok, err)
	}
	for name, call := range map[string]func() (KeyAnswer, error){
		"/v1/session-key":        func() (KeyAnswer, error) { return c.SessionKey("ghu", "acme/member", "nonce", "1.1.0") },
		"/v1/public/session-key": func() (KeyAnswer, error) { return c.PublicSessionKey("ghu", "acme/member", "nonce", "1.1.0") },
		"/v1/actions-key":        func() (KeyAnswer, error) { return c.ActionsKey("jwt", "1.1.0") },
		"/v1/item-grant":         func() (KeyAnswer, error) { return c.ItemGrant("jwt", 12, "1.1.0") },
	} {
		k, err := call()
		if err != nil || k.Key != `{"k":1}` || k.Plan != "public" || k.State != "ok" {
			t.Errorf("%s: %+v %v", name, k, err)
		}
	}
	byPath := map[string]seen{}
	for _, s := range log {
		byPath[s.path] = s
	}
	if s := byPath["/v1/session-key"]; s.auth != "Bearer ghu" || s.body["repo"] != "acme/member" || s.body["nonce"] != "nonce" || s.body["engine_version"] != "1.1.0" {
		t.Errorf("session-key %+v", s)
	}
	if s := byPath["/v1/actions-key"]; s.auth != "Bearer jwt" || s.body["engine_version"] != "1.1.0" {
		t.Errorf("actions-key %+v", s)
	}
	if s := byPath["/v1/login/refresh"]; s.body["refresh_token"] != "ghr_1" || s.auth != "" {
		t.Errorf("refresh %+v", s)
	}
	if s := byPath["/v1/item-grant"]; s.body["issue"] != float64(12) {
		t.Errorf("item-grant %+v", s)
	}
}

func TestARefusalIsTyped(t *testing.T) {
	var log []seen
	c := server(t, &log, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"refused": "app-not-installed"}`)
	})
	_, err := c.ActionsKey("jwt", "1.1.0")
	var ref *Refusal
	if !errors.As(err, &ref) || ref.Status != 403 || ref.Reason != "app-not-installed" {
		t.Fatalf("%v", err)
	}
	if IsUnreachable(err) {
		t.Error("a refusal reads as unreachable")
	}
}

func TestAnUnreachableServerIsNotARefusal(t *testing.T) {
	c := &Client{Base: "https://127.0.0.1:1", HTTP: &http.Client{Timeout: time.Second}}
	_, err := c.ActionsKey("jwt", "1.1.0")
	if err == nil || !IsUnreachable(err) {
		t.Fatalf("%v", err)
	}
}

func TestAServerErrorWithoutAReasonIsAStatus(t *testing.T) {
	var log []seen
	c := server(t, &log, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `bad gateway`)
	})
	_, err := c.ActionsKey("jwt", "1.1.0")
	var ref *Refusal
	if !errors.As(err, &ref) || ref.Status != 502 || ref.Reason != "" {
		t.Fatalf("%v", err)
	}
}

func TestTheBodyIsCapped(t *testing.T) {
	var log []seen
	c := server(t, &log, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"key": "`+strings.Repeat("a", MaxBody)+`"}`)
	})
	if _, err := c.ActionsKey("jwt", "1.1.0"); err == nil || !strings.Contains(err.Error(), "cap") {
		t.Fatalf("%v", err)
	}
}

func TestBaseMustBeHTTPSUnlessLoopback(t *testing.T) {
	for base, ok := range map[string]bool{
		"https://license.claudinite.com": true,
		"http://127.0.0.1:8080":          true,
		"http://localhost:8080":          true,
		"http://license.claudinite.com":  false,
		"ftp://x":                        false,
	} {
		_, err := New(base)
		if (err == nil) != ok {
			t.Errorf("%s: %v", base, err)
		}
	}
	t.Setenv("CLAUDINITE_LICENSE_API", "")
	c, err := FromEnv()
	if err != nil || c.Base != DefaultBase || c.HTTP.Timeout != Timeout {
		t.Fatalf("%+v %v", c, err)
	}
}
