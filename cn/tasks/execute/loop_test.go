package execute

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/precondition"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/queue"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/sim"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/world"
)

var loopNow = time.Date(2026, 8, 14, 4, 20, 0, 0, time.UTC)

type loopHarness struct {
	t    *testing.T
	gh   *sim.GitHub
	repo *sim.Repo
	gone map[string]bool
	logs []string
}

func newLoop(t *testing.T) *loopHarness {
	return &loopHarness{t: t, gh: sim.NewGitHub(sim.NewClock(loopNow)), repo: sim.NewRepo(), gone: map[string]bool{}}
}

func itemBody(task string) string {
	return "packs/acme-pack/tasks/" + task + "/task.md\n\nExecute the Claudinite task above.\n"
}

func (h *loopHarness) item(n int, task string, labels ...string) {
	h.itemWith(n, task, itemBody(task), labels...)
}

func (h *loopHarness) itemWith(n int, task, body string, labels ...string) {
	if len(labels) == 0 {
		labels = []string{workitem.StatusReady}
	}
	h.gh.Seed(sim.StoredIssue{Issue: workitem.Issue{Number: n, Title: "[claudinite-work] acme-pack/" + task, Body: body, Labels: labels}})
}

func (h *loopHarness) marked(n int, task string, labels ...string) {
	body := fmt.Sprintf("Please do the thing.\n\n<!-- claudinite-item -->\npacks/acme-pack/tasks/%s/task.md\n\nRequest: #%d\n<!-- /claudinite-item -->\n", task, n)
	h.gh.Seed(sim.StoredIssue{Issue: workitem.Issue{Number: n, Title: "A thing to do", Body: body,
		Labels: append([]string{workitem.OriginAdHoc}, labels...)}})
}

func (h *loopHarness) get(n int) sim.StoredIssue {
	h.t.Helper()
	i, ok := h.gh.Get(n)
	if !ok {
		h.t.Fatalf("no #%d", n)
	}
	return i
}

func (h *loopHarness) last(n int) string {
	c := h.get(n).Comments
	if len(c) == 0 {
		return ""
	}
	return c[len(c)-1].Body
}

func loopTask(id string, decl map[string]any) taskspec.Task {
	d := map[string]any{"id": id, "trigger": "schedule", "agent_model": "sonnet", "preconditions": []any{"gate"}, "expected_outcome": "fresh_pr"}
	for k, v := range decl {
		d[k] = v
	}
	// As a declaration file reads: numbers are float64.
	raw, _ := json.Marshal(d)
	d = map[string]any{}
	_ = json.Unmarshal(raw, &d)
	return taskspec.Task{Pack: "acme-pack", ID: id, Dir: "/tasks/" + id, Rel: "packs/acme-pack/tasks/" + id,
		Decl: taskspec.Normalize(d).(taskspec.Decl)}
}

func agentless(id string) taskspec.Task {
	return loopTask(id, map[string]any{"agent_model": "none", "code_work": "node w.mjs", "code_work_timeout": 60})
}

func goes() precondition.Verdict { yes := true; return precondition.Verdict{Run: &yes} }

func declines(reason string) precondition.Verdict {
	no := false
	return precondition.Verdict{Run: &no, Reason: reason}
}

func ascending() func() float64 {
	n := 0.0
	return func() float64 { n++; return n / 1000 }
}

type noTicker struct{}

func (noTicker) Every(time.Duration, func()) func() { return func() {} }

func (h *loopHarness) in(tasks []taskspec.Task) In {
	return In{
		Issues: h.gh, Pulls: h.repo, Lane: h.repo, Tasks: tasks,
		ExecutorID: "E1", Clock: h.gh.Clock, Draw: ascending(), Ticker: noTicker{},
		Exists:   func(dir string) bool { return !h.gone[dir] },
		Evaluate: func(taskspec.Task, workitem.Issue, time.Time) precondition.Verdict { return goes() },
		ResolveTarget: func(t taskspec.Task, at time.Time) Target {
			return Target{Mode: ModeFresh, Branch: MintBranch(t.Path(), at, "abc"), Supersedes: []int{}, Reason: "r"}
		},
		CodeWork: func(taskspec.Task, Work) CodeWorkResult { return CodeWorkResult{OK: true} },
		Invoke: func(taskspec.Task, workitem.Issue, string) Invocation {
			return Invocation{OK: true, Answered: true, SessionID: "s-1"}
		},
		Nonce: func(n int) string { return fmt.Sprintf("%d-nonce", n) },
		Log:   func(s string) { h.logs = append(h.logs, s) },
	}
}

func (h *loopHarness) drive(tasks []taskspec.Task, over ...func(*In)) []Settled {
	h.t.Helper()
	in := h.in(tasks)
	for _, o := range over {
		o(&in)
	}
	done, err := Loop(in)
	if err != nil {
		h.t.Fatal(err)
	}
	return done
}

func settled(pairs ...any) []Settled {
	out := []Settled{}
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, Settled{Issue: pairs[i].(int), Outcome: pairs[i+1].(string)})
	}
	return out
}

func (h *loopHarness) wants(n int, state string, labels ...string) {
	h.t.Helper()
	i := h.get(n)
	if i.State != state {
		h.t.Errorf("#%d state %s, want %s", n, i.State, state)
	}
	for _, l := range labels {
		if !slices.Contains(i.Labels, l) {
			h.t.Errorf("#%d labels %v lack %s", n, i.Labels, l)
		}
	}
}

