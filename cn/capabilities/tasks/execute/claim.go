package execute

import (
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/queue"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/world"
)

// ClaimComment is the lease's comment: who claimed, when, and the run.
// Executor identity is an unbounded set and so never a label.
func ClaimComment(executor, runURL, at string) string {
	s := workitem.ClaimMarker + "\nClaimed by executor `" + executor + "` at " + at + "."
	if runURL != "" {
		s += "\n\nRun: " + runURL
	}
	return s
}

func byID(comments []world.Comment) []world.Comment {
	out := append([]world.Comment{}, comments...)
	sort.SliceStable(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out
}

// ClaimWinner is the episode's earliest claim by comment id, never by
// timestamp: the episode starts after the last comment carrying the
// episode marker (a reclaim, a revert, a struck claim), so a dead claim
// never outranks a live claimant. The label swap is not the arbiter.
func ClaimWinner(comments []world.Comment) *world.Comment {
	sorted := byID(comments)
	var boundary int64 = -1
	for _, c := range sorted {
		if strings.Contains(c.Body, workitem.EpisodeMarker) {
			boundary = c.ID
		}
	}
	for i, c := range sorted {
		if strings.Contains(c.Body, workitem.ClaimMarker) && c.ID > boundary {
			return &sorted[i]
		}
	}
	return nil
}

// handedOffClaim is the claim an agent-held item's executor won, the
// newest before the newest hand-off: the hand-off closes its episode, but
// the agent holds the item on that claim's standing.
func handedOffClaim(comments []world.Comment) *world.Comment {
	sorted := byID(comments)
	var claim, held *world.Comment
	for i, c := range sorted {
		if strings.Contains(c.Body, workitem.HandoffMarker) {
			held = claim
		} else if strings.Contains(c.Body, workitem.ClaimMarker) {
			claim = &sorted[i]
		}
	}
	return held
}

// mineOf is this executor's newest claim among the comments.
func mineOf(comments []world.Comment, executor string) *world.Comment {
	sorted := byID(comments)
	var mine *world.Comment
	for i, c := range sorted {
		if strings.Contains(c.Body, workitem.ClaimMarker) && strings.Contains(c.Body, "executor `"+executor+"`") {
			mine = &sorted[i]
		}
	}
	return mine
}

// Claimed is an open item with the comment id of its episode's live claim
// (0 when it has none).
type Claimed struct {
	workitem.Issue
	ClaimID int64
}

// ConflictsWithEarlierClaim re-verifies the pick filters after a won
// claim: a running item with the same title, or the live standing item of
// a declared upstream, holding an earlier claim forces this one back.
func ConflictsWithEarlierClaim(item workitem.Issue, myClaim int64, others []Claimed, o queue.PickOpts) bool {
	idOf := func(i workitem.Issue) string { return queue.TaskIDOf(i, o.PathTo) }
	scheduled := func(id string) workitem.Scheduled {
		if o.ScheduledOf == nil {
			return workitem.Unknown
		}
		return o.ScheduledOf(id)
	}
	standing := func(i workitem.Issue) bool { return workitem.IsStandingItem(i.Title, scheduled(idOf(i))) }
	var upstreams []string
	if standing(item) && o.TaskAfter != nil {
		upstreams = o.TaskAfter(idOf(item))
	}
	title := strings.TrimSpace(item.Title)
	for _, x := range others {
		if x.Number == item.Number || !queue.Running(x.Issue) {
			continue
		}
		conflicting := strings.TrimSpace(x.Title) == title || (contains(upstreams, idOf(x.Issue)) && standing(x.Issue))
		if conflicting && x.ClaimID != 0 && x.ClaimID < myClaim {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
