package githubapi

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
