package schedule

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/calendar"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/items"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/localterms"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/precondition"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/queue"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/signals"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/world"
)

// RunIn is the world one scheduler run acts on: the same code a member's
// workflow drives is what the simulator drives.
type RunIn struct {
	Issues   world.Issues
	Tasks    []taskspec.Task
	Now      time.Time
	Disabled []string
	// PackConfig is a pack's config block, for the terms that read one.
	PackConfig func(pack string) map[string]any
	// Collector builds the signal collector bound to the queue this run
	// already holds; nil collects nothing, so every term over a signal
	// fails open.
	Collector func(items []workitem.Issue) *signals.Collector
	// LocalTerms asks a task's own terms through the runner; with none, a
	// task naming one is unasked and the run fails.
	LocalTerms *localterms.Asker
	Wake       string
	Log        func(string)
	// SetOutput publishes the drain gate; an error is reported, never fatal.
	SetOutput func(name, value string) error
	// Phase times a phase of the run for its cost record.
	Phase func(name string) func()
}

// RunOut is what the run decided: the ops, the asks, what the drain gate
// counted, and the problems that make the job red.
type RunOut struct {
	Ops      []Op
	Asked    []Asked
	Pickable int
	Problems []string
}

func (in *RunIn) defaults() {
	if in.Log == nil {
		in.Log = func(string) {}
	}
	if in.SetOutput == nil {
		in.SetOutput = func(string, string) error { return nil }
	}
	if in.Phase == nil {
		in.Phase = func(string) func() { return func() {} }
	}
	if in.PackConfig == nil {
		in.PackConfig = func(string) map[string]any { return map[string]any{} }
	}
}

// ListItems is every work item a listing returns, PRs and non-items
// dropped; a page that cannot be read fails the list, since a truncated
// queue reads a standing item as absent and mints a second.
func ListItems(gh world.Issues, q world.Query) ([]workitem.Issue, error) {
	q.Sort, q.Direction = "created", "desc"
	return queue.List(gh, q, 0, func(workitem.Issue) bool { return true })
}

// ListMarked is every open issue carrying a mark and no status, one
// listing per mark (the labels filter is conjunctive), with whether its
// author holds push access.
func ListMarked(gh world.Issues) ([]Request, error) {
	push := map[string]*bool{}
	pushOf := func(login string) *bool {
		if v, ok := push[login]; ok {
			return v
		}
		var out *bool
		role, err := gh.Permission(login)
		switch {
		case err == nil:
			v := world.HasPush(role)
			out = &v
		case errors.Is(err, world.ErrGone):
			v := false
			out = &v
		}
		push[login] = out
		return out
	}
	seen := map[int]bool{}
	out := []Request{}
	for _, label := range []string{workitem.OriginAdHoc, workitem.RequestLabel} {
		for page := 1; ; page++ {
			got, err := gh.IssuesPage(world.Query{State: "open", Label: label, Sort: "created", Direction: "desc"}, page)
			if err != nil {
				return nil, fmt.Errorf("could not list open issues marked `%s` (%v) — an unreadable request list is not an empty one", label, err)
			}
			for _, i := range got {
				if i.PullRequest || strings.HasPrefix(i.Title, workitem.WorkPrefix) || i.Status() != "" || seen[i.Number] {
					continue
				}
				seen[i.Number] = true
				r := Request{Issue: i.Issue, Author: i.Author}
				if i.Author != "" {
					r.AuthorHasPush = pushOf(i.Author)
				}
				out = append(out, r)
			}
			if len(got) < world.PageSize {
				break
			}
		}
	}
	return out, nil
}

var nonceRE = regexp.MustCompile("nonce `([^`]+)`")

func sessionNote(comments []world.Comment) string {
	for k := len(comments) - 1; k >= 0; k-- {
		if strings.Contains(comments[k].Body, workitem.HandoffMarker) {
			if m := nonceRE.FindStringSubmatch(comments[k].Body); m != nil {
				return "invocation nonce " + m[1]
			}
			return ""
		}
	}
	return ""
}

