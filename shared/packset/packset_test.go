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
	write(t, filepath.Join(dir, "pack.yaml"), "version: 1\n")
	if _, err := ReadManifest(dir); err == nil || !strings.Contains(err.Error(), "phase 6") {
		t.Errorf("pack.yaml: %v", err)
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
