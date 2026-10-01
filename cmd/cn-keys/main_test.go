package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
)

func keys(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return out.String(), errb.String(), code
}

func mode(t *testing.T, p string) os.FileMode {
	t.Helper()
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return st.Mode().Perm()
}

func TestRootNew(t *testing.T) {
	dir := t.TempDir()
	out, errOut, code := keys(t, "root", "new", "--out", dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if mode(t, filepath.Join(dir, "root.key")) != 0o600 || mode(t, filepath.Join(dir, "root.pub")) != 0o644 {
		t.Fatal("wrong modes")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "root.pub"))
	p, err := sign.ParsePublicKey(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, sign.KeyID(p)) {
		t.Fatalf("key id not printed: %q", out)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "root.key"))
	if _, _, code := keys(t, "root", "new", "--out", dir); code == 0 {
		t.Fatal("overwrote an existing root")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "root.key"))
	if !bytes.Equal(before, after) {
		t.Fatal("refused overwrite still changed the key")
	}
}

func newKey(t *testing.T, dir, name string) {
	t.Helper()
	if _, e, code := keys(t, "key", "new", "--out", dir, "--name", name); code != 0 {
		t.Fatalf("key new: %s", e)
	}
}

func TestCertify(t *testing.T) {
	dir := t.TempDir()
	if _, e, c := keys(t, "root", "new", "--out", dir); c != 0 {
		t.Fatal(e)
	}
	newKey(t, dir, "release")
	cert := filepath.Join(dir, "cert.json")
	_, e, code := keys(t, "certify", "--root", filepath.Join(dir, "root.key"), "--subject", filepath.Join(dir, "release.pub"),
		"--use", "manifest", "--days", "365", "--out", cert)
	if code != 0 {
		t.Fatalf("certify: %s", e)
	}
	raw, _ := os.ReadFile(cert)
	var c sign.Certificate
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	rootRaw, _ := os.ReadFile(filepath.Join(dir, "root.pub"))
	rootPub, _ := sign.ParsePublicKey(string(rootRaw))
	if _, err := c.Verify([]ed25519.PublicKey{rootPub}, sign.UseManifest, time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("shared/sign refuses the tool's certificate: %v", err)
	}

	refuse := func(name string, args ...string) {
		t.Helper()
		out := filepath.Join(dir, name+".json")
		base := []string{"certify", "--root", filepath.Join(dir, "root.key"), "--subject", filepath.Join(dir, "release.pub"), "--out", out}
		if _, _, code := keys(t, append(base, args...)...); code == 0 {
			t.Errorf("%s: certified", name)
		}
		if _, err := os.Stat(out); err == nil {
			t.Errorf("%s: wrote a certificate", name)
		}
	}
	refuse("bad-use", "--use", "release", "--days", "30")
	refuse("manifest-366", "--use", "manifest", "--days", "366")
	for _, u := range []string{"license", "license-public", "packs"} {
		refuse(u+"-91", "--use", u, "--days", "91")
		if _, e, code := keys(t, "certify", "--root", filepath.Join(dir, "root.key"), "--subject", filepath.Join(dir, "release.pub"),
			"--use", u, "--days", "90", "--out", filepath.Join(dir, u+"-90.json")); code != 0 {
			t.Errorf("%s 90 days refused: %s", u, e)
		}
	}
	if _, _, code := keys(t, "certify", "--root", filepath.Join(dir, "root.key"), "--subject", filepath.Join(dir, "release.pub"),
		"--use", "manifest", "--days", "30", "--out", cert); code == 0 {
		t.Error("overwrote an existing certificate")
	}
}

func TestVerifyAndKeyID(t *testing.T) {
	dir, roots, other := t.TempDir(), t.TempDir(), t.TempDir()
	keys(t, "root", "new", "--out", roots)
	keys(t, "root", "new", "--out", roots, "--name", "standby")
	keys(t, "root", "new", "--out", other)
	newKey(t, dir, "release")
	cert := filepath.Join(dir, "cert.json")
	if _, e, code := keys(t, "certify", "--root", filepath.Join(roots, "standby.key"), "--subject", filepath.Join(dir, "release.pub"),
		"--use", "manifest", "--days", "10", "--out", cert); code != 0 {
		t.Fatal(e)
	}
	raw, _ := os.ReadFile(filepath.Join(roots, "standby.pub"))
	p, _ := sign.ParsePublicKey(string(raw))
	out, e, code := keys(t, "verify", "--roots", roots, cert)
	if code != 0 {
		t.Fatalf("verify: %s", e)
	}
	if !strings.Contains(out, sign.KeyID(p)) || !strings.Contains(out, "standby.pub") {
		t.Fatalf("verify must name the signing root: %q", out)
	}
	if _, _, code := keys(t, "verify", "--roots", other, cert); code != 1 {
		t.Fatalf("verify against foreign roots exit %d, want 1", code)
	}
	out, _, code = keys(t, "keyid", filepath.Join(roots, "standby.pub"))
	if code != 0 || strings.TrimSpace(out) != sign.KeyID(p) {
		t.Fatalf("keyid printed %q", out)
	}
}

func TestUnknownCommandIsUsage(t *testing.T) {
	if _, e, code := keys(t, "frob"); code != 2 || !strings.Contains(e, "usage:") {
		t.Fatalf("exit %d: %q", code, e)
	}
}

// The ceremony runs on an offline machine from a static binary: nothing
// beyond the standard library and shared/ may be linked in.
func TestDependsOnlyOnStdlibAndShared(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", ".").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range strings.Fields(string(out)) {
		if p == "github.com/missingbulb/ClaudiniteEngine/cmd/cn-keys" || strings.HasPrefix(p, "github.com/missingbulb/ClaudiniteEngine/shared/") {
			continue
		}
		t.Errorf("cn-keys depends on %s", p)
	}
}
