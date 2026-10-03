package packindex

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const catalog = `{
  "v": 1, "serial": 7, "extra": true,
  "packs": [
    {"id": "node", "version": "61002.1", "channel": "stable", "minEngineVersion": "1.61001.1", "requires": [],
     "relevanceDetector": {"about": "a package.json", "paths": {"source": "(^|/)package\\.json$", "flags": ""}},
     "belongs": "Node projects", "future": 1},
    {"id": "node", "version": "61003.1", "channel": "canary", "minEngineVersion": "1.61001.1", "requires": [],
     "relevanceDetector": {"about": "a package.json", "paths": {"source": "(^|/)package\\.json$", "flags": ""}}},
    {"id": "jwt", "version": "61001.1", "channel": "stable", "minEngineVersion": "1.61001.1",
     "relevanceDetector": {"about": "a JWT library", "paths": {"source": "\\.(js|mjs)$", "flags": ""},
       "text": [{"source": "JSONWEBTOKEN", "flags": "i"}], "search": ["jsonwebtoken"]}},
    {"id": "asks", "version": "61001.1", "channel": "canary", "minEngineVersion": "1.61001.1", "relevanceDetector": null,
     "questions": [{"id": "goals", "prompt": "What should it prove?"}]}
  ]
}`

func TestDecodeCatalogReadsEveryEntryAndIgnoresUnknownFields(t *testing.T) {
	c, err := DecodeCatalog([]byte(catalog))
	if err != nil {
		t.Fatal(err)
	}
	if c.Serial != 7 || len(c.Packs) != 4 {
		t.Fatalf("%+v", c)
	}
	jwt := c.Packs[2].RelevanceDetector
	if !jwt.Text[0].Test("require('jsonwebtoken')") || jwt.Text[0].Test("nothing") {
		t.Error("the text pattern's flags were not compiled")
	}
	if got := jwt.Candidates([]string{"a.js", "b.py", "c/d.mjs"}); !reflect.DeepEqual(got, []string{"a.js", "c/d.mjs"}) {
		t.Errorf("candidates %v", got)
	}
	if c.Packs[3].RelevanceDetector != nil || c.Packs[3].Questions[0].ID != "goals" || c.Packs[2].Requires == nil {
		t.Errorf("%+v", c.Packs[3])
	}
}

// A stable member is fingerprinted against what its update would give
// it; a canary member against the newer of the two.
func TestTheCatalogForAChannelIsWhatThatChannelsUpdateReads(t *testing.T) {
	c, _ := DecodeCatalog([]byte(catalog))
	ids := func(ps []CatalogPack) (out []string) {
		for _, p := range ps {
			out = append(out, p.ID+" "+p.Version)
		}
		return out
	}
	if got := ids(c.For(Stable)); !reflect.DeepEqual(got, []string{"jwt 61001.1", "node 61002.1"}) {
		t.Errorf("stable %v", got)
	}
	if got := ids(c.For(Canary)); !reflect.DeepEqual(got, []string{"asks 61001.1", "jwt 61001.1", "node 61003.1"}) {
		t.Errorf("canary %v", got)
	}
}

func TestDecodeCatalogRefuses(t *testing.T) {
	for name, c := range map[string]struct{ raw, want string }{
		"v2":        {`{"v":2,"serial":1,"packs":[]}`, "v2"},
		"serial":    {`{"v":1,"serial":0,"packs":[]}`, "serial 0"},
		"channel":   {`{"v":1,"serial":1,"packs":[{"id":"a","version":"1.1","channel":"beta"}]}`, "neither canary nor stable"},
		"version":   {`{"v":1,"serial":1,"packs":[{"id":"a","version":"x","channel":"stable"}]}`, `version "x"`},
		"twice":     {`{"v":1,"serial":1,"packs":[{"id":"a","version":"1.1","channel":"stable"},{"id":"a","version":"1.2","channel":"stable"}]}`, "two stable"},
		"detector":  {`{"v":1,"serial":1,"packs":[{"id":"a","version":"1.1","channel":"stable","relevanceDetector":{"paths":"x"}}]}`, "about names"},
		"uncompile": {`{"v":1,"serial":1,"packs":[{"id":"a","version":"1.1","channel":"stable","relevanceDetector":{"about":"x","paths":{"source":"(","flags":""}}}]}`, "does not compile"},
	} {
		if _, err := DecodeCatalog([]byte(c.raw)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
}

// The grammar is the Node engine's validateRelevanceDetector, over the
// data form a manifest carries across JSON.
func TestValidateDetectorNamesEachProblemAsNodeDoes(t *testing.T) {
	for _, c := range []struct {
		raw  string
		want []string
	}{
		{`null`, nil},
		{`{"about":"x","paths":{"source":"a"}}`, nil},
		{`[]`, []string{"relevanceDetector is an object or null"}},
		{`{"about":" ","paths":"a","bogus":1}`, []string{
			`relevanceDetector declares "bogus", which is not one of about, paths, text, search`,
			"relevanceDetector.about names what is found, in words",
			"relevanceDetector.paths is a RegExp over tracked paths"}},
		{`{"about":"x","paths":{"source":"a","flags":"g"},"text":{"source":"b"}}`, []string{
			"a relevanceDetector pattern carries the g or y flag, which makes .test stateful",
			"relevanceDetector.search lists the code-search terms that find every file relevanceDetector.text matches"}},
		{`{"about":"x","paths":{"source":"a"},"text":[{"source":"b"},3],"search":["1","2","3","4","5","6","7"]}`, []string{
			"relevanceDetector.text is a RegExp or a list of them",
			"relevanceDetector.search names 7 terms; one code search joins at most six"}},
	} {
		got := ValidateDetector(json.RawMessage(c.raw))
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got %q\nwant %q", c.raw, got, c.want)
		}
	}
}
