package declared

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/checks/declared/refs"
	"github.com/missingbulb/ClaudiniteEngine/shared/descriptor"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
)

// Name is the descriptor a pack or skill declares its checks in.
const Name = "declared-checks"

// TOMLKey is the array-of-tables key a TOML declaration lists its checks
// under: [[check]].
const TOMLKey = "check"

// Check is one declared check, compiled.
type Check struct {
	ID     string
	Pack   string
	Skill  string
	OnFail string
	Since  string
	Why    string
	// Scope is "", "work" or "action".
	Scope string
	// File is the declaration's path relative to the repo.
	File string
	Tags []string
	Spec map[string]any
	// RetiredSeverity is the "severity" a local or temp pack's
	// declaration still carries, read as its on_fail; "" for none.
	RetiredSeverity string

	scanMatchers    []*Regex
	excludeMatchers []any
	namedScan       map[string]any
	edges           []refs.Edge
}

// Unplaced is a key the vocabulary has no place for, dropped at load.
type Unplaced struct {
	Key       string
	Container string
	Allowed   []string
}

// Kind is the scope tag a check runs under.
func (c *Check) Kind() string {
	if c.Scope == "" {
		return "world"
	}
	return c.Scope
}

// Load reads the declared checks of the pack at dir (relative path rel in
// the repo): its own declaration and each skills/<name>/ one. A broken
// declaration fails the whole pack's load, naming the file. own is true
// for a local or temp pack, whose declarations may still spell on_fail as
// the retired severity.
func Load(repo, rel, pack string, own bool) ([]*Check, error) {
	dir := filepath.Join(repo, filepath.FromSlash(rel))
	out, err := loadDir(dir, rel, pack, "", own)
	if err != nil {
		return nil, err
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "skills"))
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		cs, err := loadDir(filepath.Join(dir, "skills", n), rel+"/skills/"+n, pack, n, own)
		if err != nil {
			return nil, err
		}
		out = append(out, cs...)
	}
	return out, nil
}

// LoadError is a declaration that did not load.
type LoadError struct {
	// Path is the declaration file, or its folder when the folder holds
	// two spellings.
	Path string
	Err  error
}

func (e *LoadError) Error() string { return e.Path + ": " + e.Err.Error() }
func (e *LoadError) Unwrap() error { return e.Err }

func loadDir(dir, rel, pack, skill string, own bool) ([]*Check, error) {
	path, format, err := descriptor.Find(dir, Name)
	if errors.Is(err, descriptor.ErrAbsent) {
		return nil, nil
	}
	if err != nil {
		return nil, &LoadError{rel, err}
	}
	fileRel := rel + "/" + filepath.Base(path)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, &LoadError{fileRel, err}
	}
	decls, err := Declarations(raw, format)
	if err != nil {
		return nil, &LoadError{fileRel, err}
	}
	var selfExclude *Regex
	if skill != "" {
		selfExclude = mustRegex(`(^|/)skills/`+escapeRe(skill)+`/`, "")
	}
	var out []*Check
	for _, d := range decls {
		severity := ""
		// @legacy-tolerance advisory:local-pack-shape retire:#52
		if s, _ := d["severity"].(string); own && settings.RetiredOnFail[s] != "" {
			if _, set := d["on_fail"]; !set {
				d["on_fail"] = settings.RetiredOnFail[s]
			}
			delete(d, "severity")
			severity = s
		}
		c, err := Compile(d, selfExclude)
		if err != nil {
			return nil, &LoadError{fileRel, err}
		}
		c.Pack, c.Skill, c.File, c.RetiredSeverity = pack, skill, fileRel, severity
		c.Tags = []string{c.Kind(), "declared", pack}
		if c.Scope == "action" {
			c.Tags = []string{"action", "work", "pre-tool-use", "declared", pack}
		}
		out = append(out, c)
	}
	return out, nil
}

// Declarations parses a declaration file into its list of entries: a JSON
// or YAML array, or a TOML file of [[check]] tables.
func Declarations(raw []byte, format descriptor.Format) ([]map[string]any, error) {
	v, err := descriptor.ParseBytes(raw, format)
	if err != nil {
		return nil, err
	}
	if format == descriptor.TOML {
		m, _ := v.(map[string]any)
		for k := range m {
			if k != TOMLKey {
				return nil, fmt.Errorf("a TOML declaration lists its checks as [[%s]] tables and holds nothing else, not %q", TOMLKey, k)
			}
		}
		v = m[TOMLKey]
		if v == nil {
			v = []any{}
		}
	}
	list, ok := v.([]any)
	if !ok {
		return nil, errors.New("must be a list of check declarations")
	}
	out := make([]map[string]any, 0, len(list))
	for i, e := range list {
		m, ok := e.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("entry %d is not a check declaration object", i+1)
		}
		out = append(out, m)
	}
	return out, nil
}

