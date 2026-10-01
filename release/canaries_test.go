package release

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"
)

// The promotion gate's hourly schedule lands with the first registered
// canary: with none it would file a verdict every hour that nothing reads.
func TestPromoteScheduleFollowsTheCanaryList(t *testing.T) {
	raw, err := os.ReadFile("canaries.json")
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		ScheduleWhenNonEmpty bool              `json:"scheduleWhenNonEmpty"`
		Canaries             []json.RawMessage `json:"canaries"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	if !c.ScheduleWhenNonEmpty {
		t.Fatal("canaries.json must carry scheduleWhenNonEmpty: true")
	}
	wf, err := os.ReadFile("../.github/workflows/promote.yml")
	if err != nil {
		t.Fatal(err)
	}
	scheduled := regexp.MustCompile(`(?m)^  schedule:`).Match(wf)
	if len(c.Canaries) > 0 && !scheduled {
		t.Error("canaries are registered but promote.yml has no schedule:")
	}
	if len(c.Canaries) == 0 && scheduled {
		t.Error("promote.yml is scheduled with no canary registered")
	}
}
