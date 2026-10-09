package builtin

import (
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"regexp"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/checksdk"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/findings"
	"github.com/missingbulb/ClaudiniteEngine/cn/growth/provenance"
	"github.com/missingbulb/ClaudiniteEngine/cn/integrations/transcript"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/descriptor"
)

// The work half of the provenance convention: a change that alters a
// carrier's decision-bearing text carries the entry recording the
// decision, on the file the carrier names, in the same change. What owes:
// a rule or guideline whose marker-free text moved, a skill whose text
// moved beyond whitespace, the marking pass's own edits and (for a
// guidelines skill) its bullets, a check, task or manifest whose
// non-comment content moved. A pack with no provenance folder at the base
// is coming onto the convention, and owes nothing.
//
// A provenance file is meant to grow, and one that lost or altered a base
// line is advised, never refused: the rewrite that is right, the backfill,
// is the diff's to show. Several carriers may name one file; a carrier
// deleted while another still names its file owes an entry there, and
// only a file nothing names any more must end retired.
var provenanceChangeRecorded = declared.Builtin{
	ID:     "provenance-change-recorded",
	Pack:   "claudinite-growth",
	OnFail: "block",
	Since:  "2026-09-20",
	Tags:   []string{"work", "builtin", "claudinite-growth"},
	Doc:    "packs/claudinite-growth/skills/changing-pack-elements/SKILL.md",
	Why:    "the decision behind a change exists only in the head of whoever made it, at the moment they made it; a log appended later is a reconstruction",
}

func init() { register(&provenanceChangeRecorded, runProvenanceChangeRecorded) }

// baseIO reads the merge base, its directories off files.
type baseIO struct {
	ctx   *declared.Ctx
	files []string
}

func (b baseIO) Exists(p string) bool { _, ok := b.ctx.ReadBase(p); return ok }

func (b baseIO) Read(p string) (string, bool) { return b.ctx.ReadBase(p) }

func (b baseIO) ListDir(p string) ([]string, bool) { return provenance.ListFrom(b.files, p) }

