package flatdecl

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
)

func TestMemberBytes(t *testing.T) {
	repo := t.TempDir()
	put(t, repo, map[string]string{
		".claudinite/settings.yaml": `engine:
  package: "@claudinite/cli-rc"
  version: "1.61003.2"
  manifest: "sha512-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="
packs:
  channel: canary
  declared:
    - basics
    - id: claudinite-tasks
      config:
        dormant: true
        z: [1, "<a&b>"]
    - local/mine
`,
	})
	packs := []packset.Pack{
		{ID: "basics", Kind: packset.Canon, Version: "4.0"},
		{ID: "claudinite-tasks", Kind: packset.Canon, Version: "60820.10"},
		{ID: "mine", Kind: packset.Local, Version: "9"},
	}
	want := `{
  "version": 1,
  "settings": {
    "path": ".claudinite/settings.yaml",
    "format": "yaml"
  },
  "engine": {
    "package": "@claudinite/cli-rc",
    "version": "1.61003.2",
    "channel": "canary"
  },
  "packs": {
    "channel": "canary",
    "declared": [
      {
        "id": "basics"
      },
      {
        "id": "claudinite-tasks",
        "config": {
          "dormant": true,
          "z": [
            1,
            "<a&b>"
          ]
        }
      },
      {
        "id": "local/mine"
      }
    ]
  },
  "dormant": true,
  "held": {
    "basics": "4.0",
    "claudinite-tasks": "60820.10"
  }
}
`
	written, err := WriteMember(repo, packs)
	if err != nil || written != MemberFile {
		t.Fatalf("first write %v %v", written, err)
	}
	got, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(MemberFile)))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("member file:\n%s\nwant:\n%s", got, want)
	}
	if written, err := WriteMember(repo, packs); err != nil || written != "" {
		t.Errorf("a second write wrote %v %v", written, err)
	}
	if written, err := WriteMember(t.TempDir(), packs); err != nil || written != "" {
		t.Errorf("a repo with no settings file wrote %v %v", written, err)
	}

	// A member still holding the file under LegacyDir has it rewritten
	// there, so an engine update PR stays the pin and the member file.
	if err := os.MkdirAll(filepath.Join(repo, LegacyDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(repo, filepath.FromSlash(MemberFile)), filepath.Join(repo, filepath.FromSlash(LegacyPath(MemberFile)))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, filepath.FromSlash(LegacyPath(MemberFile))), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if written, err := WriteMember(repo, packs); err != nil || written != LegacyDir+"/member.GENERATED.json" {
		t.Errorf("a legacy member file was written at %q %v", written, err)
	}
	if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(MemberFile))); err == nil {
		t.Errorf("a legacy member gained %s", MemberFile)
	}
}
