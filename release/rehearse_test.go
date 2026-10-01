package release

import (
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

func TestRehearseModes(t *testing.T) {
	if version.Platform() != "linux-x64" {
		t.Skip("the fixture dist is built on linux-x64")
	}
	dist, _ := unsignedDist(t)
	for mode, want := range map[string][]string{
		"current": {"current: SessionStart fetched", "the old cache is untouched"},
		"stale":   {"stale: SessionStart halted", "Stop ran the cached"},
		"update": {
			"update 1: opened #1 for 1.2.0", "update 2: check world passes the bot's pin and refuses a person's",
			"update 3: landed 1.2.0", "update 4: SessionStart runs 1.2.0 with no download",
			"update 5: no PR: 1.3.0 would break this repo", "update 6: skipped: main is not green (failure)",
			"update 7: held 1.3.0 skipped", "update 7: revoked 1.3.0 skipped", "update 7: one issue for the revoked pin 1.2.0",
			"update 8: the old shape is one deprecation",
		},
	} {
		out, err := runScript(t, []string{"DIST=" + dist}, "release/rehearse.sh", "--mode", mode)
		if err != nil {
			t.Fatalf("--mode %s: %v\n%s", mode, err, out)
		}
		for _, w := range want {
			if !strings.Contains(out, w) {
				t.Errorf("--mode %s output lacks %q:\n%s", mode, w, out)
			}
		}
		if strings.Contains(out, "fresh: ") {
			t.Errorf("--mode %s also ran fresh", mode)
		}
	}
	if out, err := runScript(t, []string{"DIST=" + dist}, "release/rehearse.sh", "--mode", "sideways"); err == nil {
		t.Errorf("--mode sideways accepted:\n%s", out)
	}
}
