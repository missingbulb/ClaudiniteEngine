package declared

import (
	"math"
	"strings"
	"time"
)

func columnOf(line string) int {
	for i, c := range []rune(line) {
		if !isJSSpace(c) {
			return i
		}
	}
	return -1
}

func blockBelow(lines []string, i int) []string {
	opener := columnOf(lines[i])
	var out []string
	for j := i + 1; j < len(lines); j++ {
		col := columnOf(lines[j])
		if col != -1 && col <= opener {
			break
		}
		out = append(out, lines[j])
	}
	return out
}

func enclosedBy(lines []string, i int, r *Regex) bool {
	inner := columnOf(lines[i])
	if inner < 1 {
		return false
	}
	for j := i - 1; j >= 0; j-- {
		col := columnOf(lines[j])
		if col == -1 || col >= inner {
			continue
		}
		if r.Test(lines[j]) {
			return true
		}
		inner = col
		if inner == 0 {
			return false
		}
	}
	return false
}

func anyMatch(lines []string, r *Regex) bool {
	for _, l := range lines {
		if r.Test(l) {
			return true
		}
	}
	return false
}

// matchOrEmpty is String.prototype.match: an absent pattern matches the
// empty string.
func matchOrEmpty(r *Regex, s string) *Match {
	if r == nil {
		return &Match{Groups: map[string]string{}}
	}
	return r.Exec(s)
}

type lineJob struct {
	j         *job
	eligible  []map[string]any
	viewLines []string
}