func runProvenanceChangeRecorded(ctx *declared.Ctx, _ *transcript.Session) []findings.Finding {
	if onDefaultBranch(ctx) {
		return nil
	}
	changed := ctx.ChangedFiles()
	deleted := ctx.Deleted()
	packs := provenance.PackDirsIn(append(append([]string{}, changed...), deleted...))
	if len(packs) == 0 {
		return nil
	}
	head := provenance.ListIO{Files: unique(ctx.TrackedList(), ctx.UntrackedList()), ReadFile: ctx.Read, Has: ctx.Exists}
	atBase := map[string]bool{}
	for _, p := range ctx.ListBase() {
		atBase[p] = true
	}
	var baseFiles []string
	for _, p := range unique(ctx.TrackedList(), deleted) {
		for _, dir := range packs {
			if strings.HasPrefix(p, dir+"/") && atBase[p] {
				baseFiles = append(baseFiles, p)
				break
			}
		}
	}
	base := baseIO{ctx, baseFiles}
	touched := map[string]bool{}
	for _, f := range changed {
		touched[f] = true
	}
	b := provenanceChangeRecorded
	var out []findings.Finding
	for _, dir := range packs {
		now := provenance.PackCarriers(dir, head)
		before := provenance.PackCarriers(dir, base)
		listNow := provenance.Files(dir, head)
		filesNow := provenance.FileMap(listNow)
		filesBefore := provenance.FileMap(provenance.Files(dir, base))
		if len(filesBefore) == 0 {
			continue
		}
		fileOf := func(id string) string { return dir + "/" + provenance.Dir + "/" + provenance.FileOfID(id) }
		owes := func(id, file string, line int, what string) {
			h, ok := filesNow[id]
			if !ok || len(h.Entries) > len(filesBefore[id].Entries) {
				return
			}
			out = append(out, b.Finding(file, line,
				fmt.Sprintf("%s, and %s gained no entry in this change", what, fileOf(id)),
				fmt.Sprintf("append the entry that records the decision - its kind (reworded, strengthened, weakened, moved, converted, trigger-changed, policy-changed, severity-changed, split, merged) and why - through `cn provenance append %s %s`, reading the entry from stdin", path.Base(dir), id)))
		}

		// Rules and guidelines, by trigger; the base's bullets whatever
		// their skill's body said then.
		was := map[string]provenance.Rule{}
		for _, r := range before.Rules {
			was[r.Trigger] = r
		}
		for _, s := range before.Skills {
			for _, r := range s.Bullets {
				was[r.Trigger] = r
			}
		}
		for _, r := range append(append([]provenance.Rule{}, now.Rules...), now.Guidelines...) {
			id := r.Slug
			if id == "" {
				id = r.Skill
			}
			if id == "" || !touched[r.File] {
				continue
			}
			if w, ok := was[r.Trigger]; !ok {
				owes(id, r.File, r.Line, `"`+r.Trigger+`" is new`)
			} else if w.Text != r.Text {
				owes(id, r.File, r.Line, `"`+r.Trigger+`" reads differently from the base`)
			}
		}
		for _, s := range now.Skills {
			if !s.Present || !touched[s.File] {
				continue
			}
			bt, inBase := base.Read(s.File)
			ht, _ := head.Read(s.File)
			if inBase && (squash(bt) == squash(ht) || skillOnlyMarkedOrBodied(bt, ht) ||
				(s.Body == "guidelines" && sansBullets(bt) == sansBullets(ht))) {
				continue
			}
			owes(s.Name, s.File, 0, "skill "+s.Name+" changed")
		}
		seen := map[string]bool{}
		for _, c := range now.Checks {
			id := provenance.ElementOf(c)
			if seen[id] || !touched[c.File] {
				continue
			}
			seen[id] = true
			if bt, ok := base.Read(c.File); ok {
				ht, _ := head.Read(c.File)
				if checksdk.CommentOnly(c.File, &bt, &ht) {
					continue
				}
				if strings.HasSuffix(c.File, ".json") && !declarationChanged(bt, ht, c.ID) {
					continue
				}
			}
			owes(id, c.File, 0, "check "+c.ID+" changed")
		}
		for _, t := range now.Tasks {
			var files []string
			for _, f := range changed {
				if strings.HasPrefix(f, t.Dir+"/") {
					files = append(files, f)
				}
			}
			if len(files) == 0 {
				continue
			}
			commentsOnly := true
			for _, f := range files {
				bt, ok := base.Read(f)
				ht, _ := head.Read(f)
				if !ok || !checksdk.CommentOnly(f, &bt, &ht) {
					commentsOnly = false
				}
			}
			if !commentsOnly {
				owes(t.ID, files[0], 0, "task "+t.ID+" changed")
			}
		}
		if now.ManifestFile != "" {
			for _, m := range provenance.ManifestFiles {
				file := dir + "/" + m
				if !touched[file] {
					continue
				}
				if bt, ok := base.Read(file); ok {
					ht, _ := head.Read(file)
					if sameManifest(file, bt, ht) {
						continue
					}
				}
				owes(provenance.PackElement, file, 0, "the manifest changed")
				break
			}
		}

		for _, h := range listNow {
			id := h.ID
			bf, ok := filesBefore[id]
			if !touched[h.Path] || !ok || strings.TrimSpace(bf.Text) == "" {
				continue
			}
			if !strings.HasPrefix(h.Text, strings.TrimRight(bf.Text, " \t\r\n")) {
				out = append(out, b.Advice(h.Path, 0, provenance.FileOfID(id)+" lost or altered a line it had at the base - a provenance file is meant to grow",
					"a wrong entry is answered by a later entry: restore the base text and append what this change decides after the last entry, or leave it where the rewrite is the correct history (the backfill replacing what the conversion wrote) and let the diff be the record"))
			}
		}

		out = append(out, goneCarriers(b, dir, before, now, filesNow, filesBefore, owes, fileOf)...)
	}
	return out
}

