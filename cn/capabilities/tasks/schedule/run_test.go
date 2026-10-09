package schedule_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/calendar"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/localterms"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/precondition"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/schedule"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/signals"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/sim"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/tasks/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/github/world"
)

var t0 = time.Date(2026, 10, 1, 9, 17, 40, 0, time.UTC)

type harness struct {
	t      *testing.T
	fleet  func(since string) (any, error)
	terms  *localterms.Asker
	gh     *sim.GitHub
	repo   *sim.Repo
	tasks  []taskspec.Task
	local  signals.Local
	logs   []string
	output map[string]string
}

func newHarness(t *testing.T, tasks ...taskspec.Task) *harness {
	clock := sim.NewClock(t0)
	return &harness{t: t, gh: sim.NewGitHub(clock), repo: sim.NewRepo(), tasks: tasks, output: map[string]string{}}
}

func task(id string, decl map[string]any) taskspec.Task {
	decl["id"] = id
	if _, ok := decl["trigger"]; !ok {
		decl["trigger"] = "schedule"
	}
	return taskspec.Task{Pack: "acme-pack", ID: id, Rel: ".claudinite/shared/packs/acme-pack/tasks/" + id,
		Decl: taskspec.Normalize(decl).(taskspec.Decl)}
}

func (h *harness) run(wake string) schedule.RunOut {
	h.t.Helper()
	h.logs = nil
	out, err := schedule.Run(schedule.RunIn{
		Issues: h.gh, Tasks: h.tasks, Now: h.gh.Clock.Now(), Wake: wake, HasFleet: h.fleet != nil, LocalTerms: h.terms,
		Collector: func(items []workitem.Issue) *signals.Collector {
			return &signals.Collector{Issues: h.gh, Repo: h.repo, DefaultBranch: "main", Items: items, Local: h.local, Fleet: h.fleet}
		},
		Log:       func(s string) { h.logs = append(h.logs, s) },
		SetOutput: func(k, v string) error { h.output[k] = v; return nil },
	})
	if err != nil {
		h.t.Fatal(err)
	}
	return out
}

func (h *harness) open() []sim.StoredIssue {
	var out []sim.StoredIssue
	for _, i := range h.gh.All() {
		if i.State == "open" {
			out = append(out, i)
		}
	}
	return out
}

func (h *harness) commit(sha, message string, files ...string) {
	h.repo.AddCommit(world.Commit{SHA: sha, Message: message, Author: "someone", Date: calendar.ISO(h.gh.Clock.Now()), Files: files})
}

func daily() taskspec.Task {
	return task("fold", map[string]any{"preconditions": []any{"schedule:at-most-daily", "any-commit"}})
}

func TestAQuietRepoFilesNothingAndTheGateStaysShut(t *testing.T) {
	h := newHarness(t, daily())
	out := h.run("")
	if len(h.open()) != 0 {
		t.Fatalf("filed %v on a quiet repo", h.open())
	}
	if len(out.Asked) != 1 || out.Asked[0].Verdict != schedule.VerdictNo {
		t.Fatalf("asked %+v", out.Asked)
	}
	if h.output["pickable"] != "false" || out.Pickable != 0 {
		t.Fatalf("gate %q / %d", h.output["pickable"], out.Pickable)
	}
}

func TestACommitFilesOneReadyItemAndASecondRunFilesNothing(t *testing.T) {
	h := newHarness(t, daily())
	h.commit("a1", "feat: a thing", "src/a.go")
	h.run("")
	open := h.open()
	if len(open) != 1 || open[0].Title != "[claudinite-work] acme-pack/fold" ||
		!open[0].HasLabel(workitem.StatusReady) || !open[0].HasLabel(workitem.OriginPlanned) {
		t.Fatalf("open %+v", open)
	}
	if !strings.HasPrefix(open[0].Body, ".claudinite/shared/packs/acme-pack/tasks/fold/task.md\n") {
		t.Fatalf("body %q", open[0].Body)
	}
	if h.output["pickable"] != "true" {
		t.Fatal("the gate stayed shut over a ready item")
	}
	if !h.gh.LabelDefined(workitem.StatusReady) {
		t.Fatal("the label was applied before it was ensured")
	}
	h.gh.Clock.Advance(time.Hour)
	h.run("")
	if len(h.open()) != 1 {
		t.Fatalf("one live item per task: %d open", len(h.open()))
	}
}

