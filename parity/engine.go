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
	"fmt"
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
	// Mounts are the skills the engine mounted, with each SKILL.md.
	Mounts(dir string) (map[string]string, error)
	// World and Work are the findings of a whole-repo sweep and of the
	// change.
	World(dir string) ([]Finding, error)
	Work(dir string) ([]Finding, error)
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

func parseLoc(loc string) (string, int) {
	if i := strings.LastIndex(loc, ":"); i > 0 {
		if n, err := strconv.Atoi(loc[i+1:]); err == nil {
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
		fs = append(fs, Finding{Rule: m[2], Path: normPath(path), Line: line, OnFail: onFail})
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

func (n Node) Work(dir string) ([]Finding, error) {
	out, stderr, code, err := run(dir, n.env(dir), "", "node", filepath.Join(n.Root, "engine/checks/check_the_work.mjs"), "--root", dir)
	if err != nil || code > 1 {
		return nil, fmt.Errorf("check_the_work: exit %d %v: %s", code, err, stderr)
	}
	return parseNode(out), nil
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

// Settings translates the Node declaration into .claudinite/settings.yaml:
// its packs become packs.declared (an entry object keeps id, config,
// rules and accept), its top-level rules and accept become the checks
// block.
func (c Cn) Settings(dir string, node map[string]any) (string, error) {
	raw, err := Translate(node)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(dir, ".claudinite"), 0o755); err != nil {
		return "", err
	}
	return ".claudinite/settings.yaml", os.WriteFile(filepath.Join(dir, ".claudinite/settings.yaml"), raw, 0o644)
}

const indexRel = ".claudinite/flat/claudinite-rules.GENERATED.md"

func (c Cn) sessionStart(dir string) error {
	in := `{"session_id":"parity","hook_event_name":"SessionStart","source":"startup","cwd":"` + dir + `"}`
	_, stderr, code, err := run(dir, c.env(dir), in, c.Binary, "hook", "session-start")
	if err != nil || code != 0 {
		return fmt.Errorf("cn hook session-start: exit %d %v: %s", code, err, stderr)
	}
	return nil
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

var cnLine = regexp.MustCompile(`^(finding|advisory|break|deprecation) (\S+) (.*?): `)

func parseCn(out string) []Finding {
	var fs []Finding
	for _, l := range strings.Split(out, "\n") {
		m := cnLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		rule := m[2]
		if i := strings.LastIndex(rule, "/"); i >= 0 {
			rule = rule[i+1:]
		}
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

// Work runs the work-tagged checks through cn check --tag work: the Stop
// hook runs the same selection, behind the license gate a harness has no
// key for.
func (c Cn) Work(dir string) ([]Finding, error) {
	out, stderr, code, err := run(dir, c.env(dir), "", c.Binary, "check", "--tag", "work", "--repo", dir)
	if err != nil || code > 1 {
		return nil, fmt.Errorf("cn check --tag work: exit %d %v: %s", code, err, stderr)
	}
	return parseCn(out), nil
}

// Listed is one check cn lists.
type Listed struct {
	ID, Kind string
	Tags     []string
}

var listLine = regexp.MustCompile(`^(\S+) (declared|builtin|coded) \(([^)]*)\) (\S+)$`)

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
		id := m[1]
		if i := strings.LastIndex(id, "/"); i >= 0 {
			id = id[i+1:]
		}
		ls = append(ls, Listed{ID: id, Kind: m[2], Tags: strings.Split(m[3], ", ")})
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
