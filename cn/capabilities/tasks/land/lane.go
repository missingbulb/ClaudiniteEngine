package land

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/config"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/mergepolicy"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/workitem"
)

// CIWorkflow is the member workflow whose runs on the default branch the
// engine's update needs green before it acts.
const CIWorkflow = "claudinite-ci.yml"

// The member's two delivery preferences, as its tasks block spells them.
const (
	AutoMerge = config.AutoMerge
	Review    = config.Review
)

// WorkflowFile is one workflow as it exists on the delivered branch.
type WorkflowFile struct {
	Name, Content string
}

var (
	onKey      = regexp.MustCompile(`^(['"]?)on(['"]?)\s*:`)
	triggerKey = regexp.MustCompile(`^([A-Za-z_]+)\s*:`)
)

// WorkflowTriggers reads a workflow's top-level on: block textually: a
// block map of trigger keys (nested config skipped), an inline scalar or
// an inline list.
func WorkflowTriggers(yaml string) []string {
	lines := strings.Split(yaml, "\n")
	at := -1
	for i, l := range lines {
		if m := onKey.FindStringSubmatch(l); m != nil && m[1] == m[2] {
			at = i
			break
		}
	}
	if at < 0 {
		return nil
	}
	inline := onKey.ReplaceAllString(lines[at], "")
	if i := strings.Index(inline, "#"); i >= 0 {
		inline = inline[:i]
	}
	if inline = strings.TrimSpace(inline); inline != "" {
		var out []string
		for _, s := range strings.Split(strings.NewReplacer("[", "", "]", "").Replace(inline), ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	var out []string
	child := -1
	for _, line := range lines[at+1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if indent == 0 {
			break
		}
		if child < 0 {
			child = indent
		}
		if indent != child {
			continue
		}
		if m := triggerKey.FindStringSubmatch(trimmed); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

// DispatchPlan is which pull_request workflows the delivery can start
// (Dispatch) and which it cannot (Missing: no workflow_dispatch trigger).
type DispatchPlan struct {
	Dispatch, Missing []string
}

// CIDispatchPlan plans over the branch's workflow files; a workflow with
// no pull_request trigger is never touched.
func CIDispatchPlan(files []WorkflowFile) DispatchPlan {
	var p DispatchPlan
	for _, f := range files {
		ts := WorkflowTriggers(f.Content)
		if !has(ts, "pull_request") {
			continue
		}
		if has(ts, "workflow_dispatch") {
			p.Dispatch = append(p.Dispatch, f.Name)
		} else {
			p.Missing = append(p.Missing, f.Name)
		}
	}
	return p
}

// Gate is what the base branch requires before a merge.
type Gate string

// The three gates; unknown is never absent.
const (
	GatePresent Gate = "present"
	GateAbsent  Gate = "absent"
	GateUnknown Gate = "unknown"
)

// GatingRules are the ruleset rule types that queue a pull request.
var GatingRules = []string{"pull_request", "required_status_checks", "merge_queue", "required_deployments"}

// ClassifyMergeGate reads the base's protected flag (nil when unreadable)
// and the ruleset rule types that apply to it.
func ClassifyMergeGate(protected *bool, rules []string, rulesOK bool) Gate {
	if protected == nil {
		return GateUnknown
	}
	if *protected {
		return GatePresent
	}
	if !rulesOK {
		return GateUnknown
	}
	for _, r := range rules {
		if has(GatingRules, r) {
			return GatePresent
		}
	}
	return GateAbsent
}

// Action is how a delivered pull request lands.
type Action string

// The four shapes.
const (
	ActNone  Action = "none"
	ActMerge Action = "merge"
	ActLand  Action = "land"
	ActArm   Action = "arm"
)

// DeliveryAction: a review member's PR is left; with no pull_request CI
// it merges (an arm would be rejected in clean status); with CI and a
// base that requires nothing it verifies and lands on this run's evidence;
// behind a gate, or one unreadable, it arms.
func DeliveryAction(delivery string, hasPRCI bool, gate Gate) Action {
	switch {
	case delivery != AutoMerge:
		return ActNone
	case !hasPRCI:
		return ActMerge
	case gate == GateAbsent:
		return ActLand
	}
	return ActArm
}

// Run is a workflow run on a head sha.
type Run struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Event      string `json:"event"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

// RealFailures are the conclusions never merged over.
var RealFailures = []string{"failure", "timed_out", "cancelled", "startup_failure"}

// Disposition is a pull request judged by the runs on its head.
type Disposition string

// The four dispositions.
const (
	DispKeep  Disposition = "keep"
	DispWait  Disposition = "wait"
	DispClose Disposition = "close"
	DispMerge Disposition = "merge"
)

// PullDisposition: a review member's PR is kept; anything unfinished
// waits; a real failure, or no success to stand on, closes; else merge
// (a run parked at action_required never ran and is no failure).
func PullDisposition(delivery string, runs []Run) Disposition {
	if delivery != AutoMerge {
		return DispKeep
	}
	success := false
	for _, r := range runs {
		if r.Status != "completed" {
			return DispWait
		}
	}
	for _, r := range runs {
		if has(RealFailures, r.Conclusion) {
			return DispClose
		}
		success = success || r.Conclusion == "success"
	}
	if !success {
		return DispClose
	}
	return DispMerge
}

// The landing poll's two bounds: nothing visible yet, and a run seen
// executing.
const (
	Timeout         = 180 * time.Second
	InflightTimeout = 300 * time.Second
	PollEvery       = 5 * time.Second
)

// Attempt is one poll's verdict.
type Attempt string

// The three verdicts; this run's own PR is never closed.
const (
	AttemptMerge  Attempt = "merge"
	AttemptPoll   Attempt = "poll"
	AttemptGiveUp Attempt = "give-up"
)

// LandAttempt judges one poll of this run's PR: fewer visible runs than
// it started (a held action_required one not counted) polls to the
// short bound; a run still executing polls to the long one.
func LandAttempt(delivery string, runs []Run, expected int, elapsed time.Duration) Attempt {
	visible := 0
	for _, r := range runs {
		if r.Conclusion != "action_required" {
			visible++
		}
	}
	if visible < expected {
		if elapsed >= Timeout {
			return AttemptGiveUp
		}
		return AttemptPoll
	}
	switch PullDisposition(delivery, runs) {
	case DispMerge:
		return AttemptMerge
	case DispWait:
		if elapsed >= InflightTimeout {
			return AttemptGiveUp
		}
		return AttemptPoll
	}
	return AttemptGiveUp
}

// MergeReason says why the merge happened here rather than through the
// arm, naming the setting only where there is one.
func MergeReason(runs []Run) string {
	for _, r := range runs {
		if r.Conclusion == "action_required" {
			return "a pull_request run is still held at action_required (never ran) while the others passed" +
				" — check that the workflow may approve it (`actions: write`) and Settings → Actions → General → workflow-approval requirements"
		}
	}
	return "its checks concluded green and nothing on the base branch queues an auto-merge"
}

// FailureSummary says why a PR was left: the failing runs, else what was
// still moving at the bound, else no green run.
func FailureSummary(runs []Run) string {
	var failed, moving []string
	for _, r := range runs {
		if has(RealFailures, r.Conclusion) {
			failed = append(failed, r.Name+" "+r.Conclusion)
		}
		if r.Status != "" && r.Status != "completed" {
			moving = append(moving, r.Name+" "+r.Status)
		}
	}
	if len(failed) > 0 {
		return "failing CI: " + strings.Join(failed, ", ")
	}
	if len(moving) > 0 {
		return "still running at the landing bound: " + strings.Join(moving, ", ") + " — it lands next cycle"
	}
	return "no successful run on its head sha"
}

// PR is a delivered pull request at its current head, into Base.
type PR struct {
	Number  int
	NodeID  string
	HeadRef string
	HeadSHA string
	Base    string
}

// Merge is one squash merge pinned to the head the evidence was read on.
// Title replaces GitHub's squash title when set; Message is the squash
// body, the task trailer when a task wrote the PR.
type Merge struct {
	Number         int
	SHA            string
	Title, Message string
}

// StatusError is an API answer the lane reads the status of.
type StatusError struct {
	Status  int
	Message string
}

func (e *StatusError) Error() string {
	if e.Message == "" {
		return "HTTP " + strconv.Itoa(e.Status)
	}
	return strconv.Itoa(e.Status) + ": " + e.Message
}

// Merger is the half of the API a merge needs.
type Merger interface {
	MergePull(m Merge) error
	DeleteBranch(ref string) error
}

// Judgement is the policy a landing must pass: a task's automerge over
// the pull request's own diff, with the declared packs' rules. A nil
// Judgement judges nothing, for a caller that holds its own gate.
type Judgement struct {
	Automerge any
	Declared  mergepolicy.Declared
	// Diff reads the pull request's diff.
	Diff func(PR) ([]mergepolicy.Entry, error)
}

// RefusedError is a landing the policy does not authorize: the pull
// request stands for a person, and the policy is never widened to fit.
type RefusedError struct {
	PR  int
	Why string
}

func (e *RefusedError) Error() string {
	return fmt.Sprintf("PR #%d's diff is outside this task's automerge: %s", e.PR, e.Why)
}

// judge is nil when j authorizes pr's diff, a *RefusedError when it does
// not, and any other error when the diff could not be read.
func judge(j *Judgement, pr PR) error {
	if j == nil {
		return nil
	}
	entries, err := j.Diff(pr)
	if err != nil {
		return fmt.Errorf("could not read PR #%d's diff: %w", pr.Number, err)
	}
	if v := mergepolicy.Judge(j.Automerge, entries, j.Declared); !v.Mergeable {
		return &RefusedError{PR: pr.Number, Why: v.Why}
	}
	return nil
}

// Pinned squash-merges pr at the head the caller's evidence was read on,
// once j authorizes its diff, the task's trailer as the squash body when
// task is set, then deletes the branch. A merge refused (the policy, the
// head moved, a gate) is mergeErr; the branch left behind is tidyErr,
// never a failed merge.
func Pinned(m Merger, pr PR, title, task string, j *Judgement) (mergeErr, tidyErr error) {
	if err := judge(j, pr); err != nil {
		return err, nil
	}
	in := Merge{Number: pr.Number, SHA: pr.HeadSHA, Title: title}
	if task != "" {
		in.Message = workitem.TaskTrailer + ": " + task
	}
	if err := m.MergePull(in); err != nil {
		return err, nil
	}
	if pr.HeadRef == "" {
		return nil, nil
	}
	return nil, m.DeleteBranch(pr.HeadRef)
}

// API is the repository the lane delivers into.
type API interface {
	Merger
	WorkflowFiles(ref string) ([]WorkflowFile, error)
	DispatchWorkflow(name, ref string) error
	// BranchProtected is nil when the branch could not be read.
	BranchProtected(base string) (*bool, error)
	BranchRules(base string) ([]string, error)
	RunsForSHA(sha string) ([]Run, error)
	ApproveRun(id int64) error
	EnableAutoMerge(nodeID string) error
}

// Lane lands pull requests into one repository.
type Lane struct {
	API   API
	Now   func() time.Time
	Sleep func(time.Duration)
	Log   func(string)
}

// Delivered is the lane's answer: whether the PR merged in this run and
// which shape it took (none: a review member's PR stands, deliberately).
// Refused is the policy's reason when the PR's diff is outside it, and
// the lane then started, armed and merged nothing.
type Delivered struct {
	Merged  bool
	Action  Action
	Refused string
}

// Deliver lands a delivered PR under the member's delivery: it starts the
// PR's checks first (startCI: GitHub holds a job-token PR's pull_request
// runs for approval), reads the base's gate, then merges, lands on those
// runs' evidence, or arms with the landing poll as fallback; each merge it
// makes dispatches CI on base (DispatchBaseCI). task names the
// task whose trailer the merge commit carries ("" for none). j judges the
// diff before any of it: a refusal is Delivered.Refused.
func (l Lane) Deliver(pr PR, base, delivery, task string, j *Judgement) Delivered {
	if err := judge(j, pr); err != nil {
		var refused *RefusedError
		if errors.As(err, &refused) {
			l.Log(err.Error() + " — leaving it for review")
			return Delivered{Action: ActNone, Refused: refused.Why}
		}
		l.Log(err.Error() + " — leaving it for the next run")
		return Delivered{Action: ActNone}
	}
	hasCI, started := l.startCI(pr)
	gate := GateUnknown
	if p, err := l.API.BranchProtected(base); err == nil {
		rules, rerr := l.API.BranchRules(base)
		gate = ClassifyMergeGate(p, rules, rerr == nil)
	}
	action := DeliveryAction(delivery, hasCI, gate)
	pr.Base = base
	out := Delivered{Action: action}
	switch action {
	case ActMerge:
		if err := l.squash(pr, "", task); err != nil {
			l.Log(fmt.Sprintf("could not merge PR #%d (%v)", pr.Number, err))
		} else {
			out.Merged = true
			l.Log(fmt.Sprintf("merged PR #%d directly — this repo has no pull_request CI to gate on", pr.Number))
		}
	case ActLand:
		l.Log(fmt.Sprintf("%s requires nothing to merge — skipping the doomed auto-merge arm; waiting for its %d started run(s) and landing PR #%d here", base, started, pr.Number))
		out.Merged = l.landNow(pr, delivery, started, task)
	case ActArm:
		if pr.NodeID == "" {
			break
		}
		if err := l.API.EnableAutoMerge(pr.NodeID); err != nil {
			l.Log(fmt.Sprintf("could not arm auto-merge on PR #%d: %v — check Settings → General → \"Allow auto-merge\", and Settings → Actions → General for a workflow-approval requirement parking this PR's pull_request run at action_required. Waiting for its checks and landing it here.", pr.Number, err))
			out.Merged = l.landNow(pr, delivery, started, task)
		}
	}
	return out
}

// Merge is the lane's pinned-sha squash merge alone, for a caller that
// already holds the evidence: it merges pr at its head, tidies the branch
// and, when pr names its base, dispatches CI there.
func (l Lane) Merge(pr PR, title, task string) error {
	return l.squash(pr, title, task)
}

func (l Lane) squash(pr PR, title, task string) error {
	err, tidy := Pinned(l.API, pr, title, task, nil)
	if tidy != nil {
		l.Log(fmt.Sprintf("could not delete branch %s (%v)", pr.HeadRef, tidy))
	}
	if err == nil && pr.Base != "" {
		DispatchBaseCI(l.API, pr.Base, l.Log)
	}
	return err
}

// Dispatcher starts a workflow on a ref.
type Dispatcher interface {
	DispatchWorkflow(name, ref string) error
}

// DispatchBaseCI starts CIWorkflow on base after a merge made with the
// job's token: that merge's push starts no workflow, and the engine's
// update refuses to act while base's head has no CI run. A dispatch that
// fails leaves the merge standing and says why.
func DispatchBaseCI(d Dispatcher, base string, log func(string)) {
	if err := d.DispatchWorkflow(CIWorkflow, base); err != nil {
		hint := ""
		var se *StatusError
		if errors.As(err, &se) && se.Status == 403 {
			hint = " — the workflow needs `actions: write`"
		}
		log(fmt.Sprintf("could not dispatch %s on %s after the merge (%v)%s; the engine's update dispatches it when it finds no run", CIWorkflow, base, err, hint))
		return
	}
	log(fmt.Sprintf("dispatched %s on %s — a merge with the job's token starts no workflow there", CIWorkflow, base))
}

// startCI starts the PR's checks: with no pull_request workflow on its
// branch there are none; otherwise the pull_request runs GitHub holds on
// its head are approved (StartHeldCI), and only when none appears in the
// bound does it dispatch what the branch can dispatch. hasCI is true when
// the tree could not be read (CI is assumed, never merged past blind);
// started counts the runs the landing poll waits on.
func (l Lane) startCI(pr PR) (hasCI bool, started int) {
	files, err := l.API.WorkflowFiles(pr.HeadRef)
	if err != nil {
		l.Log(fmt.Sprintf("could not read the workflows on %s (%v) — assuming it has pull_request CI", pr.HeadRef, err))
	}
	plan := CIDispatchPlan(files)
	if err == nil && len(plan.Dispatch)+len(plan.Missing) == 0 {
		l.Log("no pull_request-triggered workflow — the delivered PR has no checks to wait for")
		return false, 0
	}
	h, herr := StartHeldCI(l.API, pr.HeadSHA, HeldWait, l.Sleep, l.Log)
	switch {
	case herr != nil:
		l.Log(fmt.Sprintf("could not read the runs on PR #%d's head (%v) — starting nothing", pr.Number, herr))
		return true, 0
	case h.Seen > 0:
		return true, h.Started
	case err != nil:
		return true, 0
	}
	l.Log(fmt.Sprintf("no pull_request run appeared on PR #%d's head within %s — dispatching its pull_request workflows instead", pr.Number, HeldWait))
	return true, l.dispatchCI(plan, pr.HeadRef)
}

// dispatchCI starts every workflow plan can dispatch on ref and names the
// ones it cannot, counting the dispatches GitHub accepted.
func (l Lane) dispatchCI(plan DispatchPlan, ref string) (started int) {
	for _, name := range plan.Missing {
		l.Log(name + " runs on pull_request but has no workflow_dispatch trigger — cannot start it on the delivered PR; add `workflow_dispatch:` under its `on:` to let the delivery run it")
	}
	for _, name := range plan.Dispatch {
		if err := l.API.DispatchWorkflow(name, ref); err != nil {
			hint := ""
			var se *StatusError
			if errors.As(err, &se) && se.Status == 403 {
				hint = " — the workflow needs `actions: write` (a read token 403s this POST silently)"
			}
			l.Log(fmt.Sprintf("could not dispatch %s on %s (%v)%s", name, ref, err, hint))
			continue
		}
		started++
		l.Log(fmt.Sprintf("dispatched %s on %s — the delivered PR gets its checks", name, ref))
	}
	return started
}

// landNow polls this PR's head until its runs conclude and merges on the
// evidence disposal would use a cycle later; every failure leaves the PR.
func (l Lane) landNow(pr PR, delivery string, expected int, task string) bool {
	start := l.Now()
	for {
		runs, err := l.API.RunsForSHA(pr.HeadSHA)
		if err != nil {
			l.Log(fmt.Sprintf("could not read this run's checks for PR #%d (%v) — leaving it for the next run", pr.Number, err))
			return false
		}
		switch LandAttempt(delivery, runs, expected, l.Now().Sub(start)) {
		case AttemptPoll:
			l.Sleep(PollEvery)
			continue
		case AttemptGiveUp:
			l.Log(fmt.Sprintf("PR #%d not landable this run (%s) — leaving it open for the next run to dispose of", pr.Number, FailureSummary(runs)))
			return false
		}
		if err := l.squash(pr, "", task); err != nil {
			l.Log(fmt.Sprintf("could not merge PR #%d (%v) — leaving it for the next run", pr.Number, err))
			return false
		}
		l.Log(fmt.Sprintf("merged PR #%d in-cycle — %s", pr.Number, MergeReason(runs)))
		return true
	}
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
