// Package parity runs one scenario against two engines, the frozen Node
// engine (missingbulb/Claudinite at a pinned commit, named by
// CLAUDINITE_NODE_ENGINE) and cn, and asserts they behave the same: which
// rules reach a session, which skills are mounted, which checks fire on
// which path and line. Both engines are driven as black boxes, through
// their command lines; this package imports nothing of cn.
package parity

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Finding is one finding as both engines' reports name it.
type Finding struct {
	Rule   string `json:"rule"`
	Path   string `json:"path"`
	Line   int    `json:"line,omitempty"`
	OnFail string `json:"on_fail"`
}

func (f Finding) String() string {
	loc := f.Path
	if f.Line > 0 {
		loc = fmt.Sprintf("%s:%d", f.Path, f.Line)
	}
	return fmt.Sprintf("%s %s %s", f.Rule, loc, f.OnFail)
}

// SettingsPath stands for either engine's settings file in a finding.
const SettingsPath = "(settings)"

func normPath(p string) string {
	switch p {
	case ".claudinite-settings.json", ".claudinite/settings.yaml", ".claudinite/settings.json", ".claudinite/settings.toml":
		return SettingsPath
	}
	return p
}

// Engine answers the four questions of a scenario for one engine.
type Engine interface {
	Name() string
	// Settings writes the engine's settings file for a Node-shaped
	// declaration into dir, and names it.
	Settings(dir string, node map[string]any) (string, error)
	// Rules is the rules index that reaches a session.
	Rules(dir string) (string, error)
	// Skills is the skills index the engine writes beside it, "" for none.
	Skills(dir string) (string, error)
	// Mounts are the skills the engine mounted, with each SKILL.md.
	Mounts(dir string) (map[string]string, error)
	// Flat is the flat task and dashboard declarations the active packs
	// produce, both files' text in order; cn's member file, which Node
	// has no counterpart of, is left out.
	Flat(dir string) (string, error)
	// World and Work are the findings of a whole-repo sweep and of the
	// change; Work reads the session transcript at transcript, "" for none.
	World(dir string) ([]Finding, error)
	Work(dir, transcript string) ([]Finding, error)
	// Hook answers one per-call hook event (pre-tool-use, post-tool-use,
	// user-prompt-submit) for payload.
	Hook(dir, event, payload string) (Verdict, error)
}

// Verdict is a per-call hook's answer: the exit code, stderr, and the
// additionalContext of its stdout.
type Verdict struct {
	Exit    int
	Stderr  string
	Context string
}

// logLine is a line of either engine's own record on stderr: the Node
// engine's hook log (<iso> run=<id> <hook>: …) or a cn breadcrumb.
var logLine = regexp.MustCompile(`^(\d{4}-\d\d-\d\dT\S+Z run=\S+ \S+: |\[cn\] )`)

// Block is the first line of a blocking answer's stderr that is not a log
// line, the denial the transcript records; "" when the hook did not block.
func (v Verdict) Block() string {
	if v.Exit != 2 {
		return ""
	}
	for _, l := range strings.Split(v.Stderr, "\n") {
		if !logLine.MatchString(l) {
			return l
		}
	}
	return ""
}

// hookOutput is the additionalContext of a hook's stdout, with each
// engine's own breadcrumb lines removed; stdout that is empty or {} says
// nothing.
func hookOutput(stdout string) (string, error) {
	stdout = strings.TrimSpace(stdout)
	if stdout == "" || stdout == "{}" {
		return "", nil
	}
	var out struct {
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		return "", fmt.Errorf("hook stdout is not JSON: %q", stdout)
	}
	var keep []string
	for _, l := range strings.Split(out.HookSpecificOutput.AdditionalContext, "\n") {
		if !strings.HasPrefix(l, "[cn] ") {
			keep = append(keep, l)
		}
	}
	return strings.TrimRight(strings.Join(keep, "\n"), "\n"), nil
}

// nodeHookCommands are the Node engine's per-call hook entries.
var nodeHookCommands = map[string]string{
	"pre-tool-use":       "engine/hooks/pretooluse-command.mjs",
	"post-tool-use":      "engine/hooks/post-tool-use-command.mjs",
	"user-prompt-submit": "engine/hooks/user-prompt-submit-command.mjs",
}

func run(dir string, env []string, stdin string, name string, args ...string) (string, string, int, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code, err = ee.ExitCode(), nil
	}
	return out.String(), errb.String(), code, err
}

// Node is the frozen Node engine checkout.
type Node struct{ Root string }

func (Node) Name() string { return "node" }

