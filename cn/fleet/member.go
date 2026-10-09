package fleet

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/descriptor"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/settings"
)

// Shape is which engine a repository's tree is shaped for.
type Shape string

// The shapes a tree can take.
const (
	// ShapeCn is a tree with exactly one .claudinite/settings.*.
	ShapeCn Shape = "cn"
	// ShapeNode is a tree with a root .claudinite-settings.json: a Node
	// engine member, covered and never measured until phase 9 moves it.
	ShapeNode Shape = "node"
	// ShapeNone is neither: not a member.
	ShapeNone Shape = "none"
)

// NodeSettingsFile is the Node engine's declaration.
const NodeSettingsFile = ".claudinite-settings.json"

// Member is what one repository's tree says about it, read over the
// token: its shape, its declaration, and for a cn member the pin and the
// version of each declared canon pack its mount holds.
type Member struct {
	Repo          string
	DefaultBranch string
	Shape         Shape
	// Format and File are the settings file a cn member carries; File is
	// the Node declaration's for a node member.
	Format settings.Format
	File   *File
	// Packs and Pin are a cn member's; PinErr says why a pin did not read.
	Packs  settings.Packs
	Pin    settings.Engine
	PinErr string
	// Held is each declared canon pack's manifest version in the mount,
	// absent where the mount holds none.
	Held map[string]string
	// Node is a node member's parsed declaration.
	Node    any
	Dormant bool
}

// Covered reports whether the repository is a member.
func (m Member) Covered() bool { return m.Shape != ShapeNone }

// SettingsPath is the declaration's path in the tree.
func (m Member) SettingsPath() string {
	switch m.Shape {
	case ShapeCn:
		return settings.RelPath(m.Format)
	case ShapeNode:
		return NodeSettingsFile
	}
	return ""
}

// ReadMember reads repo's shape and declaration. A tree whose declaration
// cannot be read or parsed is an error, never uncovered: a file that
// cannot be read says nothing.
func ReadMember(gh GH, repo, defaultBranch string) (Member, error) {
	m := Member{Repo: repo, DefaultBranch: defaultBranch, Shape: ShapeNone}
	entries, found, err := ListDir(gh, repo, ".claudinite")
	if err != nil {
		return m, err
	}
	var formats []settings.Format
	if found {
		for _, e := range entries {
			for _, f := range settings.Formats {
				if e.Name == "settings."+string(f) && e.Type != "dir" {
					formats = append(formats, f)
				}
			}
		}
	}
	switch {
	case len(formats) > 1:
		return m, fmt.Errorf("carries %d settings files under .claudinite/; exactly one of settings.yaml, settings.toml or settings.json may exist", len(formats))
	case len(formats) == 1:
		return readCn(gh, m, formats[0])
	}
	f, err := ReadFile(gh, repo, NodeSettingsFile)
	if err != nil || f == nil {
		return m, err
	}
	var decl any
	if err := json.Unmarshal([]byte(f.Text), &decl); err != nil {
		return m, fmt.Errorf("unparsable %s: %v", NodeSettingsFile, err)
	}
	m.Shape, m.File, m.Node, m.Dormant = ShapeNode, f, decl, IsDormant(decl)
	return m, nil
}

func readCn(gh GH, m Member, f settings.Format) (Member, error) {
	rel := settings.RelPath(f)
	file, err := ReadFile(gh, m.Repo, rel)
	if err != nil {
		return m, err
	}
	if file == nil {
		return m, fmt.Errorf("%s is listed and could not be read", rel)
	}
	packs, err := settings.ReadPacks([]byte(file.Text), f)
	if err != nil {
		return m, fmt.Errorf("unparsable %s: %v", rel, err)
	}
	m.Shape, m.Format, m.File, m.Packs = ShapeCn, f, file, packs
	if pin, err := settings.ReadEngine([]byte(file.Text), f); err != nil {
		m.PinErr = err.Error()
	} else {
		m.Pin = pin
	}
	for _, e := range packs.Entries {
		if !e.Local && e.ID == TasksPackID {
			m.Dormant = EntryDormant(e.Config)
		}
	}
	m.Held = map[string]string{}
	for _, id := range packs.Declared {
		v, err := heldVersion(gh, m.Repo, id)
		if err != nil {
			return m, err
		}
		if v != "" {
			m.Held[id] = v
		}
	}
	return m, nil
}

