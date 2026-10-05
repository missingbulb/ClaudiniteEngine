package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
)

// Verify accuracy, both directions: the verdict names what actually
// stopped the update, never the candidate's verify when it was main's CI,
// and never main's CI when it was verify.

// A red main stops the update before the candidate is fetched, so a
// candidate whose verify would pass is never run and never blamed.
func TestARedMainIsNotBlamedOnVerify(t *testing.T) {
	t.Parallel()
	w := newWorld(t, settings.YAML)
	ran := filepath.Join(t.TempDir(), "verify-ran")
	w.publish(t, v2, relOpts{binary: cnScript(v2, "touch '"+ran+"'; exit 0", "")})
	w.mainRun(t, "failure")
	v, err := Engine(w.deps(t), Options{})
	if err != nil || v != "skipped: main is not green (failure)" {
		t.Fatalf("%q %v", v, err)
	}
	if _, err := os.Stat(ran); err == nil {
		t.Error("the candidate's verify ran on a red main")
	}
	if strings.Contains(w.out.String(), "break") {
		t.Errorf("a red main printed verify findings:\n%s", w.out)
	}
}

// A verify break on a green main stops the update with the break printed
// and no pull request, and the verdict blames the candidate, not CI.
func TestAVerifyBreakOnAGreenMainIsNotBlamedOnCI(t *testing.T) {
	t.Parallel()
	w := newWorld(t, settings.YAML)
	w.publish(t, v2, relOpts{binary: cnScript(v2, `echo "break member-workflows .github/workflows/claudinite-executor.yml: missing while the scheduler runs"; exit 1`, "")})
	v, err := Engine(w.deps(t), Options{})
	if err != nil || v != "no PR: "+v2+" would break this repo" {
		t.Fatalf("%q %v", v, err)
	}
	if strings.Contains(v, "main is not green") {
		t.Error("the verdict blames CI")
	}
	if !strings.Contains(w.out.String(), "break member-workflows .github/workflows/claudinite-executor.yml") {
		t.Errorf("the break that stopped it was not printed:\n%s", w.out)
	}
	if len(w.hub.called("create-pull")) != 0 {
		t.Error("opened a PR")
	}
}
