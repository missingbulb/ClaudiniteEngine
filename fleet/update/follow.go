package update

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/fleet"
)

// The terminal outcomes a followed member ends on.
const (
	AlreadyCurrent = "already-current"
	Updated        = "updated"
	Moved          = "moved"
	NeverStarted   = "never-started"
	DidNotUpdate   = "did-not-update"
	Unknown        = "unknown"
)

// IsSuccess reports whether an outcome is a member that is where it
// should be.
func IsSuccess(o string) bool { return o == AlreadyCurrent || o == Updated || o == Moved }

// PollMS is the poll ladder: quick first, then backing off, capped.
var PollMS = []int64{15000, 30000, 45000, 60000}

// PollDelay is the wait after round.
func PollDelay(round int) int64 {
	if round >= len(PollMS) {
		round = len(PollMS) - 1
	}
	return PollMS[round]
}

// Followed is one member's outcome.
type Followed struct {
	Fired
	Outcome string
	Detail  string
}

// Clock is the follow's time, in milliseconds, and its wait.
type Clock struct {
	Now   func() int64
	Sleep func(ms int64)
	Log   func(string)
}

// Follow polls every fired member to a terminal outcome or the budget.
// read is the member's state now, fresh being its terminal; started says
// whether a scheduler run started since it was fired. The report order is
// the fired order.
func Follow(members []Fired, read func(Fired) (fleet.Freshness, error), started func(Fired) (bool, error), budgetMS int64, c Clock) []Followed {
	type state struct {
		lastErr  string
		last     *fleet.Freshness
		finished *Followed
	}
	st := make([]state, len(members))
	pending := func() []int {
		var out []int
		for i := range members {
			if st[i].finished == nil {
				out = append(out, i)
			}
		}
		return out
	}
	deadline := c.Now() + budgetMS
	done := 0
	for round := 0; len(pending()) > 0; round++ {
		for _, i := range pending() {
			v, err := read(members[i])
			if err != nil {
				st[i].lastErr = err.Error()
				continue
			}
			st[i].lastErr = ""
			if v.State == fleet.StateFresh {
				out := Updated
				switch {
				case members[i].Node:
					out = Moved
				case members[i].WasFresh:
					out = AlreadyCurrent
				}
				st[i].finished = &Followed{Fired: members[i], Outcome: out, Detail: v.Detail}
				done++
				continue
			}
			last := v
			st[i].last = &last
		}
		left := pending()
		if len(left) == 0 {
			break
		}
		remaining := deadline - c.Now()
		delay := PollDelay(round)
		if remaining <= 0 {
			break
		}
		names := make([]string, 0, len(left))
		for _, i := range left {
			names = append(names, members[i].FullName)
		}
		c.Log(fmt.Sprintf("- %d/%d current; still following %s", done, len(members), strings.Join(names, ", ")))
		c.Sleep(min(delay, remaining))
	}
	for _, i := range pending() {
		s := &st[i]
		ran, err := started(members[i])
		if err != nil {
			s.lastErr = err.Error()
		}
		f := Followed{Fired: members[i]}
		switch {
		case s.lastErr != "":
			f.Outcome, f.Detail = Unknown, "could not be read: "+s.lastErr
		case !ran:
			f.Outcome, f.Detail = NeverStarted, "no "+fleet.Scheduler+" run started since the dispatch — the run was queued and never ran"
		default:
			still := "not at the published versions"
			if s.last != nil {
				still = s.last.Detail
			}
			f.Outcome, f.Detail = DidNotUpdate, "its scheduler ran, and it is still "+still
		}
		s.finished = &f
	}
	out := make([]Followed, len(members))
	for i := range members {
		out[i] = *st[i].finished
	}
	return out
}

// StartedSince reports whether repo's scheduler has a workflow_dispatch
// run created at or after since; a listing that does not answer is an
// error, never absence.
func StartedSince(gh fleet.GH, repo, since string) (bool, error) {
	r, err := gh.Get("/repos/" + repo + "/actions/workflows/" + fleet.Scheduler + "/runs?event=workflow_dispatch&per_page=20")
	if err != nil {
		return false, err
	}
	if r.Status != 200 {
		return false, fmt.Errorf("could not list %s runs: %d", fleet.Scheduler, r.Status)
	}
	var body struct {
		Runs []struct {
			CreatedAt string `json:"created_at"`
		} `json:"workflow_runs"`
	}
	_ = json.Unmarshal(r.JSON, &body)
	at, err := time.Parse(time.RFC3339, since)
	if err != nil {
		return false, err
	}
	for _, run := range body.Runs {
		if t, err := time.Parse(time.RFC3339, run.CreatedAt); err == nil && !t.Before(at) {
			return true, nil
		}
	}
	return false, nil
}

// Read is the follow's read of one fired member: a cn member's freshness
// now, or whether a node member's stamp has moved.
func Read(gh fleet.GH, shelf fleet.Shelf) func(Fired) (fleet.Freshness, error) {
	return func(f Fired) (fleet.Freshness, error) {
		m, err := fleet.ReadMember(gh, f.Repo.FullName, f.Repo.Branch())
		if err != nil {
			return fleet.Freshness{}, err
		}
		if !m.Covered() {
			return fleet.Freshness{State: "no-declaration", Detail: "the settings file disappeared mid-run"}, nil
		}
		if f.Node {
			if m.Shape == fleet.ShapeNode && fleet.NodeStamp(m.Node) == f.Stamp {
				return fleet.Freshness{State: "not-moved", Detail: "a Node member whose declaration's stamp has not moved"}, nil
			}
			return fleet.Freshness{State: fleet.StateFresh, Detail: "its declaration's stamp moved"}, nil
		}
		return Freshness(gh, m, shelf)
	}
}