func TestCodeWorkIsHandedTheResolvedTarget(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	var seen []Target
	h.drive([]taskspec.Task{agentless("a")}, func(in *In) {
		in.CodeWork = func(_ taskspec.Task, w Work) CodeWorkResult {
			seen = append(seen, w.Target)
			return CodeWorkResult{OK: true}
		}
	})
	if len(seen) != 1 || seen[0].Mode != ModeFresh || !strings.HasPrefix(seen[0].Branch, "claudinite/acme-pack/a/2026-08-14-") {
		t.Errorf("%+v", seen)
	}
}

func TestASupersedeRunClosesItsIncumbentsOnceItsOwnExists(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	h.gh.Seed(sim.StoredIssue{Issue: workitem.Issue{Number: 3, Title: "old"}, PullRequest: true})
	h.repo.Pulls = []world.Pull{{Number: 3, HeadRef: "claudinite/acme-pack/a/2026-08-13-old", HeadSHA: "x", State: "open"}}
	h.drive([]taskspec.Task{loopTask("a", map[string]any{"agent_model": "none", "code_work": "w", "code_work_timeout": 60, "expected_outcome": "supersede_existing_pr"})}, func(in *In) {
		in.ResolveTarget = func(taskspec.Task, time.Time) Target {
			return Target{Mode: ModeFresh, Branch: "claudinite/acme-pack/a/2026-08-14-new", Supersedes: []int{3}, Reason: "r"}
		}
		in.CodeWork = func(taskspec.Task, Work) CodeWorkResult { return CodeWorkResult{OK: true, DeliveredPR: 7} }
	})
	if p, _ := h.repo.Pull(3); p.State != "closed" {
		t.Errorf("incumbent %s", p.State)
	}
	if !strings.Contains(h.last(3), "#7") {
		t.Error(h.last(3))
	}
	h.wants(1, "open", workitem.StatusNeedsHumanApprove)
}

func TestASupersedeRunThatDeliveredNothingLeavesItsIncumbents(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	h.repo.Pulls = []world.Pull{{Number: 3, HeadRef: "claudinite/acme-pack/a/2026-08-13-old", State: "open"}}
	h.drive([]taskspec.Task{agentless("a")}, func(in *In) {
		in.ResolveTarget = func(taskspec.Task, time.Time) Target {
			return Target{Mode: ModeFresh, Branch: "b", Supersedes: []int{3}, Reason: "r"}
		}
	})
	if p, _ := h.repo.Pull(3); p.State != "open" {
		t.Errorf("incumbent %s", p.State)
	}
	h.wants(1, "closed", workitem.StatusDone)
}

func TestALandedIncumbentEndsTheOccurrenceWithoutRunningTheWork(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	h.repo.Pulls = []world.Pull{{Number: 2, HeadRef: "claudinite/acme-pack/a/2026-08-12-older", State: "open"}}
	ran := 0
	done := h.drive([]taskspec.Task{agentless("a")}, func(in *In) {
		in.ResolveTarget = func(taskspec.Task, time.Time) Target {
			return Target{Mode: ModeNone, Supersedes: []int{2}, Landed: 5, Reason: "landed #5"}
		}
		in.CodeWork = func(taskspec.Task, Work) CodeWorkResult { ran++; return CodeWorkResult{OK: true} }
	})
	if ran != 0 || !reflect.DeepEqual(done, settled(1, workitem.StatusDone)) {
		t.Errorf("ran %d, %v", ran, done)
	}
	h.wants(1, "closed", workitem.StatusDone)
	if !strings.Contains(h.last(1), "#5") || !strings.Contains(h.last(1), "claudinite-task-exec v1 acme-pack/a [#1] success") {
		t.Error(h.last(1))
	}
	if p, _ := h.repo.Pull(2); p.State != "closed" {
		t.Error("the older incumbent is superseded by the landed one")
	}
}

func TestTheHandOffStampsTheTargetOnTheItem(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	h.drive([]taskspec.Task{loopTask("a", nil)}, func(in *In) {
		in.ResolveTarget = func(taskspec.Task, time.Time) Target {
			return Target{Mode: ModeFresh, Branch: "claudinite/acme-pack/a/2026-08-14-new", Supersedes: []int{3, 4}, Reason: "r"}
		}
	})
	f := workitem.ParseFields(h.get(1).Body)
	if workitem.Str(f.TargetBranch) != "claudinite/acme-pack/a/2026-08-14-new" || f.TargetPR != nil || !reflect.DeepEqual(f.Supersedes, []int{3, 4}) {
		t.Errorf("%+v", f)
	}
}

func TestASupersedeCodeWorkPerformedIsNotHandedToTheAgentAgain(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	h.repo.Pulls = []world.Pull{{Number: 3, HeadRef: "claudinite/acme-pack/a/2026-08-13-old", State: "open"}}
	h.drive([]taskspec.Task{loopTask("a", map[string]any{"code_work": "w", "code_work_timeout": 60})}, func(in *In) {
		in.ResolveTarget = func(taskspec.Task, time.Time) Target {
			return Target{Mode: ModeFresh, Branch: "claudinite/acme-pack/a/2026-08-14-new", Supersedes: []int{3}, Reason: "r"}
		}
		in.CodeWork = func(taskspec.Task, Work) CodeWorkResult {
			return CodeWorkResult{OK: true, AgentRequested: true, DeliveredPR: 7}
		}
	})
	if p, _ := h.repo.Pull(3); p.State != "closed" {
		t.Error("not superseded")
	}
	f := workitem.ParseFields(h.get(1).Body)
	if len(f.Supersedes) != 0 || workitem.Str(f.TargetBranch) != "claudinite/acme-pack/a/2026-08-14-new" {
		t.Errorf("%+v", f)
	}
	if !strings.Contains(h.get(1).Body, "PR: #7 (open)") {
		t.Error("the delivered PR is not on the item:", h.get(1).Body)
	}
}

