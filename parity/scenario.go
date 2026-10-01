package parity

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// A scenario is a directory:
//
//	settings.json  the Node engine's declaration (.claudinite-settings.json)
//	member/        the repo's committed files on main
//	change/        files laid over member/ and committed on branch change
//	change-message the change commit's message (default "change")
//	merge/         files committed on a side branch off main and merged
//	               into change with a merge commit
//	untracked/     files laid over the result and left untracked
//	expect.json    what every engine must answer (Expect)
//
// A declared pack that is not local/ is copied from the frozen engine's
// packs/ into .claudinite/shared/packs/, as a member vendors it.
type Scenario struct {
	Group, Name, Dir string
	Node             map[string]any
	Expect           Expect
}

// Expect is what each engine must answer; a missing field is not asserted
// against the expectation, only between the engines.
type Expect struct {
	Rules  *string   `json:"rules,omitempty"`
	Mounts []string  `json:"mounts"`
	World  *[]string `json:"world,omitempty"`
	Work   *[]string `json:"work,omitempty"`
	// Only narrows the compared findings to these rules; by default they
	// are the rules the scenario's declared checks and the engine's
	// built-in checks carry.
	Only []string `json:"only,omitempty"`
	// LoaderOnly compares the rules index and the mounts alone, for a
	// scenario whose findings are each engine's own settings validation
	// (the Node engine's config check, cn's verify), which differ by
	// design.
	LoaderOnly bool `json:"loaderOnly,omitempty"`
	// Why says why a scenario narrows what is compared; LoaderOnly needs
	// one.
	Why string `json:"why,omitempty"`
}

// Builtins are the built-in checks both engines run.
var Builtins = []string{"declared-check-spec-keys", "barrier", "config"}

// LoadScenarios reads every scenario under root, as group/name.
func LoadScenarios(root string) ([]Scenario, error) {
	var out []Scenario
	groups, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, g := range groups {
		if !g.IsDir() {
			continue
		}
		names, err := os.ReadDir(filepath.Join(root, g.Name()))
		if err != nil {
			return nil, err
		}
		for _, n := range names {
			if !n.IsDir() {
				continue
			}
			s, err := loadScenario(g.Name(), n.Name(), filepath.Join(root, g.Name(), n.Name()))
			if err != nil {
				return nil, err
			}
			out = append(out, s)
		}
	}
	return out, nil
}

func loadScenario(group, name, dir string) (Scenario, error) {
	s := Scenario{Group: group, Name: name, Dir: dir}
	raw, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(raw, &s.Node); err != nil {
		return s, fmt.Errorf("%s/%s settings.json: %w", group, name, err)
	}
	raw, err = os.ReadFile(filepath.Join(dir, "expect.json"))
	if err != nil {
		return s, err
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s.Expect); err != nil {
		return s, fmt.Errorf("%s/%s expect.json: %w", group, name, err)
	}
	if s.Expect.LoaderOnly && s.Expect.Why == "" {
		return s, fmt.Errorf("%s/%s expect.json: loaderOnly without a why", group, name)
	}
	return s, nil
}

