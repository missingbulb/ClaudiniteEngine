package execute

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/mergepolicy"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/land"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/sim"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/world"
)

const targetTask = "acme-pack-b/acme-task-c"

var targetNow = time.Date(2026, 9, 4, 4, 10, 0, 0, time.UTC)

func pull(n int, ref string, sha ...string) world.Pull {
	s := "sha" + itoa(n)
	if len(sha) > 0 {
		s = sha[0]
	}
	return world.Pull{Number: n, HeadRef: ref, HeadSHA: s, State: "open", NodeID: "node" + itoa(n)}
}

func numbers(ps []world.Pull) []int {
	out := []int{}
	for _, p := range ps {
		out = append(out, p.Number)
	}
	return out
}

func TestATasksPullRequestsAreItsPrefixOrItsTrailerNewestFirst(t *testing.T) {
	pulls := []world.Pull{
		pull(3, "claudinite/acme-pack-b/acme-task-c/2026-09-01-aaa"),
		pull(9, "feature/unrelated"),
		pull(5, "claudinite/acme-pack-b/acme-task-c/2026-09-03-bbb"),
		pull(4, "claudinite/acme-pack-b/updater/2026-09-02-ccc"),
	}
	if got := numbers(TaskPullsOf(pulls, targetTask, nil)); !reflect.DeepEqual(got, []int{5, 3}) {
		t.Errorf("%v", got)
	}
	if TaskBranchPrefix(targetTask) != "claudinite/acme-pack-b/acme-task-c/" {
		t.Error(TaskBranchPrefix(targetTask))
	}
	off := []world.Pull{pull(12, "claudinite/update-2026-09-02-xyz"), pull(13, "someone/elses")}
	headOf := func(p world.Pull) string {
		if p.Number == 12 {
			return targetTask
		}
		return ""
	}
	if got := numbers(TaskPullsOf(off, targetTask, headOf)); !reflect.DeepEqual(got, []int{12}) {
		t.Errorf("%v", got)
	}
	if got := TaskPullsOf(off, "other/task", headOf); len(got) != 0 {
		t.Errorf("%v", got)
	}
	if b := MintBranch(targetTask, targetNow, "ab12cd"); b != "claudinite/acme-pack-b/acme-task-c/2026-09-04-ab12cd" {
		t.Error(b)
	}
}

const fresh1 = "claudinite/acme-pack-b/acme-task-c/2026-09-04-fresh1"