func TestTheCadenceDeclinesASecondRunInTheSamePeriod(t *testing.T) {
	h := newHarness(t, daily())
	h.commit("a1", "feat: a thing", "src/a.go")
	h.run("")
	n := h.open()[0].Number
	// The executor ran it: it picked the item and converged it done.
	_ = h.gh.RemoveLabel(n, workitem.StatusReady)
	_ = h.gh.AddLabel(n, workitem.StatusDone)
	_ = h.gh.CloseIssue(n, "completed")
	h.gh.Clock.Advance(time.Hour)
	h.commit("a2", "feat: another", "src/b.go")
	out := h.run("")
	if len(h.open()) != 0 || out.Asked[0].Verdict != schedule.VerdictNo {
		t.Fatalf("a second occurrence in one day: open %v, asked %+v", h.open(), out.Asked)
	}
	h.gh.Clock.Advance(24 * time.Hour)
	h.commit("a3", "feat: next day", "src/c.go")
	h.run("")
	if len(h.open()) != 1 {
		t.Fatal("the next period's occurrence was not filed")
	}
}

// An item the executor closed rejected, its precondition declining at
// the pick, never ran, so the period stays open; one it closed done
// covers the period.
func TestADeclinedItemCoversNoPeriod(t *testing.T) {
	for _, c := range []struct {
		status, reason string
		files          bool
	}{{workitem.StatusRejected, "not_planned", true}, {workitem.StatusDone, "completed", false}} {
		h := newHarness(t, daily())
		h.commit("a1", "feat: a thing", "src/a.go")
		h.run("")
		n := h.open()[0].Number
		_ = h.gh.RemoveLabel(n, workitem.StatusReady)
		_ = h.gh.AddLabel(n, c.status)
		_ = h.gh.CloseIssue(n, c.reason)
		h.gh.Clock.Advance(time.Hour)
		h.commit("a2", "feat: another", "src/b.go")
		out := h.run("")
		if files := len(h.open()) == 1; files != c.files {
			t.Errorf("%s: filed %v, want %v — asked %+v", c.status, files, c.files, out.Asked)
		}
	}
}

func TestAnUnreadableSignalFailsOpen(t *testing.T) {
	h := newHarness(t, daily())
	h.repo.Unreadable = true
	out := h.run("")
	open := h.open()
	if len(open) != 1 || out.Asked[0].Verdict != schedule.VerdictFailOpen {
		t.Fatalf("open %v asked %+v", open, out.Asked)
	}
	if !strings.Contains(open[0].Body, "The scheduler could not decide this occurrence") {
		t.Fatalf("body %q", open[0].Body)
	}
}

func fleetTask() taskspec.Task {
	tk := task("promote", map[string]any{"preconditions": []any{"schedule:at-most-daily", "fleet-moved"}})
	tk.Terms = taskspec.Terms{{Name: "fleet-moved", Signals: []string{"fleet"}}}
	return tk
}

func TestAFleetTaskWithoutTheTokenFailsOpenOnNodesSentence(t *testing.T) {
	h := newHarness(t, fleetTask())
	out := h.run("")
	if len(out.Asked) != 1 || out.Asked[0].Verdict != schedule.VerdictFailOpen ||
		out.Asked[0].Reason != "the `fleet` signal needs FLEET_GITHUB_TOKEN, which the scheduler run does not hold" {
		t.Fatalf("asked %+v", out.Asked)
	}
}

func TestAFleetTaskWithTheTokenAsksTheReader(t *testing.T) {
	h := newHarness(t, fleetTask())
	var since []string
	h.fleet = func(s string) (any, error) {
		since = append(since, s)
		return nil, errors.New("no repositories owned by acme")
	}
	out := h.run("")
	if len(since) != 1 || len(out.Asked) != 1 || out.Asked[0].Reason != "the `fleet` signal failed: no repositories owned by acme" {
		t.Fatalf("since %v asked %+v", since, out.Asked)
	}
	h = newHarness(t, fleetTask())
	h.fleet = func(string) (any, error) { return map[string]any{"owner": "acme", "members": []any{}}, nil }
	out = h.run("")
	if len(out.Asked) != 1 || strings.Contains(out.Asked[0].Reason, "signal") {
		t.Fatalf("a read fleet is no failure: %+v", out.Asked)
	}
}

