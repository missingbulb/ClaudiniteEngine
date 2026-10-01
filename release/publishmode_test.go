package release

import (
	"strings"
	"testing"
)

func TestPublishMode(t *testing.T) {
	reserved := `["0.0.0"]`
	notFound := `{"error": {"code": "E404", "summary": "Not Found"}}`
	cases := []struct {
		name   string
		in     ModeInput
		want   string
		notice string
	}{
		{"rc, real key, reserved", ModeInput{Channel: "rc", Signing: "release", NpmVersions: reserved}, "real", ""},
		{"rc, dev key", ModeInput{Channel: "rc", Signing: "dev", NpmVersions: reserved}, "dry-run", "development key"},
		{"rc, dry_run input", ModeInput{Channel: "rc", Signing: "release", DryRunInput: true, NpmVersions: reserved}, "dry-run", "dry_run"},
		{"rc, npm view 404", ModeInput{Channel: "rc", Signing: "release", NpmVersions: notFound}, "dry-run", "package not reserved; see #2"},
		{"rc, npm view empty", ModeInput{Channel: "rc", Signing: "release", NpmVersions: ""}, "dry-run", "package not reserved; see #2"},
		{"rc, npm view empty list", ModeInput{Channel: "rc", Signing: "release", NpmVersions: "[]"}, "dry-run", "package not reserved; see #2"},
		{"rc, npm view single string", ModeInput{Channel: "rc", Signing: "release", NpmVersions: `"0.0.0"`}, "real", ""},
		{"stable, stable test fails", ModeInput{Channel: "stable", Signing: "release", NpmVersions: reserved, StableTest: "fail"}, "refuse", "#5"},
		{"stable, stable test passes", ModeInput{Channel: "stable", Signing: "release", NpmVersions: reserved, StableTest: "pass"}, "real", ""},
		{"stable, stable test not run", ModeInput{Channel: "stable", Signing: "release", NpmVersions: reserved}, "refuse", "#5"},
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
		if c.notice == "" && got.Notice != "" {
			t.Errorf("%s: unexpected notice %q", c.name, got.Notice)
		}
	}
}
