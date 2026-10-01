package settings

import (
	"strings"
	"testing"
)

const enginePart = "  version: \"1.1.0\"\n  manifest: \"" + pin1 + "\"\n"

var packsSamples = map[Format]string{
	YAML: "# mine\nengine:\n" + enginePart + "packs:\n  channel: \"canary\"\n  declared:\n    - hello\n    - \"acme-pack\"\nafter: 1\n",
	TOML: "[engine]\nversion = \"1.1.0\"\nmanifest = \"" + pin1 + "\"\n\n[packs]\nchannel = \"canary\"\ndeclared = [\"hello\", \"acme-pack\"]\n\n[other]\nx = 1\n",
	JSON: "{\n  \"engine\": {\"version\": \"1.1.0\", \"manifest\": \"" + pin1 + "\"},\n  \"packs\": {\n    \"channel\": \"canary\",\n    \"declared\": [\n      \"hello\",\n      \"acme-pack\"\n    ]\n  }\n}\n",
}

func TestReadPacks(t *testing.T) {
	for f, s := range packsSamples {
		p, err := ReadPacks([]byte(s), f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if p.Channel != "canary" || strings.Join(p.Declared, ",") != "hello,acme-pack" || !p.Present {
			t.Errorf("%s: %+v", f, p)
		}
	}
}

func TestReadPacksAbsentBlockDeclaresNothing(t *testing.T) {
	for f, s := range samples {
		p, err := ReadPacks([]byte(s), f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if p.Channel != "stable" || len(p.Declared) != 0 || p.Present {
			t.Errorf("%s: %+v", f, p)
		}
	}
}

func TestReadPacksChannelDefaultsToStable(t *testing.T) {
	raw := "engine:\n" + enginePart + "packs:\n  declared:\n    - hello\n"
	p, err := ReadPacks([]byte(raw), YAML)
	if err != nil || p.Channel != "stable" || len(p.Declared) != 1 {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestReadPacksRefuses(t *testing.T) {
	cases := map[string]struct {
		f   Format
		raw string
	}{
		"bad channel":        {YAML, "packs:\n  channel: \"beta\"\n"},
		"unquoted channel":   {YAML, "packs:\n  channel: canary\n"},
		"bad id":             {YAML, "packs:\n  declared:\n    - Hello\n"},
		"duplicate id":       {YAML, "packs:\n  declared:\n    - hello\n    - hello\n"},
		"flow list in yaml":  {YAML, "packs:\n  declared: [hello]\n"},
		"stray key":          {YAML, "packs:\n  declared:\n    - hello\n  extra: 1\n"},
		"two channel lines":  {YAML, "packs:\n  channel: \"stable\"\n  channel: \"canary\"\n"},
		"toml bad id":        {TOML, "[packs]\ndeclared = [\"a b\"]\n"},
		"toml unquoted":      {TOML, "[packs]\ndeclared = [hello]\n"},
		"json two blocks":    {JSON, "{\"packs\": {}, \"x\": {\"packs\": {}}}"},
		"json nested object": {JSON, "{\"packs\": {\"channel\": {\"a\": 1}}}"},
		"json bad channel":   {JSON, "{\"packs\": {\"channel\": \"nightly\"}}"},
		"two yaml blocks":    {YAML, "packs:\n  declared:\n    - a\npacks:\n  declared:\n    - b\n"},
	}
	for name, c := range cases {
		if p, err := ReadPacks([]byte(c.raw), c.f); err == nil {
			t.Errorf("%s: accepted %+v", name, p)
		}
	}
}

func TestAddDeclared(t *testing.T) {
	cases := []struct {
		name string
		f    Format
		in   string
		want string
	}{
		{"yaml block", YAML, packsSamples[YAML], strings.Replace(packsSamples[YAML], "    - \"acme-pack\"\n", "    - \"acme-pack\"\n    - new-pack\n", 1)},
		{"yaml no block", YAML, samples[YAML], samples[YAML] + "packs:\n  declared:\n    - new-pack\n"},
		{"yaml no newline at end", YAML, "engine:\n  version: \"1.1.0\"", "engine:\n  version: \"1.1.0\"\npacks:\n  declared:\n    - new-pack\n"},
		{"yaml block without declared", YAML, "engine:\n" + enginePart + "packs:\n  channel: \"canary\"\nz: 1\n", "engine:\n" + enginePart + "packs:\n  channel: \"canary\"\n  declared:\n    - new-pack\nz: 1\n"},
		{"yaml empty declared", YAML, "packs:\n  declared:\n  channel: \"stable\"\n", "packs:\n  declared:\n    - new-pack\n  channel: \"stable\"\n"},
		{"toml block", TOML, packsSamples[TOML], strings.Replace(packsSamples[TOML], `"acme-pack"]`, `"acme-pack", "new-pack"]`, 1)},
		{"toml empty list", TOML, "[packs]\ndeclared = []\n", "[packs]\ndeclared = [\"new-pack\"]\n"},
		{"toml block without declared", TOML, "[packs]\nchannel = \"canary\"\n\n[x]\n", "[packs]\ndeclared = [\"new-pack\"]\nchannel = \"canary\"\n\n[x]\n"},
		{"toml no block", TOML, samples[TOML], samples[TOML] + "\n[packs]\ndeclared = [\"new-pack\"]\n"},
		{"json block", JSON, packsSamples[JSON], strings.Replace(packsSamples[JSON], "\"acme-pack\"\n", "\"acme-pack\", \"new-pack\"\n", 1)},
		{"json empty list", JSON, "{\"engine\": {}, \"packs\": {\"declared\": []}}", "{\"engine\": {}, \"packs\": {\"declared\": [\"new-pack\"]}}"},
		{"json block without declared", JSON, "{\"packs\": {\"channel\": \"stable\"}}", "{\"packs\": {\"declared\": [\"new-pack\"], \"channel\": \"stable\"}}"},
		{"json no block", JSON, samples[JSON], strings.Replace(samples[JSON], pin1+"\"\n  }", pin1+"\"\n  },\n  \"packs\": {\"declared\": [\"new-pack\"]}", 1)},
	}
	for _, c := range cases {
		got, err := AddDeclared([]byte(c.in), c.f, "new-pack")
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if string(got) != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
			continue
		}
		p, err := ReadPacks(got, c.f)
		if err != nil || p.Declared[len(p.Declared)-1] != "new-pack" {
			t.Errorf("%s: re-read %+v %v", c.name, p, err)
		}
	}
}

func TestAddDeclaredRefuses(t *testing.T) {
	if _, err := AddDeclared([]byte(packsSamples[YAML]), YAML, "hello"); err == nil {
		t.Error("added an id already declared")
	}
	if _, err := AddDeclared([]byte(packsSamples[YAML]), YAML, "Bad"); err == nil {
		t.Error("added a malformed id")
	}
}

func TestPinOnlyChangeRefusesAPacksChange(t *testing.T) {
	for f, s := range packsSamples {
		moved, err := AddDeclared([]byte(s), f, "new-pack")
		if err != nil {
			t.Fatal(err)
		}
		if err := PinOnlyChange([]byte(s), moved, f); err == nil {
			t.Errorf("%s: a packs change passed as pin-only", f)
		}
		repinned, err := SetPin([]byte(s), f, "1.2.0", pin2)
		if err != nil {
			t.Fatal(err)
		}
		if err := PinOnlyChange([]byte(s), repinned, f); err != nil {
			t.Errorf("%s: a pin move beside a packs block: %v", f, err)
		}
	}
}
