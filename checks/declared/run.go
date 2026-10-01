package declared

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
)

// GraceDays is how long a new blocking check only advises: its first
// findings are usually a backlog, not a regression.
const GraceDays = 14

// Builtin names a check the engine carries itself.
type Builtin struct {
	ID     string
	Pack   string
	OnFail string
	Tags   []string
}

// Builtins are the engine's own world checks beside the declared ones:
// declared-check-spec-keys always, and barrier, the basics pack's coded
// check, mirrored while that pack's code is unported, when basics is
// declared.
var builtinSpecKeys = Builtin{ID: "declared-check-spec-keys", OnFail: "advise", Tags: []string{"world", "builtin"}}

// builtinBarrier stands in for basics' coded barrier check, and goes away
// when basics' coded checks port to Go and its own check runs instead.
var builtinBarrier = Builtin{ID: "barrier", Pack: "basics", OnFail: "block", Tags: []string{"world", "builtin", "basics"}}

// Set is what a repo declares: its declared checks, the built-ins that
// apply, the configuration, and the faults that kept a declaration from
// loading.
type Set struct {
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
}

// LoadSet reads the repo's settings and the declared checks of its active
// packs (canon, local, and temp packs present).
func LoadSet(repo, engine string) (*Set, error) {
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
	for _, p := range set.Packs {
		cs, err := Load(repo, p.Rel, p.ID)
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
		if p.ID == "basics" && p.Kind == packset.Canon {
			s.Builtins = append(s.Builtins, builtinBarrier)
		}
	}
	s.Builtins = append(s.Builtins, builtinSpecKeys)
	sort.SliceStable(s.Checks, func(i, k int) bool { return s.Checks[i].ID < s.Checks[k].ID })
	return s, nil
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
// include all of Tags, from Pack when set.
type Selection struct {
	Tags []string
	Pack string
}

func (sel Selection) takes(tags []string, pack string) bool {
	return hasAll(tags, sel.Tags) && (sel.Pack == "" || sel.Pack == pack)
}

// Run runs the selected declared and built-in checks. A check reading the
// session (action scope, or work gated on the reply class) is loaded and
// runs nothing here; stderr says so once. Findings come back with the
// grace window applied; the configuration is the caller's to apply, over
// these and the coded checks' findings together.
func (s *Set) Run(sel Selection, now time.Time, stderr io.Writer) ([]findings.Finding, int) {
	if !s.Member {
		return nil, 0
	}
	out := append([]findings.Finding{}, s.Faults...)
	var world, work []*Check
	gated := 0
	ran := 0
	for _, c := range s.Checks {
		if !sel.takes(c.Tags, c.Pack) || s.Config.Rules[c.ID] == "off" {
			continue
		}
		switch {
		case c.Scope == "action":
			gated++
		case c.Scope == "work" && has(c.Spec, "whenReplyClassIncludes"):
			gated++
		case c.Scope == "work":
			work = append(work, c)
		default:
			world = append(world, c)
		}
	}
	if gated > 0 && stderr != nil {
		fmt.Fprintf(stderr, "[cn] declared: %d check(s) read the session (action scope or a reply-class gate) and run with the guards slice, not here\n", gated)
	}
	var builtins []Builtin
	for _, b := range s.Builtins {
		if sel.takes(b.Tags, b.Pack) && s.Config.Rules[b.ID] != "off" {
			builtins = append(builtins, b)
		}
	}
	if len(world)+len(work)+len(builtins) == 0 {
		return out, 0
	}
	ctx := NewCtx(s.Repo, s.Config)
	ctx.Now = now
	all := append(append([]*Check{}, world...), work...)
	hits, errs := safeSweep(ctx, all)
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
		ran++
		out = append(out, s.runBuiltin(b, ctx)...)
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
	f := findings.Finding{ID: c.ID, Pack: c.Pack, Path: h.File, Line: h.Line, Sentence: h.What, Why: why, Fix: h.Fix}
	if onFail == "block" && c.Since != "" {
		if until, ok := graceUntil(c.Since, now); ok {
			onFail = "advise"
			f.Fix = strings.TrimSpace(f.Fix + fmt.Sprintf(" (grace: added %s, advisory until %s, blocking after)", c.Since, until))
		}
	}
	f.Class = classOf(onFail)
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

func safeSweep(ctx *Ctx, checks []*Check) (hits map[*Check][]hit, errs map[*Check]error) {
	errs = map[*Check]error{}
	if h, err := trySweep(ctx, checks); err == nil {
		return h, errs
	}
	hits = map[*Check][]hit{}
	for _, c := range checks {
		h, err := trySweep(ctx, []*Check{c})
		if err != nil {
			errs[c] = err
			continue
		}
		hits[c] = h[c]
	}
	return hits, errs
}

func trySweep(ctx *Ctx, checks []*Check) (h map[*Check][]hit, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = recovered(r)
		}
	}()
	return sweep(ctx, checks), nil
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
			switch cfg.Rules[rule] {
			case "off":
				continue
			case "block":
				f.Class = findings.Coded
			case "advise":
				f.Class = findings.Advisory
			}
			if a, ok := acceptance(cfg.Accept, rule, f.Path); ok {
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

func acceptance(accept []Acceptance, rule, path string) (Acceptance, bool) {
	for _, a := range accept {
		if a.Rule != rule {
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
}

// List names every declared and built-in check of the set.
func (s *Set) List() []Listed {
	var out []Listed
	for _, c := range s.Checks {
		out = append(out, Listed{ID: c.ID, Pack: c.Pack, Kind: "declared", Tags: c.Tags, OnFail: c.OnFail})
	}
	if s.Member {
		for _, b := range s.Builtins {
			out = append(out, Listed{ID: b.ID, Pack: b.Pack, Kind: "builtin", Tags: b.Tags, OnFail: b.OnFail})
		}
	}
	return out
}
