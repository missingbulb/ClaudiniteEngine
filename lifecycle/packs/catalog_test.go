package packs

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
)

func catalogJSON(serial int) []byte {
	return []byte(fmt.Sprintf(`{"v": 1, "serial": %d, "packs": [{"id": "hello", "version": "1.1", "channel": "stable", "minEngineVersion": "1.1.0", "relevanceDetector": null}]}`+"\n", serial))
}

func signedCatalog(t *testing.T, cat []byte) []byte {
	t.Helper()
	cert, err := sign.Issue(testRoot, packsKey.Public().(ed25519.PublicKey), sign.UsePacks, t0.AddDate(0, 0, -1), t0.AddDate(0, 0, 29))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(sign.SignPackCatalog(packsKey, cert, cat))
	return raw
}

// catalogSource is a fakeSource that also serves a catalog.
type catalogSource struct {
	fakeSource
	cat, sig []byte
	catErr   error
}

func (c *catalogSource) Catalog() ([]byte, []byte, error) { return c.cat, c.sig, c.catErr }

func TestVerifiedCatalogReadsBothSourcesAndTakesTheNewest(t *testing.T) {
	var log bytes.Buffer
	cdn := &catalogSource{fakeSource: fakeSource{name: "cdn"}, cat: catalogJSON(4), sig: signedCatalog(t, catalogJSON(4))}
	branch := &catalogSource{fakeSource: fakeSource{name: "branch"}, cat: catalogJSON(5), sig: signedCatalog(t, catalogJSON(5))}
	v, err := newReader(&log, cdn, branch).VerifiedCatalog()
	if err != nil || v.Catalog.Serial != 5 || v.From != "branch" || len(v.Catalog.Packs) != 1 {
		t.Fatalf("%+v %v", v, err)
	}
	if !strings.Contains(log.String(), "catalog serial 5 from branch (read: cdn serial 4, branch serial 5)") {
		t.Error(log.String())
	}
}

// The CDN ahead of the branch is the window between an upload and the
// branch catching up: nothing reads either until they agree.
func TestVerifiedCatalogRefusesABranchBehindTheCDN(t *testing.T) {
	cdn := &catalogSource{fakeSource: fakeSource{name: "cdn"}, cat: catalogJSON(6), sig: signedCatalog(t, catalogJSON(6))}
	branch := &catalogSource{fakeSource: fakeSource{name: "branch"}, cat: catalogJSON(5), sig: signedCatalog(t, catalogJSON(5))}
	_, err := newReader(&bytes.Buffer{}, cdn, branch).VerifiedCatalog()
	var d *SourcesDisagree
	if !errors.As(err, &d) {
		t.Fatalf("%v", err)
	}
}

func TestVerifiedCatalogRefuses(t *testing.T) {
	good := catalogJSON(3)
	indexSig := signed(t, good) // a pack-index signature over the catalog's bytes
	for name, src := range map[string]*catalogSource{
		"index domain": {fakeSource: fakeSource{name: "cdn"}, cat: good, sig: indexSig},
		"tampered":     {fakeSource: fakeSource{name: "cdn"}, cat: catalogJSON(9), sig: signedCatalog(t, good)},
	} {
		if _, err := newReader(&bytes.Buffer{}, src).VerifiedCatalog(); err == nil || !strings.Contains(err.Error(), "refused") {
			t.Errorf("%s: %v", name, err)
		}
	}
	none := &catalogSource{fakeSource: fakeSource{name: "cdn"}, catErr: errors.New("404")}
	if _, err := newReader(&bytes.Buffer{}, none, &fakeSource{name: "plain"}).VerifiedCatalog(); err == nil || !strings.Contains(err.Error(), "no source answered (cdn: 404)") {
		t.Errorf("unreachable: %v", err)
	}
}
