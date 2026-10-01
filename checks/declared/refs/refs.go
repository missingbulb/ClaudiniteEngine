// Package refs is the reference-barrier engine: which folders' files may
// not reference which other folders, read from an edge vocabulary (from,
// between, siblings, to, scope, allow, except, matchNames,
// alsoMatchNames, matchUniqueFilenames, reason), and the scan that finds
// each crossing among the module imports and path-like references of the
// guarded files. Test modules (*.test.mjs, *.test.js, *.test.cjs) are
// never scanned.
package refs

import (
	"fmt"
	"regexp"

	"strings"

	"github.com/dlclark/regexp2"
)

// Tree is what a barrier reads of the repo.
type Tree interface {
	// Files are the scanned files; Tracked the index; AllFiles the
	// scanned files before vendored ones are dropped.
	Files() []string
	Tracked() []string
	AllFiles() []string
	Read(path string) (string, bool)
}

// Edge is one normalized barrier.
type Edge struct {
	Froms                []string
	Targets              []string
	Siblings             string
	Scope                string
	Allow                []string
	Carve                []string
	Exceptions           []Exception
	MatchNames           bool
	AlsoMatchNames       []string
	MatchUniqueFilenames bool
	Reason               string
}

// Exception excuses one file's (or subtree's) crossings, optionally only
// into some barred folders.
type Exception struct {
	Path   string
	To     []string
	Reason string
}

// Problem is a malformed edge.
type Problem struct{ What, Fix string }

// Finding is one crossing, or (Spec) a fault of the declaration itself.
type Finding struct {
	File     string
	Line     int
	What     string
	Why      string
	Fix      string
	Resolved string
	// Spec marks a finding about the barrier declaration, anchored at the
	// settings file and always blocking.
	Spec bool
}

// Stale is an exception that matched nothing.
type Stale struct{ Path, To string }

