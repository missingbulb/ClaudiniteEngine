package schedule

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/calendar"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/workitem"
)

// The repair rules: each pure, returning the open items it claims, beside
// the comment it would post. Repair is a fallback: every rule repairs
// something that already went wrong, and the healthy flow of a task never
// passes through here. Only open items are ever read; an item somebody
// closed is an answer, never a state to repair.

// SupersedableParks are the park kinds a later clean run answers.
var SupersedableParks = []string{"failure", "action"}

func instant(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	return calendar.ParseInstant(s)
}

// lastTouch is the item's updated_at, else its created_at, else now.
func lastTouch(i workitem.Issue, now time.Time) time.Time {
	if t, ok := instant(i.UpdatedAt); ok {
		return t
	}
	if t, ok := instant(i.CreatedAt); ok {
		return t
	}
	return now
}

func idle(i workitem.Issue, now time.Time) time.Duration { return now.Sub(lastTouch(i, now)) }

// taskOf is the task an item names, by title or by its machine block's
// worker path.
func taskOf(i workitem.Issue) (workitem.Title, bool) { return i.TaskOf() }

func hours(d time.Duration) string { return fmt.Sprint(math.Round(d.Hours())) }
func days(d time.Duration) string  { return fmt.Sprint(math.Round(d.Hours() / 24)) }

// StaleReadyItems are ready items nobody picked for StaleReadyPeriods of
// their task's period (a day for a task keeping none).
func StaleReadyItems(open []workitem.Issue, now time.Time, periodFor func(id string) (time.Duration, bool)) []workitem.Issue {
	var out []workitem.Issue
	for _, i := range open {
		if !i.Is(workitem.StatusReady) {
			continue
		}
		t, ok := taskOf(i)
		if !ok {
			continue
		}
		per, ok := periodFor(t.ID())
		if !ok {
			per = 24 * time.Hour
		}
		if idle(i, now) >= time.Duration(workitem.StaleReadyPeriods)*per {
			out = append(out, i)
		}
	}
	return out
}

// StaleReadyComment explains a stale-ready park.
func StaleReadyComment(i workitem.Issue) string {
	what := "this task"
	if t, ok := taskOf(i); ok {
		what = t.ID()
	}
	return fmt.Sprintf("This work item for %s has sat `%s` for over ~%d of its scheduling periods without an executor picking it up. Parking it for a human and taking it out of the queue.",
		what, workitem.StatusReady, workitem.StaleReadyPeriods)
}

// DeadAgentItems are items whose agent has not moved within the agent
// leash: progress from the beat's note where it beats, the issue clock
// where it never did.
func DeadAgentItems(open []workitem.Issue, now time.Time, progressAt func(workitem.Issue) time.Time) []workitem.Issue {
	var out []workitem.Issue
	for _, i := range open {
		if !i.Is(workitem.StatusRunningAgent) {
			continue
		}
		since := idle(i, now)
		if moved := progressAt(i); !moved.IsZero() {
			since = now.Sub(moved)
		}
		if since >= workitem.AgentLeash {
			out = append(out, i)
		}
	}
	return out
}

// DeadAgentComment explains a dead-agent park; note names the session
// where the hand-off comment did.
func DeadAgentComment(note string, wedged bool) string {
	how := "with no activity"
	if wedged {
		how = "without the work moving — the session kept beating, but every beat said the same thing"
	}
	who := ""
	if note != "" {
		who = " (" + note + ")"
	}
	return fmt.Sprintf("This work item has carried `%s` for over %sh %s — the agent session that claimed it%s never converged it. Parking it for a human.",
		workitem.StatusRunningAgent, hours(workitem.AgentLeash), how, who)
}

