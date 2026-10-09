package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/findings"
)

// cleanMember is a current cn member, holding nothing of the Node engine.
const cleanMember = "../../cn/lifecycle/verify/testdata/shapes/v12-member-own-hook"

func leftoverIDs(fs []findings.Finding, class findings.Class) []string {
	var out []string
	for _, f := range fs {
		if f.Class == class {
			out = append(out, f.ID)
		}
	}
	return out
}

func TestLeftovers(t *testing.T) {
	cases := []struct {
		name         string
		mutate       func(t *testing.T, d string)
		breaks, deps []string
	}{
		{"a clean member", func(*testing.T, string) {}, nil, nil},
		{"the Node declaration beside", func(t *testing.T, d string) {
			write(t, d, ".claudinite-settings.json", `{"packs": []}`)
		}, nil, []string{"node-leftovers"}},
		{"the Node declaration alone is no leftover", func(t *testing.T, d string) {
			_ = os.Remove(filepath.Join(d, ".claudinite/settings.yaml"))
			write(t, d, ".claudinite-settings.json", `{"packs": []}`)
		}, nil, nil},
		{"the Node mount left", func(t *testing.T, d string) {
			write(t, d, ".claudinite/shared/engine/hooks/x.mjs", "\n")
		}, nil, []string{"node-leftovers"}},
		{"an index at the old path", func(t *testing.T, d string) {
			write(t, d, ".claudinite/claudinite-rules.GENERATED.md", "\n")
			write(t, d, ".claudinite/claudinite-skills.GENERATED.md", "\n")
		}, nil, []string{"node-leftovers", "node-leftovers"}},
		{"a workflow step running the Node checks", func(t *testing.T, d string) {
			write(t, d, ".github/workflows/ci.yml", "jobs:\n  c:\n    steps:\n      - run: node .claudinite/shared/engine/checks/check_the_world.mjs\n")
		}, nil, []string{"node-leftovers"}},
		{"the Node hook log ignored", func(t *testing.T, d string) {
			write(t, d, ".gitignore", "node_modules/\n/.claudinite-hooks.log*\n")
		}, nil, []string{"node-leftovers"}},
		{"the session pack root ignored from the repo root", func(t *testing.T, d string) {
			write(t, d, ".gitignore", "node_modules/\n/.claudinite/temp/\n")
		}, nil, []string{"node-leftovers"}},
		{"both Node lines ignored from the repo root", func(t *testing.T, d string) {
			write(t, d, ".gitignore", "/.claudinite-hooks.log*\n.claudinite/temp\n")
		}, nil, []string{"node-leftovers", "node-leftovers"}},
		{"a local pack's JavaScript rules", func(t *testing.T, d string) {
			raw, _ := os.ReadFile(filepath.Join(d, ".claudinite/settings.yaml"))
			write(t, d, ".claudinite/settings.yaml", strings.Replace(string(raw), "    - id: basics\n", "    - local/mine\n    - id: basics\n", 1))
			write(t, d, ".claudinite/local/packs/mine/pack.json", "{}\n")
			write(t, d, ".claudinite/local/packs/mine/worldRules/a.mjs", "export default {};\n")
			write(t, d, ".claudinite/local/packs/mine/skills/s/checks.mjs", "export default [];\n")
		}, []string{"node-leftovers", "node-leftovers"}, nil},
		{"a local pack module importing what the move removed", func(t *testing.T, d string) {
			raw, _ := os.ReadFile(filepath.Join(d, ".claudinite/settings.yaml"))
			write(t, d, ".claudinite/settings.yaml", strings.Replace(string(raw), "    - id: basics\n", "    - local/mine\n    - id: basics\n", 1))
			write(t, d, ".claudinite/local/packs/mine/pack.json", "{}\n")
			write(t, d, ".claudinite/local/packs/mine/tasks/t/label.mjs", "export const L = 1;\n")
			write(t, d, ".claudinite/local/packs/mine/tasks/t/worker.mjs", "import { L } from './label.mjs';\nimport { gh } from '../../../../../shared/packs/claudinite-tasks/public/github.mjs';\nimport { x } from '@claudinite/sdk';\n")
			write(t, d, ".claudinite/local/packs/mine/tasks/t/worker.test.mjs", "import { finding } from \"../../../../../shared/engine/checks/helpers/findings.mjs\";\n")
		}, []string{"node-leftovers", "node-leftovers"}, nil},
		{"hooks naming the Node engine", func(t *testing.T, d string) {
			write(t, d, ".claude/settings.json", `{"hooks": {"SessionStart": [{"hooks": [{"type": "command", "command": "node .claudinite/shared/engine/hooks/run-session-start.mjs"}]}]}}`)
		}, []string{"hooks"}, nil},
		{"hooks naming the canon's own engine", func(t *testing.T, d string) {
			write(t, d, ".claude/settings.json", `{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "node $CLAUDE_PROJECT_DIR/engine/hooks/stop-command.mjs"}]}]}}`)
		}, []string{"hooks"}, nil},
	}
	for _, c := range cases {
		dir := t.TempDir()
		copyTree(t, cleanMember, dir)
		c.mutate(t, dir)
		fs := leftovers(dir)
		if got := strings.Join(leftoverIDs(fs, findings.Break), " "); got != strings.Join(c.breaks, " ") {
			t.Errorf("%s: breaks [%s], want %v\n%v", c.name, got, c.breaks, fs)
		}
		if got := strings.Join(leftoverIDs(fs, findings.Deprecation), " "); got != strings.Join(c.deps, " ") {
			t.Errorf("%s: deprecations [%s], want %v\n%v", c.name, got, c.deps, fs)
		}
	}
}