func TestAnUnresolvableTargetParksAndNothingRuns(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	ran := 0
	done := h.drive([]taskspec.Task{agentless("a")}, func(in *In) {
		in.ResolveTarget = func(taskspec.Task, time.Time) Target {
			return Target{Error: "could not list the open pull requests (500)"}
		}
		in.CodeWork = func(taskspec.Task, Work) CodeWorkResult { ran++; return CodeWorkResult{OK: true} }
	})
	if ran != 0 || !reflect.DeepEqual(done, settled(1, OutcomeNeedsHuman)) {
		t.Errorf("%d %v", ran, done)
	}
	h.wants(1, "open", workitem.StatusNeedsHumanFailure)
	if !strings.Contains(h.last(1), "could not list") {
		t.Error(h.last(1))
	}
}

func TestAgentlessCodeWorkClosesDone(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	done := h.drive([]taskspec.Task{agentless("a")})
	if !reflect.DeepEqual(done, settled(1, workitem.StatusDone)) {
		t.Error(done)
	}
	h.wants(1, "closed", workitem.StatusDone)
	if slices.Contains(h.get(1).Labels, workitem.StatusRunningExecutor) {
		t.Error("still running")
	}
	if !strings.Contains(h.last(1), "claudinite-task-exec v1 acme-pack/a [#1] success") {
		t.Error(h.last(1))
	}
	if h.get(1).StateReason != "completed" {
		t.Error(h.get(1).StateReason)
	}
}

func TestCodeWorkThatOpenedAPRParksForApprovalWithNoRecord(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	h.drive([]taskspec.Task{agentless("a")}, func(in *In) {
		in.CodeWork = func(taskspec.Task, Work) CodeWorkResult { return CodeWorkResult{OK: true, DeliveredPR: 7} }
	})
	h.wants(1, "open", workitem.StatusNeedsHumanApprove)
	if !strings.Contains(h.last(1), "#7") || strings.Contains(h.last(1), "claudinite-task-exec") {
		t.Error(h.last(1))
	}
}

func TestCodeWorkThatDeliveredNoOpenPRStillCloses(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	h.drive([]taskspec.Task{agentless("a")}, func(in *In) {
		in.CodeWork = func(taskspec.Task, Work) CodeWorkResult { return CodeWorkResult{OK: true, Branch: "x"} }
	})
	h.wants(1, "closed", workitem.StatusDone)
	if !strings.Contains(h.last(1), "Branch: `x`") {
		t.Error(h.last(1))
	}
}

// The lane lands what code-work delivered when the task's policy and the
// member allow; a merged delivery asks nobody for anything.
func TestADeliveredPRTheLaneMergedClosesDone(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	var landed []int
	h.drive([]taskspec.Task{loopTask("a", map[string]any{"agent_model": "none", "code_work": "w", "code_work_timeout": 60, "automerge": []any{"code"}})}, func(in *In) {
		in.CodeWork = func(taskspec.Task, Work) CodeWorkResult { return CodeWorkResult{OK: true, DeliveredPR: 7} }
		in.Land = func(_ taskspec.Task, pr int) Landed { landed = append(landed, pr); return Landed{Merged: true} }
	})
	if !reflect.DeepEqual(landed, []int{7}) {
		t.Error(landed)
	}
	h.wants(1, "closed", workitem.StatusDone)
	if !strings.Contains(h.last(1), "already merged") {
		t.Error(h.last(1))
	}
}

// What in-process code-work said (the update's verdicts) closes the item
// with it.
func TestWhatCodeWorkSaidClosesTheItem(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	h.drive([]taskspec.Task{agentless("a")}, func(in *In) {
		in.CodeWork = func(taskspec.Task, Work) CodeWorkResult {
			return CodeWorkResult{OK: true, Said: []string{"cn update engine: opened #4 for 1.2.0", "cn update packs: up to date"}}
		}
	})
	h.wants(1, "closed", workitem.StatusDone)
	if last := h.last(1); !strings.Contains(last, "- cn update engine: opened #4 for 1.2.0\n- cn update packs: up to date") {
		t.Error(last)
	}
}

// The engine's own update runs from its item at the engine's path and
// closes on its verdicts.
func TestTheEnginesUpdateRunsAndClosesOnItsVerdicts(t *testing.T) {
	all, errs := taskspec.Discover(t.TempDir(), nil)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	h := newLoop(t)
	h.gh.Seed(sim.StoredIssue{Issue: workitem.Issue{Number: 1, Title: "[claudinite-work] engine/update",
		Body: taskspec.UpdateTaskPath + "\n\nExecute the Claudinite task above.\n", Labels: []string{workitem.StatusReady}}})
	var ran []string
	h.drive(all, func(in *In) {
		in.CodeWork = func(t taskspec.Task, _ Work) CodeWorkResult {
			ran = append(ran, t.Path())
			return CodeWorkResult{OK: true, Said: []string{"cn update engine: up to date", "cn update packs: up to date"}}
		}
	})
	if !reflect.DeepEqual(ran, []string{"engine/update"}) {
		t.Fatalf("ran %v", ran)
	}
	h.wants(1, "closed", workitem.StatusDone)
	if !strings.Contains(h.last(1), "- cn update packs: up to date") {
		t.Error(h.last(1))
	}
}