// StuckBlockedItems are blocked items idle past the bound with a blocker
// still open; the bound is idleness, so the comment is its own guard.
func StuckBlockedItems(open []workitem.Issue, now time.Time, stateOf func(int) string) []workitem.Issue {
	var out []workitem.Issue
	for _, i := range open {
		if !i.Is(workitem.StatusBlocked) {
			continue
		}
		blockedBy := workitem.ParseBody(i.Body).BlockedBy
		if len(blockedBy) == 0 {
			continue
		}
		all := true
		for _, n := range blockedBy {
			if stateOf(n) != "closed" {
				all = false
			}
		}
		if !all && idle(i, now) >= workitem.StuckBlocked {
			out = append(out, i)
		}
	}
	return out
}

// StuckBlockedComment notes a long wait.
func StuckBlockedComment(unresolved []int) string {
	refs := make([]string, len(unresolved))
	for k, n := range unresolved {
		refs[k] = fmt.Sprintf("#%d", n)
	}
	return fmt.Sprintf("This work item has been blocked on %s for over %s days. Nothing here is stuck mechanically — it will proceed by itself the moment those close — but if they are never going to, close this item by hand.",
		strings.Join(refs, ", "), days(workitem.StuckBlocked))
}

// StatelessItems are open items whose labels decode to no status.
func StatelessItems(open []workitem.Issue) []workitem.Issue {
	var out []workitem.Issue
	for _, i := range open {
		if i.Status() == "" {
			out = append(out, i)
		}
	}
	return out
}

// StatelessComment explains a stateless park.
func StatelessComment() string {
	return "This work item carries no state label at all — the leavings of a label swap that tore mid-flight, which puts it outside the state machine. " +
		"Parking it for a human: re-queue it by hand (" + workitem.RequeueHint + ") once you have looked at it."
}

// DoneAfter answers the task's newest run that converged done strictly
// after since, or nil.
type DoneAfter func(id, since string) *workitem.Issue

// SupersededItems are failure and action parks of a fungible occurrence a
// later clean run of the same task answered.
func SupersededItems(open []workitem.Issue, doneAfter DoneAfter) []workitem.Issue {
	var out []workitem.Issue
	for _, i := range open {
		if !i.Parked() || has(workitem.AskedForOrigins, i.Origin()) || !has(SupersedableParks, i.ParkKind()) {
			continue
		}
		t, ok := taskOf(i)
		if !ok {
			continue
		}
		if doneAfter(t.ID(), touchedOrCreated(i)) != nil {
			out = append(out, i)
		}
	}
	return out
}

func touchedOrCreated(i workitem.Issue) string {
	if i.UpdatedAt != "" {
		return i.UpdatedAt
	}
	return i.CreatedAt
}

// SupersededComment names the run that answered a park.
func SupersededComment(run workitem.Issue) string {
	closed := run.ClosedAt
	if closed == "" {
		closed = "null"
	}
	if len(closed) > 10 {
		closed = closed[:10]
	}
	return fmt.Sprintf("A later run of this task converged clean — #%d, on %s — so whatever this item ", run.Number, closed) +
		"was parked on is resolved. Closing it `task:status:rejected` rather than leaving a question nobody needs to answer. " +
		"If this park was about something that run did NOT cover, re-queue it (" + workitem.RequeueHint + ")."
}

// TaskPathIndex maps each task id to the worker path its items name.
func TaskPathIndex(tasks []taskspec.Task) map[string]string {
	out := map[string]string{}
	for _, t := range tasks {
		out[t.Path()] = t.TaskPath()
	}
	return out
}

// OrphanedParkItems are parks this repo cannot run at HEAD: the task is
// undeclared, or the item names a path the task no longer lives at. An
// empty task set is unknown, never "everything retired".
func OrphanedParkItems(open []workitem.Issue, tasks []taskspec.Task) []workitem.Issue {
	if len(tasks) == 0 {
		return nil
	}
	head := TaskPathIndex(tasks)
	var out []workitem.Issue
	for _, i := range open {
		if !i.Parked() {
			continue
		}
		path := workitem.ParseBody(i.Body).TaskPath
		t, ok := taskOf(i)
		if !ok {
			continue
		}
		at, declared := head[t.ID()]
		if !declared || (path != "" && at != path) {
			out = append(out, i)
		}
	}
	return out
}

