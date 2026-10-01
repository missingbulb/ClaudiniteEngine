package main

import (
	"crypto/ed25519"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/lifecycle/packs"
	"github.com/missingbulb/ClaudiniteEngine/shared/packindex"
	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
)

const src = "../testdata/hello"

// packsKey writes a packs key and its certificate from the development
// root, as release/packs-fixture.sh does with cn-keys.
func packsKey(t *testing.T) (string, string, []ed25519.PublicKey) {
	t.Helper()
	raw, err := os.ReadFile("../../keys/dev/root.key")
	if err != nil {
		t.Fatal(err)
	}
	root, err := sign.ParsePrivateKey(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	_, key, _ := ed25519.GenerateKey(nil)
	cert, err := sign.Issue(root, key.Public().(ed25519.PublicKey), sign.UsePacks, time.Now().Add(-time.Hour), time.Now().AddDate(0, 0, 30))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cb, _ := json.Marshal(cert)
	_ = os.WriteFile(filepath.Join(dir, "packs.key"), []byte(sign.FormatPrivateKey(key)), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "packs.cert.json"), cb, 0o644)
	return filepath.Join(dir, "packs.key"), filepath.Join(dir, "packs.cert.json"), []ed25519.PublicKey{root.Public().(ed25519.PublicKey)}
}

func verified(t *testing.T, tree string, roots []ed25519.PublicKey) packindex.Index {
	t.Helper()
	raw, _ := os.ReadFile(filepath.Join(tree, "hello", "index.json"))
	sigRaw, _ := os.ReadFile(filepath.Join(tree, "hello", "index.sig.json"))
	var s sign.SignedPackIndex
	if err := json.Unmarshal(sigRaw, &s); err != nil {
		t.Fatal(err)
	}
	if _, err := sign.VerifyPackIndex(s, raw, roots, time.Now()); err != nil {
		t.Fatalf("the fixture's index does not verify: %v", err)
	}
	ix, err := packindex.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	return ix
}

func TestTheFixtureIndexVerifiesAndNamesItsArchives(t *testing.T) {
	key, cert, roots := packsKey(t)
	tree := t.TempDir()
	for _, l := range []string{"v1", "v2", "v3"} {
		if err := run([]string{"--tree", tree, "--src", src, "--key", key, "--cert", cert, "--min-engine", "1.1.0", "--publish", l}); err != nil {
			t.Fatal(err)
		}
	}
	if err := run([]string{"--tree", tree, "--key", key, "--cert", cert, "--revoke", "v3"}); err != nil {
		t.Fatal(err)
	}
	ix := verified(t, tree, roots)
	if ix.Serial != 4 || len(ix.Versions) != 3 || !ix.Versions[2].Revoked || ix.Versions[0].Channel != "canary" {
		t.Fatalf("%+v", ix)
	}
	for _, e := range ix.Versions {
		a, err := os.ReadFile(filepath.Join(tree, "hello", e.Version+".tar.gz"))
		if err != nil {
			t.Fatal(err)
		}
		if err := packs.VerifyArchive(a, e); err != nil {
			t.Errorf("%s: %v", e.Version, err)
		}
		files, err := packs.ReadArchive(a)
		if err != nil {
			t.Fatalf("%s: %v", e.Version, err)
		}
		if !strings.Contains(string(files["RULES.md"].Data), "# hello "+e.Version+"\n") {
			t.Errorf("%s: RULES.md %q", e.Version, files["RULES.md"].Data)
		}
		_, always := files["checks/always.go"]
		if always != (e.Version == "1.2") {
			t.Errorf("%s: always.go present %v", e.Version, always)
		}
	}
	again, _ := Archive(mustVariant(t, "v1"))
	first, _ := os.ReadFile(filepath.Join(tree, "hello", "1.0.tar.gz"))
	if string(again) != string(first) {
		t.Error("the archive is not deterministic")
	}
	if err := run([]string{"--tree", tree, "--key", key, "--cert", cert, "--serial", "1"}); err != nil {
		t.Fatal(err)
	}
	if ix := verified(t, tree, roots); ix.Serial != 1 {
		t.Errorf("serial %d", ix.Serial)
	}
	if err := run([]string{"--tree", tree, "--flip-sig"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(tree, "hello", "index.json"))
	sigRaw, _ := os.ReadFile(filepath.Join(tree, "hello", "index.sig.json"))
	var s sign.SignedPackIndex
	_ = json.Unmarshal(sigRaw, &s)
	if _, err := sign.VerifyPackIndex(s, raw, roots, time.Now()); err == nil {
		t.Error("a flipped signature still verifies")
	}
}

func mustVariant(t *testing.T, label string) map[string]File {
	t.Helper()
	files, err := ReadPack(src)
	if err != nil {
		t.Fatal(err)
	}
	v, err := Variant(files, label, "1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// The fixture's source is the hello pack ClaudinitePacks publishes: when
// CLAUDINITE_PACKS_REPO names a vendored branch carrying hello, its
// archive at the source's version holds exactly the source's files.
func TestTheSourceIsThePublishedHelloPack(t *testing.T) {
	repo := os.Getenv("CLAUDINITE_PACKS_REPO")
	if repo == "" {
		t.Skip("CLAUDINITE_PACKS_REPO is not set")
	}
	dir := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", "--depth", "1", "--branch", packs.VendoredRef, repo, dir).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	var pj struct{ Version string }
	raw, _ := os.ReadFile(filepath.Join(src, "pack.json"))
	_ = json.Unmarshal(raw, &pj)
	archive, err := os.ReadFile(filepath.Join(dir, "hello", pj.Version+".tar.gz"))
	if err != nil {
		t.Skipf("the vendored branch has no hello %s yet: %v", pj.Version, err)
	}
	have, err := ReadPack(src)
	if err != nil {
		t.Fatal(err)
	}
	conv := map[string]packs.File{}
	var names []string
	for n, f := range have {
		conv[n] = packs.File{Data: f.Data, Executable: f.Executable}
		names = append(names, n)
	}
	sort.Strings(names)
	diff, err := packs.FilesEqual(conv, archive)
	if err != nil || diff != "" {
		t.Errorf("release/testdata/hello (%v) is not the published hello %s: %v\n%s", names, pj.Version, err, diff)
	}
}
