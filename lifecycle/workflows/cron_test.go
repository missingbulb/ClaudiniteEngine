package workflows

import (
	"strings"
	"testing"
)

// The values the Node engine's hashedCron gives at 057841ac.
func TestTheSchedulerCronIsTheRepoNamesHash(t *testing.T) {
	for name, want := range map[string]string{"missingbulb/hello": "39 6,18 * * *", "Acme/Widgets": "34 10,22 * * *", "o/r": "20 5,17 * * *"} {
		if got := SchedulerCron(name); got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
	if SchedulerCron("ACME/widgets") != SchedulerCron("acme/WIDGETS") {
		t.Error("the name is read case-blind")
	}
}

func TestARepoGetsItsOwnCronAndNothingElseChanges(t *testing.T) {
	mine := ForRepo("o/r")
	base := Templates()
	for _, n := range Names {
		if n == "claudinite-scheduler.yml" {
			continue
		}
		if string(mine[n]) != string(base[n]) {
			t.Errorf("%s changed", n)
		}
	}
	s := string(mine["claudinite-scheduler.yml"])
	if strings.Contains(s, CronPlaceholder) || !strings.Contains(s, `cron: "20 5,17 * * *"`) ||
		strings.Replace(s, "20 5,17 * * *", CronPlaceholder, 1) != string(base["claudinite-scheduler.yml"]) {
		t.Error(s)
	}
}