// evaluate asks one task. A task whose own terms could not be asked
// declines here, its error recorded in unasked under the task's path.
func (in *RunIn) evaluate(task taskspec.Task, collect *signals.Collector, unasked map[string]string) precondition.Verdict {
	if !task.Decl.Has("preconditions") {
		return precondition.Verdict{Error: `the task declares no "preconditions"`}
	}
	now := in.Now
	judge := func(s precondition.Signals, partial bool) precondition.Verdict {
		var local precondition.Judge
		failed := ""
		if !partial {
			judge := func(taskspec.Ref, precondition.Signals, precondition.Opts) precondition.Outcome {
				return precondition.Outcome{Error: "this scheduler run holds no runner to ask it through"}
			}
			if in.LocalTerms != nil {
				judge = in.LocalTerms.Judge(task, nil)
			}
			local = func(ref taskspec.Ref, s precondition.Signals, o precondition.Opts) precondition.Outcome {
				out := judge(ref, s, o)
				if out.Error != "" && failed == "" {
					failed = ref.Name + ": " + out.Error
				}
				return out
			}
		}
		v := precondition.Evaluate(precondition.Input{Preconditions: task.Decl.Preconditions(), Signals: s,
			Config: in.PackConfig(task.Pack), Terms: task.Terms, Local: local, WindowDays: precondition.WindowDays(task.Decl, s),
			Now: &now, Partial: partial})
		if failed != "" {
			unasked[task.Path()] = failed
			f := false
			return precondition.Verdict{Run: &f, Reason: failed}
		}
		return v
	}
	if collect == nil {
		return judge(precondition.Signals{}, false)
	}
	first := judge(collect.Collect(task, now, nil, []string{"runs"}), true)
	if first.Error != "" || first.Run != nil {
		return first
	}
	names := taskspec.Signals(task.Decl.Preconditions(), task.Terms)
	s := collect.Collect(task, now, nil, nil)
	for _, n := range names {
		if e := signalError(s, n); e != "" {
			return precondition.Verdict{Error: "the `" + n + "` signal failed: " + e}
		}
	}
	return judge(s, false)
}

func signalError(s precondition.Signals, name string) string {
	switch name {
	case "runs":
		if s.Runs != nil {
			return s.Runs.Error
		}
	case "commits":
		if s.Commits != nil {
			return s.Commits.Error
		}
	case "issues":
		if s.Issues != nil {
			return s.Issues.Error
		}
	case "prs":
		if s.PRs != nil {
			return s.PRs.Error
		}
	case "conversationLogs":
		if s.ConversationLogs != nil {
			return s.ConversationLogs.Error
		}
	case "sharedMount":
		if s.SharedMount != nil {
			return s.SharedMount.Error
		}
	case "request":
		if s.Request != nil {
			return s.Request.Error
		}
	default:
		if m, ok := s.Extra[name].(map[string]string); ok {
			return m["error"]
		}
	}
	return ""
}

