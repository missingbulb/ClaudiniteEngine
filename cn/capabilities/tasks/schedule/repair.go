package schedule

import (
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/workitem"
)

// The repair op kinds, applied in their own phase before anything else
// the run writes.
const (
	KindEscalate      = "escalate"
	KindRetire        = "retire"
	KindCloseTerminal = "close-terminal"
	KindNote          = "note"
	KindRelease       = "release"
)

// RepairKinds are the repair phase's op kinds.
var RepairKinds = []string{KindEscalate, KindRetire, KindCloseTerminal, KindNote, KindRelease}

// The confirmations a transient-premise rule asks of a fresh read.
const (
	ConfirmStateless = "stateless"
	ConfirmAbandoned = "abandoned"
	ConfirmUnclosed  = "unclosed"
)

// LabelOn is one label on one issue.
type LabelOn struct {
	Issue int    `json:"issue"`
	Label string `json:"label"`
}

// RepairIn is what the repair phase reads beyond the items: every lookup
// a rule needs that is not a label, so the rules stay pure and the reads
// stay the shell's.
type RepairIn struct {
	Items        []workitem.Issue
	Tasks        []taskspec.Task
	Now          time.Time
	ProgressAt   func(workitem.Issue) time.Time
	ResolutionOf func(int) string
	DoneAfter    DoneAfter
	IsRequest    func(int) bool
	StateOf      func(int) string
}

func (in *RepairIn) defaults() {
	if in.ProgressAt == nil {
		in.ProgressAt = func(workitem.Issue) time.Time { return time.Time{} }
	}
	if in.ResolutionOf == nil {
		in.ResolutionOf = func(int) string { return "" }
	}
	if in.DoneAfter == nil {
		in.DoneAfter = func(string, string) *workitem.Issue { return nil }
	}
	if in.IsRequest == nil {
		in.IsRequest = func(int) bool { return false }
	}
	if in.StateOf == nil {
		in.StateOf = func(int) string { return "" }
	}
}

