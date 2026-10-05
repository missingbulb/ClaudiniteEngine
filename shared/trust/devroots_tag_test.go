package trust

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// release/rehearse.sh lets a devroots rebuild stand for a release
// candidate, so the tag may change nothing but which roots are embedded
// and whether CLAUDINITE_LICENSE_API names the license server: only those
// files and their tests read it.
func TestOnlyTheRootsFilesReadTheDevrootsTag(t *testing.T) {
	constraint := regexp.MustCompile(`(?m)^//go:build .*\bdevroots\b`)
	var got []string
	err := filepath.WalkDir("../..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "testdata") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if constraint.Match(raw) {
			got = append(got, filepath.ToSlash(strings.TrimPrefix(path, ".."+string(filepath.Separator)+".."+string(filepath.Separator))))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	want := "shared/licenseapi/override_dev.go shared/licenseapi/override_dev_test.go shared/licenseapi/override_real.go shared/licenseapi/override_test.go shared/trust/roots_dev.go shared/trust/roots_dev_test.go shared/trust/roots_real.go shared/trust/roots_real_test.go"
	if strings.Join(got, " ") != want {
		t.Fatalf("files reading the devroots tag: %v, want %s", got, want)
	}
}
