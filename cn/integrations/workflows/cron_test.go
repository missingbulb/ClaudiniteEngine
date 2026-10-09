package workflows

import (
	"errors"
	"strings"
	"testing"
)

// withCron is the scheduler template carrying cron.
func withCron(cron string) string {
	return strings.Replace(string(Templates()["claudinite-scheduler.yml"]), CronPlaceholder, cron, 1)
}

// A member's own valid cron is its choice and survives, a single daily
// tick included: GoogleCalendarEventCreator#1441's Node-era 24 4, and
// MissingBulbWebsite's owner-chosen 39 4 that a Node move rewrote to its
// hash. It needs no name, and the placeholder never stands in for it.
func TestExpectedKeepsAMembersOwnCron(t *testing.T) {
	for _, cron := range []string{"24 4 * * *", "39 4 * * *", "0 3 * * 1-5", "*/30 * * * *", "26 4,17 * * *", "20 5,17 * * *"} {
		for _, name := range []string{"missingbulb/GoogleCalendarEventCreator", ""} {
			got, err := Expected("claudinite-scheduler.yml", []byte(withCron(cron)), name)
			if err != nil || string(got) != withCron(cron) {
				t.Errorf("%q (name %q): %v\n%s", cron, name, err, got)
			}
		}
	}
}

// A scheduler with no cron of its own, or none at all, takes the repo's
// hashed cron, never the template's placeholder; with no name to hash,
// Expected refuses.
func TestExpectedGivesACronlessSchedulerTheReposOwn(t *testing.T) {
	const gcec = "missingbulb/GoogleCalendarEventCreator"
	cronless := strings.Replace(withCron("x"), `    - cron: "x"`+"\n", "", 1)
	for name, have := range map[string][]byte{"no cron line": []byte(cronless), "a malformed cron": []byte(withCron("every day")), "no file": nil} {
		got, err := Expected("claudinite-scheduler.yml", have, gcec)
		if err != nil || string(got) != string(ForRepo(gcec)["claudinite-scheduler.yml"]) || !strings.Contains(string(got), `cron: "24 4,16 * * *"`) {
			t.Errorf("%s: %v\n%s", name, err, got)
		}
		if got, err := Expected("claudinite-scheduler.yml", have, ""); !errors.Is(err, ErrNoName) || strings.Contains(string(got), CronPlaceholder) {
			t.Errorf("%s without a name: %v\n%s", name, err, got)
		}
	}
}

// A scheduler whose owner took its schedule trigger out runs only on
// dispatch, and an update keeps it that way: TLDR#658 turned its cron off
// and TLDR#671's update put the hash back. The rest still follows the
// template, and no name is needed.
func TestExpectedKeepsAMembersScheduleOff(t *testing.T) {
	off := func(s string) string {
		return strings.Replace(s, "  schedule:\n    - cron: \""+CronPlaceholder+"\"\n", "", 1)
	}
	want := off(string(Templates()["claudinite-scheduler.yml"]))
	if strings.Contains(want, "schedule:") {
		t.Fatal("the template's schedule trigger is not where this test removes it")
	}
	stale := strings.Replace(want, "name: claudinite-scheduler", "name: Claudinite scheduler", 1)
	for _, name := range []string{"missingbulb/TLDR", ""} {
		for _, have := range []string{want, stale} {
			got, err := Expected("claudinite-scheduler.yml", []byte(have), name)
			if err != nil || string(got) != want {
				t.Errorf("name %q: %v\n%s", name, err, got)
			}
		}
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
