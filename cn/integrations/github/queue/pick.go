package queue

import (
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/workitem"
)

// Running reports an item somebody is executing: the executor holds it,
// or the agent it handed to does.
func Running(i workitem.Issue) bool {
	return i.Is(workitem.StatusRunningExecutor) || i.Is(workitem.StatusRunningAgent)
}

// TaskIDOf is the task an item names: a filed item's title, or a marked
// issue's machine-block path through pathTo, which only the run's task set
// can answer; "" when neither does.
func TaskIDOf(i workitem.Issue, pathTo func(string) string) string {
	if t, ok := workitem.ParseTitle(i.Title); ok {
		return t.ID()
	}
	if pathTo == nil {
		return ""
	}
	return pathTo(workitem.ParseBody(i.Body).TaskPath)
}

// PickOpts are the run's answers the pick order needs.
type PickOpts struct {
	// TaskAfter is a task's declared upstreams, as <pack>/<task> ids.
	TaskAfter func(id string) []string
	// ScheduledOf is whether the scheduler asks a task at HEAD.
	ScheduledOf func(id string) workitem.Scheduled
	// Draw is the tie-break draw, taken once per open item in order.
	Draw   func() float64
	PathTo func(path string) string
}

// PickOrder is the ready items in the order a worker takes them: urgent
// first, then by a random draw taken once per item, skipping an item whose
// exact title has another open item running (one task, one execution at a
// time) and a standing item whose declared upstream's standing item is
// live this cycle.
func PickOrder(open []workitem.Issue, o PickOpts) []workitem.Issue {
	idOf := func(i workitem.Issue) string { return TaskIDOf(i, o.PathTo) }
	scheduled := func(id string) workitem.Scheduled {
		if o.ScheduledOf == nil {
			return workitem.Unknown
		}
		return o.ScheduledOf(id)
	}
	standing := func(i workitem.Issue) bool { return workitem.IsStandingItem(i.Title, scheduled(idOf(i))) }
	live := func(i workitem.Issue) bool { return i.Is(workitem.StatusReady) || Running(i) }
	liveUpstream := func(id string) bool {
		for _, x := range open {
			if idOf(x) == id && standing(x) && live(x) {
				return true
			}
		}
		return false
	}
	draw := map[int]float64{}
	for _, i := range open {
		if o.Draw != nil {
			draw[i.Number] = o.Draw()
		}
	}
	var out []workitem.Issue
	for _, i := range open {
		if !i.Is(workitem.StatusReady) {
			continue
		}
		twin := false
		for _, x := range open {
			if x.Number != i.Number && strings.TrimSpace(x.Title) == strings.TrimSpace(i.Title) && Running(x) {
				twin = true
				break
			}
		}
		if twin {
			continue
		}
		if standing(i) && o.TaskAfter != nil {
			held := false
			for _, up := range o.TaskAfter(idOf(i)) {
				if liveUpstream(up) {
					held = true
					break
				}
			}
			if held {
				continue
			}
		}
		out = append(out, i)
	}
	urgent := func(i workitem.Issue) int {
		if i.HasLabel(workitem.Urgent) {
			return 1
		}
		return 0
	}
	sort.SliceStable(out, func(a, b int) bool {
		if ua, ub := urgent(out[a]), urgent(out[b]); ua != ub {
			return ua > ub
		}
		return draw[out[a].Number] < draw[out[b].Number]
	})
	return out
}
