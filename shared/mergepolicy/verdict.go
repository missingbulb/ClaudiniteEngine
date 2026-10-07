package mergepolicy

import (
	"errors"
	"fmt"
	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/shared/descriptor"
	"github.com/missingbulb/ClaudiniteEngine/shared/jsjson"
	"github.com/missingbulb/ClaudiniteEngine/shared/jsregex"
)

var (
	declaredKeys = []string{"name", "pathMatching", "excludePathMatching", "changeKinds", "editShape", "coversMountPolicySources"}
	changeKinds  = []string{"added", "modified", "deleted"}
	editShapes   = []string{"any", "removals-only", "comment-only"}
)

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func compileRegex(raw any, where string) (*jsregex.Regex, error) {
	body, flags, ok := jsregex.Form(jsjson.StringOf(raw))
	if raw == nil || !ok {
		return nil, fmt.Errorf("%s: %s is not a /pattern/ regex string", where, stringify(raw))
	}
	re, err := jsregex.Compile(body, flags)
	if err != nil {
		// The message is JavaScript's, which a pack author has read before.
		msg := fmt.Sprintf("Invalid regular expression: /%s/%s: %v", body, flags, err)
		return nil, errors.New(msg)
	}
	return re, nil
}

// stringify is JSON.stringify, with an absent value as "undefined".
func stringify(v any) string {
	if v == nil {
		return "null"
	}
	return jsjson.StringifyAny(v)
}

// CompileRule compiles one declared rule. Every key is validated, and
// changeKinds and editShape are required: a matcher whose reach is
// defaulted is a matcher nobody decided.
func CompileRule(spec any, where string) (Rule, error) {
	obj, ok := spec.(map[string]any)
	if !ok {
		return Rule{}, fmt.Errorf("%s: a merge rule is an object, not %s", where, stringify(spec))
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !has(declaredKeys, k) {
			return Rule{}, fmt.Errorf(`%s: "%s" is not a merge-rule key — the vocabulary is: %s`, where, k, strings.Join(declaredKeys, ", "))
		}
	}
	name, _ := obj["name"].(string)
	if !isKebab(name) {
		return Rule{}, fmt.Errorf(`%s: a merge rule needs a kebab-case "name"`, where)
	}
	rawPath, present := obj["pathMatching"]
	if !present {
		return Rule{}, fmt.Errorf("%s (%s).pathMatching: undefined is not a /pattern/ regex string", where, name)
	}
	pathRe, err := compileRegex(rawPath, fmt.Sprintf("%s (%s).pathMatching", where, name))
	if err != nil {
		return Rule{}, err
	}
	var exclude *jsregex.Regex
	if raw, present := obj["excludePathMatching"]; present {
		if exclude, err = compileRegex(raw, fmt.Sprintf("%s (%s).excludePathMatching", where, name)); err != nil {
			return Rule{}, err
		}
	}
	kindsRaw, isList := obj["changeKinds"].([]any)
	var kinds []string
	okKinds := isList && len(kindsRaw) > 0
	for _, k := range kindsRaw {
		s, isStr := k.(string)
		if !isStr || !has(changeKinds, s) {
			okKinds = false
		}
		kinds = append(kinds, s)
	}
	if !okKinds {
		return Rule{}, fmt.Errorf(`%s (%s): "changeKinds" is a non-empty subset of %s`, where, name, strings.Join(changeKinds, ", "))
	}
	shape, _ := obj["editShape"].(string)
	if !has(editShapes, shape) {
		return Rule{}, fmt.Errorf(`%s (%s): "editShape" must be one of %s — declared, never defaulted`, where, name, strings.Join(editShapes, ", "))
	}
	covers := false
	if raw, present := obj["coversMountPolicySources"]; present {
		if raw != true {
			return Rule{}, fmt.Errorf("%s (%s): \"coversMountPolicySources\" takes only `true` — omit it otherwise", where, name)
		}
		covers = true
	}
	return Rule{
		Name:        name,
		CoversMount: covers,
		AppliesTo: func(e Entry) bool {
			if !pathRe.Test(e.File) || (exclude != nil && exclude.Test(e.File)) {
				return false
			}
			if !has(kinds, e.ChangeKind()) {
				return false
			}
			switch shape {
			case "removals-only":
				return e.ChangeKind() != "added" && RemovalsOnly(e.Before, e.After)
			case "comment-only":
				return isCommentOnlyChange(e)
			}
			return true
		},
	}, nil
}

// PackRules is one pack's declared rules as read: its id, the file's
// name, and the parsed list (or the error reading it).
type PackRules struct {
	ID, File string
	Specs    any
	Err      error
}

