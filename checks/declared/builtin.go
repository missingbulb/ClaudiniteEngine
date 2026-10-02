package declared

import (
	"fmt"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/checks/declared/refs"
	"github.com/missingbulb/ClaudiniteEngine/shared/descriptor"
	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/shared/transcript"
)

var declarationFile = mustRegex(`(^|/)declared-checks\.(json|ya?ml|toml)$`, "")

// LocalPacksPrefix is where a repo's own packs live; their declarations
// ship with the engine that reads them only when the repo is the one
// building that engine, so an unplaced key there is a fault, not skew.
const LocalPacksPrefix = ".claudinite/local/packs/"

func (s *Set) runBuiltin(b Builtin, ctx *Ctx, session *transcript.Session) []findings.Finding {
	var out []findings.Finding
	defer func() {
		if r := recover(); r != nil {
			out = []findings.Finding{{Class: findings.Break, ID: "checks-run", Path: s.Config.SettingsPath,
				Sentence: fmt.Sprintf("the built-in check %s could not run: %v", b.ID, recovered(r))}}
		}
	}()
	switch b.ID {
	case builtinSpecKeys.ID:
		out = specKeyFindings(ctx)
	case builtinBarrier.ID:
		out = barrierFindings(ctx)
	case builtinSkillLoaded.ID:
		out = s.skillLoadedFindings(ctx, session)
	}
	return out
}

func specKeyFindings(ctx *Ctx) []findings.Finding {
	var out []findings.Finding
	for _, file := range ctx.Files() {
		if !declarationFile.Test(file) {
			continue
		}
		text, ok := ctx.Read(file)
		if !ok {
			continue
		}
		decls, err := Declarations([]byte(text), descriptor.FormatOf(file))
		if err != nil {
			continue
		}
		class := findings.Advisory
		if strings.HasPrefix(file, LocalPacksPrefix) {
			class = findings.Coded
		}
		for _, d := range decls {
			id, ok := d["id"].(string)
			if !ok {
				continue
			}
			anchor := anchorLine(text, file, id)
			for _, u := range UnplacedKeys(d) {
				what := fmt.Sprintf("%q carries %q inside %q, whose keys are: %s", id, u.Key, u.Container, strings.Join(u.Allowed, ", "))
				if u.Container == "spec" {
					what = fmt.Sprintf("%q carries %q, which is not a spec key — the vocabulary here is: %s", id, u.Key, strings.Join(u.Allowed, ", "))
				}
				out = append(out, findings.Finding{Class: class, ID: builtinSpecKeys.ID, Path: file, Line: anchor, Sentence: what,
					Why: "a key the engine cannot place is dropped at load, so a typo'd key asserts nothing at all and its check reads green forever",
					Fix: "spell the key the engine has, or drop it — unless it belongs to a newer engine, which this mount will place once its version arrives"})
			}
		}
	}
	return out
}

// anchorLine is the line a declaration's id first appears on.
func anchorLine(text, file, id string) int {
	needle := `"` + id + `"`
	i := strings.Index(text, needle)
	if i < 0 && descriptor.FormatOf(file) != descriptor.JSON {
		i = strings.Index(text, id)
	}
	if i < 0 {
		return 1
	}
	return strings.Count(text[:i], "\n") + 1
}

func barrierFindings(ctx *Ctx) []findings.Finding {
	rule := refs.Rule{Why: "a declared folder barrier encodes an architectural boundary; a crossing reference erodes it silently", SettingsPath: ctx.Config.SettingsPath}
	mk := func(f refs.Finding) findings.Finding {
		class := findings.Coded
		return findings.Finding{Class: class, ID: builtinBarrier.ID, Pack: builtinBarrier.Pack, Path: f.File, Line: f.Line, Sentence: f.What, Why: f.Why, Fix: f.Fix}
	}
	cfgAny, ok := ctx.Config.PackConfig["basics"]["barriers"]
	if !ok || cfgAny == nil {
		return nil
	}
	cfg, ok := cfgAny.(map[string]any)
	if _, hasRules := cfg["rules"]; !ok || !hasRules {
		return []findings.Finding{mk(refs.SpecFinding(rule, `the barriers config must be an object with a "rules" array`,
			`declare it on the basics pack entry: config: {barriers: {rules: [{from: "...", to: "..."}]}}`))}
	}
	var out []findings.Finding
	var unknown []string
	for k := range cfg {
		if k != "rules" {
			unknown = append(unknown, `"`+k+`"`)
		}
	}
	sort.Strings(unknown)
	if len(unknown) > 0 {
		word := "property"
		if len(unknown) > 1 {
			word = "properties"
		}
		out = append(out, mk(refs.SpecFinding(rule, fmt.Sprintf(`the barriers config has unknown %s %s — exceptions live per-rule now, in each rule's "except"`, word, strings.Join(unknown, ", ")),
			`it takes only "rules"; move accept/except entries into the owning rule's "except"`)))
	}
	edges, problems := refs.Normalize(cfg["rules"])
	for _, p := range problems {
		out = append(out, mk(refs.SpecFinding(rule, p.What, p.Fix)))
	}
	fs, stale, scanErrors := ctx.refs().Findings(edges, rule)
	for _, f := range fs {
		out = append(out, mk(f))
	}
	if len(unknown) == 0 && len(problems) == 0 && !scanErrors {
		for _, f := range refs.StaleFindings(stale, rule) {
			out = append(out, mk(f))
		}
	}
	return out
}
