// Package packset reads what a member holds of its packs: the declaration
// in its settings and the vendored trees under .claudinite/shared/packs/.
// Hooks, verify, the checks build and the lifecycle commands share it, so
// they agree on what "declared" and "held" mean.
package packset

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/shared/descriptor"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
)

// Dir is where a member's vendored packs live, relative to its root.
const Dir = ".claudinite/shared/packs"

// TreeRel is pack id's vendored tree, relative to the repo root, with
// forward slashes.
func TreeRel(id string) string { return Dir + "/" + id }

// Tree is pack id's vendored tree under repo.
func Tree(repo, id string) string { return filepath.Join(repo, filepath.FromSlash(TreeRel(id))) }

// Manifest is what the engine reads of a pack's manifest, pack.json,
// pack.yaml or pack.toml.
type Manifest struct {
	// File is the manifest's file name.
	File             string
	ID               string
	Version          string
	MinEngineVersion string
	Requires         []string
	// Prose is the prose file the manifest names; ProseSet is false when
	// the manifest is silent and the convention (RULES.md when present)
	// applies, and Prose "" with ProseSet means prose: null.
	Prose    string
	ProseSet bool
	// Skills is the manifest's subset; SkillsSet is false when the
	// convention (every skills/*/ directory) applies.
	Skills    []string
	SkillsSet bool
	// GitHubActions are the named GitHub actions the pack's task scripts
	// take through the SDK.
	GitHubActions []string
	// Engine is true when the pack's tasks are the engine's own, run under
	// the license; absent is false.
	Engine bool
	// Retired are the retired keys a local or temp pack's manifest still
	// carries, in RetiredKeys order; the reader ignores them.
	Retired []string
}

// RetiredKeys are the Node manifest spec's retired fields, which a local
// or temp pack's manifest may still carry: the fingerprint relevanceDetector
// replaced (#2374) and the pack contributions (#2395). Nothing reads them;
// a canon manifest carrying one fails as an unknown key.
//
// @legacy-tolerance advisory:local-pack-shape retire:#51
var RetiredKeys = []string{"detect", "marker", "contributes", "contributedRules"}

// ManifestName is the manifest's descriptor name.
const ManifestName = "pack"

// ModuleManifest is the Node engine's module spelling of the manifest,
// which this engine does not read.
const ModuleManifest = "pack.mjs"

// ErrModuleManifest is a tree whose only manifest is pack.mjs.
var ErrModuleManifest = errors.New("pack.mjs is a module manifest, which this engine does not read; write pack.json")

// ErrNoManifest is returned when a tree holds no pack manifest at all.
var ErrNoManifest = errors.New("holds no pack manifest (pack.json, pack.yaml or pack.toml)")

// ManifestSchema is the pack manifest's closed key vocabulary.
var ManifestSchema = descriptor.Schema{Name: "pack manifest", Keys: map[string]descriptor.Kind{
	"id":                  descriptor.String,
	"version":             descriptor.String,
	"minEngineVersion":    descriptor.String,
	"requires":            descriptor.StringList,
	"prose":               descriptor.StringOrNull,
	"skills":              descriptor.StringList,
	"pitch":               descriptor.String,
	"ruleRoutingGuidance": descriptor.Object,
	"relevanceDetector":   descriptor.ObjectOrNull,
	"questions":           descriptor.List,
	"seedOps":             descriptor.List,
	"adoptionHandover":    descriptor.List,
	"env":                 descriptor.Object,
	"badge":               descriptor.String,
	"seededByDefault":     descriptor.Bool,
	"hidden":              descriptor.Bool,
	"githubActions":       descriptor.StringList,
	"engine":              descriptor.Bool,
}}

// ManifestFiles are the manifest's three spellings.
func ManifestFiles() []string {
	var out []string
	for _, f := range descriptor.Formats {
		out = append(out, ManifestName+"."+string(f))
	}
	return out
}

// IsManifestFile reports whether a file name is one of the manifest's
// spellings.
func IsManifestFile(name string) bool {
	for _, n := range ManifestFiles() {
		if n == name {
			return true
		}
	}
	return false
}

