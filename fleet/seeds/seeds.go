// Package seeds is the fleet-pack-seeds sweep: the declarations the
// manager's packSeeds ask every member to carry, written into each cn
// member that lacks one. It is the one sweep that writes into a member's
// tree, one sha-guarded Contents PUT of its settings file, spliced in the
// member's own format so nothing outside the packs block moves. It names
// no pack: the seeds are the fleet's own config. A seed is a floor, never
// an override: an entry or a config the member already chose stays.
package seeds

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/fleet"
	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
)

// The states one member takes against one seed.
const (
	// Set is converged: declared, with its own config or with none
	// wanted.
	Set = "set"
	// NotVendored is a wait: the member's mount does not hold the pack
	// yet, and declaring a pack whose code is absent is a blocking
	// config error there.
	NotVendored = "not-vendored"
	// Writable is a seed this sweep writes.
	Writable = "writable"
	// WaitingOnMove is a Node member: never written until phase 9 moves
	// it.
	WaitingOnMove = "waiting-on-move"
)

// Verdict is one member against one seed.
type Verdict struct {
	State  string `json:"state"`
	Detail string `json:"detail"`
}

// Classify is a member against seed: whether it declares the seed's pack,
// whether that entry carries a config, and whether its mount holds the
// pack.
func Classify(declared, hasConfig bool, seed fleet.Seed, vendored bool) Verdict {
	if declared && (hasConfig || seed.Config == nil) {
		if hasConfig {
			return Verdict{Set, "already declares " + seed.ID + " with its own config"}
		}
		return Verdict{Set, "already declares " + seed.ID}
	}
	if !vendored {
		return Verdict{NotVendored, "its mount does not carry the " + seed.ID + " pack yet — waiting for the update that vendors it, rather than declaring a pack whose code is absent (a blocking config error there)"}
	}
	if declared {
		return Verdict{Writable, "declares " + seed.ID + " with no config — adding the fleet's"}
	}
	return Verdict{Writable, "does not declare " + seed.ID + " — adding it"}
}

// entryOf is the member's canon entry for id.
func entryOf(p settings.Packs, id string) (settings.PackEntry, bool) {
	for _, e := range p.Entries {
		if !e.Local && e.ID == id {
			return e, true
		}
	}
	return settings.PackEntry{}, false
}

// Splice is the member's settings file with every seed applied, in its
// own format: a missing entry declared, a config added to an entry that
// carries none, and every byte outside the packs block left as it was.
func Splice(text string, f settings.Format, seeds []fleet.Seed) (string, error) {
	raw := []byte(text)
	for _, s := range seeds {
		p, err := settings.ReadPacks(raw, f)
		if err != nil {
			return "", err
		}
		e, declared := entryOf(p, s.ID)
		if !declared {
			if raw, err = settings.AddDeclared(raw, f, s.ID); err != nil {
				return "", err
			}
		}
		if s.Config == nil || (declared && e.Config != nil) {
			continue
		}
		keys := make([]string, 0, len(s.Config))
		for k := range s.Config {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if raw, err = settings.SetEntryConfig(raw, f, s.ID, k, s.Config[k]); err != nil {
				return "", err
			}
		}
	}
	return string(raw), nil
}

// CommitMessage is the seed write's commit message.
func CommitMessage(ids []string) string {
	s := ""
	if len(ids) > 1 {
		s = "s"
	}
	return strings.Join([]string{
		fmt.Sprintf("Declare the pack%s this fleet runs everywhere (%s)", s, strings.Join(ids, ", ")),
		"",
		"These carry a parameter no repo can derive on its own — it is a fact about the",
		"fleet, held by the fleet's own repo. Seeded, never overridden: a declaration or a",
		"config this repo already had is left exactly as it was.",
		"",
		"Written by the claudinite-fleet-sheepdog pack's fleet-pack-seeds sweep.",
	}, "\n")
}

// Vendored reports whether repo's mount holds pack id's manifest.
func Vendored(gh fleet.GH, repo, id string) (bool, error) {
	for _, name := range packset.ManifestFiles() {
		ok, err := fleet.FileExists(gh, repo, packset.TreeRel(id)+"/"+name)
		if err != nil || ok {
			return ok, err
		}
	}
	return false, nil
}

// Report is the sweep's full roster: every repo under exactly one state.
type Report struct {
	Owner      string
	Seeds      []string
	Written    []string
	Already    []string
	Waiting    []string
	Node       []string
	Dormant    []string
	OutOfScope []string
	Unknown    []string
	Grant      bool
}

// Sweep converges every in-scope member against seeds, the manager among
// them: what it hands the fleet it runs too.
func Sweep(gh fleet.GH, repos []fleet.Repo, home string, cfg fleet.Config) Report {
	r := Report{Owner: cfg.Owner}
	for _, s := range cfg.PackSeeds {
		r.Seeds = append(r.Seeds, s.ID)
	}
	for _, repo := range repos {
		full := repo.Lower()
		switch fleet.Scope(repo, home, cfg) {
		case fleet.ScopeIn, fleet.ScopeHome:
		default:
			r.OutOfScope = append(r.OutOfScope, full+" — archived, a fork, or excluded")
			continue
		}
		if err := member(gh, repo, cfg.PackSeeds, &r); err != nil {
			r.Unknown = append(r.Unknown, repo.FullName+" — "+err.Error())
			r.Grant = r.Grant || fleet.IsGrant(err)
		}
	}
	return r
}

