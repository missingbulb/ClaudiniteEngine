package release

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/release/releasefiles"
)

// fakeTarballs writes DIST/tarballs/<name>-<version>.tgz for each package
// name, each holding only package.json.
func fakeTarballs(t *testing.T, version string, names ...string) string {
	t.Helper()
	dist := filepath.Join(t.TempDir(), "dist")
	dir := filepath.Join(dist, "tarballs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		pj := fmt.Sprintf("{\n  \"name\": %q,\n  \"version\": %q\n}\n", n, version)
		file := strings.NewReplacer("@claudinite/", "", "@", "", "/", "-").Replace(n) + "-" + version + ".tgz"
		if err := releasefiles.WriteTarball(filepath.Join(dir, file), []releasefiles.TarFile{{Name: "package.json", Mode: 0o644, Data: []byte(pj)}}); err != nil {
			t.Fatal(err)
		}
	}
	return dist
}

var publishLine = regexp.MustCompile(`^dry-run: npm publish (\S+\.tgz) --access public --provenance false$`)

func publishLines(t *testing.T, out string) []string {
	t.Helper()
	var files []string
	for _, l := range strings.Split(out, "\n") {
		if m := publishLine.FindStringSubmatch(l); m != nil {
			files = append(files, filepath.Base(m[1]))
		}
	}
	return files
}

func rcNames() []string {
	names := []string{"@claudinite/cli-rc"}
	for _, p := range []string{"linux-x64", "linux-arm64", "darwin-x64", "darwin-arm64", "windows-x64"} {
		names = append(names, "@claudinite/cli-rc-"+p)
	}
	return names
}

func TestPublishDryRunPrintsOneLinePerTarball(t *testing.T) {
	dist := fakeTarballs(t, "1.1.0", rcNames()...)
	out, err := runScript(t, []string{"DIST=" + dist, "VERSION=1.1.0", "PATH=" + noNpmPath(t)}, "release/publish.sh", "--channel", "rc", "--dry-run")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	files := publishLines(t, out)
	if len(files) != 6 {
		t.Fatalf("%d publish lines, want 6:\n%s", len(files), out)
	}
	if files[len(files)-1] != "cli-rc-1.1.0.tgz" {
		t.Errorf("the channel package is not published last: %v", files)
	}
}

func TestPublishBootstrapPlaceholders(t *testing.T) {
	var stable []string
	for _, p := range Placeholders() {
		if p.Channel == "stable" {
			stable = append(stable, p.Name)
		}
	}
	dist := fakeTarballs(t, "0.0.0", stable...)
	out, err := runScript(t, []string{"DIST=" + dist, "VERSION=0.0.0", "PATH=" + noNpmPath(t)}, "release/publish.sh", "--channel", "stable", "--dry-run")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if n := len(publishLines(t, out)); n != 7 {
		t.Errorf("%d publish lines, want 7:\n%s", n, out)
	}
}

func TestPublishRefusesBeforePublishingAnything(t *testing.T) {
	cases := map[string]struct {
		channel string
		dist    string
	}{
		"name outside @claudinite": {"rc", fakeTarballs(t, "1.1.0", append(rcNames(), "@evil/cli-rc")...)},
		"stable name on rc":        {"rc", fakeTarballs(t, "1.1.0", append(rcNames(), "@claudinite/cli-linux-x64")...)},
		"rc name on stable":        {"stable", fakeTarballs(t, "1.1.0", "@claudinite/cli", "@claudinite/cli-rc-linux-x64")},
		"win32 platform name":      {"rc", fakeTarballs(t, "1.1.0", "@claudinite/cli-rc-win32-x64")},
	}
	for name, c := range cases {
		out, err := runScript(t, []string{"DIST=" + c.dist, "VERSION=1.1.0", "PATH=" + noNpmPath(t)}, "release/publish.sh", "--channel", c.channel, "--dry-run")
		if err == nil {
			t.Errorf("%s: accepted\n%s", name, out)
		}
		if strings.Contains(out, "npm publish") {
			t.Errorf("%s: printed a publish before refusing:\n%s", name, out)
		}
	}

	stale := fakeTarballs(t, "1.1.0", rcNames()...)
	out, err := runScript(t, []string{"DIST=" + stale, "VERSION=1.2.0", "PATH=" + noNpmPath(t)}, "release/publish.sh", "--channel", "rc", "--dry-run")
	if err == nil || strings.Contains(out, "npm publish") || !strings.Contains(out, "1.2.0") {
		t.Errorf("stale dist: err %v\n%s", err, out)
	}
}

// noNpmPath is PATH with a fake npm first that fails the test if called:
// a dry run never runs npm.
func noNpmPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "npm"), []byte("#!/bin/sh\necho \"npm was called: $*\" >&2\nexit 99\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir + string(os.PathListSeparator) + os.Getenv("PATH")
}
