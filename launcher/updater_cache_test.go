package launcher

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/license"
	"github.com/missingbulb/ClaudiniteEngine/lifecycle/update"
	"github.com/missingbulb/ClaudiniteEngine/release/releasefiles"
	"github.com/missingbulb/ClaudiniteEngine/shared/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
)

// signRelease adds manifest.sig.json, signed with the development release
// key, to the release's channel tarball.
func signRelease(t *testing.T, r release, pkg string) {
	t.Helper()
	key, err := sign.ParsePrivateKey(readFile(t, filepath.Join(repoRoot, "keys", "dev", "release.key")))
	if err != nil {
		t.Fatal(err)
	}
	var cert sign.Certificate
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(repoRoot, "keys", "dev", "release.cert.json"))), &cert); err != nil {
		t.Fatal(err)
	}
	sig, _ := json.Marshal(sign.SignManifest(key, cert, r.manifest))
	name := strings.TrimPrefix(pkg, "@claudinite/")
	err = releasefiles.WriteTarball(filepath.Join(r.dist, "tarballs", name+"-"+testVersion+".tgz"), []releasefiles.TarFile{
		{Name: "package.json", Mode: 0o644, Data: []byte("{}\n")},
		{Name: "manifest.json", Mode: 0o644, Data: r.manifest},
		{Name: "manifest.sig.json", Mode: 0o644, Data: sig},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// The updater places the new version's binary exactly where the launcher
// looks, so the first session on the merged pin downloads nothing.
func TestUpdaterPlacementNeedsNoDownload(t *testing.T) {
	const pkg = "@claudinite/cli-rc"
	r := makeRelease(t, releaseOpts{pkg: pkg})
	signRelease(t, r, pkg)
	s := startStub(t, r.dist)
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(readFile(t, s.ca))) {
		t.Fatal("stub CA")
	}
	reg := npmreg.Client{Registry: s.url, HTTP: &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig(pool)}}, MaxBytes: npmreg.DefaultMaxBytes}
	p, err := reg.Packument(pkg)
	if err != nil {
		t.Fatal(err)
	}
	roots, err := license.Roots()
	if err != nil {
		t.Fatal(err)
	}
	m := newMember(t, s)
	got, err := update.Fetch(update.FetchInput{Registry: reg, Package: pkg, Version: testVersion, Packument: p, Roots: roots,
		CacheRoot: filepath.Join(m.cache, "claudinite"), Platform: host, Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if got.Integrity != r.pin {
		t.Fatalf("updater pin %s, release pin %s", got.Integrity, r.pin)
	}
	m.settings(t, "settings.yaml", "engine:\n  package: \""+pkg+"\"\n  version: \""+testVersion+"\"\n  manifest: \""+got.Integrity+"\"\n")
	s.reset(t)
	out, errOut, code := m.run(t, `{"hook_event_name":"SessionStart"}`, "hook", "session-start")
	if code != 0 || !strings.Contains(out, "# Claudinite engine "+testVersion) {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if reqs := s.requests(t); len(reqs) != 0 {
		t.Errorf("the launcher fetched after the updater placed the binary: %v", reqs)
	}
}

func tlsConfig(pool *x509.CertPool) *tls.Config { return &tls.Config{RootCAs: pool} }
