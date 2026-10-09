// Package mergepolicy is the automerge engine: may this diff land without
// a person? A task declares `automerge` ("nothing", "anything", or a list
// of named diff classes), and the verdict is arithmetic over the branch's
// actual diff, never the run's opinion of its own work. A policy is its
// author's prediction of the change's shape; a diff outside it parks for
// review, which is the mechanism's purpose rather than its failure.
//
// A list is a union, `&&` is the one narrowing operator, `reject:<name>`
// vetoes, `under:<dir>` scopes to a folder, and an unknown name fails
// closed. The built-in classes and the composite are string-identical with
// the Node engine's merge-policy.mjs at missingbulb/Claudinite@057841ac,
// but for the engine-update and usage-fold classes, which that engine has
// no counterpart of; a pack adds its own as data in merge-rules.json (or
// .yaml, .toml). Under a list policy the files that define policies are
// never coverable unless the edit is comment-only, so a run cannot widen
// its own authorization; the one exception is engine-pin-move, a settings
// edit moving nothing but the engine pin.
package mergepolicy

import (
	"encoding/json"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/checksdk"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/jsjson"
	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/jsregex"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/flatdecl"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/settings"
)

// The two whole policies.
const (
	Nothing  = "nothing"
	Anything = "anything"
)

// Trailer is the commit-message trailer a run stamps when it intends to
// land its own pull request under a granular policy.
const Trailer = "Claudinite-Automerge-Policy"

// TrailerRe reads the trailer's policy expression.
var TrailerRe = regexp.MustCompile(`(?m)^Claudinite-Automerge-Policy:[ \t]*(\S+)[ \t]*$`)

// UnderPrefix opens the inline folder scope.
const UnderPrefix = "under:"

// Conjunction is the intersection operator.
const Conjunction = "&&"

// RulesFile is the declared rules' descriptor name.
const RulesFile = "merge-rules"

// Entry is one changed file: a nil Before is an addition, a nil After a
// deletion.
type Entry struct {
	File   string  `json:"file"`
	Before *string `json:"before"`
	After  *string `json:"after"`
}

// ChangeKind is "added", "deleted" or "modified".
func (e Entry) ChangeKind() string {
	switch {
	case e.Before == nil:
		return "added"
	case e.After == nil:
		return "deleted"
	}
	return "modified"
}

var docExt = map[string]bool{".md": true, ".markdown": true, ".txt": true, ".rst": true}

var testDirs = map[string]bool{"test": true, "tests": true, "__tests__": true, "spec": true, "specs": true, "fixtures": true, "testdata": true}

var (
	testName   = regexp.MustCompile(`(?i)(^|[.\-_])(test|tests|spec)[.\-_]`)
	testPrefix = regexp.MustCompile(`(?i)^test_`)
)

// extname is Node's path.extname: a name's leading dot opens no extension.
func extname(name string) string {
	i := strings.LastIndex(name, ".")
	if i <= 0 {
		return ""
	}
	return name[i:]
}

// ClassifyPath is a path's kind from the path alone: "doc", "test" or
// "code". Directory names match on whole segments.
func ClassifyPath(file string) string {
	segs := strings.Split(file, "/")
	name := segs[len(segs)-1]
	for _, s := range segs[:len(segs)-1] {
		if testDirs[s] || strings.HasSuffix(s, "-tests") || strings.HasSuffix(s, "_tests") {
			return "test"
		}
	}
	if testName.MatchString(name) || testPrefix.MatchString(name) {
		return "test"
	}
	if docExt[strings.ToLower(extname(name))] {
		return "doc"
	}
	return "code"
}

// RemovalsOnly reports whether after is before with whole lines removed,
// in order; a deletion is every line removed.
func RemovalsOnly(before, after *string) bool {
	return shrinkOnly(before, after, func(line, original string) bool { return line == original })
}

// TrimsOnly is RemovalsOnly plus in-line trims: every surviving line is a
// character subsequence of the line it replaces.
func TrimsOnly(before, after *string) bool {
	return shrinkOnly(before, after, isCharSubsequence)
}

func shrinkOnly(before, after *string, ok func(line, original string) bool) bool {
	if after == nil {
		return true
	}
	if before == nil {
		return false
	}
	from := strings.Split(*before, "\n")
	i := 0
	for _, line := range strings.Split(*after, "\n") {
		for i < len(from) && !ok(line, from[i]) {
			i++
		}
		if i == len(from) {
			return false
		}
		i++
	}
	return true
}

