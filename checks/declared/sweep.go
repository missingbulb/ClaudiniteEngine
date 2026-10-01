package declared

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/checks/declared/refs"
	"github.com/missingbulb/ClaudiniteEngine/shared/descriptor"
)

// hit is one finding a family reported, before the run's grace and
// configuration apply.
type hit struct {
	File string
	Line int
	What string
	Why  string
	Fix  string
	// Block marks a finding that blocks whatever the check's on_fail
	// (a malformed barrier declaration).
	Block bool
}

type repoState struct {
	a         map[string]any
	satisfied bool
	hits      []hit
}

type setValue struct {
	value string
	file  string
	line  int
	vars  map[string]any
}

type collector struct {
	s   map[string]any
	add func(value, file string, line int, groups map[string]any)
}

type job struct {
	c          *Check
	spec       map[string]any
	out        []hit
	repoStates []*repoState
	named      map[string]bool
	sets       map[string][]*setValue
	collectors []collector
}

func (j *job) push(file string, line int, what, fix any, vars map[string]any) {
	j.out = append(j.out, hit{File: file, Line: line, What: fill(what, vars), Fix: fill(fix, vars)})
}

// excluded reports whether path is excluded by a pattern, an exact path
// or a list of those. A list nested in the list excludes nothing, as in
// the Node engine.
func excluded(path string, exclude any) bool {
	if l, ok := exclude.([]any); ok {
		for _, e := range l {
			if excludedByOne(path, e) {
				return true
			}
		}
		return false
	}
	return excludedByOne(path, exclude)
}

func excludedByOne(path string, e any) bool {
	switch x := e.(type) {
	case *Regex:
		return x.Test(path)
	case string:
		return path == x
	}
	return false
}

func (j *job) excluded(path string) bool {
	for _, e := range j.c.excludeMatchers {
		if excludedByOne(path, e) {
			return true
		}
	}
	return false
}

// sweeper is one pass over the tree for a set of checks.
type sweeper struct {
	ctx  *Ctx
	docs map[string]any
	refs *refs.Scan
}

func (w *sweeper) parsed(path string) any {
	if d, ok := w.docs[path]; ok {
		return d
	}
	var doc any
	if text, ok := w.ctx.Read(path); ok {
		if yamlPath.Test(path) {
			doc, _ = descriptor.ParseDocument([]byte(text), descriptor.YAML)
		} else {
			if err := json.Unmarshal([]byte(text), &doc); err != nil {
				doc = nil
			}
		}
	}
	w.docs[path] = doc
	return doc
}

var yamlPath = mustRegex(`\.ya?ml$`, "")

