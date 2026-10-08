package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const devPin = "engine:\n  version: \"0.0.0\"\n  # the development pin\n  manifest: \"sha512-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA==\"\n"

func runInProc(args []string) (stdout, stderr string, code int) {
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return out.String(), errOut.String(), code
}

const nodeDecl = `{"packs": ["basics", {"id": "claudinite-tasks", "config": {"dormant": true}}], "engineVersion": "61001.1", "rules": {"x": "blocking"}}`

func importMember(t *testing.T, pin string, mode os.FileMode) (dir, settingsPath string) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	dir = t.TempDir()
	settingsPath = filepath.Join(dir, ".claudinite", "settings.yaml")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if pin != "" {
		if err := os.WriteFile(settingsPath, []byte(pin), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(settingsPath, mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".claudinite-settings.json"), []byte(nodeDecl), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, settingsPath
}

// The import writes once: a second run refuses, exit 1, and leaves the
// file byte for byte as the first run wrote it, mode included.
func TestSettingsImportWritesOnce(t *testing.T) {
	dir, path := importMember(t, devPin, 0o600)
	out, errOut, code := runInProc([]string{"import", "--repo", dir})
	if code != 0 {
		t.Fatalf("first import: exit %d\n%s%s", code, out, errOut)
	}
	for _, want := range []string{`mapped packs[0] "basics"`, "dropped engineVersion", `mapped rules.x "blocking" → "block"`} {
		if !strings.Contains(out, want) {
			t.Errorf("the report lacks %q:\n%s", want, out)
		}
	}
	first, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(first), devPin) || !strings.Contains(string(first), "dormant: true") {
		t.Errorf("the pin and its comment survive, the blocks follow:\n%s", first)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", st.Mode().Perm())
	}

	_, errOut, code = runInProc([]string{"import", "--repo", dir})
	if code != 1 || !strings.Contains(errOut, "already holds a packs block") {
		t.Errorf("second import: exit %d, stderr %q", code, errOut)
	}
	if again, _ := os.ReadFile(path); string(again) != string(first) {
		t.Errorf("the second run changed the file:\n%s", again)
	}
}

// --stdout reads with no pin and writes nothing; --from names the file.
func TestSettingsImportStdoutAndFrom(t *testing.T) {
	dir, path := importMember(t, "", 0o644)
	from := filepath.Join(t.TempDir(), "elsewhere.json")
	if err := os.Rename(filepath.Join(dir, ".claudinite-settings.json"), from); err != nil {
		t.Fatal(err)
	}
	if _, _, code := runInProc([]string{"import", "--repo", dir, "--stdout"}); code != 1 {
		t.Errorf("with no Node file, exit %d", code)
	}
	out, errOut, code := runInProc([]string{"import", "--repo", dir, "--from", from, "--stdout"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.HasPrefix(out, "packs:\n  declared:\n") || !strings.Contains(out, "\n# dropped engineVersion") {
		t.Errorf("the blocks, then the report as comments:\n%s", out)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("--stdout wrote the settings: %v", err)
	}
	if _, errOut, code := runInProc([]string{"import", "--repo", dir, "--from", from}); code != 1 || !strings.Contains(errOut, "pin the engine first") {
		t.Errorf("without a pin, exit %d, stderr %q", code, errOut)
	}
}

// A refused key prints the report, exits 1 and writes nothing.
func TestSettingsImportRefusalWritesNothing(t *testing.T) {
	dir, path := importMember(t, devPin, 0o644)
	if err := os.WriteFile(filepath.Join(dir, ".claudinite-settings.json"), []byte(`{"packs": ["basics"], "maintenance": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, code := runInProc([]string{"import", "--repo", dir})
	if code != 1 || !strings.Contains(out, "refused maintenance") {
		t.Errorf("exit %d:\n%s", code, out)
	}
	if got, _ := os.ReadFile(path); string(got) != devPin {
		t.Errorf("a refusal changed the file:\n%s", got)
	}
}
