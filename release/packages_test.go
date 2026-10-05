package release

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
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

func TestPackagesAreExactlyThe7(t *testing.T) {
	release := Publisher{Workflow: "release.yml", Environment: "release"}
	promote := Publisher{Workflow: "promote.yml", Environment: "promote", DistTag: true}
	want := map[string][]Publisher{
		"@claudinite/cli":              {release, promote},
		"@claudinite/cli-linux-x64":    {release},
		"@claudinite/cli-linux-arm64":  {release},
		"@claudinite/cli-darwin-x64":   {release},
		"@claudinite/cli-darwin-arm64": {release},
		"@claudinite/cli-windows-x64":  {release},
		"@claudinite/sdk":              {{Workflow: "promote.yml", Environment: "promote"}},
	}
	got := Packages()
	if len(got) != 7 {
		t.Fatalf("%d packages, want 7", len(got))
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
		same := len(p.Publishers) == len(w)
		for i := 0; same && i < len(w); i++ {
			same = p.Publishers[i] == w[i]
		}
		if !same {
			t.Errorf("%s: publishers %+v, want %+v", p.Name, p.Publishers, w)
		}
		if !ValidPackageName(p.Name) {
			t.Errorf("%s fails ValidPackageName", p.Name)
		}
	}
	for _, bad := range []string{"@claudinite/cli-win32-x64", "@claudinite/cli-rc", "@claudinite/cli-rc-linux-x64", "@evil/cli", "@claudinite/sdk-linux-x64x", "@claudinite/cli-windows-arm64"} {
		if ValidPackageName(bad) {
			t.Errorf("ValidPackageName(%q) = true", bad)
		}
	}
}

// Every release publishes the same six packages under one dist-tag.
func TestCLIPackages(t *testing.T) {
	got := strings.Join(CLIPackages(), " ")
	if got != "@claudinite/cli @claudinite/cli-linux-x64 @claudinite/cli-linux-arm64 @claudinite/cli-darwin-x64 @claudinite/cli-darwin-arm64 @claudinite/cli-windows-x64" {
		t.Errorf("CLIPackages() = %s", got)
	}
}

// A full release is the rc candidate on all five platforms; a staging
// build is linux-x64 alone, under its own tag.
func TestReleaseKinds(t *testing.T) {
	for kind, want := range map[string]string{
		"full":    "rc canary linux-x64 linux-arm64 darwin-x64 darwin-arm64 windows-x64",
		"staging": "staging staging linux-x64",
	} {
		k, err := ReleaseKindOf(kind)
		if got := k.Tag + " " + k.Channel + " " + strings.Join(k.Platforms, " "); err != nil || got != want {
			t.Errorf("ReleaseKindOf(%q) = %q, %v; want %q", kind, got, err, want)
		}
	}
	for _, bad := range []string{"", "latest", "rc", "stable"} {
		if k, err := ReleaseKindOf(bad); err == nil {
			t.Errorf("ReleaseKindOf(%q) = %+v, want an error", bad, k)
		}
	}
}
