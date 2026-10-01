package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	pin1 = "sha512-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="
	pin2 = "sha512-BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=="
)

var samples = map[Format]string{
	YAML: "# member's own comment\nother: 1\nengine:\n  package: \"@claudinite/cli-rc\"\n  # the channel above\n  version: \"1.1.0\"\n  manifest: \"" + pin1 + "\"\nafter: true\n",
	TOML: "# comment\n[engine]\nmanifest = \"" + pin1 + "\"\nversion = \"1.1.0\"\n# pinned\n\n[other]\nversion = \"9.9.9\"\n",
	JSON: "{\n  \"x\": {\"version\": \"9.9.9\"},\n  \"engine\": {\n    \"version\": \"1.1.0\",\n    \"manifest\": \"" + pin1 + "\"\n  }\n}\n",
}

func TestFind(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := Find(dir); err == nil {
		t.Error("found settings in an empty repo")
	}
	_ = os.MkdirAll(filepath.Join(dir, ".claudinite"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, ".claudinite", "settings.toml"), []byte(samples[TOML]), 0o644)
	p, f, err := Find(dir)
	if err != nil || f != TOML || p != filepath.Join(dir, ".claudinite", "settings.toml") {
		t.Fatalf("%s %s %v", p, f, err)
	}
	_ = os.WriteFile(filepath.Join(dir, ".claudinite", "settings.json"), []byte(samples[JSON]), 0o644)
	if _, _, err := Find(dir); err == nil || !strings.Contains(err.Error(), "found 2") {
		t.Errorf("two settings files: %v", err)
	}
}

func TestReadEngine(t *testing.T) {
	for f, s := range samples {
		e, err := ReadEngine([]byte(s), f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if e.Version != "1.1.0" || e.Manifest != pin1 {
			t.Errorf("%s: %+v", f, e)
		}
		wantPkg := "@claudinite/cli"
		if f == YAML {
			wantPkg = "@claudinite/cli-rc"
		}
		if e.Package != wantPkg {
			t.Errorf("%s: package %q", f, e.Package)
		}
	}
	for name, bad := range map[string]string{
		"no block":       "other: 1\n",
		"bad version":    "engine:\n  version: \"1.1\"\n  manifest: \"" + pin1 + "\"\n",
		"unquoted":       "engine:\n  version: 1.1.0\n  manifest: \"" + pin1 + "\"\n",
		"duplicate":      "engine:\n  version: \"1.1.0\"\n  version: \"1.2.0\"\n  manifest: \"" + pin1 + "\"\n",
		"short manifest": "engine:\n  version: \"1.1.0\"\n  manifest: \"sha512-x\"\n",
		"bad package":    "engine:\n  package: \"left-pad\"\n  version: \"1.1.0\"\n  manifest: \"" + pin1 + "\"\n",
	} {
		if _, err := ReadEngine([]byte(bad), YAML); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// The pin moves by a line edit: every other byte, comments and key order
// included, survives.
func TestSetPinIsALineEdit(t *testing.T) {
	for f, s := range samples {
		out, err := SetPin([]byte(s), f, "1.2.0", pin2)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		want := strings.Replace(strings.Replace(s, "\"1.1.0\"", "\"1.2.0\"", 1), pin1, pin2, 1)
		if string(out) != want {
			t.Errorf("%s:\ngot  %q\nwant %q", f, out, want)
		}
		e, err := ReadEngine(out, f)
		if err != nil || e.Version != "1.2.0" || e.Manifest != pin2 {
			t.Errorf("%s: re-read %+v %v", f, e, err)
		}
		if err := PinOnlyChange([]byte(s), out, f); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	if _, err := SetPin([]byte(samples[YAML]), YAML, "1.2", pin2); err == nil {
		t.Error("SetPin accepted a malformed version")
	}
}

func TestPinOnlyChange(t *testing.T) {
	old := []byte(samples[YAML])
	moved, _ := SetPin(old, YAML, "1.2.0", pin2)
	for name, bad := range map[string]string{
		"package changed": strings.Replace(string(moved), "cli-rc", "cli", 1),
		"line added":      string(moved) + "extra: 1\n",
		"comment changed": strings.Replace(string(moved), "own comment", "edited", 1),
	} {
		if err := PinOnlyChange(old, []byte(bad), YAML); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := PinOnlyChange(old, old, YAML); err != nil {
		t.Errorf("unchanged: %v", err)
	}
}
