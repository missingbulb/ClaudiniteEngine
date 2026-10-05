package release

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

// golden compares got with testdata/<name>, rewriting it under -update.
func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("%s differs from the golden file (go test -update to accept):\n%s", name, got)
	}
}

func TestPackagesAreExactlyThe13(t *testing.T) {
	want := map[string][2]string{
		"@claudinite/cli":                 {"promote.yml", "promote"},
		"@claudinite/cli-linux-x64":       {"promote.yml", "promote"},
		"@claudinite/cli-linux-arm64":     {"promote.yml", "promote"},
		"@claudinite/cli-darwin-x64":      {"promote.yml", "promote"},
		"@claudinite/cli-darwin-arm64":    {"promote.yml", "promote"},
		"@claudinite/cli-windows-x64":     {"promote.yml", "promote"},
		"@claudinite/sdk":                 {"promote.yml", "promote"},
		"@claudinite/cli-rc":              {"release.yml", "release"},
		"@claudinite/cli-rc-linux-x64":    {"release.yml", "release"},
		"@claudinite/cli-rc-linux-arm64":  {"release.yml", "release"},
		"@claudinite/cli-rc-darwin-x64":   {"release.yml", "release"},
		"@claudinite/cli-rc-darwin-arm64": {"release.yml", "release"},
		"@claudinite/cli-rc-windows-x64":  {"release.yml", "release"},
	}
	got := Packages()
	if len(got) != 13 {
		t.Fatalf("%d packages, want 13", len(got))
	}
	seen := map[string]bool{}
	for _, p := range got {
		w, ok := want[p.Name]
		if !ok {
			t.Errorf("unexpected package %s", p.Name)
			continue
		}
		if seen[p.Name] {
			t.Errorf("%s listed twice", p.Name)
		}
		seen[p.Name] = true
		if p.Workflow != w[0] || p.Environment != w[1] {
			t.Errorf("%s: publisher %s/%s, want %s/%s", p.Name, p.Workflow, p.Environment, w[0], w[1])
		}
		if !ValidPackageName(p.Name) {
			t.Errorf("%s fails ValidPackageName", p.Name)
		}
	}
	for _, bad := range []string{"@claudinite/cli-win32-x64", "@claudinite/cli-rc-linux-x86", "@evil/cli", "@claudinite/sdk-linux-x64x", "@claudinite/cli-rc-"} {
		if ValidPackageName(bad) {
			t.Errorf("ValidPackageName(%q) = true", bad)
		}
	}
}