func plan(t *testing.T, in PlanIn) Target {
	t.Helper()
	in.Branch = fresh1
	got, err := PlanTarget(in)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestThePlannersMatrix(t *testing.T) {
	yes, no := true, false
	if got := plan(t, PlanIn{Outcome: "no_code_changes", Incumbents: []world.Pull{pull(3, "x")}}); got.Mode != ModeNone || got.Branch != "" || got.PR != 0 || len(got.Supersedes) != 0 || got.Landed != 0 {
		t.Errorf("none %+v", got)
	}
	if got := plan(t, PlanIn{Outcome: "fresh_pr", Incumbents: []world.Pull{pull(3, "x")}}); got.Mode != ModeFresh || got.Branch != fresh1 || got.PR != 0 || len(got.Supersedes) != 0 {
		t.Errorf("fresh %+v", got)
	}
	inc := []world.Pull{pull(5, "claudinite/acme-pack-b/acme-task-c/2026-09-03-bbb"), pull(3, "older")}
	if got := plan(t, PlanIn{Outcome: "amend_existing_or_create_new_pr", Incumbents: inc, Mergeable: &yes}); got.Mode != ModeAmend || got.PR != 5 || got.Branch != inc[0].HeadRef {
		t.Errorf("amend %+v", got)
	}
	if got := plan(t, PlanIn{Outcome: "amend_existing_or_create_new_pr", Incumbents: inc, Mergeable: &no}); got.Mode != ModeAmend || got.PR != 5 || !strings.Contains(got.Reason, "CONFLICTS") {
		t.Errorf("conflicted %+v", got)
	}
	if got := plan(t, PlanIn{Outcome: "amend_existing_or_create_new_pr", Incumbents: inc}); got.Error == "" || !strings.Contains(got.Error, "#5") || got.Mode != "" {
		t.Errorf("unreadable %+v", got)
	}
	if got := plan(t, PlanIn{Outcome: "amend_existing_or_create_new_pr"}); got.Mode != ModeFresh || got.Branch != fresh1 {
		t.Errorf("nothing to amend %+v", got)
	}
	two := []world.Pull{pull(5, "b"), pull(3, "a")}
	if got := plan(t, PlanIn{Outcome: "supersede_existing_pr", Incumbents: two, Disposition: land.DispClose}); got.Mode != ModeFresh || !reflect.DeepEqual(got.Supersedes, []int{5, 3}) || got.Landed != 0 {
		t.Errorf("supersede %+v", got)
	}
	if got := plan(t, PlanIn{Outcome: "supersede_existing_pr"}); len(got.Supersedes) != 0 {
		t.Errorf("nothing to supersede %+v", got)
	}
	if got := plan(t, PlanIn{Outcome: "supersede_existing_pr", Incumbents: two, Disposition: land.DispMerge}); got.Mode != ModeNone || got.Landed != 5 || !reflect.DeepEqual(got.Supersedes, []int{3}) {
		t.Errorf("landed %+v", got)
	}
	for _, unknown := range []string{"none", "pr", "open-pr", "merged-pr", "push"} {
		if _, err := PlanTarget(PlanIn{Outcome: unknown, Branch: fresh1}); err == nil || !strings.Contains(err.Error(), "not a legal outcome") {
			t.Errorf("%s: %v", unknown, err)
		}
	}
}

func TestATargetBecomesExactlyThreeVariables(t *testing.T) {
	cases := []struct {
		t    Target
		want []string
	}{
		{Target{Mode: ModeNone}, []string{"CLAUDINITE_TARGET_MODE=none", "CLAUDINITE_TARGET_BRANCH=", "CLAUDINITE_TARGET_PR="}},
		{Target{Mode: ModeAmend, Branch: "b", PR: 5}, []string{"CLAUDINITE_TARGET_MODE=amend", "CLAUDINITE_TARGET_BRANCH=b", "CLAUDINITE_TARGET_PR=5"}},
		{Target{Mode: ModeFresh, Branch: "b"}, []string{"CLAUDINITE_TARGET_MODE=fresh", "CLAUDINITE_TARGET_BRANCH=b", "CLAUDINITE_TARGET_PR="}},
		{Target{}, []string{"CLAUDINITE_TARGET_MODE=none", "CLAUDINITE_TARGET_BRANCH=", "CLAUDINITE_TARGET_PR="}},
	}
	for _, c := range cases {
		if got := c.t.Env(); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%+v: %v", c.t, got)
		}
	}
}

func resolveOn(gh *sim.GitHub, repo *sim.Repo, outcome, delivery string) (Target, *[]string) {
	var said []string
	got := ResolveTarget(TargetIn{Issues: gh, Repo: repo, Pulls: repo, Lane: repo, TaskID: targetTask, Outcome: outcome,
		Delivery: delivery, Now: targetNow, Seed: "seed01", Sleep: func(time.Duration) {}, Log: func(s string) { said = append(said, s) }})
	return got, &said
}

func TestResolveReadsNothingWhereNoPullRequestIsInvolved(t *testing.T) {
	repo := sim.NewRepo()
	repo.Unreadable = true
	f, _ := resolveOn(sim.NewGitHub(sim.NewClock(targetNow)), repo, "fresh_pr", land.AutoMerge)
	if f.Mode != ModeFresh || f.Branch != "claudinite/acme-pack-b/acme-task-c/2026-09-04-seed01" || f.Error != "" {
		t.Errorf("%+v", f)
	}
	if n, _ := resolveOn(sim.NewGitHub(sim.NewClock(targetNow)), repo, "no_code_changes", land.AutoMerge); n.Mode != ModeNone || n.Error != "" {
		t.Errorf("%+v", n)
	}
	if s, _ := resolveOn(sim.NewGitHub(sim.NewClock(targetNow)), repo, "supersede_existing_pr", land.AutoMerge); !strings.Contains(s.Error, "could not list") {
		t.Errorf("an unreadable list read as empty: %+v", s)
	}
}