// An engine update whose PR carries staged workflows hands that PR, not a
// branch the resolver minted, to the agent stage; one with none closes.
func TestTheEnginesUpdateHandsItsOwnPRToTheAgentStage(t *testing.T) {
	all, errs := taskspec.Discover(t.TempDir(), nil)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	h := newLoop(t)
	h.gh.Seed(sim.StoredIssue{Issue: workitem.Issue{Number: 1, Title: "[claudinite-work] engine/update",
		Body: taskspec.UpdateTaskPath + "\n\nExecute the Claudinite task above.\n", Labels: []string{workitem.StatusReady}}})
	invoked := 0
	h.drive(all, func(in *In) {
		in.ResolveTarget = func(taskspec.Task, time.Time) Target {
			return Target{Mode: ModeFresh, Branch: "claudinite/engine/update/2026-10-06-x", Supersedes: []int{}, Reason: "r"}
		}
		in.CodeWork = func(taskspec.Task, Work) CodeWorkResult {
			return CodeWorkResult{OK: true, AgentRequested: true, DeliveredPR: 5, Branch: "claudinite/engine-1.61006.1",
				Reason: "staged workflows", HandOff: &Target{Mode: ModeAmend, Branch: "claudinite/engine-1.61006.1", PR: 5, Supersedes: []int{}}}
		}
		in.Invoke = func(taskspec.Task, workitem.Issue, string) Invocation {
			invoked++
			return Invocation{OK: true, Answered: true}
		}
	})
	h.wants(1, "open", workitem.StatusRunningAgent)
	f := workitem.ParseFields(h.get(1).Body)
	if workitem.Str(f.TargetBranch) != "claudinite/engine-1.61006.1" || f.TargetPR == nil || *f.TargetPR != 5 || invoked != 1 {
		t.Errorf("invoked %d, %+v\n%s", invoked, f, h.get(1).Body)
	}
}

// A delivery whose diff the task's policy does not authorize stands for a
// person: the item parks for action with the policy's reason.
func TestADeliveryOutsideThePolicyParksForAction(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	h.drive([]taskspec.Task{loopTask("a", map[string]any{"agent_model": "none", "code_work": "w", "code_work_timeout": 60, "automerge": []any{"doc-changes"}})}, func(in *In) {
		in.CodeWork = func(taskspec.Task, Work) CodeWorkResult { return CodeWorkResult{OK: true, DeliveredPR: 7} }
		in.Land = func(taskspec.Task, int) Landed { return Landed{Refused: "src/main.go is covered by no rule"} }
	})
	h.wants(1, "open", workitem.StatusNeedsHumanAction)
	if last := h.last(1); !strings.Contains(last, "src/main.go is covered by no rule") || !strings.Contains(last, "#7") {
		t.Error(last)
	}
}

// A merge the declared ceiling never allowed parks for a decision.
func TestAMergeBeyondTheCeilingParksForADecision(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	h.drive([]taskspec.Task{agentless("a")}, func(in *In) {
		in.CodeWork = func(taskspec.Task, Work) CodeWorkResult {
			return CodeWorkResult{OK: true, DeliveredPR: 7, Merged: true}
		}
	})
	h.wants(1, "open", workitem.StatusNeedsHumanDecide)
	if !strings.Contains(h.last(1), "ceiling") {
		t.Error(h.last(1))
	}
}

func TestAMarkedIssueClosesLikeAnyOtherDoneItemAndKeepsItsOrigin(t *testing.T) {
	h := newLoop(t)
	h.marked(1, "a", workitem.StatusReady)
	done := h.drive([]taskspec.Task{agentless("a")})
	if !reflect.DeepEqual(done, settled(1, workitem.StatusDone)) {
		t.Error(done)
	}
	h.wants(1, "closed", workitem.StatusDone, workitem.OriginAdHoc)
}

func TestAMarkedIssueTheGateDeclinedIsRejectedAndClosed(t *testing.T) {
	h := newLoop(t)
	h.marked(2, "a", workitem.StatusReady)
	done := h.drive([]taskspec.Task{loopTask("a", nil)}, func(in *In) {
		in.Evaluate = func(taskspec.Task, workitem.Issue, time.Time) precondition.Verdict { return declines("nothing to do") }
	})
	if !reflect.DeepEqual(done, settled(2, OutcomeObsolete)) {
		t.Error(done)
	}
	h.wants(2, "closed", workitem.StatusRejected)
	if h.get(2).StateReason != "not_planned" {
		t.Error(h.get(2).StateReason)
	}
}

func TestAFailedCodeWorkParksAtFailureCarryingTheWorkersVerdict(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	h.drive([]taskspec.Task{agentless("a")}, func(in *In) {
		in.CodeWork = func(taskspec.Task, Work) CodeWorkResult {
			return CodeWorkResult{Why: "code-work exited 1", Triage: &Triage{Kind: "action", Detail: "PAT lacks Actions: write"}, Detail: "stack"}
		}
	})
	h.wants(1, "open", workitem.StatusNeedsHumanFailure)
	body := h.last(1)
	for _, want := range []string{"PAT lacks Actions: write", "**action**", "stack", "claudinite-task-exec v1 acme-pack/a [#1] failed"} {
		if !strings.Contains(body, want) {
			t.Errorf("lacks %q: %s", want, body)
		}
	}
	if slices.Contains(h.get(1).Labels, workitem.StatusNeedsHumanAction) {
		t.Error("the worker downgraded its own failure")
	}
}

