package releasefiles

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestFormatRoundTripsWithOneLinePerBinary(t *testing.T) {
	m := Manifest{V: 1, Version: "1.61001.1", BuiltAt: "2026-10-01T00:00:00Z", Commit: "abc", GoVersion: "go1.24",
		Binaries: map[string]Binary{
			"windows-x64": {File: "cn.exe", SHA256: strings.Repeat("a", 64), Size: 3},
			"linux-x64":   {File: "cn", SHA256: strings.Repeat("b", 64), Size: 4},
		}}
	raw := Format(m)
	got, err := ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Binaries["linux-x64"] != m.Binaries["linux-x64"] || got.Binaries["windows-x64"] != m.Binaries["windows-x64"] {
		t.Fatalf("round trip lost entries: %+v", got)
	}
	line := regexp.MustCompile(`(?m)^    "linux-x64": \{"file": "cn", "sha256": "b{64}", "size": 4\},$`)
	if !line.Match(raw) {
		t.Fatalf("linux-x64 entry is not on one line:\n%s", raw)
	}
	if strings.Index(string(raw), "linux-x64") > strings.Index(string(raw), "windows-x64") {
		t.Fatal("platforms out of release order")
	}
}

// Every UpdaterSource path is in the tree: one left behind by a move would
// silently drop out of the digest.
func TestUpdaterSourceExists(t *testing.T) {
	for _, src := range UpdaterSource {
		if _, err := os.Stat(filepath.Join("../../..", filepath.FromSlash(src))); err != nil {
			t.Errorf("UpdaterSource names %s: %v", src, err)
		}
	}
}

func TestUpdaterDigestFollowsTheUpdaterSource(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("cn/launcher/launch", "#!/bin/sh\n")
	write("cn/lifecycle/update/a.go", "package update\n")
	write("cn/lifecycle/update/sub/b.go", "package sub\n")
	write("cn/lifecycle/workflows/templates/w.yml", "on: push\n")
	write("cn/lifecycle/verify/v.go", "package verify\n")
	write("cn/shared/npmreg/n.go", "package npmreg\n")
	write("cn/shared/githubapi/g.go", "package githubapi\n")
	write("cn/shared/gitcmd/c.go", "package gitcmd\n")
	write("cn/shared/settings/s.go", "package settings\n")
	write("cn/hooks/x.go", "package hooks\n")
	first, err := UpdaterDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 64 {
		t.Fatalf("digest %q", first)
	}
	if again, _ := UpdaterDigest(root); again != first {
		t.Fatal("digest is not deterministic")
	}
	write("cn/hooks/x.go", "package hooks // changed\n")
	write("cn/lifecycle/verify/v.go", "package verify // changed\n")
	if d, _ := UpdaterDigest(root); d != first {
		t.Error("a change outside the updater moved the digest")
	}
	for _, change := range []func(){
		func() { write("cn/lifecycle/update/sub/b.go", "package sub // changed\n") },
		func() { write("cn/lifecycle/update/c.go", "package update\n") },
		func() { write("cn/launcher/launch", "#!/bin/sh\n# changed\n") },
		func() {
			_ = os.Rename(filepath.Join(root, "cn/lifecycle/update/c.go"), filepath.Join(root, "cn/lifecycle/update/d.go"))
		},
		func() { write("cn/lifecycle/workflows/templates/w.yml", "on: pull_request\n") },
		func() { write("cn/shared/npmreg/n.go", "package npmreg // changed\n") },
		func() { write("cn/shared/githubapi/g.go", "package githubapi // changed\n") },
		func() { write("cn/shared/gitcmd/c.go", "package gitcmd // changed\n") },
		func() { write("cn/shared/settings/s.go", "package settings // changed\n") },
	} {
		before, _ := UpdaterDigest(root)
		change()
		if after, _ := UpdaterDigest(root); after == before {
			t.Error("an updater change left the digest as it was")
		}
	}
	if err := os.RemoveAll(filepath.Join(root, "cn/lifecycle")); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdaterDigest(root); err == nil {
		t.Error("digest of a tree without lifecycle/")
	}
}

func TestFormatPinsUpdaterDigestAfterGoVersion(t *testing.T) {
	m := Manifest{V: 1, Version: "1.61001.1", BuiltAt: "2026-10-01T00:00:00Z", Commit: "abc", GoVersion: "go1.24", UpdaterDigest: strings.Repeat("c", 64),
		Binaries: map[string]Binary{"linux-x64": {File: "cn", SHA256: strings.Repeat("b", 64), Size: 4}}}
	raw := string(Format(m))
	lines := strings.Split(raw, "\n")
	if lines[6] != `  "updaterDigest": "`+strings.Repeat("c", 64)+`",` || lines[5] != `  "goVersion": "go1.24",` || lines[7] != `  "binaries": {` {
		t.Fatalf("updaterDigest is not the line after goVersion:\n%s", raw)
	}
	got, err := ParseManifest([]byte(raw))
	if err != nil || got.UpdaterDigest != m.UpdaterDigest {
		t.Fatalf("round trip: %v %+v", err, got)
	}
}
