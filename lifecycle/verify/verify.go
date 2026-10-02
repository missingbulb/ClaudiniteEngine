// Package verify is cn verify: the member-file checks of this engine
// version over a working tree. Each finding is a break, which means this
// engine would not work on the repo as it stands, or a deprecation, an old
// shape that still works. The updater runs a new binary's verify against
// the repo before it opens a PR, so its exit code is the answer to "would
// the new engine break this repo".
//
// testdata/shapes holds one fixture per member-file shape an earlier
// release of this major accepted; every one must raise only deprecations.
package verify

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/lifecycle/rulesindex"
	"github.com/missingbulb/ClaudiniteEngine/shared/descriptor"
	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings/node"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

// Input is what verify reads besides the repo: the launcher this binary
// embeds and the hashes of the launchers earlier releases shipped.
type Input struct {
	Repo     string
	Launcher []byte
	Shipped  []string
	// Declared reads the active packs' declared checks; nil skips the rules
	// that need them. The caller injects it, so verify imports no checks
	// capability.
	Declared func(repo string) DeclaredChecks
}

// DeclaredChecks is what the declared-checks loader found in a repo's
// active packs: every check by each name a rule may use (bare id and
// <pack>/<id>), and each descriptor that did not load. Partial says the
// list is incomplete (the coded checks could not be listed), so a rule
// naming no listed check proves nothing.
type DeclaredChecks struct {
	IDs     []string
	Faults  []DescriptorFault
	Partial bool
}

// DescriptorFault is a descriptor that did not load: Duplicate when its
// folder holds two spellings, else a parse or schema fault.
type DescriptorFault struct {
	Path, Sentence string
	Duplicate      bool
}

type rule struct {
	id string
	// check is nil for a rule another reader reaches (PackManifest).
	check func(Input) []findings.Finding
}

var rules = []rule{
	{"settings-file", checkSettingsFile},
	{"engine-pin", checkEnginePin},
	{"launcher", checkLauncher},
	{"hooks", checkHooks},
	{"member-workflows", checkWorkflows},
	{"bin-ignore", checkBinIgnore},
	{"min-engine-version-legacy", nil},
	{"pack-declared", checkPackDeclared},
	{"pack-min-engine", checkPackMinEngine},
	{"license-plan", checkLicensePlan},
	{"descriptor-format", checkDescriptorFormat},
	{"descriptor-duplicate", checkDescriptorDuplicate},
	{"settings-checks", checkSettingsChecks},
	{"rules-index-current", checkRulesIndex},
	{"claude-md-import", checkClaudeMDImport},
	{"local-pack-shape", checkLocalPackShape},
	{"node-leftovers", checkNodeLeftovers},
}

// RuleIDs lists the registered rules, sorted.
func RuleIDs() []string {
	var out []string
	for _, r := range rules {
		out = append(out, r.id)
	}
	sort.Strings(out)
	return out
}

// Verify runs every rule over in.Repo. A repo with no settings file at all
// is not a member, and gets that one break rather than one per rule.
func Verify(in Input) []findings.Finding {
	if !anySettings(in) {
		return checkSettingsFile(in)
	}
	var out []findings.Finding
	for _, r := range rules {
		if r.check != nil {
			out = append(out, r.check(in)...)
		}
	}
	return out
}

func brk(id, path, sentence string) findings.Finding {
	return findings.Finding{Class: findings.Break, ID: id, Path: path, Sentence: sentence}
}

func dep(id, path, sentence string) findings.Finding {
	return findings.Finding{Class: findings.Deprecation, ID: id, Path: path, Sentence: sentence}
}

func read(in Input, rel string) ([]byte, bool) {
	raw, err := os.ReadFile(filepath.Join(in.Repo, filepath.FromSlash(rel)))
	return raw, err == nil
}

func anySettings(in Input) bool {
	for _, f := range settings.Formats {
		if _, ok := read(in, settings.RelPath(f)); ok {
			return true
		}
	}
	return false
}