func TestResolvePollsMergeabilityAndNeverForksOnAnUnanswered(t *testing.T) {
	repo := sim.NewRepo()
	repo.Pulls = []world.Pull{pull(5, "claudinite/acme-pack-b/acme-task-c/2026-09-03-bbb")}
	yes := true
	repo.MergeableReads[5] = []*bool{nil, nil, &yes}
	got, _ := resolveOn(sim.NewGitHub(sim.NewClock(targetNow)), repo, "amend_existing_or_create_new_pr", land.AutoMerge)
	if got.Mode != ModeAmend || got.PR != 5 {
		t.Errorf("%+v", got)
	}
	repo.MergeableReads[5] = []*bool{nil}
	got, _ = resolveOn(sim.NewGitHub(sim.NewClock(targetNow)), repo, "amend_existing_or_create_new_pr", land.AutoMerge)
	if got.Error == "" || got.Mode != "" {
		t.Errorf("%+v", got)
	}
}

func TestResolveFindsAnIncumbentOffThePrefixByItsTrailer(t *testing.T) {
	repo := sim.NewRepo()
	repo.Pulls = []world.Pull{pull(12, "claudinite/update-2026-09-02-xyz", "abc"), pull(13, "feature/x", "def")}
	repo.Heads["abc"] = world.Commit{SHA: "abc", Message: "Claudinite: acme-task-c\n\nClaudinite-Task: " + targetTask + "\n"}
	got, _ := resolveOn(sim.NewGitHub(sim.NewClock(targetNow)), repo, "amend_existing_or_create_new_pr", land.AutoMerge)
	if got.PR != 12 {
		t.Errorf("%+v", got)
	}
}

func TestResolveSupersedesByTheRunsOnTheNewestHead(t *testing.T) {
	mk := func(conclusion string) *sim.Repo {
		repo := sim.NewRepo()
		repo.Pulls = []world.Pull{pull(5, "claudinite/acme-pack-b/acme-task-c/2026-09-03-bbb", "shaB"), pull(3, "claudinite/acme-pack-b/acme-task-c/2026-09-01-aaa", "shaA")}
		repo.Runs["shaB"] = []land.Run{{Name: "ci", Status: "completed", Conclusion: conclusion}}
		return repo
	}
	red := mk("failure")
	got, _ := resolveOn(sim.NewGitHub(sim.NewClock(targetNow)), red, "supersede_existing_pr", land.AutoMerge)
	if got.Mode != ModeFresh || !reflect.DeepEqual(got.Supersedes, []int{5, 3}) || got.Landed != 0 || len(red.Log) != 0 {
		t.Errorf("red %+v %v", got, red.Log)
	}
	green := mk("success")
	got, _ = resolveOn(sim.NewGitHub(sim.NewClock(targetNow)), green, "supersede_existing_pr", land.AutoMerge)
	if got.Mode != ModeNone || got.Landed != 5 || !reflect.DeepEqual(got.Supersedes, []int{3}) {
		t.Errorf("green %+v", got)
	}
	if !slices.Contains(green.Log, "merge #5 shaB Claudinite-Task: "+targetTask) || !slices.Contains(green.Log, "delete claudinite/acme-pack-b/acme-task-c/2026-09-03-bbb") {
		t.Errorf("%v", green.Log)
	}
	review := mk("success")
	got, _ = resolveOn(sim.NewGitHub(sim.NewClock(targetNow)), review, "supersede_existing_pr", land.Review)
	if got.Mode != ModeFresh || got.Landed != 0 || len(review.Log) != 0 {
		t.Errorf("review %+v %v", got, review.Log)
	}
	refused := mk("success")
	refused.MergeRefused = 405
	got, _ = resolveOn(sim.NewGitHub(sim.NewClock(targetNow)), refused, "supersede_existing_pr", land.AutoMerge)
	if got.Mode != ModeFresh || !reflect.DeepEqual(got.Supersedes, []int{5, 3}) || got.Landed != 0 {
		t.Errorf("refused %+v", got)
	}
}

