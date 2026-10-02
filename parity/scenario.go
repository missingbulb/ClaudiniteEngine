package parity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
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
//	transcript.jsonl  the session transcript the work run and every hook
//	               payload read, with subagents/agent-*.jsonl beside it
//	expect.json    what every engine must answer (Expect)
//
// A declared pack that is not local/ is copied into
// .claudinite/shared/packs/, as a member vendors it: from the frozen
// engine's packs/, or, for cn and a pack ported.txt lists, from the
// ClaudinitePacks checkout CLAUDINITE_PACKS_TREE names (PackSource).
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
	// Why says why a scenario narrows what is compared; LoaderOnly and
	// CnOnly need one.
	Why string `json:"why,omitempty"`
	// The per-call hooks' answers, each to its payload.
	PreToolUse  []HookCase `json:"pretooluse,omitempty"`
	Prompt      []HookCase `json:"prompt,omitempty"`
	PostToolUse []HookCase `json:"posttooluse,omitempty"`
	// CnOnly runs the scenario against cn alone, for a surface the Node
	// engine does not have (pack hook judges).
	CnOnly bool `json:"cnOnly,omitempty"`
}

// HookCase is one per-call hook payload and the answer it must get: the
// exit code, the first line of a block, the context. The runner adds the
// session id, the event name and, unless NoTranscript, the transcript.
type HookCase struct {
	// Payload keeps its key order: a pattern over the serialized input
	// reads it.
	Payload      json.RawMessage `json:"payload"`
	NoTranscript bool            `json:"noTranscript,omitempty"`
	Exit         int             `json:"exit"`
	Block        string          `json:"block,omitempty"`
	Context      string          `json:"context,omitempty"`
}

// hookEvents are the per-call events, as expect.json and cn hook name them,
// with Claude Code's event name.
var hookEvents = []struct{ key, event, name string }{
	{"pretooluse", "pre-tool-use", "PreToolUse"},
	{"prompt", "user-prompt-submit", "UserPromptSubmit"},
	{"posttooluse", "post-tool-use", "PostToolUse"},
}

func (x *Expect) cases(key string) *[]HookCase {
	switch key {
	case "pretooluse":
		return &x.PreToolUse
	case "prompt":
		return &x.Prompt
	}
	return &x.PostToolUse
}

// Payload is the case's payload as the hook reads it.
func (c HookCase) payload(name, transcript string) (string, error) {
	body := strings.TrimSpace(string(c.Payload))
	if !strings.HasPrefix(body, "{") {
		return "", fmt.Errorf("a payload is a JSON object, not %s", body)
	}
	head := map[string]string{"session_id": SessionID, "hook_event_name": name}
	if transcript != "" && !c.NoTranscript {
		head["transcript_path"] = transcript
	}
	var b strings.Builder
	b.WriteString("{")
	for _, k := range []string{"session_id", "hook_event_name", "transcript_path"} {
		if v, ok := head[k]; ok {
			q, _ := json.Marshal(v)
			fmt.Fprintf(&b, "%q:%s,", k, q)
		}
	}
	rest := strings.TrimSpace(body[1:])
	if rest == "}" {
		return strings.TrimSuffix(b.String(), ",") + "}", nil
	}
	b.WriteString(rest)
	return b.String(), nil
}

// Transcript lays the scenario's transcript out under parent as Claude
// Code does (<id>.jsonl beside <id>/subagents/) and names the session
// file; "" when the scenario has none.
func (s Scenario) Transcript(parent string) (string, error) {
	src := filepath.Join(s.Dir, "transcript.jsonl")
	if !exists(src) {
		return "", nil
	}
	dir := filepath.Join(parent, "session")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, SessionID+".jsonl")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return "", err
	}
	if sub := filepath.Join(s.Dir, "subagents"); exists(sub) {
		if err := copyTree(sub, filepath.Join(dir, SessionID, "subagents")); err != nil {
			return "", err
		}
	}
	return path, nil
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
	if s.Expect.CnOnly && s.Expect.Why == "" {
		return s, fmt.Errorf("%s/%s expect.json: cnOnly without a why", group, name)
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
		src, err := PackSource(id, canonPacks, e.Name())
		if err != nil {
			return "", err
		}
		if err := copyTree(src, dst); err != nil {
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

// PacksTreeEnv names a ClaudinitePacks checkout at the commit
// claudinitepacks.ref pins; cn reads every pack ported.txt lists from its
// packs/, the Node engine never does.
const PacksTreeEnv = "CLAUDINITE_PACKS_TREE"

// Ported are the pack ids parity/ported.txt lists: ported to Go, so cn
// takes them from ClaudinitePacks.
func Ported() map[string]bool {
	out := map[string]bool{}
	raw, err := os.ReadFile(portedFile())
	if err != nil {
		return out
	}
	for _, l := range strings.Split(string(raw), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			out[l] = true
		}
	}
	return out
}

func portedFile() string {
	_, self, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(self), "ported.txt")
}

// PackSource is where engine reads canon pack id from: the ClaudinitePacks
// checkout for cn and a ported pack, the frozen shelf otherwise.
func PackSource(id, canonPacks, engine string) (string, error) {
	if engine != "cn" || !Ported()[id] {
		return filepath.Join(canonPacks, id), nil
	}
	tree := os.Getenv(PacksTreeEnv)
	if tree == "" {
		return "", fmt.Errorf("pack %s is ported (parity/ported.txt), and cn reads it from a ClaudinitePacks checkout; set %s", id, PacksTreeEnv)
	}
	src := filepath.Join(tree, "packs", id)
	if !exists(src) {
		return "", fmt.Errorf("pack %s is ported, but %s holds no packs/%s", id, PacksTreeEnv, id)
	}
	return src, nil
}

// Comparable is the set of rules whose findings the engines must agree
// on in dir: Only when the scenario names it, else every declared check
// in the tree plus the built-ins.
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

// DeclaredIDs are the ids of the checks every declared-checks.json under
// dir declares.
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
			if id != "" {
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

// UnvendoredPacks are the declared canon packs the member does not hold
// itself, which come from the frozen engine's shelf.
func (s Scenario) UnvendoredPacks() []string {
	var out []string
	for _, id := range s.CanonPacks() {
		if !exists(filepath.Join(s.Dir, "member/.claudinite/shared/packs", id)) {
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
// become the checks block, a top-level sharedConstants moves to the basics
// entry's config (dropped when basics is not declared, since nothing else
// reads it), and the engine block pins the development engine. version, answers and via are the adoption and update slices'
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
	if sc, ok := node["sharedConstants"]; ok {
		for i, d := range declared {
			e, isMap := d.(map[string]any)
			if d != "basics" && (!isMap || e["id"] != "basics") {
				continue
			}
			if !isMap {
				e = map[string]any{"id": "basics"}
			}
			cfg := map[string]any{}
			if old, ok := e["config"].(map[string]any); ok {
				for k, v := range old {
					cfg[k] = v
				}
			}
			cfg["sharedConstants"] = sc
			e["config"] = cfg
			declared[i] = e
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

// TodayToken in a scenario file is replaced by today's UTC date as it is
// copied, so a check's grace window ("since") can be pinned open.
const TodayToken = "@@TODAY@@"

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
		b = bytes.ReplaceAll(b, []byte(TodayToken), []byte(time.Now().UTC().Format("2006-01-02")))
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, info.Mode().Perm())
	})
}

var gitIdentity = []string{"-c", "user.name=parity", "-c", "user.email=parity@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null", "-c", "gc.auto=0", "-c", "maintenance.auto=false"}

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
