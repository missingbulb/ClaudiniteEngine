package main

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"
)

func readCanaries(t *testing.T) (bool, []Canary) {
	t.Helper()
	raw, err := os.ReadFile("../canaries.json")
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		ScheduleWhenNonEmpty bool     `json:"scheduleWhenNonEmpty"`
		Canaries             []Canary `json:"canaries"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c.ScheduleWhenNonEmpty, c.Canaries
}

// The promotion gate's hourly schedule lands with the first canary
// workflow the gate reads: with none it would file a verdict every hour
// that nothing reads.
func TestPromoteScheduleFollowsTheCanaryList(t *testing.T) {
	schedule, canaries := readCanaries(t)
	if !schedule {
		t.Fatal("canaries.json must carry scheduleWhenNonEmpty: true")
	}
	wf, err := os.ReadFile("../../../../.github/workflows/promote.yml")
	if err != nil {
		t.Fatal(err)
	}
	scheduled := regexp.MustCompile(`(?m)^  schedule:`).Match(wf)
	if Watched(canaries) && !scheduled {
		t.Error("a canary names a workflow but promote.yml has no schedule:")
	}
	if !Watched(canaries) && scheduled {
		t.Error("promote.yml is scheduled with no canary workflow named")
	}
}

// The three canaries are registered, so the gate's no-canaries verdict
// cannot hide a forgotten registration; none names a workflow until the
// canary App (#21) can read their runs.
func TestTheCanariesAreRegisteredWithoutWorkflows(t *testing.T) {
	_, canaries := readCanaries(t)
	want := map[string]string{"lagging": "missingbulb/ClaudiniteCanaryLagging", "fresh": "missingbulb/ClaudiniteCanaryFresh", "sandbox": "missingbulb/ClaudiniteSandbox"}
	if len(canaries) != len(want) {
		t.Fatalf("canaries %v", canaries)
	}
	for _, c := range canaries {
		if want[c.Name] != c.Repo {
			t.Errorf("canary %s is %s, want %s", c.Name, c.Repo, want[c.Name])
		}
		if c.Workflows == nil || len(c.Workflows) != 0 {
			t.Errorf("canary %s names workflows %v before #21", c.Name, c.Workflows)
		}
	}
}
