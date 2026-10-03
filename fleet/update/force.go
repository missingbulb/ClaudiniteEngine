// Package update is the fleet-update lever: dispatch every covered
// member's own scheduler with wake: update, then follow each to an
// outcome read off the member itself. It reports outcomes, never
// dispatches: already-current is judged from a read taken before firing,
// updated from the follow, and a dispatched member that never reached its
// own update's versions fails the run.
package update

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/fleet"
)

// ForcedTask is the member-side task the lever wakes: the engine's
// engine/update on a cn member, Node's update on a Node member.
const ForcedTask = "update"

// DefaultFollowMinutes bounds the follow when FOLLOW_MINUTES is unset.
const DefaultFollowMinutes = 20

// Filter is the REPOS parameter: lowercased owner/name in the order given.
type Filter []string

// Has reports whether repo is named.
func (f Filter) Has(repo string) bool {
	for _, r := range f {
		if r == repo {
			return true
		}
	}
	return false
}

// ParseRepoFilter reads REPOS: whitespace-separated bare names (qualified
// with owner) or owner/name, lowercased; nil names every member.
func ParseRepoFilter(raw, owner string) Filter {
	var out Filter
	for _, n := range regexp.MustCompile(`\s+`).Split(raw, -1) {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if !strings.Contains(n, "/") {
			n = owner + "/" + n
		}
		n = strings.ToLower(n)
		if !out.Has(n) {
			out = append(out, n)
		}
	}
	return out
}

// Row is one repository's line in the report.
type Row struct {
	FullName string `json:"fullName"`
	State    string `json:"state"`
	Detail   string `json:"detail"`
}

// ClassifyScope is why r is out of this dispatch before its tree is read,
// nil when it is in. The manager is not exempt: it is a member with a
// mount to update. There is no canon row.
func ClassifyScope(r fleet.Repo, cfg fleet.Config, filter Filter) *Row {
	full := r.Lower()
	switch {
	case r.Archived:
		return &Row{full, "out-of-scope", "archived"}
	case r.Fork:
		return &Row{full, "out-of-scope", "a fork"}
	case cfg.Excluded(full):
		return &Row{full, "excluded", "on the claudinite-fleet-sheepdog config's exclude list"}
	case filter != nil && !filter.Has(full):
		return &Row{full, "filtered-out", "not in this run's REPOS filter"}
	}
	return nil
}

// Fired is one dispatched member, with what the pre-dispatch read said.
type Fired struct {
	Row
	FiredAt  string
	WasFresh bool
	Repo     fleet.Repo
	// Node and Stamp are a node member's: it is followed by its stamp
	// moving at all.
	Node  bool
	Stamp string
}

// Options are the lever's parameters.
type Options struct {
	DryRun, IncludeDormant bool
	Filter                 Filter
	Now                    func() string
}

// Force dispatches every in-scope covered member, reading freshness first.
func Force(gh fleet.GH, repos []fleet.Repo, cfg fleet.Config, shelf fleet.Shelf, o Options) (fired []Fired, skipped, failed []Row) {
	for _, r := range repos {
		if s := ClassifyScope(r, cfg, o.Filter); s != nil {
			skipped = append(skipped, *s)
			continue
		}
		full := r.Lower()
		m, err := fleet.ReadMember(gh, r.FullName, r.Branch())
		if err != nil {
			failed = append(failed, Row{full, "error", "could not read its declaration: " + err.Error()})
			continue
		}
		if !m.Covered() {
			skipped = append(skipped, Row{full, "uncovered", "no tracked declaration — adoption is the census's business, and there is nothing there to update"})
			continue
		}
		if m.Dormant && !o.IncludeDormant {
			skipped = append(skipped, Row{full, "dormant", "self-declared dormant — pass INCLUDE_DORMANT=true to force it anyway"})
			continue
		}
		if o.DryRun {
			fired = append(fired, Fired{Row: Row{full, "would-fire", fmt.Sprintf("would dispatch %s@%s to wake %s", fleet.Scheduler, r.Branch(), ForcedTask)}, Repo: r})
			continue
		}
		f := Fired{Repo: r, Node: m.Shape == fleet.ShapeNode}
		if f.Node {
			f.Stamp = fleet.NodeStamp(m.Node)
		} else {
			f.WasFresh = isFresh(gh, m, shelf)
		}
		f.FiredAt = o.Now()
		d, err := fleet.FireScheduler(gh, r.FullName, r.Branch(), ForcedTask)
		if err != nil {
			d = fleet.Dispatch{State: "error", Detail: err.Error()}
		}
		if d.State != "fired" {
			failed = append(failed, Row{full, d.State, d.Detail})
			continue
		}
		f.Row = Row{full, d.State, d.Detail}
		fired = append(fired, f)
	}
	return fired, skipped, failed
}

// isFresh is the pre-dispatch read; unreadable reads as not fresh, so a
// success is reported as updated, claiming no more than was observed.
func isFresh(gh fleet.GH, m fleet.Member, shelf fleet.Shelf) bool {
	f, err := Freshness(gh, m, shelf)
	return err == nil && f.State == fleet.StateFresh
}

// Freshness is a cn member's freshness now.
func Freshness(gh fleet.GH, m fleet.Member, shelf fleet.Shelf) (fleet.Freshness, error) {
	has, err := fleet.FileExists(gh, m.Repo, fleet.SchedulerPath)
	if err != nil {
		return fleet.Freshness{}, err
	}
	in, err := fleet.Measure(m, has, shelf)
	if err != nil {
		return fleet.Freshness{}, err
	}
	return fleet.Classify(in), nil
}
