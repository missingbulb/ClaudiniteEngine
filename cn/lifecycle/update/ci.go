package update

import (
	"fmt"
	"strconv"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/githubapi"
	"github.com/missingbulb/ClaudiniteEngine/cn/tasks/land"
)

// ciEvents are the events whose claudinite-ci.yml run on an update PR's
// head is a verdict: its own pull_request run, approved, or one
// dispatched on its branch.
var ciEvents = []string{"pull_request", "workflow_dispatch"}

// headRuns is the updater's GitHub as the landing lane's approver.
type headRuns struct{ d Deps }

func (h headRuns) RunsForSHA(sha string) ([]land.Run, error) {
	runs, err := h.d.GitHub.HeadRuns(sha)
	if err != nil {
		return nil, err
	}
	out := make([]land.Run, 0, len(runs))
	for _, r := range runs {
		out = append(out, landRun(r))
	}
	return out, nil
}

func (h headRuns) ApproveRun(id int64) error {
	err := h.d.GitHub.ApproveRun(id)
	if s := githubapi.StatusOf(err); s != 0 {
		return &land.StatusError{Status: s, Message: err.Error()}
	}
	return err
}

func landRun(r githubapi.Run) land.Run {
	return land.Run{ID: r.ID, Name: r.Name, Event: r.Event, Status: r.Status, Conclusion: r.Conclusion}
}

func (d Deps) sleep(t time.Duration) {
	if d.Sleep != nil {
		d.Sleep(t)
		return
	}
	time.Sleep(t)
}

// startPRCI starts update PR n's checks on its head sha: GitHub holds the
// pull_request runs of a PR the job token opened at action_required, so
// it approves them (land.StartHeldCI), waiting up to wait for them to
// appear. Only when no claudinite-ci.yml pull_request run is among them
// does it dispatch that workflow on branch with pr=n, whose land job lands
// the PR; h.Seen is then 0. A held run it could not approve is an error:
// nothing else would start it.
func startPRCI(d Deps, n int, branch, sha string, wait time.Duration) (land.HeldCI, error) {
	h, err := land.StartHeldCI(headRuns{d}, sha, wait, d.sleep, func(s string) { fmt.Fprintln(d.Out, s) })
	if err != nil {
		return h, err
	}
	if h.Seen > 0 && h.Started == 0 {
		return h, fmt.Errorf("#%d's pull_request runs are held for approval and none could be approved: the update job needs `actions: write`", n)
	}
	if h.Seen > 0 {
		// Another workflow's run is no verdict: the landing waits on this one's.
		runs, err := d.GitHub.WorkflowRuns(CIWorkflow, sha)
		if err != nil {
			return h, err
		}
		for _, r := range runs {
			if r.Event == "pull_request" {
				return h, nil
			}
		}
		h = land.HeldCI{}
	}
	fmt.Fprintf(d.Out, "no %s pull_request run appeared on #%d's head within %s; dispatching it on %s, whose land job lands it\n", CIWorkflow, n, wait, branch)
	return h, d.GitHub.Dispatch(CIWorkflow, branch, map[string]string{"pr": strconv.Itoa(n)})
}

// landOnCI waits, within the landing lane's bounds (land.LandAttempt), for
// update PR n's CI on sha and lands it on that evidence through Land, as
// the lane lands a delivered PR. Unlanded, verdict is "" and pending says
// whether a verdict is still to come.
func landOnCI(d Deps, n int, sha string) (verdict string, pending bool, err error) {
	for waited := time.Duration(0); ; waited += land.PollEvery {
		all, err := d.GitHub.WorkflowRuns(CIWorkflow, sha)
		if err != nil {
			return "", false, err
		}
		var runs []land.Run
		visible := 0
		for _, r := range all {
			if !has(ciEvents, r.Event) {
				continue
			}
			runs = append(runs, landRun(r))
			if r.Conclusion != "action_required" {
				visible++
			}
		}
		switch land.LandAttempt(land.AutoMerge, runs, 1, waited) {
		case land.AttemptPoll:
			d.sleep(land.PollEvery)
			continue
		case land.AttemptGiveUp:
			fmt.Fprintf(d.Out, "#%d not landed in this run: %s\n", n, land.FailureSummary(runs))
			return "", visible == 0 || land.PullDisposition(land.AutoMerge, runs) == land.DispWait, nil
		}
		v, err := Land(d, n, sha)
		return v, false, err
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
