package release

import (
	"os"
	"regexp"
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
			"update 1: opened #1 for 1.61001.2", "update 2: check world passes the bot's pin and refuses a person's",
			"update 3: landed 1.61001.2", "update 4: SessionStart runs 1.61001.2 with no download",
			"update 5: a hook naming an event 1.61001.3 does not answer: skipped: selftest failed (hooks)",
			"update 6: no PR: 1.61001.3 would break this repo", "update 7: skipped: main is not green (failure)",
			"update 8: held 1.61001.3 skipped", "update 8: revoked 1.61001.3 skipped", "update 8: one issue for the revoked pin 1.61001.2",
			"update 9: the old shape is one deprecation", "update 10: the scheduler filed #", "update 11: a second run the same day files nothing",
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

// The live-packs mode reads the real shelf; a sandbox that has no network
// sets CLAUDINITE_OFFLINE=1 and the mode stands aside before it needs a
// release, with a notice.
func TestRehearseLivePacksOffline(t *testing.T) {
	out, err := runScript(t, []string{"CLAUDINITE_OFFLINE=1", "DIST=" + t.TempDir()}, "release/rehearse.sh", "--mode", "live-packs")
	if err != nil {
		t.Fatalf("--mode live-packs offline: %v\n%s", err, out)
	}
	if !strings.Contains(out, "live-packs: skipped: CLAUDINITE_OFFLINE=1") {
		t.Errorf("--mode live-packs offline names no skip:\n%s", out)
	}
	// The default set stays the offline modes.
	raw, err := os.ReadFile("rehearse.sh")
	if err != nil {
		t.Fatal(err)
	}
	if m := regexp.MustCompile(`(?m)^modes="([^"]*)"$`).FindStringSubmatch(string(raw)); m == nil || strings.Contains(m[1], "live-packs") {
		t.Errorf("rehearse.sh's default modes include live-packs or are unreadable: %v", m)
	}
}