// Run is one scheduler run over an injected world: the listings, the
// plan, the repair writes, the ops, the forced wake and the drain gate.
func Run(in RunIn) (RunOut, error) {
	in.defaults()
	gh := in.Issues
	var out RunOut
	problem := func(s string) { out.Problems = append(out.Problems, s) }

	since := calendar.ISO(in.Now.Add(-signals.HorizonDays * 24 * time.Hour))
	endList := in.Phase("list")
	open, err := ListItems(gh, world.Query{State: "open"})
	if err != nil {
		return out, err
	}
	closed, err := ListItems(gh, world.Query{State: "closed", Since: since})
	if err != nil {
		return out, err
	}
	all := append(open, closed...)
	requests, err := ListMarked(gh)
	if err != nil {
		return out, err
	}
	known := map[int]string{}
	for _, i := range all {
		known[i.Number] = i.State
	}
	for _, n := range BlockersToResolve(all, requests, known) {
		i, err := gh.Issue(n)
		if err != nil {
			known[n] = ""
			continue
		}
		known[n] = i.State
	}
	liveness := map[int]time.Time{}
	agentComments := map[int][]world.Comment{}
	resolutions := map[int]string{}
	for _, i := range all {
		if i.State != "open" {
			continue
		}
		switch {
		case i.Is(workitem.StatusRunningExecutor):
			cs, err := gh.Comments(i.Number)
			if err == nil {
				liveness[i.Number] = queue.LastLivenessAt(cs)
			}
		case i.Is(workitem.StatusRunningAgent):
			cs, err := gh.Comments(i.Number)
			if err == nil {
				agentComments[i.Number] = cs
			}
		case i.Parked():
			f := workitem.ParseFields(i.Body)
			if f.EndsWhen == nil {
				continue
			}
			if _, done := resolutions[*f.EndsWhen]; done {
				continue
			}
			t, err := gh.Issue(*f.EndsWhen)
			switch {
			case err != nil || t.State != "closed":
				resolutions[*f.EndsWhen] = ""
			case t.MergedAt != "":
				resolutions[*f.EndsWhen] = "merged"
			default:
				resolutions[*f.EndsWhen] = "closed"
			}
		}
	}
	var done []workitem.Issue
	for _, i := range all {
		if i.State == "closed" && i.Is(workitem.StatusDone) {
			done = append(done, i)
		}
	}
	endList()

	var collect *signals.Collector
	if in.Collector != nil {
		collect = in.Collector(all)
	}
	endAsk := in.Phase("ask")
	unasked := map[string]string{}
	ops, asked, err := Plan(PlanIn{Tasks: in.Tasks, Items: all, Requests: requests, Now: in.Now, Disabled: in.Disabled,
		LivenessAt:   func(n int) time.Time { return liveness[n] },
		StateOf:      func(n int) string { return known[n] },
		Evaluate:     func(t taskspec.Task) precondition.Verdict { return in.evaluate(t, collect, unasked) },
		ProgressAt:   func(i workitem.Issue) time.Time { return queue.LastProgressAt(agentComments[i.Number]) },
		ResolutionOf: func(n int) string { return resolutions[n] },
		DoneAfter:    DoneRunLookup(done)})
	endAsk()
	if err != nil {
		return out, err
	}
	for k, a := range asked {
		if why, ok := unasked[a.Task]; ok {
			asked[k].Verdict = VerdictUnasked
			problem(fmt.Sprintf("%s: its own terms could not be asked, so nothing was filed for it (%s)", a.Task, why))
		}
	}
	out.Ops, out.Asked = ops, asked
	for _, a := range asked {
		line := "- asked " + a.Task + ": " + a.Verdict
		if a.Reason != "" {
			line += " — " + a.Reason
		}
		in.Log(line)
	}

	endRepair := in.Phase("repair")
	var repairOps []Op
	writesLabel := false
	for _, op := range ops {
		if op.IsRepair() {
			repairOps = append(repairOps, op)
			writesLabel = writesLabel || op.To != ""
		}
	}
	if len(repairOps) > 0 {
		if writesLabel {
			ensure(gh, workitem.QueueLabels, in.Log)
		}
		repaired := 0
		for _, op := range repairOps {
			if applyRepair(gh, op, in, agentComments, problem) {
				repaired++
			}
		}
		in.Log(fmt.Sprintf("- repair: %d of %d verdict(s) written", repaired, len(repairOps)))
	}
	endRepair()

	endDrain := in.Phase("drain")
	for _, op := range ops {
		if op.Kind == KindCreate || op.Kind == KindAdopt {
			ensure(gh, workitem.QueueLabels, in.Log)
			break
		}
	}
	for _, t := range in.Tasks {
		if t.Pack == taskspec.BuiltinPack && t.ID == taskspec.RequestTask {
			var origins []workitem.Label
			for _, l := range workitem.QueueLabels {
				if has(workitem.OriginLabels, l.Name) {
					origins = append(origins, l)
				}
			}
			ensure(gh, origins, in.Log)
			break
		}
	}
	var readied []int
	var minted []workitem.Issue
	for _, op := range ops {
		switch op.Kind {
		case KindCreate:
			n, err := gh.CreateIssue(op.Title, op.Body, op.Labels)
			if err != nil {
				in.Log(fmt.Sprintf("! could not create the work item for %s/%s: %v", op.Pack, op.Task, err))
				continue
			}
			readied = append(readied, n)
			minted = append(minted, workitem.Issue{Number: n, Title: op.Title, Body: op.Body, State: "open", Labels: op.Labels})
			in.Log(fmt.Sprintf("- created #%d %s/%s [%s]", n, op.Pack, op.Task, strings.Join(op.Labels, " ")))
		case KindReady:
			if err := queue.SwapStatus(gh, op.Issue, workitem.StatusBlocked, workitem.StatusReady); err != nil {
				problem(fmt.Sprintf("could not ready #%d (%v)", op.Issue, err))
				continue
			}
			readied = append(readied, op.Issue)
			in.Log(fmt.Sprintf("- readied #%d", op.Issue))
		case KindReclaim:
			if _, err := gh.Comment(op.Issue, workitem.EpisodeMarker+"\n"+op.Reason); err != nil {
				problem(fmt.Sprintf("could not reclaim #%d (%v)", op.Issue, err))
				continue
			}
			if err := queue.SwapStatus(gh, op.Issue, workitem.StatusRunningExecutor, op.To); err != nil {
				problem(fmt.Sprintf("could not reclaim #%d (%v)", op.Issue, err))
				continue
			}
			if op.To == workitem.StatusReady {
				readied = append(readied, op.Issue)
			}
			in.Log(fmt.Sprintf("- reclaimed #%d -> %s", op.Issue, op.To))
		case KindAdopt:
			adopt(gh, op, in.Log, problem)
		case KindDedupe, KindRetireOrphan:
			if err := closeRejected(gh, op.Issue, op.Reason); err != nil {
				problem(fmt.Sprintf("could not close #%d (%v)", op.Issue, err))
				continue
			}
			if op.Kind == KindDedupe {
				in.Log(fmt.Sprintf("- deduped #%d", op.Issue))
			} else {
				in.Log(fmt.Sprintf("- reaped #%d — %s/%s is not declared at HEAD", op.Issue, op.Pack, op.Task))
			}
		}
	}
	if len(ops) == 0 {
		in.Log("- nothing to do: no task said yes, nothing is marked, nothing is due to be readied, no claim is dead")
	}

	if strings.TrimSpace(in.Wake) != "" {
		listed, err := ListItems(gh, world.Query{State: "all", Since: since})
		if err != nil {
			return out, err
		}
		w := PlanWake(in.Wake, in.Tasks, WithOwnWrites(listed, minted))
		for _, t := range w.Wake {
			if err := items.Wake(gh, t.Issue, false, in.Now); err != nil {
				in.Log(fmt.Sprintf("! could not wake #%d %s: %v", t.Issue, t.ID, err))
				continue
			}
			readied = append(readied, t.Issue)
			in.Log(fmt.Sprintf("- woke #%d %s", t.Issue, t.ID))
		}
		if len(w.Create) > 0 {
			ensure(gh, workitem.QueueLabels, in.Log)
		}
		for _, c := range w.Create {
			n, err := gh.CreateIssue(workitem.Title{Pack: c.Pack, Task: c.Task}.String(),
				workitem.Body(workitem.BodySpec{TaskPath: c.TaskPath, Context: []string{ForcedWakeContext}, Woken: calendar.ISO(in.Now)}),
				[]string{workitem.OriginPlanned, workitem.StatusReady})
			if err != nil {
				in.Log(fmt.Sprintf("! could not create a work item for %s: %v", c.ID, err))
				problem(fmt.Sprintf("could not create a work item for %s (%v)", c.ID, err))
				continue
			}
			readied = append(readied, n)
			in.Log(fmt.Sprintf("- created #%d %s (forced: it had no open standing item)", n, c.ID))
		}
		for _, a := range w.Already {
			in.Log(fmt.Sprintf("- %s is already in flight on #%d — left alone", a.ID, a.Issue))
		}
		var missed []string
		for _, u := range w.Unmatched {
			in.Log(fmt.Sprintf(`! nothing woken for "%s": %s`, u.ID, u.Why))
			missed = append(missed, `"`+u.ID+`"`)
		}
		if len(missed) > 0 {
			problem("the wake matched nothing for " + strings.Join(missed, ", "))
		}
	}

	pickable, err := Announce(gh, in.Tasks, readied, in.Log, in.SetOutput)
	endDrain()
	out.Pickable = pickable
	return out, err
}

