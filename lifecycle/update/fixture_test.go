package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

func seed(b byte) ed25519.PrivateKey {
	s := make([]byte, ed25519.SeedSize)
	for i := range s {
		s[i] = b
	}
	return ed25519.NewKeyFromSeed(s)
}

var (
	testRoot  = seed(0x51)
	otherRoot = seed(0x52)
	relKey    = seed(0x53)
	t0        = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
)

func rootsOf(keys ...ed25519.PrivateKey) []ed25519.PublicKey {
	var out []ed25519.PublicKey
	for _, k := range keys {
		out = append(out, k.Public().(ed25519.PublicKey))
	}
	return out
}

// tgz writes an npm-shaped tarball, every entry under package/, as
// release/build.sh's npm pack does.
func tgz(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: "package/" + name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write(data)
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func integrity(b []byte) string {
	s := sha512.Sum512(b)
	return "sha512-" + base64.StdEncoding.EncodeToString(s[:])
}

type relOpts struct {
	issuer      ed25519.PrivateKey // default testRoot
	use         sign.Use           // default manifest
	notBefore   time.Time          // default t0 - 1 day
	flipBinary  bool               // serve a binary that differs from its manifest entry
	flipChannel bool               // serve a channel tarball that differs from dist.integrity
	deprecated  string
	binary      []byte // the host binary's bytes
	launcher    []byte // package/launch; default a stand-in naming ver
	tag         string // the dist-tag that moves to ver when it is newer; default latest
}

// registry is a TLS stand-in for npm serving packuments and tarballs.
type registry struct {
	srv   *httptest.Server
	mu    sync.Mutex
	files map[string][]byte
	pkgs  map[string]*npmreg.Packument
	log   []string
	// notServed answers 404 for a path's next n requests, as npm does for
	// a fresh version's tarballs in the minutes after its publish.
	notServed map[string]int
}

func newRegistry(t *testing.T) *registry {
	r := &registry{files: map[string][]byte{}, pkgs: map[string]*npmreg.Packument{}, notServed: map[string]int{}}
	r.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.log = append(r.log, req.URL.EscapedPath())
		if r.notServed[req.URL.EscapedPath()] > 0 {
			r.notServed[req.URL.EscapedPath()]--
			http.NotFound(w, req)
			return
		}
		if b, ok := r.files[req.URL.EscapedPath()]; ok {
			_, _ = w.Write(b)
			return
		}
		name := strings.Replace(strings.TrimPrefix(req.URL.EscapedPath(), "/"), "%2f", "/", 1)
		if p, ok := r.pkgs[name]; ok {
			_ = json.NewEncoder(w).Encode(p)
			return
		}
		http.NotFound(w, req)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *registry) client() npmreg.Client {
	return npmreg.Client{Registry: r.srv.URL, HTTP: r.srv.Client(), MaxBytes: npmreg.DefaultMaxBytes}
}

func (r *registry) requests() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.log...)
}

// publish adds pkg@ver: a signed manifest naming one binary per platform,
// the host's being o.binary, and the tarballs and packument entry.
func (r *registry) publish(t *testing.T, pkg, ver string, o relOpts) {
	t.Helper()
	if o.issuer == nil {
		o.issuer = testRoot
	}
	if o.use == "" {
		o.use = sign.UseManifest
	}
	if o.notBefore.IsZero() {
		o.notBefore = t0.AddDate(0, 0, -1)
	}
	if o.binary == nil {
		o.binary = []byte("#!/bin/sh\necho version " + ver + "\n")
	}
	name := strings.TrimPrefix(pkg, "@claudinite/")
	var lines []string
	for _, p := range version.Platforms {
		data := []byte("binary for " + p)
		if p == version.Platform() {
			data = o.binary
		}
		file := "cn"
		if strings.HasPrefix(p, "windows") {
			file = "cn.exe"
		}
		sum := sha256.Sum256(data)
		lines = append(lines, fmt.Sprintf("    %q: {\"file\": %q, \"sha256\": %q, \"size\": %d}", p, file, hex.EncodeToString(sum[:]), len(data)))
		served := data
		if o.flipBinary && p == version.Platform() {
			served = append(append([]byte{}, data...), 'X')
		}
		r.files[fmt.Sprintf("/%s-%s/-/%s-%s-%s.tgz", pkg, p, name, p, ver)] = tgz(t, map[string][]byte{"package.json": []byte("{}"), "bin/" + file: served})
	}
	manifest := []byte(fmt.Sprintf("{\n  \"v\": 1,\n  \"version\": %q,\n  \"builtAt\": \"2026-10-01T00:00:00Z\",\n  \"commit\": \"abc1234\",\n  \"goVersion\": \"go1.24\",\n  \"updaterDigest\": \"d\",\n  \"binaries\": {\n%s\n  },\n  \"testedPacks\": {}\n}\n", ver, strings.Join(lines, ",\n")))
	cert, err := sign.Issue(o.issuer, relKey.Public().(ed25519.PublicKey), o.use, o.notBefore, o.notBefore.AddDate(0, 0, 30))
	if err != nil {
		t.Fatal(err)
	}
	sig, _ := json.Marshal(sign.SignManifest(relKey, cert, manifest))
	if o.launcher == nil {
		o.launcher = []byte("#!/bin/sh\n# launcher of " + ver + "\n")
	}
	channel := tgz(t, map[string][]byte{"package.json": []byte("{}"), "manifest.json": manifest, "manifest.sig.json": sig, "launch": o.launcher})
	path := fmt.Sprintf("/%s/-/%s-%s.tgz", pkg, name, ver)
	served := channel
	if o.flipChannel {
		served = append(append([]byte{}, channel...), 0)
	}
	r.files[path] = served
	p := r.pkgs[pkg]
	if p == nil {
		p = &npmreg.Packument{Name: pkg, Versions: map[string]npmreg.Version{}}
		r.pkgs[pkg] = p
	}
	var v npmreg.Version
	v.Version, v.Deprecated = ver, o.deprecated
	v.Dist.Tarball, v.Dist.Integrity = r.srv.URL+path, integrity(channel)
	p.Versions[ver] = v
	if o.tag == "" {
		o.tag = "latest"
	}
	if p.DistTags == nil {
		p.DistTags = map[string]string{}
	}
	if c, err := version.Compare(ver, p.DistTags[o.tag]); p.DistTags[o.tag] == "" || (err == nil && c > 0) {
		p.DistTags[o.tag] = ver
	}
}