func (n Node) env(dir string) []string {
	return []string{"CLAUDE_PROJECT_DIR=" + dir, "CLAUDINITE_CHECKS_NO_FETCH=1"}
}

func (n Node) Settings(dir string, node map[string]any) (string, error) {
	raw, err := jsonIndent(node)
	if err != nil {
		return "", err
	}
	return ".claudinite-settings.json", os.WriteFile(filepath.Join(dir, ".claudinite-settings.json"), raw, 0o644)
}

func (n Node) Rules(dir string) (string, error) {
	out, stderr, code, err := run(dir, n.env(dir), "", "node", filepath.Join(n.Root, "engine/pack_loader/generate-rules-index.mjs"), dir)
	if err != nil || code != 0 {
		return "", fmt.Errorf("generate-rules-index: exit %d %v: %s", code, err, stderr)
	}
	return out, nil
}

func (n Node) Skills(dir string) (string, error) {
	out, stderr, code, err := run(dir, n.env(dir), "", "node", filepath.Join(n.Root, "engine/pack_loader/generate-skills-index.mjs"), dir)
	if err != nil || code != 0 {
		return "", fmt.Errorf("generate-skills-index: exit %d %v: %s", code, err, stderr)
	}
	return out, nil
}

func (n Node) Flat(dir string) (string, error) {
	out, stderr, code, err := run(dir, n.env(dir), "", "node", filepath.Join(n.Root, "engine/pack_loader/generate-flat-declarations.mjs"), dir)
	if err != nil || code != 0 {
		return "", fmt.Errorf("generate-flat-declarations: exit %d %v: %s", code, err, stderr)
	}
	return out, nil
}

func (n Node) Mounts(dir string) (map[string]string, error) {
	_, stderr, code, err := run(dir, n.env(dir), "", "node", filepath.Join(n.Root, "engine/pack_loader/mount-skills.mjs"))
	if err != nil || code != 0 {
		return nil, fmt.Errorf("mount-skills: exit %d %v: %s", code, err, stderr)
	}
	out := map[string]string{}
	root := filepath.Join(dir, ".claude/skills")
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if e.Type()&os.ModeSymlink == 0 {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, e.Name(), "SKILL.md"))
		if err != nil {
			return nil, err
		}
		out[e.Name()] = string(b)
	}
	return out, nil
}

var nodeHeader = regexp.MustCompile(`^\[(BLOCKING|ADVISORY)\] (\S+)  (.*)$`)

// parseLoc splits a finding's location. A line in the declaration is
// dropped: the two engines write it in different formats, so the same
// line number names a different line in each.
func parseLoc(loc string) (string, int) {
	if i := strings.LastIndex(loc, ":"); i > 0 {
		if n, err := strconv.Atoi(loc[i+1:]); err == nil {
			if normPath(loc[:i]) == SettingsPath {
				return loc[:i], 0
			}
			return loc[:i], n
		}
	}
	return loc, 0
}

func parseNode(out string) []Finding {
	var fs []Finding
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		m := nodeHeader.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		path, line := parseLoc(m[3])
		onFail := "block"
		if m[1] == "ADVISORY" {
			onFail = "advise"
		}
		fs = append(fs, Finding{Rule: bareRule(m[2]), Path: normPath(path), Line: line, OnFail: onFail})
	}
	return fs
}

func (n Node) World(dir string) ([]Finding, error) {
	out, stderr, code, err := run(dir, n.env(dir), "", "node", filepath.Join(n.Root, "engine/checks/check_the_world.mjs"), "--root", dir)
	if err != nil || code > 1 {
		return nil, fmt.Errorf("check_the_world: exit %d %v: %s", code, err, stderr)
	}
	return parseNode(out), nil
}

func (n Node) Work(dir, transcript string) ([]Finding, error) {
	args := []string{filepath.Join(n.Root, "engine/checks/check_the_work.mjs"), "--root", dir}
	if transcript != "" {
		args = append(args, "--transcript", transcript)
	}
	out, stderr, code, err := run(dir, n.env(dir), "", "node", args...)
	if err != nil || code > 1 {
		return nil, fmt.Errorf("check_the_work: exit %d %v: %s", code, err, stderr)
	}
	return parseNode(out), nil
}

func (n Node) Hook(dir, event, payload string) (Verdict, error) {
	cmd, ok := nodeHookCommands[event]
	if !ok {
		return Verdict{}, fmt.Errorf("no Node hook for %s", event)
	}
	out, stderr, code, err := run(dir, n.env(dir), payload, "node", filepath.Join(n.Root, cmd))
	if err != nil {
		return Verdict{}, err
	}
	ctx, err := hookOutput(out)
	return Verdict{Exit: code, Stderr: stderr, Context: ctx}, err
}

