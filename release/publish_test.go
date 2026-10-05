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

var publishLine = regexp.MustCompile(`^dry-run: npm publish (\S+\.tgz) --access public --provenance false --tag latest$`)

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
	t.Parallel()
	dist := fakeTarballs(t, "1.61001.1", rcNames()...)
	out, err := runScript(t, []string{"DIST=" + dist, "VERSION=1.61001.1", "PATH=" + noNpmPath(t)}, "release/publish.sh", "--channel", "rc", "--dry-run")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	files := publishLines(t, out)
	if len(files) != 6 {
		t.Fatalf("%d publish lines, want 6:\n%s", len(files), out)
	}
	if files[len(files)-1] != "cli-rc-1.61001.1.tgz" {
		t.Errorf("the channel package is not published last: %v", files)
	}
}

func TestPublishDryRunCoversTheStableChannel(t *testing.T) {
	t.Parallel()
	var stable []string
	for _, p := range Packages() {
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
	t.Parallel()
	cases := map[string]struct {
		channel string
		dist    string
	}{
		"name outside @claudinite": {"rc", fakeTarballs(t, "1.61001.1", append(rcNames(), "@evil/cli-rc")...)},
		"stable name on rc":        {"rc", fakeTarballs(t, "1.61001.1", append(rcNames(), "@claudinite/cli-linux-x64")...)},
		"rc name on stable":        {"stable", fakeTarballs(t, "1.61001.1", "@claudinite/cli", "@claudinite/cli-rc-linux-x64")},
		"win32 platform name":      {"rc", fakeTarballs(t, "1.61001.1", "@claudinite/cli-rc-win32-x64")},
	}
	for name, c := range cases {
		out, err := runScript(t, []string{"DIST=" + c.dist, "VERSION=1.61001.1", "PATH=" + noNpmPath(t)}, "release/publish.sh", "--channel", c.channel, "--dry-run")
		if err == nil {
			t.Errorf("%s: accepted\n%s", name, out)
		}
		if strings.Contains(out, "npm publish") {
			t.Errorf("%s: printed a publish before refusing:\n%s", name, out)
		}
	}

	stale := fakeTarballs(t, "1.61001.1", rcNames()...)
	out, err := runScript(t, []string{"DIST=" + stale, "VERSION=1.61001.2", "PATH=" + noNpmPath(t)}, "release/publish.sh", "--channel", "rc", "--dry-run")
	if err == nil || strings.Contains(out, "npm publish") || !strings.Contains(out, "1.61001.2") {
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
    echo "npm error 404 Not Found - PUT https://registry.npmjs.org/@claudinite%2fcli-rc-darwin-arm64 - Not found" >&2
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
	dist := fakeTarballs(t, "1.61001.1", rcNames()...)
	path, log := fakeNpm(t)
	out, err := runScript(t, []string{"DIST=" + dist, "VERSION=1.61001.1", "PATH=" + path}, "release/publish.sh", "--channel", "rc", "--auth", "oidc")
	if err == nil || !strings.Contains(out, "attach its trusted publisher") {
		t.Fatalf("err %v\n%s", err, out)
	}
	if calls := readCalls(t, log); !strings.Contains(calls, "loglevel=verbose publish") {
		t.Errorf("an OIDC publish ran npm below verbose, so a refusal hides the token exchange:\n%s", calls)
	}
}

func TestPublishRequiresAnAuthModeToPublish(t *testing.T) {
	t.Parallel()
	dist := fakeTarballs(t, "1.61001.1", rcNames()...)
	for _, args := range [][]string{{"--channel", "rc"}, {"--channel", "rc", "--auth", "password"}} {
		out, err := runScript(t, []string{"DIST=" + dist, "VERSION=1.61001.1", "PATH=" + noNpmPath(t)}, "release/publish.sh", args...)
		if err == nil || !strings.Contains(out, "--auth") || strings.Contains(out, "npm was called") {
			t.Errorf("%v: err %v\n%s", args, err, out)
		}
	}
}
