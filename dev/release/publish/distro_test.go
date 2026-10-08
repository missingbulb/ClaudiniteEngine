package publish

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/ghrelease"
	"github.com/missingbulb/ClaudiniteEngine/dev/test/scripttest"
)

// A staging build goes to the repository staging-distro names, as the two
// tarballs a linux-x64 launcher fetches and the release.json waiters read.
func TestDistroDryRunUploadsTheLauncherTarballsAndReleaseJSON(t *testing.T) {
	t.Parallel()
	const ver = "1.61008.1"
	pin := "sha512-" + strings.Repeat("A", 86) + "=="
	commit := strings.Repeat("c", 40)
	dist := fakeTarballs(t, ver, "@claudinite/cli", "@claudinite/cli-linux-x64")
	out, err := scripttest.Run(t, []string{"DIST=" + dist}, "dev/release/publish/distro.sh",
		"--version", ver, "--integrity", pin, "--commit", commit, "--dry-run")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	raw, err := os.ReadFile(filepath.Join(scripttest.Root(t), "dev/release/publish/staging-distro"))
	if err != nil {
		t.Fatal(err)
	}
	repo := strings.TrimSpace(string(raw))
	lines := strings.SplitN(out, "\n", 2)
	want := "dry-run: gh release create v" + ver + " --repo " + repo + " --latest " +
		filepath.Join(dist, "tarballs", "cli-"+ver+".tgz") + " " + filepath.Join(dist, "tarballs", "cli-linux-x64-"+ver+".tgz") + " release.json"
	if lines[0] != want {
		t.Errorf("upload line\n%s\nwant\n%s", lines[0], want)
	}
	if l, err := ghrelease.ParseLatest([]byte(lines[1])); err != nil || l != (ghrelease.Latest{Version: ver, Manifest: pin, Commit: commit}) {
		t.Errorf("release.json %q reads as %+v, %v", lines[1], l, err)
	}
}

func TestDistroRefusesABuildWithoutItsPlatformTarball(t *testing.T) {
	t.Parallel()
	const ver = "1.61008.1"
	dist := fakeTarballs(t, ver, "@claudinite/cli")
	out, err := scripttest.Run(t, []string{"DIST=" + dist}, "dev/release/publish/distro.sh",
		"--version", ver, "--integrity", "sha512-"+strings.Repeat("A", 86)+"==", "--commit", strings.Repeat("c", 40), "--dry-run")
	if err == nil || !strings.Contains(out, "cli-linux-x64-"+ver+".tgz") {
		t.Errorf("err %v, output %s; want a refusal naming the platform tarball", err, out)
	}
}
