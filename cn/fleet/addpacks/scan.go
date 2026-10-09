package addpacks

import (
	"fmt"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/fleet"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packindex"
)

// The scan: the question nothing else asks after adoption, whether a
// member's declared packs still match its tree. A pack's fingerprint is
// consulted once at adoption and never again, so a repo that grows into a
// pack is never told it exists; this is the re-ask. It is a suspicion,
// never a verdict, which is why it is an issue the member's own agent
// confirms, and a `not planned` close is a standing answer it honours.

// SuspectedBody is a suspected list's body: the packs and what each is
// for, and the fingerprints that could not be decided from outside.
// settings is the member's declaration file.
func SuspectedBody(fits []string, undecided []Undecided, settings string, packs []packindex.CatalogPack) string {
	lines := []string{
		"This repo carries file shapes that fingerprint packs its `" + settings + "`",
		"does not declare. A fingerprint only **suspects** a pack is wanted — declaring one is",
		"this project's call, which is why this is an issue and not a failing check.",
		"",
		"## Suspected packs",
		"",
	}
	ids := byID(packs)
	for _, id := range fits {
		lines = append(lines, belongsLine(id, ids))
	}
	lines = append(lines,
		"",
		"This repo's own **adopt-requested-packs** task acts on this list: its agent confirms",
		"each suspicion against the checkout, adopts what survives through the adopt-pack skill",
		"(one reviewed PR on this repo), and declines the rest with a reason here. To decline",
		"everything for good, close this issue `not planned` — that is a standing answer the",
		"weekly fleet scan honours rather than re-suggesting.",
	)
	if len(undecided) > 0 {
		lines = append(lines,
			"",
			"## Not decided from outside",
			"",
			"These fingerprints read file **contents**, which the fleet's REST sweep cannot settle",
			"within its read budget. The agent working this issue has the repo checked out and can",
			"answer them exactly, by running each pack's fingerprint over the checkout:",
			"",
		)
		for _, u := range undecided {
			lines = append(lines, fmt.Sprintf("- `%s` — %s", u.ID, u.Why))
		}
	}
	lines = append(lines,
		"",
		"Converged weekly by the fleet's add-missing-packs scan: it closes this `completed` once",
		"the declaration carries the packs (or the shape stops suggesting them).",
	)
	return strings.Join(lines, "\n")
}

// Converged is one member's suspected-list convergence: the action taken,
// "" for none, and whether the member ends the sweep with the list open.
type Converged struct {
	Action   *string `json:"action"`
	OpenWork bool    `json:"openWork"`
}

func action(s string) *string { return &s }

// ConvergeSuspected brings one member's suspected list to this sweep's
// finding: closed when nothing is suspected, rewritten when open, left
// alone after a `not planned` close, opened otherwise.
func ConvergeSuspected(gh fleet.GH, repo string, fits []string, body string) (Converged, error) {
	open, closed, err := fleet.TitledIssues(gh, repo, IsWorkListTitle)
	if err != nil {
		return Converged{}, err
	}
	var existing *fleet.Issue
	for i := range open {
		if open[i].Title == SuspectedTitle {
			existing = &open[i]
			break
		}
	}
	if len(fits) == 0 {
		if existing == nil {
			return Converged{}, nil
		}
		if _, err := fleet.Expect(gh, "POST", fmt.Sprintf("/repos/%s/issues/%d/comments", repo, existing.Number),
			map[string]string{"body": "Closed by the fleet's add-missing-packs scan: this repo now declares every pack its shape fingerprints."}, 201); err != nil {
			return Converged{}, err
		}
		if _, err := fleet.Expect(gh, "PATCH", fmt.Sprintf("/repos/%s/issues/%d", repo, existing.Number),
			map[string]string{"state": "closed", "state_reason": "completed"}, 200); err != nil {
			return Converged{}, err
		}
		return Converged{Action: action(fmt.Sprintf("closed #%d (%s: fitted)", existing.Number, repo))}, nil
	}
	if existing != nil {
		written, err := Remark(gh, repo, *existing, MarkedBody(body, existing, 0))
		if err != nil {
			return Converged{}, err
		}
		if !written {
			return Converged{OpenWork: true}, nil
		}
		return Converged{Action: action(fmt.Sprintf("updated #%d (%s)", existing.Number, repo)), OpenWork: true}, nil
	}
	var prior *fleet.Issue
	for i := range closed {
		c := &closed[i]
		if c.Title != SuspectedTitle {
			continue
		}
		if prior == nil || closedAfter(c, prior) {
			prior = c
		}
	}
	if prior != nil && prior.StateReason != nil && *prior.StateReason == "not_planned" {
		return Converged{}, nil
	}
	if err := EnsureMark(gh, repo); err != nil {
		return Converged{}, err
	}
	n, err := openIssue(gh, repo, SuspectedTitle, MarkedBody(body, nil, OtherOpenWorkList(open, SuspectedTitle)), "suspected-packs")
	if err != nil {
		return Converged{}, err
	}
	return Converged{Action: action(fmt.Sprintf("opened #%d (%s)", n, repo)), OpenWork: true}, nil
}

