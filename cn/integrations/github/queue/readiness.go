package queue

import (
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/calendar"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/workitem"
)

// IsReleasable reports whether a blocked item's wait is over: every
// Blocked-by target closed and its Not-before passed. stateOf answers a
// target that may not be a work item at all; an unknown one ("") is never
// closed, so an unreadable blocker delays rather than releases.
func IsReleasable(i workitem.Issue, stateOf func(int) string, now time.Time) bool {
	if i.State != "open" || !i.Is(workitem.StatusBlocked) {
		return false
	}
	b := workitem.ParseBody(i.Body)
	for _, n := range b.BlockedBy {
		if stateOf == nil || stateOf(n) != "closed" {
			return false
		}
	}
	if b.NotBefore == "" {
		return true
	}
	at, ok := calendar.ParseInstant(b.NotBefore)
	if !ok {
		// JavaScript's Date of an unreadable instant is NaN, and every
		// comparison with NaN is false: the time is never reached.
		return false
	}
	return !now.Before(at)
}