func TestADuplicateStandingItemSelfHeals(t *testing.T) {
	h := newHarness(t, daily())
	a := h.gh.Seed(sim.StoredIssue{Issue: workitem.Issue{Title: "[claudinite-work] acme-pack/fold", Body: "x", Labels: []string{workitem.StatusReady}}})
	b := h.gh.Seed(sim.StoredIssue{Issue: workitem.Issue{Title: "[claudinite-work] acme-pack/fold", Body: "x", Labels: []string{workitem.LegacyReady}}})
	h.run("")
	got, _ := h.gh.Get(b)
	if got.State != "closed" || got.StateReason != "not_planned" || !got.HasLabel(workitem.StatusRejected) {
		t.Fatalf("the younger twin %+v", got)
	}
	if kept, _ := h.gh.Get(a); kept.State != "open" {
		t.Fatal("the oldest twin closed")
	}
}

func TestTheLeashReclaimsASilentClaimAndDrawsTheEpisodeBoundary(t *testing.T) {
	h := newHarness(t)
	n := h.gh.Seed(sim.StoredIssue{Issue: workitem.Issue{Title: "[claudinite-work] acme-pack/fold x", Body: "x", Labels: []string{workitem.StatusRunningExecutor}}})
	h.gh.CommentAs(n, "github-actions[bot]", workitem.ClaimMarker+"\nClaimed by executor `e1`.")
	h.gh.Clock.Advance(59 * time.Minute)
	h.run("")
	if got, _ := h.gh.Get(n); !got.HasLabel(workitem.StatusRunningExecutor) {
		t.Fatal("reclaimed inside the leash")
	}
	h.gh.Clock.Advance(2 * time.Minute)
	h.run("")
	got, _ := h.gh.Get(n)
	if !got.HasLabel(workitem.StatusReady) || got.HasLabel(workitem.StatusRunningExecutor) {
		t.Fatalf("labels %v", got.Labels)
	}
	last := got.Comments[len(got.Comments)-1].Body
	if !strings.HasPrefix(last, workitem.EpisodeMarker+"\nReclaimed:") {
		t.Fatalf("comment %q", last)
	}
	if h.output["pickable"] != "true" {
		t.Fatal("a reclaimed item is pickable")
	}
}

func TestAHeartbeatKeepsALongRunAlive(t *testing.T) {
	h := newHarness(t)
	n := h.gh.Seed(sim.StoredIssue{Issue: workitem.Issue{Title: "[claudinite-work] acme-pack/fold x", Body: "x", Labels: []string{workitem.StatusRunningExecutor}}})
	h.gh.Clock.Advance(50 * time.Minute)
	h.gh.CommentAs(n, "github-actions[bot]", workitem.HeartbeatMarker+"\nStill working.")
	h.gh.Clock.Advance(50 * time.Minute)
	h.run("")
	if got, _ := h.gh.Get(n); !got.HasLabel(workitem.StatusRunningExecutor) {
		t.Fatal("a beating holder was reclaimed")
	}
}

func requestTask() taskspec.Task {
	return task("acme-task", map[string]any{"trigger": "request", "preconditions": []any{"request-eligible"}})
}

func TestAMarkedIssueIsAdoptedOnceAsItself(t *testing.T) {
	req := task("implement", map[string]any{"trigger": "request", "preconditions": []any{}})
	h := newHarness(t, req, requestTask())
	h.gh.Roles["owner"] = "admin"
	n := h.gh.Seed(sim.StoredIssue{Author: "owner", Issue: workitem.Issue{Title: "Fix the thing", Body: "Please fix it.\n\nTask: acme-pack/implement\nModel: sonnet\n",
		Labels: []string{workitem.OriginAdHoc}}})
	h.run("")
	got, _ := h.gh.Get(n)
	if !got.HasLabel(workitem.StatusReady) || got.Title != "Fix the thing" {
		t.Fatalf("adopted %+v", got)
	}
	f := workitem.ParseFields(got.Body)
	if workitem.Str(f.TaskPath) != ".claudinite/shared/packs/acme-pack/tasks/implement/task.md" || workitem.Str(f.Model) != "sonnet" || workitem.Int(f.Request) != n {
		t.Fatalf("machine block %+v in %q", f, got.Body)
	}
	if !strings.HasPrefix(got.Body, "Please fix it.") || !strings.Contains(got.Comments[0].Body, "at the `sonnet` family") {
		t.Fatalf("body %q comments %v", got.Body, got.Comments)
	}
	before := len(got.Comments)
	h.run("")
	if again, _ := h.gh.Get(n); len(again.Comments) != before {
		t.Fatal("re-adopted an item already holding a status")
	}
}

