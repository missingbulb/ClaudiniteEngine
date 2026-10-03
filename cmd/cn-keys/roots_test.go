package main

import (
	"bytes"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
)

type rootsRun struct {
	summary, log bytes.Buffer
	masked       []string
}

func runRoots(rootSeed, sealed, pass string) (*rootsRun, error) {
	r := &rootsRun{}
	return r, roots(rootSeed, sealed, pass, &r.summary, &r.log, func(v string) { r.masked = append(r.masked, v) })
}

// noSecretPrinted fails when a secret value reached the summary or the log,
// or was never masked first.
func noSecretPrinted(t *testing.T, r *rootsRun, secrets ...string) {
	t.Helper()
	out := r.summary.String() + r.log.String()
	for _, s := range secrets {
		if strings.Contains(out, s) {
			t.Errorf("a secret value reached the summary or the log: %.6s…", s)
		}
		masked := false
		for _, m := range r.masked {
			masked = masked || m == s
		}
		if !masked {
			t.Errorf("never masked %.6s…", s)
		}
	}
}

func newRoot(t *testing.T) (ed25519.PublicKey, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return pub, strings.TrimSpace(sign.FormatPrivateKey(priv))
}

func hasKey(summary, path string, pub ed25519.PublicKey) bool {
	return strings.Contains(summary, "`"+path+"`, key id `"+sign.KeyID(pub)+"`") &&
		strings.Contains(summary, sign.FormatPublicKey(pub))
}

func TestRootsRootOnly(t *testing.T) {
	rootPub, rootSeed := newRoot(t)
	r, err := runRoots(rootSeed+"\n", "", "")
	if err != nil {
		t.Fatal(err)
	}
	s := r.summary.String()
	if !hasKey(s, "license/roots/root.pub", rootPub) {
		t.Errorf("summary lacks the root public key, id or path:\n%s", s)
	}
	if !strings.Contains(s, "`license/roots/standby.pub`: missing") || !strings.Contains(s, standbySealedVar) || !strings.Contains(s, passphraseVar) {
		t.Errorf("summary does not say the standby is missing and what to add:\n%s", s)
	}
	noSecretPrinted(t, r, rootSeed)

	half, err := runRoots(rootSeed, "", passphrase)
	if err != nil {
		t.Fatal(err)
	}
	if r := half; strings.Contains(r.summary.String(), "`"+passphraseVar+"`") ||
		!strings.Contains(r.summary.String(), "`"+standbySealedVar+"`") {
		t.Errorf("a passphrase without a sealed block names the wrong secret missing:\n%s", r.summary.String())
	}

	if _, err := runRoots("not a key", "", ""); err == nil || strings.Contains(err.Error(), "not a key") {
		t.Fatalf("an unparsable ROOT_KEY: %v", err)
	}
}

func TestRootsRootAndStandby(t *testing.T) {
	rootPub, rootSeed := newRoot(t)
	standbyPub, standby, _ := ed25519.GenerateKey(nil)
	sealed, err := sealStandby(standby, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	r, err := runRoots(rootSeed, sealed, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	s := r.summary.String()
	if !hasKey(s, "license/roots/root.pub", rootPub) || !hasKey(s, "license/roots/standby.pub", standbyPub) {
		t.Errorf("summary lacks a public key, id or path:\n%s", s)
	}
	if strings.Contains(s, armorBegin) {
		t.Error("summary carries the sealed block")
	}
	secrets := []string{rootSeed, passphrase, strings.TrimSpace(sign.FormatPrivateKey(standby))}
	for _, line := range strings.Split(strings.TrimSpace(sealed), "\n")[1:] {
		if line != armorEnd {
			secrets = append(secrets, line)
		}
	}
	noSecretPrinted(t, r, secrets...)
}

func TestRootsWrongPassphraseFailsQuietly(t *testing.T) {
	rootPub, rootSeed := newRoot(t)
	_, standby, _ := ed25519.GenerateKey(nil)
	sealed, err := sealStandby(standby, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	wrong := passphrase + "x"
	r, err := runRoots(rootSeed, sealed, wrong)
	if err == nil || !strings.Contains(err.Error(), "wrong passphrase") {
		t.Fatalf("a wrong passphrase: %v", err)
	}
	s := r.summary.String()
	if !hasKey(s, "license/roots/root.pub", rootPub) || !strings.Contains(s, "not recovered") {
		t.Errorf("summary does not keep the root and report the standby:\n%s", s)
	}
	out := s + r.log.String() + err.Error()
	for _, secret := range []string{rootSeed, wrong, strings.TrimSpace(sign.FormatPrivateKey(standby)), armorBegin} {
		if strings.Contains(out, secret) {
			t.Errorf("a failed run printed %.6s…", secret)
		}
	}
	noSecretPrinted(t, r, rootSeed, wrong)
}

func TestRootsCommandReadsTheEnvironment(t *testing.T) {
	rootPub, rootSeed := newRoot(t)
	summary := filepath.Join(t.TempDir(), "summary.md")
	t.Setenv(rootSecret, rootSeed)
	t.Setenv(standbySealedVar, "")
	t.Setenv(passphraseVar, "")
	if _, e, code := keys(t, "roots", "--summary", summary); code != 0 {
		t.Fatal(e)
	}
	raw, _ := os.ReadFile(summary)
	if !hasKey(string(raw), "license/roots/root.pub", rootPub) {
		t.Errorf("summary file lacks the root:\n%s", raw)
	}
	for _, args := range [][]string{{"roots"}, {"roots", "--summary", summary, "--use", "packs"}} {
		if _, e, code := keys(t, args...); code != 2 || !strings.Contains(e, "usage:") {
			t.Errorf("%v: exit %d: %q", args, code, e)
		}
	}
}
