package declared

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// Helpers reading a compiled spec.

// items is a list value's entries as objects; anything else is none.
func items(v any) []map[string]any {
	l, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(l))
	for _, e := range l {
		out = append(out, obj(e))
	}
	return out
}

// obj is v as an object, or an empty one.
func obj(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// re is v as a pattern, or nil.
func re(v any) *Regex {
	r, _ := v.(*Regex)
	return r
}

// has is the Node engine's `x !== undefined`.
func has(m map[string]any, k string) bool {
	_, ok := m[k]
	return ok
}

func get(m map[string]any, k string) any {
	if v, ok := m[k]; ok {
		return v
	}
	return undefined
}

func str(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

func isWholeNumber(v any) bool {
	f, ok := v.(float64)
	return ok && f == math.Trunc(f) && !math.IsInf(f, 0)
}

func num(v any) float64 {
	f, _ := v.(float64)
	return f
}

func quoteList(keys []string) string {
	q := make([]string, len(keys))
	for i, k := range keys {
		q[i] = fmt.Sprintf("%q", k)
	}
	return strings.Join(q, ", ")
}

func validateEntryShapes(spec map[string]any, where string) error {
	fail := func(format string, a ...any) error {
		return errors.New(where + ": " + fmt.Sprintf(format, a...))
	}
	scope := get(spec, "scope")
	if has(spec, "scope") && scope != "work" && scope != "action" {
		return fail(`"scope" takes "work" (judging the change), "action" (judging a tool call) or nothing at all (the default, judging the repo), not %s`, jsonish(scope))
	}
	for _, key := range workAssertions {
		if has(spec, key) && scope != "work" {
			return fail(`%q reads the change, so its declaration needs scope: "work"`, key)
		}
	}
	if has(spec, "guardToolCalls") && scope != "action" {
		return fail(`"guardToolCalls" judges a tool call, so its declaration needs scope: "action"`)
	}
	if scope == "action" && len(items(spec["guardToolCalls"])) == 0 {
		return fail(`an action declaration asserts nothing — add "guardToolCalls"`)
	}
	for _, a := range items(spec["guardToolCalls"]) {
		t, isStr := str(a["tool"])
		named := isStr && strings.TrimSpace(t) != ""
		if !named && re(a["tool"]) == nil {
			return fail(`a guardToolCalls entry needs "tool" — the tool's exact name, or a regex over names`)
		}
		n := 0
		for _, k := range []string{"match", "requireMatch", "inputMatches", "inputFieldAbsent", "atMostPerSession"} {
			if has(a, k) {
				n++
			}
		}
		if n == 0 {
			return fail(`a guardToolCalls entry names a condition — "match", "requireMatch", "inputMatches", "inputFieldAbsent" or "atMostPerSession"`)
		}
		if _, ok := str(a["inputField"]); (has(a, "match") || has(a, "requireMatch") || has(a, "unlessMatches")) && !ok {
			return fail(`"match", "requireMatch" and "unlessMatches" read the value at "inputField" — name the field`)
		}
		if has(a, "inputFieldAbsent") {
			l, ok := a["inputFieldAbsent"].([]any)
			good := ok && len(l) > 0
			for _, k := range l {
				if _, s := k.(string); !s {
					good = false
				}
			}
			if !good {
				return fail(`"inputFieldAbsent" is a non-empty list of field names`)
			}
		}
		if has(a, "atMostPerSession") && !(isWholeNumber(a["atMostPerSession"]) && num(a["atMostPerSession"]) > 0) {
			return fail(`"atMostPerSession" is a positive whole number of calls`)
		}
	}
	for _, key := range []string{"forbidAddedLinesMatching", "forbidRemovedLinesMatching"} {
		for _, a := range items(spec[key]) {
			if re(a["inFilesMatching"]) == nil || re(a["match"]) == nil {
				return fail(`a %s entry needs "inFilesMatching" (which changed files) and "match" (the line pattern), both regexes`, key)
			}
		}
	}
	for _, a := range items(spec["requireCoChange"]) {
		n := 0
		if has(a, "whenChangedFileMatches") {
			n++
		}
		if has(a, "whenAddedLineMatches") {
			n++
		}
		if n != 1 {
			return fail(`a requireCoChange entry triggers on exactly one of "whenChangedFileMatches" or "whenAddedLineMatches"`)
		}
		if has(a, "whenAddedLineMatches") {
			w := obj(a["whenAddedLineMatches"])
			if re(w["inFilesMatching"]) == nil || re(w["match"]) == nil {
				return fail(`"whenAddedLineMatches" takes "inFilesMatching" (which changed files) and "match" (the added-line pattern), both regexes`)
			}
		}
		if re(a["requireChangedFileMatching"]) == nil {
			return fail(`a requireCoChange entry needs "requireChangedFileMatching", the pattern some changed file must match`)
		}
	}
	for _, a := range items(spec["flagUntrackedFilesMatching"]) {
		if re(a["match"]) == nil {
			return fail(`a flagUntrackedFilesMatching entry needs "match", the path pattern`)
		}
	}
	for _, a := range items(spec["forbidTrackedPathsMatching"]) {
		if re(a["match"]) == nil {
			return fail(`a forbidTrackedPathsMatching entry needs "match", the path pattern`)
		}
	}
	if has(spec, "whenReplyClassIncludes") {
		classes := arr(spec["whenReplyClassIncludes"])
		bad := len(classes) == 0
		for _, c := range classes {
			s, _ := c.(string)
			if !contains(replyClasses, s) {
				bad = true
			}
		}
		if bad {
			return fail(`"whenReplyClassIncludes" names comment classes from %s`, quoteList(replyClasses))
		}
	}
	for _, a := range items(spec["forbidAddedValueInArray"]) {
		if has(a, "file") == has(a, "filesMatching") {
			return fail(`a forbidAddedValueInArray entry selects by exactly one of "file" or "filesMatching"`)
		}
		if truthy(a["whereFileContains"]) && !has(a, "filesMatching") {
			return fail(`"whereFileContains" refines "filesMatching" and cannot go with "file"`)
		}
		if !fieldList(a["atFields"]) {
			return fail(`"atFields" is a non-empty list of field paths whose arrays the change may not grow`)
		}
	}
	for _, a := range items(spec["checkBranchCommits"]) {
		if re(a["someMessageMatches"]) == nil {
			return fail(`a checkBranchCommits entry needs "someMessageMatches", the pattern one message must carry`)
		}
	}
	for _, a := range items(spec["countMatchingLines"]) {
		if re(a["linesMatching"]) == nil {
			return fail(`a countMatchingLines entry needs "linesMatching", the pattern it counts`)
		}
		var bounds []any
		for _, k := range []string{"atLeast", "atMost"} {
			if has(a, k) {
				bounds = append(bounds, a[k])
			}
		}
		if len(bounds) == 0 {
			return fail(`a countMatchingLines entry needs a bound — "atLeast", "atMost", or both`)
		}
		for _, b := range bounds {
			if !isWholeNumber(b) || num(b) < 0 {
				return fail(`countMatchingLines bounds ("atLeast"/"atMost") are whole numbers of lines`)
			}
		}
		if has(a, "atLeast") && has(a, "atMost") && num(a["atLeast"]) > num(a["atMost"]) {
			return fail(`countMatchingLines declares "atLeast" above "atMost" — no count can satisfy it`)
		}
	}
	for _, a := range items(spec["checkParsedFiles"]) {
		n := 0
		for _, k := range []string{"file", "filesMatching", "everyScannedFile"} {
			if has(a, k) {
				n++
			}
		}
		if n != 1 {
			return fail(`a checkParsedFiles entry selects by exactly one of "file", "filesMatching" or "everyScannedFile"`)
		}
		if truthy(a["whereFileContains"]) && !has(a, "filesMatching") {
			return fail(`"whereFileContains" refines "filesMatching" and cannot go with "file"`)
		}
		if truthy(a["requireFieldMatching"]) {
			r := obj(a["requireFieldMatching"])
			if _, ok := str(r["field"]); !ok || re(r["pattern"]) == nil {
				return fail(`"requireFieldMatching" takes the "field" to read and the "pattern" its value must match`)
			}
		}
		asserts := false
		for _, k := range []string{"requireField", "requireFieldMatching", "forbidField", "forbidValueInArray", "requireValueInArray", "requireEqualFields"} {
			asserts = asserts || truthy(a[k])
		}
		if !asserts {
			return fail(`a checkParsedFiles entry asserts nothing — add requireField, requireFieldMatching, forbidField, forbidValueInArray, requireValueInArray, or requireEqualFields`)
		}
	}
	setSources := []string{"fromParsedFile", "fromParsedFilesMatching", "fromLinesMatching", "fromTrackedPathsMatching", "fromAddedLinesMatching"}
	for _, s := range items(spec["extractValueSets"]) {
		if n, ok := str(s["setName"]); !ok || strings.TrimSpace(n) == "" {
			return fail(`an extractValueSets entry needs a non-empty "setName"`)
		}
		var sources []string
		for _, k := range setSources {
			if has(s, k) {
				sources = append(sources, k)
			}
		}
		if len(sources) != 1 {
			return fail(`an extractValueSets entry derives from exactly one source — %s`, quoteList(setSources))
		}
		source := sources[0]
		parsedSource := source == "fromParsedFile" || source == "fromParsedFilesMatching"
		lineSource := source == "fromLinesMatching" || source == "fromAddedLinesMatching"
		if truthy(s["whereFileContains"]) && source != "fromParsedFilesMatching" {
			return fail(`"whereFileContains" refines "fromParsedFilesMatching" and cannot go with %q`, source)
		}
		var fields []any
		for _, k := range []string{"valuesOfArraysAtFields", "valuesAtFields"} {
			if has(s, k) {
				fields = append(fields, s[k])
			}
		}
		if parsedSource && (len(fields) != 1 || !fieldList(fields[0])) {
			return fail(`a parsed source reads exactly one of "valuesOfArraysAtFields" or "valuesAtFields" — a non-empty list of field paths`)
		}
		if !parsedSource && len(fields) > 0 {
			return fail(`"valuesOfArraysAtFields"/"valuesAtFields" read parsed documents and cannot go with %q`, source)
		}
		if lineSource {
			if re(s["inFilesMatching"]) == nil {
				return fail(`%q needs "inFilesMatching", the files whose lines it reads`, source)
			}
			if r := re(s[source]); r == nil || !strings.Contains(r.Source, "(?<value>") {
				return fail(`%q needs a named group "(?<value>…)" — that group is what each matching line contributes to the set`, source)
			}
		} else if has(s, "inFilesMatching") || has(s, "splitValuesOn") {
			return fail(`"inFilesMatching" and "splitValuesOn" belong to a line source and cannot go with %q`, source)
		}
		if source == "fromAddedLinesMatching" && scope != "work" {
			return fail(`"fromAddedLinesMatching" reads the change, so its declaration needs scope: "work"`)
		}
		if s["whenSetEmpty"] != "assertNothing" && !isMessage(s["whenSetEmpty"]) {
			return fail(`"whenSetEmpty" is "assertNothing" or a { what, fix } reporting the empty set — emptiness is declared, never defaulted`)
		}
	}
	declaredSets := map[string]bool{}
	var setNames []string
	for _, s := range items(spec["extractValueSets"]) {
		n, _ := str(s["setName"])
		if !declaredSets[n] {
			setNames = append(setNames, n)
		}
		declaredSets[n] = true
	}
	setList := strings.Join(setNames, ", ")
	if setList == "" {
		setList = "none"
	}
	declared := func(name any, key string) error {
		n, ok := str(name)
		if !ok || !declaredSets[n] {
			return fail(`"%s: %s" names no declared value set — declare it in "extractValueSets" (declared: %s)`, key, jsonish(name), setList)
		}
		return nil
	}
	for _, a := range items(spec["checkSetValues"]) {
		if err := declared(get(a, "setName"), "setName"); err != nil {
			return err
		}
		var forms []string
		for _, k := range []string{"requireSomeFileMatching", "forbidEveryFileMatching", "requirePathExists", "requireTrackedPathMatching"} {
			if has(a, k) {
				forms = append(forms, k)
			}
		}
		if len(forms) != 1 {
			return fail(`a checkSetValues entry asserts exactly one of "requireSomeFileMatching", "forbidEveryFileMatching", "requirePathExists" or "requireTrackedPathMatching"`)
		}
		for _, k := range []string{"requireSomeFileMatching", "forbidEveryFileMatching"} {
			if !has(a, k) {
				continue
			}
			t := obj(a[k])
			_, p := str(t["pathMatching"])
			_, x := str(t["text"])
			if !p || !x {
				return fail(`%q takes "pathMatching" (which files) and "text" (what their text must match), both regex templates`, forms[0])
			}
		}
		if has(a, "requirePathExists") {
			if _, ok := str(a["requirePathExists"]); !ok {
				return fail(`"requirePathExists" is a path template such as "packs/{value}/pack.json"`)
			}
		}
		if has(a, "requireTrackedPathMatching") {
			s, ok := str(a["requireTrackedPathMatching"])
			_, flags, form := parseForm(s)
			if !ok || !form || checkFlags(flags) != nil {
				return fail(`"requireTrackedPathMatching" is a regex template in /pattern/flags form`)
			}
		}
		if has(a, "valueIsPattern") && a["valueIsPattern"] != true {
			return fail(`"valueIsPattern" is true or absent`)
		}
		if truthy(a["valueIsPattern"]) && has(a, "requirePathExists") {
			return fail(`"valueIsPattern" inserts the value into a regex, and "requirePathExists" takes a path`)
		}
	}
	for _, a := range items(spec["checkSetPairs"]) {
		if err := declared(get(a, "everyValueOf"), "everyValueOf"); err != nil {
			return err
		}
		n := 0
		for _, k := range []string{"mustAlsoBeIn", "mustNotBeIn"} {
			if has(a, k) {
				n++
			}
		}
		if n != 1 {
			return fail(`a checkSetPairs entry relates "everyValueOf" to exactly one of "mustAlsoBeIn" or "mustNotBeIn"`)
		}
		key := "mustNotBeIn"
		if has(a, "mustAlsoBeIn") {
			key = "mustAlsoBeIn"
		}
		if err := declared(a[key], key); err != nil {
			return err
		}
	}
	for _, a := range items(spec["requireIdenticalFiles"]) {
		if re(a["everyFileMatching"]) == nil {
			return fail(`a requireIdenticalFiles entry needs "everyFileMatching", the files that must have a twin`)
		}
		if t, ok := str(a["twinAt"]); !ok || strings.TrimSpace(t) == "" {
			return fail(`"twinAt" is the twin's path template, over {path}, {basename} and the pattern's named groups`)
		}
		if a["whenTwinAbsent"] != "assertNothing" && !isMessage(a["whenTwinAbsent"]) {
			return fail(`"whenTwinAbsent" is "assertNothing" or a { what, fix } — the absent twin is declared, never defaulted`)
		}
	}
	for _, a := range items(spec["requireIndexCoverage"]) {
		n := 0
		for _, k := range []string{"eachTrackedPathMatching", "eachScannedPathMatching", "eachValueOfSet"} {
			if has(a, k) {
				n++
			}
		}
		if n != 1 {
			return fail(`a requireIndexCoverage entry quantifies by exactly one of "eachTrackedPathMatching", "eachScannedPathMatching", or "eachValueOfSet"`)
		}
		if has(a, "eachValueOfSet") {
			if s, ok := str(a["eachValueOfSet"]); !ok || !declaredSets[s] {
				return fail(`"eachValueOfSet: %s" names no declared value set — declare it in "extractValueSets" (declared: %s)`, jsonish(a["eachValueOfSet"]), setList)
			}
			if has(a, "whoseTextMatches") {
				return fail(`"whoseTextMatches" refines a path quantifier and cannot go with "eachValueOfSet"`)
			}
			if has(a, "coveredByGlobLinesMatching") {
				return fail(`"coveredByGlobLinesMatching" covers paths and cannot go with "eachValueOfSet"`)
			}
		}
		n = 0
		for _, k := range []string{"coveredByText", "coveredByGlobLinesMatching", "coveredByValueInArrayAtField"} {
			if has(a, k) {
				n++
			}
		}
		if n != 1 {
			return fail(`a requireIndexCoverage entry declares exactly one coverage form — "coveredByText", "coveredByGlobLinesMatching", or "coveredByValueInArrayAtField"`)
		}
		if a["whenIndexFileAbsent"] != "assertNothing" && a["whenIndexFileAbsent"] != "flagEveryPath" {
			return fail(`"whenIndexFileAbsent" must be "assertNothing" or "flagEveryPath" — the divergent case is declared, never defaulted`)
		}
		if a["anchorFindingsAt"] != "indexFile" && a["anchorFindingsAt"] != "eachUncoveredPath" {
			return fail(`"anchorFindingsAt" must be "indexFile" or "eachUncoveredPath"`)
		}
	}
	return nil
}

func fieldList(v any) bool {
	l, ok := v.([]any)
	if !ok || len(l) == 0 {
		return false
	}
	for _, f := range l {
		if _, ok := f.(string); !ok {
			return false
		}
	}
	return true
}

func isMessage(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	_, ok = m["what"].(string)
	return ok
}