func TestARequeueReArmsNotBeforeReturnsToBlockedAndStrikesTheClaim(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	done := h.drive([]taskspec.Task{agentless("a")}, func(in *In) {
		in.CodeWork = func(taskspec.Task, Work) CodeWorkResult {
			return CodeWorkResult{OK: true, Requeue: &Requeue{Until: "2026-08-15T04:20:00.000Z", Reason: "not yet live"}}
		}
	})
	if !reflect.DeepEqual(done, settled(1, OutcomeRequeued)) {
		t.Error(done)
	}
	h.wants(1, "open", workitem.StatusBlocked)
	i := h.get(1)
	for _, l := range i.Labels {
		if strings.HasPrefix(l, workitem.ParkPrefix) || l == workitem.StatusRunningExecutor {
			t.Error(i.Labels)
		}
	}
	if workitem.ParseBody(i.Body).NotBefore != "2026-08-15T04:20:00.000Z" {
		t.Error(i.Body)
	}
	for _, c := range i.Comments {
		if strings.Contains(c.Body, workitem.ClaimMarker) && !strings.Contains(c.Body, workitem.EpisodeMarker) {
			t.Error("the spent claim was not struck:", c.Body)
		}
	}
}

func TestARequeueOnAMarkedIssueStampsTheMachineBlock(t *testing.T) {
	h := newLoop(t)
	h.marked(3, "a", workitem.StatusReady)
	h.drive([]taskspec.Task{agentless("a")}, func(in *In) {
		in.CodeWork = func(taskspec.Task, Work) CodeWorkResult {
			return CodeWorkResult{OK: true, Requeue: &Requeue{Until: "2026-08-15T04:20:00.000Z"}}
		}
	})
	body := h.get(3).Body
	if !strings.HasPrefix(body, "Please do the thing.") || workitem.ParseBody(body).NotBefore != "2026-08-15T04:20:00.000Z" {
		t.Error(body)
	}
	open, close := strings.Index(body, "<!-- claudinite-item -->"), strings.Index(body, "<!-- /claudinite-item -->")
	if at := strings.Index(body, "Not-before: 2026-08-15"); at < open || at > close {
		t.Error("the stamp is outside the machine half:", body)
	}
}

func TestARequeueWithAnUnreadableInstantParksAtFailure(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	h.drive([]taskspec.Task{agentless("a")}, func(in *In) {
		in.CodeWork = func(taskspec.Task, Work) CodeWorkResult {
			return CodeWorkResult{OK: true, Requeue: &Requeue{Reason: "huh"}}
		}
	})
	h.wants(1, "open", workitem.StatusNeedsHumanFailure)
	if !strings.Contains(strings.ToLower(h.last(1)), "requeue") {
		t.Error(h.last(1))
	}
}

func TestAnUnconfiguredDeclaredSecretParksAtActionNamingIt(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	h.drive([]taskspec.Task{agentless("a")}, func(in *In) {
		in.CodeWork = func(taskspec.Task, Work) CodeWorkResult {
			return CodeWorkResult{MissingSecrets: []string{"STORE_TOKEN"}}
		}
	})
	h.wants(1, "open", workitem.StatusNeedsHumanAction)
	if !strings.Contains(h.last(1), "STORE_TOKEN") {
		t.Error(h.last(1))
	}
}

func TestADeclineClosesAScheduledItemWithTheReason(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	done := h.drive([]taskspec.Task{loopTask("a", nil)}, func(in *In) {
		in.Evaluate = func(taskspec.Task, workitem.Issue, time.Time) precondition.Verdict { return declines("no work") }
	})
	if !reflect.DeepEqual(done, settled(1, OutcomeObsolete)) {
		t.Error(done)
	}
	h.wants(1, "closed", workitem.StatusRejected)
	body := h.last(1)
	if !strings.Contains(body, "no work") || !strings.Contains(body, "asked again at the next scheduler run") || !strings.Contains(body, "[#1] success") {
		t.Error(body)
	}
	if workitem.ParseBody(h.get(1).Body).NotBefore != "" {
		t.Error("a decline stamps no Not-before")
	}
}

func TestAPreconditionThatCouldNotAnswerParksOpen(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	done := h.drive([]taskspec.Task{loopTask("a", nil)}, func(in *In) {
		in.Evaluate = func(taskspec.Task, workitem.Issue, time.Time) precondition.Verdict {
			return precondition.Verdict{Error: `the term "gate" threw: boom`}
		}
	})
	if !reflect.DeepEqual(done, settled(1, OutcomeNeedsHuman)) {
		t.Error(done)
	}
	h.wants(1, "open", workitem.StatusNeedsHumanFailure)
	if !strings.Contains(h.last(1), "threw: boom") {
		t.Error(h.last(1))
	}
}

func TestAHandOffSwapsToRunningAgentAndInvokesExactlyOnce(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	var nonces []string
	done := h.drive([]taskspec.Task{loopTask("a", nil)}, func(in *In) {
		in.Invoke = func(_ taskspec.Task, _ workitem.Issue, nonce string) Invocation {
			nonces = append(nonces, nonce)
			return Invocation{OK: true, Answered: true, SessionID: "s-9", SessionURL: "https://claude.ai/code/s-9"}
		}
	})
	if !reflect.DeepEqual(done, settled(1, OutcomeAgent)) || len(nonces) != 1 || !strings.HasPrefix(nonces[0], "1-") {
		t.Errorf("%v %v", done, nonces)
	}
	h.wants(1, "open", workitem.StatusRunningAgent)
	found := false
	for _, c := range h.get(1).Comments {
		found = found || strings.Contains(c.Body, nonces[0])
	}
	if !found || !strings.Contains(h.last(1), "https://claude.ai/code/s-9") {
		t.Error(h.get(1).Comments)
	}
}

