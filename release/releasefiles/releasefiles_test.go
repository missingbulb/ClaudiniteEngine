package releasefiles

import (
	"regexp"
	"strings"
	"testing"
)

func TestFormatRoundTripsWithOneLinePerBinary(t *testing.T) {
	m := Manifest{V: 1, Version: "1.1.0", BuiltAt: "2026-10-01T00:00:00Z", Commit: "abc", GoVersion: "go1.24",
		Binaries: map[string]Binary{
			"windows-x64": {File: "cn.exe", SHA256: strings.Repeat("a", 64), Size: 3},
			"linux-x64":   {File: "cn", SHA256: strings.Repeat("b", 64), Size: 4},
		}}
	raw := Format(m)
	got, err := ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Binaries["linux-x64"] != m.Binaries["linux-x64"] || got.Binaries["windows-x64"] != m.Binaries["windows-x64"] {
		t.Fatalf("round trip lost entries: %+v", got)
	}
	line := regexp.MustCompile(`(?m)^    "linux-x64": \{"file": "cn", "sha256": "b{64}", "size": 4\},$`)
	if !line.Match(raw) {
		t.Fatalf("linux-x64 entry is not on one line:\n%s", raw)
	}
	if strings.Index(string(raw), "linux-x64") > strings.Index(string(raw), "windows-x64") {
		t.Fatal("platforms out of release order")
	}
}