func ensure(gh world.Issues, labels []workitem.Label, log func(string)) {
	if err := gh.EnsureLabels(labels); err != nil {
		log(fmt.Sprintf("! could not ensure the queue's labels: %v", err))
	}
}

func closeRejected(gh world.Issues, n int, reason string) error {
	if _, err := gh.Comment(n, reason); err != nil {
		return err
	}
	if err := gh.AddLabel(n, workitem.StatusRejected); err != nil {
		return err
	}
	return gh.CloseIssue(n, "not_planned")
}

func applyRepair(gh world.Issues, op Op, in RunIn, agentComments map[int][]world.Comment, problem func(string)) bool {
	if op.Confirm != "" {
		var fresh *workitem.Issue
		if i, err := gh.Issue(op.Issue); err == nil {
			fresh = &i.Issue
		}
		if !StillHolds(op, fresh, in.Now, in.Tasks) {
			in.Log(fmt.Sprintf("- #%d settled between this run's read and its write - left alone", op.Issue))
			return false
		}
	}
	body := op.Body
	if body == "" && op.Note == "dead-agent" {
		body = DeadAgentComment(sessionNote(agentComments[op.Issue]), op.Wedged)
	}
	fail := func(err error) bool {
		problem(fmt.Sprintf("could not repair #%d (%s): %v", op.Issue, op.Rule, err))
		return false
	}
	if body != "" {
		if _, err := gh.Comment(op.Issue, body); err != nil {
			return fail(err)
		}
	}
	if op.From != "" {
		if err := queue.ClearStatus(gh, op.Issue, op.From); err != nil {
			return fail(err)
		}
	}
	if op.To != "" {
		if err := gh.AddLabel(op.Issue, op.To); err != nil {
			return fail(err)
		}
	}
	if op.Close != "" {
		if err := gh.CloseIssue(op.Issue, op.Close); err != nil {
			return fail(err)
		}
	}
	if op.ClearInReview != nil {
		if err := gh.RemoveLabel(op.ClearInReview.Issue, op.ClearInReview.Label); err != nil && !errors.Is(err, world.ErrGone) {
			problem(fmt.Sprintf("could not clear %s on #%d (%v)", op.ClearInReview.Label, op.ClearInReview.Issue, err))
		}
	}
	if op.Kind == KindNote {
		in.Log(fmt.Sprintf("- noted #%d (%s)", op.Issue, op.Rule))
	} else {
		line := fmt.Sprintf("- repaired #%d (%s)", op.Issue, op.Rule)
		if op.To != "" {
			line += " -> " + op.To
		}
		if op.Close != "" {
			line += " - closed " + op.Close
		}
		in.Log(line)
	}
	return true
}

