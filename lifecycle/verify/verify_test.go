package verify

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/findings"
)

const shapes = "testdata/shapes"

func launcherBytes(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../launcher/launch")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func copyTree(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), raw, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func ids(fs []findings.Finding, class findings.Class) []string {
	var out []string
	for _, f := range fs {
		if f.Class == class {
			out = append(out, f.ID)
		}
	}
	sort.Strings(out)
	return out
}

// newShape is a member in the shape this release writes: no finding.
func newShape(t *testing.T) string {
	t.Helper()
	dir := copyTree(t, filepath.Join(shapes, "v1-yaml"))
	_ = os.Remove(filepath.Join(dir, ".gitignore"))
	write(t, dir, ".claudinite/.gitignore", "bin/\n")
	write(t, dir, ".github/workflows/claudinite-update.yml", "name: claudinite-update\n")
	write(t, dir, ".github/workflows/claudinite-ci.yml", "name: claudinite-ci\n")
	return dir
}

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, dir string) []findings.Finding {
	return Verify(Input{Repo: dir, Launcher: launcherBytes(t)})
}

func TestNewShapeHasNoFinding(t *testing.T) {
	if fs := run(t, newShape(t)); len(fs) != 0 {
		t.Errorf("findings on the new shape: %v", fs)
	}
}

func TestEmptyRepoBreaksOnTheSettingsFile(t *testing.T) {
	fs := run(t, t.TempDir())
	if len(fs) != 1 || !findings.AnyBreak(fs) {
		t.Fatalf("want exactly the settings-file break: %v", fs)
	}
	var found bool
	for _, f := range fs {
		if f.ID == "settings-file" && f.Class == findings.Break && strings.Contains(f.Sentence, "settings.yaml") {
			found = true
		}
	}
	if !found {
		t.Errorf("no settings-file break: %v", fs)
	}
}