// isCharSubsequence walks UTF-16 code units as JavaScript's indexOf does.
func isCharSubsequence(needle, hay string) bool {
	h := []rune(hay)
	i := 0
	for _, c := range needle {
		j := -1
		for k := i; k < len(h); k++ {
			if h[k] == c {
				j = k
				break
			}
		}
		if j < 0 {
			return false
		}
		i = j + 1
	}
	return true
}

func isCommentOnlyChange(e Entry) bool {
	return e.ChangeKind() == "modified" && checksdk.CommentOnly(e.File, e.Before, e.After)
}

func isRealCodeChange(e Entry) bool {
	return ClassifyPath(e.File) == "code" && !isCommentOnlyChange(e)
}

// Rule is one diff class. AppliesTo answers both roles a policy can use
// the name in; Constraint, when set, is a whole-diff condition over the
// entries the rule covered, returning "" when it holds. CoversPolicySource
// lets the rule cover a policy source it applies to: engine-pin-move's
// alone, which no declaration can set.
type Rule struct {
	Name               string
	AppliesTo          func(Entry) bool
	Constraint         func([]Entry) string
	CoversMount        bool
	CoversPolicySource bool
}

// settingsFile is the member's settings file, whose pin an engine update
// moves.
var settingsFile = regexp.MustCompile(`^\.claudinite/settings\.(json|yaml|toml)$`)

// EngineWorkflows are the workflows the engine manages, which an engine
// update pull request may carry. The workflows package's tests hold this
// list equal to its own, which this package cannot import.
var EngineWorkflows = []string{"claudinite-ci.yml", "claudinite-scheduler.yml", "claudinite-executor.yml"}

// isEnginePinMove is a settings edit moving only engine.version and
// engine.manifest (settings.PinOnlyChange).
func isEnginePinMove(e Entry) bool {
	m := settingsFile.FindStringSubmatch(e.File)
	if m == nil || e.ChangeKind() != "modified" {
		return false
	}
	return settings.PinOnlyChange([]byte(*e.Before), []byte(*e.After), settings.Format(m[1])) == nil
}

// isEngineUpdateFile is the member file, at its path or the legacy one,
// or a managed workflow, added or modified, or the launcher, modified.
func isEngineUpdateFile(e Entry) bool {
	if e.ChangeKind() == "deleted" {
		return false
	}
	if e.File == flatdecl.MemberFile || e.File == flatdecl.LegacyPath(flatdecl.MemberFile) {
		return true
	}
	if e.File == ".claudinite/launch" {
		return e.ChangeKind() == "modified"
	}
	for _, n := range EngineWorkflows {
		if e.File == ".github/workflows/"+n {
			return true
		}
	}
	return false
}

var jsFiles = regexp.MustCompile(`\.(mjs|cjs|jsx?|mts|cts|tsx?)$`)

// builtinOrder is the built-in classes in the Node registry's order.
var builtinOrder = []string{
	"doc-changes", "readme-changes", "comment-only-changes", "test-changes", "markdown-line-removals",
	"markdown-trims", "file-additions", "generated-file-changes", "javascript-changes",
	"single-file-code-changes", "single-folder-code-changes", "engine-pin-move", "engine-update-files",
	"rolling-usage-files", "rolling-usage-file-moves",
}

// The usage fold's two rolling files, and the legacy copies a fold moves
// them out of.
var (
	rollingUsageFile       = regexp.MustCompile(`^\.claudinite/usage/[^/]+\.json$`)
	legacyRollingUsageFile = regexp.MustCompile(`^\.claudinite/local/(usage|tasks-usage)\.GENERATED\.json$`)
)