// goneCarriers judges the carriers the change removed: one whose file
// another live carrier names owes an entry there, any other's file ends
// retired.
func goneCarriers(b declared.Builtin, dir string, before, now provenance.Carriers, filesNow, filesBefore map[string]provenance.File,
	owes func(id, file string, line int, what string), fileOf func(string) string) []findings.Finding {
	type goneOne struct{ id, what string }
	var gone []goneOne
	nowTriggers := map[string]bool{}
	for _, r := range append(append([]provenance.Rule{}, now.Rules...), now.Guidelines...) {
		nowTriggers[r.Trigger] = true
	}
	for _, r := range append(append([]provenance.Rule{}, before.Rules...), before.Guidelines...) {
		id, kind := r.Slug, "rule"
		if r.Skill != "" {
			kind = "guideline"
			if id == "" {
				id = r.Skill
			}
		}
		if id != "" && !nowTriggers[r.Trigger] {
			gone = append(gone, goneOne{id, kind + ` "` + r.Trigger + `"`})
		}
	}
	nowSkills := map[string]bool{}
	for _, s := range now.Skills {
		if s.Present {
			nowSkills[s.Name] = true
		}
	}
	for _, s := range before.Skills {
		if s.Present && !nowSkills[s.Name] {
			gone = append(gone, goneOne{s.Name, "skill " + s.Name})
		}
	}
	nowChecks := map[string]bool{}
	for _, c := range now.Checks {
		nowChecks[c.ID] = true
	}
	for _, c := range before.Checks {
		if !nowChecks[c.ID] {
			gone = append(gone, goneOne{provenance.ElementOf(c), "check " + c.ID})
		}
	}
	nowTasks := map[string]bool{}
	for _, t := range now.Tasks {
		nowTasks[t.ID] = true
	}
	for _, t := range before.Tasks {
		if !nowTasks[t.ID] {
			gone = append(gone, goneOne{t.ID, "task " + t.ID})
		}
	}
	named := map[string]bool{}
	for _, r := range append(append([]provenance.Rule{}, now.Rules...), now.Guidelines...) {
		if r.Slug != "" {
			named[r.Slug] = true
		}
	}
	for s := range nowSkills {
		named[s] = true
	}
	for _, c := range now.Checks {
		named[provenance.ElementOf(c)] = true
	}
	for t := range nowTasks {
		named[t] = true
	}
	if now.ManifestFile != "" {
		named[provenance.PackElement] = true
	}
	var out []findings.Finding
	for _, g := range gone {
		if named[g.id] {
			owes(g.id, fileOf(g.id), 0, g.what+" is gone from "+dir+" in this change")
			continue
		}
		f, ok := filesNow[g.id]
		if ok && len(f.Entries) > 0 && f.Entries[len(f.Entries)-1].Kind == "retired" && filesBefore[g.id].Status != "retired" {
			continue
		}
		out = append(out, b.Finding(fileOf(g.id), 0,
			g.what+" is gone from "+dir+" in this change, and its file's last entry is not retired",
			"append `## <date> · retired · <why>` to "+provenance.FileOfID(g.id)+" in the same change - the decision to remove is a decision, and the file stays"))
	}
	return out
}

var spaces = regexp.MustCompile(`\s+`)

func squash(t string) string { return strings.TrimSpace(spaces.ReplaceAllString(t, " ")) }

// sansBullets is a skill's text with its top-level bullet blocks cut out:
// what a guidelines skill says of itself beside the bullets judged one by
// one.
func sansBullets(text string) string {
	s := provenance.SkillShape(text)
	drop := map[int]bool{}
	for _, b := range s.Bullets {
		for i := b.Start; i <= b.End; i++ {
			drop[s.BodyOffset+i] = true
		}
	}
	var kept []string
	for i, l := range strings.Split(text, "\n") {
		if !drop[i] {
			kept = append(kept, l)
		}
	}
	return squash(strings.Join(kept, "\n"))
}

var markingEdits = []*regexp.Regexp{
	regexp.MustCompile(`(?m)^\s*body:\s*(workflow|guidelines)\s*$`),
	regexp.MustCompile(`(?m)^\s*usage:\s*$`),
	regexp.MustCompile(`(?m)^\s*expect:\s*[a-z]+\s*$`),
	regexp.MustCompile(`(?m)^\s*loads-per-sessions:\s*.*$`),
	regexp.MustCompile(`(?m)^metadata:\s*$`),
	regexp.MustCompile(`(?m)\s*\([a-z][a-z0-9]*(?:-[a-z0-9]+)+\)\s*$`),
	regexp.MustCompile(`(?m)\s*\(\d+(?:\s*,\s*\d+)*\)\s*$`),
}

// skillOnlyMarkedOrBodied reports whether a skill changed only by the
// marking pass's own edits - a body line, a usage block, markers on
// bullets - which only the corpus's maintenance reads.
func skillOnlyMarkedOrBodied(before, after string) bool {
	strip := func(t string) string {
		for _, re := range markingEdits {
			t = re.ReplaceAllString(t, "")
		}
		return squash(t)
	}
	return strip(before) == strip(after)
}

// declarationChanged compares one declaration of a declared-checks.json,
// parsed, by id.
func declarationChanged(before, after, id string) bool {
	find := func(t string) any {
		var list []map[string]any
		if json.Unmarshal([]byte(t), &list) != nil {
			return nil
		}
		for _, d := range list {
			if d["id"] == id {
				return d
			}
		}
		return nil
	}
	return !reflect.DeepEqual(find(before), find(after))
}

// sameManifest reports whether a manifest's change left its meaning
// alone: the same data for a descriptor (JSON, YAML, TOML), whose layout
// and comments are not data and of which JSON has none; comments alone for
// the Node engine's module.
func sameManifest(file, a, b string) bool {
	f := descriptor.FormatOf(file)
	if f == "" {
		return checksdk.CommentOnly(file, &a, &b)
	}
	x, errA := descriptor.ParseBytes([]byte(a), f)
	y, errB := descriptor.ParseBytes([]byte(b), f)
	return errA == nil && errB == nil && reflect.DeepEqual(x, y)
}