func TestARefusedInvocationParks(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	done := h.drive([]taskspec.Task{loopTask("a", nil)}, func(in *In) {
		in.Invoke = func(taskspec.Task, workitem.Issue, string) Invocation {
			return Invocation{Answered: true, Error: `endpoint "default" returned 401`}
		}
	})
	if !reflect.DeepEqual(done, settled(1, OutcomeNeedsHuman)) {
		t.Error(done)
	}
	h.wants(1, "open", workitem.StatusNeedsHumanAction)
	if slices.Contains(h.get(1).Labels, workitem.StatusRunningAgent) || !strings.Contains(h.last(1), "401") {
		t.Error(h.get(1).Labels, h.last(1))
	}
}

func TestAnUnansweredInvocationLeavesTheItemWithTheAgent(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	calls := 0
	done := h.drive([]taskspec.Task{loopTask("a", nil)}, func(in *In) {
		in.Invoke = func(taskspec.Task, workitem.Issue, string) Invocation {
			calls++
			return Invocation{Error: "no answer: socket timeout"}
		}
	})
	if calls != 1 || !reflect.DeepEqual(done, settled(1, OutcomeUnknown)) {
		t.Error(calls, done)
	}
	if !reflect.DeepEqual([]string(h.get(1).Labels), []string{workitem.StatusRunningAgent}) || h.get(1).State != "open" {
		t.Error(h.get(1).Labels)
	}
	if !strings.Contains(h.last(1), "may or may not have started") {
		t.Error(h.last(1))
	}
}

func TestFailedCodeWorkNeverHandsOff(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	invoked := 0
	h.drive([]taskspec.Task{loopTask("a", map[string]any{"code_work": "w", "code_work_timeout": 60})}, func(in *In) {
		in.CodeWork = func(taskspec.Task, Work) CodeWorkResult { return CodeWorkResult{Why: "exited 1"} }
		in.Invoke = func(taskspec.Task, workitem.Issue, string) Invocation { invoked++; return Invocation{OK: true} }
	})
	if invoked != 0 {
		t.Error("handed off")
	}
	h.wants(1, "open", workitem.StatusNeedsHumanFailure)
}

func TestAnItemWhoseTaskIsGoneClosesObsolete(t *testing.T) {
	h := newLoop(t)
	h.item(1, "gone")
	done := h.drive([]taskspec.Task{loopTask("a", nil)})
	if !reflect.DeepEqual(done, settled(1, OutcomeObsolete)) {
		t.Error(done)
	}
	h.wants(1, "closed", workitem.StatusRejected)
	if !strings.Contains(h.last(1), "[#1] task-gone") {
		t.Error(h.last(1))
	}
}

func TestATaskDeletedFromTheCheckoutMidRunClosesObsolete(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	h.gone["/tasks/a"] = true
	ran := false
	done := h.drive([]taskspec.Task{agentless("a")}, func(in *In) {
		in.CodeWork = func(taskspec.Task, Work) CodeWorkResult { ran = true; return CodeWorkResult{OK: true} }
	})
	if ran || !reflect.DeepEqual(done, settled(1, OutcomeObsolete)) {
		t.Error(ran, done)
	}
	if !strings.Contains(h.last(1), "packs/acme-pack/tasks/a") {
		t.Error(h.last(1))
	}
}

// Only the title's id is canonical; an item open across a move keeps its
// stored path, and a path naming the very same task is merely stale.
func TestAnItemAtItsPreRenamePathClosesObsolete(t *testing.T) {
	h2 := newLoop(t)
	h2.gh.Seed(sim.StoredIssue{Issue: workitem.Issue{Number: 1, Title: "[claudinite-work] acme-pack/a",
		Body: ".claudinite/shared/packs/acme-pack/tasks/a/task.md\n", Labels: []string{workitem.StatusReady}}})
	done := h2.drive([]taskspec.Task{loopTask("a", nil)})
	if !reflect.DeepEqual(done, settled(1, OutcomeObsolete)) {
		t.Error(done)
	}
	if !strings.Contains(h2.last(1), "packs/acme-pack/tasks/a/task.md") {
		t.Error(h2.last(1))
	}
}

func TestAPathNamingADifferentTaskGoesToAHuman(t *testing.T) {
	h := newLoop(t)
	h.itemWith(1, "a", "packs/acme-pack/tasks/b/task.md\n")
	done := h.drive([]taskspec.Task{loopTask("a", nil), loopTask("b", nil)})
	if !reflect.DeepEqual(done, settled(1, OutcomeNeedsHuman)) {
		t.Error(done)
	}
	h.wants(1, "open", workitem.StatusNeedsHumanFailure)
}

func TestAMalformedItemGoesToAHuman(t *testing.T) {
	h := newLoop(t)
	h.itemWith(1, "a", "")
	done := h.drive([]taskspec.Task{loopTask("a", nil)})
	if !reflect.DeepEqual(done, settled(1, OutcomeNeedsHuman)) {
		t.Error(done)
	}
	if !strings.Contains(h.last(1), "[#1] invalid") {
		t.Error(h.last(1))
	}
}

func TestAnItemPointingElsewhereIsRefused(t *testing.T) {
	h := newLoop(t)
	h.itemWith(1, "a", "somewhere/else/task.md\n")
	done := h.drive([]taskspec.Task{loopTask("a", nil)})
	if !reflect.DeepEqual(done, settled(1, OutcomeNeedsHuman)) || !strings.Contains(h.last(1), "somewhere/else/task.md") {
		t.Error(done, h.last(1))
	}
}

func TestALosingClaimantLeavesTheItemAndDrainsTheRest(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	h.gh.CommentAs(1, "bot", workitem.ClaimMarker+"\nClaimed by executor `E0` at earlier.")
	h.item(2, "b")
	done := h.drive([]taskspec.Task{loopTask("a", nil), agentless("b")})
	if !reflect.DeepEqual(done, settled(2, workitem.StatusDone)) {
		t.Error(done)
	}
	h.wants(1, "open")
	h.wants(2, "closed")
}