// OrphanedParkComment explains an orphaned park; headPath is where the
// task lives now, "" when it is gone.
func OrphanedParkComment(id, headPath string) string {
	if headPath != "" {
		return fmt.Sprintf("This item names `%s` at a path it no longer lives at — the pack was renamed since the item was filed, ", id) +
			fmt.Sprintf("and the task is at `%s` now. An item's stored path is never rewritten, so this one can never run. ", headPath) +
			"Closing it obsolete; the scheduler files a fresh occurrence at the current path."
	}
	return fmt.Sprintf("`%s` is not a task this repo carries at HEAD — the pack may be undeclared, or the task retired. ", id) +
		"This item is parked on work that cannot run again, so it closes `task:status:rejected` rather than " +
		"waiting for an answer that would change nothing."
}

// ReleasedComment explains a person's own issue let go of by the queue: it
// was theirs before it was adopted, so it stays open as it was.
func ReleasedComment(id string) string {
	return fmt.Sprintf("`%s` is not a task this repo carries at HEAD — the pack may be undeclared, or the task retired — so the queue can never run this issue. ", id) +
		"It was yours before the queue adopted it, so it stays open: the queue's labels and its machine block are removed and nothing else is changed. " +
		"Mark it `" + workitem.OriginAdHoc + "` again to ask a task this repo carries."
}

// EndedParkItems are parks whose Ends-when target resolved.
func EndedParkItems(open []workitem.Issue, resolutionOf func(int) string) []workitem.Issue {
	var out []workitem.Issue
	for _, i := range open {
		if !i.Parked() {
			continue
		}
		f := workitem.ParseFields(i.Body)
		if f.EndsWhen == nil {
			continue
		}
		if resolutionOf(*f.EndsWhen) != "" {
			out = append(out, i)
		}
	}
	return out
}

// EndedParkComment explains an ended park.
func EndedParkComment(target int, resolution string) string {
	if resolution == "merged" {
		return fmt.Sprintf("#%d merged, which is what this item was parked waiting for. Closing it `%s` — ", target, workitem.StatusDone) +
			"the work landed and there is nothing left for anyone to do here."
	}
	return fmt.Sprintf("#%d was closed without merging, which ends what this item was parked waiting for. ", target) +
		fmt.Sprintf("Closing it `%s` — nothing landed, so if the work is still wanted, re-queue it (%s).", workitem.StatusRejected, workitem.RequeueHint)
}

// EndedReleasedComment explains releasing a person's own issue whose
// awaited pull request closed without merging.
func EndedReleasedComment(target int) string {
	return fmt.Sprintf("#%d was closed without merging, which ends what this issue was parked waiting for. ", target) +
		"It was yours before the queue adopted it, so it stays open: the queue's labels and its machine block are removed and nothing else is changed. " +
		"Mark it `" + workitem.OriginAdHoc + "` again if the work is still wanted."
}

// PeriodForTasks is a task's period from its cadence term at HEAD.
func PeriodForTasks(tasks []taskspec.Task) func(id string) (time.Duration, bool) {
	byID := index(tasks)
	return func(id string) (time.Duration, bool) {
		t, ok := byID[id]
		if !ok {
			return 0, false
		}
		c := t.Decl.Cadence()
		if c == nil {
			return 0, false
		}
		return calendar.Period(c.Cadence)
	}
}

// UnclosedTerminalItems are open items wearing a terminal status past
// TerminalOpen.
func UnclosedTerminalItems(open []workitem.Issue, now time.Time) []workitem.Issue {
	var out []workitem.Issue
	for _, i := range open {
		if i.State != "open" {
			continue
		}
		s := i.Status()
		if s != workitem.StatusDone && s != workitem.StatusRejected {
			continue
		}
		if idle(i, now) >= workitem.TerminalOpen {
			out = append(out, i)
		}
	}
	return out
}

