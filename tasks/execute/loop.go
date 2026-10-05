package execute

import (
	"fmt"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/tasks/land"
	"github.com/missingbulb/ClaudiniteEngine/tasks/precondition"
	"github.com/missingbulb/ClaudiniteEngine/tasks/queue"
	"github.com/missingbulb/ClaudiniteEngine/tasks/world"
)

// The outcomes a settled item reports, beside the two terminal statuses.
const (
	OutcomeNeedsHuman = "needs-human"
	OutcomeObsolete   = "obsolete"
	OutcomeAgent      = "agent"
	OutcomeUnknown    = "unknown"
	OutcomeRequeued   = "requeued"
	OutcomeReclaimed  = "reclaimed"
)

// Work is what code-work is handed: the item, its binding scope and the
// target the executor resolved.
type Work struct {
	Item    workitem.Issue
	Context []string
	Target  Target
}

// Triage is a worker's own verdict on its failure.
type Triage struct {
	Kind, Detail string
}

// Requeue is a worker's come-back-later ask; Until is "" when its instant
// could not be read.
type Requeue struct {
	Until, Reason string
}

// CodeWorkResult is one code-work run. MissingSecrets is a run that never
// started: the declared secrets this job does not carry. DeliveredPR is
// the pull request the run delivered, open or Merged.
type CodeWorkResult struct {
	OK             bool
	Why, Detail    string
	Triage         *Triage
	MissingSecrets []string
	AgentRequested bool
	Requeue        *Requeue
	DeliveredPR    int
	Merged         bool
	Branch         string
	Issue          int
	Reason         string
	// Said is what in-process code-work (the engine's own) reports, one
	// line each, carried onto the item's close.
	Said []string
}

// Delivered is what the run created, by identity: the agent's only source
// for those artifacts, and nothing when it created nothing.
func (r CodeWorkResult) Delivered() []string {
	var out []string
	if r.DeliveredPR != 0 {
		state := " (open)"
		if r.Merged {
			state = " (already merged — open your own PR for further work)"
		}
		out = append(out, fmt.Sprintf("PR: #%d%s", r.DeliveredPR, state))
	}
	if r.Branch != "" {
		out = append(out, "Branch: `"+r.Branch+"`")
	}
	if r.Issue != 0 {
		out = append(out, fmt.Sprintf("Issue: #%d — write this run's record there.", r.Issue))
	}
	return out
}

// Landed is the lane's answer for a delivered pull request.
type Landed struct {
	Merged bool
	Note   string
	// Refused is the policy's reason when the delivered diff is outside
	// the task's automerge.
	Refused string
}

// Invocation is one routine fire: OK started a session; Answered false is
// a fire whose outcome nobody learned.
type Invocation struct {
	OK                    bool
	Answered              bool
	SessionID, SessionURL string
	Error                 string
}

// In is one executor run: its reach and its seams.
type In struct {
	Issues world.Issues
	Pulls  world.Pulls
	Lane   land.Merger
	Tasks  []taskspec.Task

	ExecutorID, RunURL string
	Clock              world.Clock
	Draw               func() float64
	Heartbeat          time.Duration
	Ticker             queue.Ticker

	// Exists probes a task folder: one run drains several items from one
	// checkout, and an earlier item's work can delete a later one's task.
	Exists func(dir string) bool
	// Evaluate is the precondition asked again at the pick, for this
	// occurrence.
	Evaluate      func(t taskspec.Task, item workitem.Issue, at time.Time) precondition.Verdict
	ResolveTarget func(t taskspec.Task, at time.Time) Target
	CodeWork      func(t taskspec.Task, w Work) CodeWorkResult
	// Land takes the pull request code-work delivered through the landing
	// lane, as far as the task's policy and the member allow.
	Land   func(t taskspec.Task, pr int) Landed
	Invoke func(t taskspec.Task, item workitem.Issue, nonce string) Invocation
	Nonce  func(item int) string
	Cost   *queue.CostMeter
	Log    func(string)
}

// Settled is one item this run let go of, and how.
type Settled struct {
	Issue   int    `json:"issue"`
	Outcome string `json:"outcome"`
}

type run struct {
	In
	byID   map[string]taskspec.Task
	byPath map[string]string
	pick   queue.PickOpts
}