// Builtins are the built-in diff classes by name.
var Builtins = map[string]Rule{
	"doc-changes": {AppliesTo: func(e Entry) bool { return e.ChangeKind() != "deleted" && ClassifyPath(e.File) == "doc" }},
	"readme-changes": {AppliesTo: func(e Entry) bool {
		return e.ChangeKind() != "deleted" && strings.ToLower(path.Base(e.File)) == "readme.md"
	}},
	"comment-only-changes": {AppliesTo: isCommentOnlyChange},
	"test-changes":         {AppliesTo: func(e Entry) bool { return ClassifyPath(e.File) == "test" }},
	"markdown-line-removals": {AppliesTo: func(e Entry) bool {
		return strings.HasSuffix(strings.ToLower(e.File), ".md") && e.ChangeKind() != "added" && RemovalsOnly(e.Before, e.After)
	}},
	"markdown-trims": {AppliesTo: func(e Entry) bool {
		return strings.HasSuffix(strings.ToLower(e.File), ".md") && e.ChangeKind() != "added" && TrimsOnly(e.Before, e.After)
	}},
	"file-additions": {AppliesTo: func(e Entry) bool { return e.ChangeKind() == "added" }},
	"generated-file-changes": {AppliesTo: func(e Entry) bool {
		return e.ChangeKind() != "deleted" && strings.Contains(path.Base(e.File), "GENERATED")
	}},
	"javascript-changes": {AppliesTo: func(e Entry) bool { return jsFiles.MatchString(e.File) }},
	"single-file-code-changes": {AppliesTo: isRealCodeChange, Constraint: func(covered []Entry) string {
		files := uniqueSorted(covered, func(e Entry) string { return e.File })
		if len(files) <= 1 {
			return ""
		}
		return "code changed in " + itoa(len(files)) + " files: " + strings.Join(files, ", ")
	}},
	"engine-pin-move":     {AppliesTo: isEnginePinMove, CoversPolicySource: true},
	"engine-update-files": {AppliesTo: isEngineUpdateFile},
	"rolling-usage-files": {AppliesTo: func(e Entry) bool {
		return e.ChangeKind() != "deleted" && rollingUsageFile.MatchString(e.File)
	}},
	"rolling-usage-file-moves": {AppliesTo: func(e Entry) bool {
		return e.ChangeKind() == "deleted" && legacyRollingUsageFile.MatchString(e.File)
	}},
	"single-folder-code-changes": {AppliesTo: isRealCodeChange, Constraint: func(covered []Entry) string {
		dirs := uniqueSorted(covered, func(e Entry) string { return path.Dir(e.File) })
		if len(dirs) <= 1 {
			return ""
		}
		return "code changed in " + itoa(len(dirs)) + " directories: " + strings.Join(dirs, ", ")
	}},
}

func init() {
	for name, r := range Builtins {
		r.Name = name
		Builtins[name] = r
	}
}

// BuiltinNames are the built-in classes in registry order.
func BuiltinNames() []string { return append([]string(nil), builtinOrder...) }

// Composites expand into their member allow terms.
var Composites = map[string][]string{
	"narrow-diff": {"doc-changes", "test-changes", "comment-only-changes", "single-folder-code-changes"},
}