// UnplacedKeys lists the keys of a declaration the vocabulary cannot
// place, in a stable order.
func UnplacedKeys(decl map[string]any) []Unplaced {
	var out []Unplaced
	partition(decl, "spec", &out)
	return out
}

func partition(value any, container string, unplaced *[]Unplaced) any {
	if list, ok := value.([]any); ok {
		out := make([]any, len(list))
		for i, v := range list {
			out[i] = partition(v, container, unplaced)
		}
		return out
	}
	allowed, ok := specKeys[container]
	m, isMap := value.(map[string]any)
	if !ok || !isMap {
		return value
	}
	placed := map[string]any{}
	for _, k := range sortedKeys(m) {
		if !contains(allowed, k) {
			*unplaced = append(*unplaced, Unplaced{Key: k, Container: container, Allowed: allowed})
			continue
		}
		placed[k] = partition(m[k], k, unplaced)
	}
	return placed
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

var sinceRe = mustRegex(`^\d{4}-\d{2}-\d{2}$`, "")

// reForm is a /pattern/flags string.
var reForm = mustRegex(`^\/([\s\S]*)\/([dgimsuvy]*)$`, "")

func parseForm(s string) (body, flags string, ok bool) {
	if !reForm.Test(s) {
		return "", "", false
	}
	last := strings.LastIndex(s, "/")
	return s[1:last], s[last+1:], true
}

// Compile validates and compiles one declaration.
func Compile(decl map[string]any, selfExclude *Regex) (*Check, error) {
	id, _ := decl["id"].(string)
	if strings.TrimSpace(id) == "" {
		return nil, errors.New(`a declared check needs a non-empty "id"`)
	}
	where := fmt.Sprintf("the declared check %q", id)
	if _, has := decl["severity"]; has {
		return nil, fmt.Errorf(`%s: "severity" is the retired spelling; write "on_fail": "block" (was "blocking") or "advise" (was "advisory")`, where)
	}
	onFail, _ := decl["on_fail"].(string)
	if onFail != "block" && onFail != "advise" {
		return nil, fmt.Errorf(`%s: on_fail must be "block" or "advise", not %s`, where, jsonish(decl["on_fail"]))
	}
	since, hasSince := decl["since"]
	if hasSince {
		s, ok := since.(string)
		if !ok || !sinceRe.Test(s) {
			return nil, fmt.Errorf(`%s: "since" is the date this check was added, as YYYY-MM-DD, not %s`, where, jsonish(since))
		}
	}
	var ignored []Unplaced
	compiled, err := compileSpec(partition(decl, "spec", &ignored), "", where)
	if err != nil {
		return nil, err
	}
	spec := compiled.(map[string]any)
	if err := validateEntryShapes(spec, where); err != nil {
		return nil, err
	}
	classPatterns := func(v any) ([]*Regex, error) {
		var out []*Regex
		for _, n := range arr(v) {
			name, _ := n.(string)
			re, ok := fileClasses[name]
			if !ok {
				return nil, fmt.Errorf(`%s: %q is not a file class — the classes are: %s`, where, jsString(n), strings.Join(fileClassNames, ", "))
			}
			out = append(out, re)
		}
		return out, nil
	}
	scanClasses, err := classPatterns(spec["scanFileClasses"])
	if err != nil {
		return nil, err
	}
	c := &Check{ID: id, OnFail: onFail, Spec: spec}
	c.Why, _ = spec["failureMessage"].(string)
	c.Scope, _ = spec["scope"].(string)
	if s, ok := since.(string); ok {
		c.Since = s
	}
	if _, ok := spec["scanFiles"].(string); ok && len(scanClasses) > 0 {
		return nil, fmt.Errorf("%s: scanFileClasses cannot combine with an exact-path scanFiles", where)
	}
	if named, ok := spec["scanFiles"].(map[string]any); ok {
		c.namedScan = named
		delete(spec, "scanFiles")
		if len(scanClasses) > 0 {
			return nil, fmt.Errorf("%s: scanFileClasses cannot combine with a field-named scanFiles", where)
		}
		_, reOK := named["inParsedFilesMatching"].(*Regex)
		_, fieldOK := named["namedByField"].(string)
		if !reOK || !fieldOK {
			return nil, fmt.Errorf(`%s: a field-named scanFiles needs "inParsedFilesMatching" (the documents that name files) and "namedByField" (the field holding each name)`, where)
		}
		if w, has := named["whereFileContains"]; has && truthy(w) {
			if _, ok := w.(*Regex); !ok {
				return nil, fmt.Errorf(`%s: "whereFileContains" refines the naming documents by their text, so it takes a regex`, where)
			}
		}
	}
	if re, ok := spec["scanFiles"].(*Regex); ok {
		c.scanMatchers = append(c.scanMatchers, re)
	}
	c.scanMatchers = append(c.scanMatchers, scanClasses...)
	if ex, has := spec["excludeFiles"]; has && ex != nil {
		c.excludeMatchers = append(c.excludeMatchers, ex)
	}
	exClasses, err := classPatterns(spec["excludeFileClasses"])
	if err != nil {
		return nil, err
	}
	for _, re := range exClasses {
		c.excludeMatchers = append(c.excludeMatchers, re)
	}
	if selfExclude != nil {
		c.excludeMatchers = append(c.excludeMatchers, selfExclude)
	}
	if fix, ok := spec["fix"].(string); ok {
		applyFixDefault(spec, fix)
	}
	if fr, has := spec["forbidReferences"]; has {
		edges, problems := refs.Normalize(uncompile(fr))
		if len(problems) > 0 {
			return nil, fmt.Errorf("%s: %s — %s", where, problems[0].What, problems[0].Fix)
		}
		c.edges = edges
	}
	return c, nil
}

// uncompile turns compiled patterns back into their source strings, for
// a block (forbidReferences) whose own normalizer reads strings.
func uncompile(v any) any {
	switch x := v.(type) {
	case *Regex:
		return "/" + x.Source + "/" + x.Flags
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = uncompile(e)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			out[k] = uncompile(e)
		}
		return out
	}
	return v
}