// adopt writes the machine block first and the status second: a block
// with no status is re-adopted by the next run, while a status with no
// block would name no task.
func adopt(gh world.Issues, op Op, log func(string), problem func(string)) {
	if err := gh.SetIssueBody(op.Request, op.Body); err != nil {
		log(fmt.Sprintf("! could not adopt #%d: its body could not be written (%v)", op.Request, err))
		problem(fmt.Sprintf("could not adopt #%d (%v)", op.Request, err))
		return
	}
	if op.Origin != "" {
		if err := gh.AddLabel(op.Request, op.Origin); err != nil {
			problem(fmt.Sprintf("could not adopt #%d (%v)", op.Request, err))
			return
		}
	}
	if err := gh.AddLabel(op.Request, op.Status); err != nil {
		problem(fmt.Sprintf("could not adopt #%d (%v)", op.Request, err))
		return
	}
	var b strings.Builder
	b.WriteString("Queued: a run of `" + op.Task + "` for this issue")
	if op.Model != "" {
		b.WriteString(", at the `" + op.Model + "` family")
	}
	b.WriteString(".\n\n")
	refs := make([]string, len(op.BlockedBy))
	for k, n := range op.BlockedBy {
		refs[k] = fmt.Sprintf("#%d", n)
	}
	if len(refs) > 0 {
		b.WriteString("It is **blocked** on " + strings.Join(refs, ", ") + " — it enters the queue once they close.\n\n")
	}
	if op.NotBefore != "" {
		b.WriteString("It waits until " + op.NotBefore + " before entering the queue.\n\n")
	}
	if op.Merge != "" {
		b.WriteString("The run implements this issue and opens a pull request; it may land that pull request itself only when the diff sits inside the authorized policy (`" + op.Merge + "`), and leaves a wider one for review. ")
	} else {
		b.WriteString("The run implements this issue and opens a pull request for review — it never merges one. ")
	}
	if op.Ungated {
		b.WriteString("\n\nThe `Task:`/`Model:`/`Automerge:` fields in this body were ignored: they are honoured only for an author with push access on this repository, so this run takes the defaults. ")
	}
	b.WriteString("To withdraw the request before it starts, remove the `" + workitem.OriginAdHoc + "` mark and the status beside it.")
	if _, err := gh.Comment(op.Request, b.String()); err != nil {
		problem(fmt.Sprintf("could not comment on adopted #%d (%v)", op.Request, err))
	}
	model := op.Model
	if model == "" {
		model = "default model"
	}
	extra := ""
	if len(refs) > 0 {
		extra += ", blocked on " + strings.Join(refs, " ")
	}
	if op.Merge != "" {
		extra += ", may merge: " + op.Merge
	}
	log(fmt.Sprintf("- adopted #%d for %s (%s%s)", op.Request, op.Task, model, extra))
}

// Announce is the drain gate: the run's last act, whether it leaves
// anything for an executor to do, published as the `pickable` output.
func Announce(gh world.Issues, tasks []taskspec.Task, readied []int, log func(string), setOutput func(string, string) error) (int, error) {
	open, err := queue.ListOpen(gh)
	if err != nil {
		return 0, err
	}
	byID := index(tasks)
	byPath := map[string]string{}
	for _, t := range tasks {
		byPath[t.TaskPath()] = t.Path()
	}
	n := PickableCount(open, readied, queue.PickOpts{
		TaskAfter:   func(id string) []string { return byID[id].Decl.Strings("schedule_after") },
		ScheduledOf: ScheduledForTasks(tasks),
		PathTo:      func(p string) string { return byPath[p] },
	})
	value := "false"
	if n > 0 {
		value = "true"
		log(fmt.Sprintf("- %d item(s) pickable — the drain job dispatches an executor", n))
	} else {
		log("- nothing pickable — no executor is dispatched this run")
	}
	if err := setOutput("pickable", value); err != nil {
		log(fmt.Sprintf("! the drain gate could not be published (%v) — the drain job's own default decides", err))
	}
	return n, nil
}