func TestARunDrainsEveryPickableItemOneAtATime(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	h.item(2, "b")
	h.item(3, "c")
	var mu sync.Mutex
	inFlight, overlapped := 0, false
	done := h.drive([]taskspec.Task{agentless("a"), agentless("b"), agentless("c")}, func(in *In) {
		in.CodeWork = func(taskspec.Task, Work) CodeWorkResult {
			mu.Lock()
			inFlight++
			overlapped = overlapped || inFlight > 1
			inFlight--
			mu.Unlock()
			return CodeWorkResult{OK: true}
		}
	})
	got := []int{}
	for _, d := range done {
		got = append(got, d.Issue)
	}
	slices.Sort(got)
	if !reflect.DeepEqual(got, []int{1, 2, 3}) || overlapped {
		t.Error(got, overlapped)
	}
}

func TestAHandOffEndsOccupancyAndTheRunKeepsDraining(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	h.item(2, "b")
	done := h.drive([]taskspec.Task{loopTask("a", nil), agentless("b")})
	if !reflect.DeepEqual(done, settled(1, OutcomeAgent, 2, workitem.StatusDone)) {
		t.Error(done)
	}
}

// racingIssues hides #99 from every list read but the second, the
// post-claim re-verify's: the stale-read race only a second read produces.
type racingIssues struct {
	*sim.GitHub
	lists int
}

func (r *racingIssues) IssuesPage(q world.Query, page int) ([]world.Issue, error) {
	got, err := r.GitHub.IssuesPage(q, page)
	if page == 1 {
		r.lists++
	}
	if r.lists == 2 {
		return got, err
	}
	out := []world.Issue{}
	for _, i := range got {
		if i.Number != 99 {
			out = append(out, i)
		}
	}
	return out, err
}

func TestAnItemThisRunRevertedIsNeverRePickedByIt(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	h.item(2, "b")
	h.gh.Seed(sim.StoredIssue{Issue: workitem.Issue{Number: 99, Title: "[claudinite-work] acme-pack/a", Body: itemBody("a"),
		Labels: []string{workitem.StatusRunningExecutor}}})
	h.gh.CommentAs(99, "bot", workitem.ClaimMarker+"\nClaimed by executor `E0` at earlier.")
	racing := &racingIssues{GitHub: h.gh}
	done := h.drive([]taskspec.Task{agentless("a"), agentless("b")}, func(in *In) { in.Issues = racing })
	h.wants(1, "open", workitem.StatusReady)
	if !reflect.DeepEqual(done, settled(2, workitem.StatusDone)) {
		t.Error(done)
	}
}

func TestASecondExecutorWinsAParkedItemAHumanReQueued(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	h.drive([]taskspec.Task{loopTask("a", nil)}, func(in *In) {
		in.Invoke = func(taskspec.Task, workitem.Issue, string) Invocation {
			return Invocation{Answered: true, Error: "no endpoint"}
		}
	})
	h.wants(1, "open", workitem.StatusNeedsHumanAction)
	if err := queue.SwapStatus(h.gh, 1, workitem.StatusNeedsHumanAction, workitem.StatusReady); err != nil {
		t.Fatal(err)
	}
	done := h.drive([]taskspec.Task{loopTask("a", nil)}, func(in *In) { in.ExecutorID = "E2" })
	if !reflect.DeepEqual(done, settled(1, OutcomeAgent)) {
		t.Error("a park that leaves its claim standing livelocks:", done)
	}
}

func TestALosingClaimantStrikesItsOwnClaim(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	rival := workitem.ClaimMarker + "\nClaimed by executor `E9` at t."
	rivalID := h.gh.CommentAs(1, "bot", rival)
	h.drive([]taskspec.Task{agentless("a")})
	h.wants(1, "open")
	// The rival's tenure ends at a boundary older than E1's abandoned claim,
	// so unless that claim was struck it is the new episode's earliest.
	if err := h.gh.EditComment(rivalID, rival+"\n\n"+workitem.EpisodeMarker+"\nreclaimed: executor went silent"); err != nil {
		t.Fatal(err)
	}
	if err := queue.SwapStatus(h.gh, 1, workitem.StatusRunningExecutor, workitem.StatusReady); err != nil {
		t.Fatal(err)
	}
	done := h.drive([]taskspec.Task{agentless("a")}, func(in *In) { in.ExecutorID = "E2" })
	if !reflect.DeepEqual(done, settled(1, workitem.StatusDone)) {
		t.Error("a loser's leftover claim outlived its episode:", done)
	}
}

func TestACloseLeavesItsDependentBlocked(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	h.itemWith(2, "b", itemBody("b")+"\nBlocked-by: #1\n", workitem.StatusBlocked)
	done := h.drive([]taskspec.Task{agentless("a"), agentless("b")})
	if !reflect.DeepEqual(done, settled(1, workitem.StatusDone)) {
		t.Error(done)
	}
	if !reflect.DeepEqual([]string(h.get(2).Labels), []string{workitem.StatusBlocked}) || h.get(2).State != "open" {
		t.Error(h.get(2).Labels)
	}
}

// manualTicker fires a beat each time the work step asks for one.
type manualTicker struct{ fn func() }

func (m *manualTicker) Every(_ time.Duration, fn func()) func() {
	m.fn = fn
	return func() { m.fn = nil }
}