func (w *sweeper) relevant(when any) bool {
	m, ok := when.(map[string]any)
	if !ok || !truthy(when) {
		return true
	}
	ctx := w.ctx
	anyTracked := func(r *Regex) bool {
		for _, f := range ctx.Tracked {
			if r.Test(f) {
				return true
			}
		}
		return false
	}
	if p, ok := m["pathExists"].(string); ok && p != "" && !ctx.Exists(p) {
		return false
	}
	if p, ok := m["pathAbsent"].(string); ok && p != "" && ctx.Exists(p) {
		return false
	}
	if r := re(m["trackedFileMatches"]); r != nil && !anyTracked(r) {
		return false
	}
	if r := re(m["noTrackedFileMatches"]); r != nil && anyTracked(r) {
		return false
	}
	if r := re(m["exactlyOneTrackedFileMatches"]); r != nil {
		n := 0
		for _, f := range ctx.Tracked {
			if r.Test(f) {
				n++
			}
		}
		if n != 1 {
			return false
		}
	}
	if probe, ok := m["someTrackedFileContains"].(map[string]any); ok {
		pm, text := re(probe["pathMatching"]), re(probe["text"])
		found := false
		for _, f := range ctx.Tracked {
			if pm == nil || !pm.Test(f) {
				continue
			}
			view := ctx.read(f)
			if truthy(probe["ignoringComments"]) {
				view = stripComments(view)
			}
			if text != nil && text.Test(view) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// sweep runs every world assertion of checks over the tree and returns
// each check's hits.
func sweep(ctx *Ctx, checks []*Check) map[*Check][]hit {
	w := &sweeper{ctx: ctx, docs: map[string]any{}, refs: ctx.refs()}
	res := map[*Check][]hit{}
	var jobs []*job
	for _, c := range checks {
		res[c] = nil
		if !w.relevant(get(c.Spec, "relevantWhen")) {
			continue
		}
		j := &job{c: c, spec: c.Spec}
		for _, a := range items(c.Spec["repoWide"]) {
			j.repoStates = append(j.repoStates, &repoState{a: a})
		}
		jobs = append(jobs, j)
	}
	for _, j := range jobs {
		if j.c.namedScan != nil {
			j.named = w.namedScanSet(j)
		}
		if has(j.spec, "extractValueSets") {
			w.resolveValueSets(j)
		}
	}
	for _, j := range jobs {
		w.assertTreeShape(j)
		w.assertParsedShape(j)
		if len(j.c.edges) > 0 {
			w.assertReferenceEdges(j)
		}
		path, ok := j.spec["scanFiles"].(string)
		if !ok {
			continue
		}
		text, ok := ctx.Read(path)
		if !ok {
			if wm, ok := j.spec["whenMissing"].(map[string]any); ok {
				j.out = append(j.out, hit{File: path, What: jsString(get(wm, "what")), Fix: jsString(get(wm, "fix"))})
			}
			continue
		}
		w.visit([]*job{j}, path, text, nil)
	}
	var swept []*job
	for _, j := range jobs {
		if len(j.c.scanMatchers) > 0 || j.c.namedScan != nil || len(j.collectors) > 0 {
			swept = append(swept, j)
		}
	}
	if len(swept) > 0 {
		scanned := map[string]bool{}
		for _, f := range ctx.Files() {
			scanned[f] = true
		}
		tracked := map[string]bool{}
		for _, f := range ctx.Tracked {
			tracked[f] = true
		}
		paths := append([]string{}, ctx.Files()...)
		for _, f := range ctx.Tracked {
			if !scanned[f] {
				paths = append(paths, f)
			}
		}
		for _, path := range paths {
			roles := map[*job]role{}
			var order []*job
			for _, j := range swept {
				base := scanned
				if truthy(j.spec["scanTracked"]) {
					base = tracked
				}
				if !base[path] || j.excluded(path) {
					continue
				}
				scanning := false
				if j.named != nil {
					scanning = j.named[path]
				} else {
					for _, m := range j.c.scanMatchers {
						if m.Test(path) {
							scanning = true
							break
						}
					}
				}
				collecting := false
				for _, col := range j.collectors {
					if re(col.s["inFilesMatching"]).Test(path) {
						collecting = true
						break
					}
				}
				if scanning || collecting {
					roles[j] = role{scanning, collecting}
					order = append(order, j)
				}
			}
			if len(order) == 0 {
				continue
			}
			if text, ok := ctx.Read(path); ok {
				w.visit(order, path, text, roles)
			}
		}
	}
	for _, j := range jobs {
		if has(j.spec, "extractValueSets") {
			w.assertSetShape(j)
		}
	}
	for _, j := range jobs {
		for _, st := range j.repoStates {
			if !st.satisfied {
				j.out = append(j.out, st.hits...)
			}
		}
		if marker := re(obj(j.spec["relevantWhen"])["repoContains"]); marker != nil && len(j.out) > 0 {
			found := false
			for _, f := range ctx.Files() {
				if !j.excluded(f) && marker.Test(ctx.read(f)) {
					found = true
					break
				}
			}
			if !found {
				j.out = nil
			}
		}
		res[j.c] = j.out
	}
	return res
}

type role struct{ scanning, collecting bool }

func (w *sweeper) assertReferenceEdges(j *job) {
	rule := refs.Rule{Why: j.c.Why, SettingsPath: w.ctx.Config.SettingsPath}
	fs, stale, scanErrors := w.refs.Findings(j.c.edges, rule)
	fix, hasFix := j.spec["fix"].(string)
	for _, f := range fs {
		h := hit{File: f.File, Line: f.Line, What: f.What, Why: f.Why, Fix: f.Fix, Block: f.Spec}
		if hasFix && !f.Spec {
			h.Fix = fix
		}
		j.out = append(j.out, h)
	}
	if !scanErrors {
		for _, f := range refs.StaleFindings(stale, rule) {
			j.out = append(j.out, hit{File: f.File, What: f.What, Why: f.Why, Fix: f.Fix, Block: true})
		}
	}
}

func resolveFrom(namingFile, rel string) string {
	dir := ""
	if i := strings.LastIndex(namingFile, "/"); i >= 0 {
		dir = namingFile[:i]
	}
	var parts []string
	for _, seg := range strings.Split(dir+"/"+rel, "/") {
		switch seg {
		case "", ".":
		case "..":
			if len(parts) > 0 {
				parts = parts[:len(parts)-1]
			}
		default:
			parts = append(parts, seg)
		}
	}
	return strings.Join(parts, "/")
}

func (w *sweeper) namedScanSet(j *job) map[string]bool {
	n := j.c.namedScan
	out := map[string]bool{}
	in := re(n["inParsedFilesMatching"])
	where := re(n["whereFileContains"])
	field, _ := n["namedByField"].(string)
	suffix := ""
	if s, ok := n["withSuffix"]; ok && truthy(s) {
		suffix = jsString(s)
	}
	for _, f := range w.ctx.Tracked {
		if !in.Test(f) || j.excluded(f) || (where != nil && !where.Test(w.ctx.read(f))) {
			continue
		}
		doc := w.parsed(f)
		if doc == nil {
			continue
		}
		for _, name := range namesAtField(doc, field, get(n, "defaultingTo")) {
			out[resolveFrom(f, name+suffix)] = true
		}
	}
	return out
}

func collectLine(s map[string]any, text, file string, line int, pathGroups map[string]any, add func(string, string, int, map[string]any)) {
	pat := re(s["fromLinesMatching"])
	if pat == nil {
		pat = re(s["fromAddedLinesMatching"])
	}
	m := pat.Exec(text)
	if m == nil {
		return
	}
	v, ok := m.Groups["value"]
	if !ok {
		return
	}
	groups := merge(pathGroups, m.groupVars())
	delete(groups, "value")
	parts := []string{v}
	if sp := re(s["splitValuesOn"]); sp != nil {
		parts = sp.split(v)
	}
	for _, p := range parts {
		if p = strings.TrimFunc(p, isJSSpace); p != "" {
			add(p, file, line, groups)
		}
	}
}

func (w *sweeper) resolveValueSets(j *job) {
	ctx := w.ctx
	j.sets = map[string][]*setValue{}
	for _, s := range items(j.spec["extractValueSets"]) {
		name, _ := s["setName"].(string)
		seen := map[string]bool{}
		j.sets[name] = []*setValue{}
		add := func(value, file string, line int, groups map[string]any) {
			key := value + "\x00" + file
			if seen[key] {
				return
			}
			seen[key] = true
			vars := merge(groups, map[string]any{"value": value, "path": file})
			if line > 0 {
				vars["line"] = float64(line)
			}
			j.sets[name] = append(j.sets[name], &setValue{value, file, line, vars})
		}
		switch {
		case truthy(s["fromLinesMatching"]):
			j.collectors = append(j.collectors, collector{s, add})
		case truthy(s["fromTrackedPathsMatching"]):
			r := re(s["fromTrackedPathsMatching"])
			for _, f := range ctx.Tracked {
				m := r.Exec(f)
				if m == nil {
					continue
				}
				v, ok := m.Groups["value"]
				if !ok {
					v = f
				}
				add(v, f, 0, m.groupVars())
			}
		case truthy(s["fromAddedLinesMatching"]):
			in := re(s["inFilesMatching"])
			for _, f := range ctx.ChangedFiles() {
				pm := in.Exec(f)
				if pm == nil || j.excluded(f) {
					continue
				}
				before := map[string]bool{}
				for _, l := range ctx.RemovedLines(f) {
					collectLine(s, l.Text, f, l.Line, pm.groupVars(), func(v, _ string, _ int, _ map[string]any) { before[v] = true })
				}
				for _, l := range ctx.AddedLines(f) {
					collectLine(s, l.Text, f, l.Line, pm.groupVars(), func(v, file string, line int, g map[string]any) {
						if !before[v] {
							add(v, file, line, g)
						}
					})
				}
			}
		default:
			var docPaths []string
			if p, ok := s["fromParsedFile"]; ok {
				docPaths = []string{jsString(p)}
			} else {
				r, where := re(s["fromParsedFilesMatching"]), re(s["whereFileContains"])
				for _, f := range ctx.Tracked {
					if r.Test(f) && (where == nil || where.Test(ctx.read(f))) {
						docPaths = append(docPaths, f)
					}
				}
			}
			for _, dp := range docPaths {
				doc := w.parsed(dp)
				if doc == nil {
					continue
				}
				for _, field := range arr(s["valuesOfArraysAtFields"]) {
					if vs, ok := fieldAt(doc, jsString(field)).([]any); ok {
						for _, v := range vs {
							add(jsString(v), dp, 0, map[string]any{})
						}
					}
				}
				for _, field := range arr(s["valuesAtFields"]) {
					for _, v := range valuesAtPath(doc, jsString(field)) {
						add(v, dp, 0, map[string]any{})
					}
				}
			}
		}
	}
}

func basename(p string) string { return p[strings.LastIndex(p, "/")+1:] }

func (w *sweeper) assertTreeShape(j *job) {
	ctx := w.ctx
	for _, a := range items(j.spec["requirePaths"]) {
		p := jsString(get(a, "path"))
		if ctx.Exists(p) {
			continue
		}
		j.push(p, 0, get(a, "what"), get(a, "fix"), map[string]any{"path": p})
	}
	for _, a := range items(j.spec["forbidTrackedPathsMatching"]) {
		for _, p := range ctx.Files() {
			if !re(a["match"]).Test(p) || j.excluded(p) {
				continue
			}
			j.push(p, 0, get(a, "what"), get(a, "fix"), map[string]any{"path": p})
		}
	}
	for _, a := range items(j.spec["requireIdenticalFiles"]) {
		for _, p := range ctx.Files() {
			m := re(a["everyFileMatching"]).Exec(p)
			if m == nil || j.excluded(p) {
				continue
			}
			vars := merge(m.groupVars(), map[string]any{"path": p, "basename": basename(p)})
			twin := fill(a["twinAt"], vars)
			if twin == p {
				continue
			}
			vars["twin"] = twin
			twinText, ok := ctx.Read(twin)
			if !ok {
				if a["whenTwinAbsent"] != "assertNothing" {
					wta := obj(a["whenTwinAbsent"])
					j.push(p, 0, get(wta, "what"), get(wta, "fix"), vars)
				}
				continue
			}
			if own, _ := ctx.Read(p); twinText == own {
				continue
			}
			j.push(p, 0, get(a, "what"), get(a, "fix"), vars)
		}
	}
	var entries []map[string]any
	for _, a := range items(j.spec["requireIndexCoverage"]) {
		if !has(a, "eachValueOfSet") {
			entries = append(entries, a)
		}
	}
	w.coverageEntries(j, entries)
}

func (w *sweeper) coverageEntries(j *job, entries []map[string]any) {
	ctx := w.ctx
	for _, a := range entries {
		indexFile := jsString(get(a, "indexFile"))
		indexText, indexOK := ctx.Read(indexFile)
		if !indexOK && a["whenIndexFileAbsent"] == "assertNothing" {
			continue
		}
		var globs []*Regex
		if gl := re(a["coveredByGlobLinesMatching"]); gl != nil {
			for _, line := range strings.Split(indexText, "\n") {
				if gl.Test(line) && !strings.HasPrefix(strings.TrimFunc(line, isJSSpace), "#") {
					first := ""
					if f := strings.FieldsFunc(line, isJSSpace); len(f) > 0 {
						first = f[0]
					}
					globs = append(globs, globToRe(first))
				}
			}
		}
		type subject struct {
			vars       map[string]any
			anchorPath string
			anchorLine int
			globPath   string
		}
		var subjects []subject
		if setName, ok := a["eachValueOfSet"]; ok {
			for _, v := range j.sets[jsString(setName)] {
				subjects = append(subjects, subject{v.vars, v.file, v.line, ""})
			}
		} else {
			matcher := re(a["eachTrackedPathMatching"])
			paths := ctx.Tracked
			if matcher == nil {
				matcher = re(a["eachScannedPathMatching"])
				paths = ctx.Files()
				if truthy(a["includeVendored"]) {
					paths = ctx.AllFiles()
				}
			}
			for _, p := range paths {
				m := matcher.Exec(p)
				if m == nil {
					continue
				}
				if wt := re(a["whoseTextMatches"]); wt != nil {
					text := ctx.read(p)
					if truthy(j.spec["scanIgnoringComments"]) {
						text = stripComments(text)
					}
					if !wt.Test(text) {
						continue
					}
				}
				subjects = append(subjects, subject{merge(map[string]any{"path": p}, m.groupVars()), p, 0, p})
			}
		}
		atIndex := map[string]map[string]any{}
		for _, s := range subjects {
			var covered bool
			var key string
			switch {
			case has(a, "coveredByText"):
				token := fill(a["coveredByText"], s.vars)
				covered = indexOK && strings.Contains(indexText, token)
				key = token
			case has(a, "coveredByValueInArrayAtField"):
				mem := obj(a["coveredByValueInArrayAtField"])
				sought := fill(get(mem, "value"), s.vars)
				covered = arrayHoldsValue(fieldAt(w.parsed(indexFile), jsString(get(mem, "atField"))), mem, sought)
				key = sought
			default:
				base := basename(s.globPath)
				for _, g := range globs {
					if g.Test(s.globPath) || g.Test(base) {
						covered = true
						break
					}
				}
				key = s.globPath
			}
			if covered {
				continue
			}
			if a["anchorFindingsAt"] == "indexFile" {
				if _, ok := atIndex[key]; !ok {
					atIndex[key] = s.vars
				}
				continue
			}
			j.push(s.anchorPath, s.anchorLine, get(a, "what"), get(a, "fix"), s.vars)
		}
		keys := make([]string, 0, len(atIndex))
		for k := range atIndex {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			j.push(indexFile, 0, get(a, "what"), get(a, "fix"), atIndex[k])
		}
	}
}

// fillPattern fills a /pattern/flags template with vars, each value
// escaped unless raw names it, and compiles it.
func fillPattern(template string, vars map[string]any, raw map[string]bool) *Regex {
	body, flags, _ := parseForm(template)
	filled := replaceVars(body, func(key, whole string) string {
		v, ok := vars[key]
		if !ok {
			return whole
		}
		s := jsString(v)
		if !raw[key] {
			return escapeRe(s)
		}
		if b, _, ok := parseForm(s); ok {
			return b
		}
		return s
	})
	r, err := compileRegex(filled, flags)
	if err != nil {
		panic(timeoutError{fmt.Errorf("the template %s filled with %v is not a valid regex — %v", template, vars, err)})
	}
	return r
}

func (w *sweeper) view(j *job, path string) string {
	text := w.ctx.read(path)
	if truthy(j.spec["scanIgnoringComments"]) {
		text = stripComments(text)
	}
	if truthy(j.spec["scanIgnoringMarkdownFences"]) && fileClasses["markdownFiles"].Test(path) {
		text = blankMarkdownFences(text)
	}
	return text
}

func (w *sweeper) assertSetShape(j *job) {
	ctx := w.ctx
	at := func(file string, line int, vars map[string]any, a map[string]any) {
		j.push(file, line, get(a, "what"), get(a, "fix"), vars)
	}
	for _, src := range items(j.spec["extractValueSets"]) {
		name := jsString(src["setName"])
		if src["whenSetEmpty"] == "assertNothing" || len(j.sets[name]) > 0 {
			continue
		}
		selector := re(src["fromParsedFilesMatching"])
		if selector == nil {
			selector = re(src["inFilesMatching"])
		}
		if selector == nil {
			selector = re(src["fromTrackedPathsMatching"])
		}
		file := ""
		if p, ok := src["fromParsedFile"]; ok {
			file = jsString(p)
		} else {
			for _, f := range ctx.Tracked {
				if selector != nil && selector.Test(f) {
					file = f
					break
				}
			}
			if file == "" {
				file = "(repo)"
			}
		}
		wse := obj(src["whenSetEmpty"])
		j.push(file, 0, get(wse, "what"), get(wse, "fix"), map[string]any{"setName": name, "path": file})
	}
	var entries []map[string]any
	for _, a := range items(j.spec["requireIndexCoverage"]) {
		if has(a, "eachValueOfSet") {
			entries = append(entries, a)
		}
	}
	w.coverageEntries(j, entries)
	for _, a := range items(j.spec["checkSetValues"]) {
		raw := map[string]bool{}
		if truthy(a["valueIsPattern"]) {
			raw["value"] = true
		}
		values := j.sets[jsString(a["setName"])]
		tpl, isTpl := a["requireSomeFileMatching"].(map[string]any)
		require := isTpl
		if !isTpl {
			tpl, isTpl = a["forbidEveryFileMatching"].(map[string]any)
		}
		switch {
		case isTpl:
			type pend struct {
				v      *setValue
				textRe *Regex
			}
			var order []string
			groups := map[string][]*pend{}
			pathRes := map[string]*Regex{}
			for _, v := range values {
				pathRe := fillPattern(jsString(tpl["pathMatching"]), v.vars, raw)
				textRe := fillPattern(jsString(tpl["text"]), v.vars, raw)
				key := pathRe.Source + "\x00" + pathRe.Flags
				if _, ok := groups[key]; !ok {
					order = append(order, key)
					pathRes[key] = pathRe
				}
				groups[key] = append(groups[key], &pend{v, textRe})
			}
			for _, key := range order {
				pending := groups[key]
				type found struct {
					file string
					line int
				}
				hits := map[*pend]found{}
				for _, file := range ctx.Files() {
					if !pathRes[key].Test(file) || j.excluded(file) {
						continue
					}
					text := w.view(j, file)
					for _, p := range pending {
						if _, ok := hits[p]; ok {
							continue
						}
						if m := p.textRe.Exec(text); m != nil {
							hits[p] = found{file, lineOf(text, m.Index)}
						}
					}
					if len(hits) == len(pending) {
						break
					}
				}
				for _, p := range pending {
					h, ok := hits[p]
					if require {
						if !ok {
							at(p.v.file, p.v.line, p.v.vars, a)
						}
						continue
					}
					if ok {
						vars := merge(p.v.vars, map[string]any{"path": h.file, "line": float64(h.line), "source": p.v.file})
						if p.v.line > 0 {
							vars["sourceLine"] = float64(p.v.line)
						} else {
							vars["sourceLine"] = nil
						}
						at(h.file, h.line, vars, a)
					}
				}
			}
		case has(a, "requirePathExists"):
			for _, v := range values {
				target := fill(a["requirePathExists"], v.vars)
				if !ctx.Exists(target) {
					at(v.file, v.line, merge(v.vars, map[string]any{"target": target}), a)
				}
			}
		default:
			for _, v := range values {
				r := fillPattern(jsString(a["requireTrackedPathMatching"]), v.vars, raw)
				found := false
				for _, f := range ctx.Tracked {
					if r.Test(f) {
						found = true
						break
					}
				}
				if !found {
					at(v.file, v.line, v.vars, a)
				}
			}
		}
	}
	for _, a := range items(j.spec["checkSetPairs"]) {
		otherName := a["mustAlsoBeIn"]
		also := has(a, "mustAlsoBeIn")
		if !also {
			otherName = a["mustNotBeIn"]
		}
		index := map[string]*setValue{}
		for _, v := range j.sets[jsString(otherName)] {
			if _, ok := index[v.value]; !ok {
				index[v.value] = v
			}
		}
		for _, v := range j.sets[jsString(a["everyValueOf"])] {
			match, ok := index[v.value]
			if also == ok {
				continue
			}
			vars := v.vars
			if ok {
				vars = merge(v.vars, map[string]any{"other": match.file})
			}
			at(v.file, v.line, vars, a)
		}
	}
}

func (w *sweeper) scanPaths(j *job) []string {
	if p, ok := j.spec["scanFiles"].(string); ok {
		return []string{p}
	}
	base := w.ctx.Files()
	if truthy(j.spec["scanTracked"]) {
		base = w.ctx.Tracked
	}
	var out []string
	for _, p := range base {
		match := false
		if j.named != nil {
			match = j.named[p]
		} else {
			for _, m := range j.c.scanMatchers {
				if m.Test(p) {
					match = true
					break
				}
			}
		}
		if match && !j.excluded(p) {
			out = append(out, p)
		}
	}
	return out
}

func (w *sweeper) assertParsedShape(j *job) {
	ctx := w.ctx
	for _, a := range items(j.spec["checkParsedFiles"]) {
		var paths []string
		switch {
		case truthy(a["everyScannedFile"]):
			paths = w.scanPaths(j)
		case has(a, "file"):
			paths = []string{jsString(a["file"])}
		default:
			r, where := re(a["filesMatching"]), re(a["whereFileContains"])
			for _, f := range ctx.Tracked {
				if r != nil && r.Test(f) && (where == nil || where.Test(ctx.read(f))) {
					paths = append(paths, f)
				}
			}
		}
		for _, path := range paths {
			doc := w.parsed(path)
			if doc == nil {
				continue
			}
			type base struct {
				name  string
				named bool
				v     any
			}
			var bases []base
			if f := a["forEachEntryAtField"]; truthy(f) {
				entries, ok := fieldAt(doc, jsString(f)).(map[string]any)
				if !ok {
					continue
				}
				for _, k := range sortedKeys(entries) {
					e := entries[k]
					if !truthy(e) || !isObject(e) {
						continue
					}
					if eq, ok := a["whereEntryFieldEquals"].(map[string]any); ok && truthy(a["whereEntryFieldEquals"]) {
						if !strictEqual(fieldAt(e, jsString(get(eq, "field"))), get(eq, "equals")) {
							continue
						}
					}
					bases = append(bases, base{k, true, e})
				}
			} else {
				bases = []base{{"", false, doc}}
			}
			for _, b := range bases {
				if fp := a["whenFieldPresent"]; truthy(fp) && isUndef(fieldAt(b.v, jsString(fp))) {
					continue
				}
				vars := map[string]any{"path": path}
				if b.named {
					vars["entry"] = b.name
				}
				flag := func(what, fix any, at string, extra map[string]any) {
					j.push(at, 0, what, fix, merge(vars, extra))
				}
				if rf := a["requireField"]; truthy(rf) && isUndef(fieldAt(b.v, jsString(rf))) {
					flag(get(a, "what"), get(a, "fix"), path, nil)
				}
				if rfm, ok := a["requireFieldMatching"].(map[string]any); ok {
					v := fieldAt(b.v, jsString(rfm["field"]))
					if isUndef(v) || !re(rfm["pattern"]).Test(jsString(v)) {
						flag(get(a, "what"), get(a, "fix"), path, nil)
					}
				}
				if ff := a["forbidField"]; truthy(ff) && !isUndef(fieldAt(b.v, jsString(ff))) {
					flag(get(a, "what"), get(a, "fix"), path, nil)
				}
				if m, ok := a["forbidValueInArray"].(map[string]any); ok {
					if arrayHoldsValue(fieldAt(b.v, jsString(get(m, "atField"))), m, get(m, "value")) {
						flag(get(a, "what"), get(a, "fix"), path, nil)
					}
				}
				if m, ok := a["requireValueInArray"].(map[string]any); ok {
					if !arrayHoldsValue(fieldAt(b.v, jsString(get(m, "atField"))), m, get(m, "value")) {
						flag(get(a, "what"), get(a, "fix"), path, nil)
					}
				}
				if eq, ok := a["requireEqualFields"].(map[string]any); ok {
					inFile := jsString(get(eq, "inFile"))
					if _, ok := ctx.Read(inFile); !ok {
						wfm := obj(eq["whenFileMissing"])
						flag(get(wfm, "what"), get(wfm, "fix"), inFile, nil)
					} else if target := w.parsed(inFile); target != nil {
						first := fieldAt(b.v, jsString(get(eq, "field")))
						second := fieldAt(target, jsString(get(eq, "atField")))
						if !strictEqual(first, second) {
							wu := obj(eq["whenUnequal"])
							flag(get(wu, "what"), get(wu, "fix"), path, map[string]any{"first": first, "second": second})
						}
					}
				}
			}
		}
	}
	for _, a := range items(j.spec["checkKeyValueFile"]) {
		var keys []string
		for _, k := range arr(a["keys"]) {
			keys = append(keys, jsString(k))
		}
		keysVar := map[string]any{"keys": strings.Join(keys, ", ")}
		file := jsString(get(a, "file"))
		text, ok := ctx.Read(file)
		if !ok {
			wm := obj(a["whenMissing"])
			j.push(file, 0, get(wm, "what"), get(wm, "fix"), keysVar)
			continue
		}
		seen := map[string]bool{}
		for i, rawLine := range strings.Split(text, "\n") {
			line := strings.TrimFunc(rawLine, isJSSpace)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			eq := strings.Index(line, "=")
			if eq == -1 {
				m := obj(a["whenLineNotKeyValue"])
				j.push(file, i+1, get(m, "what"), get(m, "fix"), merge(keysVar, map[string]any{"line": line}))
				continue
			}
			key := strings.TrimFunc(line[:eq], isJSSpace)
			if !contains(keys, key) {
				m := obj(a["whenKeyUnknown"])
				j.push(file, i+1, get(m, "what"), get(m, "fix"), merge(keysVar, map[string]any{"key": key}))
			}
			seen[key] = true
		}
		for _, key := range keys {
			if seen[key] {
				continue
			}
			m := obj(a["whenKeyMissing"])
			j.push(file, 0, get(m, "what"), get(m, "fix"), merge(keysVar, map[string]any{"key": key}))
		}
	}
}
