package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const devPin = "engine:\n  version: \"0.0.0\"\n  # the development pin\n  manifest: \"sha512-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA==\"\n"

// pinnedMember is a repo holding settings at pin, or none when pin is "".
func pinnedMember(t *testing.T, pin string, mode os.FileMode) (dir, settingsPath string) {
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
	return dir, settingsPath
}

// cn settings answer turns the entry into an object, and refuses what it
// cannot record with exit 1.
func TestSettingsAnswer(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, ".claudinite", "settings.yaml")
	pack := filepath.Join(dir, ".claudinite", "shared", "packs", "asks")
	_ = os.MkdirAll(pack, 0o755)
	_ = os.WriteFile(filepath.Join(pack, "pack.json"), []byte(`{"version": "1.0", "questions": [{"id": "goals", "prompt": "Why?"}]}`), 0o644)
	_ = os.WriteFile(path, []byte(devPin+"packs:\n  declared:\n    - asks\n"), 0o644)
	out, errOut, code := runInProc([]string{"settings", "answer", "asks/goals", "n/a — none wanted", "--repo", dir}, "")
	if code != 0 || out != "answered asks/goals in .claudinite/settings.yaml\n" {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "answers:") || !strings.Contains(string(raw), "n/a — none wanted") || !strings.HasPrefix(string(raw), devPin) {
		t.Errorf("settings:\n%s", raw)
	}
	// A text starting with a dash follows a --, and the flags may follow it.
	out, errOut, code = runInProc([]string{"settings", "answer", "asks/goals", "--", "- none", "--repo", dir}, "")
	if code != 0 || out != "answered asks/goals in .claudinite/settings.yaml\n" {
		t.Fatalf("after --: code %d out %q err %q", code, out, errOut)
	}
	if raw, _ := os.ReadFile(path); !strings.Contains(string(raw), "- none") || strings.Contains(string(raw), "n/a — none wanted") {
		t.Errorf("after --, settings:\n%s", raw)
	}
	for _, args := range [][]string{{"asks/nope", "x"}, {"other/goals", "x"}, {"asks/goals", ""}, {"asks/goals"}, {"asks/goals", "x", "extra"}, {"asks/goals", "--"}} {
		_, errOut, code := runInProc(append(append([]string{"settings", "answer"}, args...), "--repo", dir), "")
		if code == 0 {
			t.Errorf("%v: recorded (%s)", args, errOut)
		}
	}
}

// settings config prints a declared entry's config as JSON, null for an
// entry that carries none, and refuses a pack the settings do not declare.
func TestSettingsConfigPrintsTheEntrysConfig(t *testing.T) {
	dir, _ := pinnedMember(t, devPin+"packs:\n  declared:\n    - id: claude-code-web-users-support\n      config:\n        repo: \"acme/store\"\n        nested: {a: [1, two]}\n    - basics\n    - local/mine\n", 0o644)
	for _, c := range []struct {
		pack, out string
		code      int
	}{
		{"claude-code-web-users-support", `{"nested":{"a":[1,"two"]},"repo":"acme/store"}` + "\n", 0},
		{"basics", "null\n", 0},
		{"local/mine", "null\n", 0},
		{"mine", "", 1},
	} {
		out, errOut, code := runInProc([]string{"settings", "config", c.pack, "--repo", dir}, "")
		if out != c.out || code != c.code {
			t.Errorf("settings config %s = %q (exit %d, %s), want %q exit %d", c.pack, out, code, errOut, c.out, c.code)
		}
	}
}

func TestSessionUserPackRefusesWhereThePackIsNotDeclared(t *testing.T) {
	dir, _ := pinnedMember(t, devPin+"packs:\n  declared:\n    - basics\n", 0o644)
	_, errOut, code := runInProc([]string{"session", "user-pack", "--repo", dir}, "")
	if code != 1 || !strings.Contains(errOut, "does not declare claude-code-web-users-support") {
		t.Errorf("exit %d: %s", code, errOut)
	}
}
