package packindex

import (
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestDecodeRealIndexes(t *testing.T) {
	ix, err := Decode(fixture(t, "basics.index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if ix.Pack != "basics" || ix.Serial != 2 || len(ix.Versions) != 1 {
		t.Fatalf("%+v", ix)
	}
	e := ix.Versions[0]
	if e.Version != "60928.1" || e.Size != 96779 || e.MinEngineVersion != "60928.1" || e.Channel != "stable" || e.Revoked ||
		strings.Join(e.Requires, ",") != "claudinite-lifecycle,git-github" || len(e.SHA256) != 64 {
		t.Errorf("%+v", e)
	}
	if _, err := Decode(fixture(t, "node.index.json")); err != nil {
		t.Error(err)
	}
}

func TestDecodeKeepsUnknownFields(t *testing.T) {
	raw := strings.Replace(string(fixture(t, "basics.index.json")), `"serial": 2,`, `"serial": 2, "mirror": {"x": 1},`, 1)
	raw = strings.Replace(raw, `"revoked": false,`, `"revoked": false, "testedEngines": ["1.1.0"],`, 1)
	if _, err := Decode([]byte(raw)); err != nil {
		t.Errorf("an unknown field was refused: %v", err)
	}
}

func TestDecodeRefuses(t *testing.T) {
	base := string(fixture(t, "basics.index.json"))
	cases := map[string]string{
		"v 2":                strings.Replace(base, `"v": 1`, `"v": 2`, 1),
		"v missing":          strings.Replace(base, `"v": 1,`, ``, 1),
		"string serial":      strings.Replace(base, `"serial": 2`, `"serial": "2"`, 1),
		"fractional serial":  strings.Replace(base, `"serial": 2`, `"serial": 2.5`, 1),
		"zero serial":        strings.Replace(base, `"serial": 2`, `"serial": 0`, 1),
		"missing pack":       strings.Replace(base, `"pack": "basics",`, ``, 1),
		"bad channel":        strings.Replace(base, `"channel": "stable"`, `"channel": "beta"`, 1),
		"unreadable version": strings.Replace(base, `"version": "60928.1"`, `"version": "60928.x"`, 1),
		"bad sha256":         strings.Replace(base, `"sha256": "e7`, `"sha256": "z7`, 1),
		"not json":           "{",
	}
	for name, raw := range cases {
		if ix, err := Decode([]byte(raw)); err == nil {
			t.Errorf("%s: accepted %+v", name, ix)
		}
	}
}

func entry(v, channel string, revoked bool, minEngine string) Entry {
	return Entry{Version: v, SHA256: strings.Repeat("a", 64), Size: 1, MinEngineVersion: minEngine, Channel: channel, Revoked: revoked}
}

func TestSelect(t *testing.T) {
	ix := Index{V: 1, Pack: "hello", Serial: 5, Versions: []Entry{
		entry("1.0", "stable", false, "1.60102.1"),
		entry("1.1", "stable", false, "1.60102.1"),
		entry("1.2", "canary", true, "1.60102.1"),
		entry("1.3", "canary", false, "1.60102.1"),
		entry("1.4", "canary", false, "99.991231.99"),
	}}
	cases := []struct {
		name    string
		want    Want
		pick    string
		skipped string
	}{
		{"canary from nothing", Want{Channel: "canary", Engine: "1.60102.1"}, "1.3", "1.4 not for this engine"},
		{"stable from nothing", Want{Channel: "stable", Engine: "1.60102.1"}, "1.1", "1.4 canary"},
		{"canary past a revoked", Want{Channel: "canary", Engine: "1.60102.1", Held: "1.1"}, "1.3", "1.4 not for this engine"},
		{"canary up to date", Want{Channel: "canary", Engine: "1.60102.1", Held: "1.3"}, "", "1.4 not for this engine"},
		{"stable up to date", Want{Channel: "stable", Engine: "1.60102.1", Held: "1.1"}, "", "1.4 canary"},
		{"old engine", Want{Channel: "canary", Engine: "1.60101.1"}, "", "1.4 not for this engine"},
		{"held newest", Want{Channel: "canary", Engine: "99.991231.99", Held: "1.4"}, "", "1.4 not newer"},
	}
	for _, c := range cases {
		got := Select(ix, c.want)
		pick := ""
		if got.Entry != nil {
			pick = got.Entry.Version
		}
		skipped := ""
		if got.Skipped != nil {
			skipped = got.Skipped.Version + " " + got.Skipped.Reason
		}
		if pick != c.pick || skipped != c.skipped {
			t.Errorf("%s: picked %q skipped %q, want %q %q", c.name, pick, skipped, c.pick, c.skipped)
		}
	}
}

// A two-part minEngineVersion names a Node engine: the entry is passed
// over with that reason and never chosen, never an error.
func TestSelectSkipsANodeEngineFloor(t *testing.T) {
	ix := Index{V: 1, Pack: "hello", Serial: 2, Versions: []Entry{
		entry("1.0", "canary", false, "1.60102.1"),
		entry("1.1", "canary", false, "60928.1"),
	}}
	got := Select(ix, Want{Channel: "canary", Engine: "99.991231.99"})
	if got.Entry == nil || got.Entry.Version != "1.0" || got.Skipped == nil || got.Skipped.Version != "1.1" || got.Skipped.Reason != "names a Node engine version" {
		t.Errorf("%+v %+v", got.Entry, got.Skipped)
	}
	if got := Select(ix, Want{Channel: "canary", Engine: "99.991231.99", Held: "1.0"}); got.Entry != nil || got.Skipped == nil || got.Skipped.Reason != "names a Node engine version" {
		t.Errorf("held: %+v %+v", got.Entry, got.Skipped)
	}
}

func TestSelectNamesTheRevokedSkipBelowThePick(t *testing.T) {
	ix := Index{V: 1, Pack: "hello", Serial: 3, Versions: []Entry{
		entry("1.1", "canary", false, "1.60102.1"),
		entry("1.2", "canary", true, "1.60102.1"),
		entry("1.3", "canary", false, "1.60102.1"),
	}}
	got := Select(ix, Want{Channel: "canary", Engine: "1.60102.1", Held: "1.1"})
	if got.Entry == nil || got.Entry.Version != "1.3" || got.Skipped == nil || got.Skipped.Version != "1.2" || got.Skipped.Reason != "revoked" {
		t.Errorf("%+v %+v", got.Entry, got.Skipped)
	}
}

func TestSelectRevokedHeldVersion(t *testing.T) {
	ix := Index{V: 1, Pack: "hello", Serial: 4, Versions: []Entry{
		entry("1.0", "canary", false, "1.60102.1"),
		entry("1.1", "canary", true, "1.60102.1"),
	}}
	got := Select(ix, Want{Channel: "canary", Engine: "1.60102.1", Held: "1.1"})
	if got.Entry != nil || got.Skipped == nil || got.Skipped.Version != "1.1" || got.Skipped.Reason != "revoked, no replacement published" {
		t.Errorf("no replacement: %+v %+v", got.Entry, got.Skipped)
	}
	ix.Versions = append(ix.Versions, entry("1.2", "canary", false, "1.60102.1"))
	ix.Versions[1].Revoked = true
	if got := Select(ix, Want{Channel: "canary", Engine: "1.60102.1", Held: "1.1"}); got.Entry == nil || got.Entry.Version != "1.2" {
		t.Errorf("replacement: %+v", got.Entry)
	}
}

func TestEntryLookup(t *testing.T) {
	ix, _ := Decode(fixture(t, "basics.index.json"))
	if e, ok := ix.Entry("60928.1"); !ok || e.Size != 96779 {
		t.Errorf("%+v %v", e, ok)
	}
	if _, ok := ix.Entry("60928.2"); ok {
		t.Error("found a version not in the index")
	}
}