// PlanRepair decides which op each rule's verdict becomes, in the rules'
// precedence order, and threads the effects the shell is certain to write
// back into items, so the ask judges this run's world. An op carrying a
// confirmation is never threaded: the shell may drop it on its fresh read.
func PlanRepair(in RepairIn) (ops []Op, closed map[int]bool) {
	in.defaults()
	var open []workitem.Issue
	for _, i := range in.Items {
		if i.State == "open" {
			open = append(open, i)
		}
	}
	taken := map[int]bool{}
	claim := func(i workitem.Issue) bool {
		if taken[i.Number] {
			return false
		}
		taken[i.Number] = true
		return true
	}
	at := calendarISO(in.Now)

	for _, i := range SupersededItems(open, in.DoneAfter) {
		if !claim(i) {
			continue
		}
		t, _ := taskOf(i)
		run := in.DoneAfter(t.ID(), touchedOrCreated(i))
		ops = append(ops, Op{Kind: KindRetire, Rule: "superseded", Issue: i.Number, From: i.Status(), To: workitem.StatusRejected,
			Close: "not_planned", At: at, Body: SupersededComment(*run)})
	}
	head := TaskPathIndex(in.Tasks)
	for _, i := range OrphanedParkItems(open, in.Tasks) {
		if !claim(i) {
			continue
		}
		t, _ := taskOf(i)
		if i.IsAdopted() {
			var marks []string
			for _, l := range i.Labels {
				if workitem.IsQueueLabel(l) {
					marks = append(marks, l)
				}
			}
			ops = append(ops, Op{Kind: KindRelease, Rule: "orphaned", Issue: i.Number, Labels: marks,
				IssueBody: workitem.HumanTextOf(i.Body), Body: ReleasedComment(t.ID())})
			continue
		}
		ops = append(ops, Op{Kind: KindRetire, Rule: "orphaned", Issue: i.Number, From: i.Status(), To: workitem.StatusRejected,
			Close: "not_planned", At: at, Body: OrphanedParkComment(t.ID(), head[t.ID()])})
	}
	for _, i := range EndedParkItems(open, in.ResolutionOf) {
		if !claim(i) {
			continue
		}
		b := workitem.ParseBody(i.Body)
		resolution := in.ResolutionOf(b.EndsWhen)
		op := Op{Kind: KindRetire, Rule: "ended", Issue: i.Number, From: i.Status(), To: workitem.StatusRejected,
			Close: "not_planned", At: at, Body: EndedParkComment(b.EndsWhen, resolution)}
		if resolution == "merged" {
			op.To, op.Close = workitem.StatusDone, "completed"
		}
		if b.Request != 0 && b.Request != i.Number {
			op.ClearInReview = &LabelOn{Issue: b.Request, Label: workitem.InReviewLabel}
		}
		ops = append(ops, op)
	}
	for _, i := range AbandonedParkItems(open, in.Now, ScheduledForTasks(in.Tasks)) {
		if !claim(i) {
			continue
		}
		ops = append(ops, Op{Kind: KindRetire, Rule: "abandoned", Issue: i.Number, From: i.Status(), To: workitem.StatusRejected,
			Close: "not_planned", At: at, Body: AbandonedParkComment(), Confirm: ConfirmAbandoned})
	}
	for _, i := range UnclosedTerminalItems(open, in.Now) {
		if !claim(i) {
			continue
		}
		s := i.Status()
		op := Op{Kind: KindCloseTerminal, Rule: "unclosed", Issue: i.Number, Close: "not_planned",
			Body: UnclosedTerminalComment(s), Confirm: ConfirmUnclosed}
		if s == workitem.StatusDone {
			op.Close = "completed"
		}
		ops = append(ops, op)
	}
	for _, i := range StaleReadyItems(open, in.Now, PeriodForTasks(in.Tasks)) {
		if !claim(i) {
			continue
		}
		ops = append(ops, Op{Kind: KindEscalate, Rule: "stale-ready", Issue: i.Number, From: workitem.StatusReady,
			To: workitem.StatusNeedsHumanAction, Body: StaleReadyComment(i)})
	}
	for _, i := range DeadAgentItems(open, in.Now, in.ProgressAt) {
		if !claim(i) {
			continue
		}
		ops = append(ops, Op{Kind: KindEscalate, Rule: "dead-agent", Issue: i.Number, From: workitem.StatusRunningAgent,
			To: workitem.StatusNeedsHumanFailure, Note: "dead-agent", Wedged: !in.ProgressAt(i).IsZero()})
	}
	for _, i := range StuckBlockedItems(open, in.Now, in.StateOf) {
		if taken[i.Number] {
			continue
		}
		var unresolved []int
		for _, n := range workitem.ParseBody(i.Body).BlockedBy {
			if in.StateOf(n) != "closed" {
				unresolved = append(unresolved, n)
			}
		}
		ops = append(ops, Op{Kind: KindNote, Rule: "stuck-dependency", Issue: i.Number, Body: StuckBlockedComment(unresolved)})
	}
	for _, i := range StatelessItems(open) {
		if in.IsRequest(i.Number) || !claim(i) {
			continue
		}
		ops = append(ops, Op{Kind: KindEscalate, Rule: "stateless", Issue: i.Number,
			To: workitem.StatusNeedsHumanFailure, Body: StatelessComment(), Confirm: ConfirmStateless})
	}

	closed = map[int]bool{}
	pos := map[int]int{}
	for k, i := range in.Items {
		pos[i.Number] = k
	}
	for _, op := range ops {
		if op.Confirm != "" {
			continue
		}
		if k, ok := pos[op.Issue]; ok {
			threadEffect(&in.Items[k], op)
		}
		if op.Kind == KindRetire {
			closed[op.Issue] = true
		}
	}
	return ops, closed
}

// threadEffect mirrors on the in-memory item exactly what the shell
// writes: the status cleared of every spelling, the new label added, and
// for a retire the close.
func threadEffect(i *workitem.Issue, op Op) {
	if op.From != "" {
		gone := map[string]bool{}
		for _, s := range workitem.SpellingsOf(op.From) {
			gone[s] = true
		}
		kept := workitem.LabelList{}
		for _, l := range i.Labels {
			if !gone[l] {
				kept = append(kept, l)
			}
		}
		i.Labels = kept
	}
	if op.To != "" && !has(i.Labels, op.To) {
		i.Labels = append(i.Labels, op.To)
	}
	if op.Kind == KindRelease {
		kept := workitem.LabelList{}
		for _, l := range i.Labels {
			if !has(op.Labels, l) {
				kept = append(kept, l)
			}
		}
		i.Labels, i.Body = kept, op.IssueBody
	}
	if op.Kind == KindRetire {
		i.State, i.ClosedAt = "closed", op.At
	}
}

// StillHolds re-runs a transient-premise rule's predicate on a fresh read
// of its issue; a verdict that no longer holds is dropped, not softened.
func StillHolds(op Op, fresh *workitem.Issue, now time.Time, tasks []taskspec.Task) bool {
	if fresh == nil || fresh.State != "open" {
		return false
	}
	one := []workitem.Issue{*fresh}
	switch op.Confirm {
	case ConfirmStateless:
		return len(StatelessItems(one)) > 0
	case ConfirmAbandoned:
		return len(AbandonedParkItems(one, now, ScheduledForTasks(tasks))) > 0
	case ConfirmUnclosed:
		return len(UnclosedTerminalItems(one, now)) > 0
	}
	return true
}