func (w *sweeper) visit(subs []*job, path, text string, roles map[*job]role) {
	ctx := w.ctx
	fenced := fileClasses["markdownFiles"].Test(path)
	viewKey := func(j *job) string {
		k := ""
		if truthy(j.spec["scanIgnoringComments"]) {
			k += "c"
		}
		if truthy(j.spec["scanIgnoringMarkdownFences"]) && fenced {
			k += "f"
		}
		return k
	}
	textViews := map[string]string{}
	lineViews := map[string][]string{}
	textForKey := func(k string) string {
		if t, ok := textViews[k]; ok {
			return t
		}
		t := text
		if strings.Contains(k, "c") {
			t = stripComments(t)
		}
		if strings.Contains(k, "f") {
			t = blankMarkdownFences(t)
		}
		textViews[k] = t
		return t
	}
	linesForKey := func(k string) []string {
		if l, ok := lineViews[k]; ok {
			return l
		}
		l := strings.Split(textForKey(k), "\n")
		lineViews[k] = l
		return l
	}
	textFor := func(j *job) string { return textForKey(viewKey(j)) }
	linesFor := func(j *job) []string { return linesForKey(viewKey(j)) }
	rawLines := func() []string { return linesForKey("") }
	var md *markdownDoc
	mdDoc := func() *markdownDoc {
		if md == nil {
			md = newMarkdownDoc(text)
		}
		return md
	}
	var lineJobs []lineJob
	for _, j := range subs {
		s := j.spec
		r := role{scanning: true}
		if roles != nil {
			r = roles[j]
		}
		if r.collecting {
			for _, col := range j.collectors {
				pm := re(col.s["inFilesMatching"]).Exec(path)
				if pm == nil {
					continue
				}
				view := linesFor(j)
				for i, l := range view {
					collectLine(col.s, l, path, i+1, pm.groupVars(), col.add)
				}
			}
		}
		if !r.scanning {
			continue
		}
		if has(s, "checkSections") {
			w.assertSections(j, path, mdDoc)
		}
		if ml, ok := s["maxLines"].(map[string]any); ok {
			limit := num(ml["limit"])
			if n := len(rawLines()); float64(n) > limit {
				j.push(path, int(limit)+1, get(ml, "what"), get(ml, "fix"), map[string]any{"lines": float64(n), "limit": ml["limit"]})
			}
		}
		if mll, ok := s["maxLineLength"].(map[string]any); ok {
			limit := num(mll["bytes"])
			count, first, longest := 0, 0, 0
			for i, ln := range linesFor(j) {
				if float64(len(ln)) > limit {
					count++
					if first == 0 {
						first = i + 1
					}
					if len(ln) > longest {
						longest = len(ln)
					}
				}
			}
			if count > 0 {
				j.push(path, first, get(mll, "what"), get(mll, "fix"), map[string]any{"count": float64(count), "bytes": mll["bytes"], "longest": float64(longest)})
			}
		}
		for _, a := range items(s["countMatchingLines"]) {
			view := linesFor(j)
			count, overflowAt := 0, 0
			atMost, hasMost := a["atMost"]
			for i, l := range view {
				if !re(a["linesMatching"]).Test(l) {
					continue
				}
				count++
				if overflowAt == 0 && hasMost && float64(count) == num(atMost)+1 {
					overflowAt = i + 1
				}
			}
			atLeast, hasLeast := a["atLeast"]
			under := hasLeast && float64(count) < num(atLeast)
			if !under && overflowAt == 0 {
				continue
			}
			vars := map[string]any{"count": float64(count)}
			if hasLeast {
				vars["atLeast"] = atLeast
			}
			if hasMost {
				vars["atMost"] = atMost
			}
			line := overflowAt
			if under {
				line = 0
			}
			j.push(path, line, get(a, "what"), get(a, "fix"), vars)
		}
		for _, a := range items(s["checkEachFile"]) {
			if rw, ok := a["relevantWhen"]; ok && truthy(rw) && !w.relevant(rw) {
				continue
			}
			all := true
			for _, m := range arr(a["whenFileMatches"]) {
				if !re(m).Test(textFor(j)) {
					all = false
					break
				}
			}
			if !all {
				continue
			}
			var bad bool
			if f := re(a["forbid"]); f != nil && truthy(a["forbid"]) {
				bad = f.Test(textFor(j))
			} else {
				bad = !re(a["require"]).Test(textFor(j))
			}
			if bad {
				j.out = append(j.out, hit{File: path, What: jsString(get(a, "what")), Fix: jsString(get(a, "fix"))})
			}
		}
		for _, st := range j.repoStates {
			if re(st.a["unlessSomeFileMatches"]).Test(textFor(j)) {
				st.satisfied = true
			}
			if excluded(path, get(st.a, "neverFlagFiles")) {
				continue
			}
			for _, g := range arr(st.a["flagFilesMatching"]) {
				group := arr(g)
				ok := true
				for _, r := range group {
					if !re(r).Test(textFor(j)) {
						ok = false
						break
					}
				}
				if !ok {
					continue
				}
				at := 0
				if len(group) > 0 {
					for i, ln := range linesFor(j) {
						if re(group[0]).Test(ln) {
							at = i + 1
							break
						}
					}
				}
				st.hits = append(st.hits, hit{File: path, Line: at, What: jsString(get(st.a, "what")), Fix: jsString(get(st.a, "fix"))})
				break
			}
		}
		var eligible []map[string]any
		for _, a := range items(s["matchLines"]) {
			if wp := re(a["whenPathMatches"]); wp != nil && !wp.Test(path) {
				continue
			}
			ok := true
			for _, m := range arr(a["whenFileMatches"]) {
				if !re(m).Test(textFor(j)) {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
			if uf := re(a["unlessFileMatches"]); uf != nil && uf.Test(textFor(j)) {
				continue
			}
			eligible = append(eligible, a)
		}
		if len(eligible) > 0 {
			lineJobs = append(lineJobs, lineJob{j, eligible, linesFor(j)})
		}
	}
	if len(lineJobs) == 0 {
		return
	}
	n := len(rawLines())
	for i := 0; i < n; i++ {
		for _, lj := range lineJobs {
			if i >= len(lj.viewLines) {
				continue
			}
			ln := lj.viewLines[i]
			if skip := re(lj.j.spec["skipLinesMatching"]); skip != nil && skip.Test(ln) {
				continue
			}
			for _, a := range lj.eligible {
				m := matchOrEmpty(re(a["match"]), ln)
				if m == nil {
					continue
				}
				if r := re(a["andLineMatches"]); r != nil && !r.Test(ln) {
					continue
				}
				if r := re(a["unlessLineMatches"]); r != nil && r.Test(ln) {
					continue
				}
				if r := re(a["unlessPreviousLineMatches"]); r != nil && i > 0 && r.Test(lj.viewLines[i-1]) {
					continue
				}
				if r := re(a["andIndentedBlockBelowMatches"]); r != nil && !anyMatch(blockBelow(lj.viewLines, i), r) {
					continue
				}
				if r := re(a["unlessIndentedBlockBelowMatches"]); r != nil && anyMatch(blockBelow(lj.viewLines, i), r) {
					continue
				}
				if r := re(a["andWithinBlockOpenedBy"]); r != nil && !enclosedBy(lj.viewLines, i, r) {
					continue
				}
				if r := re(a["unlessWithinBlockOpenedBy"]); r != nil && enclosedBy(lj.viewLines, i, r) {
					continue
				}
				lj.j.push(path, i+1, get(a, "what"), get(a, "fix"), map[string]any{"match": m.Text})
				break
			}
		}
	}
	_ = ctx
}

var (
	mdBullet       = mustRegex(`^[-*+]\s`, "")
	mdDated        = mustRegex(`^[-*+]\s+(?:\*\*)?(\d{4})-(\d{2})-(\d{2})(?:\*\*)?\b`, "")
	mdContinuation = mustRegex(`^\s+\S`, "")
)

const dayMs = 86_400_000

// realDateUTC is the date's instant in milliseconds, or false when the
// digits name no real day.
func realDateUTC(y, mo, d int) (int64, bool) {
	t := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC)
	if t.Year() != y || int(t.Month()) != mo || t.Day() != d {
		return 0, false
	}
	return t.UnixMilli(), true
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

func excerpt(s string) string {
	r := []rune(strings.TrimFunc(s, isJSSpace))
	if len(r) > 80 {
		r = r[:80]
	}
	return string(r)
}

func (w *sweeper) assertSections(j *job, path string, md func() *markdownDoc) {
	now := w.ctx.Now.UnixMilli()
	push := func(a any, at int, vars map[string]any) {
		m := obj(a)
		j.push(path, at, get(m, "what"), get(m, "fix"), vars)
	}
	for _, entry := range items(j.spec["checkSections"]) {
		var names []string
		if l, ok := entry["sections"].([]any); ok {
			for _, n := range l {
				names = append(names, jsString(n))
			}
		} else {
			names = []string{jsString(get(entry, "section"))}
		}
		for _, name := range names {
			body, ok := md().section(name)
			vars := map[string]any{"section": name}
			if !ok {
				if truthy(entry["requirePresent"]) {
					push(entry["requirePresent"], 0, vars)
				}
				continue
			}
			if truthy(entry["requireFirstOnPage"]) {
				if first := md().firstHeading; first != nil && !mustRegex(`^`+escapeRe(name)+`\b`, "i").Test(*first) {
					push(entry["requireFirstOnPage"], 0, merge(vars, map[string]any{"first": *first}))
				}
			}
			bullets := 0
			var dated []int64
			for i := 0; i < len(body); i++ {
				line, t := body[i].line, body[i].text
				if !mdBullet.Test(t) {
					if truthy(entry["forbidProseLines"]) && strings.TrimFunc(t, isJSSpace) != "" && !mdContinuation.Test(t) {
						push(entry["forbidProseLines"], line, merge(vars, map[string]any{"line": excerpt(t)}))
					}
					continue
				}
				bullets++
				m := mdDated.Exec(t)
				var y, mo, d int
				if m != nil {
					parts := mdDatedParts(t)
					y, mo, d = parts[0], parts[1], parts[2]
				}
				if ebl, ok := entry["eachBulletLeadsWithDate"].(map[string]any); ok {
					if m == nil {
						push(ebl["whenUndated"], line, merge(vars, map[string]any{"bullet": excerpt(t)}))
					} else if _, real := realDateUTC(y, mo, d); !real {
						push(ebl["whenNotRealDate"], line, merge(vars, map[string]any{"date": mdDatedText(t)}))
					}
				}
				if truthy(entry["newestDatedBulletWithinDays"]) && m != nil {
					if ts, real := realDateUTC(y, mo, d); real && ts <= now+2*dayMs {
						dated = append(dated, ts)
					}
				}
				if truthy(entry["eachBulletBlockMatches"]) || truthy(entry["maxBulletBlockLength"]) {
					block := t
					for k := i + 1; k < len(body) && mdContinuation.Test(body[k].text); k++ {
						block += " " + strings.TrimFunc(body[k].text, isJSSpace)
					}
					if ebm, ok := entry["eachBulletBlockMatches"].(map[string]any); ok && !re(ebm["pattern"]).Test(block) {
						push(ebm, line, merge(vars, map[string]any{"bullet": excerpt(t)}))
					}
					if mbl, ok := entry["maxBulletBlockLength"].(map[string]any); ok {
						n := float64(len(utf16Len(strings.TrimFunc(block, isJSSpace))))
						if n > num(mbl["characters"]) {
							push(mbl, line, merge(vars, map[string]any{"bullet": excerpt(t), "characters": n}))
						}
					}
				}
			}
			minB, hasMin := entry["minBullets"].(map[string]any)
			maxB, hasMax := entry["maxBullets"].(map[string]any)
			switch {
			case hasMin && float64(bullets) < num(minB["count"]):
				push(minB, 0, merge(vars, map[string]any{"bullets": float64(bullets)}))
			case hasMax && float64(bullets) > num(maxB["count"]):
				push(maxB, 0, merge(vars, map[string]any{"bullets": float64(bullets)}))
			}
			if nd, ok := entry["newestDatedBulletWithinDays"].(map[string]any); ok && len(dated) > 0 {
				newest := dated[0]
				for _, ts := range dated {
					if ts > newest {
						newest = ts
					}
				}
				age := math.Floor(float64(now-newest) / dayMs)
				if age > num(nd["days"]) {
					push(nd, 0, merge(vars, map[string]any{"age": age, "days": nd["days"], "date": time.UnixMilli(newest).UTC().Format("2006-01-02")}))
				}
			}
		}
	}
}

// mdDatedParts reads the year, month and day a dated bullet leads with.
func mdDatedParts(t string) [3]int {
	s := mdDatedText(t)
	return [3]int{atoi(s[0:4]), atoi(s[5:7]), atoi(s[8:10])}
}

func mdDatedText(t string) string {
	i := strings.IndexFunc(t, func(r rune) bool { return r >= '0' && r <= '9' })
	return t[i : i+10]
}

// utf16Len is a string as JavaScript counts its length.
func utf16Len(s string) []uint16 {
	var out []uint16
	for _, r := range s {
		if r >= 0x10000 {
			out = append(out, 0, 0)
		} else {
			out = append(out, 0)
		}
	}
	return out
}
