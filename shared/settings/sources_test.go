package settings

import (
	"reflect"
	"strings"
	"testing"
)

var sourcesSamples = map[Format]string{
	YAML: "engine:\n" + enginePart + "packs:\n  sources:\n    - \"acme/fleet\"\n    - \"https://packs.example.com\"\n  declared:\n    - hello\n",
	TOML: "[engine]\nversion = \"1.1.0\"\nmanifest = \"" + pin1 + "\"\n\n[packs]\nsources = [\"acme/fleet\", \"https://packs.example.com\"]\ndeclared = [\"hello\"]\n",
	JSON: "{\n  \"engine\": {\"version\": \"1.1.0\", \"manifest\": \"" + pin1 + "\"},\n  \"packs\": {\"sources\": [\"acme/fleet\", \"https://packs.example.com\"], \"declared\": [\"hello\"]}\n}\n",
}

func TestReadPacksSources(t *testing.T) {
	for f, s := range sourcesSamples {
		p, err := ReadPacks([]byte(s), f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if !reflect.DeepEqual(p.Sources, []string{"acme/fleet", "https://packs.example.com"}) {
			t.Errorf("%s: sources %v", f, p.Sources)
		}
	}
	for f, s := range packsSamples {
		p, err := ReadPacks([]byte(s), f)
		if err != nil || p.Sources != nil {
			t.Errorf("%s: a block naming no sources read %v (%v)", f, p.Sources, err)
		}
	}
}

func TestReadPacksRefusesBadSources(t *testing.T) {
	cases := map[string]string{
		"empty":     `[]`,
		"not text":  `[3]`,
		"not list":  `"acme/fleet"`,
		"plain url": `["http://packs.example.com"]`,
		"bare name": `["fleet"]`,
		"twice":     `["acme/fleet", "acme/fleet"]`,
	}
	for name, list := range cases {
		raw := "{\"engine\": {\"version\": \"1.1.0\", \"manifest\": \"" + pin1 + "\"}, \"packs\": {\"sources\": " + list + "}}"
		if _, err := ReadPacks([]byte(raw), JSON); err == nil || !strings.Contains(err.Error(), "packs.sources") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestSetSources(t *testing.T) {
	for f, c := range entrySamples {
		out, err := SetSources([]byte(c.raw), f, []string{"acme/fleet"})
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		p, err := ReadPacks(out, f)
		if err != nil {
			t.Fatalf("%s: %v\n%s", f, err, out)
		}
		if !reflect.DeepEqual(p.Sources, []string{"acme/fleet"}) || strings.Join(p.Declared, ",") != "hello,acme-pack" || p.Channel != "canary" {
			t.Errorf("%s: %+v\n%s", f, p, out)
		}
		before, after := outside(t, []byte(c.raw), f)
		gotBefore, gotAfter := outside(t, out, f)
		if before != gotBefore || after != gotAfter {
			t.Errorf("%s: a byte outside the packs block moved\n%s", f, out)
		}
	}
}

func TestSetSourcesRefusesWhatItWouldNotRead(t *testing.T) {
	if _, err := SetSources([]byte(sourcesSamples[YAML]), YAML, []string{"fleet"}); err == nil {
		t.Fatal("wrote a source no reader takes")
	}
	if _, err := SetSources([]byte(sourcesSamples[YAML]), YAML, nil); err == nil {
		t.Fatal("wrote an empty list")
	}
}

func TestAddDeclaredBesideSources(t *testing.T) {
	for f, s := range sourcesSamples {
		out, err := AddDeclared([]byte(s), f, "acme-pack")
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		p, err := ReadPacks(out, f)
		if err != nil || strings.Join(p.Declared, ",") != "hello,acme-pack" || len(p.Sources) != 2 {
			t.Errorf("%s: %+v %v\n%s", f, p, err, out)
		}
	}
}

func TestSetSourcesAddsAPacksBlockWhereThereIsNone(t *testing.T) {
	for f, s := range pinSamples {
		out, err := SetSources([]byte(s), f, []string{"acme/fleet"})
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		p, err := ReadPacks(out, f)
		if err != nil || len(p.Sources) != 1 || p.Sources[0] != "acme/fleet" || len(p.Declared) != 0 {
			t.Errorf("%s: %+v %v\n%s", f, p, err, out)
		}
		if f != JSON && !strings.HasPrefix(string(out), s) {
			t.Errorf("%s: a byte already there moved\n%s", f, out)
		}
	}
}
