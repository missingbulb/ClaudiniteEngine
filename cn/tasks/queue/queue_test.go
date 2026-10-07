package queue_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/queue"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/sim"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/world"
)

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func item(title string, labels ...string) sim.StoredIssue {
	return sim.StoredIssue{Issue: workitem.Issue{Title: title, Body: "acme-pack/acme-task", Labels: labels}}
}

func TestListOpenPagesPastAFullPageAndKeepsOnlyQueueItems(t *testing.T) {
	gh := sim.NewGitHub(sim.NewClock(t0))
	for k := 0; k < world.PageSize+3; k++ {
		gh.Seed(item(fmt.Sprintf("%s acme-pack/acme-task n%d", workitem.WorkPrefix, k), workitem.StatusReady))
	}
	gh.Seed(sim.StoredIssue{Issue: workitem.Issue{Title: "a person's issue"}})
	pr := item(workitem.WorkPrefix+" acme-pack/acme-task pr", workitem.StatusReady)
	pr.PullRequest = true
	gh.Seed(pr)
	got, err := queue.ListOpen(gh)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != world.PageSize+3 {
		t.Fatalf("listed %d items, want %d (second page read, PR and non-item dropped)", len(got), world.PageSize+3)
	}
	if got[0].Number != 1 {
		t.Fatalf("oldest first: got #%d first", got[0].Number)
	}
}

func TestListOpenFailsOnAnUnreadablePageRatherThanShortening(t *testing.T) {
	gh := sim.NewGitHub(sim.NewClock(t0))
	gh.Seed(item(workitem.WorkPrefix+" acme-pack/acme-task", workitem.StatusReady))
	gh.Faults.RateLimited = 1
	if _, err := queue.ListOpen(gh); !errors.Is(err, sim.ErrRateLimited) {
		t.Fatalf("want the page error, got %v", err)
	}
}

func TestListDoneKeepsOnlyDone(t *testing.T) {
	gh := sim.NewGitHub(sim.NewClock(t0))
	a := gh.Seed(item(workitem.WorkPrefix+" acme-pack/acme-task a", workitem.StatusDone))
	b := gh.Seed(item(workitem.WorkPrefix+" acme-pack/acme-task b", workitem.StatusRejected))
	for _, n := range []int{a, b} {
		if err := gh.CloseIssue(n, "completed"); err != nil {
			t.Fatal(err)
		}
	}
	got, err := queue.ListDone(gh)
	if err != nil || len(got) != 1 || got[0].Number != a {
		t.Fatalf("ListDone = %v, %v; want only #%d", got, err, a)
	}
}

func TestSwapStatusRemovesEveryLegacySpelling(t *testing.T) {
	gh := sim.NewGitHub(sim.NewClock(t0))
	n := gh.Seed(item(workitem.WorkPrefix+" acme-pack/acme-task",
		workitem.StatusRunningExecutor, workitem.LegacyExecuting, workitem.Urgent))
	if err := queue.SwapStatus(gh, n, workitem.StatusRunningExecutor, workitem.StatusDone); err != nil {
		t.Fatal(err)
	}
	got, _ := gh.Get(n)
	want := []string{workitem.Urgent, workitem.StatusDone}
	if strings.Join(got.Labels, ",") != strings.Join(want, ",") {
		t.Fatalf("labels %v, want %v", got.Labels, want)
	}
}

func TestClearStatusOfAParkClearsEveryParkSpelling(t *testing.T) {
	gh := sim.NewGitHub(sim.NewClock(t0))
	n := gh.Seed(item(workitem.WorkPrefix+" acme-pack/acme-task", workitem.StatusNeedsHumanAction, workitem.NeedsHuman))
	if err := queue.ClearStatus(gh, n, workitem.StatusNeedsHumanFailure); err != nil {
		t.Fatal(err)
	}
	if got, _ := gh.Get(n); len(got.Labels) != 0 {
		t.Fatalf("labels left: %v", got.Labels)
	}
}

func TestATornSwapLeavesTheItemWithoutAStatus(t *testing.T) {
	gh := sim.NewGitHub(sim.NewClock(t0))
	n := gh.Seed(item(workitem.WorkPrefix+" acme-pack/acme-task", workitem.StatusReady))
	gh.Faults.TearNextSwap = true
	if err := queue.SwapStatus(gh, n, workitem.StatusReady, workitem.StatusRunningExecutor); err == nil {
		t.Fatal("the torn add should surface")
	}
	if got, _ := gh.Get(n); got.Status() != "" {
		t.Fatalf("status %q, want none (the swap is two calls)", got.Status())
	}
}

type manualTicker struct{ fn func() }

func (m *manualTicker) Every(_ time.Duration, fn func()) func() {
	m.fn = fn
	return func() { m.fn = nil }
}

func TestWithHeartbeatBeatsWithMinutesAndSurvivesAFailedBeat(t *testing.T) {
	clock := sim.NewClock(t0)
	tick := &manualTicker{}
	var beats []int
	var logs []string
	fail := false
	err := queue.WithHeartbeat(func() error {
		for k := 0; k < 3; k++ {
			clock.Advance(queue.HeartbeatEvery)
			fail = k == 1
			tick.fn()
		}
		return nil
	}, func(m int) error {
		if fail {
			return errors.New("boom")
		}
		beats = append(beats, m)
		return nil
	}, queue.HeartbeatEvery, clock, tick, func(s string) { logs = append(logs, s) })
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(beats) != "[15 45]" {
		t.Fatalf("beats %v", beats)
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "heartbeat 2 did not post") || !strings.Contains(joined, "ran 45 minute(s) and beat 2 time(s)") {
		t.Fatalf("logs:\n%s", joined)
	}
	if tick.fn != nil {
		t.Fatal("the ticker was not stopped")
	}
}

func TestWithHeartbeatPassesTheWorkErrorThrough(t *testing.T) {
	want := errors.New("work failed")
	got := queue.WithHeartbeat(func() error { return want }, func(int) error { return nil },
		time.Minute, sim.NewClock(t0), &manualTicker{}, func(string) {})
	if !errors.Is(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestLivenessReadsTheLatestBeat(t *testing.T) {
	gh := sim.NewGitHub(sim.NewClock(t0))
	n := gh.Seed(item(workitem.WorkPrefix + " acme-pack/acme-task"))
	gh.CommentAs(n, "someone", "unrelated")
	gh.Clock.Advance(20 * time.Minute)
	if _, err := gh.Comment(n, queue.HeartbeatComment("run-1", "x", 20)); err != nil {
		t.Fatal(err)
	}
	cs, _ := gh.Comments(n)
	if got := queue.LastLivenessAt(cs); !got.Equal(t0.Add(20 * time.Minute)) {
		t.Fatalf("liveness at %v", got)
	}
}