// A green incumbent lands only when the task's policy authorizes its diff.
func TestResolveLandsAGreenIncumbentOnlyInsideThePolicy(t *testing.T) {
	repo := sim.NewRepo()
	repo.Pulls = []world.Pull{pull(5, "claudinite/acme-pack-b/acme-task-c/2026-09-03-bbb", "shaB")}
	repo.Runs["shaB"] = []land.Run{{Name: "ci", Status: "completed", Conclusion: "success"}}
	code := "package main\n"
	j := &land.Judgement{Automerge: []any{"doc-changes"}, Diff: func(land.PR) ([]mergepolicy.Entry, error) {
		return []mergepolicy.Entry{{File: "src/main.go", After: &code}}, nil
	}}
	var said []string
	got := ResolveTarget(TargetIn{Issues: sim.NewGitHub(sim.NewClock(targetNow)), Repo: repo, Pulls: repo, Lane: repo, TaskID: targetTask,
		Outcome: "supersede_existing_pr", Delivery: land.AutoMerge, Now: targetNow, Seed: "seed01", Sleep: func(time.Duration) {},
		Log: func(s string) { said = append(said, s) }, Judgement: j})
	if got.Landed != 0 || got.Mode != ModeFresh || len(repo.Log) != 0 {
		t.Errorf("an incumbent outside the policy landed: %+v %v", got, repo.Log)
	}
	if !strings.Contains(strings.Join(said, "\n"), "outside this task's automerge") {
		t.Errorf("%v", said)
	}
}

func TestCloseSupersededCommentsClosesAndTidiesBestEffort(t *testing.T) {
	gh := sim.NewGitHub(sim.NewClock(targetNow))
	repo := sim.NewRepo()
	repo.Pulls = []world.Pull{pull(5, "claudinite/acme-pack-b/acme-task-c/2026-09-03-bbb"), pull(3, "claudinite/acme-pack-b/acme-task-c/2026-09-01-aaa")}
	gh.Seed(sim.StoredIssue{Issue: workitem.Issue{Number: 5, Title: "pr five"}})
	gh.Seed(sim.StoredIssue{Issue: workitem.Issue{Number: 3, Title: "pr three"}})
	var said []string
	CloseSuperseded(gh, repo, repo, []int{5, 3, 77}, 9, func(s string) { said = append(said, s) })
	for _, n := range []int{5, 3} {
		cs, _ := gh.Comments(n)
		if len(cs) != 1 || !strings.Contains(cs[0].Body, "Superseded by #9") {
			t.Errorf("#%d %v", n, cs)
		}
	}
	if !slices.Contains(repo.Log, "close #5") || !slices.Contains(repo.Log, "delete claudinite/acme-pack-b/acme-task-c/2026-09-01-aaa") {
		t.Errorf("%v", repo.Log)
	}
	joined := strings.Join(said, "\n")
	if !strings.Contains(joined, "closed #5 — superseded by #9") || !strings.Contains(joined, "could not close #77") {
		t.Errorf("%s", joined)
	}
}

// Landing a green incumbent is a merge the job token makes, so it
// dispatches CI on the incumbent's base.
func TestLandingAGreenIncumbentDispatchesCIOnItsBase(t *testing.T) {
	repo := sim.NewRepo()
	p := pull(5, "claudinite/acme-pack-b/acme-task-c/2026-09-03-bbb", "shaB")
	p.BaseRef = "main"
	repo.Pulls = []world.Pull{p}
	repo.Runs["shaB"] = []land.Run{{Name: "ci", Status: "completed", Conclusion: "success"}}
	repo.Dispatchable[land.CIWorkflow] = true
	got, _ := resolveOn(sim.NewGitHub(sim.NewClock(targetNow)), repo, "supersede_existing_pr", land.AutoMerge)
	if got.Landed != 5 {
		t.Fatalf("%+v", got)
	}
	at := slices.Index(repo.Log, "dispatch "+land.CIWorkflow+" main")
	if at < 0 || at < slices.Index(repo.Log, "merge #5 shaB Claudinite-Task: "+targetTask) {
		t.Errorf("%v", repo.Log)
	}
}
