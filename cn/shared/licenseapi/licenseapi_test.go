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

func TestTheActionsKeyRoute(t *testing.T) {
	var log []seen
	c := server(t, &log, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"key": "{\"k\":1}", "plan": "personal", "state": "ok"}`)
	})
	k, err := c.ActionsKey("jwt", "1.1.0")
	if err != nil || k.Key != `{"k":1}` || k.Plan != "personal" {
		t.Fatalf("%+v %v", k, err)
	}
	if len(log) != 1 || log[0].method != http.MethodPost || log[0].path != "/v1/actions-key" || log[0].auth != "Bearer jwt" || log[0].body["engine_version"] != "1.1.0" || len(log[0].body) != 1 {
		t.Errorf("%+v", log)
	}
	c = server(t, &log, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"plan": "personal"}`) })
	if _, err := c.ActionsKey("jwt", "1.1.0"); err == nil {
		t.Error("an answer with no key")
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
}

func TestAnUnreachableServerIsNotARefusal(t *testing.T) {
	c := &Client{Base: "https://127.0.0.1:1", HTTP: &http.Client{Timeout: time.Second}}
	_, err := c.ActionsKey("jwt", "1.1.0")
	var ref *Refusal
	if err == nil || errors.As(err, &ref) {
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