func (r *run) phase(name string) func() {
	if r.Cost == nil {
		return func() {}
	}
	return r.Cost.Phase(name)
}

func (r *run) nowISO() string { return r.Clock.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

// Loop drains the queue: pick a ready item, claim it by a verified lease,
// re-evaluate, run, and settle it, then pick again until nothing is
// pickable. An item this run let go of (a lost race, a reverted claim) is
// never picked again by it. Items settle one at a time.
func Loop(in In) ([]Settled, error) {
	r := &run{In: in, byID: map[string]taskspec.Task{}, byPath: map[string]string{}}
	for _, t := range in.Tasks {
		r.byID[t.Path()] = t
		r.byPath[t.TaskPath()] = t.Path()
	}
	r.pick = queue.PickOpts{
		TaskAfter: func(id string) []string { return r.byID[id].Decl.Strings("schedule_after") },
		ScheduledOf: func(id string) workitem.Scheduled {
			t, ok := r.byID[id]
			if !ok {
				return workitem.Unknown
			}
			return scheduledOf(t)
		},
		Draw:   in.Draw,
		PathTo: func(p string) string { return r.byPath[p] },
	}
	var done []Settled
	standDown := map[int]bool{}
	for {
		endPick := r.phase("pick")
		open, err := queue.ListOpen(r.Issues)
		if err != nil {
			endPick()
			return done, err
		}
		var candidate *workitem.Issue
		for _, i := range queue.PickOrder(open, r.pick) {
			if !standDown[i.Number] {
				c := i
				candidate = &c
				break
			}
		}
		endPick()
		if candidate == nil {
			return done, nil
		}

		endClaim := r.phase("claim")
		if err := queue.SwapStatus(r.Issues, candidate.Number, workitem.StatusReady, workitem.StatusRunningExecutor); err != nil {
			endClaim()
			return done, err
		}
		if _, err := r.Issues.Comment(candidate.Number, ClaimComment(r.ExecutorID, r.RunURL, r.nowISO())); err != nil {
			endClaim()
			return done, err
		}
		comments, err := r.Issues.Comments(candidate.Number)
		if err != nil {
			endClaim()
			return done, err
		}
		winner, mine := ClaimWinner(comments), mineOf(comments, r.ExecutorID)
		if winner == nil || mine == nil || winner.ID != mine.ID {
			if err := r.strike(mine); err != nil {
				endClaim()
				return done, err
			}
			standDown[candidate.Number] = true
			endClaim()
			r.Log(fmt.Sprintf("- #%d: another executor holds this episode's earliest claim — leaving it to them", candidate.Number))
			continue
		}

		others, err := r.withClaimIDs(candidate.Number)
		if err != nil {
			endClaim()
			return done, err
		}
		if ConflictsWithEarlierClaim(*candidate, winner.ID, others, r.pick) {
			if _, err := r.Issues.Comment(candidate.Number, workitem.EpisodeMarker+"\nReverting this claim: a conflicting item holds an earlier claim this cycle. Returning the item to the queue."); err != nil {
				endClaim()
				return done, err
			}
			if err := queue.SwapStatus(r.Issues, candidate.Number, workitem.StatusRunningExecutor, workitem.StatusReady); err != nil {
				endClaim()
				return done, err
			}
			standDown[candidate.Number] = true
			endClaim()
			r.Log(fmt.Sprintf("- #%d: reverted — a conflicting item claimed earlier", candidate.Number))
			continue
		}
		endClaim()

		outcome, err := r.execute(*candidate, *winner)
		if err != nil {
			return done, err
		}
		done = append(done, Settled{Issue: candidate.Number, Outcome: outcome})
	}
}

func (r *run) withClaimIDs(self int) ([]Claimed, error) {
	open, err := queue.ListOpen(r.Issues)
	if err != nil {
		return nil, err
	}
	out := make([]Claimed, 0, len(open))
	for _, i := range open {
		c := Claimed{Issue: i}
		if i.Number != self && queue.Running(i) {
			comments, err := r.Issues.Comments(i.Number)
			if err != nil {
				return nil, err
			}
			if w := ClaimWinner(comments); w != nil {
				c.ClaimID = w.ID
			}
		}
		out = append(out, c)
	}
	return out, nil
}

// holdsClaim is whether claim is still the episode's live claim.
func (r *run) holdsClaim(n int, claim world.Comment) (bool, error) {
	comments, err := r.Issues.Comments(n)
	if err != nil {
		return false, err
	}
	w := ClaimWinner(comments)
	return w != nil && w.ID == claim.ID, nil
}

// strike appends the episode marker to an executor's own claim when it
// lets go of an open item without closing it, so the spent claim never
// outranks the next claimant. Struck before the label swap: a crash
// between the two leaves a running item with a spent claim, which the
// leash recovers.
func (r *run) strike(claim *world.Comment) error {
	if claim == nil || strings.Contains(claim.Body, workitem.EpisodeMarker) {
		return nil
	}
	return r.Issues.EditComment(claim.ID, claim.Body+"\n\n"+workitem.EpisodeMarker+"\nThis claim is spent — the executor released this item without closing it.")
}

// record is the execution record (and the run's cost) a settling comment
// carries; status "" writes no execution record.
func (r *run) record(item workitem.Issue, id, status string) string {
	var lines []string
	if status != "" {
		pack, task := "", ""
		if id != "" {
			pack, task, _ = strings.Cut(id, "/")
		} else if t, ok := workitem.ParseTitle(item.Title); ok {
			pack, task = t.Pack, t.Task
		}
		if pack != "" {
			lines = append(lines, queue.RenderTaskExec(queue.TaskExec{Pack: pack, Task: task, Slot: fmt.Sprintf("#%d", item.Number), Status: status}))
		}
	}
	if r.Cost != nil {
		lines = append(lines, r.Cost.Record())
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n\n```\n" + strings.Join(lines, "\n") + "\n```"
}

// park converges an item to one park status with one comment, striking
// the claim first.
func (r *run) park(item workitem.Issue, id, from, to string, claim *world.Comment, body, status string) error {
	end := r.phase("converge")
	defer end()
	if err := r.strike(claim); err != nil {
		return err
	}
	if _, err := r.Issues.Comment(item.Number, body+r.record(item, id, status)); err != nil {
		return err
	}
	return queue.SwapStatus(r.Issues, item.Number, from, to)
}

// close writes the comment, leaves the status, adds the terminal and
// closes the issue it stands on; it writes to this item alone.
func (r *run) close(item workitem.Issue, id, from, outcome, reason, body, status string) error {
	end := r.phase("converge")
	defer end()
	if _, err := r.Issues.Comment(item.Number, body+r.record(item, id, status)); err != nil {
		return err
	}
	if err := queue.ClearStatus(r.Issues, item.Number, from); err != nil {
		return err
	}
	if err := r.Issues.AddLabel(item.Number, outcome); err != nil {
		return err
	}
	return r.Issues.CloseIssue(item.Number, reason)
}

func scheduledOf(t taskspec.Task) workitem.Scheduled {
	if t.Decl.IsScheduled() {
		return workitem.Yes
	}
	return workitem.No
}

func requeueHint() string { return workitem.RequeueHint }

// execute takes one claimed item from validation to a terminal state or a
// hand-off.
func (r *run) execute(item workitem.Issue, claim world.Comment) (string, error) {
	parsed, titled := workitem.ParseTitle(item.Title)
	taskPath := workitem.ParseBody(item.Body).TaskPath
	id := ""
	if titled {
		id = parsed.ID()
	} else {
		id = r.byPath[taskPath]
	}
	task, known := r.byID[id]
	running := workitem.StatusRunningExecutor

	if taskPath == "" || (!titled && id == "") {
		return OutcomeNeedsHuman, r.park(item, id, running, workitem.StatusNeedsHumanFailure, &claim,
			"This work item is malformed — its title or first body line does not name a task. Possible forgery; a human should look at it.", "invalid")
	}
	if !known {
		return OutcomeObsolete, r.close(item, id, running, workitem.StatusRejected, "not_planned",
			fmt.Sprintf("`%s` is not a task this repo carries at HEAD (the pack may be undeclared, or the task removed). Closing obsolete.", id), "task-gone")
	}
	if task.Dir != "" && r.Exists != nil && !r.Exists(task.Dir) {
		return OutcomeObsolete, r.close(item, id, running, workitem.StatusRejected, "not_planned",
			fmt.Sprintf("`%s` no longer exists in this checkout (`%s`) — it was removed while this run was in flight. Nothing ran. Closing obsolete.", id, task.TaskPath()), "task-gone")
	}
	if task.TaskPath() != taskPath {
		if named, ok := workitem.TaskIDFromPath(taskPath); ok && named.ID() == id {
			return OutcomeObsolete, r.close(item, id, running, workitem.StatusRejected, "not_planned",
				fmt.Sprintf("This item names `%s` at `%s`, where it no longer lives — the pack was renamed since the item was filed, and the task is at `%s` now. An item's stored path is never rewritten, so this one can never run. Closing obsolete; the scheduler files a fresh occurrence at the current path.", id, taskPath, task.TaskPath()), "task-gone")
		}
		return OutcomeNeedsHuman, r.park(item, id, running, workitem.StatusNeedsHumanFailure, &claim,
			fmt.Sprintf("This item's task path (`%s`) is not where `%s` lives at HEAD (`%s`). Not running it.", taskPath, id, task.TaskPath()), "invalid")
	}

	at := r.Clock.Now()
	verdict := r.Evaluate(task, item, at)
	if verdict.Error != "" {
		r.Log(fmt.Sprintf("! #%d %s: the precondition could not answer — %s", item.Number, id, verdict.Error))
		return OutcomeNeedsHuman, r.park(item, id, running, workitem.StatusNeedsHumanFailure, &claim,
			fmt.Sprintf("This run could not be decided: %s\n\nNothing ran and nothing was written. Re-queue this item (%s) once the cause has cleared.", verdict.Error, requeueHint()), "failed")
	}
	if !verdict.Go() {
		reason := verdict.Reason
		if reason == "" {
			reason = "no work"
		}
		body := "The precondition declined: " + reason
		if workitem.IsStandingItem(item.Title, scheduledOf(task)) {
			body += "\n\nThis task is asked again at the next scheduler run; a decline is recorded nowhere but here."
		}
		return OutcomeObsolete, r.close(item, id, running, workitem.StatusRejected, "not_planned", body, "success")
	}

	context := workitem.MergeContext(workitem.ContextLines(item.Body), verdict.Context)
	target := r.ResolveTarget(task, at)
	if target.Error != "" {
		r.Log(fmt.Sprintf("! #%d %s: the target could not be resolved — %s", item.Number, id, target.Error))
		return OutcomeNeedsHuman, r.park(item, id, running, workitem.StatusNeedsHumanFailure, &claim,
			fmt.Sprintf("This run could not be given a target: %s\n\nNothing ran and nothing was written. Re-queue this item (%s) once the cause has cleared.", target.Error, requeueHint()), "failed")
	}
	r.Log(fmt.Sprintf("- #%d %s: target — %s", item.Number, id, target.Reason))
	if target.Landed != 0 {
		CloseSuperseded(r.Issues, r.Pulls, r.Lane, target.Supersedes, target.Landed, r.Log)
		return workitem.StatusDone, r.close(item, id, running, workitem.StatusDone, "completed",
			fmt.Sprintf("Landed #%d, this task's previous delivery, which had concluded green and was never merged. The checkout this run holds predates that merge, so nothing else ran; the next occurrence converges from the moved base.", target.Landed), "success")
	}

	if task.Decl.DeclaresCodeWork() {
		return r.codeWork(item, task, id, claim, context, target)
	}
	if !task.Decl.RunsAgent() {
		return OutcomeNeedsHuman, r.park(item, id, running, workitem.StatusNeedsHumanFailure, &claim,
			"This task is agentless but declares no code_work, so there is nothing to run — a contract-forbidden shape that reached the queue.", "invalid")
	}
	return r.handOff(item, task, id, claim, context, CodeWorkResult{}, target)
}

func (r *run) codeWork(item workitem.Issue, task taskspec.Task, id string, claim world.Comment, context []string, target Target) (string, error) {
	running := workitem.StatusRunningExecutor
	endWork := r.phase("code-work")
	var result CodeWorkResult
	_ = queue.WithHeartbeat(func() error {
		result = r.CodeWork(task, Work{Item: item, Context: context, Target: target})
		return nil
	}, func(minutes int) error {
		_, err := r.Issues.Comment(item.Number, queue.HeartbeatComment(r.ExecutorID, r.nowISO(), minutes))
		return err
	}, r.Heartbeat, r.Clock, r.Ticker, r.Log)
	endWork()

	if held, err := r.holdsClaim(item.Number, claim); err != nil {
		return "", err
	} else if !held {
		r.Log(fmt.Sprintf("- #%d %s: reclaimed while this run's work step ran — another executor holds it now, leaving it to them", item.Number, id))
		return OutcomeReclaimed, nil
	}
	if len(result.MissingSecrets) > 0 {
		return OutcomeNeedsHuman, r.park(item, id, running, workitem.StatusNeedsHumanAction, &claim,
			fmt.Sprintf("This task declares repo Actions secrets that are not configured: %s. Set them in repo settings and re-queue this item (%s).", strings.Join(result.MissingSecrets, ", "), requeueHint()), "failed")
	}
	if !result.OK {
		body := "Code-work failed: " + result.Why
		if result.Triage != nil && result.Triage.Kind != "" {
			body += "\n\nThe worker asks for: **" + result.Triage.Kind + "**"
		}
		if result.Triage != nil && result.Triage.Detail != "" {
			body += "\n\nThe worker's own verdict: " + result.Triage.Detail
		}
		if result.Detail != "" {
			body += "\n\n```\n" + result.Detail + "\n```"
		}
		return OutcomeNeedsHuman, r.park(item, id, running, workitem.StatusNeedsHumanFailure, &claim, body, "failed")
	}

	// A pull request a task may not open is never landed: the ceiling
	// check below parks it with the pull request still open.
	if result.DeliveredPR != 0 && !result.Merged && r.Land != nil && !result.AgentRequested &&
		taskspec.OpensPullRequest(taskspec.CanonicalOutcome(task.Decl.Outcome())) {
		landed := r.Land(task, result.DeliveredPR)
		result.Merged = landed.Merged
		if landed.Note != "" {
			r.Log(fmt.Sprintf("- #%d %s: %s", item.Number, id, landed.Note))
		}
		if landed.Refused != "" {
			return OutcomeNeedsHuman, r.park(item, id, running, workitem.StatusNeedsHumanAction, &claim,
				fmt.Sprintf("Code-work opened #%d, and its diff is outside this task's automerge: %s.\n\nReview #%d and merge or close it, then close this item. The policy is the task's prediction of its change; it is not widened to fit.", result.DeliveredPR, landed.Refused, result.DeliveredPR), "")
		}
	}
	if result.DeliveredPR != 0 {
		check := land.VerifyOutcome(task.Decl.Outcome(), task.Decl["automerge"], true, result.Merged)
		if !check.OK {
			return OutcomeNeedsHuman, r.park(item, id, running, workitem.StatusNeedsHumanDecide, &claim,
				fmt.Sprintf("Code-work exceeded this task's declared ceiling: %s.\n\nWhat it left:\n%s\n\nDecide whether that stands, then close this item.", *check.Violation, bullets(result.Delivered())), "failed")
		}
	}
	if len(target.Supersedes) > 0 && result.DeliveredPR != 0 {
		CloseSuperseded(r.Issues, r.Pulls, r.Lane, target.Supersedes, result.DeliveredPR, r.Log)
		target.Supersedes = []int{}
	}
	if result.AgentRequested {
		return r.handOff(item, task, id, claim, context, result, target)
	}
	if result.Requeue != nil {
		if result.Requeue.Until == "" {
			reason := ""
			if result.Requeue.Reason != "" {
				reason = " (" + result.Requeue.Reason + ")"
			}
			return OutcomeNeedsHuman, r.park(item, id, running, workitem.StatusNeedsHumanFailure, &claim,
				fmt.Sprintf("Code-work asked to requeue this item but its `claudinite-requeue:` instant could not be read%s. Fix the worker's marker, then re-queue this item (%s).", reason, requeueHint()), "failed")
		}
		body := workitem.EditItemBody(item.Body, func(m string) string { return workitem.WithNotBefore(m, result.Requeue.Until) })
		if err := r.Issues.SetIssueBody(item.Number, body); err != nil {
			return "", err
		}
		if err := r.strike(&claim); err != nil {
			return "", err
		}
		if err := queue.SwapStatus(r.Issues, item.Number, running, workitem.StatusBlocked); err != nil {
			return "", err
		}
		why := ""
		if result.Requeue.Reason != "" {
			why = " — " + result.Requeue.Reason
		}
		r.Log(fmt.Sprintf("- #%d %s: requeued until %s%s", item.Number, id, result.Requeue.Until, why))
		return OutcomeRequeued, nil
	}
	if result.DeliveredPR != 0 && !result.Merged {
		return OutcomeNeedsHuman, r.park(item, id, running, workitem.StatusNeedsHumanApprove, &claim,
			fmt.Sprintf("Code-work did this run's work and opened a PR for you to approve:\n%s\n\nMerge or close #%d, then close this item. This task keeps running on schedule meanwhile.", bullets(result.Delivered()), result.DeliveredPR), "")
	}
	body := "Code-work did this run's work; no agent was needed."
	if d := result.Delivered(); len(d) > 0 {
		body = "Code-work did this run's work and left:\n" + bullets(d)
	}
	if len(result.Said) > 0 {
		body += "\n\n" + bullets(result.Said)
	}
	return workitem.StatusDone, r.close(item, id, running, workitem.StatusDone, "completed", body, "success")
}

func bullets(lines []string) string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = "- " + l
	}
	return strings.Join(out, "\n")
}

