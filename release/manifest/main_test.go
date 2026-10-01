package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/release/releasefiles"
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
	if _, e, c := tool(t, "write", "--dist", dist, "--version", "1.1.0", "--commit", "abc1234"); c != 0 {
		t.Fatalf("write: %s", e)
	}
	root := repoRoot(t)
	if _, e, c := tool(t, "sign", "--dist", dist, "--key", filepath.Join(root, "keys/dev/release.key"), "--cert", filepath.Join(root, "keys/dev/release.cert.json")); c != 0 {
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
	for _, k := range []string{"v", "version", "builtAt", "commit", "goVersion", "binaries", "testedPacks"} {
		if _, ok := m[k]; !ok {
			t.Errorf("manifest lacks %q", k)
		}
	}
	if len(m) != 7 {
		t.Errorf("manifest has %d keys, want 7", len(m))
	}
	parsed, err := releasefiles.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.V != 1 || parsed.Version != "1.1.0" || parsed.Commit != "abc1234" || len(parsed.TestedPacks) != 0 {
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

func TestWriteRefusesAMissingPlatform(t *testing.T) {
	dist := fakeDist(t)
	if err := os.RemoveAll(filepath.Join(dist, "bin", "darwin-arm64")); err != nil {
		t.Fatal(err)
	}
	if _, _, c := tool(t, "write", "--dist", dist, "--version", "1.1.0", "--commit", "x"); c == 0 {
		t.Fatal("wrote a manifest missing darwin-arm64")
	}
}

func TestVerify(t *testing.T) {
	dist := fakeDist(t)
	writeSign(t, dist)
	roots := filepath.Join(repoRoot(t), "license/roots")
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
