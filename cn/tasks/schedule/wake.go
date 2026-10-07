package schedule

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/queue"
)

// ForcedWakeContext is the Context a force-minted standing item carries:
// the force is the only reason it exists, and the executor still decides
// at pick.
const ForcedWakeContext = "Minted by a force — this task had no open standing item at the time. The wake stands in for the task's cadence; every other condition is still evaluated at pick, so converge to a no-op if there is nothing to do."

// inFlight are the statuses meaning someone already holds an item.
var inFlight = []string{workitem.StatusReady, workitem.StatusRunningExecutor, workitem.StatusRunningAgent}

// WakeTarget is an item a force wakes.
type WakeTarget struct {
	ID    string `json:"id"`
	Issue int    `json:"issue"`
}

// WakeMint is a standing item a force files because none stands.
type WakeMint struct {
	ID       string `json:"id"`
	Pack     string `json:"pack"`
	Task     string `json:"task"`
	TaskPath string `json:"taskPath"`
}

// WakeMiss is an id the force could not resolve, and why.
type WakeMiss struct {
	ID  string `json:"id"`
	Why string `json:"why"`
}

// WakePlan is what a wake spec resolves to.
type WakePlan struct {
	Wake      []WakeTarget `json:"wake"`
	Create    []WakeMint   `json:"create"`
	Already   []WakeTarget `json:"already"`
	Unmatched []WakeMiss   `json:"unmatched"`
}

var wakeSplitRE = regexp.MustCompile(`[\s,]+`)

// PlanWake resolves a wake spec (ids, pack/task or a bare task) against
// the declared tasks and the open items: an item in flight is left alone,
// a missing standing item is minted, a request task's open items are
// woken by their machine block's path and never minted, and every id that
// matches nothing is reported.
func PlanWake(spec string, tasks []taskspec.Task, items []workitem.Issue) WakePlan {
	p := WakePlan{Wake: []WakeTarget{}, Create: []WakeMint{}, Already: []WakeTarget{}, Unmatched: []WakeMiss{}}
	for _, id := range wakeSplitRE.Split(spec, -1) {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		pack, name := "", id
		scoped := strings.Contains(id, "/")
		if scoped {
			parts := strings.Split(id, "/")
			pack, name = parts[0], parts[1]
		}
		var owners []taskspec.Task
		for _, t := range tasks {
			if t.ID == name && (!scoped || t.Pack == pack) {
				owners = append(owners, t)
			}
		}
		if len(owners) != 1 {
			why := "no declared pack owns a task by that name"
			if len(owners) > 0 {
				why = fmt.Sprintf(`%d declared packs own a "%s" task — name it as pack/task`, len(owners), name)
			}
			p.Unmatched = append(p.Unmatched, WakeMiss{id, why})
			continue
		}
		owner := owners[0]
		full := owner.Path()
		if !owner.Decl.IsScheduled() {
			var routed []workitem.Issue
			for _, i := range items {
				if i.State != "open" {
					continue
				}
				if t, ok := workitem.TaskIDFromPath(workitem.ParseBody(i.Body).TaskPath); ok && t.Pack == owner.Pack && t.Task == owner.ID {
					routed = append(routed, i)
				}
			}
			if len(routed) == 0 {
				p.Unmatched = append(p.Unmatched, WakeMiss{id, fmt.Sprintf(`"%s" is not on the schedule and has no open item — nothing stands for it to mint, so an item exists only where an issue names the task`, full)})
				continue
			}
			for _, i := range routed {
				if has(inFlight, i.Status()) {
					p.Already = append(p.Already, WakeTarget{id, i.Number})
				} else {
					p.Wake = append(p.Wake, WakeTarget{full, i.Number})
				}
			}
			continue
		}
		var item *workitem.Issue
		for k, i := range items {
			if i.State != "open" {
				continue
			}
			if t, ok := workitem.ParseTitle(i.Title); ok && t.Pack == owner.Pack && t.Task == owner.ID {
				item = &items[k]
				break
			}
		}
		switch {
		case item == nil:
			p.Create = append(p.Create, WakeMint{full, owner.Pack, owner.ID, owner.TaskPath()})
		case has(inFlight, item.Status()):
			p.Already = append(p.Already, WakeTarget{id, item.Number})
		default:
			p.Wake = append(p.Wake, WakeTarget{full, item.Number})
		}
	}
	return p
}

// PickableCount is the drain gate's verdict: the union of what the list
// shows pickable and what this run itself readied, since the list can
// miss an item created milliseconds earlier.
func PickableCount(open []workitem.Issue, readied []int, o queue.PickOpts) int {
	seen := map[int]bool{}
	for _, i := range queue.PickOrder(open, o) {
		seen[i.Number] = true
	}
	for _, n := range readied {
		seen[n] = true
	}
	return len(seen)
}

// WithOwnWrites is the queue as this run knows it: the listing, plus the
// items this run filed that the listing has not caught up to.
func WithOwnWrites(listed, minted []workitem.Issue) []workitem.Issue {
	seen := map[int]bool{}
	for _, i := range listed {
		seen[i.Number] = true
	}
	out := append([]workitem.Issue{}, listed...)
	for _, i := range minted {
		if !seen[i.Number] {
			out = append(out, i)
		}
	}
	return out
}

// BlockersToResolve are the Blocked-by targets the run still has to read:
// a blocked item's and a marked issue's, less those known.
func BlockersToResolve(items []workitem.Issue, requests []Request, known map[int]string) []int {
	var out []int
	seen := map[int]bool{}
	want := func(n int) {
		if _, ok := known[n]; ok || seen[n] {
			return
		}
		seen[n] = true
		out = append(out, n)
	}
	for _, i := range items {
		if i.State != "open" || !i.Is(workitem.StatusBlocked) {
			continue
		}
		for _, n := range workitem.ParseBody(i.Body).BlockedBy {
			want(n)
		}
	}
	for _, r := range requests {
		for _, n := range workitem.ParseBlockedBy(r.Body) {
			want(n)
		}
	}
	return out
}