func closedAfter(a, b *fleet.Issue) bool {
	at, bt := "", ""
	if a.ClosedAt != nil {
		at = *a.ClosedAt
	}
	if b.ClosedAt != nil {
		bt = *b.ClosedAt
	}
	return at > bt
}

// Finding is one member with fits.
type Finding struct {
	Repo string `json:"repo"`
	Fits
}

// Fire is a member to nudge: its scheduler adopts the open list.
type Fire struct {
	FullName      string `json:"fullName"`
	DefaultBranch string `json:"defaultBranch"`
}

// Scan is a scan's whole roster: every repo under the owner lands under
// exactly one state.
type Scan struct {
	Findings   []Finding `json:"findings"`
	Unknown    []string  `json:"unknown"`
	ToFire     []Fire    `json:"toFire"`
	Actions    []string  `json:"actions"`
	Fitted     []string  `json:"fitted"`
	Dormant    []string  `json:"dormant"`
	OutOfScope []string  `json:"outOfScope"`
	// Grant says an unknown member was the token's grant.
	Grant bool `json:"-"`
}

// RunScan fingerprints every in-scope member against the corpus its own
// channel sees and converges
// its suspected list; repos nil is the whole fleet, else the qualified
// names the run was scoped to. A member that cannot be swept is unknown,
// never fitted, and does not stop the rest.
func RunScan(gh fleet.GH, repos []fleet.Repo, home string, cfg fleet.Config, corpus Corpus, scoped []string) Scan {
	s := Scan{Findings: []Finding{}, Unknown: []string{}, ToFire: []Fire{}, Actions: []string{}, Fitted: []string{}, Dormant: []string{}, OutOfScope: []string{}}
	unknown := func(r fleet.Repo, msg string, err error) {
		s.Unknown = append(s.Unknown, r.FullName+" — "+msg)
		if fleet.IsGrant(err) {
			s.Grant = true
		}
	}
	for _, r := range repos {
		full := r.Lower()
		switch {
		case full == strings.ToLower(home):
			continue
		case scoped != nil && !contains(scoped, full):
			continue
		case cfg.Excluded(full):
			s.OutOfScope = append(s.OutOfScope, r.FullName+" (ignored — config.exclude)")
			continue
		case r.Archived:
			s.OutOfScope = append(s.OutOfScope, r.FullName+" (archived)")
			continue
		case r.Fork:
			s.OutOfScope = append(s.OutOfScope, r.FullName+" (fork)")
			continue
		}
		m, err := fleet.ReadMember(gh, r.FullName, r.Branch())
		if err != nil {
			unknown(r, err.Error(), err)
			continue
		}
		if !m.Covered() {
			s.OutOfScope = append(s.OutOfScope, r.FullName+" (uncovered — the census's question)")
			continue
		}
		if m.Dormant {
			s.Dormant = append(s.Dormant, full)
			continue
		}
		tree, err := FetchTree(gh, r.FullName, r.Branch())
		if err != nil {
			unknown(r, err.Error(), err)
			continue
		}
		packs := corpus.For(m)
		declared := DeclaredIDs(m)
		var canonical []string
		for _, id := range declared {
			canonical = append(canonical, DeclaredID(id))
		}
		if m.Shape == fleet.ShapeCn {
			canonical = declared
		}
		result := UndeclaredFits(packs, canonical, RemoteEvaluator(gh, r.FullName, r.Branch(), tree, DefaultReadBudget))
		conv, err := ConvergeSuspected(gh, r.FullName, result.Fits, SuspectedBody(result.Fits, result.Undecided, m.SettingsPath(), packs))
		if err == nil {
			if conv.Action != nil {
				s.Actions = append(s.Actions, *conv.Action)
			}
			var open []fleet.Issue
			open, _, err = fleet.TitledIssues(gh, r.FullName, IsWorkListTitle)
			if err == nil {
				var closed string
				closed, err = CloseSatisfied(gh, r.FullName, declared, open)
				if err == nil {
					if closed != "" {
						s.Actions = append(s.Actions, closed)
					}
					outstanding := false
					for _, i := range open {
						if i.Title == RequestedTitle {
							outstanding = true
						}
					}
					if conv.OpenWork || (closed == "" && outstanding) {
						s.ToFire = append(s.ToFire, Fire{r.FullName, r.Branch()})
					}
				}
			}
		}
		if err != nil {
			unknown(r, "work-list convergence failed: "+err.Error(), err)
			continue
		}
		if len(result.Fits) > 0 {
			s.Findings = append(s.Findings, Finding{Repo: full, Fits: result})
		} else {
			s.Fitted = append(s.Fitted, full)
		}
	}
	return s
}