// Declared is the rules every pack's merge-rules descriptor declares, by
// name, and the errors: a broken declaration or a name collision is an
// error, never a silent drop, so a policy naming it fails closed.
type Declared struct {
	Rules  map[string]Rule
	Errors []string
}

// ReadPackRules reads dir's merge-rules descriptor, or reports false when
// the pack declares none.
func ReadPackRules(id, dir string) (PackRules, bool) {
	file, _, err := descriptor.Find(dir, RulesFile)
	if errors.Is(err, descriptor.ErrAbsent) {
		return PackRules{}, false
	}
	pr := PackRules{ID: id, File: RulesFile + ".json"}
	if err != nil {
		pr.Err = err
		return pr, true
	}
	pr.File = file[strings.LastIndex(file, string(os.PathSeparator))+1:]
	raw, err := os.ReadFile(file)
	if err != nil {
		pr.Err = err
		return pr, true
	}
	pr.Specs, pr.Err = descriptor.ParseBytes(raw, descriptor.FormatOf(file))
	return pr, true
}

// Compile compiles each pack's rules into one flat namespace, in the order
// given.
func Compile(packs []PackRules) Declared {
	d := Declared{Rules: map[string]Rule{}}
	for _, p := range packs {
		where := p.ID + "/" + p.File
		if p.Err != nil {
			d.Errors = append(d.Errors, where+": "+p.Err.Error())
			continue
		}
		specs, ok := p.Specs.([]any)
		if !ok {
			d.Errors = append(d.Errors, where+": the file must hold an array of rule objects")
			continue
		}
		for _, spec := range specs {
			r, err := CompileRule(spec, where)
			if err == nil {
				_, builtin := Builtins[r.Name]
				_, comp := Composites[r.Name]
				_, taken := d.Rules[r.Name]
				if builtin || comp || taken {
					err = fmt.Errorf(`rule name "%s" is already taken — merge-rule names are one flat namespace`, r.Name)
				}
			}
			if err != nil {
				msg := err.Error()
				if !strings.HasPrefix(msg, where) {
					msg = where + ": " + msg
				}
				d.Errors = append(d.Errors, msg)
				continue
			}
			d.Rules[r.Name] = r
		}
	}
	return d
}

// FileVerdict is one changed file's line in a verdict.
type FileVerdict struct {
	File    string `json:"file"`
	Verdict string `json:"verdict"`
}

// Problem is one refusal; File is nil for a whole-diff refusal.
type Problem struct {
	File *string `json:"file"`
	What string  `json:"what"`
}

// Verdict is the answer over a diff.
type Verdict struct {
	Mergeable bool          `json:"mergeable"`
	Why       string        `json:"why"`
	Files     []FileVerdict `json:"files"`
	Problems  []Problem     `json:"problems"`
}

// policySources are the files no granular policy may cover except as
// comment-only edits: each task's declaration and each pack's merge rules
// in any format, the member's settings (which pin the engine that judges
// and declare the packs whose rules it reads), and the frozen Node
// engine's two policy modules where a tree still carries them.
var (
	policySources = regexp.MustCompile(`(^|/)(merge-rules\.(json|yaml|toml)|merge-policy\.mjs|workRules/automerge-policy-scope\.mjs|tasks/[^/]+/task\.(json|yaml|toml))$|^\.claudinite/settings\.(json|yaml|toml)$`)
	vendoredMount = regexp.MustCompile(`^\.claudinite/shared/`)
)

// IsPolicySource reports whether file defines policy.
func IsPolicySource(file string) bool { return policySources.MatchString(file) }

func refuse(problems []Problem, files []FileVerdict) Verdict {
	whats := make([]string, len(problems))
	for i, p := range problems {
		whats[i] = p.What
	}
	if files == nil {
		files = []FileVerdict{}
	}
	return Verdict{Why: strings.Join(whats, "; "), Files: files, Problems: problems}
}

