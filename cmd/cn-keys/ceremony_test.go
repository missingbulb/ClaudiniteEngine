package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
)

const passphrase = "correct horse battery staple ceremony"

type fakeStore struct {
	authErr   error
	missing   map[string]bool // repo or repo/env that does not exist
	failSetAt int             // 1-based SetSecret call that fails; 0 never
	calls     int
	created   []string
	secrets   map[string][]byte // repo|env|name
}

func place(repo, env string) string {
	if env == "" {
		return repo
	}
	return repo + "/" + env
}

func (f *fakeStore) CheckAuth() error { return f.authErr }

func (f *fakeStore) EnvironmentExists(repo, env string) (bool, error) {
	return !f.missing[place(repo, env)], nil
}

func (f *fakeStore) CreateEnvironment(repo, env string) error {
	f.created = append(f.created, place(repo, env))
	delete(f.missing, place(repo, env))
	return nil
}

func (f *fakeStore) SecretNames(repo, env string) ([]string, error) {
	var names []string
	for k := range f.secrets {
		if parts := strings.Split(k, "|"); parts[0] == repo && parts[1] == env {
			names = append(names, parts[2])
		}
	}
	return names, nil
}

func (f *fakeStore) SetSecret(repo, env, name string, value []byte) error {
	f.calls++
	if f.calls == f.failSetAt {
		return errors.New("HTTP 403")
	}
	if f.missing[place(repo, env)] {
		return errors.New("HTTP 404: no such environment")
	}
	if f.secrets == nil {
		f.secrets = map[string][]byte{}
	}
	f.secrets[repo+"|"+env+"|"+name] = append([]byte(nil), value...)
	return nil
}

var at = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

type ceremonyRun struct {
	summary, log bytes.Buffer
	masked       []string
}

func (r *ceremonyRun) mask(v string) { r.masked = append(r.masked, v) }

func runCeremony(store *fakeStore, pass string) (*ceremonyRun, error) {
	r := &ceremonyRun{}
	return r, ceremony(store, pass, at, &r.summary, &r.log, r.mask)
}

func TestCeremonyPrerequisiteFailureWritesNothing(t *testing.T) {
	for name, c := range map[string]struct {
		store *fakeStore
		pass  string
	}{
		"gh not authenticated": {&fakeStore{authErr: errors.New("no token")}, passphrase},
		"missing environment":  {&fakeStore{missing: map[string]bool{"missingbulb/ClaudinitePacks/release": true}}, passphrase},
		"unreachable repo":     {&fakeStore{missing: map[string]bool{"missingbulb/ClaudiniteLicenses": true}}, passphrase},
		"short passphrase":     {&fakeStore{}, "too short"},
	} {
		t.Run(name, func(t *testing.T) {
			r, err := runCeremony(c.store, c.pass)
			if err == nil {
				t.Fatal("ceremony went ahead")
			}
			if len(c.store.secrets) != 0 || len(c.store.created) != 0 || r.summary.Len() != 0 {
				t.Fatalf("set %d secrets, created %v, summary %q", len(c.store.secrets), c.store.created, r.summary.String())
			}
		})
	}
}