// FitSummary is the scan's section of the run summary; scoped is nil for
// the whole fleet. The corpus is named with its count, so a clean report
// against a shelf that quietly shrank cannot read as a clean fleet.
func FitSummary(owner, home string, packCount int, s Scan, fired, scoped []string) string {
	undecidedCount := 0
	for _, f := range s.Findings {
		undecidedCount += len(f.Undecided)
	}
	var lines []string
	add := func(cond bool, line string) {
		if cond {
			lines = append(lines, line)
		}
	}
	lines = append(lines, "# Fleet pack-fit scan — "+owner)
	add(scoped != nil, fmt.Sprintf("Scoped to **%d named repo(s)**: %s", len(scoped), strings.Join(scoped, ", ")))
	lines = append(lines,
		fmt.Sprintf("Fingerprinted against **%d pack(s)** from the shelf's signed catalog.", packCount),
		"| members with fits | fitted (nothing suspected) | schedulers fired | dormant | out of scope | unknown |",
		"| --- | --- | --- | --- | --- | --- |",
		fmt.Sprintf("| %d | %d | %d | %d | %d | %d |", len(s.Findings), len(s.Fitted), len(fired), len(s.Dormant), len(s.OutOfScope), len(s.Unknown)),
	)
	if len(s.Findings) > 0 {
		var rows []string
		for _, f := range s.Findings {
			rows = append(rows, fmt.Sprintf("- `%s` → %s", f.Repo, strings.Join(f.Fits.Fits, ", ")))
		}
		lines = append(lines, "**Undeclared fits (work list placed in each repo):**\n"+strings.Join(rows, "\n"))
	} else {
		lines = append(lines, "**Undeclared fits:** none — every member declares what its shape suspects 🎉")
	}
	add(len(s.Fitted) > 0, "**Fitted:** "+strings.Join(s.Fitted, ", "))
	add(len(s.Dormant) > 0, "**Dormant (not swept — upkeep stopped on purpose):** "+strings.Join(s.Dormant, ", "))
	add(len(s.OutOfScope) > 0, "**Out of scope:** "+strings.Join(s.OutOfScope, ", "))
	add(undecidedCount > 0, fmt.Sprintf("**Undecided fingerprints (content-reading, deferred to each member's own agent):** %d across the fleet", undecidedCount))
	add(len(s.Unknown) > 0, "**UNKNOWN (could not be swept — fix the token/scope):** "+strings.Join(s.Unknown, "; "))
	lines = append(lines, "**Not swept:** "+home+" — the enforcer itself")
	if len(s.Actions) > 0 {
		lines = append(lines, "**Issue actions:** "+strings.Join(s.Actions, "; "))
	} else {
		lines = append(lines, "**Issue actions:** none (converged)")
	}
	return strings.Join(lines, "\n")
}
