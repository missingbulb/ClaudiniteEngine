package provenance

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/missingbulb/ClaudiniteEngine/shared/gitcmd"
	"github.com/missingbulb/ClaudiniteEngine/shared/jsjson"
	prov "github.com/missingbulb/ClaudiniteEngine/shared/provenance"
)

// The backfill brief: a pack's history written pull request first, the
// squash commit's body once and a draft entry per element and event with
// every field git vouches for (the date, the kind, the actor by handle,
// the model from the trailer, the carrier as Mechanism, Landed with the
// version the version log cut for it). What git cannot vouch for, Source,
// Reason, Rejected and Retire when, is left out for the session to fill.
//
// An element's events are read from its carrier's own history: a rule is
// walked back through its file by slug, then trigger, then text, and
// through an in-place rewording by its own words; a skill, check, task or
// manifest is born at its file's oldest commit and every later commit is a
// candidate. A commit touching sweepPacks or more packs is a sweep, listed
// and set aside rather than drafted onto an element.

const (
	sweepPacks      = 5
	briefBodyLines  = 60
	inPlaceShare    = 0.6
	inPlaceMinWords = 5
)

var (
	trailerRE    = regexp.MustCompile(`(?i)^(?:co-authored-by|claude-session|signed-off-by|reviewed-by|refs|fixes|closes|resolves):?\s`)
	modelRE      = regexp.MustCompile(`(?i)^co-authored-by:\s*(Claude\b[^<]*?)\s*<`)
	referencedRE = regexp.MustCompile(`(?i)\b(Refs|Fixes|Closes|Resolves)\s*:?\s*#(\d+)`)
	prAtEnd      = regexp.MustCompile(`\s*\(#(\d+)\)\s*$`)
	packFileRE   = regexp.MustCompile(`^(?:\.claudinite/local/)?packs/([^/]+)/`)
	handleRE     = regexp.MustCompile(`^\d+\+([^@]+)@users\.noreply\.github\.com$`)
	ownerRE      = regexp.MustCompile(`github\.com[:/]([^/]+)/`)
	blankRuns    = regexp.MustCompile(`\n{3,}`)
	wordRE       = regexp.MustCompile(`[\p{L}\p{N}][\p{L}\p{N}'-]{3,}`)
	markupRE     = regexp.MustCompile("[`*]")
	jsSpaces     = regexp.MustCompile(`[\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]+`)
	fenceLine    = regexp.MustCompile("^\\s*```")
	headingLine  = regexp.MustCompile(`^#{1,6} `)
	bulletLine   = regexp.MustCompile(`^[-*] `)
	versionLine  = regexp.MustCompile(`^[-+]\s*"?version"?:\s*['"]?[\d.]+['"]?,?\s*$`)
	versionRow   = regexp.MustCompile(`^\|\s*([^|]+?)\s*\|\s*(\d{4}-\d{2}-\d{2})\s*\|\s*(.*?)\s*\|\s*$`)
	manifestVer  = regexp.MustCompile(`(?m)(?:^|[\s{,])"?version"?:\s*['"]?([\d.]+)`)
	rawLine      = regexp.MustCompile(`^:\d+ \d+ ([0-9a-f]+) ([0-9a-f]+) \w+\t(.+)$`)
	zeroSha      = regexp.MustCompile(`^0+$`)
	carryExt     = regexp.MustCompile(`\.(md|mjs|json)$`)
	anyCite      = regexp.MustCompile(`#\d+`)
	skillFileRE  = regexp.MustCompile(`/skills/([^/]+)/SKILL\.md$`)
	commentLine  = regexp.MustCompile(`^\s*//`)
	commentLead  = regexp.MustCompile(`^\s*// ?`)
)

var commonWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields("that this with from when never what your into than them they have each only rather where which does their there then also before after every other should would could will been were more most some such just like over here dont") {
		commonWords[w] = true
	}
}

// jsLen is a string's length as JavaScript counts it, in UTF-16 units.
func jsLen(s string) int { return len(utf16.Encode([]rune(s))) }

// searchable is the text a needle is matched against: markup dropped and
// every whitespace run one space, so a wrapped or backticked spelling
// still matches.
func searchable(text string) string {
	return jsSpaces.ReplaceAllString(markupRE.ReplaceAllString(text, ""), " ")
}

