package taskspec

import (
	_ "embed"
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/checksdk"
	"github.com/missingbulb/ClaudiniteEngine/shared/descriptor"
	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
)

// DeclarationName is a task declaration's file name, without its format.
const DeclarationName = "task"

// CodeWorkEnvVars are the variables a task's code work is handed, and the
// only CLAUDINITE_* names its code may read.
var CodeWorkEnvVars = []string{
	"CLAUDINITE_REPO_ROOT", "CLAUDINITE_REPO", "CLAUDINITE_DEFAULT_BRANCH", "CLAUDINITE_ITEM",
	"CLAUDINITE_PACK", "CLAUDINITE_TASK", "CLAUDINITE_CONTEXT", "CLAUDINITE_REQUEST_AGENT",
	"CLAUDINITE_TARGET_MODE", "CLAUDINITE_TARGET_BRANCH", "CLAUDINITE_TARGET_PR",
}

// Found is one task folder holding a declaration.
type Found struct {
	Pack, Name string
	// Dir is the task folder and File its declaration, both absolute.
	Dir, File string
}

// DeclarationFiles walks every pack's tasks/ for the folders holding a
// declaration, packs in the given order and folders by name. An
// unreadable tasks/ and a folder carrying two spellings are problems,
// never a sunk walk.
func DeclarationFiles(packs []packset.Pack) ([]Found, []DiscoveryError) {
	var found []Found
	var problems []DiscoveryError
	for _, p := range packs {
		root := filepath.Join(p.Dir, "tasks")
		st, err := os.Stat(root)
		if err != nil {
			continue
		}
		if !st.IsDir() {
			problems = append(problems, DiscoveryError{
				Pack: p.ID,
				What: p.ID + "'s tasks/ is not a readable directory: not a directory",
				Fix:  "make " + p.Rel + "/tasks a directory (or remove it)",
			})
			continue
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			problems = append(problems, DiscoveryError{Pack: p.ID, What: p.ID + "'s tasks/ is not a readable directory: " + err.Error(), Fix: "make " + p.Rel + "/tasks a directory (or remove it)"})
			continue
		}
		var names []string
		for _, e := range entries {
			if e.IsDir() {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, name := range names {
			dir := filepath.Join(root, name)
			file, _, err := descriptor.Find(dir, DeclarationName)
			if errors.Is(err, descriptor.ErrAbsent) {
				continue
			}
			if err != nil {
				problems = append(problems, DiscoveryError{Pack: p.ID, Task: name, What: p.Rel + "/tasks/" + name + ": " + err.Error(), Fix: "keep one declaration per task folder"})
				continue
			}
			found = append(found, Found{Pack: p.ID, Name: name, Dir: dir, File: file})
		}
	}
	return found, problems
}

// ParseText reads a declaration's text: JSON as JavaScript's JSON.parse
// would (a repeated key keeps its last value), YAML and TOML through the
// descriptor parser. `$schema` is the editor's pointer and leaves here.
func ParseText(file string, text []byte) (Decl, error) {
	var raw any
	var err error
	if f := descriptor.FormatOf(file); strings.HasSuffix(file, ".json") {
		err = json.Unmarshal(text, &raw)
	} else {
		raw, err = descriptor.ParseBytes(text, f)
	}
	if err != nil {
		return nil, err
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, errors.New("not a JSON object")
	}
	delete(obj, "$schema")
	return Decl(obj), nil
}

// SecretNames are the repository secrets a set of declarations ask the
// executor to carry, sorted and unique.
func SecretNames(decls []Decl) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range decls {
		for _, s := range d.Strings("code_work_required_secrets") {
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	sort.Strings(out)
	return out
}

// The engine's own tasks: one, the request implementer a marked issue's
// work item names. It is not a pack and declares nothing; wherever the
// queue runs it is active, which is also what fences the one field only
// it may declare (model_from_request).
const (
	BuiltinPack = "engine"
	RequestTask = "implement-request"
)

//go:embed builtin/implement-request/task.json
var requestDeclaration []byte

// Task is one discovered, validated task.
type Task struct {
	Pack, ID string
	// Dir is the task folder, absolute, and Rel the same relative to the
	// repo with forward slashes; both empty for the engine's own task.
	Dir, Rel string
	Decl     Decl
	// Terms are the task's own precondition terms.
	Terms Terms
}

// Path is the task as a work item's title names it.
func (t Task) Path() string { return t.Pack + "/" + t.ID }

// DiscoveryError is a task discovery dropped, with the reason; discovery is
// fail-soft per task.
type DiscoveryError struct {
	Pack string `json:"pack"`
	Task string `json:"task,omitempty"`
	What string `json:"what"`
	Fix  string `json:"fix"`
}

// Discover finds every task the given packs contribute, then the engine's
// own, each validated; a task that does not load, does not validate or
// whose id is not its folder's name is an error, never a sunk scan.
func Discover(repo string, packs []packset.Pack) ([]Task, []DiscoveryError) {
	var tasks []Task
	var active []packset.Pack
	for _, p := range packs {
		if p.Kind != packset.Temp {
			active = append(active, p)
		}
	}
	found, errs := DeclarationFiles(active)
	rel := func(p string) string {
		r, err := filepath.Rel(repo, p)
		if err != nil {
			return filepath.ToSlash(p)
		}
		return filepath.ToSlash(r)
	}
	for _, f := range found {
		terms := Terms(nil)
		if text, err := os.ReadFile(filepath.Join(f.Dir, "preconditions.mjs")); err == nil {
			terms = TermsFromText(checksdk.StripComments(string(text)))
		}
		raw, err := os.ReadFile(f.File)
		var d Decl
		if err == nil {
			d, err = ParseText(f.File, raw)
		}
		if err != nil {
			errs = append(errs, DiscoveryError{Pack: f.Pack, Task: f.Name, What: rel(f.File) + " failed to load: " + err.Error(), Fix: "fix or remove the task"})
			continue
		}
		if t, e := admit(f.Pack, f.Name, rel(f.File), d, terms); e != nil {
			errs = append(errs, *e)
		} else {
			t.Dir, t.Rel = f.Dir, rel(f.Dir)
			tasks = append(tasks, t)
		}
	}
	d, err := ParseText("task.json", requestDeclaration)
	if err != nil {
		panic("the engine's own task declaration does not parse: " + err.Error())
	}
	if t, e := admit(BuiltinPack, RequestTask, BuiltinPack+"/"+RequestTask, d, EngineTerms); e != nil {
		errs = append(errs, *e)
	} else {
		tasks = append(tasks, t)
	}
	return tasks, errs
}

func admit(pack, name, where string, d Decl, terms Terms) (Task, *DiscoveryError) {
	norm := Normalize(map[string]any(d)).(Decl)
	if problems := Validate(map[string]any(d), terms); len(problems) > 0 {
		whats := make([]string, len(problems))
		for i, p := range problems {
			whats[i] = p.What
		}
		return Task{}, &DiscoveryError{Pack: pack, Task: name, What: where + " is not a valid task declaration: " + strings.Join(whats, "; "), Fix: problems[0].Fix}
	}
	if norm.ID() != name {
		return Task{}, &DiscoveryError{Pack: pack, Task: name, What: "task in " + path.Dir(where) + ` declares id "` + norm.ID() + `" but its directory is "` + name + `"`, Fix: "rename the directory to the task id, or set the id to the directory name"}
	}
	return Task{Pack: pack, ID: norm.ID(), Decl: norm, Terms: terms}, nil
}
