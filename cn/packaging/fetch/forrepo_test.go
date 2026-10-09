package fetch

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const devPin = "engine:\n  version: \"0.0.0\"\n  # the development pin\n  manifest: \"sha512-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA==\"\n"

func writeSettings(t *testing.T, body string) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".claudinite"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".claudinite", "settings.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

func readerNames(r *Reader) string {
	var names []string
	for _, s := range r.Sources {
		names = append(names, s.Name())
	}
	return strings.Join(names, ",")
}

func TestPackReaderReadsTheRepoSources(t *testing.T) {
	member := writeSettings(t, devPin+"packs:\n  sources:\n    - \"acme/packs\"\n  declared:\n    - hello\n")
	r, closer, err := ForRepo(member, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	if got := readerNames(r); got != "acme/packs" {
		t.Errorf("a repo listing one source reads %s, not that source alone", got)
	}
	single := writeSettings(t, devPin+"packs:\n  declared:\n    - hello\n")
	r, closer, err = ForRepo(single, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	if got := readerNames(r); got != "cdn,branch" {
		t.Errorf("a single repo reads %s, not the shelf", got)
	}
	r, closer, err = ForRepo(t.TempDir(), nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	if got := readerNames(r); got != "cdn,branch" {
		t.Errorf("a repo being adopted reads %s, not the shelf", got)
	}
	broken := writeSettings(t, devPin+"packs:\n  sources: []\n")
	if _, _, err := ForRepo(broken, nil, io.Discard); err == nil || !strings.Contains(err.Error(), "packs.sources") {
		t.Errorf("a settings file that does not read fell back to the shelf: %v", err)
	}
}
