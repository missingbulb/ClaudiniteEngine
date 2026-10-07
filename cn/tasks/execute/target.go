package execute

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/workitem"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/land"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/world"
)

// The three target modes.
const (
	ModeNone  = "none"
	ModeFresh = "fresh"
	ModeAmend = "amend"
)

// BranchRoot is the root every branch the executor mints lives under.
const BranchRoot = "claudinite"

// TaskBranchPrefix is a task's branch family.
func TaskBranchPrefix(taskID string) string { return BranchRoot + "/" + taskID + "/" }

// MintBranch is one run's branch: the task's prefix, the day, a seed.
func MintBranch(taskID string, now time.Time, seed string) string {
	return TaskBranchPrefix(taskID) + now.UTC().Format("2006-01-02") + "-" + seed
}

// Target is which branch and pull request a run works on, decided once
// after the go and handed to both phases. Error is a read the resolver
// could not make, which parks the run; Landed is an incumbent the
// resolution landed, which ends the occurrence.
type Target struct {
	Mode       string
	Branch     string
	PR         int
	Supersedes []int
	Landed     int
	Reason     string
	Error      string
}

// Env is the target in code-work's environment: three variables in every
// mode, so a worker can tell "no target" from an executor that sets none.
func (t Target) Env() []string {
	mode := t.Mode
	if mode == "" {
		mode = ModeNone
	}
	pr := ""
	if t.PR != 0 {
		pr = strconv.Itoa(t.PR)
	}
	return []string{"CLAUDINITE_TARGET_MODE=" + mode, "CLAUDINITE_TARGET_BRANCH=" + t.Branch, "CLAUDINITE_TARGET_PR=" + pr}
}

// Item is the target as the item's fields carry it to the agent.
func (t Target) Item() workitem.Target {
	return workitem.Target{Mode: t.Mode, Branch: t.Branch, PR: t.PR, Supersedes: t.Supersedes}
}

// TaskPullsOf is a task's open pull requests, newest first: those on its
// branch prefix, and those whose head commit's trailer names it.
func TaskPullsOf(pulls []world.Pull, taskID string, headTaskOf func(world.Pull) string) []world.Pull {
	prefix := TaskBranchPrefix(taskID)
	var out []world.Pull
	for _, p := range pulls {
		if strings.HasPrefix(p.HeadRef, prefix) || (headTaskOf != nil && headTaskOf(p) == taskID) {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Number > out[b].Number })
	return out
}

// PlanIn is what the planner decides over: the incumbents newest first,
// the newest one's mergeability where the outcome amends, its disposition
// where it supersedes (merge meaning the shell already landed it).
type PlanIn struct {
	Outcome     string
	Incumbents  []world.Pull
	Mergeable   *bool
	Disposition land.Disposition
	Branch      string
}

// PlanTarget is the decision, pure. An outcome the contract does not know
// is an error: a word the executor cannot read stops the run rather than
// picking a mode for it.
func PlanTarget(in PlanIn) (Target, error) {
	canonical := taskspec.CanonicalOutcome(in.Outcome)
	if canonical == "" {
		return Target{}, fmt.Errorf("%q is not a legal outcome ceiling", in.Outcome)
	}
	none := func(reason string) Target { return Target{Mode: ModeNone, Supersedes: []int{}, Reason: reason} }
	fresh := func(reason string) Target {
		return Target{Mode: ModeFresh, Branch: in.Branch, Supersedes: []int{}, Reason: reason}
	}
	if !taskspec.OpensPullRequest(canonical) {
		return none("this task changes no code"), nil
	}
	if canonical == "fresh_pr" {
		return fresh("a fresh branch; earlier pull requests of this task are left as they are"), nil
	}
	if len(in.Incumbents) == 0 {
		if canonical == "amend_existing_or_create_new_pr" {
			return fresh("no open pull request of this task to amend"), nil
		}
		return fresh("no open pull request of this task to supersede"), nil
	}
	newest := in.Incumbents[0]
	if canonical == "amend_existing_or_create_new_pr" {
		if in.Mergeable == nil {
			return Target{Error: fmt.Sprintf("#%d's mergeability could not be read, and this task amends rather than forks - nothing ran", newest.Number)}, nil
		}
		reason := fmt.Sprintf("amending #%d, which has no conflicts with its base", newest.Number)
		if !*in.Mergeable {
			reason = fmt.Sprintf("amending #%d, which CONFLICTS with its base - resolve that conflict as part of this run, by merging the base branch in; do not open a second pull request", newest.Number)
		}
		return Target{Mode: ModeAmend, Branch: newest.HeadRef, PR: newest.Number, Supersedes: []int{}, Reason: reason}, nil
	}
	numbers := make([]int, len(in.Incumbents))
	refs := make([]string, len(in.Incumbents))
	for i, p := range in.Incumbents {
		numbers[i] = p.Number
		refs[i] = "#" + strconv.Itoa(p.Number)
	}
	if in.Disposition == land.DispMerge {
		t := none(fmt.Sprintf("landed #%d, this task's previous delivery, which had concluded green — the occurrence ends there", newest.Number))
		t.Landed, t.Supersedes = newest.Number, numbers[1:]
		return t, nil
	}
	t := fresh("a fresh branch; " + strings.Join(refs, ", ") + " close as superseded once this run's pull request exists")
	t.Supersedes = numbers
	return t, nil
}

