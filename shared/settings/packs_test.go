package settings

import (
	"reflect"
	"strings"
	"testing"
)

const enginePart = "  version: \"1.1.0\"\n  manifest: \"" + pin1 + "\"\n"

var packsSamples = map[Format]string{
	YAML: "# mine\nengine:\n" + enginePart + "packs:\n  channel: \"canary\"\n  declared:\n    - hello\n    - \"acme-pack\"\nchecks:\n  rules: {}\n",
	TOML: "[engine]\nversion = \"1.1.0\"\nmanifest = \"" + pin1 + "\"\n\n[packs]\nchannel = \"canary\"\ndeclared = [\"hello\", \"acme-pack\"]\n\n[checks]\n",
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

// pinSamples are the engine samples without the member's own top-level
// keys, which the pin readers skip and the parsed settings refuse.
var pinSamples = map[Format]string{
	YAML: "# member's own comment\nengine:\n  package: \"@claudinite/cli-rc\"\n  version: \"1.1.0\"\n  manifest: \"" + pin1 + "\"\n",
	TOML: "# comment\n[engine]\nmanifest = \"" + pin1 + "\"\nversion = \"1.1.0\"\n",
	JSON: "{\n  \"engine\": {\n    \"version\": \"1.1.0\",\n    \"manifest\": \"" + pin1 + "\"\n  }\n}\n",
}

func TestReadPacksAbsentBlockDeclaresNothing(t *testing.T) {
	for f, s := range pinSamples {
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
		"bad id":             {YAML, "packs:\n  declared:\n    - Hello\n"},
		"duplicate id":       {YAML, "packs:\n  declared:\n    - hello\n    - hello\n"},
		"unknown top key":    {YAML, "packs:\n  declared:\n    - hello\nafter: 1\n"},
		"entry version":      {YAML, "packs:\n  declared:\n    - id: hello\n      version: \"1.0\"\n"},
		"entry stray key":    {YAML, "packs:\n  declared:\n    - id: hello\n      extra: 1\n"},
		"entry without id":   {YAML, "packs:\n  declared:\n    - config: {}\n"},
		"entry bad local":    {YAML, "packs:\n  declared:\n    - local/\n"},
		"accept stray key":   {YAML, "checks:\n  accept:\n    - rule: x\n      reason: y\n      until: z\n"},
		"accept no rule":     {YAML, "checks:\n  accept:\n    - reason: y\n"},
		"checks stray key":   {YAML, "checks:\n  severity: {}\n"},
		"local twice":        {YAML, "packs:\n  declared:\n    - local/mine\n    - id: local/mine\n"},
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

// The Node engine's pack-entry via and answers are carried opaque, and an
// override in the retired severity spelling is read as its on_fail and
// recorded for verify to name.
func TestParseFileReadsTheNodeShapes(t *testing.T) {
	raw := "packs:\n  declared:\n    - id: hello\n      via: [basics]\n      answers: {store: \"o/r\"}\n      rules: {x: advisory}\nchecks:\n  rules:\n    y: blocking\n    z: \"off\"\n"
	p, err := ParseFile([]byte(raw), YAML)
	if err != nil {
		t.Fatal(err)
	}
	e := p.Packs.Entries[0]
	if !reflect.DeepEqual(e.Via, []any{"basics"}) || !reflect.DeepEqual(e.Answers, map[string]string{"store": "o/r"}) {
		t.Errorf("via %v, answers %v", e.Via, e.Answers)
	}
	rules, _, _ := p.Effective()
	if !reflect.DeepEqual(rules, map[string]string{"x": "advise", "y": "block", "z": "off"}) {
		t.Errorf("rules %v", rules)
	}
	got := map[string]string{}
	for _, r := range p.Retired {
		got[r.Rule] = r.Where + " " + r.Value + " " + r.OnFail
	}
	want := map[string]string{"x": "the hello pack entry advisory advise", "y": "the top-level checks block blocking block"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("retired %v", got)
	}
}

// Spellings the strict patterns refused parse once the parser reads the
// block, as any YAML reader reads them.
func TestReadPacksParsedSpellings(t *testing.T) {
	for name, raw := range map[string]string{
		"unquoted channel":  "packs:\n  channel: canary\n  declared:\n    - hello\n",
		"flow list in yaml": "packs:\n  channel: \"canary\"\n  declared: [hello]\n",
	} {
		p, err := ReadPacks([]byte(raw), YAML)
		if err != nil || p.Channel != "canary" || strings.Join(p.Declared, ",") != "hello" {
			t.Errorf("%s: %+v %v", name, p, err)
		}
	}
}

// An entry is an id string or an object; local/<name> names the repo's own
// pack and is kept out of the canon ids.
func TestReadPacksEntries(t *testing.T) {
	samples := map[Format]string{
		YAML: "packs:\n  declared:\n    - basics\n    - local/mine\n    - id: git-github\n      config: {barriers: [1]}\n      rules: {some-check: advise}\n      accept:\n        - rule: other\n          path: docs/\n          reason: why\nchecks:\n  rules:\n    hello-declared: \"off\"\n  accept:\n    - rule: top\n      reason: because\n",
		TOML: "[packs]\ndeclared = [\"basics\", \"local/mine\", {id = \"git-github\", config = {barriers = [1]}, rules = {some-check = \"advise\"}, accept = [{rule = \"other\", path = \"docs/\", reason = \"why\"}]}]\n\n[checks]\nrules = {hello-declared = \"off\"}\naccept = [{rule = \"top\", reason = \"because\"}]\n",
		JSON: `{"packs": {"declared": ["basics", "local/mine", {"id": "git-github", "config": {"barriers": [1]}, "rules": {"some-check": "advise"}, "accept": [{"rule": "other", "path": "docs/", "reason": "why"}]}]}, "checks": {"rules": {"hello-declared": "off"}, "accept": [{"rule": "top", "reason": "because"}]}}`,
	}
	for f, raw := range samples {
		parsed, err := ParseFile([]byte(raw), f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		p := parsed.Packs
		if strings.Join(p.Declared, ",") != "basics,git-github" || strings.Join(p.Local, ",") != "mine" || len(p.Entries) != 3 {
			t.Fatalf("%s: %+v", f, p)
		}
		e, ok := p.Entry("git-github", false)
		if !ok || !e.Object || e.Config["barriers"] == nil || e.Rules["some-check"] != "advise" || len(e.Accept) != 1 || e.Accept[0].Pack != "git-github" || e.Accept[0].Path != "docs/" {
			t.Errorf("%s: entry %+v", f, e)
		}
		if l, ok := p.Entry("mine", true); !ok || l.Token() != "local/mine" {
			t.Errorf("%s: local %+v", f, l)
		}
		rules, accept, conflicts := parsed.Effective()
		if rules["hello-declared"] != "off" || rules["some-check"] != "advise" || len(accept) != 2 || len(conflicts) != 0 {
			t.Errorf("%s: effective %v %+v %v", f, rules, accept, conflicts)
		}
	}
}

// Two sources setting one rule differently is reported, never resolved by
// order; agreeing sources are not a conflict.
func TestEffectiveConflict(t *testing.T) {
	raw := "packs:\n  declared:\n    - id: a\n      rules: {x: advise, y: \"off\"}\n    - id: b\n      rules: {y: \"off\"}\nchecks:\n  rules: {x: block}\n"
	parsed, err := ParseFile([]byte(raw), YAML)
	if err != nil {
		t.Fatal(err)
	}
	_, _, conflicts := parsed.Effective()
	if len(conflicts) != 1 || !strings.Contains(conflicts[0], `"x"`) {
		t.Fatalf("%v", conflicts)
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
		{"yaml no block", YAML, pinSamples[YAML], pinSamples[YAML] + "packs:\n  declared:\n    - new-pack\n"},
		{"yaml no newline at end", YAML, "engine:\n  version: \"1.1.0\"", "engine:\n  version: \"1.1.0\"\npacks:\n  declared:\n    - new-pack\n"},
		{"yaml block without declared", YAML, "engine:\n" + enginePart + "packs:\n  channel: \"canary\"\nchecks: {}\n", "engine:\n" + enginePart + "packs:\n  channel: \"canary\"\n  declared:\n    - new-pack\nchecks: {}\n"},
		{"yaml local entry", YAML, "packs:\n  declared:\n    - local/mine\n", "packs:\n  declared:\n    - local/mine\n    - new-pack\n"},
		{"yaml empty declared", YAML, "packs:\n  declared:\n  channel: \"stable\"\n", "packs:\n  declared:\n    - new-pack\n  channel: \"stable\"\n"},
		{"toml block", TOML, packsSamples[TOML], strings.Replace(packsSamples[TOML], `"acme-pack"]`, `"acme-pack", "new-pack"]`, 1)},
		{"toml empty list", TOML, "[packs]\ndeclared = []\n", "[packs]\ndeclared = [\"new-pack\"]\n"},
		{"toml block without declared", TOML, "[packs]\nchannel = \"canary\"\n\n[checks]\n", "[packs]\ndeclared = [\"new-pack\"]\nchannel = \"canary\"\n\n[checks]\n"},
		{"toml no block", TOML, pinSamples[TOML], pinSamples[TOML] + "\n[packs]\ndeclared = [\"new-pack\"]\n"},
		{"json block", JSON, packsSamples[JSON], strings.Replace(packsSamples[JSON], "\"acme-pack\"\n", "\"acme-pack\", \"new-pack\"\n", 1)},
		{"json empty list", JSON, "{\"engine\": {}, \"packs\": {\"declared\": []}}", "{\"engine\": {}, \"packs\": {\"declared\": [\"new-pack\"]}}"},
		{"json block without declared", JSON, "{\"packs\": {\"channel\": \"stable\"}}", "{\"packs\": {\"declared\": [\"new-pack\"], \"channel\": \"stable\"}}"},
		{"json no block", JSON, pinSamples[JSON], strings.Replace(pinSamples[JSON], pin1+"\"\n  }", pin1+"\"\n  },\n  \"packs\": {\"declared\": [\"new-pack\"]}", 1)},
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
