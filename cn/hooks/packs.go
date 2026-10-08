package hooks

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/descriptor"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/interview"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/skillfm"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/taskspec"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/workitem"
)

// SkillsDir is where Claude Code reads a project's skills.
const SkillsDir = ".claude/skills"

// mountMarker marks a skill directory cn mounted, holding the pack's id;
// a directory without it is a person's and is never touched.
const mountMarker = ".claudinite-mount"

var ruleLine = regexp.MustCompile(`(?m)^[-*] `)

type assembled struct {
	notes     []string
	selfCheck string
	// skills are the mounted skills' metadata, by name.
	skills map[string]skillfm.Meta
	// tasks is whether the canon tasks pack loaded.
	tasks bool
}

// mountedSkill is one skill a loaded pack offers.
type mountedSkill struct {
	name, pack, dir string
}

// assemble loads the active packs (canon, local, then temp) and mounts
// their skills, returning the lines naming what is not loaded or not run
// and the self-check line. A repo with no settings file has no self-check
// line. The prose reaches the session through the rules index, not here.
func assemble(repo, engine string) assembled {
	var a assembled
	if _, _, err := settings.Find(repo); err != nil {
		return a
	}
	set, err := packset.Load(repo, engine, true)
	if err != nil {
		a.notes = append(a.notes, fmt.Sprintf("[cn] packs not loaded: %v", err))
		a.selfCheck = "[cn] packs 0/0 loaded"
		return a
	}
	var parts []string
	var offered []mountedSkill
	for _, p := range set.Packs {
		if p.Kind == packset.Canon && p.ID == workitem.TasksPackID {
			a.tasks = true
		}
		var body []byte
		if path := p.ProsePath(); path != "" {
			body, _ = os.ReadFile(path)
		}
		if hasJSChecks(p.Dir) {
			a.notes = append(a.notes, fmt.Sprintf("pack %s: its JavaScript checks (worldRules/, workRules/, skills/*/checks.mjs) are not run by this engine; its declared checks are", p.Token()))
		}
		for _, task := range moduleTasks(p.Dir) {
			a.notes = append(a.notes, fmt.Sprintf("pack %s: tasks/%s is declared by a task.mjs, which this engine does not read, so it never runs; write its task.json", p.Token(), task))
		}
		n := 0
		for _, s := range p.Skills {
			dir := filepath.Join(p.Dir, "skills", s)
			if st, err := os.Stat(filepath.Join(dir, "SKILL.md")); err == nil && st.Mode().IsRegular() {
				offered = append(offered, mountedSkill{s, p.Token(), dir})
				n++
			}
		}
		label := p.Token()
		if p.Version != "" {
			label += " " + p.Version
		}
		parts = append(parts, fmt.Sprintf("%s: rules %d skills %d", label, len(ruleLine.FindAll(body, -1)), n))
	}
	for _, n := range set.Unmet {
		a.notes = append(a.notes, fmt.Sprintf("pack %s: %s; declare it, or the rules that lean on it have nothing behind them", n.Token, n.Why))
	}
	for _, n := range set.NotLoaded {
		a.notes = append(a.notes, fmt.Sprintf("pack %s: not loaded: %s", n.Token, n.Why))
		parts = append(parts, n.Token+": not loaded")
	}
	var mountNotes []string
	a.skills, mountNotes = mount(repo, offered)
	a.notes = append(a.notes, mountNotes...)
	a.notes = append(a.notes, malformedNotes(set.Packs)...)
	if l := pendingLine(set); l != "" {
		a.notes = append(a.notes, l)
	}
	a.selfCheck = fmt.Sprintf("[cn] packs %d/%d loaded", len(set.Packs), len(set.Packs)+len(set.NotLoaded))
	if len(parts) > 0 {
		a.selfCheck += " (" + strings.Join(parts, "; ") + ")"
	}
	return a
}

// pendingLine is the one line naming the adoption questions the active
// packs ask and their entries do not answer; never a finding.
func pendingLine(set packset.Set) string {
	pending, _ := interview.State(set)
	var ids []string
	for _, p := range pending {
		for _, q := range p.Questions {
			ids = append(ids, p.Pack.Token()+"/"+q.ID)
		}
	}
	if len(ids) == 0 {
		return ""
	}
	return "[cn] adoption questions pending (" + strings.Join(ids, ", ") + "): in an interactive session, at a natural moment, ask the person and record each with cn settings answer; an unattended session ignores this"
}

