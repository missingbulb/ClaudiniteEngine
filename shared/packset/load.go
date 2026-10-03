package packset

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

// Where a member's own packs live: a local pack is tracked and declared as
// local/<name>; a temp pack is copied in for one session and active by
// presence.
const (
	LocalDir = ".claudinite/local/packs"
	TempDir  = ".claudinite/temp/packs"
)

// ProseFile is the prose a pack carries by convention.
const ProseFile = "RULES.md"

// Kind is where a pack came from.
type Kind string

const (
	Canon Kind = "canon"
	Local Kind = "local"
	Temp  Kind = "temp"
)

// Pack is one active pack with the manifest conventions applied.
type Pack struct {
	ID   string
	Kind Kind
	// Dir is the pack's directory under the repo.
	Dir string
	// Rel is Dir relative to the repo, with forward slashes.
	Rel       string
	Version   string
	MinEngine string
	// Prose is the prose file's name in Dir, or "" for none.
	Prose    string
	Skills   []string
	Requires []string
	Manifest Manifest
	// JSRules are a local or temp pack's JavaScript rules, relative to
	// Dir: worldRules/*.mjs, workRules/*.mjs and skills/*/checks.mjs. The
	// pack loads, and none of them runs.
	JSRules []string
}

// Token is the pack as a declaration names it.
func (p Pack) Token() string {
	switch p.Kind {
	case Local:
		return settings.LocalPrefix + p.ID
	case Temp:
		return "temp/" + p.ID
	}
	return p.ID
}

// ProsePath is the prose file's path, or "" when the pack has none on disk.
func (p Pack) ProsePath() string {
	if p.Prose == "" {
		return ""
	}
	path := filepath.Join(p.Dir, filepath.FromSlash(p.Prose))
	if st, err := os.Stat(path); err != nil || !st.Mode().IsRegular() {
		return ""
	}
	return path
}

// NotLoaded is a declared or present pack the engine did not load, and why.
type NotLoaded struct {
	Token, Why string
}

// Set is what Load found.
type Set struct {
	Declared  settings.Packs
	Packs     []Pack
	NotLoaded []NotLoaded
	// Unmet are loaded packs whose requires names an undeclared pack: the
	// Node engine resolves requires when the declaration is written and
	// loads the pack regardless, and so does this one.
	Unmet []NotLoaded
}

// Load reads the repo's declaration and returns its active packs in the
// order the Node engine's registry gives them: the declared canon packs by
// directory name, then the declared local packs by directory name, then,
// when session is true, every temp pack present, by directory name. A
// pack whose requires names an undeclared pack loads, and is recorded in
// Unmet. A development engine (0.0.0) skips the minEngineVersion check.
// It returns an error only when the declaration cannot be read. Under
// Memoize the first answer stands for the process.
func Load(repo, engine string, session bool) (Set, error) {
	l := Remember(loadKey(repo, engine, session), func() any {
		s, err := load(repo, engine, session)
		return loaded{s, err}
	}).(loaded)
	return l.set, l.err
}

func load(repo, engine string, session bool) (Set, error) {
	declared, err := Declared(repo)
	if err != nil {
		return Set{}, err
	}
	s := Set{Declared: declared}
	canon := append([]string{}, declared.Declared...)
	sort.Strings(canon)
	local := append([]string{}, declared.Local...)
	sort.Strings(local)
	isCanon := map[string]bool{}
	isDeclared := map[string]bool{}
	for _, id := range canon {
		isCanon[id], isDeclared[id] = true, true
	}
	for _, name := range local {
		isDeclared[name] = true
	}
	var candidates []Pack
	for _, id := range canon {
		p, why := loadOne(repo, Canon, id, TreeRel(id), engine)
		if why != "" {
			s.NotLoaded = append(s.NotLoaded, NotLoaded{id, why})
			continue
		}
		candidates = append(candidates, p)
	}
	for _, name := range local {
		token := settings.LocalPrefix + name
		if isCanon[name] {
			s.NotLoaded = append(s.NotLoaded, NotLoaded{token, "its id is the declared canon pack " + name + "'s; a local pack may not shadow a canon pack"})
			continue
		}
		p, why := loadOne(repo, Local, name, LocalDir+"/"+name, engine)
		if why != "" {
			s.NotLoaded = append(s.NotLoaded, NotLoaded{token, why})
			continue
		}
		candidates = append(candidates, p)
	}
	if session {
		taken := map[string]bool{}
		for _, p := range candidates {
			taken[p.ID] = true
		}
		for _, name := range subdirs(filepath.Join(repo, filepath.FromSlash(TempDir))) {
			rel := TempDir + "/" + name
			if _, err := readManifestAny(filepath.Join(repo, filepath.FromSlash(rel)), true); errors.Is(err, ErrNoManifest) {
				continue
			}
			if taken[name] {
				s.NotLoaded = append(s.NotLoaded, NotLoaded{"temp/" + name, "its id is a tracked pack's; a copied pack may not shadow one"})
				continue
			}
			p, why := loadOne(repo, Temp, name, rel, engine)
			if why != "" {
				s.NotLoaded = append(s.NotLoaded, NotLoaded{"temp/" + name, why})
				continue
			}
			candidates = append(candidates, p)
		}
	}
	for _, p := range candidates {
		missing := ""
		for _, r := range p.Requires {
			if !isDeclared[r] {
				missing = r
				break
			}
		}
		if missing != "" {
			s.Unmet = append(s.Unmet, NotLoaded{p.Token(), fmt.Sprintf("requires %s, which is not declared", missing)})
		}
		s.Packs = append(s.Packs, p)
	}
	return s, nil
}

