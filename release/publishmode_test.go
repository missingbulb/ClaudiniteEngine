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
		{"rc, real key, reserved", ModeInput{Tag: "rc", Signing: "release", NpmVersions: reserved}, "real", ""},
		{"rc, the retired dev signing", ModeInput{Tag: "rc", Signing: "dev", NpmVersions: reserved}, "refuse", "signing"},
		{"rc, dry_run input", ModeInput{Tag: "rc", Signing: "release", DryRunInput: true, NpmVersions: reserved}, "dry-run", "dry_run"},
		{"rc, npm view 404", ModeInput{Tag: "rc", Signing: "release", NpmVersions: notFound}, "dry-run", "package not reserved; see #2"},
		{"rc, npm view 500", ModeInput{Tag: "rc", Signing: "release", NpmVersions: `{"error": {"code": "E500", "summary": "500 Internal Server Error - GET https://registry.npmjs.org/@claudinite%2fcli"}}`}, "dry-run", "E500"},
		{"rc, npm view network failure", ModeInput{Tag: "rc", Signing: "release", NpmVersions: `{"error": {"code": "ECONNRESET", "summary": "socket hang up"}}`}, "dry-run", "ECONNRESET"},
		{"rc, npm view empty", ModeInput{Tag: "rc", Signing: "release", NpmVersions: ""}, "dry-run", "package not reserved; see #2"},
		{"rc, npm view empty list", ModeInput{Tag: "rc", Signing: "release", NpmVersions: "[]"}, "dry-run", "package not reserved; see #2"},
		{"rc, npm view single string", ModeInput{Tag: "rc", Signing: "release", NpmVersions: `"0.0.0"`}, "real", ""},
		{"staging, real key, reserved", ModeInput{Tag: "staging", Signing: "release", NpmVersions: reserved}, "real", ""},
		{"staging, dry_run input", ModeInput{Tag: "staging", Signing: "release", DryRunInput: true, NpmVersions: reserved}, "dry-run", "dry_run"},
		{"latest is moved by promotion, never published", ModeInput{Tag: "latest", Signing: "release", NpmVersions: reserved}, "refuse", "promote.yml"},
		{"unknown tag", ModeInput{Tag: "beta", Signing: "release", NpmVersions: reserved}, "refuse", "tag"},
		{"no tag", ModeInput{Signing: "release", NpmVersions: reserved}, "refuse", "tag"},
		{"unknown signing", ModeInput{Tag: "rc", Signing: "", NpmVersions: reserved}, "refuse", "signing"},
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