func TestRules(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, dir string)
		breaks []string
		deps   []string
	}{
		{"two settings files", func(t *testing.T, d string) { write(t, d, ".claudinite/settings.json", "{}") }, []string{"settings-file"}, nil},
		{"bad version", func(t *testing.T, d string) {
			raw, _ := os.ReadFile(filepath.Join(d, ".claudinite/settings.yaml"))
			write(t, d, ".claudinite/settings.yaml", strings.Replace(string(raw), `"60930.1.0"`, `"60930.1"`, 1))
		}, []string{"engine-pin"}, nil},
		{"bad package", func(t *testing.T, d string) {
			raw, _ := os.ReadFile(filepath.Join(d, ".claudinite/settings.yaml"))
			write(t, d, ".claudinite/settings.yaml", strings.Replace(string(raw), `"@claudinite/cli-rc"`, `"@claudinite/cli-beta"`, 1))
		}, []string{"engine-pin"}, nil},
		{"edited launcher", func(t *testing.T, d string) { write(t, d, ".claudinite/launch", string(launcherBytes(t))+"# edited\n") }, []string{"launcher"}, nil},
		{"no launcher", func(t *testing.T, d string) { _ = os.Remove(filepath.Join(d, ".claudinite/launch")) }, []string{"launcher"}, nil},
		{"no SessionStart", func(t *testing.T, d string) {
			write(t, d, ".claude/settings.json", `{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": ".claudinite/bin/cn hook stop"}]}]}}`)
		}, []string{"hooks"}, []string{"hooks", "hooks", "hooks", "hooks"}},
		{"no Claude settings", func(t *testing.T, d string) { _ = os.Remove(filepath.Join(d, ".claude/settings.json")) }, []string{"hooks"}, []string{"hooks", "hooks", "hooks", "hooks", "hooks"}},
		{"one guard missing", func(t *testing.T, d string) {
			raw, _ := os.ReadFile(filepath.Join(d, ".claude/settings.json"))
			write(t, d, ".claude/settings.json", strings.Replace(string(raw), "cn hook pre-tool-use", "cn hook something-else", 1))
		}, nil, []string{"hooks"}},
		{"no workflows", func(t *testing.T, d string) { _ = os.RemoveAll(filepath.Join(d, ".github")) }, nil, []string{"member-workflows", "member-workflows"}},
		{"old ignore shape", func(t *testing.T, d string) {
			_ = os.Remove(filepath.Join(d, ".claudinite/.gitignore"))
			write(t, d, ".gitignore", "node_modules/\n.claudinite/bin/\n")
		}, nil, []string{"bin-ignore"}},
		{"bin not ignored", func(t *testing.T, d string) { _ = os.Remove(filepath.Join(d, ".claudinite/.gitignore")) }, []string{"bin-ignore"}, nil},
		{"declared pack held", func(t *testing.T, d string) {
			declare(t, d, "acme-pack", `{"version": "1.0", "minEngineVersion": "60930.1.0"}`)
		}, nil, nil},
		{"declared pack missing", func(t *testing.T, d string) {
			declare(t, d, "acme-pack", "")
		}, []string{"pack-declared"}, nil},
		{"vendored pack undeclared", func(t *testing.T, d string) {
			write(t, d, ".claudinite/shared/packs/acme-old/pack.json", `{"version": "1.0", "minEngineVersion": "60930.1.0"}`)
		}, nil, []string{"pack-declared"}},
		{"pack needs a newer engine", func(t *testing.T, d string) {
			declare(t, d, "acme-pack", `{"version": "1.0", "minEngineVersion": "60930.2.0"}`)
		}, []string{"pack-min-engine"}, nil},
		{"pack with two-part minimum", func(t *testing.T, d string) {
			declare(t, d, "acme-pack", `{"version": "1.0", "minEngineVersion": "60928.1"}`)
		}, nil, []string{"min-engine-version-legacy"}},
		{"pack with malformed minimum", func(t *testing.T, d string) {
			declare(t, d, "acme-pack", `{"version": "1.0", "minEngineVersion": "soon"}`)
		}, []string{"min-engine-version-legacy"}, nil},
		{"pack.yaml", func(t *testing.T, d string) {
			declare(t, d, "acme-pack", `{"version": "1.0", "minEngineVersion": "60930.1.0"}`)
			write(t, d, ".claudinite/shared/packs/acme-pack/pack.yaml", "version: 1\n")
		}, []string{"pack-declared"}, nil},
		{"malformed packs block", func(t *testing.T, d string) {
			raw, _ := os.ReadFile(filepath.Join(d, ".claudinite/settings.yaml"))
			write(t, d, ".claudinite/settings.yaml", string(raw)+"packs:\n  channel: \"nightly\"\n")
		}, []string{"pack-declared"}, nil},
	}
	for _, c := range cases {
		dir := newShape(t)
		c.mutate(t, dir)
		fs := run(t, dir)
		if got := strings.Join(ids(fs, findings.Break), " "); got != strings.Join(c.breaks, " ") {
			t.Errorf("%s: breaks [%s], want %v\n%v", c.name, got, c.breaks, fs)
		}
		if got := strings.Join(ids(fs, findings.Deprecation), " "); got != strings.Join(c.deps, " ") {
			t.Errorf("%s: deprecations [%s], want %v\n%v", c.name, got, c.deps, fs)
		}
		for _, f := range fs {
			if f.Path == "" || f.Sentence == "" {
				t.Errorf("%s: finding without a path or sentence: %+v", c.name, f)
			}
		}
	}
}

// declare adds id to the member's packs block and, unless manifest is
// empty, vendors a tree holding that pack.json.
func declare(t *testing.T, dir, id, manifest string) {
	t.Helper()
	raw, _ := os.ReadFile(filepath.Join(dir, ".claudinite/settings.yaml"))
	write(t, dir, ".claudinite/settings.yaml", string(raw)+"packs:\n  declared:\n    - "+id+"\n")
	if manifest != "" {
		write(t, dir, ".claudinite/shared/packs/"+id+"/pack.json", manifest)
	}
}

