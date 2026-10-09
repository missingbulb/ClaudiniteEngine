// Package seeds is the fleet-pack-seeds sweep: the declarations the
// manager's packSeeds ask every member to carry, written into each cn
// member that lacks one, and the pack sources that make every member read
// new pack versions from the manager. It is the one sweep that writes into
// a member's tree, one sha-guarded Contents PUT of its settings file,
// spliced in the member's own format so nothing outside the packs block
// moves. It names no pack: the seeds are the fleet's own config. A seed
// and the sources are floors, never overrides: an entry, a config or
// sources the member already chose stay.
//
// Before it points any member at the manager, the sweep brings the
// manager's mirror of the shelf up to date (fleet/mirror) over every pack
// a member declares or is seeded; a mirror that fails points no member.
package seeds

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/fleet"
	"github.com/missingbulb/ClaudiniteEngine/cn/fleet/mirror"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/version"
)

// SourcesSince is the first engine that reads packs.sources: the first
// build of the day after the key landed, the earliest whole day every
// release carries it. An older engine refuses a settings file carrying
// the key, so a member pinned below it is not pointed at its manager until
// its own update moves the pin.
const SourcesSince = "1.61006.1"

// MirrorFunc brings the manager's mirror level with the shelf for the
// packs the fleet carries.
type MirrorFunc func(declared []string) (mirror.Result, error)

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