// hasJSChecks reports whether a pack ships the Node engine's coded
// rules, which this engine does not run: rule modules or a skill's
// checks.mjs.
func hasJSChecks(dir string) bool {
	found := false
	for _, sub := range []string{"worldRules", "workRules"} {
		_ = filepath.WalkDir(filepath.Join(dir, sub), func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(path, ".mjs") {
				found = true
				return fs.SkipAll
			}
			return nil
		})
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "skills", "*", "checks.mjs")); len(m) > 0 {
		found = true
	}
	return found
}

// moduleTasks are a pack's task folders declared by the Node engine's
// task.mjs alone, sorted. Task discovery reads only a task descriptor, so
// these never run; a folder with one runs, its .mjs workers and
// preconditions included.
func moduleTasks(dir string) []string {
	entries, _ := os.ReadDir(filepath.Join(dir, "tasks"))
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		task := filepath.Join(dir, "tasks", e.Name())
		if st, err := os.Stat(filepath.Join(task, "task.mjs")); err != nil || !st.Mode().IsRegular() {
			continue
		}
		if _, _, err := descriptor.Find(task, taskspec.DeclarationName); errors.Is(err, descriptor.ErrAbsent) {
			out = append(out, e.Name())
		}
	}
	return out
}

// mount copies each offered skill to .claude/skills/<name>/SKILL.md, the
// first pack offering a name winning (canon before local before temp),
// writing only what changed, and removes the mounts of skills no longer
// offered. It returns the mounted skills' frontmatter and the lines naming
// conflicts.
func mount(repo string, offered []mountedSkill) (map[string]skillfm.Meta, []string) {
	var notes []string
	metas := map[string]skillfm.Meta{}
	root := filepath.Join(repo, filepath.FromSlash(SkillsDir))
	first := map[string]mountedSkill{}
	offeredBy := map[string][]string{}
	var order []string
	for _, s := range offered {
		if _, ok := first[s.name]; !ok {
			first[s.name] = s
			order = append(order, s.name)
		}
		offeredBy[s.name] = append(offeredBy[s.name], s.pack)
	}
	for _, name := range order {
		s := first[name]
		if by := offeredBy[name]; len(by) > 1 {
			notes = append(notes, fmt.Sprintf("[cn] skill %s is offered by %s; %s's is mounted", name, joinAnd(by), by[0]))
		}
		dst := filepath.Join(root, name)
		if _, err := os.Stat(dst); err == nil {
			if _, err := os.Stat(filepath.Join(dst, mountMarker)); err != nil {
				notes = append(notes, fmt.Sprintf("[cn] skill %s from pack %s is not mounted: %s/%s is not a pack's", name, s.pack, SkillsDir, name))
				continue
			}
		}
		src, err := os.ReadFile(filepath.Join(s.dir, "SKILL.md"))
		if err == nil {
			err = writeIfChanged(filepath.Join(dst, "SKILL.md"), src)
		}
		if err == nil {
			err = writeIfChanged(filepath.Join(dst, mountMarker), []byte(s.pack+"\n"))
		}
		if err != nil {
			notes = append(notes, fmt.Sprintf("[cn] skill %s from pack %s is not mounted: %v", name, s.pack, err))
			continue
		}
		metas[name] = skillfm.Read(string(src))
	}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if _, ok := first[e.Name()]; !e.IsDir() || ok {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, e.Name(), mountMarker)); err == nil {
			if err := os.RemoveAll(filepath.Join(root, e.Name())); err != nil {
				notes = append(notes, fmt.Sprintf("[cn] stale skill mount %s not removed: %v", e.Name(), err))
			}
		}
	}
	return metas, notes
}

func joinAnd(ids []string) string {
	if len(ids) == 1 {
		return ids[0]
	}
	return strings.Join(ids[:len(ids)-1], ", ") + " and " + ids[len(ids)-1]
}

func writeIfChanged(path string, data []byte) error {
	if have, err := os.ReadFile(path); err == nil && bytes.Equal(have, data) {
		return nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
