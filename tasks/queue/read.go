// Package queue reads and moves the work-item queue: the open listing, the
// pick order both stages ask, the status transition every stage writes,
// the holder's heartbeat, when a blocked item may run, and the records a
// run leaves. The list comes from the issues list API, never the search
// index: search is eventually consistent, and a list that misses a
// just-created item is how a second standing item gets minted.
package queue

import (
	"fmt"

	"github.com/missingbulb/ClaudiniteEngine/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/tasks/world"
)

// ListOpen is every open work item, oldest first. A page that cannot be
// read is an error, never a shorter queue.
func ListOpen(gh world.Issues) ([]workitem.Issue, error) {
	return list(gh, "open", 0, func(workitem.Issue) bool { return true })
}

// DonePages is how far back the closed half is read: the superseded-park
// rule only asks whether a task ran clean since a park.
const DonePages = 2

// ListDone is every work item that converged done, most recently updated
// first, over the newest DonePages pages.
func ListDone(gh world.Issues) ([]workitem.Issue, error) {
	return list(gh, "closed", DonePages, func(i workitem.Issue) bool { return i.Status() == workitem.StatusDone })
}

func list(gh world.Issues, state string, pages int, keep func(workitem.Issue) bool) ([]workitem.Issue, error) {
	out := []workitem.Issue{}
	for page := 1; pages == 0 || page <= pages; page++ {
		got, err := gh.IssuesPage(state, page)
		if err != nil {
			return out, fmt.Errorf("listing %s issues, page %d: %w", state, page, err)
		}
		for _, i := range got {
			if i.PullRequest || !i.IsQueueItem() || !keep(i.Issue) {
				continue
			}
			out = append(out, i.Issue)
		}
		if len(got) < world.PageSize {
			break
		}
	}
	return out, nil
}
