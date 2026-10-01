package packs

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
)

func seed(b byte) ed25519.PrivateKey {
	s := make([]byte, ed25519.SeedSize)
	for i := range s {
		s[i] = b
	}
	return ed25519.NewKeyFromSeed(s)
}

var (
	testRoot = seed(0x61)
	packsKey = seed(0x62)
	t0       = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
)

func roots() []ed25519.PublicKey { return []ed25519.PublicKey{testRoot.Public().(ed25519.PublicKey)} }

func indexJSON(serial int, versions ...string) []byte {
	var entries []string
	for _, v := range versions {
		entries = append(entries, fmt.Sprintf(`{"version": %q, "sha256": %q, "size": 1, "minEngineVersion": "1.1.0", "requires": [], "channel": "canary", "revoked": false, "publishedAt": "2026-10-01T00:00:00Z", "sourceCommit": "x"}`, v, strings.Repeat("a", 64)))
	}
	return []byte(fmt.Sprintf("{\n  \"v\": 1,\n  \"pack\": \"hello\",\n  \"serial\": %d,\n  \"versions\": [%s]\n}\n", serial, strings.Join(entries, ", ")))
}

func signWith(t *testing.T, issuer ed25519.PrivateKey, use sign.Use, nb time.Time, index []byte) []byte {
	t.Helper()
	cert, err := sign.Issue(issuer, packsKey.Public().(ed25519.PublicKey), use, nb, nb.AddDate(0, 0, 30))
	if err != nil {
		t.Fatal(err)
	}
	var s sign.Detached
	if use == sign.UseManifest {
		s = sign.SignManifest(packsKey, cert, index)
	} else {
		s = sign.SignPackIndex(packsKey, cert, index)
	}
	raw, _ := json.Marshal(s)
	return raw
}

func signed(t *testing.T, index []byte) []byte {
	return signWith(t, testRoot, sign.UsePacks, t0.AddDate(0, 0, -1), index)
}

type pair struct{ index, sig []byte }

// fakeSource answers from a queue of pairs per call, repeating the last.
type fakeSource struct {
	name     string
	pairs    []pair
	err      error
	calls    int
	archives map[string][]byte
}

func (f *fakeSource) Name() string { return f.name }

func (f *fakeSource) Index(id string) ([]byte, []byte, error) {
	f.calls++
	if f.err != nil {
		return nil, nil, f.err
	}
	p := f.pairs[len(f.pairs)-1]
	if f.calls <= len(f.pairs) {
		p = f.pairs[f.calls-1]
	}
	return p.index, p.sig, nil
}

func (f *fakeSource) Archive(id, version string) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	if a, ok := f.archives[id+"@"+version]; ok {
		return a, nil
	}
	return nil, fmt.Errorf("%s: no %s %s", f.name, id, version)
}

func newReader(log *bytes.Buffer, srcs ...Source) *Reader {
	return &Reader{Sources: srcs, Roots: roots(), Now: func() time.Time { return t0 }, Log: log}
}

func TestVerifiedIndexFromTheFirstSourceThatAnswers(t *testing.T) {
	ix := indexJSON(3, "1.0")
	cdn := &fakeSource{name: "cdn", err: errors.New("dial tcp: refused")}
	branch := &fakeSource{name: "branch", pairs: []pair{{ix, signed(t, ix)}}}
	var log bytes.Buffer
	got, err := newReader(&log, cdn, branch).VerifiedIndex("hello")
	if err != nil {
		t.Fatal(err)
	}
	if got.Index.Serial != 3 || got.From != "branch" || got.KeyID != sign.KeyID(packsKey.Public().(ed25519.PublicKey)) {
		t.Errorf("%+v", got)
	}
	if !strings.Contains(log.String(), "hello: index serial 3 from branch") || !strings.Contains(log.String(), "cdn: dial tcp: refused") {
		t.Errorf("log %q", log.String())
	}
}

func TestVerifiedIndexReadsBothSourcesAndRefusesARegression(t *testing.T) {
	newer, older := indexJSON(4, "1.0", "1.1"), indexJSON(1, "1.0", "1.1")
	cdn := &fakeSource{name: "cdn", pairs: []pair{{newer, signed(t, newer)}}}
	branch := &fakeSource{name: "branch", pairs: []pair{{older, signed(t, older)}}}
	_, err := newReader(&bytes.Buffer{}, cdn, branch).VerifiedIndex("hello")
	if err == nil || !strings.Contains(err.Error(), "serial 1") || !strings.Contains(err.Error(), "serial 4") {
		t.Errorf("%v", err)
	}
	// A branch ahead of the CDN (the upload trails the branch) is the
	// newer index, and is the one used.
	cdn.pairs, branch.pairs = []pair{{older, signed(t, older)}}, []pair{{newer, signed(t, newer)}}
	cdn.calls, branch.calls = 0, 0
	got, err := newReader(&bytes.Buffer{}, cdn, branch).VerifiedIndex("hello")
	if err != nil || got.Index.Serial != 4 || got.From != "branch" {
		t.Errorf("%+v %v", got, err)
	}
}

