// Package provenance is the provenance verbs over the convention's reader
// (shared/provenance): mark a pack onto it, append an entry, check a
// pack, read one element's history, apply an edited brief, convert a
// retired references doc, and reduce a file for promotion across a
// repository boundary.
package provenance

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/growth/capture"
	"github.com/missingbulb/ClaudiniteEngine/shared/gitcmd"
	prov "github.com/missingbulb/ClaudiniteEngine/shared/provenance"
)

// VersionsFile is a pack's version log beside its elements' files.
const VersionsFile = "VERSIONS.md"

// LocalPrefix is how a declaration names a local pack.
const LocalPrefix = "local/"

// MountDir is the vendored mount, which the update flows replace whole.
const MountDir = ".claudinite/shared"

func manifestOf(io WriteIO, dir string) bool {
	for _, f := range prov.ManifestFiles {
		if io.Exists(dir + "/" + f) {
			return true
		}
	}
	return false
}

// ResolvePack is the pack directory id names: a path to a pack, local/<name>
// for a member's local pack, else packs/<id> in a canon checkout, then
// .claudinite/local/packs/<id>. The vendored mount is never a target: a
// path under it is refused. "" for none.
func ResolvePack(io WriteIO, id string) (string, error) {
	if name, ok := strings.CutPrefix(id, LocalPrefix); ok && name != "" && !strings.Contains(name, "/") {
		dir := prov.PackRoots[1] + "/" + name
		if manifestOf(io, dir) {
			return dir, nil
		}
		return "", nil
	}
	if strings.Contains(id, "/") {
		dir := path.Clean(id)
		if dir == MountDir || strings.HasPrefix(dir, MountDir+"/") {
			return "", fmt.Errorf("%s is under the vendored mount %s/, which the update flows replace whole - change the pack in the canon, or carry the difference in a local pack", dir, MountDir)
		}
		if manifestOf(io, dir) {
			return dir, nil
		}
		return "", nil
	}
	for _, r := range prov.PackRoots {
		if manifestOf(io, r+"/"+id) {
			return r + "/" + id, nil
		}
	}
	return "", nil
}