// Cn is a built cn binary.
type Cn struct {
	Binary string
	// Cache isolates the engine's caches from the machine's.
	Cache string
}

func (Cn) Name() string { return "cn" }

func (c Cn) env(dir string) []string {
	return []string{"CLAUDE_PROJECT_DIR=" + dir, "CLAUDINITE_CHECKS_NO_FETCH=1", "XDG_CACHE_HOME=" + c.Cache}
}

// Settings writes the development pin into .claudinite/settings.yaml and
// has cn import the Node declaration into it, `cn settings import`, the
// one reader of .claudinite-settings.json. The declaration is written
// beside dir, never into it, so the member holds no Node file.
func (c Cn) Settings(dir string, node map[string]any) (string, error) {
	raw, err := jsonIndent(node)
	if err != nil {
		return "", err
	}
	from := strings.TrimSuffix(dir, string(filepath.Separator)) + ".claudinite-settings.json"
	if err := os.WriteFile(from, raw, 0o644); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(dir, ".claudinite"), 0o755); err != nil {
		return "", err
	}
	pin := fmt.Sprintf("engine:\n  version: \"0.0.0\"\n  manifest: %q\n", DevManifest)
	if err := os.WriteFile(filepath.Join(dir, ".claudinite/settings.yaml"), []byte(pin), 0o644); err != nil {
		return "", err
	}
	out, stderr, code, err := run(dir, c.env(dir), "", c.Binary, "settings", "import", "--from", from, "--repo", dir)
	if err != nil || code != 0 {
		return "", fmt.Errorf("cn settings import: exit %d %v: %s%s", code, err, out, stderr)
	}
	return ".claudinite/settings.yaml", nil
}

const (
	indexRel  = ".claudinite/cache/claudinite-rules.GENERATED.md"
	skillsRel = ".claudinite/cache/claudinite-skills.GENERATED.md"
)

func (c Cn) sessionStart(dir string) error {
	in := `{"session_id":"parity","hook_event_name":"SessionStart","source":"startup","cwd":"` + dir + `"}`
	_, stderr, code, err := run(dir, c.env(dir), in, c.Binary, "hook", "session-start")
	if err != nil || code != 0 {
		return fmt.Errorf("cn hook session-start: exit %d %v: %s", code, err, stderr)
	}
	return nil
}

func (c Cn) Flat(dir string) (string, error) {
	out, stderr, code, err := run(dir, c.env(dir), "", c.Binary, "tasks", "flat", "--repo", dir)
	if err != nil || code != 0 {
		return "", fmt.Errorf("cn tasks flat: exit %d %v: %s", code, err, stderr)
	}
	rest, _, err := splitMemberFile(out)
	return rest, err
}

// MemberFileRel is the flat file only cn writes, stating the member.
const MemberFileRel = ".claudinite/cache/member.GENERATED.json"

// WriteMemberFile writes the member file cn produces for dir, as an
// adoption would leave it beside the settings file.
func (c Cn) WriteMemberFile(dir string) error {
	out, stderr, code, err := run(dir, c.env(dir), "", c.Binary, "tasks", "flat", "--repo", dir)
	if err != nil || code != 0 {
		return fmt.Errorf("cn tasks flat: exit %d %v: %s", code, err, stderr)
	}
	_, member, err := splitMemberFile(out)
	if err != nil || member == "" {
		return err
	}
	p := filepath.Join(dir, filepath.FromSlash(MemberFileRel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(member), 0o644)
}

// splitMemberFile parts cn's flat output into the documents Node writes
// too and the member file, the document stating a settings file, which
// only cn writes; each keeps its bytes.
func splitMemberFile(out string) (rest, member string, err error) {
	dec := json.NewDecoder(strings.NewReader(out))
	var kept strings.Builder
	for {
		start := dec.InputOffset()
		var doc map[string]json.RawMessage
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				return kept.String(), member, nil
			}
			return "", "", fmt.Errorf("cn tasks flat: %v", err)
		}
		text := strings.TrimLeft(out[start:dec.InputOffset()], "\n") + "\n"
		if _, ok := doc["settings"]; ok {
			member = text
			continue
		}
		kept.WriteString(text)
	}
}

func (c Cn) Rules(dir string) (string, error) {
	if err := c.sessionStart(dir); err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(dir, indexRel))
	if os.IsNotExist(err) {
		return "", nil
	}
	return string(b), err
}

func (c Cn) Skills(dir string) (string, error) {
	if err := c.sessionStart(dir); err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(dir, skillsRel))
	if os.IsNotExist(err) {
		return "", nil
	}
	return string(b), err
}