func TestALongWorkStepLeavesHeartbeatsOnItsOwnItem(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	ticker := &manualTicker{}
	h.drive([]taskspec.Task{agentless("a")}, func(in *In) {
		in.Heartbeat = time.Minute
		in.Ticker = ticker
		in.CodeWork = func(taskspec.Task, Work) CodeWorkResult {
			ticker.fn()
			ticker.fn()
			return CodeWorkResult{OK: true}
		}
	})
	beats := 0
	for _, c := range h.get(1).Comments {
		if strings.Contains(c.Body, workitem.HeartbeatMarker) {
			beats++
			if !strings.Contains(c.Body, "executor `E1`") {
				t.Error(c.Body)
			}
		}
	}
	if beats != 2 {
		t.Errorf("%d beats", beats)
	}
}

// A run reclaimed while its work step ran writes nothing more: the item
// is the new claimant's.
func TestARunReclaimedMidWorkLeavesTheItemToItsNewHolder(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	done := h.drive([]taskspec.Task{agentless("a")}, func(in *In) {
		in.CodeWork = func(taskspec.Task, Work) CodeWorkResult {
			h.gh.CommentAs(1, "bot", workitem.EpisodeMarker+"\nreclaimed")
			h.gh.CommentAs(1, "bot", workitem.ClaimMarker+"\nClaimed by executor `E7` at t.")
			return CodeWorkResult{OK: true}
		}
	})
	if !reflect.DeepEqual(done, settled(1, OutcomeReclaimed)) {
		t.Error(done)
	}
	h.wants(1, "open", workitem.StatusRunningExecutor)
}

func stopwatch(runID string) *queue.CostMeter {
	t0 := time.Unix(0, 0)
	calls := 0
	return &queue.CostMeter{Workflow: "executor", RunID: runID,
		Calls: func() *int { calls += 5; c := calls; return &c },
		Now:   func() time.Time { t0 = t0.Add(time.Millisecond); return t0 }}
}

func costsOn(i sim.StoredIssue) []queue.RunCost {
	var out []queue.RunCost
	for _, c := range i.Comments {
		for _, line := range strings.Split(c.Body, "\n") {
			if rc, ok := queue.ParseRunCost(line); ok {
				out = append(out, rc)
			}
		}
	}
	return out
}

func phasesOf(rc queue.RunCost) []string {
	var out []string
	for k := range rc.PhaseMs {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func TestTheRunStampsItsCostOnTheItemItSettled(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	h.drive([]taskspec.Task{agentless("a")}, func(in *In) { in.Cost = stopwatch("77") })
	records := costsOn(h.get(1))
	if len(records) != 1 || records[0].RunID != "77" || records[0].Workflow != "executor" {
		t.Fatal(records)
	}
	if !reflect.DeepEqual(phasesOf(records[0]), []string{"claim", "code-work", "pick"}) {
		t.Error(phasesOf(records[0]))
	}
	if !strings.Contains(h.last(1), "[#1] success") {
		t.Error(h.last(1))
	}
}

func TestAHandedOffItemCarriesTheRecordOnItsHandOff(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	h.drive([]taskspec.Task{loopTask("a", nil)}, func(in *In) { in.Cost = stopwatch("77") })
	records := costsOn(h.get(1))
	if len(records) != 1 || !reflect.DeepEqual(phasesOf(records[0]), []string{"claim", "pick"}) {
		t.Error(records)
	}
}

func TestTwoSettledItemsCarryGrowingSnapshotsOfOneRun(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	h.item(2, "b")
	h.drive([]taskspec.Task{agentless("a"), agentless("b")}, func(in *In) { in.Cost = stopwatch("77") })
	first, second := costsOn(h.get(1)), costsOn(h.get(2))
	if len(first) != 1 || len(second) != 1 || first[0].RunID != second[0].RunID {
		t.Fatal(first, second)
	}
	if *second[0].APICalls <= *first[0].APICalls || second[0].PhaseMs["pick"] <= first[0].PhaseMs["pick"] {
		t.Error(first, second)
	}
}

func TestARunWithNoStopwatchWritesNoCostRecord(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	h.drive([]taskspec.Task{agentless("a")})
	if len(costsOn(h.get(1))) != 0 {
		t.Error(h.last(1))
	}
}

func TestAnApprovalParkCarriesTheCostRecordWithoutAnExecRecord(t *testing.T) {
	h := newLoop(t)
	h.item(1, "a")
	h.drive([]taskspec.Task{agentless("a")}, func(in *In) {
		in.Cost = stopwatch("77")
		in.CodeWork = func(taskspec.Task, Work) CodeWorkResult { return CodeWorkResult{OK: true, DeliveredPR: 7} }
	})
	h.wants(1, "open", workitem.StatusNeedsHumanApprove)
	if len(costsOn(h.get(1))) != 1 || strings.Contains(h.last(1), "claudinite-task-exec") {
		t.Error(h.last(1))
	}
}

// The after yield reads whether a task is on the schedule off its
// declaration: a dependent whose upstream is live this cycle waits.
func TestADependentYieldsWhileItsScheduledUpstreamIsLive(t *testing.T) {
	h := newLoop(t)
	h.itemWith(5, "up", itemBody("up"), workitem.StatusRunningAgent)
	h.item(6, "down")
	down := loopTask("down", map[string]any{"agent_model": "none", "code_work": "w", "code_work_timeout": 60, "schedule_after": []any{"acme-pack/up"}})
	done := h.drive([]taskspec.Task{loopTask("up", nil), down})
	if len(done) != 0 {
		t.Error("the dependent ran beside its live upstream:", done)
	}
	h.wants(6, "open", workitem.StatusReady)
}
