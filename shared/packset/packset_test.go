package packset

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadManifest(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadManifest(dir); !errors.Is(err, ErrNoManifest) {
		t.Errorf("empty tree: %v", err)
	}
	write(t, filepath.Join(dir, "pack.json"), `{"version": "1.0", "minEngineVersion": "1.1.0", "requires": ["a"], "pitch": "x"}`)
	m, err := ReadManifest(dir)
	if err != nil || m.Version != "1.0" || m.MinEngineVersion != "1.1.0" || len(m.Requires) != 1 {
		t.Fatalf("%+v %v", m, err)
	}
	write(t, filepath.Join(dir, "pack.yaml"), "version: \"1.0\"\n")
	if _, err := ReadManifest(dir); err == nil || !strings.Contains(err.Error(), "pack.json and pack.yaml") {
		t.Errorf("two spellings: %v", err)
	}
}

func TestReadManifestThreeFormats(t *testing.T) {
	for name, body := range map[string]string{
		"pack.json": `{"version": "1.1", "minEngineVersion": "1.1.0", "requires": ["a"], "prose": null, "skills": ["s"], "seededByDefault": true, "hidden": false, "githubActions": ["openPr"]}`,
		"pack.yaml": "version: \"1.1\"\nminEngineVersion: \"1.1.0\"\nrequires: [a]\nprose: null\nskills:\n  - s\nseededByDefault: true\nhidden: false\ngithubActions: [openPr]\n",
		"pack.toml": "version = \"1.1\"\nminEngineVersion = \"1.1.0\"\nrequires = [\"a\"]\nprose = \"\"\nskills = [\"s\"]\nseededByDefault = true\nhidden = false\ngithubActions = [\"openPr\"]\n",
	} {
		dir := t.TempDir()
		write(t, filepath.Join(dir, name), body)
		m, err := ReadManifest(dir)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if m.File != name || m.Version != "1.1" || m.MinEngineVersion != "1.1.0" || strings.Join(m.Requires, ",") != "a" || !m.ProseSet || m.Prose != "" || !m.SkillsSet || strings.Join(m.Skills, ",") != "s" || strings.Join(m.GitHubActions, ",") != "openPr" {
			t.Errorf("%s: %+v", name, m)
		}
	}
}

// Whether a pack's tasks are the engine's own is the manifest's to say,
// with no default: absent is not engine.
func TestManifestEngineProperty(t *testing.T) {
	for body, want := range map[string]bool{
		`{"version": "1.0", "engine": true}`:  true,
		`{"version": "1.0", "engine": false}`: false,
		`{"version": "1.0"}`:                  false,
	} {
		m, err := ParseManifest([]byte(body))
		if err != nil || m.Engine != want {
			t.Errorf("%s: engine %v, %v", body, m.Engine, err)
		}
	}
	if _, err := ParseManifest([]byte(`{"version": "1.0", "engine": "yes"}`)); err == nil || !strings.Contains(err.Error(), `"engine"`) {
		t.Errorf("a non-boolean engine: %v", err)
	}
}

func TestReadManifestRefuses(t *testing.T) {
	cases := map[string]string{
		`"extra" is not a pack manifest key`:        `{"version": "1.0", "extra": 1}`,
		`"requires" must be a list of strings`:      `{"version": "1.0", "requires": "a"}`,
		`has no version`:                            `{"minEngineVersion": "1.1.0"}`,
		`"githubActions" must be a list of strings`: `{"version": "1.0", "githubActions": "openPr"}`,
	}
	for want, body := range cases {
		dir := t.TempDir()
		write(t, filepath.Join(dir, "pack.json"), body)
		if _, err := ReadManifest(dir); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", want, err)
		}
	}
	dir := t.TempDir()
	write(t, filepath.Join(dir, "pack.mjs"), "export default {}\n")
	if _, err := ReadManifest(dir); err == nil || !strings.Contains(err.Error(), "pack.mjs is a module manifest, which this engine does not read") {
		t.Errorf("pack.mjs: %v", err)
	}
}

func TestDeclaredAndVendored(t *testing.T) {
	repo := t.TempDir()
	write(t, filepath.Join(repo, ".claudinite/settings.yaml"), "engine:\n  version: \"1.1.0\"\npacks:\n  declared:\n    - hello\n")
	write(t, filepath.Join(Tree(repo, "hello"), "pack.json"), `{"version": "1.0"}`)
	write(t, filepath.Join(Tree(repo, "stale"), "pack.json"), `{"version": "1.0"}`)
	p, err := Declared(repo)
	if err != nil || strings.Join(p.Declared, ",") != "hello" {
		t.Fatalf("%+v %v", p, err)
	}
	v, err := Vendored(repo)
	if err != nil || strings.Join(v, ",") != "hello,stale" {
		t.Fatalf("%v %v", v, err)
	}
	if v, err := Vendored(t.TempDir()); err != nil || len(v) != 0 {
		t.Errorf("no tree: %v %v", v, err)
	}
}
