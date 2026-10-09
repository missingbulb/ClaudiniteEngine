// Package roster is the fleet-roster sweep: one walk of every repository
// under the owner answering two questions of each, whether it is a member
// (the adoption issues in the manager) and whether that membership
// still means anything (the report's freshness section, which files
// nothing). Each repository's tree is read once and both halves consume
// the one roster, so a repository has one membership verdict and the
// report one failure boundary.
package roster

import (
	"fmt"

	"github.com/missingbulb/ClaudiniteEngine/cn/fleet"
)

// Entry is one repository's facts, never a verdict: the two questions
// read some facts differently (an excluded repository is out of both;
// a dormant member is covered and not measured).
type Entry struct {
	FullName    string `json:"fullName"`
	DisplayName string `json:"displayName"`
	IsHome      bool   `json:"isHome"`
	Archived    bool   `json:"archived"`
	Fork        bool   `json:"fork"`
	Excluded    bool   `json:"excluded"`
	// Covered says the tree carries a declaration.
	Covered          bool             `json:"covered"`
	DeclarationError string           `json:"declarationError"`
	Dormant          bool             `json:"dormant"`
	Freshness        *fleet.Freshness `json:"freshness"`
	FreshnessError   string           `json:"freshnessError"`
	// Grant says an error above is the token's grant.
	Grant bool `json:"-"`
	// Verdict is the same reads judged as `cn fleet judge` would, the
	// roster artifact's row.
	Verdict fleet.Verdict `json:"-"`
}

// Coverage is the coverage question's buckets; every repository but the
// manager lands in exactly one.
type Coverage struct {
	Covered   []string `json:"covered"`
	Dormant   []string `json:"dormant"`
	Uncovered []string `json:"uncovered"`
	Ignored   []string `json:"ignored"`
	Skipped   []string `json:"skipped"`
	Unknown   []string `json:"unknown"`
}

func reason(e Entry) string {
	if e.Archived {
		return "archived"
	}
	return "fork"
}

// CoverageView buckets the roster for the coverage question. An ignored
// repository is named before anything is asked of it.
func CoverageView(roster []Entry) Coverage {
	c := Coverage{Covered: []string{}, Dormant: []string{}, Uncovered: []string{}, Ignored: []string{}, Skipped: []string{}, Unknown: []string{}}
	for _, e := range roster {
		switch {
		case e.IsHome:
		case e.Excluded:
			c.Ignored = append(c.Ignored, e.FullName)
		case e.Archived || e.Fork:
			c.Skipped = append(c.Skipped, fmt.Sprintf("%s (%s)", e.DisplayName, reason(e)))
		case e.DeclarationError != "":
			c.Unknown = append(c.Unknown, e.DisplayName+" — "+e.DeclarationError)
		case e.Covered && e.Dormant:
			c.Dormant = append(c.Dormant, e.FullName)
		case e.Covered:
			c.Covered = append(c.Covered, e.FullName)
		default:
			c.Uncovered = append(c.Uncovered, e.FullName)
		}
	}
	return c
}

// Fresh is a measured member and how fresh.
type Fresh struct {
	FullName string `json:"fullName"`
	Detail   string `json:"detail"`
}

// Unhealthy is a measured member and its root cause.
type Unhealthy struct {
	FullName string `json:"fullName"`
	State    string `json:"state"`
	Detail   string `json:"detail"`
}

// Freshness is the freshness question's buckets.
type Freshness struct {
	Fresh      []Fresh     `json:"fresh"`
	Unhealthy  []Unhealthy `json:"unhealthy"`
	Node       []string    `json:"node,omitempty"`
	Dormant    []string    `json:"dormant"`
	Ignored    []string    `json:"ignored"`
	OutOfScope []string    `json:"outOfScope"`
	Unknown    []string    `json:"unknown"`
}

// FreshnessView buckets the roster for the freshness question: a dormant
// member and a node member are named and not measured.
func FreshnessView(roster []Entry) Freshness {
	f := Freshness{Fresh: []Fresh{}, Unhealthy: []Unhealthy{}, Dormant: []string{}, Ignored: []string{}, OutOfScope: []string{}, Unknown: []string{}}
	for _, e := range roster {
		switch {
		case e.IsHome:
		case e.Excluded:
			f.Ignored = append(f.Ignored, e.FullName)
		case e.Archived || e.Fork:
			f.OutOfScope = append(f.OutOfScope, fmt.Sprintf("%s (%s)", e.DisplayName, reason(e)))
		case e.DeclarationError != "":
			f.Unknown = append(f.Unknown, e.DisplayName+" — "+e.DeclarationError)
		case !e.Covered:
			f.OutOfScope = append(f.OutOfScope, e.DisplayName+" (uncovered — the adoption half's subject)")
		case e.Dormant:
			f.Dormant = append(f.Dormant, e.FullName)
		case e.FreshnessError != "":
			f.Unknown = append(f.Unknown, e.DisplayName+" — "+e.FreshnessError)
		case e.Freshness == nil:
			f.Unknown = append(f.Unknown, e.DisplayName+" — not measured")
		case e.Freshness.State == fleet.StateNode:
			f.Node = append(f.Node, e.FullName)
		case e.Freshness.State == fleet.StateFresh:
			f.Fresh = append(f.Fresh, Fresh{e.FullName, e.Freshness.Detail})
		default:
			f.Unhealthy = append(f.Unhealthy, Unhealthy{e.FullName, e.Freshness.State, e.Freshness.Detail})
		}
	}
	return f
}
