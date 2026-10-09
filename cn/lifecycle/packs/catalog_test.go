package packs

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/sign"
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

// The catalog follows the index's rule: the CDN while it answers, the
// branch only behind an unreachable CDN.
func TestVerifiedCatalogTakesTheCDNWhileItAnswers(t *testing.T) {
	for name, c := range map[string]struct{ cdn, branch int }{"branch ahead": {4, 5}, "branch behind": {6, 5}} {
		var log bytes.Buffer
		cdn := &catalogSource{fakeSource: fakeSource{name: "cdn"}, cat: catalogJSON(c.cdn), sig: signedCatalog(t, catalogJSON(c.cdn))}
		branch := &catalogSource{fakeSource: fakeSource{name: "branch"}, catErr: errors.New("must not be read")}
		v, err := newReader(&log, cdn, branch).VerifiedCatalog()
		if err != nil || int(v.Catalog.Serial) != c.cdn || v.From != "cdn" || len(v.Catalog.Packs) != 1 {
			t.Fatalf("%s: %+v %v", name, v, err)
		}
		if !strings.Contains(log.String(), fmt.Sprintf("catalog serial %d from cdn\n", c.cdn)) {
			t.Errorf("%s: %s", name, log.String())
		}
	}
	cdn := &catalogSource{fakeSource: fakeSource{name: "cdn"}, catErr: errors.New("dial tcp: refused")}
	branch := &catalogSource{fakeSource: fakeSource{name: "branch"}, cat: catalogJSON(5), sig: signedCatalog(t, catalogJSON(5))}
	if v, err := newReader(&bytes.Buffer{}, cdn, branch).VerifiedCatalog(); err != nil || v.From != "branch" {
		t.Errorf("behind an unreachable CDN: %+v %v", v, err)
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