// NormPrefix folds a folder prefix: separators, ./, trailing /; "" is the
// repo root and "*" passes through.
func NormPrefix(p string) string {
	if p == "*" {
		return "*"
	}
	s := strings.ReplaceAll(p, `\`, "/")
	s = strings.TrimPrefix(s, "./")
	s = strings.TrimLeft(s, "/")
	s = strings.TrimRight(s, "/")
	if s == "." {
		return ""
	}
	return s
}

// Under reports whether path is prefix or inside it.
func Under(path, prefix string) bool {
	if prefix == "" {
		return true
	}
	if prefix == "*" {
		return false
	}
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

func isTestFile(f string) bool { return testFileRe.MatchString(f) }

var testFileRe = regexp.MustCompile(`\.test\.[cm]?js$`)

func normJoin(base, rel string) (string, bool) {
	var parts []string
	if base != "" {
		parts = strings.Split(base, "/")
	}
	parts = append(parts, strings.Split(rel, "/")...)
	var out []string
	for _, seg := range parts {
		switch seg {
		case "", ".":
			continue
		case "..":
			if len(out) > 0 && out[len(out)-1] != ".." {
				out = out[:len(out)-1]
			} else {
				return "", false
			}
		default:
			out = append(out, seg)
		}
	}
	return strings.Join(out, "/"), true
}

func isGlob(t string) bool       { return strings.HasSuffix(t, "/*") && !strings.HasPrefix(t, "*.") }
func globPrefix(t string) string { return t[:len(t)-2] }

var childGlob = regexp.MustCompile(`^(.+?)/\*/([^*]+)$`)

func isChildGlob(t string) bool { return childGlob.MatchString(t) && !strings.HasPrefix(t, "*.") }

func childGlobParts(t string) (string, string) {
	m := childGlob.FindStringSubmatch(t)
	return m[1], m[2]
}

func carveMatch(path string, carve []string) bool {
	for _, e := range carve {
		if strings.HasPrefix(e, "*.") {
			base := path[strings.LastIndex(path, "/")+1:]
			if strings.HasSuffix(base, e[1:]) {
				return true
			}
		} else if Under(path, e) {
			return true
		}
	}
	return false
}

func carveCovers(e, t string) bool {
	if strings.HasPrefix(e, "*.") || isChildGlob(e) {
		return false
	}
	tp := t
	if isGlob(t) {
		tp = globPrefix(t)
	}
	if !isGlob(e) {
		return Under(tp, e)
	}
	if isGlob(t) {
		return Under(tp, globPrefix(e))
	}
	return Under(tp, globPrefix(e)) && tp != globPrefix(e)
}

var hasExtRe = regexp.MustCompile(`\.[A-Za-z][A-Za-z0-9]{0,19}$`)

var tryExt = []string{".js", ".mjs", ".cjs", ".jsx", ".ts", ".tsx", ".json", ".css", ".scss", ".less", ".py", ".go", ".rb", ".java", ".rs", ".php", ".html", ".vue", ".svelte"}
var tryIndex = []string{"/index.js", "/index.mjs", "/index.ts", "/index.tsx", "/index.jsx"}

const sassRank = 200

type completion struct {
	path string
	rank int
}

// Index is the tree as references resolve against it.
type Index struct {
	files       map[string]bool
	dirs        map[string]bool
	dirList     []string
	byBase      map[string][]string
	completions map[string]completion
}

var sassRe = regexp.MustCompile(`^_(.+)\.(scss|sass)$`)

// BuildIndex indexes the tracked and scanned files.
func BuildIndex(t Tree) *Index {
	idx := &Index{files: map[string]bool{}, dirs: map[string]bool{}, byBase: map[string][]string{}, completions: map[string]completion{}}
	complete := func(prefix, path string, rank int) {
		if cur, ok := idx.completions[prefix]; !ok || rank < cur.rank {
			idx.completions[prefix] = completion{path, rank}
		}
	}
	seen := map[string]bool{}
	var all []string
	for _, f := range append(append([]string{}, t.Tracked()...), t.AllFiles()...) {
		if !seen[f] {
			seen[f] = true
			all = append(all, f)
		}
	}
	for _, raw := range all {
		f := strings.ReplaceAll(raw, `\`, "/")
		idx.files[f] = true
		parts := strings.Split(f, "/")
		for i := 1; i < len(parts); i++ {
			d := strings.Join(parts[:i], "/")
			if !idx.dirs[d] {
				idx.dirs[d] = true
				idx.dirList = append(idx.dirList, d)
			}
		}
		base := parts[len(parts)-1]
		idx.byBase[base] = append(idx.byBase[base], f)
		for i, ext := range tryExt {
			if len(base) > len(ext) && strings.HasSuffix(f, ext) {
				complete(f[:len(f)-len(ext)], f, i)
				break
			}
		}
		for i, ix := range tryIndex {
			if strings.HasSuffix(f, ix) {
				complete(f[:len(f)-len(ix)], f, len(tryExt)+i)
				break
			}
		}
		if m := sassRe.FindStringSubmatch(base); m != nil {
			r := sassRank
			if m[2] == "sass" {
				r++
			}
			complete(f[:len(f)-len(base)]+m[1], f, r)
		}
	}
	return idx
}

func (idx *Index) matchTree(p string) string {
	if p == "" {
		return ""
	}
	if idx.files[p] || idx.dirs[p] {
		return p
	}
	return idx.completions[p].path
}

var (
	quotedRe   = regexp.MustCompile("'([^'\\n]+)'|\"([^\"\\n]+)\"|`([^`\\n]+)`")
	pathishRe  = regexp.MustCompile(`(?:\.{1,2}[\\/])[\w.@+\-\\/]+|[\w@+\-][\w.@+\-]*(?:[\\/][\w.@+\-]+)+|[\w@+\-][\w.@+\-]*\.[A-Za-z][A-Za-z0-9]{0,19}`)
	urlishRe   = regexp.MustCompile(`(?i)^(?:[a-z][a-z0-9+.\-]*:)?//`)
	codeFileRe = regexp.MustCompile(`\.(?:mjs|cjs|jsx?|mts|cts|tsx?)$`)
	importRe   = regexp.MustCompile("(?:\\bfrom|\\bimport|\\brequire)\\s*\\(?\\s*['\"`](\\.[^'\"`\\n]+)['\"`]")
	candChars  = regexp.MustCompile("[.\"'`/\\\\]")
	queryRe    = regexp.MustCompile(`[?#].*$`)
	leadPunct  = regexp.MustCompile("^[(\\[{<'\"`]+")
	trailPunct = regexp.MustCompile("[)\\]}>'\"`.,;:]+$")
	dottedRe   = regexp.MustCompile(`^[A-Za-z_][\w.]*[A-Za-z0-9_]$`)
)

type spec struct {
	spec string
	line int
}

func importSpecifiers(source string) []spec {
	var out []spec
	for _, m := range importRe.FindAllStringSubmatchIndex(source, -1) {
		at := m[2]
		out = append(out, spec{source[m[2]:m[3]], strings.Count(source[:at], "\n") + 1})
	}
	return out
}

func (idx *Index) resolveImport(s, fromDir string) string {
	p, ok := normJoin(fromDir, strings.ReplaceAll(s, `\`, "/"))
	if !ok || p == "" {
		return ""
	}
	if idx.files[p] {
		return p
	}
	if c, ok := idx.completions[p]; ok && c.rank < sassRank {
		return c.path
	}
	return ""
}

// jsTrim trims what String.prototype.trim does.
func jsTrim(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', '\v', '\f', '\r', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
			return true
		}
		return r >= 0x2000 && r <= 0x200a
	})
}

func candidatesOn(line string) []string {
	if !candChars.MatchString(line) {
		return nil
	}
	var raw []string
	seen := map[string]bool{}
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			raw = append(raw, s)
		}
	}
	for _, m := range quotedRe.FindAllStringSubmatch(line, -1) {
		switch {
		case m[1] != "":
			add(m[1])
		case m[2] != "":
			add(m[2])
		default:
			add(m[3])
		}
	}
	for _, m := range pathishRe.FindAllString(line, -1) {
		add(m)
	}
	var out []string
	done := map[string]bool{}
	for _, c := range raw {
		c = queryRe.ReplaceAllString(jsTrim(c), "")
		c = trailPunct.ReplaceAllString(leadPunct.ReplaceAllString(c, ""), "")
		if c == "" || urlishRe.MatchString(c) || strings.HasPrefix(c, "mailto:") || done[c] {
			continue
		}
		done[c] = true
		out = append(out, c)
	}
	return out
}

func (idx *Index) resolveRef(candidate, fromDir string, unique bool) string {
	c := strings.ReplaceAll(candidate, `\`, "/")
	var attempts []string
	try := func(base, rel string) {
		if p, ok := normJoin(base, rel); ok {
			attempts = append(attempts, p)
		} else {
			attempts = append(attempts, "")
		}
	}
	switch {
	case strings.HasPrefix(c, "./") || strings.HasPrefix(c, "../"):
		try(fromDir, c)
	case strings.HasPrefix(c, "/"):
		try("", c[1:])
	default:
		try(fromDir, c)
		if strings.Contains(c, "/") {
			try("", c)
		}
	}
	for _, p := range attempts {
		if hit := idx.matchTree(p); hit != "" {
			return hit
		}
	}
	if !strings.Contains(c, "/") && strings.Contains(c, ".") && dottedRe.MatchString(c) {
		if d, ok := normJoin("", strings.ReplaceAll(c, ".", "/")); ok && d != "" {
			switch {
			case idx.files[d], idx.dirs[d]:
				return d
			case idx.files[d+".py"]:
				return d + ".py"
			case idx.files[d+"/__init__.py"]:
				return d + "/__init__.py"
			}
		}
	}
	if unique && !strings.Contains(c, "/") && hasExtRe.MatchString(c) {
		if paths := idx.byBase[c]; len(paths) == 1 {
			return paths[0]
		}
	}
	return ""
}

func (idx *Index) childDirs(p string) []string {
	var out []string
	for _, d := range idx.dirList {
		if strings.HasPrefix(d, p+"/") && !strings.Contains(d[len(p)+1:], "/") {
			out = append(out, d)
		}
	}
	return out
}

// Rule is what the scan says about the rule that owns the edges.
type Rule struct {
	Why string
	// SettingsPath anchors a fault of the declaration.
	SettingsPath string
}

// Scan is one run's shared state: the index and per-file caches, reused
// by every barrier over one tree.
type Scan struct {
	tree       Tree
	idx        *Index
	lines      map[string][]string
	candidates map[string][][]string
	resolved   map[string]string
}

// NewScan starts a scan over t.
func NewScan(t Tree) *Scan {
	return &Scan{tree: t, lines: map[string][]string{}, candidates: map[string][][]string{}, resolved: map[string]string{}}
}

// SpecFinding is a fault of the barrier declaration.
func SpecFinding(r Rule, what, fix string) Finding {
	return Finding{File: r.SettingsPath, What: "barriers config: " + what, Why: "a malformed barrier declaration silently enforces nothing", Fix: fix, Spec: true}
}

// StaleFindings reports exceptions that matched nothing.
func StaleFindings(stale []Stale, r Rule) []Finding {
	var out []Finding
	for _, s := range stale {
		to, more := "", ""
		if s.To != "" {
			to = fmt.Sprintf(` → "%s"`, s.To)
			more = ` (or the stale "to" item)`
		}
		out = append(out, SpecFinding(r, fmt.Sprintf(`exception for "%s"%s matched nothing — the coupling it excused is gone`, s.Path, to),
			fmt.Sprintf(`remove the stale exception%s from the rule's "except"`, more)))
	}
	return out
}

// Findings scans every edge. scanErrors reports whether a spec finding
// came out of the scan itself (an empty expansion, a name matching no
// barred folder), which suppresses the stale-exception report.
func (s *Scan) Findings(edges []Edge, r Rule) (out []Finding, stale []Stale, scanErrors bool) {
	if len(edges) == 0 {
		return nil, nil, false
	}
	if s.idx == nil {
		s.idx = BuildIndex(s.tree)
	}
	var scannable []string
	for _, f := range s.tree.Files() {
		f = strings.ReplaceAll(f, `\`, "/")
		if !isTestFile(f) {
			scannable = append(scannable, f)
		}
	}
	for _, e := range edges {
		if e.Siblings != "" {
			kids := s.idx.childDirs(e.Siblings)
			if len(kids) == 0 {
				out = append(out, SpecFinding(r, fmt.Sprintf(`"siblings" folder "%s" has no tracked child directories`, e.Siblings),
					"check the folder exists and has tracked subdirectories — an empty expansion would enforce nothing"))
				scanErrors = true
				continue
			}
			for _, kid := range kids {
				k := e
				k.Froms = []string{kid}
				f, st, bad := s.scanEdge(k, r, scannable)
				out, stale, scanErrors = append(out, f...), append(stale, st...), scanErrors || bad
			}
			continue
		}
		f, st, bad := s.scanEdge(e, r, scannable)
		out, stale, scanErrors = append(out, f...), append(stale, st...), scanErrors || bad
	}
	for _, f := range out {
		scanErrors = scanErrors || f.Spec
	}
	return out, stale, scanErrors
}

func escRe(s string) string { return regexp.QuoteMeta(s) }

func (s *Scan) scanEdge(e Edge, r Rule, scannable []string) ([]Finding, []Stale, bool) {
	idx := s.idx
	var out []Finding
	var carve []string
	for _, c := range e.Carve {
		switch {
		case isGlob(c):
			carve = append(carve, idx.childDirs(globPrefix(c))...)
		case isChildGlob(c):
			prefix, rest := childGlobParts(c)
			for _, kid := range idx.childDirs(prefix) {
				carve = append(carve, kid+"/"+rest)
			}
		default:
			carve = append(carve, c)
		}
	}
	star := false
	var barred []string
	broken := false
	for _, t := range e.Targets {
		if t == "*" {
			star = true
			continue
		}
		if isGlob(t) {
			kids := idx.childDirs(globPrefix(t))
			if len(kids) == 0 {
				out = append(out, SpecFinding(r, fmt.Sprintf(`"to" glob "%s" matched no directories`, t),
					"check the folder exists and has tracked subdirectories — an empty expansion would enforce nothing"))
				broken = true
			}
			barred = append(barred, kids...)
		} else {
			barred = append(barred, t)
		}
	}
	if broken {
		return out, nil, true
	}
	inGuard := func(p string) bool {
		for _, fp := range e.Froms {
			if Under(p, fp) {
				return !carveMatch(p, carve)
			}
		}
		return false
	}
	allowed := func(p string) bool {
		for _, a := range e.Allow {
			if Under(p, a) {
				return true
			}
		}
		return false
	}
	var nameRe *regexp2.Regexp
	nameDirs := map[string][]string{}
	if e.MatchNames {
		bases := map[string][]string{}
		var order []string
		for _, d := range barred {
			base := d[strings.LastIndex(d, "/")+1:]
			if _, ok := bases[base]; !ok {
				order = append(order, base)
			}
			bases[base] = append(bases[base], d)
		}
		var names []string
		for _, base := range order {
			if strings.ContainsAny(base, "-_") || contains(e.AlsoMatchNames, base) {
				nameDirs[base] = bases[base]
				names = append(names, escRe(base))
			}
		}
		for _, n := range e.AlsoMatchNames {
			if _, ok := bases[n]; !ok {
				out = append(out, SpecFinding(r, fmt.Sprintf(`"alsoMatchNames" entry "%s" is not the name of any barred folder`, n), "fix the name or drop the entry — it matches nothing"))
			}
		}
		if len(names) > 0 {
			nameRe = regexp2.MustCompile(`(^|[^\w./-])(`+strings.Join(names, "|")+`)(?=[^\w./-]|$)`, regexp2.ECMAScript)
		}
	}
	allowHint := "route shared code through a shared/contracts folder both sides may use"
	if len(e.Allow) > 0 {
		allowHint = fmt.Sprintf("route shared code through an allowed folder (%s)", strings.Join(e.Allow, ", "))
	}
	excuse := `add a reviewed exception to the barrier's "except" if the crossing is deliberate`
	why := e.Reason
	if why == "" {
		why = r.Why
	}
	var raw []Finding
	seen := map[string]bool{}
	emit := func(file string, line int, what, resolved string) {
		key := fmt.Sprintf("%s:%d:%s", file, line, resolved)
		if seen[key] {
			return
		}
		seen[key] = true
		raw = append(raw, Finding{File: file, Line: line, What: what, Why: why, Fix: allowHint + ", remove the reference, or " + excuse, Resolved: resolved})
	}
	barredHit := func(p string) string {
		for _, b := range barred {
			if Under(p, b) {
				return b
			}
		}
		return ""
	}
	importsOnly := e.Scope == "imports"
	for _, file := range scannable {
		if !inGuard(file) || (importsOnly && !codeFileRe.MatchString(file)) {
			continue
		}
		text, ok := s.tree.Read(file)
		if !ok {
			continue
		}
		fromDir := ""
		if i := strings.LastIndex(file, "/"); i >= 0 {
			fromDir = file[:i]
		}
		if importsOnly {
			for _, sp := range importSpecifiers(text) {
				res := idx.resolveImport(sp.spec, fromDir)
				if res == "" || inGuard(res) || allowed(res) {
					continue
				}
				if star {
					emit(file, sp.line, fmt.Sprintf(`imports "%s" → resolves to "%s", outside the guarded region`, sp.spec, res), res)
				} else if t := barredHit(res); t != "" {
					emit(file, sp.line, fmt.Sprintf(`imports "%s" → resolves to "%s", inside the barred folder "%s"`, sp.spec, res, t), res)
				}
			}
			continue
		}
		lines, ok := s.lines[file]
		if !ok {
			lines = strings.Split(text, "\n")
			s.lines[file] = lines
		}
		cands, ok := s.candidates[file]
		if !ok {
			cands = make([][]string, len(lines))
			for i, l := range lines {
				cands[i] = candidatesOn(l)
			}
			s.candidates[file] = cands
		}
		for i := range lines {
			for _, rawRef := range cands[i] {
				key := "n"
				if e.MatchUniqueFilenames {
					key = "u"
				}
				key += "\x00" + file + "\x00" + rawRef
				res, ok := s.resolved[key]
				if !ok {
					res = idx.resolveRef(rawRef, fromDir, e.MatchUniqueFilenames)
					s.resolved[key] = res
				}
				if res == "" || inGuard(res) || allowed(res) {
					continue
				}
				if star {
					emit(file, i+1, fmt.Sprintf(`references "%s" → resolves to "%s", outside the guarded region`, rawRef, res), res)
				} else if t := barredHit(res); t != "" {
					emit(file, i+1, fmt.Sprintf(`references "%s" → resolves to "%s", inside the barred folder "%s"`, rawRef, res, t), res)
				}
			}
			if nameRe != nil {
				m, _ := nameRe.FindStringMatch(lines[i])
				for m != nil {
					name := m.GroupByNumber(2).String()
					for _, d := range nameDirs[name] {
						if inGuard(d) || allowed(d) {
							continue
						}
						emit(file, i+1, fmt.Sprintf(`mentions "%s", the name of the barred folder "%s"`, name, d), d)
					}
					m, _ = nameRe.FindNextMatch(m)
				}
			}
		}
	}
	kept, stale := applyExceptions(raw, e.Exceptions)
	return append(out, kept...), stale, false
}

func applyExceptions(fs []Finding, exceptions []Exception) ([]Finding, []Stale) {
	used := make([][]bool, len(exceptions))
	for i, e := range exceptions {
		n := len(e.To)
		if e.To == nil {
			n = 1
		}
		used[i] = make([]bool, n)
	}
	var kept []Finding
outer:
	for _, f := range fs {
		for i, e := range exceptions {
			covers := e.Path == f.File || (strings.HasSuffix(e.Path, "/") && strings.HasPrefix(f.File, e.Path))
			if !covers {
				continue
			}
			if e.To == nil {
				used[i][0] = true
				continue outer
			}
			for j, t := range e.To {
				if Under(f.Resolved, t) {
					used[i][j] = true
					continue outer
				}
			}
		}
		kept = append(kept, f)
	}
	var stale []Stale
	for i, e := range exceptions {
		if e.To == nil {
			if !used[i][0] {
				stale = append(stale, Stale{Path: e.Path})
			}
			continue
		}
		for j, t := range e.To {
			if !used[i][j] {
				stale = append(stale, Stale{Path: e.Path, To: t})
			}
		}
	}
	return kept, stale
}

func contains(l []string, s string) bool {
	for _, e := range l {
		if e == s {
			return true
		}
	}
	return false
}
