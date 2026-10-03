// Package packset reads what a member holds of its packs: the declaration
// in its settings and the vendored trees under .claudinite/shared/packs/.
// Hooks, verify, the checks build and the lifecycle commands share it, so
// they agree on what "declared" and "held" mean.
package packset

import (
	"errors"
	"fmt"
	"os"
	"path"
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
	// Questions are the adoption questions the pack asks.
	Questions []Question
	// SeedOps are the files adoption writes once, when absent.
	SeedOps []SeedOp
	// Handover are the steps only a person can do after adoption.
	Handover []HandoverStep
}

// Question is one adoption question: a stable id the answer is recorded
// under, the prompt asked, and how the answer becomes config.
type Question struct {
	ID, Prompt, Distill string
}

// SeedOp is one file adoption writes when it is absent and the repo owns
// from then on: Template inside the pack, Dest in the repo.
type SeedOp struct {
	Template, Dest string
}

// HandoverStep is a step only a person can perform: what to do, what
// breaks while it is undone, and what shows it is done.
type HandoverStep struct {
	Step, Breaks, Done string
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
	if m.Questions, err = readQuestions(obj["questions"]); err != nil {
		return Manifest{}, fmt.Errorf("%s: %w", name, err)
	}
	if m.SeedOps, err = readSeedOps(obj["seedOps"]); err != nil {
		return Manifest{}, fmt.Errorf("%s: %w", name, err)
	}
	if m.Handover, err = readHandover(obj["adoptionHandover"]); err != nil {
		return Manifest{}, fmt.Errorf("%s: %w", name, err)
	}
	return m, nil
}

// readQuestions validates questions as the Node engine's packQuestions
// did at discovery: each an object with a non-empty string id and prompt,
// ids unique, distill text when present.
func readQuestions(v any) ([]Question, error) {
	list, _ := v.([]any)
	var out []Question
	seen := map[string]bool{}
	for i, e := range list {
		o, _ := e.(map[string]any)
		id, _ := o["id"].(string)
		prompt, _ := o["prompt"].(string)
		distill, isText := o["distill"].(string)
		switch {
		case o == nil || id == "" || prompt == "":
			return nil, fmt.Errorf("questions[%d] needs a non-empty string id and prompt", i)
		case o["distill"] != nil && !isText:
			return nil, fmt.Errorf("questions[%d].distill must be text", i)
		case seen[id]:
			return nil, fmt.Errorf("questions names id %q twice; ids are unique within a pack", id)
		}
		seen[id] = true
		out = append(out, Question{ID: id, Prompt: prompt, Distill: distill})
	}
	return out, nil
}

// readSeedOps validates seedOps: a template inside the pack and a dest
// inside the repo, never under the vendored tree.
func readSeedOps(v any) ([]SeedOp, error) {
	list, _ := v.([]any)
	var out []SeedOp
	for i, e := range list {
		o, _ := e.(map[string]any)
		tmpl, _ := o["template"].(string)
		dest, _ := o["dest"].(string)
		switch {
		case o == nil || tmpl == "" || dest == "":
			return nil, fmt.Errorf("seedOps[%d] needs a template and a dest", i)
		case !inside(tmpl):
			return nil, fmt.Errorf("seedOps[%d].template %q is not a path inside the pack", i, tmpl)
		case !inside(dest):
			return nil, fmt.Errorf("seedOps[%d].dest %q is not a path inside the repo", i, dest)
		case dest == ".claudinite/shared" || strings.HasPrefix(dest, ".claudinite/shared/"):
			return nil, fmt.Errorf("seedOps[%d].dest %q is under .claudinite/shared/, which the engine regenerates", i, dest)
		}
		out = append(out, SeedOp{Template: tmpl, Dest: dest})
	}
	return out, nil
}

// inside reports whether p is a clean relative slash path that stays
// where it starts.
func inside(p string) bool {
	return !strings.HasPrefix(p, "/") && !strings.Contains(p, "\\") && path.Clean(p) == p && p != "." && p != ".." && !strings.HasPrefix(p, "../")
}

// readHandover validates adoptionHandover: each step names all three
// parts, or it is not one.
func readHandover(v any) ([]HandoverStep, error) {
	list, _ := v.([]any)
	var out []HandoverStep
	for i, e := range list {
		o, _ := e.(map[string]any)
		var parts [3]string
		for k, key := range []string{"step", "breaks", "done"} {
			parts[k], _ = o[key].(string)
			if strings.TrimSpace(parts[k]) == "" {
				return nil, fmt.Errorf("adoptionHandover[%d] needs a non-empty %s", i, key)
			}
		}
		out = append(out, HandoverStep{Step: parts[0], Breaks: parts[1], Done: parts[2]})
	}
	return out, nil
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
