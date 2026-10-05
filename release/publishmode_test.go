package release

import (
	"strings"
	"testing"
)

func TestPublishMode(t *testing.T) {
	t.Parallel()
	reserved := `["0.0.0"]`
	notFound := `{"error": {"code": "E404", "summary": "Not Found"}}`
	cases := []struct {
		name   string
		in     ModeInput
		want   string
		notice string
	}{
		{"rc, real key, reserved", ModeInput{Channel: "rc", Signing: "release", NpmVersions: reserved}, "real", ""},
		{"rc, the retired dev signing", ModeInput{Channel: "rc", Signing: "dev", NpmVersions: reserved}, "refuse", "signing"},
		{"rc, dry_run input", ModeInput{Channel: "rc", Signing: "release", DryRunInput: true, NpmVersions: reserved}, "dry-run", "dry_run"},
		{"rc, npm view 404", ModeInput{Channel: "rc", Signing: "release", NpmVersions: notFound}, "dry-run", "package not reserved; see #2"},
		{"rc, npm view 500", ModeInput{Channel: "rc", Signing: "release", NpmVersions: `{"error": {"code": "E500", "summary": "500 Internal Server Error - GET https://registry.npmjs.org/@claudinite%2fcli-rc"}}`}, "dry-run", "E500"},
		{"rc, npm view network failure", ModeInput{Channel: "rc", Signing: "release", NpmVersions: `{"error": {"code": "ECONNRESET", "summary": "socket hang up"}}`}, "dry-run", "ECONNRESET"},
		{"rc, npm view empty", ModeInput{Channel: "rc", Signing: "release", NpmVersions: ""}, "dry-run", "package not reserved; see #2"},
		{"rc, npm view empty list", ModeInput{Channel: "rc", Signing: "release", NpmVersions: "[]"}, "dry-run", "package not reserved; see #2"},
		{"rc, npm view single string", ModeInput{Channel: "rc", Signing: "release", NpmVersions: `"0.0.0"`}, "real", ""},
		{"stable, stable test fails", ModeInput{Channel: "stable", Signing: "release", NpmVersions: reserved, StableTest: "fail"}, "refuse", "development roots"},
		{"stable, stable test passes", ModeInput{Channel: "stable", Signing: "release", NpmVersions: reserved, StableTest: "pass"}, "real", ""},
		{"stable, stable test not run", ModeInput{Channel: "stable", Signing: "release", NpmVersions: reserved}, "refuse", "development roots"},
		{"stable, not reserved", ModeInput{Channel: "stable", Signing: "release", NpmVersions: notFound, StableTest: "pass"}, "dry-run", "see #2"},
		{"unknown channel", ModeInput{Channel: "beta", Signing: "release", NpmVersions: reserved}, "refuse", "channel"},
		{"unknown signing", ModeInput{Channel: "rc", Signing: "", NpmVersions: reserved}, "refuse", "signing"},
	}
	for _, c := range cases {
		got := PublishMode(c.in)
		if got.Name != c.want {
			t.Errorf("%s: mode %s, want %s (%s)", c.name, got.Name, c.want, got.Notice)
		}
		if c.notice != "" && !strings.Contains(got.Notice, c.notice) {
			t.Errorf("%s: notice %q lacks %q", c.name, got.Notice, c.notice)
		}
		if strings.Contains(c.name, "500") || strings.Contains(c.name, "network") {
			if strings.Contains(got.Notice, "#2") {
				t.Errorf("%s: notice %q blames the reservation", c.name, got.Notice)
			}
		}
		if c.notice == "" && got.Notice != "" {
			t.Errorf("%s: unexpected notice %q", c.name, got.Notice)
		}
	}
}
