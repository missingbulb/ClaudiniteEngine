package ghrelease

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The launcher and the updater fetch the same release assets; their URL
// spellings are one protocol, so this evaluates the launcher's own
// assignments.
func TestTarballURLsAreTheLaunchers(t *testing.T) {
	raw, err := os.ReadFile("../launcher/launch")
	if err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{"github": "https://g.example", "releases": "acme/acme-distro", "name": "cli-rc", "version": "1.60930.1", "platform": "linux-arm64"}
	eval := func(name string) string {
		var m []string
		for _, a := range regexp.MustCompile(`(?m)^\s*`+name+`=(\S+)$`).FindAllStringSubmatch(string(raw), -1) {
			if strings.HasPrefix(a[1], "$github/") {
				m = a
			}
		}
		if m == nil {
			t.Fatalf("launcher assigns no %s from $github", name)
		}
		return regexp.MustCompile(`\$\{?([a-z_]+)\}?`).ReplaceAllStringFunc(m[1], func(v string) string {
			return vars[strings.Trim(v, "${}")]
		})
	}
	if got, want := TarballURL("https://g.example", "acme/acme-distro", "@claudinite/cli-rc", "1.60930.1"), eval("manifest_url"); got != want {
		t.Errorf("TarballURL %s, launcher %s", got, want)
	}
	if got, want := PlatformTarballURL("https://g.example", "acme/acme-distro", "@claudinite/cli-rc", "linux-arm64", "1.60930.1"), eval("binary_url"); got != want {
		t.Errorf("PlatformTarballURL %s, launcher %s", got, want)
	}
}

// The URLs are the launcher's: tag v<version>, assets named as npm's
// tarballs.
func TestURLs(t *testing.T) {
	for got, want := range map[string]string{
		TarballURL("", "acme/acme-distro", "@claudinite/cli", "1.2.3"):                      "https://github.com/acme/acme-distro/releases/download/v1.2.3/cli-1.2.3.tgz",
		PlatformTarballURL("", "acme/acme-distro", "@claudinite/cli", "linux-x64", "1.2.3"): "https://github.com/acme/acme-distro/releases/download/v1.2.3/cli-linux-x64-1.2.3.tgz",
		LatestURL("https://h", "acme/acme-distro"):                                          "https://h/acme/acme-distro/releases/latest/download/release.json",
	} {
		if got != want {
			t.Errorf("%s, want %s", got, want)
		}
	}
}

func TestParseLatest(t *testing.T) {
	pin := "sha512-" + strings.Repeat("A", 86) + "=="
	commit := strings.Repeat("a", 40)
	good := Latest{Version: "1.2.3", Manifest: pin, Commit: commit}
	raw, err := FormatLatest(good)
	if err != nil {
		t.Fatal(err)
	}
	if l, err := ParseLatest(raw); err != nil || l != good {
		t.Errorf("%s reads back as %+v, %v", raw, l, err)
	}
	for _, bad := range []Latest{
		{Version: "1.2", Manifest: pin, Commit: commit},
		{Version: "1.2.3", Manifest: "sha512-x", Commit: commit},
		{Version: "1.2.3", Manifest: pin, Commit: "abc"},
	} {
		if _, err := FormatLatest(bad); err == nil {
			t.Errorf("%+v formatted", bad)
		}
	}
	if _, err := ParseLatest([]byte("not json")); err == nil {
		t.Error("not json parsed")
	}
}
