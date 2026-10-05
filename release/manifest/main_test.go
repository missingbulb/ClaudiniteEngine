package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/release/releasefiles"
	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

func fakeDist(t *testing.T) string {
	t.Helper()
	dist := t.TempDir()
	for _, p := range version.Platforms {
		name := "cn"
		if p == "windows-x64" {
			name = "cn.exe"
		}
		dir := filepath.Join(dist, "bin", p)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte("binary for "+p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dist
}

func tool(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return out.String(), errb.String(), code
}

func repoRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func writeSign(t *testing.T, dist string) {
	t.Helper()
	if _, e, c := tool(t, "write", "--dist", dist, "--version", "1.61001.1", "--commit", "abc1234", "--source", repoRoot(t)); c != 0 {
		t.Fatalf("write: %s", e)
	}
	root := repoRoot(t)
	if _, e, c := tool(t, "sign", "--dist", dist, "--key", filepath.Join(root, "testkeys/release.key"), "--cert", filepath.Join(root, "testkeys/release.cert.json")); c != 0 {
		t.Fatalf("sign: %s", e)
	}
}

func TestWriteListsExactlyTheFivePlatforms(t *testing.T) {
	dist := fakeDist(t)
	writeSign(t, dist)
	raw, err := os.ReadFile(filepath.Join(dist, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"v", "version", "builtAt", "commit", "goVersion", "updaterDigest", "binaries", "testedPacks"} {
		if _, ok := m[k]; !ok {
			t.Errorf("manifest lacks %q", k)
		}
	}
	if len(m) != 8 {
		t.Errorf("manifest has %d keys, want 8", len(m))
	}
	want, err := releasefiles.UpdaterDigest(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(m["updaterDigest"]); got != `"`+want+`"` {
		t.Errorf("updaterDigest %s, want %s", got, want)
	}
	parsed, err := releasefiles.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.V != 1 || parsed.Version != "1.61001.1" || parsed.Commit != "abc1234" || len(parsed.TestedPacks) != 0 {
		t.Errorf("manifest %+v", parsed)
	}
	if len(parsed.Binaries) != 5 {
		t.Fatalf("%d binaries", len(parsed.Binaries))
	}
	for _, p := range version.Platforms {
		b, ok := parsed.Binaries[p]
		if !ok {
			t.Errorf("no %s", p)
			continue
		}
		want := "cn"
		if p == "windows-x64" {
			want = "cn.exe"
		}
		if b.File != want || len(b.SHA256) != 64 || b.Size != int64(len("binary for "+p)) {
			t.Errorf("%s: %+v", p, b)
		}
	}
}

func TestWriteNeedsTheSource(t *testing.T) {
	if _, e, c := tool(t, "write", "--dist", fakeDist(t), "--version", "1.61001.1", "--commit", "x"); c == 0 || !strings.Contains(e, "--source") {
		t.Fatalf("write without --source: exit %d %s", c, e)
	}
}

func TestWriteRefusesAMissingPlatform(t *testing.T) {
	dist := fakeDist(t)
	if err := os.RemoveAll(filepath.Join(dist, "bin", "darwin-arm64")); err != nil {
		t.Fatal(err)
	}
	if _, _, c := tool(t, "write", "--dist", dist, "--version", "1.61001.1", "--commit", "x", "--source", repoRoot(t)); c == 0 {
		t.Fatal("wrote a manifest missing darwin-arm64")
	}
}

func TestVerify(t *testing.T) {
	dist := fakeDist(t)
	writeSign(t, dist)
	roots := filepath.Join(repoRoot(t), "shared/trust/devroots")
	if out, e, c := tool(t, "verify", "--dist", dist, "--roots", roots); c != 0 {
		t.Fatalf("verify: %s %s", out, e)
	}
	if _, err := os.Stat(filepath.Join(dist, "manifest.sig.json")); err != nil {
		t.Fatal("sign wrote no manifest.sig.json")
	}

	flipByte := func(p string) func() {
		raw, _ := os.ReadFile(p)
		orig := append([]byte{}, raw...)
		raw[len(raw)/2] ^= 1
		_ = os.WriteFile(p, raw, 0o644)
		return func() { _ = os.WriteFile(p, orig, 0o644) }
	}
	targets := []string{filepath.Join(dist, "manifest.json")}
	for _, p := range version.Platforms {
		name := "cn"
		if p == "windows-x64" {
			name = "cn.exe"
		}
		targets = append(targets, filepath.Join(dist, "bin", p, name))
	}
	for _, target := range targets {
		restore := flipByte(target)
		if _, _, c := tool(t, "verify", "--dist", dist, "--roots", roots); c == 0 {
			t.Errorf("verify passed with a flipped byte in %s", strings.TrimPrefix(target, dist))
		}
		restore()
	}
	if _, e, c := tool(t, "verify", "--dist", dist, "--roots", roots); c != 0 {
		t.Fatalf("restored dist fails: %s", e)
	}
	if _, _, c := tool(t, "verify", "--dist", dist, "--roots", t.TempDir()); c == 0 {
		t.Error("verify passed with no trusted roots")
	}
}

// A staging build carries linux-x64 alone: write lists exactly the named
// platforms, and verify holds the manifest to the set it is told, so a
// staging manifest never passes as a full one.
func TestWriteAndVerifyANamedPlatformSet(t *testing.T) {
	dist := fakeDist(t)
	root := repoRoot(t)
	roots := filepath.Join(root, "shared/trust/devroots")
	if _, e, c := tool(t, "write", "--dist", dist, "--version", "1.61001.1", "--commit", "abc1234", "--source", root, "--platforms", "linux-x64"); c != 0 {
		t.Fatalf("write: %s", e)
	}
	raw, _ := os.ReadFile(filepath.Join(dist, "manifest.json"))
	m, err := releasefiles.ParseManifest(raw)
	if err != nil || len(m.Binaries) != 1 || m.Binaries["linux-x64"].File != "cn" {
		t.Fatalf("manifest %+v %v", m.Binaries, err)
	}
	if _, e, c := tool(t, "sign", "--dist", dist, "--key", filepath.Join(root, "testkeys/release.key"), "--cert", filepath.Join(root, "testkeys/release.cert.json")); c != 0 {
		t.Fatalf("sign: %s", e)
	}
	if _, e, c := tool(t, "verify", "--dist", dist, "--roots", roots, "--platforms", "linux-x64"); c != 0 {
		t.Errorf("verify --platforms linux-x64: %s", e)
	}
	if _, e, c := tool(t, "verify", "--dist", dist, "--roots", roots); c == 0 || !strings.Contains(e, "1 binaries, want 5") {
		t.Errorf("a one-platform manifest verified as a full release: exit %d %s", c, e)
	}
	for _, bad := range []string{"linux-x86", "linux-x64 linux-x64", ""} {
		if _, _, c := tool(t, "write", "--dist", dist, "--version", "1.61001.1", "--commit", "abc1234", "--source", root, "--platforms", bad); c == 0 {
			t.Errorf("write --platforms %q passed", bad)
		}
	}
}

func TestIntegrity(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	_ = os.WriteFile(p, []byte("abc"), 0o644)
	out, _, c := tool(t, "integrity", p)
	// sha512("abc"), base64.
	want := "sha512-3a81oZNherrMQXNJriBBMRLm+k6JqX6iCp7u5ktV05ohkpkqJ0/BqDa6PCOj/uu9RU1EI2Q86A4qmslPpUyknw=="
	if c != 0 || strings.TrimSpace(out) != want {
		t.Fatalf("got %q", out)
	}
}

func TestSignRefusesACertificateExpiringWithin14Days(t *testing.T) {
	dist := fakeDist(t)
	if _, e, c := tool(t, "write", "--dist", dist, "--version", "1.61001.1", "--commit", "abc1234", "--source", repoRoot(t)); c != 0 {
		t.Fatal(e)
	}
	root := repoRoot(t)
	rootRaw, _ := os.ReadFile(filepath.Join(root, "testkeys/root.key"))
	rootKey, err := sign.ParsePrivateKey(string(rootRaw))
	if err != nil {
		t.Fatal(err)
	}
	pubRaw, _ := os.ReadFile(filepath.Join(root, "testkeys/release.pub"))
	pub, err := sign.ParsePublicKey(string(pubRaw))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, c := range []struct {
		days int
		ok   bool
	}{{13, false}, {15, true}} {
		cert, err := sign.Issue(rootKey, pub, sign.UseManifest, now.Add(-time.Hour), now.Add(time.Duration(c.days)*24*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		certPath := filepath.Join(t.TempDir(), "cert.json")
		raw, _ := json.Marshal(cert)
		_ = os.WriteFile(certPath, raw, 0o644)
		_, e, code := tool(t, "sign", "--dist", dist, "--key", filepath.Join(root, "testkeys/release.key"), "--cert", certPath)
		if (code == 0) != c.ok {
			t.Errorf("%d days left: exit %d %s", c.days, code, e)
		}
		if !c.ok && !strings.Contains(e, "Rotating the release key") {
			t.Errorf("%d days left: %q does not name the rotation step", c.days, e)
		}
	}
}

func TestSumsListsEveryReleaseFile(t *testing.T) {
	dist := filepath.Join(t.TempDir(), "out")
	for _, f := range []string{"bin/linux-x64/cn", "npm/cli/package/package.json", "tarballs/cli-1.61001.1.tgz", "manifest.json", "manifest.integrity"} {
		p := filepath.Join(dist, f)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(f), 0o644)
	}
	if _, e, c := tool(t, "sums", "--dist", dist); c != 0 {
		t.Fatal(e)
	}
	raw, err := os.ReadFile(filepath.Join(dist, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		names = append(names, strings.SplitN(line, "  ", 2)[1])
	}
	got := strings.Join(names, " ")
	if got != "out/bin/linux-x64/cn out/manifest.json out/npm/cli/package/package.json out/tarballs/cli-1.61001.1.tgz" {
		t.Errorf("SHA256SUMS names %s", got)
	}
}
