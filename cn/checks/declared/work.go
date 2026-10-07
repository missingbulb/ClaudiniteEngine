package declared

import (
	"encoding/json"
	"strings"
)

type fileLine struct {
	file string
	line int
	text string
}

func (c *Ctx) addedLinesOf(files []string) []fileLine {
	var out []fileLine
	for _, f := range files {
		for _, l := range c.AddedLines(f) {
			out = append(out, fileLine{f, l.Line, l.Text})
		}
	}
	return out
}

func (c *Ctx) removedLinesOf(files []string) []fileLine {
	var out []fileLine
	for _, f := range files {
		for _, l := range c.RemovedLines(f) {
			out = append(out, fileLine{f, l.Line, l.Text})
		}
	}
	return out
}

func parseJSON(text string, ok bool) any {
	if !ok {
		return nil
	}
	var v any
	if json.Unmarshal([]byte(text), &v) != nil {
		return nil
	}
	return v
}

// workFindings are a work check's assertions over the change.
func workFindings(c *Check, ctx *Ctx) []hit {
	s := c.Spec
	var out []hit
	push := func(file string, line int, a map[string]any, vars map[string]any) {
		out = append(out, hit{File: file, Line: line, What: fill(get(a, "what"), vars), Fix: fill(get(a, "fix"), vars)})
	}
	for _, a := range items(s["checkBranchCommits"]) {
		if truthy(a["unlessOnDefaultBranch"]) {
			if b := ctx.Branch(); b == "main" || b == "master" {
				continue
			}
		}
		commits := ctx.Commits()
		found := len(commits) == 0
		for _, m := range commits {
			if re(a["someMessageMatches"]).Test(m) {
				found = true
				break
			}
		}
		if found {
			continue
		}
		var base any = ctx.BaseRef
		if ctx.BaseRef == "" {
			base = nil
		}
		push("(branch)", 0, a, map[string]any{"commits": float64(len(commits)), "base": base})
	}
	if merges, ok := s["forbidIntroducedMergeCommits"].(map[string]any); ok && truthy(s["forbidIntroducedMergeCommits"]) {
		for _, m := range ctx.IntroducedMerges() {
			branch := ctx.Branch()
			if branch == "" {
				branch = "HEAD"
			}
			push(branch+"@"+m.Sha, 0, merges, map[string]any{"sha": m.Sha, "subject": m.Subject})
		}
	}
	for _, a := range items(s["forbidAddedValueInArray"]) {
		var paths []string
		if f, ok := a["file"]; ok {
			paths = []string{jsString(f)}
		} else {
			r, where := re(a["filesMatching"]), re(a["whereFileContains"])
			for _, f := range ctx.Tracked {
				if r.Test(f) && (where == nil || where.Test(ctx.read(f))) {
					paths = append(paths, f)
				}
			}
		}
		for _, p := range paths {
			head := parseJSON(ctx.Read(p))
			if head == nil {
				continue
			}
			base := parseJSON(ctx.ReadBase(p))
			valuesAt := func(doc any) []string {
				var vs []string
				for _, field := range arr(a["atFields"]) {
					if doc == nil {
						continue
					}
					if l, ok := fieldAt(doc, jsString(field)).([]any); ok {
						for _, v := range l {
							vs = append(vs, jsString(v))
						}
					}
				}
				return vs
			}
			before := map[string]bool{}
			for _, v := range valuesAt(base) {
				before[v] = true
			}
			done := map[string]bool{}
			for _, v := range valuesAt(head) {
				if done[v] || before[v] {
					continue
				}
				done[v] = true
				push(p, 0, a, map[string]any{"value": v})
			}
		}
	}
	inScope := func(a map[string]any) []string {
		var out []string
		for _, f := range ctx.ChangedFiles() {
			if re(a["inFilesMatching"]).Test(f) && !c.excludedPath(f) {
				out = append(out, f)
			}
		}
		return out
	}
	for _, a := range items(s["forbidAddedLinesMatching"]) {
		for _, l := range ctx.addedLinesOf(inScope(a)) {
			m := re(a["match"]).Exec(l.text)
			if m == nil || re(a["unlessLineMatches"]).Test(l.text) {
				continue
			}
			push(l.file, l.line, a, merge(m.groupVars(), map[string]any{"match": m.Text, "path": l.file, "line": float64(l.line)}))
		}
	}
	for _, a := range items(s["forbidRemovedLinesMatching"]) {
		for _, l := range ctx.removedLinesOf(inScope(a)) {
			m := re(a["match"]).Exec(l.text)
			if m == nil || re(a["unlessLineMatches"]).Test(l.text) {
				continue
			}
			if truthy(a["unlessMatchRemainsInFile"]) && strings.Contains(ctx.read(l.file), m.Text) {
				continue
			}
			push(l.file, l.line, a, merge(m.groupVars(), map[string]any{"match": m.Text, "path": l.file, "line": float64(l.line)}))
		}
	}
	for _, a := range items(s["requireCoChange"]) {
		changed := ctx.ChangedFiles()
		done := false
		for _, f := range changed {
			if re(a["requireChangedFileMatching"]).Test(f) {
				done = true
				break
			}
		}
		if done {
			continue
		}
		var triggering []string
		if r := re(a["whenChangedFileMatches"]); r != nil && truthy(a["whenChangedFileMatches"]) {
			for _, f := range changed {
				if r.Test(f) {
					triggering = append(triggering, f)
				}
			}
		} else {
			w := obj(a["whenAddedLineMatches"])
			var in []string
			for _, f := range changed {
				if re(w["inFilesMatching"]).Test(f) {
					in = append(in, f)
				}
			}
			seen := map[string]bool{}
			for _, l := range ctx.addedLinesOf(in) {
				if re(w["match"]).Test(l.text) && !seen[l.file] {
					seen[l.file] = true
					triggering = append(triggering, l.file)
				}
			}
		}
		for _, f := range triggering {
			push(f, 0, a, map[string]any{"path": f})
		}
	}
	for _, a := range items(s["flagUntrackedFilesMatching"]) {
		for _, f := range ctx.Untracked {
			if !re(a["match"]).Test(f) || c.excludedPath(f) {
				continue
			}
			push(f, 0, a, map[string]any{"path": f})
		}
	}
	return out
}

func (c *Check) excludedPath(path string) bool {
	for _, e := range c.excludeMatchers {
		if excludedByOne(path, e) {
			return true
		}
	}
	return false
}