func TestTwoPartMinEngineShapeRaisesOnlyItsDeprecation(t *testing.T) {
	fs := Verify(Input{Repo: filepath.Join(shapes, "v3-two-part-min-engine"), Launcher: launcherBytes(t)})
	if findings.AnyBreak(fs) {
		t.Fatalf("%v", fs)
	}
	var legacy int
	for _, f := range fs {
		if f.ID == "min-engine-version-legacy" && f.Class == findings.Deprecation && f.Path == ".claudinite/shared/packs/acme-pack/pack.json" {
			legacy++
		}
	}
	if legacy != 1 {
		t.Errorf("want one min-engine-version-legacy deprecation: %v", fs)
	}
}

func TestOldIgnoreShapeNamesBothFiles(t *testing.T) {
	dir := newShape(t)
	_ = os.Remove(filepath.Join(dir, ".claudinite/.gitignore"))
	write(t, dir, ".gitignore", ".claudinite/bin/\n")
	fs := run(t, dir)
	if len(fs) != 1 || !strings.Contains(fs[0].Sentence, ".gitignore") || !strings.Contains(fs[0].Sentence, ".claudinite/.gitignore") {
		t.Errorf("%v", fs)
	}
}

func TestAShippedLauncherIsAccepted(t *testing.T) {
	dir := newShape(t)
	old := string(launcherBytes(t)) + "# an older launcher\n"
	write(t, dir, ".claudinite/launch", old)
	sum := sha256.Sum256([]byte(old))
	fs := Verify(Input{Repo: dir, Launcher: launcherBytes(t), Shipped: []string{hex.EncodeToString(sum[:])}})
	if len(fs) != 0 {
		t.Errorf("%v", fs)
	}
}

func TestPackManifestLegacyMinEngineVersion(t *testing.T) {
	if fs := PackManifest(".claudinite/shared/packs/basics/pack.json", "60928.1"); len(fs) != 1 || fs[0].Class != findings.Deprecation || fs[0].ID != "min-engine-version-legacy" {
		t.Errorf("two-part: %v", fs)
	}
	if fs := PackManifest("p/pack.json", "60928.1.0"); len(fs) != 0 {
		t.Errorf("three-part: %v", fs)
	}
	if fs := PackManifest("p/pack.json", "60928"); len(fs) != 1 || fs[0].Class != findings.Break {
		t.Errorf("malformed: %v", fs)
	}
}

// Every shape an earlier release accepted still raises only deprecations.
func TestShapeCorpus(t *testing.T) {
	dirs, _ := filepath.Glob(filepath.Join(shapes, "*-*"))
	if len(dirs) < 5 {
		t.Fatalf("shape corpus holds %d fixtures", len(dirs))
	}
	for _, d := range dirs {
		fs := Verify(Input{Repo: d, Launcher: launcherBytes(t)})
		if findings.AnyBreak(fs) {
			t.Errorf("%s: a shape an earlier release accepted now breaks: %v", filepath.Base(d), fs)
		}
	}
}

// A change to the rule set must grow the corpus: rules.txt's newest line
// names the registered rules, and every line's prefix has fixtures.
func TestCorpusGrowsWithTheRuleSet(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(shapes, "rules.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var last []string
	seen := map[string]bool{}
	for _, l := range strings.Split(string(raw), "\n") {
		f := strings.Fields(l)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		if seen[f[0]] {
			t.Errorf("rules.txt repeats prefix %s", f[0])
		}
		seen[f[0]] = true
		if m, _ := filepath.Glob(filepath.Join(shapes, f[0]+"-*")); len(m) == 0 {
			t.Errorf("rules.txt names prefix %s, which has no fixture", f[0])
		}
		last = f[1:]
	}
	got := RuleIDs()
	sort.Strings(last)
	if strings.Join(got, " ") != strings.Join(last, " ") {
		t.Errorf("verify registers [%s] but rules.txt's newest line says [%s]: add a line with a new prefix and its fixture", strings.Join(got, " "), strings.Join(last, " "))
	}
}