func member(gh fleet.GH, repo fleet.Repo, seeds []fleet.Seed, r *Report) error {
	full := repo.Lower()
	m, err := fleet.ReadMember(gh, repo.FullName, repo.Branch())
	switch {
	case err != nil:
		return err
	case !m.Covered():
		r.OutOfScope = append(r.OutOfScope, full+" — uncovered (the census owns it)")
		return nil
	case m.Dormant:
		r.Dormant = append(r.Dormant, full)
		return nil
	case m.Shape == fleet.ShapeNode:
		r.Node = append(r.Node, full)
		return nil
	}
	var toWrite []fleet.Seed
	var waiting []string
	for _, s := range seeds {
		e, declared := entryOf(m.Packs, s.ID)
		vendored := false
		if !declared || e.Config == nil && s.Config != nil {
			if vendored, err = Vendored(gh, repo.FullName, s.ID); err != nil {
				return err
			}
		}
		switch Classify(declared, e.Config != nil, s, vendored).State {
		case NotVendored:
			waiting = append(waiting, s.ID)
		case Writable:
			toWrite = append(toWrite, s)
		}
	}
	if len(waiting) > 0 {
		r.Waiting = append(r.Waiting, full+" — "+strings.Join(waiting, ", "))
	}
	if len(toWrite) == 0 {
		if len(waiting) == 0 {
			r.Already = append(r.Already, full)
		}
		return nil
	}
	text, err := Splice(m.File.Text, m.Format, toWrite)
	if err != nil {
		return fmt.Errorf("could not splice %s: %v", m.SettingsPath(), err)
	}
	ids := make([]string, len(toWrite))
	for i, s := range toWrite {
		ids[i] = s.ID
	}
	if err := fleet.PutFile(gh, repo.FullName, m.SettingsPath(), text, m.File.SHA, CommitMessage(ids)); err != nil {
		return err
	}
	r.Written = append(r.Written, full+" — "+strings.Join(ids, ", "))
	return nil
}

// Render is the report, every repo under exactly one state.
func (r Report) Render() string {
	if len(r.Seeds) == 0 {
		return NoSeeds(r.Owner)
	}
	lines := []string{
		fmt.Sprintf("# Fleet pack seeds — %s (seeds: %s)", r.Owner, strings.Join(r.Seeds, ", ")),
		"",
		"| already declaring | written | waiting on the mount | node (moves with phase 9) | dormant | out of scope | unknown |",
		"| --- | --- | --- | --- | --- | --- | --- |",
		fmt.Sprintf("| %d | %d | %d | %d | %d | %d | %d |", len(r.Already), len(r.Written), len(r.Waiting), len(r.Node), len(r.Dormant), len(r.OutOfScope), len(r.Unknown)),
		"",
	}
	bullets := func(xs []string) string {
		out := make([]string, len(xs))
		for i, x := range xs {
			out[i] = "- " + x
		}
		return strings.Join(out, "\n")
	}
	if len(r.Written) > 0 {
		lines = append(lines, "**Written:**\n"+bullets(r.Written))
	} else {
		lines = append(lines, "**Written:** none")
	}
	add := func(cond bool, s string) {
		if cond {
			lines = append(lines, s)
		}
	}
	add(len(r.Already) > 0, "**Already declaring every seed:** "+strings.Join(r.Already, ", "))
	add(len(r.Waiting) > 0, "**Waiting on their next update (the pack is not in their mount yet):**\n"+bullets(r.Waiting))
	add(len(r.Node) > 0, "**Waiting on the move to cn (a Node member's declaration is never written from the fleet):** "+strings.Join(r.Node, ", "))
	add(len(r.Dormant) > 0, "**Dormant (self-declared, not written to):** "+strings.Join(r.Dormant, ", "))
	add(len(r.OutOfScope) > 0, "**Out of scope:**\n"+bullets(r.OutOfScope))
	add(len(r.Unknown) > 0, "**UNKNOWN (read or write failed — fix the token/scope):** "+strings.Join(r.Unknown, "; "))
	return strings.Join(lines, "\n")
}

// NoSeeds is the report of a fleet that seeds nothing.
func NoSeeds(owner string) string {
	return fmt.Sprintf("# Fleet pack seeds — %s\n\nNo `packSeeds` on this repo's %s entry: this fleet asks its members to declare nothing in particular.", owner, fleet.PackID)
}

// Err is the sweep's failure, nil when every repo was read and written.
func (r Report) Err() error {
	if len(r.Unknown) == 0 {
		return nil
	}
	msg := fmt.Sprintf("%d repo(s) could not be read or written — this run fails so the cause is escalated rather than leaving a member quietly undeclared", len(r.Unknown))
	if r.Grant {
		return &fleet.GrantError{Msg: msg}
	}
	return errors.New(msg)
}
