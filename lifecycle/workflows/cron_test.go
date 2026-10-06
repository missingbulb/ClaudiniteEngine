package workflows

import (
	"errors"
	"strings"
	"testing"
)

// GoogleCalendarEventCreator#1441: a cron the hash did not write (the Node
// engine's single tick) becomes the repo's own hashed cron, never the
// template's placeholder; with no name to hash, Expected refuses.
func TestExpectedGivesAnUnhashedCronTheReposOwn(t *testing.T) {
	const gcec = "missingbulb/GoogleCalendarEventCreator"
	have := strings.Replace(string(Templates()["claudinite-scheduler.yml"]), CronPlaceholder, "24 4 * * *", 1)
	got, err := Expected("claudinite-scheduler.yml", []byte(have), gcec)
	if err != nil || string(got) != string(ForRepo(gcec)["claudinite-scheduler.yml"]) || !strings.Contains(string(got), `cron: "24 4,16 * * *"`) {
		t.Errorf("%v\n%s", err, got)
	}
	for name, h := range map[string][]byte{"an unhashed cron": []byte(have), "no file": nil} {
		if got, err := Expected("claudinite-scheduler.yml", h, ""); !errors.Is(err, ErrNoName) || strings.Contains(string(got), CronPlaceholder) {
			t.Errorf("%s without a name: %v\n%s", name, err, got)
		}
	}
	hashed := string(ForRepo("o/r")["claudinite-scheduler.yml"])
	if got, err := Expected("claudinite-scheduler.yml", []byte(hashed), ""); err != nil || string(got) != hashed {
		t.Errorf("a hashed cron needs no name: %v\n%s", err, got)
	}
}

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
