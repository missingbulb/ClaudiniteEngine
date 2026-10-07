package githubapi

import (
	"testing"
)

func TestParseRemote(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/acme/member.git":      "acme/member",
		"https://github.com/acme/member":          "acme/member",
		"https://x-access-token:t@github.com/a/b": "a/b",
		"git@github.com:acme/member.git":          "acme/member",
		"ssh://git@github.com/acme/member.git":    "acme/member",
		"git://github.com/acme/member.git":        "acme/member",
		"https://gitlab.com/acme/member.git":      "",
		"/tmp/origin.git":                         "",
		"https://github.com/acme":                 "",
		"https://github.com/acme/member/extra":    "",
	} {
		got, ok := ParseRemote(in)
		if got != want || ok != (want != "") {
			t.Errorf("%s: %q %v, want %q", in, got, ok, want)
		}
	}
}
