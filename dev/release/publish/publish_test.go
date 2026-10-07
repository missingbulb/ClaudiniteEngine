package publish

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/dev/internal/scripttest"
	"github.com/missingbulb/ClaudiniteEngine/dev/release"

	"github.com/missingbulb/ClaudiniteEngine/dev/release/create/releasefiles"
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

var publishLine = regexp.MustCompile(`^dry-run: npm publish (\S+\.tgz) --access public --provenance false --tag (\S+)$`)

// publishLines is the tarball of each dry-run publish line, and the tags
// they carry.
func publishLines(t *testing.T, out string) ([]string, map[string]bool) {
	t.Helper()
	var files []string
	tags := map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		if m := publishLine.FindStringSubmatch(l); m != nil {
			files = append(files, filepath.Base(m[1]))
			tags[m[2]] = true
		}
	}
	return files, tags
}

func TestPublishDryRunPrintsOneLinePerTarball(t *testing.T) {
	t.Parallel()
	for _, tag := range []string{"rc", "staging"} {
		dist := fakeTarballs(t, "1.61001.1", release.CLIPackages()...)
		out, err := scripttest.Run(t, []string{"DIST=" + dist, "VERSION=1.61001.1", "PATH=" + scripttest.NoNpmPath(t)}, "dev/release/publish/publish.sh", "--tag", tag, "--dry-run")
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		files, tags := publishLines(t, out)
		if len(files) != 6 {
			t.Fatalf("%d publish lines, want 6:\n%s", len(files), out)
		}
		if files[len(files)-1] != "cli-1.61001.1.tgz" {
			t.Errorf("the manifest package is not published last: %v", files)
		}
		if len(tags) != 1 || !tags[tag] {
			t.Errorf("--tag %s published under %v", tag, tags)
		}
	}
}

// A staging build publishes the manifest package and linux-x64 alone.
func TestPublishAStagingBuild(t *testing.T) {
	t.Parallel()
	dist := fakeTarballs(t, "1.61005.1", "@claudinite/cli", "@claudinite/cli-linux-x64")
	out, err := scripttest.Run(t, []string{"DIST=" + dist, "VERSION=1.61005.1", "PATH=" + scripttest.NoNpmPath(t)}, "dev/release/publish/publish.sh", "--tag", "staging", "--dry-run")
	if files, _ := publishLines(t, out); err != nil || strings.Join(files, " ") != "cli-linux-x64-1.61005.1.tgz cli-1.61005.1.tgz" {
		t.Errorf("err %v\n%s", err, out)
	}
}

func TestPublishRefusesBeforePublishingAnything(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		tag  string
		dist string
	}{
		"name outside @claudinite":  {"rc", fakeTarballs(t, "1.61001.1", append(release.CLIPackages(), "@evil/cli")...)},
		"the retired rc package":    {"rc", fakeTarballs(t, "1.61001.1", append(release.CLIPackages(), "@claudinite/cli-rc-linux-x64")...)},
		"the sdk, not the engine's": {"rc", fakeTarballs(t, "1.61001.1", append(release.CLIPackages(), "@claudinite/sdk")...)},
		"win32 platform name":       {"rc", fakeTarballs(t, "1.61001.1", "@claudinite/cli-win32-x64")},
		"latest, which only moves":  {"latest", fakeTarballs(t, "1.61001.1", release.CLIPackages()...)},
		"a tag that is no channel":  {"beta", fakeTarballs(t, "1.61001.1", release.CLIPackages()...)},
	}
	for name, c := range cases {
		out, err := scripttest.Run(t, []string{"DIST=" + c.dist, "VERSION=1.61001.1", "PATH=" + scripttest.NoNpmPath(t)}, "dev/release/publish/publish.sh", "--tag", c.tag, "--dry-run")
		if err == nil {
			t.Errorf("%s: accepted\n%s", name, out)
		}
		if strings.Contains(out, "npm publish") {
			t.Errorf("%s: printed a publish before refusing:\n%s", name, out)
		}
	}

	stale := fakeTarballs(t, "1.61001.1", release.CLIPackages()...)
	out, err := scripttest.Run(t, []string{"DIST=" + stale, "VERSION=1.61001.2", "PATH=" + scripttest.NoNpmPath(t)}, "dev/release/publish/publish.sh", "--tag", "rc", "--dry-run")
	if err == nil || strings.Contains(out, "npm publish") || !strings.Contains(out, "1.61001.2") {
		t.Errorf("stale dist: err %v\n%s", err, out)
	}
}

// fakeNpm is PATH with a fake npm first: view finds nothing and publish
// fails E404, the way registry.npmjs.org refuses a package with no trusted
// publisher. Every call is appended to the returned log file.
func fakeNpm(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	script := `#!/bin/sh
echo "${npm_config_loglevel:+loglevel=$npm_config_loglevel }$*" >> "` + log + `"
case $1 in
  view) exit 1 ;;
  publish)
    echo "npm error code E404" >&2
    echo "npm error 404 Not Found - PUT https://registry.npmjs.org/@claudinite%2fcli-darwin-arm64 - Not found" >&2
    exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "npm"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir + string(os.PathListSeparator) + os.Getenv("PATH"), log
}

func readCalls(t *testing.T, log string) string {
	t.Helper()
	raw, err := os.ReadFile(log)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(raw)
}

func TestPublishOIDCRefusalPointsAtTheTrustedPublisher(t *testing.T) {
	t.Parallel()
	dist := fakeTarballs(t, "1.61001.1", release.CLIPackages()...)
	path, log := fakeNpm(t)
	out, err := scripttest.Run(t, []string{"DIST=" + dist, "VERSION=1.61001.1", "PATH=" + path}, "dev/release/publish/publish.sh", "--tag", "rc", "--auth", "oidc")
	if err == nil || !strings.Contains(out, "attach its trusted publisher") {
		t.Fatalf("err %v\n%s", err, out)
	}
	if calls := readCalls(t, log); !strings.Contains(calls, "loglevel=verbose publish") {
		t.Errorf("an OIDC publish ran npm below verbose, so a refusal hides the token exchange:\n%s", calls)
	}
}

func TestPublishRequiresAnAuthModeToPublish(t *testing.T) {
	t.Parallel()
	dist := fakeTarballs(t, "1.61001.1", release.CLIPackages()...)
	for _, args := range [][]string{{"--tag", "rc"}, {"--tag", "rc", "--auth", "password"}} {
		out, err := scripttest.Run(t, []string{"DIST=" + dist, "VERSION=1.61001.1", "PATH=" + scripttest.NoNpmPath(t)}, "dev/release/publish/publish.sh", args...)
		if err == nil || !strings.Contains(out, "--auth") || strings.Contains(out, "npm was called") {
			t.Errorf("%v: err %v\n%s", args, err, out)
		}
	}
}
