package release

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/dev/release/releasefiles"
)

// tarballNames reads the package.json name of every tarball in DIST/tarballs.
func tarballNames(t *testing.T, dist string) map[string]string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dist, "tarballs", "*.tgz"))
	out := map[string]string{}
	for _, f := range files {
		raw, err := exec.Command("tar", "-xzOf", f, "package/package.json").Output()
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for _, l := range strings.Split(string(raw), "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(l), `"name": "`); ok {
				out[filepath.Base(f)] = strings.TrimSuffix(v, `",`)
			}
		}
	}
	return out
}

// tarballRepositoryURLs reads the package.json repository.url of every
// tarball in DIST/tarballs.
func tarballRepositoryURLs(t *testing.T, dist string) map[string]string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dist, "tarballs", "*.tgz"))
	out := map[string]string{}
	for _, f := range files {
		raw, err := exec.Command("tar", "-xzOf", f, "package/package.json").Output()
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		var pj struct {
			Repository struct {
				URL string `json:"url"`
			} `json:"repository"`
		}
		if err := json.Unmarshal(raw, &pj); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		out[filepath.Base(f)] = pj.Repository.URL
	}
	return out
}

func TestBuildNamesEveryTarballOfTheCLIPackage(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds a release; the full run covers it")
	}
	dist := filepath.Join(t.TempDir(), "dist")
	if out, err := runScript(t, []string{"VERSION=0.0.0", "DIST=" + dist}, "dev/release/build.sh"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if out, err := runScript(t, []string{"DIST=" + dist}, "dev/release/smoke.sh"); err != nil {
		t.Fatalf("smoke: %v\n%s", err, out)
	}
	for file, url := range tarballRepositoryURLs(t, dist) {
		if url != "git+https://github.com/missingbulb/ClaudiniteEngine.git" {
			t.Errorf("%s names repository.url %q; npm matches repository.url against the publishing repository", file, url)
		}
	}
	var got []string
	for file, name := range tarballNames(t, dist) {
		if file != strings.TrimPrefix(name, "@claudinite/")+"-0.0.0.tgz" {
			t.Errorf("tarball %s holds %s", file, name)
		}
		got = append(got, name)
	}
	sort.Strings(got)
	want := CLIPackages()
	sort.Strings(want)
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("tarballs %v, want %v", got, want)
	}
}

// A staging build is linux-x64 alone: its manifest, its packages and the
// smoke all name that one platform, and the smoke refuses it as a full
// release.
func TestBuildRestrictsPlatforms(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds a release; the full run covers it")
	}
	dist := filepath.Join(t.TempDir(), "dist")
	if out, err := runScript(t, []string{"VERSION=1.61005.1", "DIST=" + dist, "PLATFORMS=linux-x64"}, "dev/release/build.sh"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var got []string
	for _, name := range tarballNames(t, dist) {
		got = append(got, name)
	}
	sort.Strings(got)
	if strings.Join(got, " ") != "@claudinite/cli @claudinite/cli-linux-x64" {
		t.Errorf("tarballs %v", got)
	}
	raw, err := os.ReadFile(filepath.Join(dist, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := releasefiles.ParseManifest(raw)
	if err != nil || len(m.Binaries) != 1 || m.Binaries["linux-x64"].File != "cn" {
		t.Errorf("manifest binaries %+v %v", m.Binaries, err)
	}
	if out, err := runScript(t, []string{"DIST=" + dist, "PLATFORMS=linux-x64"}, "dev/release/smoke.sh"); err != nil {
		t.Errorf("smoke of the staging build: %v\n%s", err, out)
	}
	if out, err := runScript(t, []string{"DIST=" + dist}, "dev/release/smoke.sh"); err == nil || !strings.Contains(out, "linux-arm64") {
		t.Errorf("smoke passed a staging build as a full one: %v\n%s", err, out)
	}
	for _, bad := range []string{"linux-x86", "linux-x64 linux-x64", " "} {
		if out, err := runScript(t, []string{"VERSION=1.61005.1", "DIST=" + filepath.Join(t.TempDir(), "dist"), "PLATFORMS=" + bad}, "dev/release/build.sh"); err == nil {
			t.Errorf("PLATFORMS=%q built:\n%s", bad, out)
		}
	}
}
