package main

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
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/adopt"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/fetch"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/packindex"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/sign"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/update"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/version"
)

// The fake engine release and pack shelf the move reads, as cn's adopt
// tests serve them.

const (
	pkg = "@claudinite/cli"
	ver = "1.61001.1"
)

var (
	root   = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x51}, 32))
	relKey = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x52}, 32))
	now    = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
)

func tgz(t *testing.T, prefix string, files map[string][]byte) []byte {
	t.Helper()
	var names []string
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, n := range names {
		if err := tw.WriteHeader(&tar.Header{Name: prefix + n, Mode: 0o644, Size: int64(len(files[n])), Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write(files[n])
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

// registry serves one signed engine release.
func registry(t *testing.T) npmreg.Client {
	t.Helper()
	files := map[string][]byte{}
	bin := []byte("#!/bin/sh\necho version " + ver + "\necho platform test\n")
	var lines []string
	name := strings.TrimPrefix(pkg, "@claudinite/")
	for _, p := range version.Platforms {
		data := []byte("binary for " + p)
		file := "cn"
		if strings.HasPrefix(p, "windows") {
			file = "cn.exe"
		}
		if p == version.Platform() {
			data = bin
		}
		sum := sha256.Sum256(data)
		lines = append(lines, fmt.Sprintf("    %q: {\"file\": %q, \"sha256\": %q, \"size\": %d}", p, file, hex.EncodeToString(sum[:]), len(data)))
		files[fmt.Sprintf("/%s-%s/-/%s-%s-%s.tgz", pkg, p, name, p, ver)] = tgz(t, "package/", map[string][]byte{"package.json": []byte("{}"), "bin/" + file: data})
	}
	manifest := []byte(fmt.Sprintf("{\n  \"v\": 1,\n  \"version\": %q,\n  \"binaries\": {\n%s\n  }\n}\n", ver, strings.Join(lines, ",\n")))
	cert, err := sign.Issue(root, relKey.Public().(ed25519.PublicKey), sign.UseManifest, now.AddDate(0, 0, -1), now.AddDate(0, 0, 30))
	if err != nil {
		t.Fatal(err)
	}
	sig, _ := json.Marshal(sign.SignManifest(relKey, cert, manifest))
	channel := tgz(t, "package/", map[string][]byte{"package.json": []byte("{}"), "manifest.json": manifest, "manifest.sig.json": sig, "launch": []byte("#!/bin/sh\n# the release's launcher\n")})
	path := fmt.Sprintf("/%s/-/%s-%s.tgz", pkg, name, ver)
	files[path] = channel
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b, ok := files[r.URL.EscapedPath()]; ok {
			_, _ = w.Write(b)
			return
		}
		if strings.Replace(strings.TrimPrefix(r.URL.EscapedPath(), "/"), "%2f", "/", 1) == pkg {
			s := sha512.Sum512(channel)
			p := npmreg.Packument{Name: pkg, Versions: map[string]npmreg.Version{}, DistTags: map[string]string{"latest": ver, "rc": ver}}
			var v npmreg.Version
			v.Version = ver
			v.Dist.Tarball, v.Dist.Integrity = srv.URL+path, "sha512-"+base64.StdEncoding.EncodeToString(s[:])
			p.Versions[ver] = v
			_ = json.NewEncoder(w).Encode(p)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return npmreg.Client{Registry: srv.URL, HTTP: srv.Client(), MaxBytes: npmreg.DefaultMaxBytes}
}

type fakePacks struct {
	t        *testing.T
	entries  map[string][]packindex.Entry
	archives map[string][]byte
}

func (f *fakePacks) publish(id, v, channel string, requires ...string) {
	f.publishFiles(id, v, channel, "", nil, requires...)
}

// publishFiles publishes a pack whose manifest carries extra (JSON
// members after version and minEngineVersion) and whose archive carries
// files beside its manifest and prose.
func (f *fakePacks) publishFiles(id, v, channel, extra string, files map[string][]byte, requires ...string) {
	all := map[string][]byte{"pack.json": []byte(`{"version": "` + v + `", "minEngineVersion": "` + ver + `"` + extra + `}`), "RULES.md": []byte("- " + id + "\n")}
	for n, b := range files {
		all[n] = b
	}
	a := tgz(f.t, "", all)
	sum := sha256.Sum256(a)
	f.archives[id+"/"+v] = a
	f.entries[id] = append(f.entries[id], packindex.Entry{Version: v, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(a)), MinEngineVersion: ver, Channel: channel, Requires: requires})
}

func (f *fakePacks) VerifiedIndex(id string) (fetch.Verified, error) {
	e, ok := f.entries[id]
	if !ok {
		return fetch.Verified{}, fmt.Errorf("packs: %s: no source answered", id)
	}
	return fetch.Verified{Index: packindex.Index{V: 1, Pack: id, Serial: 2, Versions: e}, From: "cdn"}, nil
}

func (f *fakePacks) Archive(id string, e packindex.Entry) ([]byte, error) {
	return f.archives[id+"/"+e.Version], nil
}

func newPacks(t *testing.T) *fakePacks {
	f := &fakePacks{t: t, entries: map[string][]packindex.Entry{}, archives: map[string][]byte{}}
	f.publish("hello", "1.0", "canary", "base")
	f.publish("base", "2.0", "stable")
	return f
}

func input(t *testing.T, repo string) (adopt.Input, *bytes.Buffer) {
	var out bytes.Buffer
	return adopt.Input{Repo: repo, Channel: "canary", Reader: newPacks(t), Timeout: 10 * time.Second, Out: &out,
		Fetch: update.FetchInput{Registry: registry(t), Roots: []ed25519.PublicKey{root.Public().(ed25519.PublicKey)},
			CacheRoot: filepath.Join(t.TempDir(), "claudinite"), Platform: version.Platform(), Now: now}}, &out
}
