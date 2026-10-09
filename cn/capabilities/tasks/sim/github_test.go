package sim

import (
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/workitem"
)

// The fake holds the engine to the approved labels as the real port does, so
// every simulated flow that writes one off the list fails its test.
func TestTheFakeRefusesALabelOffTheApprovedList(t *testing.T) {
	g := NewGitHub(NewClock(time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)))
	n, err := g.CreateIssue("t", "b", []string{workitem.OriginPlanned})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.AddLabel(n, "made-up"); err == nil {
		t.Error("adding an unapproved label succeeded")
	}
	if _, err := g.CreateIssue("t", "b", []string{"made-up"}); err == nil {
		t.Error("filing an issue under an unapproved label succeeded")
	}
	if err := g.EnsureLabels([]workitem.Label{{Name: "made-up"}}); err == nil {
		t.Error("defining an unapproved label succeeded")
	}
}
