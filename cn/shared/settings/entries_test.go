package settings

import (
	"reflect"
	"strings"
	"testing"
)

// entrySamples each hold an engine block, a packs block with a bare id,
// an entry object and a comment inside, and blocks and comments after it.
var entrySamples = map[Format]struct{ raw, packs string }{
	YAML: {
		raw:   "# head comment\nengine:\n" + enginePart + "packs:\n  channel: \"canary\"\n  # inside the block\n  declared:\n    - hello\n    - id: acme-pack\n      config:\n        zeta: 1\n        alpha: \"a\"\n\n# between blocks\nchecks:\n  rules:\n    x: \"off\"\n",
		packs: "packs:\n  channel: \"canary\"\n  # inside the block\n  declared:\n    - hello\n    - id: acme-pack\n      config:\n        zeta: 1\n        alpha: \"a\"\n",
	},
	TOML: {
		raw:   "# head comment\n[engine]\nversion = \"1.1.0\"\nmanifest = \"" + pin1 + "\"\n\n[packs]\nchannel = \"canary\"\ndeclared = [\"hello\", { id = \"acme-pack\", config = { zeta = 1, alpha = \"a\" } }]\n\n[checks.rules]\nx = \"off\"\n",
		packs: "[packs]\nchannel = \"canary\"\ndeclared = [\"hello\", { id = \"acme-pack\", config = { zeta = 1, alpha = \"a\" } }]\n",
	},
	JSON: {
		raw:   "{\n  \"engine\": {\"version\": \"1.1.0\", \"manifest\": \"" + pin1 + "\"},\n  \"packs\": {\"channel\": \"canary\", \"declared\": [\"hello\", {\"id\": \"acme-pack\", \"config\": {\"zeta\": 1, \"alpha\": \"a\"}}]},\n  \"checks\": {\"rules\": {\"x\": \"off\"}}\n}\n",
		packs: "{\"channel\": \"canary\", \"declared\": [\"hello\", {\"id\": \"acme-pack\", \"config\": {\"zeta\": 1, \"alpha\": \"a\"}}]}",
	},
}

// outside is raw with its packs block cut out, at the range blockRange
// names.
func outside(t *testing.T, raw []byte, f Format) (string, string) {
	t.Helper()
	s, e, err := blockRange(raw, f, "packs")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw[:s]), string(raw[e:])
}

func TestBlockRange(t *testing.T) {
	for f, c := range entrySamples {
		s, e, err := blockRange([]byte(c.raw), f, "packs")
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if got := c.raw[s:e]; got != c.packs {
			t.Errorf("%s: block %q, want %q", f, got, c.packs)
		}
	}
}

func TestSetAnswerTurnsAnIDIntoAnEntry(t *testing.T) {
	for f, c := range entrySamples {
		out, err := SetAnswer([]byte(c.raw), f, "hello", "goals", "n/a — none wanted")
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		p, err := ParseFile(out, f)
		if err != nil {
			t.Fatalf("%s: %v\n%s", f, err, out)
		}
		e, ok := p.Packs.Entry("hello", false)
		if !ok || !e.Object || !reflect.DeepEqual(e.Answers, map[string]string{"goals": "n/a — none wanted"}) {
			t.Errorf("%s: hello entry %+v\n%s", f, e, out)
		}
		acme, _ := p.Packs.Entry("acme-pack", false)
		if acme.Config["alpha"] != "a" || p.Packs.Channel != "canary" || p.Checks.Rules["x"] != "off" {
			t.Errorf("%s: another value moved: %+v %+v\n%s", f, acme, p, out)
		}
		wantBefore, wantAfter := outside(t, []byte(c.raw), f)
		gotBefore, gotAfter := outside(t, out, f)
		if gotBefore != wantBefore || gotAfter != wantAfter {
			t.Errorf("%s: bytes outside the packs block changed:\n%s", f, out)
		}
		if f != JSON && strings.Index(string(out), "zeta") > strings.Index(string(out), "alpha") {
			t.Errorf("%s: config keys reordered:\n%s", f, out)
		}
		again, err := SetAnswer(out, f, "hello", "goals", "other")
		if err != nil {
			t.Fatal(err)
		}
		if p, _ := ParseFile(again, f); p.Packs.Entries[0].Answers["goals"] != "other" {
			t.Errorf("%s: a second answer did not replace the first:\n%s", f, again)
		}
	}
}

func TestSetEntryConfig(t *testing.T) {
	for f, c := range entrySamples {
		out, err := SetEntryConfig([]byte(c.raw), f, "acme-pack", "beta", true)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		p, err := ParseFile(out, f)
		if err != nil {
			t.Fatal(err)
		}
		e, _ := p.Packs.Entry("acme-pack", false)
		if e.Config["beta"] != true || e.Config["alpha"] != "a" {
			t.Errorf("%s: config %v", f, e.Config)
		}
	}
}

func TestEntryWriterRefuses(t *testing.T) {
	raw := []byte(entrySamples[YAML].raw)
	if _, err := SetAnswer(raw, YAML, "nope", "q", "a"); err == nil || !strings.Contains(err.Error(), "does not name nope") {
		t.Errorf("answered an undeclared pack: %v", err)
	}
	if _, err := SetAnswer(raw, YAML, "hello", "", "a"); err == nil {
		t.Error("answered no question")
	}
	inline := []byte("engine:\n" + enginePart + "packs: {declared: [hello]}\n")
	if _, err := SetAnswer(inline, YAML, "hello", "q", "a"); err == nil || !strings.Contains(err.Error(), "by hand") {
		t.Errorf("rewrote a flow-style block: %v", err)
	}
}

func TestAnswersAreText(t *testing.T) {
	raw := []byte("engine:\n" + enginePart + "packs:\n  declared:\n    - id: hello\n      answers:\n        q: 3\n")
	if _, err := ParseFile(raw, YAML); err == nil || !strings.Contains(err.Error(), "must be text") {
		t.Errorf("a number answer parsed: %v", err)
	}
}

func TestAddDeclaredBesideAnEntryObject(t *testing.T) {
	for f, c := range entrySamples {
		out, err := AddDeclared([]byte(c.raw), f, "new-pack")
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		p, err := ReadPacks(out, f)
		if err != nil || !reflect.DeepEqual(p.Declared, []string{"hello", "acme-pack", "new-pack"}) {
			t.Errorf("%s: %v %v\n%s", f, p.Declared, err, out)
		}
	}
}