// AllPacks are every pack under both roots.
func AllPacks(io WriteIO) []string {
	var out []string
	for _, r := range prov.PackRoots {
		names, _ := io.ListDir(r)
		sort.Strings(names)
		for _, n := range names {
			if manifestOf(io, r+"/"+n) {
				out = append(out, r+"/"+n)
			}
		}
	}
	return out
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// Mark marks every pack, the report's last line counting the empty files
// left (pending history).
func Mark(io WriteIO, packs []string, dryRun bool) ([]string, error) {
	var lines []string
	empty := 0
	for _, p := range packs {
		got, err := MarkPack(p, io)
		if err != nil {
			return nil, err
		}
		lines = append(lines, got...)
		empty += len(prov.AuditPack(p, io).Empty)
	}
	where := packs[0]
	if len(packs) != 1 {
		where = strconv.Itoa(len(packs)) + " packs"
	}
	tail := ""
	if dryRun {
		tail = " (dry run: nothing written)"
	}
	lines = append(lines, fmt.Sprintf("%d empty provenance %s under %s%s", empty, plural(empty, "file"), where, tail))
	return lines, nil
}

// Check is the audit as text: what each file is named by, then every
// fault, and how many faults there were. Pending history is no fault.
func Check(io WriteIO, git gitcmd.Repo, packs []string) ([]string, int) {
	var lines []string
	faults := 0
	for _, pack := range packs {
		a := prov.AuditPack(pack, io)
		var order []string
		namedBy := map[string][]string{}
		name := func(id, by string) {
			if _, ok := namedBy[id]; !ok {
				order = append(order, id)
			}
			namedBy[id] = append(namedBy[id], by)
		}
		c := a.Carriers
		for _, r := range c.Rules {
			if r.Slug != "" {
				name(r.Slug, `rule "`+r.Trigger+`"`)
			}
		}
		for _, g := range c.Guidelines {
			if g.Slug != "" {
				name(g.Slug, `guideline "`+g.Trigger+`" (`+g.Skill+`)`)
			}
		}
		for _, s := range c.Skills {
			if s.Present {
				body := s.Body
				if body == "" {
					body = "no body"
				}
				name(s.Name, "skill "+s.Name+" ("+body+")")
			}
		}
		for _, ch := range c.Checks {
			name(prov.ElementID(ch.ID), "check "+ch.ID)
		}
		for _, t := range c.Tasks {
			name(t.ID, "task "+t.ID)
		}
		for _, d := range c.Decls {
			name(d.ID, "declared rule "+d.ID)
		}
		if c.ManifestFile != "" {
			name(prov.PackElement, "the manifest")
		}
		lines = append(lines, pack+"/"+prov.Dir+"/")
		shallow := false
		for _, f := range a.Files {
			if f.ConvertedOnly {
				shallow = strings.TrimSpace(gitOut(git, "rev-parse", "--is-shallow-repository")) != "false"
				break
			}
		}
		files := append([]prov.File{}, a.Files...)
		sort.SliceStable(files, func(i, j int) bool { return files[i].ID < files[j].ID })
		for _, f := range files {
			state := ""
			switch {
			case f.Status == "retired":
				state = " (retired)"
			case f.Empty:
				state = " (empty)"
			case f.ConvertedOnly && !shallow:
				state = " (conversion only, its birth unverified)"
			}
			by := namedBy[f.ID]
			if len(by) == 0 {
				by = []string{"nothing"}
			}
			lines = append(lines, "  "+prov.FileOfID(f.ID)+" ← "+strings.Join(by, ", ")+state)
		}
		declined := pack + "/" + prov.Dir + "/" + prov.DeclinedFile
		if text, ok := io.Read(declined); ok {
			entries, _ := prov.ParseKinds(text, []string{prov.DeclinedKind})
			lines = append(lines, fmt.Sprintf("  %s ← %d %s turned down", prov.DeclinedFile, len(entries), plural(len(entries), "candidate")))
		}
		fault := func(file string, line int, what string) {
			faults++
			at := file
			if line > 0 {
				at += ":" + strconv.Itoa(line)
			}
			lines = append(lines, "  "+at+": "+what)
		}
		for _, u := range a.Unmarked {
			fault(u.File, u.Line, `"`+u.Trigger+`" ends with no marker`)
		}
		for _, d := range a.Dangling {
			which := "no file"
			if d.Retired {
				which = "retired"
			}
			fault(d.File, d.Line, d.Carrier+" names "+prov.FileOfID(d.ID)+", which is "+which)
		}
		for _, u := range a.Unnamed {
			fault(u.Path, 0, "live, and named by no carrier")
		}
		for _, n := range a.NoBody {
			fault(n.File, 0, "skill "+n.Name+" declares no body")
		}
		for _, m := range a.MarkerInWorkflow {
			fault(m.File, m.LastLine, `"`+m.Trigger+`" carries a marker inside a workflow skill`)
		}
		for _, e := range a.ParseErrors {
			fault(e.File, e.Line, e.What)
		}
		for _, e := range a.EntryFaults {
			fault(e.File, e.Line, e.What)
		}
		if doc := pack + "/references.md"; io.Exists(doc) {
			fault(doc, 0, "a references.md still exists - convert-references retires it")
		}
		if shallow {
			for _, f := range files {
				if f.ConvertedOnly {
					fault(f.Path, 0, "filled by the conversion, and the clone is shallow - unshallow it before reading whether its date is the birth")
				}
			}
		}
	}
	return lines, faults
}

// AppendOpts overrides an entry's kind or date, and Backfill takes the
// backfill's lane: entries dated in the past, written in date order, a
// born opening a file the marking pass could not.
type AppendOpts struct {
	Kind, Date string
	Backfill   bool
}

const secretRefusal = "the entry carries what reads as a secret; a decision log is the one place nothing else scans, so it is refused whole"

// Append appends one entry to each element's file, refusing an entry
// that carries what reads as a secret, since a decision log is prose an
// agent writes and the one place nothing else scans. It returns the
// files written, or the problems that stopped it.
func Append(io WriteIO, pack string, elements []string, text string, o AppendOpts) ([]string, []string) {
	if capture.Scrub(text, nil) != text {
		return nil, []string{secretRefusal}
	}
	e, problems := ParseEntryText(text)
	if len(problems) > 0 {
		return nil, problems
	}
	if o.Kind != "" {
		e.Kind = o.Kind
	}
	if o.Date != "" {
		e.Date = o.Date
	}
	var written []string
	for _, el := range elements {
		declined := el == prov.DeclinedKind || el == "_declined"
		file := pack + "/" + prov.Dir + "/" + prov.FileOfID(el)
		if declined {
			file = pack + "/" + prov.Dir + "/" + prov.DeclinedFile
		}
		opensFile := o.Backfill && e.Kind == "born"
		if !declined && !io.Exists(file) && !opensFile {
			hint := ""
			if o.Backfill {
				hint = "; a --backfill batch opens a new file with born"
			}
			return written, []string{fmt.Sprintf("%s does not exist - no carrier of %s names an element %q (run mark, or check the id%s)", file, pack, el, hint)}
		}
		existing, _ := io.Read(file)
		var out string
		var probs []string
		switch {
		case o.Backfill && !declined:
			out, _, probs = Backfilled(existing, []Entry{e}, prov.Kinds, "born")
		case declined:
			out, probs = Appended(existing, e, []string{prov.DeclinedKind}, "")
		default:
			out, probs = Appended(existing, e, prov.Kinds, "born")
		}
		if len(probs) > 0 {
			for i := range probs {
				probs[i] = file + ": " + probs[i]
			}
			return written, probs
		}
		if err := io.Write(file, out); err != nil {
			return written, []string{err.Error()}
		}
		written = append(written, file)
	}
	return written, nil
}

// gitOut is a git command's stdout, "" where it failed.
func gitOut(git gitcmd.Repo, args ...string) string {
	r, err := git.Run(args...)
	if err != nil || r.Code != 0 {
		return ""
	}
	return r.Stdout
}

// headIO reads files as HEAD holds them and lists the working tree, as
// the Node tool's change detection did.
type headIO struct {
	WriteIO
	git gitcmd.Repo
}

func (h headIO) Read(p string) (string, bool) {
	t := gitOut(h.git, "show", "HEAD:"+p)
	if t == "" {
		if r, err := h.git.Run("cat-file", "-e", "HEAD:"+p); err != nil || r.Code != 0 {
			return "", false
		}
	}
	return t, true
}

// Changed are the elements the working tree's change touched: a rule
// whose normalized text differs from HEAD's, a skill, check, task or
// manifest whose file changed.
func Changed(io WriteIO, git gitcmd.Repo, pack string) []string {
	changed := map[string]bool{}
	for _, l := range strings.Split(gitOut(git, "diff", "--name-only", "HEAD", "--", pack)+"\n"+gitOut(git, "ls-files", "--others", "--exclude-standard", "--", pack), "\n") {
		if l != "" {
			changed[l] = true
		}
	}
	now := prov.PackCarriers(pack, io)
	before := prov.PackCarriers(pack, headIO{io, git})
	out := map[string]bool{}
	prose := func(nowList, thenList []prov.Rule) {
		then := map[string]prov.Rule{}
		for _, r := range thenList {
			then[r.Trigger] = r
		}
		for _, r := range nowList {
			if r.Slug == "" || !changed[r.File] {
				continue
			}
			if was, ok := then[r.Trigger]; !ok || was.Text != r.Text {
				out[r.Slug] = true
			}
		}
	}
	prose(now.Rules, before.Rules)
	prose(now.Guidelines, before.Guidelines)
	for _, s := range now.Skills {
		if s.Present && changed[s.File] {
			out[s.Name] = true
		}
	}
	for _, c := range now.Checks {
		if changed[c.File] {
			out[prov.ElementID(c.ID)] = true
		}
	}
	for _, t := range now.Tasks {
		for f := range changed {
			if strings.HasPrefix(f, t.Dir+"/") {
				out[t.ID] = true
			}
		}
	}
	if now.ManifestFile != "" {
		for _, f := range prov.ManifestFiles {
			if changed[pack+"/"+f] {
				out[prov.PackElement] = true
			}
		}
	}
	ids := make([]string, 0, len(out))
	for id := range out {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

var (
	prRef     = regexp.MustCompile(`\(#(\d+)\)`)
	historyRE = regexp.MustCompile(`#\d+|\buntil\b|\bdistilled from\b|\bkept as\b|\breplaced\b|\babsorbed\b|\b20\d\d-\d\d-\d\d\b`)
)

// sentences splits text after each sentence's closing mark.
func sentences(text string) []string {
	var out []string
	start := 0
	rs := []rune(text)
	for i := 0; i < len(rs); i++ {
		if (rs[i] == '.' || rs[i] == '!' || rs[i] == '?') && i+1 < len(rs) && isSpace(rs[i+1]) {
			j := i + 1
			for j < len(rs) && isSpace(rs[j]) {
				j++
			}
			out = append(out, string(rs[start:i+1]))
			start = j
			i = j - 1
		}
	}
	return append(out, string(rs[start:]))
}

func isSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\v', '\f', '\r', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// History is one element's raw evidence for a backfill: the commits
// touching its carrier, the commits adding or removing a rule's lead-in,
// the pull requests they name, the VERSIONS.md rows naming those and the
// README's sentences that read as history.
func History(io WriteIO, git gitcmd.Repo, pack, element string) []string {
	c := prov.PackCarriers(pack, io)
	var files, triggers []string
	add := func(f string) {
		if !has(files, f) {
			files = append(files, f)
		}
	}
	for _, r := range append(append([]prov.Rule{}, c.Rules...), c.Guidelines...) {
		if r.Slug == element {
			add(r.File)
			triggers = append(triggers, r.Trigger)
		}
	}
	for _, s := range c.Skills {
		if s.Name == element {
			add(s.File)
		}
	}
	for _, ch := range c.Checks {
		if prov.ElementID(ch.ID) == element {
			add(ch.File)
		}
	}
	for _, t := range c.Tasks {
		if t.ID == element {
			add(t.Dir)
		}
	}
	if element == prov.PackElement {
		for _, f := range prov.ManifestFiles {
			add(pack + "/" + f)
		}
	}
	lines := []string{"# " + pack + " · " + element}
	if len(files) == 0 {
		return append(lines, "named by no carrier of this pack")
	}
	prs := map[int]bool{}
	note := func(log string) {
		for _, m := range prRef.FindAllStringSubmatch(log, -1) {
			if n, err := strconv.Atoi(m[1]); err == nil {
				prs[n] = true
			}
		}
	}
	for _, f := range files {
		lines = append(lines, "\n## commits touching "+f)
		log := gitOut(git, "log", "--follow", "--format=%h %as %s", "--", f)
		if strings.TrimSpace(log) == "" {
			lines = append(lines, "(none - is the clone shallow?)")
		} else {
			lines = append(lines, strings.TrimSpace(log))
		}
		note(log)
	}
	for _, t := range triggers {
		lines = append(lines, "\n## commits adding or removing \""+t+"\"")
		log := gitOut(git, "log", "--format=%h %as %s", "-S"+t, "--", pack)
		if strings.TrimSpace(log) == "" {
			lines = append(lines, "(none)")
		} else {
			lines = append(lines, strings.TrimSpace(log))
		}
		note(log)
	}
	nums := make([]int, 0, len(prs))
	for n := range prs {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	lines = append(lines, "\n## pull requests those commits name")
	if len(nums) == 0 {
		lines = append(lines, "(none)")
	} else {
		refs := make([]string, len(nums))
		for i, n := range nums {
			refs[i] = "#" + strconv.Itoa(n)
		}
		lines = append(lines, strings.Join(refs, " "))
	}
	if versions, _ := io.Read(pack + "/" + prov.Dir + "/" + VersionsFile); versions != "" {
		lines = append(lines, "\n## VERSIONS.md rows naming them")
		var rows []string
		for _, l := range strings.Split(versions, "\n") {
			if !strings.HasPrefix(l, "|") {
				continue
			}
			for _, n := range nums {
				if strings.Contains(l, "#"+strconv.Itoa(n)) {
					rows = append(rows, l)
					break
				}
			}
		}
		if len(rows) == 0 {
			lines = append(lines, "(none)")
		} else {
			lines = append(lines, strings.Join(rows, "\n"))
		}
	}
	if readme, _ := io.Read(pack + "/README.md"); readme != "" {
		lines = append(lines, "\n## README sentences that read as history")
		var tells []string
		for _, s := range sentences(readme) {
			hit := historyRE.MatchString(s)
			for _, t := range triggers {
				hit = hit || strings.Contains(s, t)
			}
			if hit {
				tells = append(tells, "- "+strings.TrimSpace(spaces.ReplaceAllString(s, " ")))
			}
		}
		if len(tells) == 0 {
			lines = append(lines, "(none)")
		} else {
			lines = append(lines, strings.Join(tells, "\n"))
		}
	}
	return append(lines, "\n## tracker comments", "read the promote tracker and the extract issues on GitHub; git does not hold them")
}

// Usage is the verbs' usage text.
const Usage = `usage: cn provenance <command> …
  mark <pack>|--all [--dry-run]          markers, bodies and empty files for every carrier
  check <pack>|--all                     what each file is named by, and every fault
  append <pack> <element> [--kind K] [--date D] [--changed] [--backfill] < entry.md
  history <pack> <element>               one element's raw evidence from git, VERSIONS.md and the README
  apply <pack> <brief.md> [--backfill]   every entry fence of an edited brief, as one batch
  convert-references <pack>|--all        a references.md turned into entries, then deleted
  reduce <file> [--public]               a provenance file as it may cross into the canon
  --backfill                             the backfill's lane: entries dated in the past, written in
                                         date order over the file, creating one the marking pass
                                         could not. every other caller appends, and only at the end`

// Main runs one verb over the repository at root, Node's sentences on
// stdout and stderr, and returns the exit code: 1 for a fault or a
// refusal, 2 for a usage error.
func Main(args []string, root string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, Usage)
		return 2
	}
	command, rest := args[0], args[1:]
	valued := map[string]bool{"--kind": true, "--date": true, "--repo": true}
	flags := map[string]bool{}
	values := map[string]string{}
	var positional []string
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		if valued[a] {
			if i+1 < len(rest) {
				values[a] = rest[i+1]
			}
			i++
			continue
		}
		if strings.HasPrefix(a, "--") {
			flags[a] = true
			continue
		}
		positional = append(positional, a)
	}
	io := Checkout{Root: root}
	git := gitcmd.Repo{Dir: root}
	packsFor := func() []string {
		if flags["--all"] {
			return AllPacks(io)
		}
		id := ""
		if len(positional) > 0 {
			id = positional[0]
		}
		dir := ""
		if id != "" {
			var err error
			if dir, err = ResolvePack(io, id); err != nil {
				fmt.Fprintln(stderr, "provenance: "+err.Error())
				return nil
			}
		}
		if dir == "" {
			fmt.Fprintf(stderr, "no pack %q under %s\n", id, strings.Join(prov.PackRoots, " or "))
			return nil
		}
		return []string{dir}
	}
	print := func(lines []string) { fmt.Fprintln(stdout, strings.Join(lines, "\n")) }
	switch command {
	case "mark":
		packs := packsFor()
		if packs == nil {
			return 2
		}
		var w WriteIO = io
		if flags["--dry-run"] {
			w = NewOverlay(io)
		}
		lines, err := Mark(w, packs, flags["--dry-run"])
		if err != nil {
			fmt.Fprintln(stderr, "provenance: "+err.Error())
			return 1
		}
		print(lines)
		return 0
	case "check":
		packs := packsFor()
		if packs == nil {
			return 2
		}
		lines, faults := Check(io, git, packs)
		print(lines)
		if faults > 0 {
			return 1
		}
		return 0
	case "append":
		packs := packsFor()
		if packs == nil {
			return 2
		}
		var elements []string
		if flags["--changed"] {
			elements = Changed(io, git, packs[0])
		} else if len(positional) > 1 {
			elements = positional[1:2]
		}
		if len(elements) == 0 {
			if flags["--changed"] {
				fmt.Fprintln(stderr, "the working tree changed no carrier of this pack")
			} else {
				fmt.Fprintln(stderr, "append needs an element id")
			}
			return 2
		}
		raw, err := readAll(stdin)
		if err != nil {
			fmt.Fprintln(stderr, "provenance: "+err.Error())
			return 1
		}
		written, problems := Append(io, packs[0], elements, raw, AppendOpts{Kind: values["--kind"], Date: values["--date"], Backfill: flags["--backfill"]})
		if len(problems) > 0 {
			fmt.Fprintln(stderr, strings.Join(problems, "\n"))
			return 1
		}
		out := make([]string, len(written))
		for i, f := range written {
			out[i] = f + ": appended"
		}
		print(out)
		return 0
	case "reduce":
		file := ""
		if len(positional) > 0 {
			file = positional[0]
		}
		text, ok := io.Read(file)
		if file == "" || !ok {
			fmt.Fprintln(stderr, "reduce needs a file")
			return 2
		}
		fmt.Fprint(stdout, ReduceFile(text, flags["--public"]))
		return 0
	case "convert-references":
		packs := packsFor()
		if packs == nil {
			return 2
		}
		var lines []string
		for _, p := range packs {
			lines = append(lines, ConvertReferences(p, io, ReferenceDates(git, p+"/"+ReferencesDoc), Today())...)
		}
		if len(lines) == 0 {
			lines = []string{"no references.md to convert"}
		}
		print(lines)
		return 0
	case "apply":
		packs := packsFor()
		if packs == nil {
			return 2
		}
		if len(positional) < 2 {
			fmt.Fprintln(stderr, "apply needs the brief file")
			return 2
		}
		brief := positional[1]
		if !filepath.IsAbs(brief) {
			brief = filepath.Join(root, brief)
		}
		raw, err := os.ReadFile(brief)
		if err != nil {
			fmt.Fprintln(stderr, "provenance: "+err.Error())
			return 1
		}
		backfill := flags["--backfill"]
		a := Apply(io, packs[0], string(raw), backfill)
		if len(a.Problems) > 0 {
			fmt.Fprintln(stderr, strings.Join(a.Problems, "\n"))
			return 1
		}
		verb := "appended"
		if backfill {
			verb = "written in date order"
		}
		out := []string{"nothing to append"}
		if len(a.Written) > 0 {
			out = out[:0]
			for _, f := range a.Written {
				out = append(out, f+": "+verb)
			}
		}
		if len(a.Created) > 0 {
			out = append(out, "created: "+strings.Join(a.Created, ", "))
		}
		if len(a.Superseded) > 0 {
			out = append(out, "the conversion's placeholder replaced by the batch's born: "+strings.Join(a.Superseded, "; "))
		}
		if len(a.Skipped) > 0 {
			var once []string
			for _, s := range a.Skipped {
				if !has(once, s) {
					once = append(once, s)
				}
			}
			out = append(out, "already in its file, skipped: "+strings.Join(once, ", "))
		}
		print(out)
		return 0
	case "history":
		packs := packsFor()
		if packs == nil {
			return 2
		}
		if len(positional) < 2 {
			fmt.Fprintln(stderr, "history needs an element id")
			return 2
		}
		print(History(io, git, packs[0], positional[1]))
		return 0
	}
	fmt.Fprintln(stderr, Usage)
	return 2
}

func readAll(r io.Reader) (string, error) {
	if r == nil {
		return "", nil
	}
	b, err := io.ReadAll(r)
	return string(b), err
}
