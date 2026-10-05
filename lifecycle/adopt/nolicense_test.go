package adopt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A repo needs no license: init asks for no key, writes no license block
// and hands over neither an App install nor a plan.
func TestInitAsksForNoLicense(t *testing.T) {
	repo := t.TempDir()
	in, out := input(t, repo, "hello")
	if err := Init(in); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	raw, _ := os.ReadFile(filepath.Join(repo, ".claudinite/settings.yaml"))
	if strings.Contains(string(raw), "license") {
		t.Errorf("a license block:\n%s", raw)
	}
	for _, s := range []string{"plan", "Claudinite GitHub App", "license"} {
		if strings.Contains(strings.ToLower(out.String()), strings.ToLower(s)) {
			t.Errorf("init says %q:\n%s", s, out)
		}
	}
}
