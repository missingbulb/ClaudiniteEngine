package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/packindex"
	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
)

// Every index the fixture writes rewrites the catalog: signed for use
// packs, its serial the indexes' sum, each pack's newest version that is
// not revoked, its fingerprint in the data form the engine compiles.
func TestTheFixtureCatalogVerifiesAndOffersTheNewestVersions(t *testing.T) {
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
	if err := run([]string{"--tree", tree, "--src", "../testdata/hello-asks", "--key", key, "--cert", cert, "--min-engine", "1.1.0", "--pack", "hello-asks", "--publish", SourceLabel}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(tree, "catalog.json"))
	sigRaw, _ := os.ReadFile(filepath.Join(tree, "catalog.sig.json"))
	var s sign.SignedPackCatalog
	if err := json.Unmarshal(sigRaw, &s); err != nil {
		t.Fatal(err)
	}
	if _, err := sign.VerifyPackCatalog(s, raw, roots, time.Now()); err != nil {
		t.Fatalf("the catalog does not verify: %v", err)
	}
	c, err := packindex.DecodeCatalog(raw)
	if err != nil {
		t.Fatal(err)
	}
	if c.Serial != 5 {
		t.Errorf("serial %d, want 4 for hello and 1 for hello-asks", c.Serial)
	}
	got := c.For(packindex.Canary)
	if len(got) != 2 || got[0].ID != "hello" || got[0].Version != "1.4" || got[1].ID != "hello-asks" {
		t.Fatalf("canary catalog %+v", got)
	}
	if !got[0].RelevanceDetector.Paths.Test("hello.json") || got[0].RelevanceDetector.Paths.Test("hello.yaml") {
		t.Errorf("hello's fingerprint %+v", got[0].RelevanceDetector)
	}
	asks := got[1]
	if len(asks.Questions) != 1 || asks.Questions[0].ID != "goals" || len(asks.RelevanceDetector.Text) != 1 || !asks.RelevanceDetector.Text[0].Test("HELLO-ASKS") {
		t.Errorf("hello-asks %+v %+v", asks, asks.RelevanceDetector)
	}
	if len(c.For(packindex.Stable)) != 0 {
		t.Error("a stable member sees canary versions")
	}
}
