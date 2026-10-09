package addpacks

import (
	"fmt"

	"github.com/missingbulb/ClaudiniteEngine/cn/fleet"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/workitem"
)

// markLabel is the queue's own definition of the mark, never a second
// copy of its colour and description.
func markLabel() workitem.Label {
	for _, l := range workitem.QueueLabels {
		if l.Name == Mark {
			return l
		}
	}
	panic("workitem.QueueLabels does not define " + Mark)
}

// EnsureMark creates the mark in repo before it is applied: applying a
// label GitHub does not know is refused, and a refused mark is a work
// list nobody runs.
func EnsureMark(gh fleet.GH, repo string) error {
	l := markLabel()
	return fleet.EnsureLabel(gh, repo, Mark, l.Color, l.Description)
}

// MarkedBody is the body to write for a work list: the targeting fields,
// the list, and the machine block the member already wrote into existing,
// re-attached unchanged.
func MarkedBody(body string, existing *fleet.Issue, blockedBy int) string {
	human := WithTargeting(body, blockedBy)
	if existing != nil {
		if block, ok := workitem.MachineBlockOf(existing.Body); ok {
			return workitem.WithMachineBlock(human, block)
		}
	}
	return human
}

// Remark brings an existing work-list issue to the marked shape: the mark
// applied, the body rewritten only where it differs, and every spelling
// of the standing status cleared when it did, which is the re-ask. It
// reports whether the body was written.
func Remark(gh fleet.GH, repo string, existing fleet.Issue, body string) (bool, error) {
	if err := EnsureMark(gh, repo); err != nil {
		return false, err
	}
	n := existing.Number
	if !contains(existing.Labels, Mark) {
		if _, err := fleet.Expect(gh, "POST", fmt.Sprintf("/repos/%s/issues/%d/labels", repo, n), map[string][]string{"labels": {Mark}}, 200); err != nil {
			return false, err
		}
	}
	if existing.Body == body {
		return false, nil
	}
	if _, err := fleet.Expect(gh, "PATCH", fmt.Sprintf("/repos/%s/issues/%d", repo, n), map[string]string{"body": body}, 200); err != nil {
		return false, err
	}
	if status := workitem.StatusOf(existing.Labels); status != "" {
		for _, s := range workitem.SpellingsOf(status) {
			// A spelling the issue does not wear answers 404; the
			// clear is every spelling, whichever one it wears.
			if _, err := gh("DELETE", fmt.Sprintf("/repos/%s/issues/%d/labels/%s", repo, n, fleet.EncodeURIComponent(s)), nil); err != nil {
				return true, err
			}
		}
	}
	return true, nil
}

// OtherOpenWorkList is the number of the member's open work list titled
// other than title, 0 for none: what a newly placed list waits on, so two
// lists in one member never put two sessions on one declaration.
func OtherOpenWorkList(open []fleet.Issue, title string) int {
	for _, i := range open {
		if i.Title != title {
			return i.Number
		}
	}
	return 0
}