func uniqueSorted(es []Entry, key func(Entry) string) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range es {
		k := key(e)
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func itoa(n int) string { return jsjson.FormatNumber(float64(n)) }

var pathSegment = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// UnderRule is the rule an `under:<dir>` term denotes, or false when the
// directory is not a usable repo-relative one.
func UnderRule(term string) (Rule, bool) {
	dir := strings.TrimRight(term[len(UnderPrefix):], "/")
	if dir == "" {
		return Rule{}, false
	}
	for _, s := range strings.Split(dir, "/") {
		if !pathSegment.MatchString(s) || s == "." || s == ".." {
			return Rule{}, false
		}
	}
	prefix := dir + "/"
	return Rule{Name: term, AppliesTo: func(e Entry) bool { return strings.HasPrefix(e.File, prefix) }}, true
}

func conjunctionParts(term string) []string {
	parts := strings.Split(term, Conjunction)
	for i, p := range parts {
		parts[i] = jsregex.Trim(p)
	}
	return parts
}

// Policy is a normalized policy: Kind "nothing", "anything", "rules" (with
// Allow and Reject) or "invalid" (with Reason).
type Policy struct {
	Kind          string
	Allow, Reject []string
	Reason        string
}

// MarshalJSON writes the Node engine's shape.
func (p Policy) MarshalJSON() ([]byte, error) {
	switch p.Kind {
	case "rules":
		return json.Marshal(struct {
			Kind   string   `json:"kind"`
			Allow  []string `json:"allow"`
			Reject []string `json:"reject"`
		}{p.Kind, nonNil(p.Allow), nonNil(p.Reject)})
	case "invalid":
		return json.Marshal(struct {
			Kind   string `json:"kind"`
			Reason string `json:"reason"`
		}{p.Kind, p.Reason})
	}
	return json.Marshal(struct {
		Kind string `json:"kind"`
	}{p.Kind})
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func invalid(reason string) Policy { return Policy{Kind: "invalid", Reason: reason} }

// isTermName is /^(under:\S+|[a-z0-9]+(-[a-z0-9]+)*)$/.
func isTermName(s string) bool {
	if rest, ok := strings.CutPrefix(s, UnderPrefix); ok {
		if rest == "" {
			return false
		}
		for _, r := range rest {
			if jsregex.IsSpace(r) {
				return false
			}
		}
		return true
	}
	return isKebab(s)
}

func isKebab(s string) bool {
	if s == "" {
		return false
	}
	for _, part := range strings.Split(s, "-") {
		if part == "" {
			return false
		}
		for _, r := range part {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
				return false
			}
		}
	}
	return true
}

func termsOf(raw any) []string {
	if arr, ok := raw.([]any); ok {
		out := make([]string, len(arr))
		for i, t := range arr {
			out[i] = jsregex.Trim(jsjson.StringOf(t))
		}
		return out
	}
	if arr, ok := raw.([]string); ok {
		out := make([]string, len(arr))
		for i, t := range arr {
			out[i] = jsregex.Trim(t)
		}
		return out
	}
	parts := strings.Split(jsregex.Trim(jsjson.StringOf(raw)), ";")
	for i, p := range parts {
		parts[i] = jsregex.Trim(p)
	}
	return parts
}

// Normalize reads a policy from any surface it rides: the declaration's
// string or list, an item's Merge field, the trailer's `a;b;reject:c`.
func Normalize(raw any) Policy {
	if raw == nil {
		return Policy{Kind: Nothing}
	}
	terms := termsOf(raw)
	if len(terms) == 1 {
		switch one := strings.ToLower(terms[0]); one {
		case Nothing, "":
			return Policy{Kind: Nothing}
		case Anything:
			return Policy{Kind: Anything}
		case "if-narrow", "yes", "true":
			return Normalize([]any{"narrow-diff"})
		}
	}
	var allow, reject []string
	for _, term := range terms {
		isReject := strings.HasPrefix(term, "reject:")
		body := term
		if isReject {
			body = term[len("reject:"):]
		}
		parts := conjunctionParts(body)
		for _, part := range parts {
			if !isTermName(part) {
				return invalid(`"` + term + `" is not a policy term (rule names or under:<dir>, joined with && and optionally reject:-prefixed)`)
			}
			if strings.HasPrefix(part, UnderPrefix) {
				if _, ok := UnderRule(part); !ok {
					return invalid(`"` + part + `" names no usable repo-relative directory — spell it under:packs/some-pack`)
				}
			}
		}
		if len(parts) > 1 {
			for _, p := range parts {
				if _, comp := Composites[p]; comp || p == Anything || p == Nothing {
					return invalid(`"` + p + `" cannot sit in an && term — intersect specific rules`)
				}
			}
		}
		name := strings.Join(parts, Conjunction)
		switch {
		case isReject:
			if _, comp := Composites[name]; comp {
				return invalid(`"` + term + `" rejects a composite — name the specific rules to reject`)
			}
			reject = append(reject, name)
		case name == Anything:
			allow = append(allow, name)
		case name == Nothing:
			return invalid(`"nothing" cannot sit in a rule list — it is a whole policy of its own`)
		default:
			if members, comp := Composites[name]; comp {
				allow = append(allow, members...)
			} else {
				allow = append(allow, name)
			}
		}
	}
	if len(allow) == 0 {
		return invalid(`the policy lists no allow term — "anything except X" is spelled ["anything", "reject:X"]`)
	}
	return Policy{Kind: "rules", Allow: dedupe(allow), Reject: dedupe(reject)}
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// Expression is the policy's string form, what rides the trailer: terms
// joined with `;`, whitespace around `&&` collapsed.
func Expression(raw any) string {
	var terms []string
	switch x := raw.(type) {
	case []any:
		for _, t := range x {
			terms = append(terms, jsjson.StringOf(t))
		}
	case []string:
		terms = x
	case nil:
		terms = strings.Split(Nothing, ";")
	default:
		terms = strings.Split(jsjson.StringOf(raw), ";")
	}
	out := make([]string, len(terms))
	for i, t := range terms {
		out[i] = strings.Join(conjunctionParts(jsregex.Trim(t)), Conjunction)
	}
	return strings.Join(out, ";")
}