// Each half-moved shape cn verify once named, with the leftovers it
// carries.
func TestLeftoverShapes(t *testing.T) {
	want := map[string]string{
		"v8-node-declaration-beside": ".claudinite-settings.json",
		"v8-node-mount-leftover":     ".claudinite/shared/engine",
		"v8-old-index-path":          ".claudinite/claudinite-rules.GENERATED.md",
		"v9-node-ci-step":            ".github/workflows/ci.yml .gitignore",
	}
	dirs, _ := filepath.Glob(filepath.Join("testdata", "leftovers", "*"))
	if len(dirs) != len(want) {
		t.Fatalf("testdata/leftovers holds %d shapes, want %d", len(dirs), len(want))
	}
	for _, d := range dirs {
		var paths []string
		for _, f := range leftovers(d) {
			if f.Class != findings.Deprecation {
				t.Errorf("%s: %s", filepath.Base(d), f)
			}
			paths = append(paths, f.Path)
		}
		if got := strings.Join(paths, " "); got != want[filepath.Base(d)] {
			t.Errorf("%s: leftovers [%s], want [%s]", filepath.Base(d), got, want[filepath.Base(d)])
		}
	}
}

// nodeHook matches the Node engine's hooks under either root, and nothing
// that merely ends in engine/hooks/.
func TestNodeHookMatchesBothRoots(t *testing.T) {
	for cmd, want := range map[string]bool{
		"node $CLAUDE_PROJECT_DIR/.claudinite/shared/engine/hooks/stop-command.mjs": true,
		"node $CLAUDE_PROJECT_DIR/engine/hooks/stop-command.mjs":                    true,
		"node engine/hooks/stop-command.mjs":                                        true,
		"bash \"$CLAUDE_PROJECT_DIR\"/engine/hooks/session-start-command.sh":        true,
		"sh tools/myengine/hooks/stop.sh":                                           false,
		"sh tools/my.engine/hooks/stop.sh":                                          false,
		".claudinite/bin/cn hook stop":                                              false,
	} {
		if got := nodeHook.MatchString(cmd); got != want {
			t.Errorf("%q: %v, want %v", cmd, got, want)
		}
	}
}

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
