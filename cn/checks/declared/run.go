package declared

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/provenance"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/skilltriggers"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/transcript"
)

// GraceDays is how long a new blocking check only advises: its first
// findings are usually a backlog, not a regression.
const GraceDays = 14

// Builtin is a check the engine carries itself. Pack is the pack that
// owns it: it runs only where that pack is declared, as the Node pack's
// coded check rode its pack's activation; "" or EnginePack runs on every
// member. Its findings take the grace window from Since and the member's
// checks configuration like a declared check's.
type Builtin struct {
	ID     string
	Pack   string
	OnFail string
	Tags   []string
	// Since is the date the check was added, YYYY-MM-DD, for the grace
	// window; Why is why a finding matters; Doc names the page that says
	// more.
	Since, Why, Doc string
	// Run reports the check's findings over the run's walk and session
	// (nil for none). A built-in with no Run is answered elsewhere: an
	// action built-in, by the guard.
	Run func(*Ctx, *transcript.Session) []findings.Finding
}

// EnginePack is the pack token of the engine's own built-ins that belong
// to no pack: they run on every member.
const EnginePack = "cn"

// Finding is one finding of the check, classed by its OnFail.
func (b Builtin) Finding(path string, line int, what, fix string) findings.Finding {
	return findings.Finding{Class: classOf(b.OnFail), ID: b.ID, Pack: b.Pack, Path: path, Line: line, Sentence: what, Why: b.Why, Fix: fix}
}

// Advice is one advisory finding of the check, whatever its OnFail.
func (b Builtin) Advice(path string, line int, what, fix string) findings.Finding {
	f := b.Finding(path, line, what, fix)
	f.Class = findings.Advisory
	return f
}

// active reports whether the built-in runs for a repo whose loaded packs
// carry these ids.
func (b Builtin) active(packs map[string]bool) bool {
	return b.Pack == "" || b.Pack == EnginePack || packs[b.Pack]
}

// builtinSpecKeys checks every declaration's keys, on every member.
var builtinSpecKeys = Builtin{ID: "declared-check-spec-keys", OnFail: "advise", Tags: []string{"world", "builtin"}}

// builtinBarrier stands in for basics' coded barrier check, and goes away
// when basics' coded checks port to Go and its own check runs instead.
var builtinBarrier = Builtin{ID: "barrier", Pack: "basics", OnFail: "block", Tags: []string{"world", "builtin", "basics"}}

// The two read their own ids into their findings, so their Run is set
// once the vars exist.
func init() {
	builtinSpecKeys.Run = func(ctx *Ctx, _ *transcript.Session) []findings.Finding { return specKeyFindings(ctx) }
	builtinBarrier.Run = func(ctx *Ctx, _ *transcript.Session) []findings.Finding { return barrierFindings(ctx) }
	// Each names the element its Node rule was in the shelf: the two basics
	// checks basics', the forced-skill check claudinite-lifecycle's.
	provenance.RegisterEngineCheck("basics", builtinSpecKeys.ID)
	provenance.RegisterEngineCheck("basics", builtinBarrier.ID)
	provenance.RegisterEngineCheck("claudinite-lifecycle", BuiltinSkillLoaded)
}

// Set is what a repo declares: its declared checks, the built-ins that
// apply, the configuration, and the faults that kept a declaration from
// loading.
type Set struct {
	// ctx is the run's one walk of the tree, shared by the declared,
	// built-in and coded checks.
	ctx      *Ctx
	Repo     string
	Checks   []*Check
	Builtins []Builtin
	Config   Config
	// Faults are declarations that failed to load; each is a checks-run
	// break, never fewer checks running silently.
	Faults []findings.Finding
	// LoadErrors are the same faults, by declaration.
	LoadErrors []*LoadError
	// Member is false for a repo with no settings file, which runs
	// nothing.
	Member bool
	// Triggers are the active packs' skills' force-load declarations.
	Triggers []skilltriggers.Trigger
	// Packs are the active packs, in the pack set's order.
	Packs []packset.Pack
	// Skipped are the checks the last Run did not run because a git
	// fault spent the tree they read, as <pack>/<id> (the id alone for
	// an engine-wide built-in).
	Skipped []string
}