// MergeableReads and MergeableWait cover GitHub's lazy mergeability: the
// first read after a push answers null while it computes.
const (
	MergeableReads = 3
	MergeableWait  = 2 * time.Second
)

// TargetIn is the resolver's reach.
type TargetIn struct {
	Issues   world.Issues
	Repo     world.Repo
	Pulls    world.Pulls
	Lane     land.API
	TaskID   string
	Outcome  string
	Delivery string
	Now      time.Time
	Seed     string
	Sleep    func(time.Duration)
	Log      func(string)
	// Judgement is the task's policy a green incumbent's diff must pass
	// before it lands; nil judges nothing.
	Judgement *land.Judgement
}

// ResolveTarget reads exactly what the plan needs: nothing for the two
// outcomes that involve no existing pull request; for the other two the
// open list, the head trailer of each one off the prefix, and the newest
// incumbent's mergeability (amend) or the runs on its head (supersede,
// landing it there when the member's delivery allows and they concluded
// green).
func ResolveTarget(in TargetIn) Target {
	canonical := taskspec.CanonicalOutcome(in.Outcome)
	if canonical == "" {
		return Target{Error: fmt.Sprintf("%q is not a legal outcome ceiling", in.Outcome)}
	}
	branch := MintBranch(in.TaskID, in.Now, in.Seed)
	planned := func(p PlanIn) Target {
		p.Outcome, p.Branch = canonical, branch
		t, err := PlanTarget(p)
		if err != nil {
			return Target{Error: err.Error()}
		}
		return t
	}
	if canonical == taskspec.OutcomeNoPR || canonical == "fresh_pr" {
		return planned(PlanIn{})
	}
	listed, err := world.OpenPulls(in.Repo)
	if err != nil {
		return Target{Error: fmt.Sprintf("could not list the open pull requests (%v) — an unreadable list is not an empty one", err)}
	}
	prefix := TaskBranchPrefix(in.TaskID)
	heads := map[int]string{}
	for _, p := range listed {
		if strings.HasPrefix(p.HeadRef, prefix) || p.HeadSHA == "" {
			continue
		}
		if c, err := in.Repo.Commit(p.HeadSHA); err == nil {
			heads[p.Number] = workitem.TaskFromMessage(c.Message)
		}
	}
	incumbents := TaskPullsOf(listed, in.TaskID, func(p world.Pull) string { return heads[p.Number] })
	if len(incumbents) == 0 {
		return planned(PlanIn{})
	}
	newest := incumbents[0]
	if canonical == "amend_existing_or_create_new_pr" {
		var mergeable *bool
		for n := 0; n < MergeableReads; n++ {
			if n > 0 {
				in.Sleep(MergeableWait)
			}
			m, err := in.Pulls.Mergeable(newest.Number)
			if err != nil {
				break
			}
			if m != nil {
				mergeable = m
				break
			}
		}
		return planned(PlanIn{Incumbents: incumbents, Mergeable: mergeable})
	}
	disposition := land.DispClose
	if runs, err := in.Lane.RunsForSHA(newest.HeadSHA); err == nil {
		disposition = land.PullDisposition(in.Delivery, runs)
	}
	if disposition == land.DispMerge {
		pr := land.PR{Number: newest.Number, NodeID: newest.NodeID, HeadRef: newest.HeadRef, HeadSHA: newest.HeadSHA, Base: newest.BaseRef}
		mergeErr, tidy := land.Pinned(in.Lane, pr, "", in.TaskID, in.Judgement)
		if mergeErr != nil {
			in.Log(fmt.Sprintf("could not land #%d (%v) — superseding it instead", newest.Number, mergeErr))
			disposition = land.DispClose
		} else {
			in.Log(fmt.Sprintf("landed #%d — %s's previous delivery had concluded green and was never merged", newest.Number, in.TaskID))
			if tidy != nil {
				in.Log(fmt.Sprintf("could not delete branch %s (%v)", newest.HeadRef, tidy))
			}
			if pr.Base != "" {
				land.DispatchBaseCI(in.Lane, pr.Base, in.Log)
			}
		}
	}
	return planned(PlanIn{Incumbents: incumbents, Disposition: disposition})
}

// CloseSuperseded closes the pull requests a run superseded, now that its
// own exists: a comment naming the successor, the close, the branch
// tidied. Best-effort throughout: the successor is the deliverable.
func CloseSuperseded(gh world.Issues, pulls world.Pulls, lane land.Merger, numbers []int, successor int, log func(string)) {
	for _, n := range numbers {
		p, err := pulls.Pull(n)
		if err != nil {
			log(fmt.Sprintf("could not close #%d (%v) — leaving it", n, err))
			continue
		}
		if _, err := gh.Comment(n, fmt.Sprintf("Superseded by #%d, a later run of the same task. Closing this one.", successor)); err != nil && !errors.Is(err, world.ErrGone) {
			log(fmt.Sprintf("could not comment on #%d (%v)", n, err))
		}
		if err := pulls.ClosePull(n); err != nil {
			log(fmt.Sprintf("could not close #%d (%v) — leaving it", n, err))
			continue
		}
		log(fmt.Sprintf("closed #%d — superseded by #%d", n, successor))
		if p.HeadRef != "" {
			if err := lane.DeleteBranch(p.HeadRef); err != nil {
				log(fmt.Sprintf("could not delete branch %s (%v)", p.HeadRef, err))
			}
		}
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
