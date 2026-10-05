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
	for _, tag := range []string{"rc", "staging"} {
		dist := fakeTarballs(t, "1.61001.1", CLIPackages()...)
		out, err := runScript(t, []string{"DIST=" + dist, "VERSION=1.61001.1", "PATH=" + noNpmPath(t)}, "release/publish.sh", "--tag", tag, "--dry-run")
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

// The bridge for members pinned to the retired rc package publishes its
// names under latest, as each of its versions always was, and nothing else.
func TestPublishTheCLIRCBridge(t *testing.T) {
	var rc []string
	for _, n := range CLIPackages() {
		rc = append(rc, strings.Replace(n, "@claudinite/cli", "@claudinite/cli-rc", 1))
	}
	dist := fakeTarballs(t, "1.61005.1", rc...)
	out, err := runScript(t, []string{"DIST=" + dist, "VERSION=1.61005.1", "PATH=" + noNpmPath(t)}, "release/publish.sh", "--legacy-cli-rc", "--dry-run")
	files, tags := publishLines(t, out)
	if err != nil || len(files) != 6 || files[5] != "cli-rc-1.61005.1.tgz" || len(tags) != 1 || !tags["latest"] {
		t.Errorf("err %v\n%s", err, out)
	}
	for _, args := range [][]string{{"--legacy-cli-rc", "--dry-run"}, {"--legacy-cli-rc", "--tag", "rc", "--dry-run"}} {
		d := dist
		if len(args) == 2 {
			d = fakeTarballs(t, "1.61005.1", CLIPackages()...)
		}
		if out, err := runScript(t, []string{"DIST=" + d, "VERSION=1.61005.1", "PATH=" + noNpmPath(t)}, "release/publish.sh", args...); err == nil || strings.Contains(out, "npm publish") {
			t.Errorf("%v: err %v\n%s", args, err, out)
		}
	}
}

// A staging build publishes the manifest package and linux-x64 alone.
func TestPublishAStagingBuild(t *testing.T) {
	dist := fakeTarballs(t, "1.61005.1", "@claudinite/cli", "@claudinite/cli-linux-x64")
	out, err := runScript(t, []string{"DIST=" + dist, "VERSION=1.61005.1", "PATH=" + noNpmPath(t)}, "release/publish.sh", "--tag", "staging", "--dry-run")
	if files, _ := publishLines(t, out); err != nil || strings.Join(files, " ") != "cli-linux-x64-1.61005.1.tgz cli-1.61005.1.tgz" {
		t.Errorf("err %v\n%s", err, out)
	}
}

func TestPublishBootstrapPlaceholders(t *testing.T) {
	var names []string
	for _, p := range Placeholders() {
		names = append(names, p.Name)
	}
	dist := fakeTarballs(t, "0.0.0", names...)
	out, err := runScript(t, []string{"DIST=" + dist, "VERSION=0.0.0", "PATH=" + noNpmPath(t)}, "release/publish.sh", "--tag", "rc", "--dry-run")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if files, _ := publishLines(t, out); len(files) != 7 {
		t.Errorf("%d publish lines, want 7:\n%s", len(files), out)
	}
}

func TestPublishRefusesBeforePublishingAnything(t *testing.T) {
	cases := map[string]struct {
		tag  string
		dist string
	}{
		"name outside @claudinite": {"rc", fakeTarballs(t, "1.61001.1", append(CLIPackages(), "@evil/cli")...)},
		"the retired rc package":   {"rc", fakeTarballs(t, "1.61001.1", append(CLIPackages(), "@claudinite/cli-rc-linux-x64")...)},
		"the sdk past its reserve": {"rc", fakeTarballs(t, "1.61001.1", append(CLIPackages(), "@claudinite/sdk")...)},
		"win32 platform name":      {"rc", fakeTarballs(t, "1.61001.1", "@claudinite/cli-win32-x64")},
		"latest, which only moves": {"latest", fakeTarballs(t, "1.61001.1", CLIPackages()...)},
		"a tag that is no channel": {"beta", fakeTarballs(t, "1.61001.1", CLIPackages()...)},
	}
	for name, c := range cases {
		out, err := runScript(t, []string{"DIST=" + c.dist, "VERSION=1.61001.1", "PATH=" + noNpmPath(t)}, "release/publish.sh", "--tag", c.tag, "--dry-run")
		if err == nil {
			t.Errorf("%s: accepted\n%s", name, out)
		}
		if strings.Contains(out, "npm publish") {
			t.Errorf("%s: printed a publish before refusing:\n%s", name, out)
		}
	}

	stale := fakeTarballs(t, "1.61001.1", CLIPackages()...)
	out, err := runScript(t, []string{"DIST=" + stale, "VERSION=1.61001.2", "PATH=" + noNpmPath(t)}, "release/publish.sh", "--tag", "rc", "--dry-run")
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

// fakeNpm is PATH with a fake npm first: whoami prints whoami (or fails 401
// when empty), org ls prints orgJSON (or fails E404 when empty), view finds
// nothing and publish fails E404 the way registry.npmjs.org refused the
// first bootstrap. Every call is appended to the returned log file.
func fakeNpm(t *testing.T, whoami, orgJSON string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	script := `#!/bin/sh
echo "${npm_config_loglevel:+loglevel=$npm_config_loglevel }$*" >> "` + log + `"
case $1 in
  whoami)
    [ -n "` + whoami + `" ] || { echo "npm error code E401" >&2; echo "npm error 401 Unauthorized - GET https://registry.npmjs.org/-/whoami" >&2; exit 1; }
    echo "` + whoami + `" ;;
  org)
    [ -n '` + orgJSON + `' ] || { echo "npm error code E404" >&2; echo "npm error 404 Not Found - GET https://registry.npmjs.org/-/org/claudinite/user" >&2; exit 1; }
    echo '` + orgJSON + `' ;;
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

func TestPublishTokenAuthMustAuthenticateBeforePublishing(t *testing.T) {
	dist := fakeTarballs(t, "0.0.0", CLIPackages()...)
	path, log := fakeNpm(t, "", "")
	out, err := runScript(t, []string{"DIST=" + dist, "VERSION=0.0.0", "PATH=" + path}, "release/publish.sh", "--tag", "rc", "--auth", "token", "--skip-existing")
	if err == nil {
		t.Fatalf("published with a token npm whoami rejects:\n%s", out)
	}
	if calls := readCalls(t, log); strings.Contains(calls, "publish") {
		t.Errorf("ran npm publish before the token authenticated:\n%s", calls)
	}
	if !strings.Contains(out, "does not authenticate") || strings.Contains(out, "trusted publisher") {
		t.Errorf("message does not name the token as the cause:\n%s", out)
	}
}

func TestPublishTokenAuthNamesTheCauseOfARefusal(t *testing.T) {
	cases := map[string]struct {
		whoami, org string
		want        []string
	}{
		"no org or not a member":        {"ariel", "", []string{"ariel", "npm org claudinite", "does not exist or ariel is not a member"}},
		"member, token lacks the scope": {"ariel", `{"ariel": "owner"}`, []string{"ariel", "owner of the npm org claudinite", "Packages and scopes", "read and write", "@claudinite"}},
		"listed org, user absent":       {"ariel", `{"someone": "owner"}`, []string{"ariel is not a member of the npm org claudinite"}},
	}
	for name, c := range cases {
		dist := fakeTarballs(t, "0.0.0", CLIPackages()...)
		path, log := fakeNpm(t, c.whoami, c.org)
		out, err := runScript(t, []string{"DIST=" + dist, "VERSION=0.0.0", "PATH=" + path}, "release/publish.sh", "--tag", "rc", "--auth", "token", "--skip-existing")
		if calls := readCalls(t, log); strings.Contains(calls, "loglevel=verbose") {
			t.Errorf("%s: a token publish ran npm at verbose:\n%s", name, calls)
		}
		if err == nil {
			t.Errorf("%s: a refused publish succeeded:\n%s", name, out)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Errorf("%s: message lacks %q:\n%s", name, w, out)
			}
		}
		if strings.Contains(out, "trusted publisher") {
			t.Errorf("%s: a token publish was told to attach a trusted publisher:\n%s", name, out)
		}
	}
}

func TestPublishOIDCRefusalPointsAtTheTrustedPublisher(t *testing.T) {
	dist := fakeTarballs(t, "1.61001.1", CLIPackages()...)
	path, log := fakeNpm(t, "", "")
	out, err := runScript(t, []string{"DIST=" + dist, "VERSION=1.61001.1", "PATH=" + path}, "release/publish.sh", "--tag", "rc", "--auth", "oidc")
	if err == nil || !strings.Contains(out, "attach its trusted publisher") {
		t.Fatalf("err %v\n%s", err, out)
	}
	calls := readCalls(t, log)
	if strings.Contains(calls, "whoami") {
		t.Errorf("an OIDC publish ran npm whoami, which has no token to check:\n%s", calls)
	}
	if !strings.Contains(calls, "loglevel=verbose publish") {
		t.Errorf("an OIDC publish ran npm below verbose, so a refusal hides the token exchange:\n%s", calls)
	}
}

func TestPublishRequiresAnAuthModeToPublish(t *testing.T) {
	dist := fakeTarballs(t, "1.61001.1", CLIPackages()...)
	for _, args := range [][]string{{"--tag", "rc"}, {"--tag", "rc", "--auth", "password"}} {
		out, err := runScript(t, []string{"DIST=" + dist, "VERSION=1.61001.1", "PATH=" + noNpmPath(t)}, "release/publish.sh", args...)
		if err == nil || !strings.Contains(out, "--auth") || strings.Contains(out, "npm was called") {
			t.Errorf("%v: err %v\n%s", args, err, out)
		}
	}
}
