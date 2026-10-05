package release

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/release/releasefiles"
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
	dist := filepath.Join(t.TempDir(), "dist")
	if out, err := runScript(t, []string{"VERSION=0.0.0", "DIST=" + dist}, "release/build.sh", "--placeholder-sdk"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if out, err := runScript(t, []string{"DIST=" + dist}, "release/smoke.sh"); err != nil {
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
	var want []string
	for _, p := range Placeholders() {
		want = append(want, p.Name)
	}
	sort.Strings(want)
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("tarballs %v, want %v", got, want)
	}
}

// A staging build is linux-x64 alone: its manifest, its packages and the
// smoke all name that one platform, and the smoke refuses it as a full
// release.
func TestBuildRestrictsPlatforms(t *testing.T) {
	dist := filepath.Join(t.TempDir(), "dist")
	if out, err := runScript(t, []string{"VERSION=1.61005.1", "DIST=" + dist, "PLATFORMS=linux-x64"}, "release/build.sh"); err != nil {
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
	if out, err := runScript(t, []string{"DIST=" + dist, "PLATFORMS=linux-x64"}, "release/smoke.sh"); err != nil {
		t.Errorf("smoke of the staging build: %v\n%s", err, out)
	}
	if out, err := runScript(t, []string{"DIST=" + dist}, "release/smoke.sh"); err == nil || !strings.Contains(out, "linux-arm64") {
		t.Errorf("smoke passed a staging build as a full one: %v\n%s", err, out)
	}
	for _, bad := range []string{"linux-x86", "linux-x64 linux-x64", " "} {
		if out, err := runScript(t, []string{"VERSION=1.61005.1", "DIST=" + filepath.Join(t.TempDir(), "dist"), "PLATFORMS=" + bad}, "release/build.sh"); err == nil {
			t.Errorf("PLATFORMS=%q built:\n%s", bad, out)
		}
	}
}

func TestPlaceholderSDK(t *testing.T) {
	dist := filepath.Join(t.TempDir(), "dist")
	out, err := runScript(t, []string{"VERSION=0.0.0", "DIST=" + dist}, "release/build.sh", "--placeholder-sdk")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	sdk := filepath.Join(dist, "npm", "sdk", "package")
	entries, _ := os.ReadDir(sdk)
	var files []string
	for _, e := range entries {
		files = append(files, e.Name())
	}
	if strings.Join(files, " ") != "README.md index.mjs package.json" {
		t.Errorf("sdk package holds %v", files)
	}
	readme, _ := os.ReadFile(filepath.Join(sdk, "README.md"))
	if strings.TrimSpace(string(readme)) != "placeholder, see ClaudiniteEngine" {
		t.Errorf("README %q", readme)
	}
	index, _ := os.ReadFile(filepath.Join(sdk, "index.mjs"))
	if strings.TrimSpace(string(index)) != "export {};" {
		t.Errorf("index.mjs %q", index)
	}

	out, err = runScript(t, []string{"VERSION=1.61001.1", "DIST=" + filepath.Join(t.TempDir(), "dist")}, "release/build.sh", "--placeholder-sdk")
	if err == nil || !strings.Contains(out, "0.0.0") {
		t.Errorf("--placeholder-sdk at 1.61001.1: err %v\n%s", err, out)
	}
}
