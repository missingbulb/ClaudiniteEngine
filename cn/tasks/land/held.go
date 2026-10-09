package land

import (
	"errors"
	"fmt"
	"time"
)

// HeldWait bounds the wait for a pull request's pull_request runs to
// appear; GitHub creates them within seconds of the pull request opening.
const HeldWait = 90 * time.Second

// Approver reads the runs on a head and approves one held for approval.
type Approver interface {
	RunsForSHA(sha string) ([]Run, error)
	ApproveRun(id int64) error
}

// HeldCI is what StartHeldCI found on a pull request's head: Seen
// pull_request runs, of which Started will execute, approved here or
// already going on their own.
type HeldCI struct {
	Seen, Started, Approved int
}

// StartHeldCI makes the pull_request runs on sha the pull request's
// checks: GitHub holds the runs of a pull request the job token opened at
// action_required until someone approves them, so it waits up to wait for
// them to appear and approves each held one. A run that started on its own
// needs nothing. Seen is 0 when no pull_request run appeared within the
// bound, the caller's cue to start its checks another way.
func StartHeldCI(a Approver, sha string, wait time.Duration, sleep func(time.Duration), log func(string)) (HeldCI, error) {
	for waited := time.Duration(0); ; waited += PollEvery {
		runs, err := a.RunsForSHA(sha)
		if err != nil {
			return HeldCI{}, err
		}
		var out HeldCI
		for _, r := range runs {
			if r.Event != "pull_request" {
				continue
			}
			out.Seen++
			if r.Conclusion != "action_required" {
				out.Started++
				continue
			}
			if err := a.ApproveRun(r.ID); err != nil {
				hint := ""
				var se *StatusError
				if errors.As(err, &se) && se.Status == 403 {
					hint = " — the workflow needs `actions: write` to approve a held run"
				}
				log(fmt.Sprintf("could not approve the held run %d (%s) on %s (%v)%s", r.ID, r.Name, short(sha), err, hint))
				continue
			}
			out.Started++
			out.Approved++
			log(fmt.Sprintf("approved the held pull_request run %d (%s) on %s — it is the PR's check", r.ID, r.Name, short(sha)))
		}
		if out.Seen > 0 {
			if out.Approved == 0 && out.Started > 0 {
				log(fmt.Sprintf("%d pull_request run(s) on %s started without approval — nothing to approve", out.Started, short(sha)))
			}
			return out, nil
		}
		if waited >= wait {
			return out, nil
		}
		sleep(PollEvery)
	}
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