// HandoffComment is the hand-off's record on the item: the executor and
// the nonce the session proves its fire with.
func HandoffComment(executor, nonce string) string {
	return workitem.HandoffMarker + "\nHanded off by executor `" + executor + "` — invocation nonce `" + nonce + "`."
}

// handOff gives the item to an agent session: one fire per item, ever.
// The target, the scope and what code-work created land in the machine's
// half of the body first; then running-agent, the hand-off comment with
// the nonce, the fire.
func (r *run) handOff(item workitem.Issue, task taskspec.Task, id string, claim world.Comment, context []string, result CodeWorkResult, target Target) (string, error) {
	end := r.phase("hand-off")
	defer end()
	nonce := r.Nonce(item.Number)
	body := workitem.EditItemBody(item.Body, func(m string) string {
		out := m
		if target.Mode != "" {
			out = workitem.WithTarget(out, target.Item())
		}
		if len(context) > 0 {
			out = workitem.WithSection(out, "Context", context)
		}
		if d := result.Delivered(); len(d) > 0 {
			out = workitem.WithSection(out, workitem.DeliveredHeading, d)
		}
		if result.Reason != "" {
			out = workitem.WithSection(out, "Why the agent is here", []string{result.Reason})
		}
		return out
	})
	if err := r.Issues.SetIssueBody(item.Number, body); err != nil {
		return "", err
	}
	if err := queue.SwapStatus(r.Issues, item.Number, workitem.StatusRunningExecutor, workitem.StatusRunningAgent); err != nil {
		return "", err
	}
	if _, err := r.Issues.Comment(item.Number, HandoffComment(r.ExecutorID, nonce)+r.record(item, id, "")); err != nil {
		return "", err
	}
	agent := workitem.StatusRunningAgent
	inv := r.Invoke(task, item, nonce)
	switch {
	case inv.OK:
		started := "Agent session started."
		if inv.SessionURL != "" {
			started = "Agent session started: " + inv.SessionURL + "."
		}
		if _, err := r.Issues.Comment(item.Number, started); err != nil {
			return "", err
		}
		sid := ""
		if inv.SessionID != "" {
			sid = " (" + inv.SessionID + ")"
		}
		r.Log(fmt.Sprintf("- #%d %s: handed off%s", item.Number, id, sid))
		return OutcomeAgent, nil
	case inv.Answered:
		return OutcomeNeedsHuman, r.park(item, id, agent, workitem.StatusNeedsHumanAction, &claim,
			fmt.Sprintf("Could not start an agent session: %s\n\nNo session was started. Fix the invocation endpoint, then re-queue this item (%s).", inv.Error, requeueHint()), "failed")
	}
	if _, err := r.Issues.Comment(item.Number, "The agent invocation got no answer: "+inv.Error+"\n\n"+
		"The session may or may not have started, so nothing here re-tries it — a second call could put two sessions on this item. "+
		"If a session did start it will converge this item; if it did not, the repair phase's agent leash parks it for a human within a few hours."); err != nil {
		return "", err
	}
	r.Log(fmt.Sprintf("! #%d %s: invocation unanswered — left with the agent, leash decides — %s", item.Number, id, inv.Error))
	return OutcomeUnknown, nil
}
