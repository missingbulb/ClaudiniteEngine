package release

import (
	"os"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/version"
)

// The hop job: the candidate's own updater moves a member off it to a
// second build of the same source, steps 1 to 4 of the rehearsal's update
// mode and nothing more.
func TestHop(t *testing.T) {
	t.Parallel()
	if version.Platform() != "linux-x64" {
		t.Skip("the fixture dist is built on linux-x64")
	}
	dist, _ := unsignedDist(t)
	out, err := runScript(t, []string{"DIST=" + dist}, "dev/release/hop.sh")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, w := range []string{"update 1: opened #1 for 1.61001.2", "update 3: landed 1.61001.2", "update 4: SessionStart runs 1.61001.2", "hop: 1.61001.1 updated a member to 1.61001.2"} {
		if !strings.Contains(out, w) {
			t.Errorf("output lacks %q:\n%s", w, out)
		}
	}
	for _, not := range []string{"update 5:", "rehearsal_break", "fresh: ", "current: "} {
		if strings.Contains(out, not) {
			t.Errorf("output has %q:\n%s", not, out)
		}
	}
	if _, err := os.Stat(dist + "/manifest.sig.json"); err == nil {
		t.Error("hop signed the candidate's own dist")
	}
	if out, err := runScript(t, []string{"DIST=" + dist, "UPDATE_STEPS=5"}, "dev/release/rehearse.sh", "--mode", "update"); err == nil || !strings.Contains(out, "UPDATE_STEPS") {
		t.Errorf("UPDATE_STEPS=5 accepted:\n%s", out)
	}
}
