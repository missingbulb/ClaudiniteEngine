package update

import (
	"fmt"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/lifecycle/rulesindex"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/flatdecl"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/version"
)

// The update's decision cores: pure functions over values, which the
// flow calls and the parity harness's update face asks one fixture at a
// time against the Node engine's packs/claudinite-lifecycle/updates/.
// Where they answer otherwise, a design record row says why: no update
// migrates member files and none runs an agent (32), a pin only moves
// forward (8), a minimum engine is <major>.<day>.<n> (12), and the pack
// PR carries the packs, the rules index and its CLAUDE.md import alone
// (39).

// ShelfPack is a pack version on offer: its id, version and minimum
// engine.
type ShelfPack struct {
	ID               string `json:"id"`
	Version          string `json:"version"`
	MinEngineVersion string `json:"minEngineVersion"`
}

// PackPlan is what an update would do to one declared pack: the version
// held, the version it ends on, why the move is blocked, and the
// migration records in between, which are always none.
type PackPlan struct {
	ID      string   `json:"id"`
	From    *string  `json:"from"`
	To      *string  `json:"to"`
	Blocked *string  `json:"blocked"`
	Records []string `json:"records"`
}

// MinEngineBlock is the sentence naming why a pack version cannot load on
// engine, or "" when it can.
func MinEngineBlock(id, ver, min, engine string) string {
	m, err := version.ParseMinEngineVersion(min)
	if err != nil {
		return fmt.Sprintf("pack %q version %s: %v, so no engine loads it", id, ver, err)
	}
	if m.Satisfies(engine) {
		return ""
	}
	return fmt.Sprintf("pack %q version %s needs engine %s; this repo runs engine %s", id, ver, min, engine)
}

// PlanPacks plans each declared canon pack the shelf offers, in declared
// order, once: local packs and packs the shelf lacks are not planned. A
// pack never moves back, and no plan carries a migration record.
func PlanPacks(shelf []ShelfPack, declared []string, installed map[string]string, engine string) []PackPlan {
	byID := map[string]ShelfPack{}
	for _, p := range shelf {
		byID[p.ID] = p
	}
	out := []PackPlan{}
	seen := map[string]bool{}
	for _, id := range declared {
		p, ok := byID[id]
		if !ok || seen[id] || strings.HasPrefix(id, settings.LocalPrefix) {
			continue
		}
		seen[id] = true
		plan := PackPlan{ID: id, Records: []string{}}
		if from := installed[id]; packVersion(from) {
			plan.From = &from
		}
		if packVersion(p.Version) {
			to := p.Version
			if plan.From != nil {
				if c, err := version.ComparePack(to, *plan.From); err == nil && c < 0 {
					to = *plan.From
				}
			}
			plan.To = &to
		}
		if b := MinEngineBlock(id, p.Version, p.MinEngineVersion, engine); b != "" {
			plan.Blocked = &b
		}
		out = append(out, plan)
	}
	return out
}

func packVersion(v string) bool {
	_, err := version.ComparePack(v, v)
	return v != "" && err == nil
}

// RecordsInGap is the migration records an engine update owes a repo:
// none, at any installed version, since no update migrates member files.
func RecordsInGap() []string { return []string{} }

// Delivery is what happens to an update's pull request, and why.
type Delivery struct {
	Action string `json:"action"`
	Label  string `json:"label,omitempty"`
	Forced bool   `json:"forced,omitempty"`
	Why    string `json:"why"`
}

// NeedsHuman is the action and label of a stop a person resolves.
const NeedsHuman = "needs-human"

// DeliveryDecision is the update PR's fate given the candidate's
// self-test and the member's delivery: a green self-test opens it to land
// (merge) or to stand for review (keep); a red one opens nothing, and no
// force overrides it.
func DeliveryDecision(selftestOK bool, delivery string) Delivery {
	if delivery == "" {
		delivery = "auto-merge"
	}
	if selftestOK {
		if delivery == "auto-merge" {
			return Delivery{Action: "merge", Why: "the converged tree passed its self-test"}
		}
		return Delivery{Action: "keep", Why: fmt.Sprintf("the converged tree passed its self-test; delivery is %q, so the PR is the owner's to merge", delivery)}
	}
	return Delivery{Action: NeedsHuman, Label: NeedsHuman, Why: "the converged tree FAILED its self-test — the machinery that runs the rules is not intact, so the PR stays open"}
}

// ApplyStage is whether an update needs an agent stage: never.
type ApplyStage struct {
	Needed bool `json:"needed"`
}

// ApplyStageFor is the agent stage an update needs: none, whatever its
// records, withheld workflows or test-visible writes; verify reports and a
// person fixes.
func ApplyStageFor() ApplyStage { return ApplyStage{} }

// Outcome is what an update flow returned, as the terminal reads it.
type Outcome struct {
	Status     string `json:"status"`
	Detail     string `json:"detail"`
	ApplyStage *struct {
		Needed bool `json:"needed"`
	} `json:"applyStage"`
	Decision *Delivery `json:"decision"`
}

