package npmreg

import (
	"crypto/sha512"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
)

// The launcher and the updater fetch the same files; their URL spellings
// are one protocol, so this evaluates the launcher's own assignments.
func TestTarballURLsAreTheLaunchers(t *testing.T) {
	raw, err := os.ReadFile("../launcher/launch")
	if err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{"registry": "https://r.example", "package": "@claudinite/cli-rc", "name": "cli-rc", "version": "1.60930.1", "platform": "linux-arm64"}
	eval := func(name string) string {
		var m []string
		for _, a := range regexp.MustCompile(`(?m)^\s*`+name+`=(\S+)$`).FindAllStringSubmatch(string(raw), -1) {
			if strings.HasPrefix(a[1], "$registry/") {
				m = a
			}
		}
		if m == nil {
			t.Fatalf("launcher assigns no %s from $registry", name)
		}
		return regexp.MustCompile(`\$\{?([a-z_]+)\}?`).ReplaceAllStringFunc(m[1], func(v string) string {
			return vars[strings.Trim(v, "${}")]
		})
	}
	if got, want := TarballURL("https://r.example", "@claudinite/cli-rc", "1.60930.1"), eval("manifest_url"); got != want {
		t.Errorf("TarballURL %s, launcher %s", got, want)
	}
	if got, want := PlatformTarballURL("https://r.example", "@claudinite/cli-rc", "linux-arm64", "1.60930.1"), eval("binary_url"); got != want {
		t.Errorf("PlatformTarballURL %s, launcher %s", got, want)
	}
}

func TestParseDeprecation(t *testing.T) {
	for msg, want := range map[string]State{
		"":                      {},
		"held: canary red":      {Kind: Held, Reason: "canary red"},
		"revoked: bad build":    {Kind: Revoked, Reason: "bad build"},
		"use 1.60931.1 instead": {Kind: Deprecated, Reason: "use 1.60931.1 instead"},
		"Held: not a hold":      {Kind: Deprecated, Reason: "Held: not a hold"},
		"revoked:":              {Kind: Revoked, Reason: ""},
	} {
		if got := ParseDeprecation(msg); got != want {
			t.Errorf("%q: %+v, want %+v", msg, got, want)
		}
	}
}

func TestGetPackumentAndDownload(t *testing.T) {
	body := []byte("tarball bytes")
	sum := sha512.Sum512(body)
	integrity := "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/@claudinite%2fcli-rc":
			_, _ = w.Write([]byte(`{"name":"@claudinite/cli-rc","dist-tags":{"latest":"1.2.0"},"versions":{"1.2.0":{"version":"1.2.0","deprecated":"held: x","dist":{"tarball":"` + srv.URL + `/t.tgz","integrity":"` + integrity + `"}}}}`))
		case "/t.tgz":
			_, _ = w.Write(body)
		case "/big.tgz":
			_, _ = w.Write(make([]byte, 100))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := Client{Registry: srv.URL, HTTP: srv.Client(), MaxBytes: 64}
	p, err := c.Packument("@claudinite/cli-rc")
	if err != nil {
		t.Fatal(err)
	}
	v := p.Versions["1.2.0"]
	if v.Deprecated != "held: x" || v.Dist.Integrity != integrity {
		t.Fatalf("%+v", v)
	}
	got, err := c.Download(v.Dist.Tarball)
	if err != nil || string(got) != string(body) {
		t.Fatalf("%q %v", got, err)
	}
	if err := CheckIntegrity(got, integrity); err != nil {
		t.Error(err)
	}
	if err := CheckIntegrity(append(got, 'x'), integrity); err == nil {
		t.Error("a changed tarball passed its integrity")
	}
	if _, err := c.Download(srv.URL + "/big.tgz"); err == nil || !strings.Contains(err.Error(), "cap") {
		t.Errorf("over the cap: %v", err)
	}
	if _, err := c.Download("http://" + strings.TrimPrefix(srv.URL, "https://") + "/t.tgz"); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Errorf("plain http: %v", err)
	}
	if _, err := c.Packument("@claudinite/missing"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("missing package: %v", err)
	}
}
