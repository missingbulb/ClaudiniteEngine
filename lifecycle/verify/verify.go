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

	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

// Input is what verify reads besides the repo: the launcher this binary
// embeds and the hashes of the launchers earlier releases shipped.
type Input struct {
	Repo     string
	Launcher []byte
	Shipped  []string
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
	if _, _, err := settings.Find(in.Repo); err != nil {
		return []findings.Finding{brk("settings-file", ".claudinite", err.Error()+"; keep the one that pins the engine and delete the rest")}
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
	for _, w := range []string{".github/workflows/claudinite-update.yml", ".github/workflows/claudinite-ci.yml"} {
		if _, ok := read(in, w); !ok {
			out = append(out, dep("member-workflows", w, "missing: the repo still works, but the engine never updates on its own until a person adds it from the engine's templates"))
		}
	}
	return out
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
		rel := packset.TreeRel(id) + "/pack.json"
		if _, err := packset.ReadManifest(packset.Tree(in.Repo, id)); errors.Is(err, packset.ErrNoManifest) {
			out = append(out, brk("pack-declared", rel, "the settings declare "+id+" but the repo does not hold it; vendor it with `cn adopt "+id+"`, or remove it from packs.declared"))
		} else if err != nil {
			out = append(out, brk("pack-declared", rel, err.Error()))
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
		rel := packset.TreeRel(id) + "/pack.json"
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
