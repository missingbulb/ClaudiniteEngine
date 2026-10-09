package packset

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
)

func registerAcme(t *testing.T, files fstest.MapFS) {
	t.Helper()
	was := embedded
	t.Cleanup(func() { embedded = was })
	RegisterEmbedded(Embedded{ID: "acme-pack", Files: files, Active: func(p settings.Parsed) bool { return p.Fleet != nil }})
}

// A pack the binary carries is written under the temp root and loaded
// after the declared packs, in a session or not, only where the settings
// turn it on; a file it no longer carries goes, and the temp root is
// never tracked.
func TestAnEmbeddedPackIsWrittenAndLoadedWhereTheSettingsTurnItOn(t *testing.T) {
	registerAcme(t, fstest.MapFS{
		"pack.json":                  {Data: []byte(`{"version": "1.0"}`)},
		"RULES.md":                   {Data: []byte("- a\n")},
		"skills/acme-skill/SKILL.md": {Data: []byte("---\nname: acme-skill\n---\n")},
	})
	off := member(t, "    - zeta\n")
	write(t, filepath.Join(Tree(off, "zeta"), "pack.json"), `{"version": "1.0"}`)
	if s, err := Load(off, "0.0.0", true); err != nil || tokens(s) != "zeta" {
		t.Fatalf("no fleet block: %s %v", tokens(s), err)
	}
	if _, err := os.Stat(filepath.Join(off, TempDir, "acme-pack")); err == nil {
		t.Error("a pack the settings do not turn on was written")
	}

	on := member(t, "    - zeta\n")
	write(t, filepath.Join(Tree(on, "zeta"), "pack.json"), `{"version": "1.0"}`)
	write(t, filepath.Join(on, ".claudinite/settings.yaml"), "engine:\n  version: \"1.1.0\"\npacks:\n  declared:\n    - zeta\nfleet:\n  owner: acme\n")
	write(t, filepath.Join(on, TempDir, "acme-pack", "stale.md"), "old\n")
	for _, session := range []bool{false, true} {
		s, err := Load(on, "0.0.0", session)
		if err != nil || tokens(s) != "zeta,engine/acme-pack" || notLoaded(s) != "" {
			t.Fatalf("session %v: %s %v\n%s", session, tokens(s), err, notLoaded(s))
		}
		p := s.Packs[1]
		if p.Kind != Engine || p.Rel != TempDir+"/acme-pack" || p.ProsePath() == "" || len(p.Skills) != 1 {
			t.Errorf("session %v: %+v", session, p)
		}
	}
	if _, err := os.Stat(filepath.Join(on, TempDir, "acme-pack", "stale.md")); err == nil {
		t.Error("a file the binary no longer carries was left")
	}
	if raw, err := os.ReadFile(filepath.Join(on, ".claudinite/temp/.gitignore")); err != nil || string(raw) != tempIgnore {
		t.Errorf("the temp root's ignore file: %q %v", raw, err)
	}

	shadow := member(t, "    - acme-pack\n")
	write(t, filepath.Join(Tree(shadow, "acme-pack"), "pack.json"), `{"version": "1.0"}`)
	write(t, filepath.Join(shadow, ".claudinite/settings.yaml"), "engine:\n  version: \"1.1.0\"\npacks:\n  declared:\n    - acme-pack\nfleet: {}\n")
	if s, _ := Load(shadow, "0.0.0", false); tokens(s) != "acme-pack" || notLoaded(s) == "" {
		t.Errorf("a declared pack of the same id: %s\n%s", tokens(s), notLoaded(s))
	}
}
