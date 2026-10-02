package checksdk

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Repo is the repository a run checks. Root and the working-tree reads
// need nothing else; every other method is a call to the engine, which
// answers from the walk, the change and the session its own run already
// read, memoised for the run.
type Repo struct {
	Root string
	st   *state
}

// Handler answers one engine call: the method and its arguments, as JSON
// or any value encoding/json takes. NewRepo builds a Repo over one, for
// tests and tools that are not the checks binary.
type Handler func(method string, args json.RawMessage) (any, error)

type caller interface {
	handle(method string, args any) (json.RawMessage, error)
}

type state struct {
	eng     caller
	methods map[string]bool // nil: every method answered
	engine  string

	mu    sync.Mutex
	memo  map[string]memoEntry
	reads map[string]*string
}

type memoEntry struct {
	raw json.RawMessage
	err error
}

func newState(eng caller, methods map[string]bool, engine string) *state {
	return &state{eng: eng, methods: methods, engine: engine, memo: map[string]memoEntry{}, reads: map[string]*string{}}
}

type handlerCaller struct{ h Handler }

func (c handlerCaller) handle(method string, args any) (json.RawMessage, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	if args == nil {
		raw = []byte("{}")
	}
	v, err := c.h(method, raw)
	if err != nil {
		return nil, err
	}
	if r, ok := v.(json.RawMessage); ok {
		return r, nil
	}
	return json.Marshal(v)
}

// NewRepo is a Repo at root whose engine calls h answers.
func NewRepo(root string, h Handler) Repo {
	return Repo{Root: root, st: newState(handlerCaller{h}, nil, "")}
}