// LoadSet reads the repo's settings and the declared checks of its active
// packs (canon, local, and temp packs present), and takes the engine's
// own built-ins and extra, each where its pack is active.
func LoadSet(repo, engine string, extra ...Builtin) (*Set, error) {
	s := &Set{Repo: repo, Config: Config{Rules: map[string]string{}, PackConfig: map[string]map[string]any{}}}
	path, format, err := settings.Find(repo)
	if err != nil {
		return s, nil
	}
	s.Member = true
	s.Config.SettingsPath = settings.RelPath(format)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	parsed, err := settings.ParseFile(raw, format)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", s.Config.SettingsPath, err)
	}
	rules, accept, conflicts := parsed.Effective()
	s.Config.Rules = rules
	for _, a := range accept {
		s.Config.Accept = append(s.Config.Accept, Acceptance{Rule: a.Rule, Path: a.Path, Reason: a.Reason, Pack: a.Pack})
	}
	s.Config.Errors = conflicts
	for _, e := range parsed.Packs.Entries {
		if e.Config != nil {
			s.Config.PackConfig[e.ID] = e.Config
		}
	}
	set, err := packset.Load(repo, engine, true)
	if err != nil {
		return nil, err
	}
	s.Packs = set.Packs
	s.Config.Packs = set.Packs
	s.Triggers, _ = skilltriggers.FromPacks(set.Packs)
	active := map[string]bool{}
	for _, p := range set.Packs {
		active[p.ID] = true
		cs, err := Load(repo, p.Rel, p.ID, p.Kind != packset.Canon)
		if err != nil {
			var le *LoadError
			if errors.As(err, &le) {
				s.LoadErrors = append(s.LoadErrors, le)
			}
			s.Faults = append(s.Faults, findings.Finding{Class: findings.Break, ID: "checks-run", Path: p.Rel,
				Sentence: fmt.Sprintf("the declared checks of pack %s failed to load: %v", p.ID, err)})
			continue
		}
		s.Checks = append(s.Checks, cs...)
	}
	for _, b := range append([]Builtin{builtinBarrier, builtinSpecKeys, builtinSkillLoaded, builtinRemoteDelete}, extra...) {
		if b.active(active) {
			s.Builtins = append(s.Builtins, b)
		}
	}
	sort.SliceStable(s.Checks, func(i, k int) bool { return s.Checks[i].ID < s.Checks[k].ID })
	return s, nil
}

// Context is the run's walk of the tree and its change, made once: the
// declared and built-in checks read it, and the coded checks' SDK calls
// are answered from it.
func (s *Set) Context(now time.Time) *Ctx {
	if s.ctx == nil {
		s.ctx = NewCtx(s.Repo, s.Config)
		s.ctx.Now = now
		s.ctx.Triggers = s.Triggers
	}
	return s.ctx
}

func hasAll(tags, want []string) bool {
	for _, t := range want {
		if !contains(tags, t) {
			return false
		}
	}
	return true
}

// IDs are the ids of the set's declared and built-in checks.
func (s *Set) IDs() []string {
	var out []string
	for _, c := range s.Checks {
		out = append(out, c.ID)
	}
	for _, b := range s.Builtins {
		out = append(out, b.ID)
	}
	return out
}

// Selection is which checks one run takes: every check whose tags
// include all of Tags, from Pack when set. Session is the session's
// transcript, nil for none (CI, cn check world).
type Selection struct {
	Tags    []string
	Pack    string
	Session *transcript.Session
}

func (sel Selection) takes(tags []string, pack string) bool {
	return hasAll(tags, sel.Tags) && (sel.Pack == "" || sel.Pack == pack)
}