// Judge is the whole verdict over a diff.
func Judge(policy any, entries []Entry, declared Declared) Verdict {
	norm := Normalize(policy)
	switch norm.Kind {
	case "invalid":
		return refuse([]Problem{{What: "invalid policy: " + norm.Reason}}, nil)
	case Nothing:
		return refuse([]Problem{{What: "the policy authorizes nothing to auto-merge"}}, nil)
	case Anything:
		return Verdict{Mergeable: true, Why: "the policy authorizes any diff (the repo's delivery settings still apply)", Files: []FileVerdict{}, Problems: []Problem{}}
	}
	if len(entries) == 0 {
		return refuse([]Problem{{What: "this branch changes nothing against the base — there is nothing to merge"}}, nil)
	}
	resolveOne := func(name string) (Rule, bool) {
		if strings.HasPrefix(name, UnderPrefix) {
			return UnderRule(name)
		}
		if r, ok := Builtins[name]; ok {
			return r, true
		}
		r, ok := declared.Rules[name]
		return r, ok
	}
	resolve := func(name string) (Rule, bool) {
		if !strings.Contains(name, Conjunction) {
			return resolveOne(name)
		}
		var parts []Rule
		for _, p := range strings.Split(name, Conjunction) {
			r, ok := resolveOne(p)
			if !ok {
				return Rule{}, false
			}
			parts = append(parts, r)
		}
		return Rule{
			Name: name,
			AppliesTo: func(e Entry) bool {
				for _, r := range parts {
					if !r.AppliesTo(e) {
						return false
					}
				}
				return true
			},
			Constraint: func(covered []Entry) string {
				for _, r := range parts {
					if r.Constraint != nil {
						if s := r.Constraint(covered); s != "" {
							return s
						}
					}
				}
				return ""
			},
		}, true
	}
	var unknown []string
	for _, n := range append(append([]string{}, norm.Allow...), norm.Reject...) {
		if n == Anything {
			continue
		}
		if _, ok := resolve(n); !ok {
			unknown = append(unknown, n)
		}
	}
	if len(unknown) > 0 {
		errs := ""
		if len(declared.Errors) > 0 {
			errs = " (rule declarations also failed to load: " + strings.Join(declared.Errors, "; ") + ")"
		}
		return refuse([]Problem{{What: "unresolved rule name(s): " + strings.Join(unknown, ", ") + " — an unknown rule authorizes nothing" + errs}}, nil)
	}

	files := []FileVerdict{}
	problems := []Problem{}
	var order []string
	coveredBy := map[string][]Entry{}
	for _, e := range entries {
		file := e.File
		if policySources.MatchString(file) && !isCommentOnlyChange(e) {
			mount := false
			for _, n := range norm.Allow {
				if r, ok := resolve(n); ok && r.CoversPolicySource && r.AppliesTo(e) {
					mount = true
					break
				}
			}
			if !mount && vendoredMount.MatchString(file) {
				for _, n := range norm.Allow {
					if r, ok := resolve(n); ok && r.CoversMount && r.AppliesTo(e) {
						mount = true
						break
					}
				}
			}
			if !mount {
				files = append(files, FileVerdict{file, "policy-source"})
				problems = append(problems, Problem{&file, file + " defines auto-merge policy itself — no granular policy may change it"})
				continue
			}
		}
		rejected := ""
		for _, n := range norm.Reject {
			if r, _ := resolve(n); r.AppliesTo(e) {
				rejected = n
				break
			}
		}
		if rejected != "" {
			files = append(files, FileVerdict{file, "rejected:" + rejected})
			problems = append(problems, Problem{&file, file + " matches reject:" + rejected})
			continue
		}
		coverer := ""
		for _, n := range norm.Allow {
			if n == Anything {
				coverer = n
				break
			}
			if r, _ := resolve(n); r.AppliesTo(e) {
				coverer = n
				break
			}
		}
		if coverer == "" {
			files = append(files, FileVerdict{file, "uncovered"})
			problems = append(problems, Problem{&file, fmt.Sprintf("%s (%s %s) is covered by no allow term", file, e.ChangeKind(), ClassifyPath(file))})
			continue
		}
		files = append(files, FileVerdict{file, "covered:" + coverer})
		if _, seen := coveredBy[coverer]; !seen {
			order = append(order, coverer)
		}
		coveredBy[coverer] = append(coveredBy[coverer], e)
	}
	for _, name := range order {
		if r, ok := resolve(name); ok && r.Constraint != nil {
			if failed := r.Constraint(coveredBy[name]); failed != "" {
				problems = append(problems, Problem{What: name + ": " + failed})
			}
		}
	}
	if len(problems) > 0 {
		return refuse(problems, files)
	}
	return Verdict{Mergeable: true, Why: "every changed file is covered (" + strings.Join(order, ", ") + ")", Files: files, Problems: []Problem{}}
}

// DeclaredBy is the rules the given packs declare, compiled in their order.
func DeclaredBy(packs []packset.Pack) Declared {
	var read []PackRules
	for _, p := range packs {
		if pr, ok := ReadPackRules(p.ID, p.Dir); ok {
			read = append(read, pr)
		}
	}
	return Compile(read)
}