// heldVersion is the version of id's manifest in repo's mount, "" when
// the mount holds no readable one.
func heldVersion(gh GH, repo, id string) (string, error) {
	for _, name := range packset.ManifestFiles() {
		f, err := ReadFile(gh, repo, packset.TreeRel(id)+"/"+name)
		if err != nil {
			return "", err
		}
		if f == nil {
			continue
		}
		v, err := descriptor.ParseBytes([]byte(f.Text), descriptor.FormatOf(name))
		if err != nil {
			return "", nil
		}
		o, _ := v.(map[string]any)
		s, _ := o["version"].(string)
		return s, nil
	}
	return "", nil
}

// NodeStamp is what a Node declaration stamps: its engine version and
// each entry's version, as one comparable string. The update lever
// follows a node member by this moving.
func NodeStamp(decl any) string {
	cfg, _ := decl.(map[string]any)
	out := map[string]any{"engineVersion": cfg["engineVersion"]}
	packs := map[string]any{}
	entries, _ := cfg["packs"].([]any)
	for _, e := range entries {
		if o, ok := e.(map[string]any); ok {
			if id, ok := o["id"].(string); ok && o["version"] != nil {
				packs[id] = o["version"]
			}
		}
	}
	out["packs"] = packs
	raw, _ := json.Marshal(out)
	return string(raw)
}

// Repo is a repository as the owner's listing returns it.
type Repo struct {
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	Owner    struct {
		Login string `json:"login"`
		ID    int64  `json:"id"`
	} `json:"owner"`
	Archived      bool   `json:"archived"`
	Fork          bool   `json:"fork"`
	DefaultBranch string `json:"default_branch"`
}

// Lower is the repository's lowercased owner/name.
func (r Repo) Lower() string { return strings.ToLower(r.FullName) }

// Branch is the default branch, main when the listing names none.
func (r Repo) Branch() string {
	if r.DefaultBranch == "" {
		return "main"
	}
	return r.DefaultBranch
}

// Enumerate is every repository owner owns that the token sees, by name.
// None is an error: a wrong token user or scope, never an empty fleet.
func Enumerate(gh GH, owner string) ([]Repo, error) {
	raw, err := Paged(gh, "/user/repos?affiliation=owner")
	if err != nil {
		return nil, err
	}
	owner = strings.ToLower(owner)
	var out []Repo
	for _, r := range raw {
		var repo Repo
		if json.Unmarshal(r, &repo) == nil && strings.ToLower(repo.Owner.Login) == owner {
			out = append(out, repo)
		}
	}
	if len(out) == 0 {
		return nil, noOwned{owner}
	}
	SortRepos(out)
	return out, nil
}

// SortRepos orders repositories by name, case folded first.
func SortRepos(rs []Repo) {
	sort.SliceStable(rs, func(i, j int) bool {
		a, b := strings.ToLower(rs[i].Name), strings.ToLower(rs[j].Name)
		if a != b {
			return a < b
		}
		return rs[i].Name < rs[j].Name
	})
}

// Scopes a repository can fall under before anything is read of it.
const (
	ScopeHome = "home"
	// ScopeOutOfOwner is a repository some other account owns: the fleet
	// reaches no further than its owner.
	ScopeOutOfOwner = "out-of-owner"
	ScopeArchived   = "archived"
	ScopeFork       = "fork"
	ScopeExcluded   = "excluded"
	ScopeIn         = "in"
)

// Scope is where r stands before its tree is read: the manager itself,
// another owner's, archived, a fork, excluded, or in. There is no canon
// row.
func Scope(r Repo, home string, c Config) string {
	switch {
	case r.Lower() == strings.ToLower(home):
		return ScopeHome
	case !c.Owns(r.Lower()):
		return ScopeOutOfOwner
	case r.Archived:
		return ScopeArchived
	case r.Fork:
		return ScopeFork
	case c.Excluded(r.Lower()):
		return ScopeExcluded
	}
	return ScopeIn
}

type noOwned struct{ owner string }

func (e noOwned) Error() string {
	return "enumeration returned no repos owned by " + e.owner + " — wrong token user or scope"
}

func (e noOwned) Is(target error) bool { return target == ErrNoOwnedRepos }