func TestAStrangersParametersAreIgnored(t *testing.T) {
	h := newHarness(t, requestTask())
	n := h.gh.Seed(sim.StoredIssue{Author: "stranger", Issue: workitem.Issue{Title: "Do it", Body: "Model: opus\nAutomerge: doc-changes\n",
		Labels: []string{workitem.RequestLabel}}})
	h.run("")
	got, _ := h.gh.Get(n)
	f := workitem.ParseFields(got.Body)
	if f.Model != nil || f.Merge != nil || !got.HasLabel(workitem.OriginAdHoc) || !got.HasLabel(workitem.StatusReady) {
		t.Fatalf("fields %+v labels %v", f, got.Labels)
	}
	if workitem.Str(f.TaskPath) != requestTask().TaskPath() {
		t.Fatalf("path %v", workitem.Str(f.TaskPath))
	}
	if !strings.Contains(got.Comments[0].Body, "were ignored") {
		t.Fatalf("comment %q", got.Comments[0].Body)
	}
}

func TestAMarkedIssueNamingNoTaskWaitsUnlessExactlyOneTaskTakesRequests(t *testing.T) {
	second := task("other-task", map[string]any{"trigger": "request", "preconditions": []any{"request-eligible"}})
	for _, c := range []struct {
		name  string
		tasks []taskspec.Task
		adopt bool
	}{
		{"none", nil, false},
		{"one", []taskspec.Task{requestTask()}, true},
		{"two", []taskspec.Task{requestTask(), second}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, c.tasks...)
			h.gh.Roles["owner"] = "admin"
			n := h.gh.Seed(sim.StoredIssue{Author: "owner", Issue: workitem.Issue{Title: "Do it", Body: "Please.\n", Labels: []string{workitem.OriginAdHoc}}})
			h.run("")
			got, _ := h.gh.Get(n)
			if got.HasLabel(workitem.StatusReady) != c.adopt {
				t.Fatalf("labels %v", got.Labels)
			}
		})
	}
}

func TestABlockedItemIsReadiedOnlyWhenItsWaitIsOver(t *testing.T) {
	h := newHarness(t)
	blocker := h.gh.Seed(sim.StoredIssue{Issue: workitem.Issue{Title: "a dependency", Body: "x"}})
	n := h.gh.Seed(sim.StoredIssue{Issue: workitem.Issue{Title: "[claudinite-work] acme-pack/fold x",
		Body: workitem.Body(workitem.BodySpec{TaskPath: "p/task.md", BlockedBy: []int{blocker}}), Labels: []string{workitem.StatusBlocked}}})
	h.run("")
	if got, _ := h.gh.Get(n); !got.HasLabel(workitem.StatusBlocked) {
		t.Fatal("readied behind an open blocker")
	}
	_ = h.gh.CloseIssue(blocker, "completed")
	h.run("")
	if got, _ := h.gh.Get(n); !got.HasLabel(workitem.StatusReady) || got.HasLabel(workitem.StatusBlocked) {
		t.Fatalf("labels %v", got.Labels)
	}
}

func TestAWakeMintsTheMissingStandingItemAndReportsWhatMatchedNothing(t *testing.T) {
	h := newHarness(t, daily())
	out := h.run("fold nothing")
	open := h.open()
	if len(open) != 1 || !strings.Contains(open[0].Body, "Woken: "+calendar.ISO(t0)) || !strings.Contains(open[0].Body, schedule.ForcedWakeContext) {
		t.Fatalf("open %+v", open)
	}
	if len(out.Problems) != 1 || !strings.Contains(out.Problems[0], `"nothing"`) {
		t.Fatalf("problems %v", out.Problems)
	}
	h.run("fold")
	if len(h.open()) != 1 {
		t.Fatal("a second force minted a twin")
	}
}

func TestAnAbandonedFailureParkCloses(t *testing.T) {
	h := newHarness(t, daily())
	n := h.gh.Seed(sim.StoredIssue{Issue: workitem.Issue{Title: "[claudinite-work] acme-pack/fold", Body: "x", Labels: []string{workitem.StatusNeedsHumanFailure}}})
	h.gh.Clock.Advance(11 * 24 * time.Hour)
	h.run("")
	got, _ := h.gh.Get(n)
	if got.State != "closed" || !got.HasLabel(workitem.StatusRejected) || got.HasLabel(workitem.StatusNeedsHumanFailure) {
		t.Fatalf("park %+v", got)
	}
}