func TestVerifiedIndexRefusesALowerSerialLaterInTheRun(t *testing.T) {
	a, b := indexJSON(5, "1.0"), indexJSON(2, "1.0")
	src := &fakeSource{name: "cdn", pairs: []pair{{a, signed(t, a)}}}
	r := newReader(&bytes.Buffer{}, src)
	if _, err := r.VerifiedIndex("hello"); err != nil {
		t.Fatal(err)
	}
	src.pairs, src.calls = []pair{{b, signed(t, b)}}, 0
	if _, err := r.VerifiedIndex("hello"); err == nil || !strings.Contains(err.Error(), "serial 2") {
		t.Errorf("%v", err)
	}
}

func TestVerifiedIndexRetriesAMismatchedPairOnce(t *testing.T) {
	ix1, ix2 := indexJSON(1, "1.0"), indexJSON(2, "1.0", "1.1")
	src := &fakeSource{name: "cdn", pairs: []pair{{ix2, signed(t, ix1)}, {ix2, signed(t, ix2)}}}
	got, err := newReader(&bytes.Buffer{}, src).VerifiedIndex("hello")
	if err != nil || got.Index.Serial != 2 || src.calls != 2 {
		t.Errorf("%+v %v calls %d", got, err, src.calls)
	}
}

func TestVerifiedIndexRefuses(t *testing.T) {
	ix := indexJSON(1, "1.0")
	good := signed(t, ix)
	flip := func(b []byte, at int) []byte { c := append([]byte{}, b...); c[at] ^= 1; return c }
	var sig sign.Detached
	_ = json.Unmarshal(good, &sig)
	certFlipped := sig
	certFlipped.Certificate.Signature = string(flip([]byte(sig.Certificate.Signature), 5))
	certRaw, _ := json.Marshal(certFlipped)
	cases := map[string]struct {
		p    pair
		want string
	}{
		"flipped index byte":     {pair{flip(ix, 30), good}, "signature"},
		"flipped signature byte": {pair{ix, flip(good, len(good)-5)}, "signature"},
		"flipped certificate":    {pair{ix, certRaw}, "certificate"},
		"manifest certificate":   {pair{ix, signWith(t, testRoot, sign.UseManifest, t0.AddDate(0, 0, -1), ix)}, "use"},
		"expired certificate":    {pair{ix, signWith(t, testRoot, sign.UsePacks, t0.AddDate(0, 0, -60), ix)}, "expired"},
		"untrusted root":         {pair{ix, signWith(t, seed(0x99), sign.UsePacks, t0.AddDate(0, 0, -1), ix)}, "trusted root"},
		"not json":               {pair{ix, []byte("{")}, "index.sig.json"},
	}
	for name, c := range cases {
		src := &fakeSource{name: "cdn", pairs: []pair{c.p}}
		_, err := newReader(&bytes.Buffer{}, src).VerifiedIndex("hello")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestVerifiedIndexNeitherSourceAnswers(t *testing.T) {
	cdn := &fakeSource{name: "cdn", err: errors.New("cdn down")}
	branch := &fakeSource{name: "branch", err: errors.New("clone failed")}
	_, err := newReader(&bytes.Buffer{}, cdn, branch).VerifiedIndex("hello")
	if err == nil || !strings.Contains(err.Error(), "cdn down") || !strings.Contains(err.Error(), "clone failed") {
		t.Errorf("%v", err)
	}
}

func TestReaderArchiveFallsBackAndVerifies(t *testing.T) {
	data := helloArchive(t, "1.0")
	e := entryFor(data)
	cdn := &fakeSource{name: "cdn", archives: map[string][]byte{}}
	branch := &fakeSource{name: "branch", archives: map[string][]byte{"hello@1.0": data}}
	var log bytes.Buffer
	got, err := newReader(&log, cdn, branch).Archive("hello", e)
	if err != nil || !bytes.Equal(got, data) || !strings.Contains(log.String(), "hello 1.0: archive from branch") {
		t.Errorf("%v %q", err, log.String())
	}
	cdn.archives["hello@1.0"] = append(append([]byte{}, data...), 1)
	if _, err := newReader(&log, cdn).Archive("hello", e); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Errorf("bad archive: %v", err)
	}
}

func TestCDNSource(t *testing.T) {
	ix := indexJSON(1, "1.0")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/packs/hello/index.json":
			_, _ = w.Write(ix)
		case "/packs/hello/index.sig.json":
			_, _ = w.Write([]byte("sig"))
		case "/packs/hello/1.0.tar.gz":
			_, _ = w.Write([]byte("archive"))
		case "/packs/big/index.json", "/packs/big/index.sig.json":
			_, _ = w.Write(bytes.Repeat([]byte("x"), 100))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := CDN{Base: srv.URL, HTTP: srv.Client()}
	c.MaxBytes = 1 << 20
	var gotIx, gotSig []byte
	var err error
	if gotIx, gotSig, err = c.Index("hello"); err != nil || !bytes.Equal(gotIx, ix) || string(gotSig) != "sig" {
		t.Fatalf("%q %q %v", gotIx, gotSig, err)
	}
	if a, err := c.Archive("hello", "1.0"); err != nil || string(a) != "archive" {
		t.Errorf("%q %v", a, err)
	}
	if _, _, err := c.Index("absent"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("absent: %v", err)
	}
	c.MaxBytes = 50
	if _, _, err := c.Index("big"); err == nil || !strings.Contains(err.Error(), "cap") {
		t.Errorf("over the cap: %v", err)
	}
	if _, _, err := (CDN{Base: "http://example.com", HTTP: http.DefaultClient, MaxBytes: 10}).Index("hello"); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Errorf("plain http: %v", err)
	}
}