func checkSettingsFile(in Input) []findings.Finding {
	if _, _, err := settings.Find(in.Repo); err != nil && !anySettings(in) {
		if _, ok := read(in, node.File); ok {
			return []findings.Finding{brk("settings-file", node.File, "this repo is on the Node engine; `cn settings import` reads its declaration once the pin is written")}
		}
		return []findings.Finding{brk("settings-file", ".claudinite", err.Error())}
	}
	return nil
}

func checkEnginePin(in Input) []findings.Finding {
	p, f, err := settings.Find(in.Repo)
	if err != nil {
		return nil
	}
	raw, err := os.ReadFile(p)
	rel := settings.RelPath(f)
	if err != nil {
		return []findings.Finding{brk("engine-pin", rel, err.Error())}
	}
	if _, err := settings.ReadEngine(raw, f); err != nil {
		return []findings.Finding{brk("engine-pin", rel, err.Error()+"; the launcher refuses to run until the engine block holds a quoted version, manifest and, if any, package")}
	}
	return nil
}

// checkLicensePlan breaks on a license.plan the key server does not know;
// no block passes, since the paid plans are the default.
func checkLicensePlan(in Input) []findings.Finding {
	p, f, err := settings.Find(in.Repo)
	if err != nil {
		return nil
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	if _, err := settings.ReadLicense(raw, f); err != nil {
		return []findings.Finding{brk("license-plan", settings.RelPath(f), err.Error()+"; sessions ask the paid key server until it names a plan the server knows, quoted")}
	}
	return nil
}

func checkLauncher(in Input) []findings.Finding {
	raw, ok := read(in, ".claudinite/launch")
	if !ok {
		return []findings.Finding{brk("launcher", ".claudinite/launch", "the launcher is missing; restore it from the engine release this repo pins")}
	}
	if bytes.Equal(raw, in.Launcher) {
		return nil
	}
	sum := sha256.Sum256(raw)
	h := hex.EncodeToString(sum[:])
	for _, s := range in.Shipped {
		if s == h {
			return nil
		}
	}
	return []findings.Finding{brk("launcher", ".claudinite/launch", "the launcher is not one a Claudinite release shipped; only the update and cn init write it, so restore it unchanged")}
}

// HookWiring is one hook event and the command a member wires to it.
type HookWiring struct{ Event, Command string }

// Hooks are the wirings the member fixture and cn init write.
var Hooks = []HookWiring{
	{"SessionStart", `sh "$CLAUDE_PROJECT_DIR/.claudinite/launch" hook session-start`},
	{"PreToolUse", ".claudinite/bin/cn hook pre-tool-use"},
	{"PostToolUse", ".claudinite/bin/cn hook post-tool-use"},
	{"UserPromptSubmit", ".claudinite/bin/cn hook user-prompt-submit"},
	{"Stop", ".claudinite/bin/cn hook stop"},
	{"SessionEnd", ".claudinite/bin/cn hook session-end"},
}

// nodeHooks is where the Node engine's hook commands live in a member.
const nodeHooks = ".claudinite/shared/engine/hooks/"

func checkHooks(in Input) []findings.Finding {
	var cfg struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	raw, ok := read(in, ".claude/settings.json")
	if ok {
		_ = json.Unmarshal(raw, &cfg)
	}
	var out []findings.Finding
	events := make([]string, 0, len(cfg.Hooks))
	for e := range cfg.Hooks {
		events = append(events, e)
	}
	sort.Strings(events)
	for _, event := range events {
		for _, group := range cfg.Hooks[event] {
			for _, c := range group.Hooks {
				if strings.Contains(c.Command, nodeHooks) {
					return []findings.Finding{brk("hooks", ".claude/settings.json", event+" runs the Node engine's hooks under "+nodeHooks+", which the move removes; the move skill wires every hook to cn")}
				}
			}
		}
	}
	for _, h := range Hooks {
		wired := false
		for _, group := range cfg.Hooks[h.Event] {
			for _, c := range group.Hooks {
				wired = wired || strings.TrimSpace(c.Command) == h.Command
			}
		}
		if wired {
			continue
		}
		if h.Event == "SessionStart" {
			out = append(out, brk("hooks", ".claude/settings.json", "SessionStart does not run `"+h.Command+"`, so no session loads the engine; add that hook"))
		} else {
			out = append(out, dep("hooks", ".claude/settings.json", h.Event+" does not run `"+h.Command+"`; Claude Code runs without it, but that hook's checks never run"))
		}
	}
	return out
}

func checkWorkflows(in Input) []findings.Finding {
	var out []findings.Finding
	if ci, ok := read(in, ".github/workflows/claudinite-ci.yml"); ok && pullRequestFiltered(ci) {
		out = append(out, dep("member-workflows", ".github/workflows/claudinite-ci.yml", "filters its pull_request trigger by path, so a pull request touching none of those paths runs no engine check; drop the filter (`cn workflows diff` prints the patch)"))
	} else if !ok {
		out = append(out, dep("member-workflows", ".github/workflows/claudinite-ci.yml", "missing: the repo still works, but no pull request runs the engine's checks and no update pull request lands until a person adds it from the engine's templates"))
	}
	const scheduler, executor, update = ".github/workflows/claudinite-scheduler.yml", ".github/workflows/claudinite-executor.yml", ".github/workflows/claudinite-update.yml"
	_, hasScheduler := read(in, scheduler)
	_, hasExecutor := read(in, executor)
	_, hasUpdate := read(in, update)
	switch {
	case hasScheduler && !hasExecutor:
		out = append(out, brk("member-workflows", executor, "missing while the scheduler runs: it files work items no executor ever picks up; add it from the engine's templates (`cn workflows diff` prints the patch)"))
	case !hasScheduler:
		for _, w := range []string{scheduler, executor} {
			if _, ok := read(in, w); !ok {
				out = append(out, dep("member-workflows", w, "missing: the repo still works, but no task of its packs, the engine's own update included, runs until a person adds it from the engine's templates"))
			}
		}
	}
	if hasUpdate && hasScheduler && hasExecutor {
		out = append(out, dep("member-workflows", update, "superseded by the engine/update task, which the queue runs; until it is deleted the task stands aside and this workflow runs the update (`cn workflows diff` prints the patch)"))
	}
	return out
}

// pullRequestFiltered is a workflow whose on.pull_request trigger carries
// paths or paths-ignore.
func pullRequestFiltered(raw []byte) bool {
	lines := strings.Split(string(raw), "\n")
	inOn, prIndent := false, -1
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		indent := len(l) - len(strings.TrimLeft(l, " "))
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if indent == 0 {
			inOn, prIndent = strings.HasPrefix(trimmed, "on:"), -1
			continue
		}
		if !inOn {
			continue
		}
		if prIndent >= 0 && indent <= prIndent {
			prIndent = -1
		}
		if prIndent < 0 && strings.HasPrefix(trimmed, "pull_request:") {
			prIndent = indent
			continue
		}
		if prIndent >= 0 && (strings.HasPrefix(trimmed, "paths:") || strings.HasPrefix(trimmed, "paths-ignore:")) {
			return true
		}
	}
	return false
}

