package packset

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/version"
)

// Where a member's own packs live: a local pack is tracked and declared as
// local/<name>; a temp pack is copied in for one session and active by
// presence.
const (
	LocalDir = ".claudinite/local/packs"
	TempDir  = ".claudinite/temp/packs"
)

// UserPackID is the canon pack whose declaration turns on the
// SessionStart step copying the person in front of a session's own pack
// in, as the temp pack CurrentUser.
const (
	UserPackID  = "claude-code-web-users-support"
	CurrentUser = "current_user"
)

// FleetPack is the engine's own fleet pack: its rules, skills, tasks and
// checks are carried in the binary and active wherever the settings hold
// a fleet block.
const FleetPack = "fleet"

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
}

// Token is the pack as a declaration names it.
func (p Pack) Token() string {
	switch p.Kind {
	case Local:
		return settings.LocalPrefix + p.ID
	case Temp:
		return "temp/" + p.ID
	case Engine:
		return string(Engine) + "/" + p.ID
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
// directory name, then the declared local packs by directory name, then
// the engine's own packs the settings turn on, written out first, then,
// when session is true, every other temp pack present, by directory name. A
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
	parsed, err := parsedSettings(repo)
	if err != nil {
		return Set{}, err
	}
	declared := parsed.Packs
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
	for _, e := range activeEmbedded(parsed) {
		token := string(Engine) + "/" + e.ID
		if isDeclared[e.ID] {
			s.NotLoaded = append(s.NotLoaded, NotLoaded{token, "its id is a declared pack's; the engine's own pack may not be shadowed"})
			continue
		}
		if err := materialize(repo, e); err != nil {
			s.NotLoaded = append(s.NotLoaded, NotLoaded{token, "could not be written under " + TempDir + ": " + err.Error()})
			continue
		}
		p, why := loadOne(repo, Engine, e.ID, TempDir+"/"+e.ID, engine)
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
			if _, err := readManifestAny(filepath.Join(repo, filepath.FromSlash(rel))); errors.Is(err, ErrNoManifest) {
				continue
			}
			if taken[name] {
				if !engineWrote(candidates, name) {
					s.NotLoaded = append(s.NotLoaded, NotLoaded{"temp/" + name, "its id is a tracked pack's; a copied pack may not shadow one"})
				}
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

// engineWrote reports whether TempDir/name is the engine's own pack this
// load wrote.
func engineWrote(candidates []Pack, name string) bool {
	for _, p := range candidates {
		if p.Kind == Engine && p.ID == name {
			return true
		}
	}
	return false
}

// LoadLocal loads the local pack name as Load would, or says why it
// would not load.
func LoadLocal(repo, name string) (Pack, error) {
	p, why := loadOne(repo, Local, name, LocalDir+"/"+name, "0.0.0")
	if why != "" {
		return Pack{}, errors.New(why)
	}
	return p, nil
}

func loadOne(repo string, kind Kind, id, rel, engine string) (Pack, string) {
	dir := filepath.Join(repo, filepath.FromSlash(rel))
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return Pack{}, rel + " is missing"
	}
	m, err := readManifestAny(dir)
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
			min, err := version.ParseMinEngineVersion(m.MinEngineVersion)
			if err != nil {
				return Pack{}, err.Error()
			}
			if !min.Satisfies(engine) {
				return Pack{}, fmt.Sprintf("pack %s %s needs engine %s or newer; this is %s", id, m.Version, m.MinEngineVersion, engine)
			}
		}
	}
	p := Pack{ID: id, Kind: kind, Dir: dir, Rel: rel, Version: m.Version, MinEngine: m.MinEngineVersion, Requires: m.Requires, Manifest: m}
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