func TestATornItemSettledBeforeTheWriteIsLeftAlone(t *testing.T) {
	tk := daily()
	fresh := workitem.Issue{Number: 1, State: "open", Labels: []string{workitem.StatusReady}}
	op := schedule.Op{Kind: schedule.KindEscalate, Issue: 1, Confirm: schedule.ConfirmStateless}
	if schedule.StillHolds(op, &fresh, t0, []taskspec.Task{tk}) {
		t.Fatal("an item that gained a status still reads as stateless")
	}
	fresh.Labels = nil
	if !schedule.StillHolds(op, &fresh, t0, []taskspec.Task{tk}) {
		t.Fatal("a stateless item read fresh no longer holds")
	}
}

// The engine's own update is a scheduled task like any other: at most one
// occurrence a UTC day, filed at the engine's path, whatever the repo did.
// The usage fold is the engine's own on every repo the queue runs in, and
// a repo whose only activity is its own machinery files it while the
// fold's run mark stands before the UTC day opened.
func TestTheUsageFoldIsFiledWhileTheMachineryRanUnfolded(t *testing.T) {
	all, errs := taskspec.Discover(t.TempDir(), nil)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	var fold taskspec.Task
	for _, tk := range all {
		if tk.ID == taskspec.UsageFoldTask {
			fold = tk
		}
	}
	h := newHarness(t, fold)
	caughtUp := "2026-10-01T05:00:00Z"
	h.local.UsageFold = precondition.UsageFold{RunsFoldedThrough: &caughtUp}
	if out := h.run(""); len(h.open()) != 0 || out.Asked[0].Verdict != schedule.VerdictNo {
		t.Fatalf("a mark inside today: open %v, asked %+v", h.open(), out.Asked)
	}
	behind := "2026-09-30T17:10:00Z"
	h.local.UsageFold = precondition.UsageFold{RunsFoldedThrough: &behind}
	h.run("")
	open := h.open()
	if len(open) != 1 || open[0].Title != "[claudinite-work] engine/usage-fold" ||
		!strings.HasPrefix(open[0].Body, taskspec.BuiltinTaskPath(taskspec.UsageFoldTask)+"\n") {
		t.Fatalf("a mark before today: %+v", open)
	}
}

func TestTheEnginesUpdateIsFiledOnceADay(t *testing.T) {
	all, errs := taskspec.Discover(t.TempDir(), nil)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	var update taskspec.Task
	for _, tk := range all {
		if tk.ID == taskspec.UpdateTask {
			update = tk
		}
	}
	h := newHarness(t, update)
	h.run("")
	open := h.open()
	if len(open) != 1 || open[0].Title != "[claudinite-work] engine/update" || !strings.HasPrefix(open[0].Body, taskspec.UpdateTaskPath+"\n") {
		t.Fatalf("a quiet repo's first run today: %+v", open)
	}
	n := open[0].Number
	_ = h.gh.RemoveLabel(n, workitem.StatusReady)
	_ = h.gh.AddLabel(n, workitem.StatusDone)
	_ = h.gh.CloseIssue(n, "completed")
	h.gh.Clock.Advance(time.Hour)
	if out := h.run(""); len(h.open()) != 0 || out.Asked[0].Verdict != schedule.VerdictNo {
		t.Fatalf("a second occurrence in one day: open %v, asked %+v", h.open(), out.Asked)
	}
	// The fleet's force lever sends the bare id; it reaches the engine's
	// update, minting the standing item the cadence declined.
	if h.run("update"); len(h.open()) != 1 || h.open()[0].Title != "[claudinite-work] engine/update" {
		t.Fatalf("a forced update the same day: %+v", h.open())
	}
	m := h.open()[0].Number
	_ = h.gh.RemoveLabel(m, workitem.StatusReady)
	_ = h.gh.AddLabel(m, workitem.StatusDone)
	_ = h.gh.CloseIssue(m, "completed")
	h.gh.Clock.Advance(24 * time.Hour)
	h.run("")
	if len(h.open()) != 1 {
		t.Fatal("the next day's update was not filed")
	}
}