func hasLine(raw []byte, want ...string) bool {
	for _, l := range strings.Split(string(raw), "\n") {
		l = strings.TrimSpace(l)
		for _, w := range want {
			if l == w {
				return true
			}
		}
	}
	return false
}

func checkBinIgnore(in Input) []findings.Finding {
	if raw, ok := read(in, ".claudinite/.gitignore"); ok && hasLine(raw, "bin/", "/bin/", "bin") {
		return nil
	}
	if raw, ok := read(in, ".gitignore"); ok && hasLine(raw, ".claudinite/bin/", "/.claudinite/bin/", ".claudinite/bin") {
		return []findings.Finding{dep("bin-ignore", ".gitignore", "ignores .claudinite/bin/ from the repo root's .gitignore, an old shape: move it to .claudinite/.gitignore as a line `bin/`, since nothing of Claudinite's belongs in the repo root")}
	}
	return []findings.Finding{brk("bin-ignore", ".claudinite/.gitignore", "nothing ignores .claudinite/bin/, so the linked engine binary can be committed: add .claudinite/.gitignore holding `bin/`")}
}

// declaredPacks reads the packs block, or nil when the settings file is
// missing or unreadable (settings-file and pack-declared report that).
func declaredPacks(in Input) ([]string, error) {
	if _, _, err := settings.Find(in.Repo); err != nil {
		return nil, nil
	}
	p, err := packset.Declared(in.Repo)
	return p.Declared, err
}