// ReadManifest reads dir's one manifest. A version is required: a canon
// pack's tree always has one, and Load waives it for a local or temp pack.
func ReadManifest(dir string) (Manifest, error) {
	m, err := readManifestAny(dir, false)
	if err == nil && m.Version == "" {
		return Manifest{}, fmt.Errorf("%s has no version", m.File)
	}
	return m, err
}

// ReadOwnManifest reads a local or temp pack's manifest: no version is
// required, and the retired keys are read and listed in Retired.
func ReadOwnManifest(dir string) (Manifest, error) { return readManifestAny(dir, true) }

func readManifestAny(dir string, own bool) (Manifest, error) {
	path, _, err := descriptor.Find(dir, ManifestName)
	if errors.Is(err, descriptor.ErrAbsent) {
		if st, e := os.Stat(filepath.Join(dir, ModuleManifest)); e == nil && st.Mode().IsRegular() {
			return Manifest{}, ErrModuleManifest
		}
		return Manifest{}, ErrNoManifest
	}
	if err != nil {
		return Manifest{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	return parseManifestOf(filepath.Base(path), raw, own)
}

// ParseManifestFile reads a manifest's bytes, its format named by its file
// name; a version is required.
func ParseManifestFile(name string, raw []byte) (Manifest, error) {
	m, err := parseManifest(name, raw)
	if err == nil && m.Version == "" {
		return Manifest{}, fmt.Errorf("%s has no version", name)
	}
	return m, err
}

// ParseManifest reads pack.json's bytes.
func ParseManifest(raw []byte) (Manifest, error) { return ParseManifestFile("pack.json", raw) }

func parseManifest(name string, raw []byte) (Manifest, error) {
	return parseManifestOf(name, raw, false)
}

func parseManifestOf(name string, raw []byte, own bool) (Manifest, error) {
	f := descriptor.FormatOf(name)
	if f == "" {
		return Manifest{}, fmt.Errorf("%s is not a manifest spelling", name)
	}
	v, err := descriptor.ParseBytes(raw, f)
	if err != nil {
		return Manifest{}, fmt.Errorf("%s: %w", name, err)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return Manifest{}, fmt.Errorf("%s must hold an object", name)
	}
	var retired []string
	if own {
		for _, k := range RetiredKeys {
			if _, ok := obj[k]; ok {
				retired = append(retired, k)
				delete(obj, k)
			}
		}
	}
	if errs := ManifestSchema.Validate(obj); len(errs) > 0 {
		var s []string
		for _, e := range errs {
			s = append(s, e.Error())
		}
		return Manifest{}, fmt.Errorf("%s: %s", name, strings.Join(s, "; "))
	}
	m := Manifest{File: name, Retired: retired}
	m.ID, _ = obj["id"].(string)
	m.Version, _ = obj["version"].(string)
	m.MinEngineVersion, _ = obj["minEngineVersion"].(string)
	m.Requires = stringList(obj["requires"])
	m.GitHubActions = stringList(obj["githubActions"])
	m.Engine, _ = obj["engine"].(bool)
	if p, ok := obj["prose"]; ok {
		m.ProseSet = true
		m.Prose, _ = p.(string)
	}
	if s, ok := obj["skills"]; ok {
		m.SkillsSet = true
		m.Skills = stringList(s)
	}
	return m, nil
}

func stringList(v any) []string {
	l, _ := v.([]any)
	var out []string
	for _, e := range l {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// Declared reads the repo's settings file and its packs block.
func Declared(repo string) (settings.Packs, error) {
	path, f, err := settings.Find(repo)
	if err != nil {
		return settings.Packs{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return settings.Packs{}, err
	}
	p, err := settings.ReadPacks(raw, f)
	if err != nil {
		return settings.Packs{}, fmt.Errorf("%s: %w", settings.RelPath(f), err)
	}
	return p, nil
}

// Vendored lists the ids of the trees under Dir, sorted.
func Vendored(repo string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(repo, filepath.FromSlash(Dir)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}
