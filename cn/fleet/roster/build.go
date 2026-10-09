package roster

import (
	"github.com/missingbulb/ClaudiniteEngine/cn/fleet"
)

// Build walks repos once: each in-scope repository's tree is read, and a
// covered, awake cn member is measured against the shelf. Nothing is read
// of the manager, an archived repository, a fork or an excluded one.
func Build(gh fleet.GH, repos []fleet.Repo, home string, cfg fleet.Config, shelf fleet.Shelf) []Entry {
	var roster []Entry
	for _, r := range repos {
		scope := fleet.Scope(r, home, cfg)
		e := Entry{FullName: r.Lower(), DisplayName: r.FullName, IsHome: scope == fleet.ScopeHome,
			Archived: r.Archived, Fork: r.Fork, Excluded: scope == fleet.ScopeExcluded}
		roster = append(roster, Measure(gh, r, scope, shelf, e))
	}
	return roster
}

// Measure fills one in-scope entry's facts, and its verdict from the
// same reads.
func Measure(gh fleet.GH, r fleet.Repo, scope string, shelf fleet.Shelf, e Entry) Entry {
	if scope != fleet.ScopeIn {
		e.Verdict = fleet.Judge(r, scope, fleet.Member{}, nil, nil, nil, nil)
		return e
	}
	m, err := fleet.ReadMember(gh, r.FullName, r.Branch())
	if err != nil {
		e.DeclarationError, e.Grant = err.Error(), fleet.IsGrant(err)
		e.Verdict = fleet.Judge(r, scope, m, err, nil, nil, nil)
		return e
	}
	e.Verdict = fleet.Judge(r, scope, m, nil, nil, nil, nil)
	if !m.Covered() {
		return e
	}
	e.Covered, e.Dormant = true, m.Dormant
	if e.Dormant {
		return e
	}
	if m.Shape == fleet.ShapeNode {
		f := fleet.Classify(fleet.FreshIn{Shape: m.Shape})
		e.Freshness = &f
		return e
	}
	has, err := fleet.FileExists(gh, r.FullName, fleet.SchedulerPath)
	if err == nil {
		var in fleet.FreshIn
		if in, err = fleet.Measure(m, has, shelf); err == nil {
			f := fleet.Classify(in)
			e.Freshness = &f
			e.Verdict = fleet.Judge(r, scope, m, nil, &has, &in, nil)
			return e
		}
		e.Verdict = fleet.Judge(r, scope, m, nil, &has, nil, err)
	} else {
		e.Verdict = fleet.Judge(r, scope, m, nil, nil, nil, err)
	}
	e.FreshnessError, e.Grant = err.Error(), fleet.IsGrant(err)
	return e
}