func checkPackDeclared(in Input) []findings.Finding {
	declared, err := declaredPacks(in)
	if err != nil {
		_, f, _ := settings.Find(in.Repo)
		return []findings.Finding{brk("pack-declared", settings.RelPath(f), err.Error())}
	}
	var out []findings.Finding
	isDeclared := map[string]bool{}
	for _, id := range declared {
		isDeclared[id] = true
		if _, err := packset.ReadManifest(packset.Tree(in.Repo, id)); errors.Is(err, packset.ErrNoManifest) {
			if today, ok := node.Renamed[id]; ok {
				out = append(out, brk("pack-declared", packset.TreeRel(id), "the settings declare "+id+", which was renamed to or absorbed into "+today+"; declare "+today+" instead"))
				continue
			}
			out = append(out, brk("pack-declared", packset.TreeRel(id), "the settings declare "+id+" but the repo does not hold it; vendor it with `cn adopt "+id+"`, or remove it from packs.declared"))
		}
	}
	if p, err := packset.Declared(in.Repo); err == nil {
		for _, name := range p.Local {
			rel := packset.LocalDir + "/" + name
			if st, err := os.Stat(filepath.Join(in.Repo, filepath.FromSlash(rel))); err != nil || !st.IsDir() {
				out = append(out, brk("pack-declared", rel, "the settings declare local/"+name+" but the repo holds no "+rel+"/; create it, or remove local/"+name+" from packs.declared"))
			}
		}
	}
	vendored, _ := packset.Vendored(in.Repo)
	for _, id := range vendored {
		if !isDeclared[id] {
			out = append(out, dep("pack-declared", packset.TreeRel(id), "the repo holds "+id+" but the settings do not declare it, so nothing loads it; delete the folder, or declare it"))
		}
	}
	return out
}

func checkPackMinEngine(in Input) []findings.Finding {
	declared, err := declaredPacks(in)
	if err != nil {
		return nil
	}
	pin := ""
	if p, f, err := settings.Find(in.Repo); err == nil {
		if raw, err := os.ReadFile(p); err == nil {
			if e, err := settings.ReadEngine(raw, f); err == nil {
				pin = e.Version
			}
		}
	}
	var out []findings.Finding
	for _, id := range declared {
		m, err := packset.ReadManifest(packset.Tree(in.Repo, id))
		if err != nil {
			continue
		}
		rel := packset.TreeRel(id) + "/" + m.File
		legacy := PackManifest(rel, m.MinEngineVersion)
		out = append(out, legacy...)
		if len(legacy) != 0 || pin == "" {
			continue
		}
		if min, _ := version.ParseMinEngineVersion(m.MinEngineVersion); !min.Satisfies(pin) {
			out = append(out, brk("pack-min-engine", rel, fmt.Sprintf("%s %s needs engine %s or newer, but the repo pins %s; the engine refuses to load it until the pin moves past that or the pack moves back", id, m.Version, m.MinEngineVersion, pin)))
		}
	}
	return out
}

