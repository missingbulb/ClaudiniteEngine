package addpacks

import (
	"errors"
	"fmt"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/fleet"
	"github.com/missingbulb/ClaudiniteEngine/shared/packindex"
)

// Refusal is a force refused before any member was touched.
type Refusal struct{ Msg string }

func (r *Refusal) Error() string { return r.Msg }

// IsRefusal reports whether err is a refused force.
func IsRefusal(err error) bool {
	var r *Refusal
	return errors.As(err, &r)
}

// Validate refuses a force whole, before anything is read of any member:
// an ignored repo, a pack the catalog does not offer, an adoption question
// left unanswered.
func Validate(p Params, owner string, cfg fleet.Config, packs []packindex.CatalogPack) error {
	if len(p.AddPacks) == 0 {
		return nil
	}
	var ignored []string
	for _, n := range p.Repos {
		if q := Qualify(n, owner); cfg.Excluded(q) {
			ignored = append(ignored, q)
		}
	}
	if len(ignored) > 0 {
		return &Refusal{fmt.Sprintf("%s — ignored by this fleet (the %s pack entry's config.exclude), and nothing was written. Take the repo off that list to bring it back into the fleet.", strings.Join(ignored, ", "), fleet.PackID)}
	}
	if unknown := UnknownPacks(p.AddPacks, packs); len(unknown) > 0 {
		return &Refusal{fmt.Sprintf("unknown pack id(s): %s — not in the shelf's %d-pack catalog. An unknown id in a member's declaration is a BLOCKING settings error there, so nothing was written.", strings.Join(unknown, ", "), len(packs))}
	}
	if un := UnansweredQuestions(p.AddPacks, packs, p.PackAnswers); len(un) > 0 {
		var parts []string
		for _, u := range un {
			parts = append(parts, fmt.Sprintf("%s.%s (%q)", u.Pack, u.Question, u.Prompt))
		}
		return &Refusal{fmt.Sprintf("%d adoption-interview question(s) were not answered, so this run was refused entirely: %s. "+
			"Send each as `PACK_ANSWER…=<pack>.<question>=<the answer>` as a `--context` line — an answer is the owner's to give, "+
			"never one this task may infer (adopt-pack, \"when nobody is there to ask\").", len(un), strings.Join(parts, "; "))}
	}
	return nil
}

// Outcome is a run's reports and verdict.
type Outcome struct {
	Summaries []string
	// Fits is the members with fits or a placed request; Members the
	// members swept or named.
	Fits, Members int
	// Problems are the members that did not come through cleanly; a run
	// with any fails, after its reports.
	Problems []string
	Grant    bool
	Logs     []string
}

// Err is the run's failure, nil for a clean run.
func (o Outcome) Err() error {
	if len(o.Problems) == 0 {
		return nil
	}
	msg := fmt.Sprintf("%d member(s) did not come through cleanly — %s — the rest are reported above, and this run fails so the cause is escalated", len(o.Problems), strings.Join(o.Problems, "; "))
	if o.Grant {
		return &fleet.GrantError{Msg: msg}
	}
	return errors.New(msg)
}

// Run is the sweep with its parameters validated and its corpus in hand:
// the scan, the force, and the nudge to each member left with an open
// list. A refused nudge is reported and nothing more: the member adopts
// the marked issue on its own next scheduler run.
func Run(gh fleet.GH, repos []fleet.Repo, home string, cfg fleet.Config, p Params, packs []packindex.CatalogPack) (Outcome, error) {
	var o Outcome
	branches := map[string]string{}
	for _, r := range repos {
		branches[r.Lower()] = r.Branch()
	}
	var fireFailures []string
	fire := func(full, branch string) bool {
		if branch == "" {
			fireFailures = append(fireFailures, full+": not in the enumeration — cannot resolve its default branch")
			return false
		}
		d, err := fleet.FireScheduler(gh, full, branch, "")
		if err != nil {
			fireFailures = append(fireFailures, full+": "+err.Error())
			return false
		}
		if d.State != "fired" {
			fireFailures = append(fireFailures, fmt.Sprintf("%s: %s — %s", full, d.State, d.Detail))
			return false
		}
		return true
	}
	var scoped []string
	if p.Repos != nil {
		for _, n := range p.Repos {
			scoped = append(scoped, Qualify(n, cfg.Owner))
		}
	}
	if p.Scan {
		o.Logs = append(o.Logs, "scanning the fleet for packs a member's shape suspects but its declaration does not carry")
		s := RunScan(gh, repos, home, cfg, packs, scoped)
		var fired []string
		for _, t := range s.ToFire {
			if fire(t.FullName, t.DefaultBranch) {
				fired = append(fired, t.FullName)
			}
		}
		o.Summaries = append(o.Summaries, FitSummary(cfg.Owner, home, len(packs), s, fired, scoped))
		if len(fired) > 0 {
			o.Logs = append(o.Logs, fmt.Sprintf("fired %d member scheduler(s): %s", len(fired), strings.Join(fired, ", ")))
		}
		o.Fits += len(s.Findings)
		o.Members += len(s.Findings) + len(s.Fitted) + len(s.Unknown)
		for _, u := range s.Unknown {
			o.Problems = append(o.Problems, "unswept: "+u)
		}
		o.Grant = o.Grant || s.Grant
	}
	if len(p.AddPacks) > 0 {
		o.Logs = append(o.Logs, fmt.Sprintf("requesting %s in %d named repo(s)", strings.Join(p.AddPacks, ", "), len(p.Repos)))
		res, err := ResolveTargets(gh, p.Repos, cfg.Owner, p.AddPacks, branches)
		if err != nil {
			return o, err
		}
		var actions, fired []string
		for _, t := range res.Targets {
			body := RequestedBody(Request{AddPacks: t.Missing, PackConfig: p.PackConfig, PackAnswers: p.PackAnswers, Packs: packs, Enforcer: home, Settings: t.Settings})
			_, a, err := ConvergeRequested(gh, t.FullName, body)
			if err != nil {
				return o, err
			}
			if a != "" {
				actions = append(actions, fmt.Sprintf("%s (%s)", a, t.FullName))
			}
			if fire(t.FullName, branches[t.FullName]) {
				fired = append(fired, t.FullName)
			}
		}
		o.Summaries = append(o.Summaries, ForceSummary(cfg.Owner, p.AddPacks, res.Targets, res.AlreadyDeclared, actions, fired))
		o.Fits += len(res.Targets)
		o.Members += len(res.Targets) + len(res.AlreadyDeclared)
	}
	if len(fireFailures) > 0 {
		o.Logs = append(o.Logs, fmt.Sprintf("%d member(s) did not take the nudge dispatch — they adopt on their own next scheduler run: %s", len(fireFailures), strings.Join(fireFailures, "; ")))
	}
	return o, nil
}