// Materialize builds the scenario's repo for one engine under parent and
// returns its directory. The engine's settings file is excluded from git,
// so both engines see the same tracked and untracked files.
func (s Scenario) Materialize(parent, canonPacks string, e Engine) (string, error) {
	dir := filepath.Join(parent, e.Name())
	if err := copyTree(filepath.Join(s.Dir, "member"), dir); err != nil {
		return "", err
	}
	for _, id := range PackIDs(s.Node) {
		if strings.HasPrefix(id, "local/") {
			continue
		}
		dst := filepath.Join(dir, ".claudinite/shared/packs", id)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if err := copyTree(filepath.Join(canonPacks, id), dst); err != nil {
			return "", fmt.Errorf("canon pack %s: %w", id, err)
		}
	}
	settings, err := e.Settings(dir, s.Node)
	if err != nil {
		return "", err
	}
	if err := gitDo(dir, "init", "-q", "-b", "main"); err != nil {
		return "", err
	}
	if err := appendFile(filepath.Join(dir, ".git/info/exclude"), "/"+settings+"\n"); err != nil {
		return "", err
	}
	if err := commitAll(dir, "base"); err != nil {
		return "", err
	}
	if exists(filepath.Join(s.Dir, "change")) {
		if err := gitDo(dir, "checkout", "-q", "-b", "change"); err != nil {
			return "", err
		}
		if err := copyTree(filepath.Join(s.Dir, "change"), dir); err != nil {
			return "", err
		}
		msg := "change"
		if b, err := os.ReadFile(filepath.Join(s.Dir, "change-message")); err == nil {
			msg = strings.TrimSpace(string(b))
		}
		if err := commitAll(dir, msg); err != nil {
			return "", err
		}
	}
	if exists(filepath.Join(s.Dir, "merge")) {
		if !exists(filepath.Join(s.Dir, "change")) {
			if err := gitDo(dir, "checkout", "-q", "-b", "change"); err != nil {
				return "", err
			}
		}
		if err := gitDo(dir, "checkout", "-q", "-b", "side", "main"); err != nil {
			return "", err
		}
		if err := copyTree(filepath.Join(s.Dir, "merge"), dir); err != nil {
			return "", err
		}
		if err := commitAll(dir, "side"); err != nil {
			return "", err
		}
		for _, st := range [][]string{{"checkout", "-q", "change"}, {"merge", "-q", "--no-ff", "-m", "Merge side", "side"}, {"branch", "-q", "-D", "side"}} {
			if err := gitDo(dir, st...); err != nil {
				return "", err
			}
		}
	}
	if exists(filepath.Join(s.Dir, "untracked")) {
		if err := copyTree(filepath.Join(s.Dir, "untracked"), dir); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// Comparable is the set of rules whose findings the engines must agree
// on in dir: Only when the scenario names it, else every non-action
// declared check in the tree plus the built-ins.
func (s Scenario) Comparable(dir string) (map[string]bool, error) {
	set := map[string]bool{}
	if s.Expect.LoaderOnly {
		return set, nil
	}
	if len(s.Expect.Only) > 0 {
		for _, id := range s.Expect.Only {
			set[id] = true
		}
		return set, nil
	}
	for _, id := range Builtins {
		set[id] = true
	}
	ids, err := DeclaredIDs(dir)
	for _, id := range ids {
		set[id] = true
	}
	return set, err
}

// DeclaredIDs are the ids of the world and work checks every
// declared-checks.json under dir declares.
func DeclaredIDs(dir string) ([]string, error) {
	var ids []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.IsDir() || d.Name() != "declared-checks.json" {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var decls []map[string]any
		if json.Unmarshal(raw, &decls) != nil {
			return nil
		}
		for _, c := range decls {
			id, _ := c["id"].(string)
			if scope, _ := c["scope"].(string); id != "" && scope != "action" {
				ids = append(ids, id)
			}
		}
		return nil
	})
	sort.Strings(ids)
	return ids, err
}

// Keep drops the findings of rules outside set.
func Keep(fs []Finding, set map[string]bool) []Finding {
	var out []Finding
	for _, f := range fs {
		if set[f.Rule] {
			out = append(out, f)
		}
	}
	return sortFindings(out)
}

// CanonPacks are the declaration's packs that come from the frozen
// engine's shelf.
func (s Scenario) CanonPacks() []string {
	var out []string
	for _, id := range PackIDs(s.Node) {
		if !strings.HasPrefix(id, "local/") {
			out = append(out, id)
		}
	}
	return out
}

// PackIDs are the declaration's pack ids, entry objects included.
func PackIDs(node map[string]any) []string {
	var ids []string
	packs, _ := node["packs"].([]any)
	for _, p := range packs {
		switch v := p.(type) {
		case string:
			ids = append(ids, v)
		case map[string]any:
			if id, ok := v["id"].(string); ok {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// DevManifest is the integrity string a development engine's settings
// carry; nothing reads it at 0.0.0.
const DevManifest = "sha512-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="

// Translate turns a Node declaration (.claudinite-settings.json) into
// cn's settings: each pack entry keeps id, config, rules and accept (a
// bare id when nothing else is left), the top-level rules and accept
// become the checks block, and the engine block pins the development
// engine. version, answers and via are the adoption and update slices'
// and are dropped. The result is YAML, which cn reads as
// .claudinite/settings.yaml.
func Translate(node map[string]any) ([]byte, error) {
	var declared []any
	packs, _ := node["packs"].([]any)
	for _, p := range packs {
		switch v := p.(type) {
		case string:
			declared = append(declared, v)
		case map[string]any:
			e := map[string]any{}
			for _, k := range []string{"id", "config", "rules", "accept"} {
				if x, ok := v[k]; ok {
					e[k] = x
				}
			}
			if _, ok := e["id"]; !ok {
				return nil, errors.New("a pack entry with no id")
			}
			if len(e) == 1 {
				declared = append(declared, e["id"])
			} else {
				declared = append(declared, e)
			}
		default:
			return nil, fmt.Errorf("a pack entry %v is neither an id nor an object", p)
		}
	}
	if declared == nil {
		declared = []any{}
	}
	out := map[string]any{
		"engine": map[string]any{"version": "0.0.0", "manifest": DevManifest},
		"packs":  map[string]any{"declared": declared},
	}
	checks := map[string]any{}
	for _, k := range []string{"rules", "accept"} {
		if x, ok := node[k]; ok {
			checks[k] = x
		}
	}
	if len(checks) > 0 {
		out["checks"] = checks
	}
	doc, err := yamlDoc(out)
	return []byte(doc), err
}

func jsonIndent(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func appendFile(p, s string) error {
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = f.WriteString(s)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, info.Mode().Perm())
	})
}

var gitIdentity = []string{"-c", "user.name=parity", "-c", "user.email=parity@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}

func gitDo(dir string, args ...string) error {
	cmd := exec.Command("git", append(append([]string{}, gitIdentity...), args...)...)
	cmd.Dir = dir
	// A fixed date makes every commit's sha the same on both engines' trees.
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %v: %v: %s", args, err, out)
	}
	return nil
}

func commitAll(dir, msg string) error {
	if err := gitDo(dir, "add", "-A"); err != nil {
		return err
	}
	return gitDo(dir, "commit", "-q", "--allow-empty", "-m", msg)
}

func sortStrings(s []string) { sort.Strings(s) }