// PackManifest checks a declared pack's minEngineVersion.
func PackManifest(path, minEngineVersion string) []findings.Finding {
	m, err := version.ParseMinEngineVersion(minEngineVersion)
	if err != nil {
		return []findings.Finding{brk("min-engine-version-legacy", path, err.Error())}
	}
	if m.Legacy() {
		return []findings.Finding{dep("min-engine-version-legacy", path, "minEngineVersion "+minEngineVersion+" is the old two-part form, which any engine satisfies; publish a version declaring <day>.<n>.<patch>")}
	}
	return nil
}

// declaredTrees are the directories of the declared packs, canon and local.
func declaredTrees(in Input) []string {
	if _, _, err := settings.Find(in.Repo); err != nil {
		return nil
	}
	p, err := packset.Declared(in.Repo)
	if err != nil {
		return nil
	}
	var out []string
	for _, id := range p.Declared {
		out = append(out, packset.TreeRel(id))
	}
	for _, name := range p.Local {
		out = append(out, packset.LocalDir+"/"+name)
	}
	return out
}

// checkDescriptorFormat breaks on a declared pack's manifest or declared
// checks that do not parse or validate: the engine does not load that pack.
func checkDescriptorFormat(in Input) []findings.Finding {
	var out []findings.Finding
	for _, rel := range declaredTrees(in) {
		dir := filepath.Join(in.Repo, filepath.FromSlash(rel))
		var err error
		if strings.HasPrefix(rel, packset.LocalDir+"/") {
			_, err = packset.ReadOwnManifest(dir)
		} else {
			_, err = packset.ReadManifest(dir)
		}
		if err == nil || errors.Is(err, packset.ErrNoManifest) || errors.Is(err, descriptor.ErrDuplicate) {
			continue
		}
		out = append(out, brk("descriptor-format", rel, err.Error()+"; the engine does not load this pack until it parses"))
	}
	if in.Declared != nil {
		for _, f := range in.Declared(in.Repo).Faults {
			if !f.Duplicate {
				out = append(out, brk("descriptor-format", f.Path, f.Sentence))
			}
		}
	}
	return out
}

// checkDescriptorDuplicate breaks on two spellings of one descriptor in one
// folder: the settings, a declared pack's manifest or its declared checks.
func checkDescriptorDuplicate(in Input) []findings.Finding {
	var out []findings.Finding
	var found []string
	for _, f := range settings.Formats {
		if _, ok := read(in, settings.RelPath(f)); ok {
			found = append(found, settings.RelPath(f))
		}
	}
	if len(found) > 1 {
		out = append(out, brk("descriptor-duplicate", ".claudinite", "holds "+strings.Join(found, " and ")+"; keep the one that pins the engine and delete the rest"))
	}
	for _, rel := range declaredTrees(in) {
		if _, _, err := descriptor.Find(filepath.Join(in.Repo, filepath.FromSlash(rel)), packset.ManifestName); errors.Is(err, descriptor.ErrDuplicate) {
			out = append(out, brk("descriptor-duplicate", rel, err.Error()))
		}
	}
	if in.Declared != nil {
		for _, f := range in.Declared(in.Repo).Faults {
			if f.Duplicate {
				out = append(out, brk("descriptor-duplicate", f.Path, f.Sentence))
			}
		}
	}
	return out
}