// UnclosedTerminalComment explains the close a torn converge never made.
func UnclosedTerminalComment(status string) string {
	if status == workitem.StatusDone {
		return fmt.Sprintf("This item carries `%s` — its run finished and nothing is left for anyone to act on — but it was never closed, ", workitem.StatusDone) +
			"so it has been sitting in the open queue looking like live work. Closing it, which is all the terminal was missing."
	}
	return fmt.Sprintf("This item carries `%s` — it was taken out of the queue — but it was never closed, ", workitem.StatusRejected) +
		fmt.Sprintf("so it has been sitting open looking like live work. Closing it; if the work is still wanted, re-queue it (%s).", workitem.RequeueHint)
}

// AbandonedParkItems are standing items' failure parks nobody touched in
// AbandonedPark.
func AbandonedParkItems(open []workitem.Issue, now time.Time, scheduledFor func(id string) workitem.Scheduled) []workitem.Issue {
	var out []workitem.Issue
	for _, i := range open {
		if i.Status() != workitem.StatusNeedsHumanFailure {
			continue
		}
		t, ok := workitem.ParseTitle(i.Title)
		if !ok || !workitem.IsStandingItem(i.Title, scheduledFor(t.ID())) {
			continue
		}
		if idle(i, now) >= workitem.AbandonedPark {
			out = append(out, i)
		}
	}
	return out
}

// AbandonedParkComment explains an abandoned park's close.
func AbandonedParkComment() string {
	return fmt.Sprintf("Nothing has touched this park in over %s days. Leaving it standing does not preserve the ", days(workitem.AbandonedPark)) +
		"report — nobody is going to read it now, and where the task declares `last-run-not-failed` it also keeps the task from " +
		"running again at all. A later clean run is " +
		fmt.Sprintf("what would otherwise have closed this. Closing it `%s`: the next scheduled occurrence runs, and if the ", workitem.StatusRejected) +
		fmt.Sprintf("fault is still there it parks again against a trace worth reading. If you were part-way through diagnosing it, re-queue it (%s).", workitem.RequeueHint)
}

// ScheduledForTasks is whether a task at HEAD is on the schedule; Unknown
// for a task the repo no longer carries.
func ScheduledForTasks(tasks []taskspec.Task) func(id string) workitem.Scheduled {
	byID := index(tasks)
	return func(id string) workitem.Scheduled {
		t, ok := byID[id]
		switch {
		case !ok:
			return workitem.Unknown
		case t.Decl.IsScheduled():
			return workitem.Yes
		}
		return workitem.No
	}
}

// DoneRunLookup indexes the closed done half of the queue for the
// superseded rule.
func DoneRunLookup(done []workitem.Issue) DoneAfter {
	byTask := map[string][]workitem.Issue{}
	for _, d := range done {
		t, ok := taskOf(d)
		if !ok {
			continue
		}
		byTask[t.ID()] = append(byTask[t.ID()], d)
	}
	closedAt := func(d workitem.Issue) (time.Time, bool) {
		if d.ClosedAt != "" {
			return instant(d.ClosedAt)
		}
		return instant(d.UpdatedAt)
	}
	return func(id, since string) *workitem.Issue {
		at, ok := instant(since)
		if !ok {
			return nil
		}
		var runs []workitem.Issue
		for _, d := range byTask[id] {
			if c, ok := closedAt(d); ok && c.After(at) {
				runs = append(runs, d)
			}
		}
		if len(runs) == 0 {
			return nil
		}
		sort.SliceStable(runs, func(a, b int) bool {
			x, _ := closedAt(runs[a])
			y, _ := closedAt(runs[b])
			return x.Before(y)
		})
		r := runs[len(runs)-1]
		return &r
	}
}

func index(tasks []taskspec.Task) map[string]taskspec.Task {
	out := map[string]taskspec.Task{}
	for _, t := range tasks {
		out[t.Path()] = t
	}
	return out
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