// withoutRows drops table rows, which list every element by construction.
func withoutRows(text string) string {
	var keep []string
	for _, l := range strings.Split(text, "\n") {
		if !strings.HasPrefix(strings.TrimLeftFunc(l, unicode.IsSpace), "|") {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\n")
}

func couldCarry(p string) bool {
	return carryExt.MatchString(p) && !strings.HasSuffix(p, ".test.mjs") && !strings.Contains(p, "/"+prov.Dir+"/")
}

type commitRef struct{ n, keyword string }

type commitInfo struct {
	sha, short, date, subject, pr, title, handle, body string
	refs                                               []commitRef
	models                                             []string
	files                                              []string
	sweep                                              bool
}

// at is how a line names the commit: its pull request, else its short sha.
func (c *commitInfo) at() string {
	if c.pr != "" {
		return "#" + c.pr
	}
	return c.short
}

type briefer struct {
	io       WriteIO
	git      gitcmd.Repo
	cache    map[string]*commitInfo
	position map[string]int
}

func (b *briefer) out(args ...string) string { return gitOut(b.git, args...) }

func addModel(models []string, m string) []string {
	if has(models, m) {
		return models
	}
	return append(models, m)
}

func capitalized(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}

// commit reads one commit once: what it carried and how many packs it
// touched. A merge's own trailer names a bare "Claude", so the branch side
// is read back for the model that did the work.
func (b *briefer) commit(sha string) *commitInfo {
	if c, ok := b.cache[sha]; ok {
		return c
	}
	parts := strings.Split(b.out("show", "-s", "--format=%H%x00%h%x00%as%x00%ae%x00%P%x00%s%x00%b", sha), "\x00")
	for len(parts) < 7 {
		parts = append(parts, "")
	}
	full, short, date, email, parents, subject, body := parts[0], parts[1], parts[2], parts[3], parts[4], parts[5], parts[6]
	var models, lines []string
	for _, l := range strings.Split(body, "\n") {
		t := strings.TrimSpace(l)
		if m := modelRE.FindStringSubmatch(t); m != nil {
			models = addModel(models, strings.TrimSpace(m[1]))
			continue
		}
		if trailerRE.MatchString(t) {
			continue
		}
		lines = append(lines, strings.TrimRightFunc(l, unicode.IsSpace))
	}
	pr := ""
	if m := prAtEnd.FindStringSubmatch(subject); m != nil {
		pr = m[1]
	}
	var refs []commitRef
	for _, m := range referencedRE.FindAllStringSubmatch(body, -1) {
		n := m[2]
		dup := n == pr
		for _, r := range refs {
			dup = dup || r.n == n
		}
		if !dup {
			refs = append(refs, commitRef{n: n, keyword: capitalized(m[1])})
		}
	}
	branch := strings.Fields(parents)
	if len(branch) > 1 && (len(models) == 0 || (len(models) == 1 && models[0] == "Claude")) {
		for _, l := range strings.Split(b.out("log", "--format=%b", branch[0]+".."+sha), "\n") {
			if m := modelRE.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
				models = addModel(models, strings.TrimSpace(m[1]))
			}
		}
	}
	packs := map[string]bool{}
	var files []string
	for _, f := range strings.Split(b.out("diff-tree", "--root", "-r", "-m", "--first-parent", "--no-commit-id", "--name-only", sha), "\n") {
		if f != "" {
			files = append(files, f)
		}
		if m := packFileRE.FindStringSubmatch(f); m != nil {
			packs[m[1]] = true
		}
	}
	if len(models) > 1 {
		kept := models[:0:0]
		for _, m := range models {
			if m != "Claude" {
				kept = append(kept, m)
			}
		}
		models = kept
	}
	handle := ""
	if m := handleRE.FindStringSubmatch(email); m != nil {
		handle = m[1]
	}
	c := &commitInfo{
		sha: full, short: short, date: date, subject: subject, pr: pr, refs: refs, models: models, files: files,
		title:  strings.TrimSpace(prAtEnd.ReplaceAllString(subject, "")),
		handle: handle,
		body:   strings.TrimSpace(blankRuns.ReplaceAllString(strings.Join(lines, "\n"), "\n\n")),
		sweep:  len(packs) >= sweepPacks,
	}
	b.cache[sha] = c
	return c
}

type pathCommit struct{ sha, path string }

// commitsOf are the commits touching a path, newest first, each with the
// path it had then.
func (b *briefer) commitsOf(p string, follow bool) []pathCommit {
	args := []string{"log"}
	if follow {
		args = append(args, "--follow")
	}
	args = append(args, "--format=%x01%H", "--name-only", "--", p)
	var out []pathCommit
	for _, line := range strings.Split(b.out(args...), "\n") {
		if strings.HasPrefix(line, "\x01") {
			out = append(out, pathCommit{sha: line[1:], path: p})
			continue
		}
		if strings.TrimSpace(line) != "" && len(out) > 0 {
			out[len(out)-1].path = strings.TrimSpace(line)
		}
	}
	return out
}

func (b *briefer) show(sha, p string) string { return b.out("show", sha+":"+p) }

func ruleWords(text string) map[string]bool {
	out := map[string]bool{}
	for _, w := range wordRE.FindAllString(strings.ToLower(searchable(text)), -1) {
		if !commonWords[w] {
			out[w] = true
		}
	}
	return out
}

// orderedWords are a text's rule words in the order they first appear.
func orderedWords(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, w := range wordRE.FindAllString(strings.ToLower(searchable(text)), -1) {
		if !commonWords[w] && !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}

// passages are a revision's top-level bullets with their continuations
// and its paragraphs, each normalized as a rule's text is.
func passages(text string) []string {
	var out, cur []string
	fenced := false
	flush := func() {
		if len(cur) > 0 {
			out = append(out, prov.NormalizeRuleText(strings.Join(cur, "\n")))
		}
		cur = nil
	}
	for _, line := range strings.Split(text, "\n") {
		if fenceLine.MatchString(line) {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		if strings.TrimSpace(line) == "" || headingLine.MatchString(line) {
			flush()
			continue
		}
		if bulletLine.MatchString(line) {
			flush()
		}
		cur = append(cur, line)
	}
	flush()
	return out
}

type inPlaceMatch struct {
	share   float64
	kept    string
	passage string
}

// inPlace is the passage of text holding most of words, where it holds at
// least inPlaceShare of them; a passage another rule still matches is that
// rule's, not this one's.
func inPlace(words map[string]bool, text string, claimed []string) *inPlaceMatch {
	var best *inPlaceMatch
	for _, p := range passages(text) {
		taken := false
		for _, c := range claimed {
			taken = taken || strings.Contains(c, p)
		}
		if taken {
			continue
		}
		var kept []string
		for _, w := range orderedWords(p) {
			if words[w] {
				kept = append(kept, w)
			}
		}
		share := float64(len(kept)) / float64(len(words))
		if best == nil || share > best.share {
			sort.Strings(kept)
			best = &inPlaceMatch{share: share, kept: strings.Join(kept, " "), passage: p}
		}
	}
	if best != nil && best.share >= inPlaceShare {
		return best
	}
	return nil
}

// soleHeir: a passage that was one rule's own bullet goes to the newer
// rule holding most of it; a prose paragraph may feed several.
func soleHeir(passage string, older, newer []prov.Block, rule prov.Block, share float64) bool {
	in := false
	for _, b := range older {
		in = in || strings.Contains(b.Text, passage)
	}
	if !in {
		return true
	}
	for _, b := range newer {
		if b.Text == rule.Text {
			continue
		}
		w := ruleWords(b.Text)
		if len(w) < inPlaceMinWords {
			continue
		}
		if m := inPlace(w, passage, nil); m != nil && m.share > share {
			return false
		}
	}
	return true
}

type event struct {
	kind, sha string
	from      []string
}

type inPlaceAt struct {
	sha   string
	share float64
}

func findBlock(blocks []prov.Block, like prov.Block) (prov.Block, bool) {
	if like.Slug != "" {
		for _, b := range blocks {
			if b.Slug == like.Slug {
				return b, true
			}
		}
	}
	for _, b := range blocks {
		if b.Trigger == like.Trigger {
			return b, true
		}
	}
	for _, b := range blocks {
		if b.Text == like.Text {
			return b, true
		}
	}
	return prov.Block{}, false
}

// ruleEvents walks a rule back through its file: born where it is first
// found, reworded at each commit after which its text differs, and
// followed through an in-place rewording by its own words.
func (b *briefer) ruleEvents(file string, rule prov.Block, blocksOf func(string) []prov.Block) ([]event, *inPlaceAt) {
	state := rule
	newer := ""
	var words map[string]bool
	kept := ""
	newerText := ""
	var events []event
	var at *inPlaceAt
	for _, c := range b.commitsOf(file, true) {
		text := b.show(c.sha, c.path)
		newerRevision := newerText
		newerText = text
		if words == nil {
			var blocks []prov.Block
			if text != "" {
				blocks = blocksOf(text)
			}
			if found, ok := findBlock(blocks, state); ok {
				if newer != "" && found.Text != state.Text {
					events = append(events, event{kind: "reworded", sha: newer})
				}
				state = found
				newer = c.sha
				continue
			}
			w := ruleWords(state.Text)
			var newerRules []prov.Block
			if newerRevision != "" {
				newerRules = blocksOf(newerRevision)
			}
			var claimed []string
			for _, nb := range newerRules {
				if nb.Text == state.Text {
					continue
				}
				if o, ok := findBlock(blocks, nb); ok {
					claimed = append(claimed, o.Text)
				}
			}
			var match *inPlaceMatch
			if newer != "" && text != "" && len(w) >= inPlaceMinWords {
				match = inPlace(w, text, claimed)
			}
			if match == nil || !soleHeir(match.passage, blocks, newerRules, state, match.share) {
				break
			}
			events = append(events, event{kind: "reworded", sha: newer})
			at = &inPlaceAt{sha: newer, share: match.share}
			words = w
			kept = match.kept
			newer = c.sha
			continue
		}
		var match *inPlaceMatch
		if text != "" {
			match = inPlace(words, text, nil)
		}
		if match == nil {
			break
		}
		if match.kept != kept {
			events = append(events, event{kind: "reworded", sha: newer})
		}
		kept = match.kept
		newer = c.sha
	}
	if newer != "" {
		events = append(events, event{kind: "born", sha: newer})
	}
	return events, at
}

// declarationOf is one check's declaration in a shared declarations file,
// re-serialized as JSON.stringify writes its parsed value, so two spellings
// of one value read the same.
func declarationOf(text, id string) (string, bool) {
	all, err := jsjson.Decode([]byte(text))
	if err != nil || all.Kind != jsjson.Array {
		return "", false
	}
	for _, d := range all.Arr {
		if got, ok := d.Prop("id"); ok && got.Kind == jsjson.String && got.Str == id {
			return jsjson.Stringify(d), true
		}
	}
	return "", false
}

// declaredCheckEvents are the commits after which one check's own
// declaration differs, in a file it shares with its siblings.
func (b *briefer) declaredCheckEvents(p, id string) []event {
	var events []event
	state, newer := "", ""
	for _, c := range b.commitsOf(p, true) {
		text, ok := declarationOf(b.show(c.sha, c.path), id)
		if !ok {
			break
		}
		if newer != "" && text != state {
			events = append(events, event{kind: "reworded", sha: newer})
		}
		state = text
		newer = c.sha
	}
	if newer == "" {
		return nil
	}
	return append(events, event{kind: "born", sha: newer})
}

func changedLine(l string) bool {
	if l == "" || (l[0] != '-' && l[0] != '+') {
		return false
	}
	return len(l) == 1 || (l[1] != '-' && l[1] != '+')
}

// fileEvents: born at the oldest commit of the path, every later commit a
// reworded candidate except one that changed nothing but a version line.
func (b *briefer) fileEvents(p string, follow bool) []event {
	commits := b.commitsOf(p, follow)
	if len(commits) == 0 {
		return nil
	}
	bump := func(c pathCommit) bool {
		n := 0
		for _, l := range strings.Split(b.out("show", "--format=", c.sha, "--", c.path), "\n") {
			if !changedLine(l) {
				continue
			}
			if !versionLine.MatchString(l) {
				return false
			}
			n++
		}
		return n > 0
	}
	var out []event
	for _, c := range commits[:len(commits)-1] {
		if !bump(c) {
			out = append(out, event{kind: "reworded", sha: c.sha})
		}
	}
	return append(out, event{kind: "born", sha: commits[len(commits)-1].sha})
}

type versionRowT struct{ version, date, what string }

func versionRows(io WriteIO, pack string) []versionRowT {
	text, _ := io.Read(pack + "/" + prov.Dir + "/" + VersionsFile)
	var out []versionRowT
	for _, l := range strings.Split(text, "\n") {
		if m := versionRow.FindStringSubmatch(l); m != nil {
			out = append(out, versionRowT{version: m[1], date: m[2], what: m[3]})
		}
	}
	return out
}

func (b *briefer) manifestTextAt(sha, pack string) string {
	for _, f := range prov.ManifestFiles {
		if t := b.show(sha, pack+"/"+f); t != "" {
			return t
		}
	}
	return ""
}

// ownCut is the version a commit's own diff cut, "" where it cut none.
func (b *briefer) ownCut(pack string) func(*commitInfo) string {
	at := map[string]string{}
	versionAt := func(sha string) string {
		if v, ok := at[sha]; ok {
			return v
		}
		v := ""
		if m := manifestVer.FindStringSubmatch(b.manifestTextAt(sha, pack)); m != nil {
			v = m[1]
		}
		at[sha] = v
		return v
	}
	return func(c *commitInfo) string {
		own := versionAt(c.sha)
		if own != "" && own != versionAt(c.sha+"^") {
			return own
		}
		return ""
	}
}

// packPaths is every path the pack has lived at, its current one first,
// read back from the renames of the files a pack cannot exist without.
func (b *briefer) packPaths(pack string) []string {
	out := []string{pack}
	c := prov.PackCarriers(pack, b.io)
	var anchors []string
	for _, f := range prov.ManifestFiles {
		anchors = append(anchors, pack+"/"+f)
	}
	anchors = append(anchors, pack+"/"+prov.ProseFile, pack+"/README.md")
	for _, s := range c.Skills {
		if s.Present {
			anchors = append(anchors, s.File)
		}
	}
	for _, t := range c.Tasks {
		anchors = append(anchors, t.File)
	}
	for _, anchor := range anchors {
		if !b.io.Exists(anchor) {
			continue
		}
		suffix := anchor[len(pack)+1:]
		for _, pc := range b.commitsOf(anchor, true) {
			if pc.path == anchor {
				continue
			}
			at := ""
			if strings.HasSuffix(pc.path, "/"+suffix) {
				at = pc.path[:len(pc.path)-len(suffix)-1]
			}
			if at == "" {
				at = pc.path
				if i := strings.LastIndex(at, "/"); i >= 0 && i < len(at)-1 {
					at = at[:i]
				}
			}
			if at == "" || at == pack || has(out, at) {
				continue
			}
			if has(prov.PackRoots, at) || len(strings.Split(at, "/")) < 2 {
				continue
			}
			out = append(out, at)
		}
	}
	return out
}

type blobPath struct{ path, blob string }

type rev struct {
	sha, date   string
	files, lost []blobPath
}

type carrierIdx struct {
	revs            []rev
	carried, listed map[string]string
	size            map[string]int
}

// readBlobs reads several blobs in one cat-file --batch, delimited by the
// sizes its headers give.
func (b *briefer) readBlobs(shas []string) map[string]string {
	out := map[string]string{}
	if len(shas) == 0 {
		return out
	}
	r, err := b.git.RunInput(strings.Join(shas, "\n")+"\n", "cat-file", "--batch")
	if err != nil || r.Code != 0 {
		return out
	}
	buf := r.Stdout
	at := 0
	for at < len(buf) {
		nl := strings.IndexByte(buf[at:], '\n')
		if nl < 0 {
			break
		}
		head := strings.Split(buf[at:at+nl], " ")
		if len(head) < 3 || head[1] != "blob" {
			break
		}
		size, err := strconv.Atoi(head[2])
		start := at + nl + 1
		if err != nil || start+size > len(buf) {
			break
		}
		out[head[0]] = buf[start : start+size]
		at = start + size + 1
	}
	return out
}

// carrierIndex is every revision of every file the pack's carriers could
// live in, oldest commit first, each read whole as the two texts a needle
// is searched against.
func (b *briefer) carrierIndex(paths []string) *carrierIdx {
	idx := &carrierIdx{carried: map[string]string{}, listed: map[string]string{}, size: map[string]int{}}
	wanted := map[string]bool{}
	var order []string
	want := func(s string) {
		if !wanted[s] {
			wanted[s] = true
			order = append(order, s)
		}
	}
	args := append([]string{"log", "--reverse", "--format=%x01%H %as", "--raw", "--no-abbrev", "--no-renames", "--"}, paths...)
	for _, rec := range strings.Split(b.out(args...), "\x01") {
		lines := strings.Split(rec, "\n")
		head := strings.Split(lines[0], " ")
		if head[0] == "" {
			continue
		}
		r := rev{sha: head[0]}
		if len(head) > 1 {
			r.date = head[1]
		}
		for _, line := range lines[1:] {
			m := rawLine.FindStringSubmatch(line)
			if m == nil || !couldCarry(m[3]) {
				continue
			}
			if !zeroSha.MatchString(m[1]) {
				r.lost = append(r.lost, blobPath{m[3], m[1]})
				want(m[1])
			}
			if zeroSha.MatchString(m[2]) {
				continue
			}
			r.files = append(r.files, blobPath{m[3], m[2]})
			want(m[2])
		}
		if len(r.files) > 0 || len(r.lost) > 0 {
			idx.revs = append(idx.revs, r)
		}
	}
	for sha, text := range b.readBlobs(order) {
		idx.carried[sha] = searchable(withoutRows(text))
		idx.listed[sha] = searchable(text)
		idx.size[sha] = jsLen(text)
	}
	return idx
}

type listedAt struct{ sha, date, file string }

type followed struct {
	sha, date, kind string
	files           []string
}

type unfollowed struct {
	listedAt *listedAt
	shrank   string
}

type element struct {
	id, mechanism, carrier, needle, check string
	events, later                         []event
	inPlace                               *inPlaceAt
	followed                              *followed
	unfollowed                            *unfollowed
}

// shrankAt is a carrier other than except that the commit deleted or
// shortened: the trace a move leaves at the old path.
func shrankAt(idx *carrierIdx, sha, except string) string {
	for _, r := range idx.revs {
		if r.sha != sha {
			continue
		}
		for _, l := range r.lost {
			if l.path == except {
				continue
			}
			post := ""
			for _, f := range r.files {
				if f.path == l.path {
					post = f.blob
					break
				}
			}
			if post == "" || idx.size[post] < idx.size[l.blob] {
				return l.path
			}
		}
		return ""
	}
	return ""
}

func extensionOf(f string) string {
	if m := carryExt.FindStringSubmatch(f); m != nil {
		return m[1]
	}
	return ""
}

// carrierChange is a move where the element stayed the same kind of file,
// a conversion where it changed kind.
func carrierChange(from []string, to string) string {
	var exts []string
	for _, f := range from {
		if e := extensionOf(f); e != "" {
			exts = append(exts, e)
		}
	}
	if len(exts) > 0 && !has(exts, extensionOf(to)) {
		return "converted"
	}
	return "moved"
}

// withEarlierCarrier moves the birth back to a carrier before today's that
// held the element's text, and redrafts the commit that had been its birth
// as the move or conversion it was. An unfollowable birth with evidence
// against it is named.
func (b *briefer) withEarlierCarrier(idx *carrierIdx, el *element) {
	bornAt := -1
	for i, e := range el.events {
		if e.kind == "born" {
			bornAt = i
			break
		}
	}
	if bornAt < 0 || el.needle == "" {
		return
	}
	born := el.events[bornAt]
	pos, ok := b.position[born.sha]
	if !ok {
		return
	}
	want := searchable(el.needle)
	var listed *listedAt
	for _, r := range idx.revs {
		rp, ok := b.position[r.sha]
		if !ok || rp >= pos {
			continue
		}
		var files []string
		for _, f := range r.files {
			if t, ok := idx.carried[f.blob]; ok && strings.Contains(t, want) {
				files = append(files, f.path)
			}
		}
		if len(files) > 0 {
			kind := carrierChange(files, el.carrier)
			events := append([]event{}, el.events...)
			events[bornAt] = event{kind: kind, sha: born.sha, from: files}
			el.events = append(events, event{kind: "born", sha: r.sha})
			el.followed = &followed{sha: r.sha, date: r.date, kind: kind, files: files}
			return
		}
		if listed != nil {
			continue
		}
		for _, f := range r.files {
			if t, ok := idx.listed[f.blob]; ok && strings.Contains(t, want) {
				listed = &listedAt{sha: r.sha, date: r.date, file: f.path}
				break
			}
		}
	}
	if shrank := shrankAt(idx, born.sha, el.carrier); listed != nil || shrank != "" {
		el.unfollowed = &unfollowed{listedAt: listed, shrank: shrank}
	}
}

// elements are the brief's elements, each with its carrier and events.
func (b *briefer) elements(pack string, wanted, paths []string) []*element {
	c := prov.PackCarriers(pack, b.io)
	ids := wanted
	if len(ids) == 0 {
		for _, f := range prov.Files(pack, b.io) {
			if f.Empty || f.ConvertedOnly {
				ids = append(ids, f.ID)
			}
		}
	}
	var out []*element
	var idx *carrierIdx
	follow := func(el *element) {
		if el.needle != "" && idx == nil {
			idx = b.carrierIndex(paths)
		}
		if el.needle != "" {
			b.withEarlierCarrier(idx, el)
		}
		out = append(out, el)
	}
	ruleBlocks := func(t string) []prov.Block { return prov.RuleBlocks(t, false) }
	skillBullets := func(t string) []prov.Block { return prov.SkillShape(t).Bullets }
	asBlock := func(r prov.Rule) prov.Block {
		return prov.Block{Slug: r.Slug, Numeric: r.Numeric, Trigger: r.Trigger, Text: r.Text}
	}
	for _, id := range ids {
		el := &element{id: id}
		if r, ok := findRule(c.Rules, id); ok {
			el.mechanism = `a RULES.md rule, triggered on "` + r.Trigger + `".`
			el.carrier, el.needle = r.File, r.Trigger
			el.events, el.inPlace = b.ruleEvents(r.File, asBlock(r), ruleBlocks)
			follow(el)
		} else if g, ok := findRule(c.Guidelines, id); ok {
			el.mechanism = "a guideline of the " + g.Skill + ` skill, triggered on "` + g.Trigger + `".`
			el.carrier, el.needle = g.File, g.Trigger
			el.events, el.inPlace = b.ruleEvents(g.File, asBlock(g), skillBullets)
			follow(el)
		} else if s, ok := findSkill(c.Skills, id); ok {
			body := s.Body
			if body == "" {
				body = s.Proposed
			}
			el.mechanism = "the " + id + " skill, body " + body + ", reached by its description."
			el.carrier = s.File
			el.events = b.fileEvents(s.File, true)
			follow(el)
		} else if ch, ok := findCheck(c.Checks, id); ok {
			el.mechanism = "check " + ch.ID + ", in " + ch.File + "."
			el.carrier, el.needle, el.check = ch.File, ch.ID, ch.ID
			if strings.HasSuffix(ch.File, "declared-checks.json") {
				el.events = b.declaredCheckEvents(ch.File, ch.ID)
			} else {
				el.events = b.fileEvents(ch.File, true)
			}
			follow(el)
		} else if t, ok := findTask(c.Tasks, id); ok {
			el.mechanism = "task " + id + "."
			el.carrier = t.File
			el.events = b.fileEvents(t.Dir, false)
			follow(el)
		} else if id == prov.PackElement {
			el.mechanism = "the pack manifest."
			for _, f := range prov.ManifestFiles {
				for _, e := range b.fileEvents(pack+"/"+f, true) {
					if e.kind == "born" {
						el.events = append(el.events, e)
					} else {
						el.later = append(el.later, e)
					}
				}
			}
			out = append(out, el)
		} else {
			out = append(out, el)
		}
	}
	return out
}

func findRule(rs []prov.Rule, id string) (prov.Rule, bool) {
	for _, r := range rs {
		if r.Slug == id {
			return r, true
		}
	}
	return prov.Rule{}, false
}

func findSkill(ss []prov.Skill, id string) (prov.Skill, bool) {
	for _, s := range ss {
		if s.Name == id && s.Present {
			return s, true
		}
	}
	return prov.Skill{}, false
}

func findCheck(cs []prov.Carrier, id string) (prov.Carrier, bool) {
	for _, c := range cs {
		if prov.ElementOf(c) == id {
			return c, true
		}
	}
	return prov.Carrier{}, false
}

func findTask(ts []prov.Carrier, id string) (prov.Carrier, bool) {
	for _, t := range ts {
		if t.ID == id {
			return t, true
		}
	}
	return prov.Carrier{}, false
}

// names reports whether a row's text names pull request n, not one whose
// number merely starts with it.
func names(what, n string) bool {
	if !strings.Contains(what, "#"+n) {
		return false
	}
	return !regexp.MustCompile(`#` + regexp.QuoteMeta(n) + `\d`).MatchString(what)
}

func citesNothing(r versionRowT) bool { return !anyCite.MatchString(r.what) }

func rowFor(rows []versionRowT, introducers map[string]string, c *commitInfo) *versionRowT {
	ns := []string{c.pr}
	for _, r := range c.refs {
		ns = append(ns, r.n)
	}
	for i, r := range rows {
		for _, n := range ns {
			if n != "" && names(r.what, n) {
				return &rows[i]
			}
		}
	}
	for i, r := range rows {
		if citesNothing(r) && introducers[r.version] == c.sha {
			return &rows[i]
		}
	}
	return nil
}

func (b *briefer) rowIntroducers(pack string, rows []versionRowT) map[string]string {
	file := pack + "/" + prov.Dir + "/" + VersionsFile
	out := map[string]string{}
	for _, r := range rows {
		sha := strings.Split(strings.TrimSpace(b.out("log", "--reverse", "--format=%H", "-S| "+r.version+" |", "--", file)), "\n")[0]
		if sha != "" {
			out[r.version] = sha
		}
	}
	return out
}

func versionFor(rows []versionRowT, introducers map[string]string, c *commitInfo, ownCut func(*commitInfo) string) string {
	if c.pr != "" {
		for _, r := range rows {
			if names(r.what, c.pr) {
				return r.version
			}
		}
	}
	for _, r := range rows {
		if citesNothing(r) && introducers[r.version] == c.sha {
			return r.version
		}
	}
	return ownCut(c)
}

// manifestHeader is the manifest's leading comment block.
func manifestHeader(text string) []string {
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		if commentLine.MatchString(l) {
			lines = append(lines, commentLead.ReplaceAllString(l, ""))
			continue
		}
		if strings.TrimSpace(l) == "" {
			if len(lines) == 0 {
				continue
			}
			lines = append(lines, "")
			continue
		}
		break
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// sharedFields are the fields every entry of one commit shares: only what
// git vouches for, never the issues its trailer references.
func sharedFields(c *commitInfo, version, owner string) []Field {
	var out []Field
	if c.handle != "" {
		v := "@" + c.handle
		if c.handle == owner {
			v += " (owner)"
		}
		out = append(out, Field{"Actor", v + "."})
	}
	if len(c.models) > 0 {
		out = append(out, Field{"Model", strings.Join(c.models, ", ") + ", per the commit trailer."})
	}
	landed := "commit " + c.short
	if c.pr != "" {
		landed = "#" + c.pr
	}
	if version != "" {
		landed += " · pack version " + version
	}
	return append(out, Field{"Landed", landed + "."})
}

// mechanismAt is the carrier a followed birth was born in, for the shapes
// the text search reaches.
func mechanismAt(file string, el *element) string {
	if el.check != "" {
		return "check " + el.check + ", in " + file + "."
	}
	if m := skillFileRE.FindStringSubmatch(file); m != nil {
		return "a guideline of the " + m[1] + ` skill, triggered on "` + el.needle + `".`
	}
	if strings.HasSuffix(file, "/"+prov.ProseFile) {
		return `a RULES.md rule, triggered on "` + el.needle + `".`
	}
	return ""
}

func draftEntry(el *element, ev event, c *commitInfo) string {
	var fields []Field
	if ev.kind == "born" {
		m := ""
		if el.followed != nil {
			m = mechanismAt(el.followed.files[0], el)
		}
		if m == "" {
			m = el.mechanism
		}
		fields = append(fields, Field{"Mechanism", m})
	} else if ev.from != nil {
		fields = append(fields, Field{"Mechanism", el.mechanism + " it was carried by " + strings.Join(ev.from, ", ") + " until here; say why the carrier changed."})
	}
	return Render(Entry{Date: c.date, Kind: ev.kind, Title: c.title + " (" + c.at() + ")", Fields: fields})
}

func count(n int, what string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, what)
	}
	return fmt.Sprintf("%d %ss", n, what)
}

func jsRound(x float64) int { return int(math.Floor(x + 0.5)) }

// Brief is the backfill brief of one pack: every commit that touched it
// once, and a draft entry per element and event, for the wanted elements
// or, with none named, every pending file.
func Brief(io WriteIO, git gitcmd.Repo, pack string, wanted []string) []string {
	b := &briefer{io: io, git: git, cache: map[string]*commitInfo{}, position: map[string]int{}}
	for i, sha := range strings.Split(b.out("rev-list", "--reverse", "--topo-order", "HEAD"), "\n") {
		if sha != "" {
			b.position[sha] = i
		}
	}
	owner := ""
	if m := ownerRE.FindStringSubmatch(b.out("remote", "get-url", "origin")); m != nil {
		owner = m[1]
	}
	rows := versionRows(io, pack)
	introducers := b.rowIntroducers(pack, rows)
	ownCut := b.ownCut(pack)
	paths := b.packPaths(pack)
	elements := b.elements(pack, wanted, paths)
	type draft struct {
		el *element
		ev event
	}
	byCommit := map[string][]draft{}
	var commitOrder []string
	sweeps := map[string][]string{}
	var unknown []string
	for _, el := range elements {
		if len(el.events) == 0 {
			unknown = append(unknown, el.id)
			continue
		}
		for _, ev := range el.events {
			c := b.commit(ev.sha)
			if c.sweep && ev.kind != "born" && el.id != prov.PackElement {
				sweeps[ev.sha] = append(sweeps[ev.sha], el.id)
				continue
			}
			if _, ok := byCommit[ev.sha]; !ok {
				commitOrder = append(commitOrder, ev.sha)
			}
			byCommit[ev.sha] = append(byCommit[ev.sha], draft{el, ev})
		}
	}
	order := make([]*commitInfo, len(commitOrder))
	for i, sha := range commitOrder {
		order[i] = b.cache[sha]
	}
	sort.SliceStable(order, func(i, j int) bool { return b.position[order[i].sha] < b.position[order[j].sha] })

	what := "pending file"
	if len(wanted) > 0 {
		what = "element"
	}
	lines := []string{"# " + pack + " · backfill brief", ""}
	lines = append(lines, count(len(elements), what)+" · "+count(len(order), "pack-local commit")+" · "+count(len(sweeps), "sweep"))
	if len(paths) > 1 {
		lines = append(lines, "", "this pack has moved: its history is read under "+strings.Join(paths, ", ")+", and a row naming a file in full is from before the move")
	}
	var converted []string
	for _, f := range prov.Files(pack, io) {
		if f.ConvertedOnly {
			converted = append(converted, f.ID)
		}
	}
	if len(converted) > 0 {
		lines = append(lines, "", "## files the references conversion filled", "each holds one born entry dated by the CONVERSION rather than by the element, plus the Reason and Retire when the doc carried. these are drafted below like an empty file; a born the batch dates on or before the placeholder replaces it as `apply --backfill` merges the history in date order, and a batch bringing no born leaves it standing - read each file's own evidence rather than truncating them as a class")
		for _, id := range converted {
			lines = append(lines, "- "+prov.FileOfID(id))
		}
	}
	lines = append(lines, "", "## every commit that touched this pack",
		"oldest first, each with the files it touched under the pack, the version row that claims it, and what it drafts below. a row reading NOTHING DRAFTED decided nothing, re-wrapped, or decided something no carrier's text shows - read its files before passing it",
		fmt.Sprintf("a commit touching %d or more packs is marked \"sweep\": it goes on _pack.md where it changed the pack's shape, and on an element only where it decided something about that one - never where it merely re-wrapped it", sweepPacks))
	claimed := map[string][]string{}
	for _, sha := range strings.Split(b.out(append([]string{"log", "--format=%H", "--reverse", "--"}, paths...)...), "\n") {
		if sha == "" {
			continue
		}
		c := b.commit(sha)
		var parts []string
		var under []string
		for _, f := range c.files {
			for _, p := range paths {
				if strings.HasPrefix(f, p+"/") {
					if strings.HasPrefix(f, paths[0]+"/") {
						under = append(under, f[len(paths[0])+1:])
					} else {
						under = append(under, f)
					}
					break
				}
			}
		}
		if len(under) == 0 {
			parts = append(parts, "(nothing under the pack)")
		} else {
			parts = append(parts, strings.Join(under, ", "))
		}
		row := rowFor(rows, introducers, c)
		if row != nil {
			claimed[row.version] = append(claimed[row.version], c.at())
			parts = append(parts, "version "+row.version)
		}
		if c.sweep {
			parts = append(parts, "sweep")
		}
		var drafted []string
		for _, d := range byCommit[c.sha] {
			drafted = append(drafted, d.el.id+" ("+d.ev.kind+")")
		}
		for _, id := range sweeps[c.sha] {
			drafted = append(drafted, id+" (set aside)")
		}
		if len(drafted) == 0 {
			parts = append(parts, "NOTHING DRAFTED")
		} else {
			parts = append(parts, strings.Join(drafted, ", "))
		}
		lines = append(lines, "- "+c.at()+" "+c.date+" "+c.title+" · "+strings.Join(parts, " · "))
	}
	var claiming, orphans []versionRowT
	for _, r := range rows {
		if _, ok := claimed[r.version]; ok {
			claiming = append(claiming, r)
		} else {
			orphans = append(orphans, r)
		}
	}
	if len(claiming) > 0 {
		lines = append(lines, "", "## version rows", "each row the commits above claim, once: its text is the decision the version was cut for")
		for _, r := range claiming {
			lines = append(lines, "- "+r.version+" "+r.date+" "+r.what+" (claims "+strings.Join(claimed[r.version], ", ")+")")
		}
	}
	if len(orphans) > 0 {
		lines = append(lines, "", "## version rows no commit here claims", "the row names the decision and the pull request that made it; neither reached a commit subject, so this is history the drafts below cannot carry")
		for _, r := range orphans {
			lines = append(lines, "- "+r.version+" "+r.date+" "+r.what)
		}
	}
	if len(unknown) > 0 {
		lines = append(lines, "", "## no history found", "git holds no commit for: "+strings.Join(unknown, ", ")+" (is the clone shallow?)")
	}
	var follows, inPlaces, unfollows []*element
	var manifest *element
	for _, el := range elements {
		if el.followed != nil {
			follows = append(follows, el)
		}
		if el.inPlace != nil {
			inPlaces = append(inPlaces, el)
		}
		if el.unfollowed != nil {
			unfollows = append(unfollows, el)
		}
		if el.id == prov.PackElement && manifest == nil {
			manifest = el
		}
	}
	if len(follows) > 0 {
		lines = append(lines, "", "## elements older than the carrier they sit in", "the birth below is drafted at the EARLIER carrier the pickaxe found, and the commit that would otherwise have read as the birth is drafted as the move or conversion it is. verify each against the old path before trusting it - `git show <sha>:<old path>`")
		for _, el := range follows {
			lines = append(lines, "- "+el.id+": "+el.followed.kind+" into "+el.carrier+"; carried by "+strings.Join(el.followed.files, ", ")+" from "+el.followed.date+" ("+el.followed.sha[:min(8, len(el.followed.sha))]+")")
		}
	}
	if len(inPlaces) > 0 {
		lines = append(lines, "", "## rules followed through an in-place rewording", "at the commit named, the rule's slug, trigger and text all changed at once inside the same file, so no exact match reaches behind it; the passage holding the share of its words named here is taken for the rule before that commit, the commit is drafted as reworded, and the birth below is where those words first stand together. verify each against the older revision - `git show <sha>^:<file>` - and restore the born to that commit where the passage turns out to be a different rule")
		for _, el := range inPlaces {
			c := b.commit(el.inPlace.sha)
			lines = append(lines, fmt.Sprintf("- %s: reworded in place at %s, %d%% of its words in the passage before it", el.id, c.at(), jsRound(el.inPlace.share*100)))
		}
	}
	if len(unfollows) > 0 {
		lines = append(lines, "", "## births the search could not go behind", "no carrier the pack has held holds the element's text before the birth drafted below, yet something says the element is older, so that birth is an ASSUMPTION rather than a derivation - an element REWORDED before it moved carries different text and cannot be followed by its text at all. read the evidence named beside each, `git show <sha>:<old path>`, before trusting the draft. (an unfollowable birth with nothing against it is an ordinary one and is not listed)")
		for _, el := range unfollows {
			var why []string
			if at := el.unfollowed.listedAt; at != nil {
				c := b.commit(at.sha)
				why = append(why, at.file+" listed it at "+at.date+" ("+c.at()+"), a table row that carries nothing")
			}
			if el.unfollowed.shrank != "" {
				why = append(why, "the birth commit took text out of "+el.unfollowed.shrank)
			}
			lines = append(lines, "- "+el.id+`: nothing earlier carries "`+el.needle+`"; `+strings.Join(why, ", and "))
		}
	}
	if manifest != nil {
		mf := prov.PackCarriers(pack, io).ManifestFile
		if mf == "" {
			mf = prov.ManifestFiles[0]
		}
		lines = append(lines, "", "## the manifest, "+pack+"/"+mf, "its header comment is the pack-level record the _pack entries are written from, and is trimmed like the README once they are; a _pack entry is a decision about the pack's shape, never every change in its scope, so the manifest's later commits are listed here and not drafted")
		text, _ := io.Read(pack + "/" + mf)
		header := manifestHeader(text)
		if len(header) == 0 {
			lines = append(lines, "(no header comment)")
		}
		for _, l := range header {
			if l == "" {
				lines = append(lines, ">")
			} else {
				lines = append(lines, "> "+l)
			}
		}
		for _, ev := range manifest.later {
			c := b.commit(ev.sha)
			sweep := ""
			if c.sweep {
				sweep = " (sweep)"
			}
			lines = append(lines, "- "+c.at()+" "+c.date+" "+c.title+sweep)
		}
	}
	if readme, _ := io.Read(pack + "/README.md"); readme != "" {
		lines = append(lines, "", "## README sentences that read as history", "each moves onto the entry it evidences and leaves the README")
		var prose []string
		for _, l := range strings.Split(readme, "\n") {
			if !strings.HasPrefix(l, "|") && !strings.HasPrefix(l, "#") {
				prose = append(prose, l)
			}
		}
		var tells []string
		for _, s := range sentences(strings.Join(prose, "\n")) {
			if historyRE.MatchString(s) {
				tells = append(tells, "- "+strings.TrimSpace(jsSpaces.ReplaceAllString(s, " ")))
			}
		}
		if len(tells) == 0 {
			lines = append(lines, "(none)")
		} else {
			lines = append(lines, strings.Join(tells, "\n"))
		}
		lines = append(lines, "", "## README sections", "every heading, the prose under it and its tables. a section explaining why an element reads as it does is an entry's Reason, not the README's - the adopter's half is what the pack activates on, what each element demands, and when to reach for it. a table is counted separately because either answer is possible and the byte count is what says which is at stake: an evidence or per-member table is history and moves onto the entries it evidences, a catalog of what the pack carries stays")
		type section struct {
			heading      string
			bytes, table int
		}
		var sections []section
		for _, l := range strings.Split(readme, "\n") {
			if headingLine.MatchString(l) {
				sections = append(sections, section{heading: l})
				continue
			}
			if len(sections) == 0 {
				continue
			}
			n := jsLen(strings.TrimSpace(l))
			if strings.HasPrefix(l, "|") {
				sections[len(sections)-1].table += n
			} else {
				sections[len(sections)-1].bytes += n
			}
		}
		for _, s := range sections {
			line := fmt.Sprintf("- %s · %d bytes of prose", s.heading, s.bytes)
			if s.table > 0 {
				line += fmt.Sprintf(" · %d bytes of table", s.table)
			}
			lines = append(lines, line)
		}
	}
	for _, c := range order {
		drafts := byCommit[c.sha]
		head := "commit " + c.short
		if c.pr != "" {
			head = "PR #" + c.pr
		}
		lines = append(lines, "", "## "+head+" · "+c.date+" · "+c.title)
		version := versionFor(rows, introducers, c, ownCut)
		var facts []string
		if c.handle != "" {
			facts = append(facts, "by @"+c.handle)
		}
		if len(c.models) > 0 {
			facts = append(facts, "model "+strings.Join(c.models, ", "))
		}
		if version != "" {
			facts = append(facts, "pack version "+version)
		}
		if len(facts) > 0 {
			lines = append(lines, "- "+strings.Join(facts, " · "))
		}
		if len(c.refs) > 0 {
			var rs []string
			for _, r := range c.refs {
				rs = append(rs, r.keyword+" #"+r.n)
			}
			lines = append(lines, "- the commit trailer references "+strings.Join(rs, ", ")+" - READ THE PULL REQUEST BODY for the keyword it used and write that into Landed. the two disagree on most older pull requests, the body's Closes being what fills GitHub's Development panel and the trailer's Refs linking nothing there")
		}
		var ds []string
		for _, d := range drafts {
			ds = append(ds, d.el.id+" ("+d.ev.kind+")")
		}
		lines = append(lines, "- "+strings.Join(ds, ", "))
		if c.body != "" {
			body := strings.Split(c.body, "\n")
			lines = append(lines, "")
			for _, l := range body[:min(briefBodyLines, len(body))] {
				if l == "" {
					lines = append(lines, ">")
				} else {
					lines = append(lines, "> "+l)
				}
			}
			if len(body) > briefBodyLines {
				lines = append(lines, fmt.Sprintf("> (… %d more lines: git show %s)", len(body)-briefBodyLines, c.short))
			}
		}
		lines = append(lines, "", "the defaults go under every entry below them; fill Source, Reason, Rejected and Retire when where the evidence carries them, there or on one entry; delete a draft the commit did not decide")
		lines = append(lines, "```entry-defaults")
		for _, f := range sharedFields(c, version, owner) {
			lines = append(lines, "- **"+f.Name+":** "+f.Value)
		}
		lines = append(lines, "```")
		for _, d := range drafts {
			lines = append(lines, "```entry "+d.el.id, strings.TrimRight(draftEntry(d.el, d.ev, c), " \t\r\n"), "```")
		}
	}
	return lines
}