func loadOne(repo string, kind Kind, id, rel, engine string) (Pack, string) {
	dir := filepath.Join(repo, filepath.FromSlash(rel))
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return Pack{}, rel + " is missing"
	}
	m, err := readManifestAny(dir, kind != Canon)
	if err != nil {
		return Pack{}, err.Error()
	}
	if m.ID != "" && m.ID != id {
		return Pack{}, fmt.Sprintf("%s names id %q but its directory is %q", m.File, m.ID, id)
	}
	if kind == Canon {
		if m.Version == "" {
			return Pack{}, m.File + " has no version"
		}
		if engine != "0.0.0" {
			// A Node engine floor was vendored before the shelf named the cn
			// floor: the pack keeps running until the update replaces it,
			// and verify names it.
			min, err := version.ParseMinEngineVersion(m.MinEngineVersion)
			if err != nil && !errors.Is(err, version.ErrNodeEngine) {
				return Pack{}, err.Error()
			}
			if err == nil && !min.Satisfies(engine) {
				return Pack{}, fmt.Sprintf("pack %s %s needs engine %s or newer; this is %s", id, m.Version, m.MinEngineVersion, engine)
			}
		}
	}
	p := Pack{ID: id, Kind: kind, Dir: dir, Rel: rel, Version: m.Version, MinEngine: m.MinEngineVersion, Requires: m.Requires, Manifest: m}
	if kind != Canon {
		p.JSRules = JSRules(dir)
	}
	switch {
	case m.ProseSet:
		p.Prose = m.Prose
	default:
		if st, err := os.Stat(filepath.Join(dir, ProseFile)); err == nil && st.Mode().IsRegular() {
			p.Prose = ProseFile
		}
	}
	dirs := subdirs(filepath.Join(dir, "skills"))
	if m.SkillsSet {
		have := map[string]bool{}
		for _, d := range dirs {
			have[d] = true
		}
		for _, s := range m.Skills {
			if !have[s] {
				return Pack{}, fmt.Sprintf("%s names a skill %q with no skills/%s/ directory", m.File, s, s)
			}
		}
		p.Skills = m.Skills
	} else {
		p.Skills = dirs
	}
	return p, ""
}

// JSRules lists the Node engine's coded rules under a pack directory,
// relative to it, sorted: worldRules/*.mjs, workRules/*.mjs and
// skills/*/checks.mjs.
//
// @legacy-tolerance advisory:local-pack-shape retire:#53
func JSRules(dir string) []string {
	var out []string
	for _, scope := range []string{"worldRules", "workRules"} {
		entries, _ := os.ReadDir(filepath.Join(dir, scope))
		for _, e := range entries {
			if !e.IsDir() && filepath.Ext(e.Name()) == ".mjs" {
				out = append(out, scope+"/"+e.Name())
			}
		}
	}
	for _, s := range subdirs(filepath.Join(dir, "skills")) {
		if st, err := os.Stat(filepath.Join(dir, "skills", s, "checks.mjs")); err == nil && st.Mode().IsRegular() {
			out = append(out, "skills/"+s+"/checks.mjs")
		}
	}
	sort.Strings(out)
	return out
}

func subdirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}