// Run runs the selected declared and built-in checks. An action check
// judges the session's recorded calls, every finding advisory; a work
// check gated on the reply class asserts nothing unless the session's
// replies declared one of its classes; with no transcript both assert
// nothing, which stderr says once. Findings come back with the grace
// window applied; the configuration is the caller's to apply, over these
// and the coded checks' findings together.
func (s *Set) Run(sel Selection, now time.Time, stderr io.Writer) ([]findings.Finding, int) {
	if !s.Member {
		return nil, 0
	}
	out := append([]findings.Finding{}, s.Faults...)
	var world, work, action []*Check
	unread := 0
	ran := 0
	for _, c := range s.Checks {
		if !sel.takes(c.Tags, c.Pack) || s.Config.Rule(c.Pack, c.ID) == "off" {
			continue
		}
		if readsSession(c) && !sel.Session.Present() {
			unread++
			continue
		}
		switch c.Scope {
		case "action":
			action = append(action, c)
		case "work":
			if !replyGateOpen(c, sel.Session) {
				ran++
				continue
			}
			work = append(work, c)
		default:
			world = append(world, c)
		}
	}
	if unread > 0 && stderr != nil {
		fmt.Fprintf(stderr, "[cn] declared: %d check(s) read the session transcript (action scope or a reply-class gate) and assert nothing without one\n", unread)
	}
	for _, c := range action {
		ran++
		hs, err := ActionFindings(c, sel.Session.Calls())
		if err != nil {
			out = append(out, findings.Finding{Class: findings.Break, ID: "checks-run", Path: c.File,
				Sentence: fmt.Sprintf("the declared check %s/%s could not run: %v", c.Pack, c.ID, err)})
			continue
		}
		for _, h := range hs {
			out = append(out, s.actionFinding(c, h))
		}
	}
	var builtins []Builtin
	for _, b := range s.Builtins {
		if !sel.takes(b.Tags, b.Pack) || s.Config.Rule(b.Pack, b.ID) == "off" || contains(b.Tags, "action") || b.Run == nil {
			continue
		}
		builtins = append(builtins, b)
	}
	if len(world)+len(work)+len(builtins) == 0 {
		return out, ran
	}
	ctx := s.Context(now)
	all := append(append([]*Check{}, world...), work...)
	defer func() {
		for _, c := range all {
			bind(c.Spec, nil)
		}
	}()
	s.Skipped = nil
	skip := func(pack, id string) {
		if pack != "" {
			id = pack + "/" + id
		}
		s.Skipped = append(s.Skipped, id)
	}
	var hits map[*Check][]hit
	var errs map[*Check]error
	if ctx.Spent() == nil {
		ctx.Files()
	}
	if ctx.Spent() == nil {
		hits, errs = sweep(ctx, all)
	}
	// A fault during the sweep may have emptied any check's read, so the
	// whole sweep is skipped rather than trusted in part.
	if ctx.Spent() != nil {
		for _, c := range all {
			skip(c.Pack, c.ID)
		}
		all = nil
	}
	for _, c := range all {
		ran++
		if e, ok := errs[c]; ok {
			out = append(out, findings.Finding{Class: findings.Break, ID: "checks-run", Path: c.File,
				Sentence: fmt.Sprintf("the declared check %s/%s could not run: %v", c.Pack, c.ID, e)})
			continue
		}
		hs := hits[c]
		if c.Scope == "work" {
			w, err := safeWork(c, ctx)
			if err != nil {
				out = append(out, findings.Finding{Class: findings.Break, ID: "checks-run", Path: c.File,
					Sentence: fmt.Sprintf("the declared check %s/%s could not run: %v", c.Pack, c.ID, err)})
				continue
			}
			hs = append(hs, w...)
		}
		for _, h := range hs {
			out = append(out, toFinding(c, h, now))
		}
	}
	for _, b := range builtins {
		if ctx.Spent() != nil {
			skip(b.Pack, b.ID)
			continue
		}
		fs := s.runBuiltin(b, ctx, sel.Session)
		if ctx.Spent() != nil {
			skip(b.Pack, b.ID)
			continue
		}
		ran++
		for _, f := range fs {
			out = append(out, Grace(f, b.Since, now))
		}
	}
	if err := ctx.Spent(); err != nil {
		ctx.GitFaults()
		sentence := "the checks read the repository through git, and " + err.Error()
		if n := len(s.Skipped); n > 0 {
			sentence += fmt.Sprintf("; %d check(s) did not run (cn check -v names them)", n)
		}
		out = append(out, findings.Finding{Class: findings.Break, ID: "checks-run", Path: ".", Sentence: sentence})
	}
	return out, ran
}

func toFinding(c *Check, h hit, now time.Time) findings.Finding {
	onFail := c.OnFail
	if h.Block {
		onFail = "block"
	}
	why := h.Why
	if why == "" {
		why = c.Why
	}
	f := findings.Finding{ID: c.ID, Pack: c.Pack, Path: h.File, Line: h.Line, Sentence: h.What, Why: why, Fix: h.Fix, Class: classOf(onFail)}
	return Grace(f, c.Since, now)
}

// Grace demotes a blocking finding of a check added on since to an
// advisory while now falls inside the check's grace window, naming in its
// fix the day it starts biting. It runs before the member's overrides, so
// a rule set to block bites from its first day.
func Grace(f findings.Finding, since string, now time.Time) findings.Finding {
	if f.Class != findings.Coded || since == "" {
		return f
	}
	if until, ok := graceUntil(since, now); ok {
		f.Class = findings.Advisory
		f.Fix = strings.TrimSpace(f.Fix + fmt.Sprintf(" (grace: added %s, advisory until %s, blocking after)", since, until))
	}
	return f
}

func classOf(onFail string) findings.Class {
	if onFail == "block" {
		return findings.Coded
	}
	return findings.Advisory
}

// graceUntil is the day a check added on since starts blocking, when now
// falls inside its window; a since in the future grants nothing.
func graceUntil(since string, now time.Time) (string, bool) {
	start, err := time.Parse("2006-01-02", since)
	if err != nil {
		return "", false
	}
	until := start.Add(GraceDays * 24 * time.Hour)
	if now.Before(start) || !now.Before(until) {
		return "", false
	}
	return until.Format("2006-01-02"), true
}

func safeWork(c *Check, ctx *Ctx) (h []hit, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = recovered(r)
		}
	}()
	return workFindings(c, ctx), nil
}