func jsonish(v any) string {
	switch x := v.(type) {
	case string:
		return fmt.Sprintf("%q", x)
	case undefinedT:
		return "undefined"
	case nil:
		return "null"
	}
	return jsString(v)
}

// arr is the Node engine's arr(): nothing, one value, or a list.
func arr(v any) []any {
	if v == nil || isUndef(v) {
		return nil
	}
	if l, ok := v.([]any); ok {
		return l
	}
	return []any{v}
}

func compileSpec(value any, key, where string) (any, error) {
	switch x := value.(type) {
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			c, err := compileSpec(v, key, where)
			if err != nil {
				return nil, err
			}
			out[i] = c
		}
		return out, nil
	case string:
		if !patternKeys[key] && !pathOrPatternKeys[key] {
			return x, nil
		}
		body, flags, ok := parseForm(x)
		if !ok {
			if pathOrPatternKeys[key] {
				return x, nil
			}
			return nil, fmt.Errorf(`%s: %q takes a regex in /pattern/flags form, not %q`, where, key, x)
		}
		re, err := compileRegex(body, flags)
		if err != nil {
			return nil, fmt.Errorf(`%s: %q is not a valid regex — %v`, where, key, err)
		}
		return re, nil
	case map[string]any:
		if templateContainers[key] {
			out := map[string]any{}
			for _, k := range sortedKeys(x) {
				s, ok := x[k].(string)
				if !ok {
					return nil, fmt.Errorf(`%s: "%s.%s" takes a regex template in /pattern/flags form, not %s`, where, key, k, jsonish(x[k]))
				}
				if _, flags, ok := parseForm(s); !ok {
					return nil, fmt.Errorf(`%s: "%s.%s" takes a regex template in /pattern/flags form, not %q`, where, key, k, s)
				} else if err := checkFlags(flags); err != nil {
					return nil, fmt.Errorf(`%s: "%s.%s": %v`, where, key, k, err)
				}
				out[k] = s
			}
			return out, nil
		}
		out := map[string]any{}
		for k, v := range x {
			c, err := compileSpec(v, k, where)
			if err != nil {
				return nil, err
			}
			out[k] = c
		}
		return out, nil
	}
	return value, nil
}

func checkFlags(flags string) error {
	for _, f := range flags {
		if !strings.ContainsRune("mis", f) {
			return fmt.Errorf("the flag %q is not one this engine takes (m, i and s are)", f)
		}
	}
	return nil
}

func applyFixDefault(value any, fix string) {
	switch x := value.(type) {
	case []any:
		for _, v := range x {
			applyFixDefault(v, fix)
		}
	case map[string]any:
		if _, ok := x["what"].(string); ok {
			if _, has := x["fix"]; !has {
				x["fix"] = fix
			}
		}
		for k, v := range x {
			if k != "what" && k != "fix" {
				applyFixDefault(v, fix)
			}
		}
	}
}
