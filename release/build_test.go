package release

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
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

func TestBuildNamesEveryTarballForItsChannel(t *testing.T) {
	for _, pkg := range []string{"@claudinite/cli", "@claudinite/cli-rc"} {
		dist := filepath.Join(t.TempDir(), "dist")
		env := []string{"VERSION=0.0.0", "PACKAGE=" + pkg, "DIST=" + dist}
		if out, err := runScript(t, env, "release/build.sh"); err != nil {
			t.Fatalf("%s: %v\n%s", pkg, err, out)
		}
		if out, err := runScript(t, []string{"DIST=" + dist}, "release/smoke.sh"); err != nil {
			t.Fatalf("%s: smoke: %v\n%s", pkg, err, out)
		}
		names := tarballNames(t, dist)
		for file, url := range tarballRepositoryURLs(t, dist) {
			if url != "git+https://github.com/missingbulb/ClaudiniteEngine.git" {
				t.Errorf("%s: %s names repository.url %q; npm matches repository.url against the publishing repository", pkg, file, url)
			}
		}
		var got []string
		for file, name := range names {
			if file != strings.TrimPrefix(name, "@claudinite/")+"-0.0.0.tgz" {
				t.Errorf("%s: tarball %s holds %s", pkg, file, name)
			}
			got = append(got, name)
		}
		sort.Strings(got)
		channel := map[string]string{"@claudinite/cli": "stable", "@claudinite/cli-rc": "rc"}[pkg]
		var want []string
		for _, p := range Packages() {
			if p.Channel == channel && p.Name != "@claudinite/sdk" {
				want = append(want, p.Name)
			}
		}
		sort.Strings(want)
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("%s: tarballs %v, want %v", pkg, got, want)
		}
	}
}