// checkSettingsChecks judges the settings' rule overrides and acceptances:
// a value outside block, advise and off, an acceptance with no reason or
// two sources setting one rule differently is a break; a rule naming no
// check of the declared packs is a deprecation.
func checkSettingsChecks(in Input) []findings.Finding {
	p, f, err := settings.Find(in.Repo)
	if err != nil {
		return nil
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	parsed, err := settings.ParseFile(raw, f)
	if err != nil {
		return nil
	}
	rel := settings.RelPath(f)
	var out []findings.Finding
	rules, accept, conflicts := parsed.Effective()
	ids := make([]string, 0, len(rules))
	for id := range rules {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		v := rules[id]
		ok := false
		for _, want := range settings.OnFailValues {
			ok = ok || v == want
		}
		if !ok {
			out = append(out, brk("settings-checks", rel, fmt.Sprintf("rules.%s is %q; an override is \"block\", \"advise\" or \"off\"", id, v)))
		}
	}
	for _, c := range conflicts {
		out = append(out, brk("settings-checks", rel, c+"; set it in one place"))
	}
	for _, a := range accept {
		if strings.TrimSpace(a.Reason) == "" {
			out = append(out, brk("settings-checks", rel, fmt.Sprintf("the acceptance of %s%s has no reason; an acceptance is reviewable only by its reason", a.Rule, onPath(a.Path))))
		}
	}
	retired := append([]settings.RetiredOverride{}, parsed.Retired...)
	sort.Slice(retired, func(i, k int) bool {
		return retired[i].Where+retired[i].Rule < retired[k].Where+retired[k].Rule
	})
	for _, r := range retired {
		out = append(out, dep("settings-checks", rel, fmt.Sprintf("rules.%s on %s is %q, the retired spelling; write %q", r.Rule, r.Where, r.Value, r.OnFail)))
	}
	if parsed.LegacySharedConstants {
		out = append(out, dep("settings-checks", rel, "carries a top-level sharedConstants; move it to the basics entry's config (packs.declared: - id: basics, config: {sharedConstants: …}), where basics/shared-constants reads it"))
	}
	if dc := declaredOf(in); dc != nil && !dc.Partial {
		known := map[string]bool{}
		for _, id := range dc.IDs {
			known[id] = true
		}
		named := map[string]bool{}
		for _, id := range ids {
			named[id] = true
		}
		for _, a := range accept {
			named[a.Rule] = true
		}
		var unknown []string
		for id := range named {
			if !known[id] {
				unknown = append(unknown, id)
			}
		}
		sort.Strings(unknown)
		for _, id := range unknown {
			out = append(out, dep("settings-checks", rel, fmt.Sprintf("names rule %q, which no declared pack's check carries; drop the entry, or declare the pack that carries it", id)))
		}
	}
	return out
}

func declaredOf(in Input) *DeclaredChecks {
	if in.Declared == nil {
		return nil
	}
	dc := in.Declared(in.Repo)
	return &dc
}

func onPath(p string) string {
	if p == "" {
		return ""
	}
	return " on " + p
}

func pinOf(in Input) string {
	p, f, err := settings.Find(in.Repo)
	if err != nil {
		return "0.0.0"
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return "0.0.0"
	}
	e, err := settings.ReadEngine(raw, f)
	if err != nil {
		return "0.0.0"
	}
	return e.Version
}

// checkRulesIndex breaks when the rules index differs from what the
// declaration produces; an absent index is a deprecation until the rules
// channel's live measurement makes it a break.
func checkRulesIndex(in Input) []findings.Finding {
	if _, _, err := settings.Find(in.Repo); err != nil {
		return nil
	}
	st, _, err := rulesindex.Check(in.Repo, pinOf(in))
	switch {
	case err != nil:
		return nil
	case st == rulesindex.Stale:
		return []findings.Finding{brk("rules-index-current", rulesindex.File, "is not the import index the declared packs produce, so sessions read another set of rules; run `cn rules-index` and commit it")}
	case st == rulesindex.Absent:
		return []findings.Finding{dep("rules-index-current", rulesindex.File, "is missing, so no session reads the declared packs' rules; run `cn rules-index` and commit it")}
	}
	return nil
}

// checkClaudeMDImport deprecates a CLAUDE.md without the index's import
// line while the declared packs have prose to import.
func checkClaudeMDImport(in Input) []findings.Finding {
	if _, _, err := settings.Find(in.Repo); err != nil {
		return nil
	}
	if st, _, err := rulesindex.Check(in.Repo, pinOf(in)); err != nil || st == rulesindex.Empty {
		return nil
	}
	if rulesindex.HasImport(in.Repo) {
		return nil
	}
	return []findings.Finding{dep("claude-md-import", rulesindex.ClaudeMD, "does not import "+rulesindex.File+" on a line of its own, so sessions never read the declared packs' rules; add the line `"+rulesindex.Import+"`")}
}