// Terminal is how an update ends: merge, keep or needs-human.
type Terminal struct {
	Action string `json:"action"`
	Label  string `json:"label,omitempty"`
	Forced *bool  `json:"forced,omitempty"`
	Why    string `json:"why"`
}

func needsHuman(why string) Terminal {
	return Terminal{Action: NeedsHuman, Label: NeedsHuman, Why: why}
}

// TerminalFor is the end an outcome reaches, most specific first: a stop
// the flow reached, then an asked-for agent stage (which ends at a person,
// as no agent stage runs), then the delivery decision; an outcome that
// decided nothing never merges.
func TerminalFor(o *Outcome) Terminal {
	switch {
	case o == nil:
		return needsHuman("the update flow returned nothing to act on")
	case o.Status == NeedsHuman:
		why := o.Detail
		if why == "" {
			why = "the flow ended at a human terminal"
		}
		return needsHuman(why)
	case o.ApplyStage != nil && o.ApplyStage.Needed:
		return needsHuman("the update asks for an agent stage, and the engine runs none: verify names the break and a person fixes it")
	case o.Decision != nil && o.Decision.Action == "merge":
		forced := o.Decision.Forced
		return Terminal{Action: "merge", Forced: &forced, Why: o.Decision.Why}
	case o.Decision != nil && o.Decision.Action == "keep":
		return Terminal{Action: "keep", Why: o.Decision.Why}
	}
	return needsHuman("the flow reported success but reached no delivery decision")
}

// IsConvergeBookkeeping reports whether a pack PR may change file: the
// vendored packs, the files cn generates from them (and their old
// directory, which a pack PR empties), and CLAUDE.md for the rules index
// import. Anything else is a change a pack update never makes.
func IsConvergeBookkeeping(file string) bool {
	return strings.HasPrefix(file, packset.Dir+"/") || strings.HasPrefix(file, flatdecl.Dir+"/") || strings.HasPrefix(file, flatdecl.LegacyDir+"/") || file == rulesindex.ClaudeMD
}

// PinOnlyEdit reports whether an edit of a settings file moved the engine
// pin and nothing else; anything it cannot read is false.
func PinOnlyEdit(before, after *string, f settings.Format) bool {
	if before == nil || after == nil {
		return false
	}
	return settings.PinOnlyChange([]byte(*before), []byte(*after), f) == nil
}

// settingsFormatOf is the settings format of path, "" when it is not a
// settings file.
func settingsFormatOf(path string) settings.Format {
	for _, f := range settings.Formats {
		if path == settings.RelPath(f) {
			return f
		}
	}
	return ""
}

// TestVisible are the paths an update changed between head and working
// that a member's own tests could see, sorted: every changed path but the
// bookkeeping and a settings file whose edit only moved the pin. A nil
// working file is a deletion.
func TestVisible(head, working map[string]*string) []string {
	var out []string
	for p, after := range working {
		before, had := head[p]
		if had && (before == nil) == (after == nil) && (before == nil || *before == *after) {
			continue
		}
		if IsConvergeBookkeeping(p) {
			continue
		}
		if f := settingsFormatOf(p); f != "" && PinOnlyEdit(before, after, f) {
			continue
		}
		out = append(out, p)
	}
	sort.Strings(out)
	if out == nil {
		out = []string{}
	}
	return out
}

// EngineTitle is the engine update PR's title.
func EngineTitle(ver string) string { return "Claudinite engine " + ver }

// PacksTitle is the pack update PR's title on day: every move, from →
// to, an absent from read as none.
func PacksTitle(day int, moves []PackMove) string {
	return fmt.Sprintf("Claudinite packs %d: %s", day, describeMoves(moves, true))
}

// PackMove is one pack a pack update moves.
type PackMove struct {
	ID, From, To string
}

func describeMoves(moves []PackMove, arrow bool) string {
	var parts []string
	for _, m := range moves {
		if arrow {
			from := m.From
			if from == "" {
				from = "none"
			}
			parts = append(parts, m.ID+" "+from+"→"+m.To)
		} else {
			parts = append(parts, m.ID+" "+m.To)
		}
	}
	return strings.Join(parts, ", ")
}

// PullTitles are the titles the update opens for an engine move and a
// pack plan, which a needs-human terminal opens none of: one pull request
// per kind, the engine's first, since a pack
// update waits while an engine PR is open. An update never amends a pull
// request; a newer one supersedes it.
func PullTitles(day int, engineFrom, engineTo *string, plan []PackPlan) []string {
	out := []string{}
	if engineTo != nil && (engineFrom == nil || *engineFrom != *engineTo) {
		out = append(out, EngineTitle(*engineTo))
	}
	var moves []PackMove
	for _, p := range plan {
		if p.To == nil || (p.From != nil && *p.From == *p.To) {
			continue
		}
		m := PackMove{ID: p.ID, To: *p.To}
		if p.From != nil {
			m.From = *p.From
		}
		moves = append(moves, m)
	}
	if len(moves) > 0 {
		out = append(out, PacksTitle(day, moves))
	}
	return out
}
