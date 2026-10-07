package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/npmreg"
)

func pipeline(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return out.String(), errb.String(), code
}

func TestNamesAreTheCLIPackages(t *testing.T) {
	out, _, code := pipeline(t, "names")
	if code != 0 || strings.Fields(out)[0] != "@claudinite/cli" || len(strings.Fields(out)) != 6 || strings.Contains(out, "sdk") || strings.Contains(out, "cli-rc") {
		t.Errorf("exit %d:\n%s", code, out)
	}
}

// release.yml reads its kind's tag and platforms as $GITHUB_OUTPUT lines.
func TestReleaseKind(t *testing.T) {
	for kind, want := range map[string]string{
		"full":    "tag=rc\nchannel=canary\nplatforms=linux-x64 linux-arm64 darwin-x64 darwin-arm64 windows-x64\n",
		"staging": "tag=staging\nchannel=staging\nplatforms=linux-x64\n",
	} {
		if out, e, code := pipeline(t, "release-kind", "--kind", kind); code != 0 || out != want {
			t.Errorf("%s: exit %d %q %s, want %q", kind, code, out, e, want)
		}
	}
	if out, _, code := pipeline(t, "release-kind", "--kind", "stable"); code == 0 || out != "" {
		t.Errorf("stable: exit %d %q", code, out)
	}
}

func TestPublishModeTakesTheTag(t *testing.T) {
	versions := filepath.Join(t.TempDir(), "versions.json")
	if err := os.WriteFile(versions, []byte(`["0.0.0"]`), 0o644); err != nil {
		t.Fatal(err)
	}
	for tag, want := range map[string]string{"rc": "mode=real\n", "staging": "mode=real\n", "latest": "mode=refuse\n"} {
		if out, e, code := pipeline(t, "publish-mode", "--tag", tag, "--signing", "release", "--dry-run", "false", "--npm-versions", versions); code != 0 || out != want {
			t.Errorf("%s: exit %d %q %s, want %q", tag, code, out, e, want)
		}
	}
}

func TestUnpublishCommandsReadsTheDistTags(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "@claudinite"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, _, _ := pipeline(t, "names")
	for _, n := range strings.Fields(out) {
		if err := os.WriteFile(filepath.Join(dir, n+".json"), []byte(`["1.61005.1","1.61005.2"]`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tags := filepath.Join(t.TempDir(), "tags.json")
	for latest, wantCode := range map[string]int{"1.61005.1": 0, "1.61005.2": 1} {
		if err := os.WriteFile(tags, []byte(`{"latest":"`+latest+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		out, e, code := pipeline(t, "unpublish-commands", "--version", "1.61005.2", "--versions-dir", dir, "--dist-tags", tags)
		if code != wantCode || (code == 0) != (strings.Count(out, "npm unpublish") == 6) {
			t.Errorf("latest %s: exit %d, want %d\n%s%s", latest, code, wantCode, out, e)
		}
	}
	if err := os.WriteFile(tags, []byte(`{"latest":"1.61005.1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out, e, code := pipeline(t, "deprecate-commands", "--action", "hold", "--version", "1.61005.2", "--reason", "x", "--versions-dir", dir)
	if code != 0 || strings.Count(out, "npm deprecate") != 6 {
		t.Errorf("deprecate: exit %d\n%s%s", code, out, e)
	}
}

// npm-holds waits as long as cn update does for npm to serve a release,
// and needs the repository and channel its way-on message names.
func TestNPMHoldsWaitsTheServeWait(t *testing.T) {
	in, ok := holdsFlags([]string{"--dist", "dist", "--version", "1.61005.9", "--channel", "canary", "--repo", "o/r"})
	if !ok || in.Timeout != npmreg.ServeWait || in.Channel != "canary" || in.Repo != "o/r" {
		t.Errorf("ok %v, timeout %v, channel %q; want npmreg.ServeWait (%v) and canary", ok, in.Timeout, in.Channel, npmreg.ServeWait)
	}
	if _, e, code := pipeline(t, "npm-holds", "--dist", "dist", "--version", "1.61005.9", "--repo", "o/r"); code != 2 || !strings.Contains(e, "usage") {
		t.Errorf("no --channel: exit %d %s", code, e)
	}
}
