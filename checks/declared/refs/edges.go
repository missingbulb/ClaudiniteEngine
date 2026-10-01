package refs

import (
	"fmt"
	"sort"
	"strings"
)

var edgeKeys = []string{"from", "to", "between", "siblings", "scope", "allow", "except", "matchNames", "alsoMatchNames", "matchUniqueFilenames", "reason"}
var exceptionKeys = []string{"path", "to", "reason"}

func unknownKeys(m map[string]any, allowed []string) []string {
	var out []string
	for k := range m {
		if !contains(allowed, k) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func properties(n int) string {
	if n > 1 {
		return "properties"
	}
	return "property"
}

func quoted(keys []string) string {
	q := make([]string, len(keys))
	for i, k := range keys {
		q[i] = `"` + k + `"`
	}
	return strings.Join(q, ", ")
}

// stringList reads a string or a list of strings.
func stringList(v any) ([]string, bool) {
	switch x := v.(type) {
	case string:
		return []string{x}, true
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			s, ok := e.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	return nil, false
}

func parseFrom(raw any, at string, problems *[]Problem) ([]string, bool) {
	list, ok := stringList(raw)
	bad := !ok || len(list) == 0
	for _, f := range list {
		if strings.TrimSpace(f) == "" {
			bad = true
		}
	}
	if bad {
		*problems = append(*problems, Problem{at + ` needs a "from" folder (or an array of them), and a "to"`, `add "from": "checks" or "from": ["checks", "growth"], and a "to"`})
		return nil, false
	}
	var out []string
	for _, f := range list {
		if strings.Contains(f, "*") {
			*problems = append(*problems, Problem{fmt.Sprintf(`%s: "from" entry "%s" cannot contain a wildcard`, at, f), `name a real folder/file, e.g. "checks" — or "." for the repo root`})
			return nil, false
		}
		p := NormPrefix(f)
		if p == "" && strings.TrimSpace(f) != "." {
			*problems = append(*problems, Problem{fmt.Sprintf(`%s: "from" entry "%s" must name a real folder/file, or "." for the repo root`, at, f), `name the folder to guard, or write "." explicitly for the whole repo`})
			return nil, false
		}
		out = append(out, p)
	}
	return out, true
}

func parseExcept(raw any, has bool, at string, problems *[]Problem) ([]string, []Exception, bool) {
	var carve []string
	var exceptions []Exception
	if !has {
		return carve, exceptions, true
	}
	var list []any
	switch x := raw.(type) {
	case string:
		list = []any{x}
	case []any:
		list = x
	default:
		*problems = append(*problems, Problem{at + `: "except" must be an array (carve-out strings and/or { path, to?, reason } exceptions)`, `use "except": ["vendor", { "path": "src/bridge.js", "to": "server", "reason": "…" }]`})
		return nil, nil, false
	}
	for _, e := range list {
		if s0, ok := e.(string); ok {
			s := strings.ReplaceAll(s0, `\`, "/")
			if strings.HasPrefix(s, "*.") {
				if len(s) < 3 || strings.Contains(s[1:], "/") || strings.Contains(s[2:], "*") {
					*problems = append(*problems, Problem{fmt.Sprintf(`%s: pattern "%s" is not a "*.suffix" name pattern`, at, s0), `a pattern entry is "*." plus a name suffix, e.g. "*.stories.js"`})
					return nil, nil, false
				}
				carve = append(carve, s)
				continue
			}
			if isChildGlob(s) {
				prefix, rest := childGlobParts(s)
				p, r := NormPrefix(prefix), NormPrefix(rest)
				if p == "" || r == "" || strings.Contains(p, "*") || strings.Contains(r, "*") {
					*problems = append(*problems, Problem{fmt.Sprintf(`%s: carve-out "%s" must read "<folder>/*/<name>" - a folder, its child directories, then the name each carries`, at, s0), "name the folder whose children each carry the region, and the region's own name, with no other wildcard"})
					return nil, nil, false
				}
				carve = append(carve, p+"/*/"+r)
				continue
			}
			glob := strings.HasSuffix(s, "/*")
			base := s
			if glob {
				base = s[:len(s)-2]
			}
			p := NormPrefix(base)
			if p == "" || strings.Contains(p, "*") {
				*problems = append(*problems, Problem{fmt.Sprintf(`%s: carve-out "%s" must name a folder/file, a "<folder>/*" glob, a "<folder>/*/<name>" child glob, or a "*.suffix" pattern`, at, s0), "remove the empty/malformed entry - an empty one would carve out everything"})
				return nil, nil, false
			}
			if glob {
				p += "/*"
			}
			carve = append(carve, p)
			continue
		}
		m, ok := e.(map[string]any)
		if !ok {
			*problems = append(*problems, Problem{at + `: an "except" entry must be a carve-out string or a { path, to?, reason } object`, `use a folder string or { "path": "…", "to": "…", "reason": "…" }`})
			return nil, nil, false
		}
		if unk := unknownKeys(m, exceptionKeys); len(unk) > 0 {
			*problems = append(*problems, Problem{fmt.Sprintf("%s: exception has unknown %s %s", at, properties(len(unk)), quoted(unk)), "an exception takes only: " + strings.Join(exceptionKeys, ", ")})
			return nil, nil, false
		}
		path, ok := m["path"].(string)
		if !ok || strings.TrimSpace(path) == "" {
			*problems = append(*problems, Problem{at + `: an exception needs a string "path"`, `name the file with the deliberate crossing (or a subtree with a trailing "/")`})
			return nil, nil, false
		}
		var to []string
		if tv, has := m["to"]; has {
			list, ok := stringList(tv)
			bad := !ok || len(list) == 0
			for _, t := range list {
				if NormPrefix(t) == "" || strings.Contains(t, "*") {
					bad = true
				}
			}
			if bad {
				*problems = append(*problems, Problem{fmt.Sprintf(`%s: exception "%s" has a malformed "to"`, at, path), `omit "to" to excuse the whole file, or name the barred folder(s) it may reference — no wildcards`})
				return nil, nil, false
			}
			to = make([]string, len(list))
			for i, t := range list {
				to[i] = NormPrefix(t)
			}
		}
		reason, ok := m["reason"].(string)
		if !ok || strings.TrimSpace(reason) == "" {
			*problems = append(*problems, Problem{fmt.Sprintf(`%s: exception "%s" has no reason`, at, path), `add a non-empty "reason" — the reason string is what makes an accepted crossing reviewable`})
			return nil, nil, false
		}
		exceptions = append(exceptions, Exception{Path: strings.ReplaceAll(path, `\`, "/"), To: to, Reason: reason})
	}
	return carve, exceptions, true
}

func edgeProblem(e Edge, at string) *Problem {
	root := contains(e.Froms, "")
	for _, a := range e.Allow {
		if a == "" || strings.Contains(a, "*") {
			return &Problem{at + `: every "allow" entry must name a real folder`, "remove the empty/wildcard allow entry (it would disable or no-op the barrier), or name a shared folder"}
		}
	}
	for _, t := range e.Targets {
		if t == "*" {
			if root {
				return &Problem{at + ": isolating the repo root is meaningless", "name the folder to isolate — everything is already inside the repo root"}
			}
			if e.MatchNames {
				return &Problem{at + `: "matchNames" needs named "to" folders`, `drop "matchNames": true, or bar concrete folders instead of "*"`}
			}
			continue
		}
		glob := isGlob(t)
		tp := t
		if glob {
			tp = globPrefix(t)
		}
		if root {
			covered := false
			for _, c := range e.Carve {
				covered = covered || carveCovers(c, t)
			}
			if !covered {
				return &Problem{fmt.Sprintf(`%s: "to" (%s) overlaps the repo-root guard — the self-reference rule would exempt it`, at, t), `add the barred folder (or its "<folder>/*" glob) to "except" so it leaves the guarded region, or guard specific folders instead of "."`}
			}
		}
		for _, fp := range e.Froms {
			if fp == "" {
				continue
			}
			var overlap bool
			if glob {
				overlap = fp == tp || Under(tp, fp)
			} else {
				overlap = fp == t || Under(t, fp) || Under(fp, t)
			}
			if overlap {
				return &Problem{fmt.Sprintf(`%s: "from" (%s) and "to" (%s) overlap`, at, fp, tp), "a folder cannot be barred from itself or an ancestor — pick disjoint folders"}
			}
		}
	}
	return nil
}

// Normalize reads a list of barrier declarations.
func Normalize(specs any) ([]Edge, []Problem) {
	var edges []Edge
	var problems []Problem
	list, ok := specs.([]any)
	if !ok {
		return nil, []Problem{{"barrier rules must be an array", `set "rules" to an array of barrier entries`}}
	}
	for i, raw := range list {
		at := fmt.Sprintf("barrier rule #%d", i+1)
		spec, ok := raw.(map[string]any)
		if !ok {
			problems = append(problems, Problem{at + " must be an object", `use { "from": "...", "to": "..." } or { "between": ["a","b"] }`})
			continue
		}
		if unk := unknownKeys(spec, edgeKeys); len(unk) > 0 {
			problems = append(problems, Problem{fmt.Sprintf("%s has unknown %s %s", at, properties(len(unk)), quoted(unk)), "a barrier rule takes only: " + strings.Join(edgeKeys, ", ")})
			continue
		}
		var allow []string
		if av, has := spec["allow"]; has {
			l, ok := stringList(av)
			if !ok {
				problems = append(problems, Problem{at + `: "allow" must be a folder path or an array of them`, `use "allow": "shared" or "allow": ["shared", "contracts"]`})
				continue
			}
			for _, a := range l {
				allow = append(allow, NormPrefix(a))
			}
		}
		ev, hasExcept := spec["except"]
		carve, exceptions, ok := parseExcept(ev, hasExcept, at, &problems)
		if !ok {
			continue
		}
		matchNames := false
		if mv, has := spec["matchNames"]; has {
			b, ok := mv.(bool)
			if !ok {
				problems = append(problems, Problem{at + `: "matchNames" must be true or false`, `set "matchNames": true to also match the bare names of barred folders`})
				continue
			}
			matchNames = b
		}
		var also []string
		if av, has := spec["alsoMatchNames"]; has {
			l, ok := stringList(av)
			if !ok {
				problems = append(problems, Problem{at + `: "alsoMatchNames" must be a bare folder name or an array of them`, `use "alsoMatchNames": ["someword"] — bare folder names, no paths`})
				continue
			}
			also = l
		}
		bad := false
		for _, n := range also {
			if strings.TrimSpace(n) == "" || strings.ContainsAny(n, `\/*`) {
				bad = true
			}
		}
		if bad {
			problems = append(problems, Problem{at + `: every "alsoMatchNames" entry must be a bare folder name`, "name the barred folder itself (its last path segment), without separators or wildcards"})
			continue
		}
		if len(also) > 0 && !matchNames {
			problems = append(problems, Problem{at + `: "alsoMatchNames" requires "matchNames": true`, `set "matchNames": true — alsoMatchNames only extends the bare-name layer`})
			continue
		}
		unique := true
		if uv, has := spec["matchUniqueFilenames"]; has {
			b, ok := uv.(bool)
			if !ok {
				problems = append(problems, Problem{at + `: "matchUniqueFilenames" must be true or false`, `set "matchUniqueFilenames": false to stop resolving a bare filename that only one folder carries (a convention marker)`})
				continue
			}
			unique = b
		}
		scope := "all"
		if sv, has := spec["scope"]; has {
			if sv != "imports" && sv != "all" {
				problems = append(problems, Problem{at + `: "scope" must be "imports" or "all"`, `set "scope": "imports" to match only import/require specifiers in code files, or drop it for every resolvable reference`})
				continue
			}
			scope = sv.(string)
		}
		if scope == "imports" && matchNames {
			problems = append(problems, Problem{at + `: "matchNames" cannot combine with "scope": "imports"`, "the names layer matches arbitrary text, which an imports-scoped edge excludes by definition — drop one of the two"})
			continue
		}
		siblings := ""
		if sv, has := spec["siblings"]; has {
			s, ok := sv.(string)
			if !ok || strings.TrimSpace(s) == "" || strings.Contains(s, "*") {
				problems = append(problems, Problem{at + `: "siblings" must name a real folder`, `use "siblings": "<folder>" — each of its direct child directories is guarded in turn; no wildcards`})
				continue
			}
			_, hasFrom := spec["from"]
			_, hasBetween := spec["between"]
			if hasFrom || hasBetween {
				problems = append(problems, Problem{at + `: "siblings" cannot combine with "from" or "between"`, `a siblings edge derives its guarded folders from the named folder's children — drop "from"/"between", or drop "siblings"`})
				continue
			}
			siblings = NormPrefix(s)
			if siblings == "" {
				problems = append(problems, Problem{at + `: "siblings" must name a real subfolder, not the repo root`, "name the folder whose child directories should be mutually guarded"})
				continue
			}
		}
		reason, _ := spec["reason"].(string)
		mk := func(froms, targets []string) Edge {
			return Edge{Froms: froms, Targets: targets, Siblings: siblings, Scope: scope, Allow: allow, Carve: carve, Exceptions: exceptions,
				MatchNames: matchNames, AlsoMatchNames: also, MatchUniqueFilenames: unique, Reason: reason}
		}
		if between, ok := spec["between"].([]any); ok {
			pair, ok := stringList(between)
			bad := !ok || len(pair) != 2
			for _, s := range pair {
				if strings.TrimSpace(s) == "" || strings.Contains(s, "*") {
					bad = true
				}
			}
			if bad {
				problems = append(problems, Problem{at + ` "between" must be two folder paths`, `use "between": ["clientDir", "serverDir"] — no wildcards`})
				continue
			}
			a, b := NormPrefix(pair[0]), NormPrefix(pair[1])
			if a == "" || b == "" {
				problems = append(problems, Problem{at + ` "between" must name real subfolders`, `use "between": ["clientDir", "serverDir"] (not "", ".", or "/")`})
				continue
			}
			e1, e2 := mk([]string{a}, []string{b}), mk([]string{b}, []string{a})
			p := edgeProblem(e1, at)
			if p == nil {
				p = edgeProblem(e2, at)
			}
			if p != nil {
				problems = append(problems, *p)
				continue
			}
			edges = append(edges, e1, e2)
			continue
		}
		var froms []string
		if siblings == "" {
			f, ok := parseFrom(spec["from"], at, &problems)
			if !ok {
				continue
			}
			froms = f
		}
		var toRaw []string
		switch tv := spec["to"].(type) {
		case string:
			if strings.TrimSpace(tv) != "" {
				toRaw = []string{tv}
			}
		case []any:
			l, ok := stringList(tv)
			good := ok && len(l) > 0
			for _, t := range l {
				if strings.TrimSpace(t) == "" {
					good = false
				}
			}
			if good {
				toRaw = l
			}
		}
		if toRaw == nil {
			problems = append(problems, Problem{at + ` needs a "to" folder path (or an array of them), or a "between" pair`, `add a "to" folder path, or use "between": [a, b]`})
			continue
		}
		var targets []string
		bad = false
		for _, t := range toRaw {
			if t == "*" {
				targets = append(targets, "*")
				continue
			}
			s := strings.ReplaceAll(t, `\`, "/")
			glob := strings.HasSuffix(s, "/*")
			base := s
			if glob {
				base = s[:len(s)-2]
			}
			p := NormPrefix(base)
			if p == "" || strings.Contains(p, "*") {
				problems = append(problems, Problem{at + `: "to" must name a real subfolder, a "<folder>/*" glob, or "*"`, `name the barred folder, "<folder>/*" for its child directories, or "*" for isolation`})
				bad = true
				break
			}
			if glob {
				p += "/*"
			}
			targets = append(targets, p)
		}
		if bad {
			continue
		}
		if contains(targets, "*") && len(targets) > 1 {
			problems = append(problems, Problem{at + `: "*" cannot be combined with other "to" entries`, `isolation ("*") already bars everything — drop the other entries or the "*"`})
			continue
		}
		e := mk(froms, targets)
		if p := edgeProblem(e, at); p != nil {
			problems = append(problems, *p)
			continue
		}
		edges = append(edges, e)
	}
	return edges, problems
}