// call asks the engine, once per method and arguments for the run, and
// decodes the answer into out. A method the engine does not answer
// panics, failing the check that asked; an error the engine answers is
// returned.
func (r Repo) call(method string, args any, out any) error {
	if r.st == nil || (r.st.methods != nil && !r.st.methods[method]) {
		panic(missing(fmt.Sprintf("checksdk: the engine does not answer %s; it needs cn %s or newer", method, EngineFloor)))
	}
	key := method
	if args != nil {
		b, _ := json.Marshal(args)
		key += "\x00" + string(b)
	}
	r.st.mu.Lock()
	e, ok := r.st.memo[key]
	r.st.mu.Unlock()
	if !ok {
		e.raw, e.err = r.st.eng.handle(method, args)
		r.st.mu.Lock()
		r.st.memo[key] = e
		r.st.mu.Unlock()
	}
	if e.err != nil {
		return e.err
	}
	if out == nil || len(e.raw) == 0 || string(e.raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(e.raw, out); err != nil {
		return fmt.Errorf("checksdk: the engine's answer to %s: %w", method, err)
	}
	return nil
}

// must is call for a method whose engine error is a fault in the run, not
// something a check decides on: it panics with the error.
func (r Repo) must(method string, args any, out any) {
	if err := r.call(method, args, out); err != nil {
		panic(fmt.Sprintf("checksdk: %s: %v", method, err))
	}
}

func (r Repo) strings(method string, args any) []string {
	var out []string
	r.must(method, args, &out)
	return out
}

// Path is rel under the repo root.
func (r Repo) Path(rel string) string { return filepath.Join(r.Root, filepath.FromSlash(rel)) }

// Exists reports whether rel exists under the repo root.
func (r Repo) Exists(rel string) bool {
	_, err := os.Stat(r.Path(rel))
	return err == nil
}

// ReadFile reads rel under the repo root.
func (r Repo) ReadFile(rel string) ([]byte, error) { return os.ReadFile(r.Path(rel)) }

// Read is rel's text from the working tree, invalid UTF-8 replaced, and
// false when it cannot be read; memoised for the run.
func (r Repo) Read(rel string) (string, bool) {
	if r.st != nil {
		r.st.mu.Lock()
		t, ok := r.st.reads[rel]
		r.st.mu.Unlock()
		if ok {
			if t == nil {
				return "", false
			}
			return *t, true
		}
	}
	b, err := r.ReadFile(rel)
	var t *string
	if err == nil {
		s := strings.ToValidUTF8(string(b), "�")
		t = &s
	}
	if r.st != nil {
		r.st.mu.Lock()
		r.st.reads[rel] = t
		r.st.mu.Unlock()
	}
	if t == nil {
		return "", false
	}
	return *t, true
}

// Files are the scanned files: tracked and untracked, minus the vendored
// mount, anything that is not a regular file, and what the attributes mark
// linguist-vendored or linguist-generated.
func (r Repo) Files() []string { return r.strings("tree.files", nil) }

// Tracked is the index, unfiltered.
func (r Repo) Tracked() []string { return r.strings("tree.tracked", nil) }

// AllFiles are the scanned files with the vendored and generated ones kept.
func (r Repo) AllFiles() []string { return r.strings("tree.all", nil) }

// Untracked are the files git does not track and does not ignore.
func (r Repo) Untracked() []string { return r.strings("tree.untracked", nil) }

// ChangedFiles are the scanned files the change touched: changed since the
// merge base, or untracked.
func (r Repo) ChangedFiles() []string { return r.strings("change.files", nil) }

// Deleted are the paths the change deletes; none when no merge base
// resolves.
func (r Repo) Deleted() []string { return r.strings("change.deleted", nil) }

type base struct {
	Branch    string `json:"branch"`
	BaseRef   string `json:"baseRef"`
	MergeBase string `json:"mergeBase"`
}

func (r Repo) base() base {
	var b base
	r.must("change.base", nil, &b)
	return b
}

// Branch is the checked-out branch, "HEAD" when detached.
func (r Repo) Branch() string { return r.base().Branch }

// BaseRef is the ref the change is measured against, "" when none
// resolves.
func (r Repo) BaseRef() string { return r.base().BaseRef }

// MergeBase is the change's merge base with BaseRef, "" when none.
func (r Repo) MergeBase() string { return r.base().MergeBase }

// OnDefaultBranch reports whether the checked-out branch is main or
// master.
func (r Repo) OnDefaultBranch() bool {
	b := r.Branch()
	return b == "main" || b == "master"
}

// ReadBase is rel's text at the merge base, false when it was absent
// there or no merge base resolves.
func (r Repo) ReadBase(rel string) (string, bool) {
	var a struct {
		Text string `json:"text"`
		OK   bool   `json:"ok"`
	}
	r.must("change.readBase", map[string]string{"path": rel}, &a)
	return a.Text, a.OK
}

// ListBase is every path at the merge base.
func (r Repo) ListBase() []string { return r.strings("change.listBase", nil) }

// Line is one line of a file: its path, its 1-based number and its text.
type Line struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// AddedLines are the change's added lines in files, every line of an
// untracked one; nil files is every changed file.
func (r Repo) AddedLines(files []string) []Line {
	var out []Line
	r.must("change.added", map[string]any{"files": files}, &out)
	return out
}

// RemovedLines are the change's removed lines in files, numbered as the
// base had them; nil files is every changed file.
func (r Repo) RemovedLines(files []string) []Line {
	var out []Line
	r.must("change.removed", map[string]any{"files": files}, &out)
	return out
}

// Commit is one of the change's own commits.
type Commit struct {
	Sha     string   `json:"sha"`
	Date    string   `json:"date"`
	Subject string   `json:"subject"`
	Files   []string `json:"files"`
}

// Time is the commit's committer date, the zero time when it does not
// parse.
func (c Commit) Time() time.Time {
	t, _ := time.Parse(time.RFC3339, c.Date)
	return t
}

// BranchCommits are the change's commits since the merge base, oldest
// first, merges excluded, each with the files it changed.
func (r Repo) BranchCommits() []Commit {
	var out []Commit
	r.must("change.commits", nil, &out)
	return out
}

// CommitMessages are the change's commit messages, subject and body.
func (r Repo) CommitMessages() []string { return r.strings("change.messages", nil) }

// Merge is one merge commit.
type Merge struct {
	Sha     string `json:"sha"`
	Subject string `json:"subject"`
}

// IntroducedMerges are the change's first-parent merge commits that are
// not on the base branch.
func (r Repo) IntroducedMerges() []Merge {
	var out []Merge
	r.must("change.merges", nil, &out)
	return out
}

// GrepTracked is every tracked line containing needle, the vendored mount
// excluded.
func (r Repo) GrepTracked(needle string) []Line {
	var out []Line
	r.must("change.grep", map[string]string{"needle": needle}, &out)
	return out
}

// PackConfig is the config object on the member's entry for pack id, as
// JSON; null when the entry carries none.
func (r Repo) PackConfig(id string) json.RawMessage {
	var out json.RawMessage
	r.must("config.pack", map[string]string{"id": id}, &out)
	if len(out) == 0 {
		return json.RawMessage("null")
	}
	return out
}

// Acceptance is one accepted finding in the member's settings.
type Acceptance struct {
	Rule   string `json:"rule"`
	Path   string `json:"path,omitempty"`
	Reason string `json:"reason,omitempty"`
	Pack   string `json:"pack,omitempty"`
}

// ChecksConfig is what the member's settings say about checks.
type ChecksConfig struct {
	Rules  map[string]string `json:"rules"`
	Accept []Acceptance      `json:"accept"`
}

// ChecksConfig is the member's checks overrides and acceptances.
func (r Repo) ChecksConfig() ChecksConfig {
	var out ChecksConfig
	r.must("config.checks", nil, &out)
	return out
}

// Parsed is rel parsed by its extension, JSON, YAML or TOML, as the
// engine reads descriptors: a YAML tag the parser does not know (a
// CloudFormation !Ref) keeps its scalar. The error is the parser's.
func (r Repo) Parsed(rel string) (any, error) {
	var out any
	err := r.call("doc.parse", map[string]string{"path": rel}, &out)
	return out, err
}

// Session is the session transcript the run was given.
func (r Repo) Session() Session { return Session{r} }

// Pack is one pack of the run's declared set, as the engine loaded it:
// Kind is canon, local or temp; Dir is repo-relative; Prose is the prose
// file's name, "" for none.
type Pack struct {
	ID               string   `json:"id"`
	Kind             string   `json:"kind"`
	Dir              string   `json:"dir"`
	Version          string   `json:"version"`
	MinEngineVersion string   `json:"minEngineVersion"`
	Prose            string   `json:"prose"`
	Skills           []string `json:"skills"`
	Requires         []string `json:"requires"`
}

// Packs is the member's declared pack set, in the engine's order.
func (r Repo) Packs() []Pack {
	out := []Pack{}
	r.must("packs.list", nil, &out)
	return out
}