func TestCeremonyStoresEveryKey(t *testing.T) {
	store := &fakeStore{missing: map[string]bool{"missingbulb/ClaudiniteEngine/root": true}}
	r, err := runCeremony(store, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	summary := r.summary.String()

	want := []string{
		"missingbulb/ClaudiniteEngine|release|CN_RELEASE_CERT",
		"missingbulb/ClaudiniteEngine|release|CN_RELEASE_KEY",
		"missingbulb/ClaudiniteEngine|root|ROOT_KEY",
		"missingbulb/ClaudiniteLicenses||ISSUING_KEY_CERT",
		"missingbulb/ClaudiniteLicenses||ISSUING_KEY_PRIVATE",
		"missingbulb/ClaudinitePacks|release|CN_PACKS_CERT",
		"missingbulb/ClaudinitePacks|release|CN_PACKS_KEY",
	}
	var got []string
	for k := range store.secrets {
		got = append(got, k)
	}
	sort.Strings(got)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("secrets set:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if strings.Join(store.created, " ") != "missingbulb/ClaudiniteEngine/root" {
		t.Fatalf("created %v", store.created)
	}

	rootSeed := string(store.secrets["missingbulb/ClaudiniteEngine|root|ROOT_KEY"])
	rootKey, err := sign.ParsePrivateKey(rootSeed)
	if err != nil {
		t.Fatal(err)
	}
	root := rootKey.Public().(ed25519.PublicKey)
	if !strings.Contains(summary, sign.FormatPublicKey(root)) || !strings.Contains(summary, sign.KeyID(root)) {
		t.Error("summary lacks the root public key or its id")
	}

	standbyKey, err := openStandby(summary, passphrase)
	if err != nil {
		t.Fatalf("the summary's sealed standby does not open: %v", err)
	}
	standby := standbyKey.Public().(ed25519.PublicKey)
	if !strings.Contains(summary, sign.FormatPublicKey(standby)) || !strings.Contains(summary, sign.KeyID(standby)) {
		t.Error("summary lacks the standby public key or its id")
	}
	standbySeed := strings.TrimSpace(sign.FormatPrivateKey(standbyKey))

	secrets := []string{rootSeed, standbySeed, passphrase}
	for _, k := range workingKeys {
		prefix := k.Repo + "|" + k.Env + "|"
		certJSON := store.secrets[prefix+k.CertSecret]
		var cert sign.Certificate
		if err := json.Unmarshal(certJSON, &cert); err != nil {
			t.Fatal(err)
		}
		body, err := cert.Verify([]ed25519.PublicKey{root}, k.Use, at.Add(time.Minute))
		if err != nil {
			t.Fatalf("%s: shared/sign refuses the certificate against the new root: %v", k.Use, err)
		}
		nb, _ := time.Parse(time.RFC3339, body.NotBefore)
		na, _ := time.Parse(time.RFC3339, body.NotAfter)
		if na.Sub(nb) != time.Duration(k.Days)*24*time.Hour {
			t.Errorf("%s: certified for %v", k.Use, na.Sub(nb))
		}
		seed := string(store.secrets[prefix+k.KeySecret])
		priv, err := sign.ParsePrivateKey(seed)
		if err != nil {
			t.Fatalf("%s: %v", k.KeySecret, err)
		}
		subject, _ := body.Subject()
		if !priv.Public().(ed25519.PublicKey).Equal(subject) {
			t.Errorf("%s does not hold the certified key", k.KeySecret)
		}
		if !strings.Contains(summary, string(certJSON)) || !strings.Contains(summary, k.CertSecret) {
			t.Errorf("summary lacks the %s certificate or its secret name", k.Use)
		}
		secrets = append(secrets, seed)
	}
	for _, s := range secrets {
		if strings.Contains(summary, s) || strings.Contains(r.log.String(), s) {
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

func TestCeremonyRunsOnce(t *testing.T) {
	store := &fakeStore{secrets: map[string][]byte{"missingbulb/ClaudiniteEngine|root|ROOT_KEY": []byte("kept")}}
	r, err := runCeremony(store, passphrase)
	if err == nil || !strings.Contains(err.Error(), "rotate") {
		t.Fatalf("ceremony with ROOT_KEY present: %v", err)
	}
	if len(store.secrets) != 1 || string(store.secrets["missingbulb/ClaudiniteEngine|root|ROOT_KEY"]) != "kept" || r.summary.Len() != 0 {
		t.Fatal("a refused ceremony changed secrets or wrote a summary")
	}
}

func TestCeremonyFailingMidwayStoresNoRoot(t *testing.T) {
	store := &fakeStore{failSetAt: 3}
	r, err := runCeremony(store, passphrase)
	if err == nil {
		t.Fatal("ceremony reported success")
	}
	if _, ok := store.secrets["missingbulb/ClaudiniteEngine|root|ROOT_KEY"]; ok || r.summary.Len() != 0 {
		t.Fatal("stored a root or wrote a summary after a failed working key")
	}
	if !strings.Contains(err.Error(), "CN_RELEASE_KEY, CN_RELEASE_CERT") {
		t.Fatalf("error does not name the secrets already set: %v", err)
	}
	if _, err := runCeremony(store, passphrase); err != nil {
		t.Fatalf("re-run after a failure: %v", err)
	}
}

func TestCeremonyFailingToStoreTheRootSaysDiscard(t *testing.T) {
	store := &fakeStore{failSetAt: 2*len(workingKeys) + 1}
	r, err := runCeremony(store, passphrase)
	if err == nil {
		t.Fatal("ceremony reported success")
	}
	if !strings.Contains(r.summary.String(), "Discard everything above") {
		t.Fatal("summary does not void itself")
	}
}

func TestRotateReplacesOnlyTheNamedKeys(t *testing.T) {
	_, root, _ := ed25519.GenerateKey(nil)
	keys, err := selectKeys("packs")
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{}
	var summary, log bytes.Buffer
	var masked []string
	if err := rotate(root, keys, store, at, &summary, &log, func(v string) { masked = append(masked, v) }); err != nil {
		t.Fatal(err)
	}
	if len(store.secrets) != 2 {
		t.Fatalf("set %d secrets", len(store.secrets))
	}
	var cert sign.Certificate
	_ = json.Unmarshal(store.secrets["missingbulb/ClaudinitePacks|release|CN_PACKS_CERT"], &cert)
	if _, err := cert.Verify([]ed25519.PublicKey{root.Public().(ed25519.PublicKey)}, sign.UsePacks, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	seed := string(store.secrets["missingbulb/ClaudinitePacks|release|CN_PACKS_KEY"])
	if strings.Contains(summary.String(), seed) || len(masked) != 1 || masked[0] != seed {
		t.Fatal("rotation printed or failed to mask the private key")
	}
	if _, err := selectKeys("license"); err == nil {
		t.Fatal("selected a use no working key has")
	}
}

func TestStandbySealRoundTrip(t *testing.T) {
	_, k, _ := ed25519.GenerateKey(nil)
	sealed, err := sealStandby(k, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, strings.TrimSpace(sign.FormatPrivateKey(k))) {
		t.Fatal("sealed block holds the key in the clear")
	}
	if _, err := openStandby(sealed, passphrase+"x"); err == nil {
		t.Fatal("opened with the wrong passphrase")
	}
	if _, err := sealStandby(k, "short"); err == nil {
		t.Fatal("sealed under a short passphrase")
	}

	dir := t.TempDir()
	in, out := filepath.Join(dir, "sealed.txt"), filepath.Join(dir, "standby.key")
	if err := os.WriteFile(in, []byte("pasted from the summary:\n"+sealed), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(passphraseVar, passphrase)
	if _, e, code := keys(t, "standby", "decrypt", "--in", in, "--out", out); code != 0 {
		t.Fatal(e)
	}
	raw, _ := os.ReadFile(out)
	got, err := sign.ParsePrivateKey(string(raw))
	if err != nil || !got.Equal(k) || mode(t, out) != 0o600 {
		t.Fatalf("decrypted key differs or has the wrong mode: %v", err)
	}
}

func TestCommandsReadSecretsFromTheEnvironmentOnly(t *testing.T) {
	for _, args := range [][]string{
		{"ceremony"},
		{"ceremony", "--summary", "s", "--use", "packs"},
		{"rotate"},
		{"standby", "decrypt", "--in", "x"},
	} {
		if _, e, code := keys(t, args...); code != 2 || !strings.Contains(e, "usage:") {
			t.Errorf("%v: exit %d: %q", args, code, e)
		}
	}
}

// The workflow drives the tool, so its environments and secret names must
// be the ones the tool reads and writes.
func TestKeyCeremonyWorkflowShape(t *testing.T) {
	raw, err := os.ReadFile("../../.github/workflows/key-ceremony.yml")
	if err != nil {
		t.Fatal(err)
	}
	wf := string(raw)
	if m := regexp.MustCompile(`(?ms)^on:\n(.*?)^\S`).FindStringSubmatch(wf); m == nil || strings.TrimSpace(strings.SplitN(m[1], "\n", 2)[0]) != "workflow_dispatch:" ||
		regexp.MustCompile(`(?m)^  (push|pull_request|schedule|workflow_run|repository_dispatch)`).MatchString(m[1]) {
		t.Error("key-ceremony.yml must run on workflow_dispatch only")
	}
	for _, forbidden := range []string{"upload-artifact", "cache@", "set -x"} {
		if strings.Contains(wf, forbidden) {
			t.Errorf("key-ceremony.yml uses %s; the repository is public", forbidden)
		}
	}
	for _, want := range []string{
		"environment: ceremony",
		"environment: " + rootEnv,
		"secrets." + passphraseVar,
		"secrets." + rootSecret,
		"secrets.CEREMONY_TOKEN",
		"go run ./cmd/cn-keys ceremony --summary \"$GITHUB_STEP_SUMMARY\"",
		"go run ./cmd/cn-keys rotate --summary \"$GITHUB_STEP_SUMMARY\"",
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("key-ceremony.yml lacks %q", want)
		}
	}
}