// CommitMessage is the seed write's commit message: the seeded ids, and
// the manager when the write also points the member at it.
func CommitMessage(ids []string, manager string) string {
	var subject string
	switch {
	case len(ids) == 0:
		subject = "Read new pack versions from this fleet's manager, " + manager
	case len(ids) == 1:
		subject = fmt.Sprintf("Declare the pack this fleet runs everywhere (%s)", ids[0])
	default:
		subject = fmt.Sprintf("Declare the packs this fleet runs everywhere (%s)", strings.Join(ids, ", "))
	}
	lines := []string{subject, ""}
	if len(ids) > 0 {
		lines = append(lines,
			"These carry a parameter no repo can derive on its own — it is a fact about the",
			"fleet, held by the fleet's own repo. Seeded, never overridden: a declaration or a",
			"config this repo already had is left exactly as it was.",
			"")
	}
	if manager != "" {
		lines = append(lines,
			"packs.sources now names "+manager+", whose vendored branch mirrors the shelf's signed",
			"packs: this repo's own update reads new versions there and verifies them as before.",
			"")
	}
	lines = append(lines, "Written by the fleet's fleet-pack-seeds sweep.")
	return strings.Join(lines, "\n")
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

// Report is the sweep's full roster: every repo under exactly one state
// for the seeds, and each cn member's pack sources.
type Report struct {
	Owner      string
	Home       string
	Seeds      []string
	Written    []string
	Already    []string
	Waiting    []string
	Node       []string
	Dormant    []string
	OutOfScope []string
	Unknown    []string
	Grant      bool
	// Mirror is the mirror's one line; the sources lists are the members
	// pointed at the manager this run, those already naming sources, and
	// those whose pin predates the key.
	Mirror         string
	SourcesWritten []string
	SourcesAlready []string
	SourcesWaiting []string
	// Members is how many cn members were read; Writes how many of them
	// this run wrote.
	Members, Writes int
}

// read is one in-scope cn member as read.
type read struct {
	repo fleet.Repo
	m    fleet.Member
}

// Sweep reads every in-scope member, the manager among them, mirrors the
// shelf for the packs they carry and the seeds, then converges each one:
// what it hands the fleet it runs too, except the sources, since the
// manager itself reads the shelf.
func Sweep(gh fleet.GH, repos []fleet.Repo, home string, cfg fleet.Config, mirrorShelf MirrorFunc) Report {
	r := Report{Owner: cfg.Owner, Home: home}
	for _, s := range cfg.PackSeeds {
		r.Seeds = append(r.Seeds, s.ID)
	}
	var members []read
	carried := map[string]bool{}
	for _, s := range cfg.PackSeeds {
		carried[s.ID] = true
	}
	for _, repo := range repos {
		full := repo.Lower()
		switch fleet.Scope(repo, home, cfg) {
		case fleet.ScopeIn, fleet.ScopeHome:
		default:
			r.OutOfScope = append(r.OutOfScope, full+" — archived, a fork, or excluded")
			continue
		}
		m, err := fleet.ReadMember(gh, repo.FullName, repo.Branch())
		switch {
		case err != nil:
			r.Unknown = append(r.Unknown, repo.FullName+" — "+err.Error())
			r.Grant = r.Grant || fleet.IsGrant(err)
			continue
		case !m.Covered():
			r.OutOfScope = append(r.OutOfScope, full+" — uncovered (the census owns it)")
			continue
		case m.Dormant:
			r.Dormant = append(r.Dormant, full)
			continue
		case m.Shape == fleet.ShapeNode:
			r.Node = append(r.Node, full)
			continue
		}
		for _, id := range m.Packs.Declared {
			carried[id] = true
		}
		members = append(members, read{repo, m})
	}
	r.Members = len(members)
	ids := make([]string, 0, len(carried))
	for id := range carried {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	mirrored := false
	if res, err := mirrorShelf(ids); err != nil {
		r.Mirror = "Mirror: FAILED — " + err.Error() + "; no member was pointed at " + home
		r.Unknown = append(r.Unknown, home+" (its mirror) — "+err.Error())
		r.Grant = r.Grant || fleet.IsGrant(err)
	} else {
		r.Mirror, mirrored = res.Summary(home), res.Remaining == 0
	}
	for _, rd := range members {
		if err := member(gh, rd, home, cfg.PackSeeds, mirrored, &r); err != nil {
			r.Unknown = append(r.Unknown, rd.repo.FullName+" — "+err.Error())
			r.Grant = r.Grant || fleet.IsGrant(err)
		}
	}
	return r
}

// pointable says whether a member is pointed at the manager this run, and
// why not when it is not.
func pointable(m fleet.Member, home string) (bool, string) {
	switch {
	case strings.EqualFold(m.Repo, home):
		return false, ""
	case m.Packs.Sources != nil:
		return false, "already"
	case m.PinErr != "":
		return false, "its pin does not read (" + m.PinErr + ")"
	}
	if c, err := version.Compare(m.Pin.Version, SourcesSince); err != nil || c < 0 {
		return false, "pinned to " + m.Pin.Version + ", older than " + SourcesSince + ", the first engine that reads packs.sources"
	}
	return true, ""
}

func member(gh fleet.GH, rd read, home string, seeds []fleet.Seed, mirrored bool, r *Report) error {
	repo, m := rd.repo, rd.m
	full := repo.Lower()
	var toWrite []fleet.Seed
	var waiting []string
	for _, s := range seeds {
		e, declared := entryOf(m.Packs, s.ID)
		vendored := false
		if !declared || e.Config == nil && s.Config != nil {
			var err error
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
	if len(seeds) > 0 {
		if len(waiting) > 0 {
			r.Waiting = append(r.Waiting, full+" — "+strings.Join(waiting, ", "))
		} else if len(toWrite) == 0 {
			r.Already = append(r.Already, full)
		}
	}
	point, why := pointable(m, home)
	switch {
	case why == "already":
		r.SourcesAlready = append(r.SourcesAlready, full+" — "+strings.Join(m.Packs.Sources, ", "))
	case why != "":
		r.SourcesWaiting = append(r.SourcesWaiting, full+" — "+why)
	}
	point = point && mirrored
	if len(toWrite) == 0 && !point {
		return nil
	}
	text, err := Splice(m.File.Text, m.Format, toWrite)
	if err != nil {
		return fmt.Errorf("could not splice %s: %v", m.SettingsPath(), err)
	}
	manager := ""
	if point {
		raw, err := settings.SetSources([]byte(text), m.Format, []string{home})
		if err != nil {
			return fmt.Errorf("could not set the pack sources in %s: %v", m.SettingsPath(), err)
		}
		text, manager = string(raw), home
	}
	ids := make([]string, len(toWrite))
	for i, s := range toWrite {
		ids[i] = s.ID
	}
	if err := fleet.PutFile(gh, repo.FullName, m.SettingsPath(), text, m.File.SHA, CommitMessage(ids, manager)); err != nil {
		return err
	}
	r.Writes++
	if len(ids) > 0 {
		r.Written = append(r.Written, full+" — "+strings.Join(ids, ", "))
	}
	if point {
		r.SourcesWritten = append(r.SourcesWritten, full)
	}
	return nil
}

// Render is the report, every repo under exactly one state.
func (r Report) Render() string {
	sources := r.renderSources()
	if len(r.Seeds) == 0 {
		return NoSeeds(r.Owner) + "\n\n" + sources
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
	return strings.Join(lines, "\n") + "\n\n" + sources
}

// renderSources is the mirror and where each member reads new packs.
func (r Report) renderSources() string {
	lines := []string{"## Pack sources — members read new pack versions from " + r.Home, "", r.Mirror}
	list := func(title string, xs []string) {
		if len(xs) > 0 {
			lines = append(lines, "", "**"+title+":**")
			for _, x := range xs {
				lines = append(lines, "- "+x)
			}
		}
	}
	list("Pointed at "+r.Home+" this run", r.SourcesWritten)
	list("Already naming their own sources (never overridden)", r.SourcesAlready)
	list("Not pointed yet", r.SourcesWaiting)
	return strings.Join(lines, "\n")
}

// NoSeeds is the report of a fleet that seeds nothing.
func NoSeeds(owner string) string {
	return fmt.Sprintf("# Fleet pack seeds — %s\n\nNo `packSeeds` in this repo's fleet block: this fleet asks its members to declare nothing in particular.", owner)
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