func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@x"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestBranchSource(t *testing.T) {
	root := t.TempDir()
	bare, work := filepath.Join(root, "packs.git"), filepath.Join(root, "work")
	gitT(t, root, "init", "-q", "--bare", "-b", "main", bare)
	gitT(t, root, "init", "-q", "-b", "vendored", work)
	_ = os.MkdirAll(filepath.Join(work, "hello"), 0o755)
	_ = os.WriteFile(filepath.Join(work, "hello", "index.json"), []byte("IX"), 0o644)
	_ = os.WriteFile(filepath.Join(work, "hello", "index.sig.json"), []byte("SIG"), 0o644)
	_ = os.WriteFile(filepath.Join(work, "hello", "1.0.tar.gz"), []byte("TGZ"), 0o644)
	gitT(t, work, "add", "-A")
	gitT(t, work, "commit", "-q", "-m", "Release hello 1.0")
	gitT(t, work, "push", "-q", bare, "vendored")
	b := &Branch{Repo: bare}
	defer b.Close()
	ix, sig, err := b.Index("hello")
	if err != nil || string(ix) != "IX" || string(sig) != "SIG" {
		t.Fatalf("%q %q %v", ix, sig, err)
	}
	if a, err := b.Archive("hello", "1.0"); err != nil || string(a) != "TGZ" {
		t.Errorf("%q %v", a, err)
	}
	if _, err := b.Archive("hello", "9.9"); err == nil {
		t.Error("read an archive the branch does not hold")
	}
	dir := b.dir
	b.Close()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("clone left behind: %v", err)
	}
	missing := &Branch{Repo: filepath.Join(root, "absent.git")}
	if _, _, err := missing.Index("hello"); err == nil {
		t.Error("an absent repo answered")
	}
}

func TestVerifiedIndexRefusesASerialBelowTheKeysFloor(t *testing.T) {
	ix := indexJSON(3, "1.0")
	src := &fakeSource{name: "cdn", pairs: []pair{{ix, signed(t, ix)}}}
	r := newReader(&bytes.Buffer{}, src)
	r.SerialFloor = 7
	if _, err := r.VerifiedIndex("hello"); err == nil || !strings.Contains(err.Error(), "serial 3") || !strings.Contains(err.Error(), "license key's pack index serial 7") {
		t.Errorf("%v", err)
	}
	src.calls = 0
	r = newReader(&bytes.Buffer{}, src)
	r.SerialFloor = 3
	if _, err := r.VerifiedIndex("hello"); err != nil {
		t.Errorf("at the floor: %v", err)
	}
}

func TestVerifiedIndexRefusesAKeyTheLicenseKeyDoesNotList(t *testing.T) {
	ix := indexJSON(3, "1.0")
	src := &fakeSource{name: "cdn", pairs: []pair{{ix, signed(t, ix)}}}
	id := sign.KeyID(packsKey.Public().(ed25519.PublicKey))
	r := newReader(&bytes.Buffer{}, src)
	r.AcceptedKeys = []string{"0123456789abcdef"}
	if _, err := r.VerifiedIndex("hello"); err == nil || !strings.Contains(err.Error(), id) {
		t.Errorf("%v", err)
	}
	src.calls = 0
	r = newReader(&bytes.Buffer{}, src)
	r.AcceptedKeys = []string{id}
	if _, err := r.VerifiedIndex("hello"); err != nil {
		t.Errorf("listed key: %v", err)
	}
}