func recovered(r any) error {
	if t, ok := r.(timeoutError); ok {
		return t
	}
	if e, ok := r.(error); ok {
		return e
	}
	return errors.New(fmt.Sprint(r))
}

// RuleID is a finding's rule as the configuration names it: the check's
// id without its pack.
func RuleID(f findings.Finding) string {
	if i := strings.LastIndex(f.ID, "/"); i >= 0 {
		return f.ID[i+1:]
	}
	return f.ID
}

// Names are the two spellings an override or an acceptance may name a
// check by: <pack>/<id> first, then the bare id.
func Names(pack, id string) []string {
	if pack == "" {
		return []string{id}
	}
	return []string{pack + "/" + id, id}
}

// Rule is the override the member set for a check, by either spelling,
// the pack-qualified one first; "" when none.
func (c Config) Rule(pack, id string) string {
	for _, n := range Names(pack, id) {
		if v, ok := c.Rules[n]; ok {
			return v
		}
	}
	return ""
}

// ApplyConfig applies the member's overrides, then its acceptances, to a
// run's findings, as the Node engine's applyConfig does: an override
// changes how a finding fails or, set to off, drops it; an acceptance with
// a reason drops it, and one with none is itself a blocking finding on the
// settings file.
// Findings come back blocking first.
func ApplyConfig(fs []findings.Finding, cfg Config) []findings.Finding {
	var out []findings.Finding
	for _, f := range fs {
		if f.Class == findings.Coded || f.Class == findings.Advisory {
			rule := RuleID(f)
			pack := f.Pack
			if pack == "" {
				if i := strings.LastIndex(f.ID, "/"); i >= 0 {
					pack = f.ID[:i]
				}
			}
			switch cfg.Rule(pack, rule) {
			case "off":
				continue
			case "block":
				f.Class = findings.Coded
			case "advise":
				f.Class = findings.Advisory
			}
			if a, ok := acceptance(cfg.Accept, Names(pack, rule), f.Path); ok {
				if strings.TrimSpace(a.Reason) != "" {
					continue
				}
				on, pack := "", ""
				if a.Path != "" {
					on = " on " + a.Path
				}
				if a.Pack != "" {
					pack = fmt.Sprintf(" (on the %q pack entry)", a.Pack)
				}
				out = append(out, findings.Finding{Class: findings.Coded, ID: "config", Path: cfg.SettingsPath,
					Sentence: fmt.Sprintf("acceptance for %s%s%s has no reason", rule, on, pack),
					Why:      "the reason string is what makes an accepted violation reviewable",
					Fix:      `add a non-empty "reason" to the acceptance entry`})
			}
		}
		out = append(out, f)
	}
	for _, e := range cfg.Errors {
		out = append(out, findings.Finding{Class: findings.Coded, ID: "config", Path: cfg.SettingsPath, Sentence: e,
			Why: "the settings file is what executes — a bad key, value, or pack name silently changes what runs",
			Fix: "make the overrides agree, or keep the rule on one of them"})
	}
	sort.SliceStable(out, func(i, k int) bool { return blocks(out[i]) && !blocks(out[k]) })
	return out
}

func blocks(f findings.Finding) bool { return f.Class == findings.Coded || f.Class == findings.Break }

func acceptance(accept []Acceptance, names []string, path string) (Acceptance, bool) {
	for _, a := range accept {
		if !contains(names, a.Rule) {
			continue
		}
		if a.Path == "" || a.Path == path || (strings.HasSuffix(a.Path, "/") && strings.HasPrefix(path, a.Path)) {
			return a, true
		}
	}
	return Acceptance{}, false
}

// Summary is the line a run with findings ends on.
func Summary(fs []findings.Finding, scope string) string {
	if len(fs) == 0 {
		return ""
	}
	b := 0
	for _, f := range fs {
		if blocks(f) {
			b++
		}
	}
	return fmt.Sprintf("%d blocking, %d advisory (%s scope)", b, len(fs)-b, scope)
}

// Listed is one check as cn check list prints it.
type Listed struct {
	ID     string
	Pack   string
	Kind   string
	Tags   []string
	OnFail string
	Since  string
}

// List names every declared and built-in check of the set.
func (s *Set) List() []Listed {
	var out []Listed
	for _, c := range s.Checks {
		out = append(out, Listed{ID: c.ID, Pack: c.Pack, Kind: "declared", Tags: c.Tags, OnFail: c.OnFail, Since: c.Since})
	}
	if s.Member {
		for _, b := range s.Builtins {
			out = append(out, Listed{ID: b.ID, Pack: b.Pack, Kind: "builtin", Tags: b.Tags, OnFail: b.OnFail, Since: b.Since})
		}
	}
	return out
}