func (c Cn) Mounts(dir string) (map[string]string, error) {
	if err := c.sessionStart(dir); err != nil {
		return nil, err
	}
	out := map[string]string{}
	root := filepath.Join(dir, ".claude/skills")
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(root, e.Name(), ".claudinite-mount")); err != nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, e.Name(), "SKILL.md"))
		if err != nil {
			return nil, err
		}
		out[e.Name()] = string(b)
	}
	return out, nil
}

// SessionID is the session every scenario's payloads name.
const SessionID = "parity"

func (c Cn) Hook(dir, event, payload string) (Verdict, error) {
	out, stderr, code, err := run(dir, c.env(dir), payload, c.Binary, "hook", event)
	if err != nil {
		return Verdict{}, err
	}
	ctx, err := hookOutput(out)
	return Verdict{Exit: code, Stderr: stderr, Context: ctx}, err
}

var cnLine = regexp.MustCompile(`^(finding|advisory|break|deprecation) (\S+) (.*?): `)

func parseCn(out string) []Finding {
	var fs []Finding
	for _, l := range strings.Split(out, "\n") {
		m := cnLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		rule := bareRule(m[2])
		path, line := parseLoc(m[3])
		onFail := "block"
		if m[1] == "advisory" || m[1] == "deprecation" {
			onFail = "advise"
		}
		fs = append(fs, Finding{Rule: rule, Path: normPath(path), Line: line, OnFail: onFail})
	}
	return fs
}

func (c Cn) World(dir string) ([]Finding, error) {
	out, stderr, code, err := run(dir, c.env(dir), "", c.Binary, "check", "world", "--repo", dir)
	if err != nil || code > 1 {
		return nil, fmt.Errorf("cn check world: exit %d %v: %s", code, err, stderr)
	}
	return parseCn(out), nil
}

// Work runs the work-tagged checks through cn check --tag work, the
// selection the Stop hook runs.
func (c Cn) Work(dir, transcript string) ([]Finding, error) {
	args := []string{"check", "--tag", "work", "--repo", dir}
	if transcript != "" {
		args = append(args, "--transcript", transcript)
	}
	out, stderr, code, err := run(dir, c.env(dir), "", c.Binary, args...)
	if err != nil || code > 1 {
		return nil, fmt.Errorf("cn check --tag work: exit %d %v: %s", code, err, stderr)
	}
	return parseCn(out), nil
}

// Listed is one check cn lists.
type Listed struct {
	ID, Pack, Kind string
	Tags           []string
}

var listLine = regexp.MustCompile(`^(\S+) (declared|builtin|coded|judge) \(([^)]*)\) (\S+)(?: since \S+)?$`)

// bareRule is a rule's id without its pack: the Node engine names a few
// coded checks <pack>/<id> and cn names every check so; both compare by
// the id.
func bareRule(rule string) string {
	if i := strings.LastIndex(rule, "/"); i >= 0 {
		return rule[i+1:]
	}
	return rule
}

// List is cn check list over dir.
func (c Cn) List(dir string) ([]Listed, error) {
	out, stderr, code, err := run(dir, c.env(dir), "", c.Binary, "check", "list", "--repo", dir)
	if err != nil || code != 0 {
		return nil, fmt.Errorf("cn check list: exit %d %v: %s", code, err, stderr)
	}
	var ls []Listed
	for _, l := range strings.Split(out, "\n") {
		m := listLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		pack, id := "", m[1]
		if i := strings.LastIndex(id, "/"); i >= 0 {
			pack, id = id[:i], id[i+1:]
		}
		ls = append(ls, Listed{ID: id, Pack: pack, Kind: m[2], Tags: strings.Split(m[3], ", ")})
	}
	return ls, nil
}

func sortFindings(fs []Finding) []Finding {
	out := append([]Finding{}, fs...)
	sort.Slice(out, func(i, k int) bool { return out[i].String() < out[k].String() })
	return out
}

// diff is the symmetric difference of two finding sets.
func diff(a, b []Finding) (onlyA, onlyB []Finding, agreed int) {
	inA, inB := map[Finding]bool{}, map[Finding]bool{}
	for _, f := range a {
		inA[f] = true
	}
	for _, f := range b {
		inB[f] = true
	}
	for f := range inA {
		if inB[f] {
			agreed++
		} else {
			onlyA = append(onlyA, f)
		}
	}
	for f := range inB {
		if !inA[f] {
			onlyB = append(onlyB, f)
		}
	}
	return sortFindings(onlyA), sortFindings(onlyB), agreed
}
