package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/release/releasefiles"
	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

const ver = "60930.1.0"

type tamper struct {
	manifest, sig, binary bool
}

// rcTarballs writes the rc channel's six tarballs for ver into a folder, as
// npm pack names them once the scope prefix is dropped, signed with the
// development release key.
func rcTarballs(t *testing.T, how tamper) string {
	t.Helper()
	dir := t.TempDir()
	bins := map[string]releasefiles.Binary{}
	data := map[string][]byte{}
	for _, p := range version.Platforms {
		d := []byte("binary for " + p)
		sum := sha256.Sum256(d)
		bins[p] = releasefiles.Binary{File: releasefiles.BinaryName(p), SHA256: hex.EncodeToString(sum[:]), Size: int64(len(d))}
		data[p] = d
	}
	manifest := releasefiles.Format(releasefiles.Manifest{V: 1, Version: ver, BuiltAt: "2026-09-30T00:00:00Z", Commit: "abc1234", GoVersion: runtime.Version(), UpdaterDigest: strings.Repeat("1", 64), Binaries: bins})
	keyRaw, _ := os.ReadFile("../../testkeys/release.key")
	key, err := sign.ParsePrivateKey(string(keyRaw))
	if err != nil {
		t.Fatal(err)
	}
	certRaw, _ := os.ReadFile("../../testkeys/release.cert.json")
	var cert sign.Certificate
	if err := json.Unmarshal(certRaw, &cert); err != nil {
		t.Fatal(err)
	}
	sig, _ := json.MarshalIndent(sign.SignManifest(key, cert, manifest), "", "  ")
	if how.manifest {
		manifest = append([]byte{}, manifest...)
		manifest[len(manifest)/2] ^= 1
	}
	if how.sig {
		sig = []byte(strings.Replace(string(sig), `"signature": "`, `"signature": "A`, 1))
	}
	if how.binary {
		data["darwin-x64"] = append([]byte{}, data["darwin-x64"]...)
		data["darwin-x64"][3] ^= 1
	}
	write := func(name string, files ...releasefiles.TarFile) {
		if err := releasefiles.WriteTarball(filepath.Join(dir, name), files); err != nil {
			t.Fatal(err)
		}
	}
	write("cli-rc-"+ver+".tgz",
		releasefiles.TarFile{Name: "package.json", Mode: 0o644, Data: []byte(`{"name": "@claudinite/cli-rc"}`)},
		releasefiles.TarFile{Name: "manifest.json", Mode: 0o644, Data: manifest},
		releasefiles.TarFile{Name: "manifest.sig.json", Mode: 0o644, Data: sig})
	for _, p := range version.Platforms {
		write("cli-rc-"+p+"-"+ver+".tgz",
			releasefiles.TarFile{Name: "package.json", Mode: 0o644, Data: []byte(`{"name": "@claudinite/cli-rc-` + p + `"}`)},
			releasefiles.TarFile{Name: "bin/" + releasefiles.BinaryName(p), Mode: 0o755, Data: data[p]})
	}
	return dir
}

func passing() error { return nil }
func failing() error { return errors.New("--- FAIL: TestStableBuildDoesNotEmbedTheDevRoot") }

func TestCheck(t *testing.T) {
	cases := []struct {
		name   string
		how    tamper
		stable func() error
		want   string
		reason string
		verdct string
	}{
		{"intact", tamper{}, passing, "pass", "", "pass"},
		{"flipped manifest byte", tamper{manifest: true}, passing, "refuse", "signature", "not-run"},
		{"bad signature", tamper{sig: true}, passing, "refuse", "signature", "not-run"},
		{"flipped binary byte", tamper{binary: true}, passing, "refuse", "darwin-x64", "not-run"},
		{"stable test fails", tamper{}, failing, "refuse", "#5", "fail"},
	}
	for _, c := range cases {
		dir := rcTarballs(t, c.how)
		got, reason, stable := Check(dir, ver, "../../license/devroots", "abc1234ffffffffffffffffffffffffffffffff", c.stable)
		if got != c.want {
			t.Errorf("%s: %s (%s), want %s", c.name, got, reason, c.want)
		}
		if stable != c.verdct {
			t.Errorf("%s: stable test verdict %q, want %q", c.name, stable, c.verdct)
		}
		if c.reason != "" && !strings.Contains(reason, c.reason) {
			t.Errorf("%s: reason %q lacks %q", c.name, reason, c.reason)
		}
	}
}

func TestCheckRefusesAMissingPlatform(t *testing.T) {
	dir := rcTarballs(t, tamper{})
	_ = os.Remove(filepath.Join(dir, "cli-rc-windows-x64-"+ver+".tgz"))
	if got, reason, _ := Check(dir, ver, "../../license/devroots", "abc1234ffffffffffffffffffffffffffffffff", passing); got != "refuse" || !strings.Contains(reason, "windows-x64") {
		t.Errorf("%s %s", got, reason)
	}
}

// A moved v<version> tag must not test a different commit than the bytes
// were built from.
func TestCheckRefusesACandidateCheckoutAtAnotherCommit(t *testing.T) {
	dir := rcTarballs(t, tamper{})
	head := "def5678000000000000000000000000000000000"
	got, reason, _ := Check(dir, ver, "../../license/devroots", head, passing)
	if got != "refuse" || !strings.Contains(reason, head) || !strings.Contains(reason, "abc1234") {
		t.Errorf("%s %s", got, reason)
	}
}
