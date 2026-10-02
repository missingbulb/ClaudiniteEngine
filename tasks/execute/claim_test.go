package execute

import (
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/tasks/queue"
	"github.com/missingbulb/ClaudiniteEngine/tasks/world"
)

func claimBy(id int64, executor string) world.Comment {
	return world.Comment{ID: id, Body: ClaimComment(executor, "", "2026-08-14T04:00:00Z")}
}

func holder(c *world.Comment) string {
	if c == nil {
		return ""
	}
	_, rest, _ := strings.Cut(c.Body, "executor `")
	name, _, _ := strings.Cut(rest, "`")
	return name
}

func TestTheEarliestClaimWinsByCommentID(t *testing.T) {
	if got := holder(ClaimWinner([]world.Comment{claimBy(20, "E2"), claimBy(10, "E1")})); got != "E1" {
		t.Error(got)
	}
}

func TestTheArbiterIsEpisodeScoped(t *testing.T) {
	comments := []world.Comment{claimBy(10, "E1"), {ID: 11, Body: workitem.EpisodeMarker + "\nreclaimed"}, claimBy(12, "E2")}
	if got := holder(ClaimWinner(comments)); got != "E2" {
		t.Error(got)
	}
	if w := ClaimWinner([]world.Comment{claimBy(10, "E1")}); w == nil || w.ID != 10 {
		t.Error(w)
	}
	if w := ClaimWinner([]world.Comment{{ID: 1, Body: "just a comment"}}); w != nil {
		t.Error(w)
	}
}

func TestMineIsThisExecutorsNewestClaim(t *testing.T) {
	comments := []world.Comment{claimBy(30, "E1"), claimBy(20, "E2"), claimBy(10, "E1")}
	if m := mineOf(comments, "E1"); m == nil || m.ID != 30 {
		t.Error(m)
	}
	if m := mineOf(comments, "E10"); m != nil {
		t.Error("a prefix of another executor's name is not its claim:", m)
	}
}

func running(n int, task, qualifier string) workitem.Issue {
	title := "[claudinite-work] acme-pack/" + task
	if qualifier != "" {
		title += " " + qualifier
	}
	return workitem.Issue{Number: n, Title: title, Body: "packs/acme-pack/tasks/" + task + "/task.md\n", State: "open",
		Labels: []string{workitem.StatusRunningExecutor}}
}

func TestATwinHoldingAnEarlierClaimForcesARevert(t *testing.T) {
	mine := running(1, "a", "")
	if !ConflictsWithEarlierClaim(mine, 10, []Claimed{{Issue: running(2, "a", ""), ClaimID: 5}}, queue.PickOpts{}) {
		t.Error("earlier twin")
	}
	if ConflictsWithEarlierClaim(mine, 10, []Claimed{{Issue: running(2, "a", ""), ClaimID: 50}}, queue.PickOpts{}) {
		t.Error("later twin")
	}
	if ConflictsWithEarlierClaim(mine, 10, []Claimed{{Issue: running(2, "a", "x"), ClaimID: 5}}, queue.PickOpts{}) {
		t.Error("a fan-out qualifier is not a twin")
	}
}

func TestAnUpstreamThatClaimedEarlierForcesItsDependentBack(t *testing.T) {
	o := queue.PickOpts{
		TaskAfter: func(id string) []string {
			if id == "acme-pack/down" {
				return []string{"acme-pack/up"}
			}
			return nil
		},
		ScheduledOf: func(string) workitem.Scheduled { return workitem.Yes },
	}
	mine := running(1, "down", "")
	if !ConflictsWithEarlierClaim(mine, 9, []Claimed{{Issue: running(2, "up", ""), ClaimID: 4}}, o) {
		t.Error("upstream")
	}
	if ConflictsWithEarlierClaim(mine, 9, []Claimed{{Issue: running(3, "elsewhere", ""), ClaimID: 1}}, o) {
		t.Error("unrelated")
	}
	o.ScheduledOf = func(string) workitem.Scheduled { return workitem.No }
	if ConflictsWithEarlierClaim(mine, 9, []Claimed{{Issue: running(2, "up", ""), ClaimID: 4}}, o) {
		t.Error("the after yield is a scheduled-chain property")
	}
}

func TestAClaimCommentCarriesTheMarkerItsExecutorAndRun(t *testing.T) {
	c := ClaimComment("E1", "http://x", "now")
	if !strings.Contains(c, workitem.ClaimMarker) || !strings.Contains(c, "executor `E1`") || !strings.Contains(c, "Run: http://x") {
		t.Error(c)
	}
}
