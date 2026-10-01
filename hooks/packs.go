package hooks

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

// SkillsDir is where Claude Code reads a project's skills.
const SkillsDir = ".claude/skills"

// mountMarker marks a skill directory cn mounted, holding the pack's id;
// a directory without it is a person's and is never touched.
const mountMarker = ".claudinite-mount"

var ruleLine = regexp.MustCompile(`(?m)^[-*] `)

// jsDirs are the pack folders whose .mjs files the Node engine ran.
var jsDirs = []string{"worldRules", "workRules", "tasks", "skills"}

type assembled struct {
	rules     string
	notes     []string
	selfCheck string
}

type loadedPack struct {
	id, version string
	dir         string
	skills      []string
}

// assemble reads the declared packs in declared order: their rules, the
// lines naming what is not loaded or not run, the mounted skills and the
// self-check line. A repo with no settings file has no self-check line.
func assemble(repo, engine string) assembled {
	var a assembled
	if _, _, err := settings.Find(repo); err != nil {
		return a
	}
	declared, err := packset.Declared(repo)
	if err != nil {
		a.notes = append(a.notes, fmt.Sprintf("[cn] packs not loaded: %v", err))
		a.selfCheck = "[cn] packs 0/0 loaded"
		return a
	}
	var loaded []loadedPack
	var parts []string
	var rules strings.Builder
	for _, id := range declared.Declared {
		p, why := load(repo, id, engine)
		if why != "" {
			a.notes = append(a.notes, fmt.Sprintf("pack %s: not loaded: %s", id, why))
			parts = append(parts, id+": not loaded")
			continue
		}
		body, _ := os.ReadFile(filepath.Join(p.dir, "RULES.md"))
		fmt.Fprintf(&rules, "# %s %s\n", id, p.version)
		rules.Write(body)
		if len(body) > 0 && !bytes.HasSuffix(body, []byte("\n")) {
			rules.WriteString("\n")
		}
		rules.WriteString("\n")
		if hasJS(p.dir) {
			a.notes = append(a.notes, fmt.Sprintf("pack %s: its JavaScript checks and tasks are not run by this engine (phase 6)", id))
		}
		loaded = append(loaded, p)
		parts = append(parts, fmt.Sprintf("%s %s: rules %d skills %d", id, p.version, len(ruleLine.FindAll(body, -1)), len(p.skills)))
	}
	a.rules = rules.String()
	a.notes = append(a.notes, mount(repo, loaded)...)
	a.selfCheck = fmt.Sprintf("[cn] packs %d/%d loaded", len(loaded), len(declared.Declared))
	if len(parts) > 0 {
		a.selfCheck += " (" + strings.Join(parts, "; ") + ")"
	}
	return a
}

func load(repo, id, engine string) (loadedPack, string) {
	dir := packset.Tree(repo, id)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return loadedPack{}, packset.TreeRel(id) + " is missing"
	}
	m, err := packset.ReadManifest(dir)
	if err != nil {
		return loadedPack{}, err.Error()
	}
	// A development engine (0.0.0) loads every pack.
	if engine != "0.0.0" {
		min, err := version.ParseMinEngineVersion(m.MinEngineVersion)
		if err != nil {
			return loadedPack{}, err.Error()
		}
		if !min.Satisfies(engine) {
			return loadedPack{}, fmt.Sprintf("pack %s %s needs engine %s or newer; this is %s", id, m.Version, m.MinEngineVersion, engine)
		}
	}
	p := loadedPack{id: id, version: m.Version, dir: dir}
	matches, _ := filepath.Glob(filepath.Join(dir, "skills", "*", "SKILL.md"))
	for _, s := range matches {
		p.skills = append(p.skills, filepath.Base(filepath.Dir(s)))
	}
	sort.Strings(p.skills)
	return p, ""
}

func hasJS(dir string) bool {
	found := false
	for _, sub := range jsDirs {
		_ = filepath.WalkDir(filepath.Join(dir, sub), func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(path, ".mjs") {
				found = true
				return fs.SkipAll
			}
			return nil
		})
	}
	return found
}

// mount copies each loaded pack's skills to .claude/skills/<name>/SKILL.md,
// writing only what changed, and removes the mounts of skills no longer
// offered. It returns the lines naming conflicts.
func mount(repo string, packs []loadedPack) []string {
	var notes []string
	root := filepath.Join(repo, filepath.FromSlash(SkillsDir))
	owner := map[string]string{}
	var offeredBy = map[string][]string{}
	var order []string
	for _, p := range packs {
		for _, s := range p.skills {
			if _, ok := offeredBy[s]; !ok {
				order = append(order, s)
			}
			offeredBy[s] = append(offeredBy[s], p.id)
			if _, ok := owner[s]; !ok {
				owner[s] = p.id
			}
		}
	}
	dirOf := map[string]string{}
	for _, p := range packs {
		dirOf[p.id] = p.dir
	}
	for _, s := range order {
		by := offeredBy[s]
		if len(by) > 1 {
			notes = append(notes, fmt.Sprintf("[cn] skill %s is offered by %s; %s's is mounted", s, joinAnd(by), by[0]))
		}
		dst := filepath.Join(root, s)
		if _, err := os.Stat(dst); err == nil {
			if _, err := os.Stat(filepath.Join(dst, mountMarker)); err != nil {
				notes = append(notes, fmt.Sprintf("[cn] skill %s from pack %s is not mounted: %s/%s is not a pack's", s, owner[s], SkillsDir, s))
				continue
			}
		}
		src, err := os.ReadFile(filepath.Join(dirOf[owner[s]], "skills", s, "SKILL.md"))
		if err == nil {
			err = writeIfChanged(filepath.Join(dst, "SKILL.md"), src)
		}
		if err == nil {
			err = writeIfChanged(filepath.Join(dst, mountMarker), []byte(owner[s]+"\n"))
		}
		if err != nil {
			notes = append(notes, fmt.Sprintf("[cn] skill %s from pack %s is not mounted: %v", s, owner[s], err))
		}
	}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if !e.IsDir() || owner[e.Name()] != "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, e.Name(), mountMarker)); err == nil {
			if err := os.RemoveAll(filepath.Join(root, e.Name())); err != nil {
				notes = append(notes, fmt.Sprintf("[cn] stale skill mount %s not removed: %v", e.Name(), err))
			}
		}
	}
	return notes
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
