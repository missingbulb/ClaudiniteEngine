package descriptor

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindExactlyOne(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := Find(dir, "pack"); !errors.Is(err, ErrAbsent) {
		t.Fatalf("none: %v", err)
	}
	write(t, dir, "pack.yaml", "version: \"1.0\"\n")
	p, f, err := Find(dir, "pack")
	if err != nil || f != YAML || filepath.Base(p) != "pack.yaml" {
		t.Fatalf("one: %s %s %v", p, f, err)
	}
	write(t, dir, "pack.json", "{}")
	_, _, err = Find(dir, "pack")
	if !errors.Is(err, ErrDuplicate) || !strings.Contains(err.Error(), "pack.json") || !strings.Contains(err.Error(), "pack.yaml") {
		t.Fatalf("two: %v", err)
	}
}

// One descriptor written three ways parses to the same Go value.
func TestThreeFormatsOneValue(t *testing.T) {
	js := `{"version": "1.1", "minEngineVersion": "1.61001.1", "requires": ["a", "b"], "prose": null,
  "n": 3, "f": 1.5, "ok": true, "nested": {"k": "v", "list": [1, 2]}}`
	ym := `version: "1.1"
minEngineVersion: "1.61001.1"
requires:
  - a
  - b
prose: null
n: 3
f: 1.5
ok: true
nested:
  k: v
  list: [1, 2]
`
	tm := `version = "1.1"
minEngineVersion = "1.61001.1"
requires = ["a", "b"]
n = 3
f = 1.5
ok = true
[nested]
k = "v"
list = [1, 2]
`
	j, err := ParseBytes([]byte(js), JSON)
	if err != nil {
		t.Fatal(err)
	}
	y, err := ParseBytes([]byte(ym), YAML)
	if err != nil {
		t.Fatal(err)
	}
	tv, err := ParseBytes([]byte(tm), TOML)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(j, y) {
		t.Fatalf("json %#v\nyaml %#v", j, y)
	}
	// TOML has no null: the same descriptor without the null key.
	jm := j.(map[string]any)
	delete(jm, "prose")
	if !reflect.DeepEqual(jm, tv) {
		t.Fatalf("json %#v\ntoml %#v", jm, tv)
	}
}

// YAML 1.1's yes/no/on/off are plain strings here, as in YAML 1.2.
func TestYAMLBoolsAreStrict(t *testing.T) {
	v, err := ParseBytes([]byte("a: yes\nb: on\nc: true\n"), YAML)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["a"] != "yes" || m["b"] != "on" || m["c"] != true {
		t.Fatalf("%#v", m)
	}
}

// A CloudFormation short tag keeps its scalar, as the Node engine's
// minimal YAML reader does.
func TestYAMLDocumentShortTags(t *testing.T) {
	v, err := ParseDocument([]byte("Resources:\n  Fn:\n    Properties:\n      Bucket: !Ref MyBucket\n      Port: !Ref 8080\n      List: !Split [a, b]\n"), YAML)
	if err != nil {
		t.Fatal(err)
	}
	props := v.(map[string]any)["Resources"].(map[string]any)["Fn"].(map[string]any)["Properties"].(map[string]any)
	if props["Bucket"] != "MyBucket" || props["Port"] != float64(8080) || !reflect.DeepEqual(props["List"], []any{"a", "b"}) {
		t.Fatalf("%#v", props)
	}
}

func TestParseRefusesBrokenInput(t *testing.T) {
	for f, body := range map[Format]string{JSON: "{", YAML: "a: [", TOML: "a = "} {
		if _, err := ParseBytes([]byte(body), f); err == nil {
			t.Errorf("%s: no error", f)
		}
	}
}

func TestSchemaValidation(t *testing.T) {
	s := Schema{Name: "pack", Required: []string{"version"}, Keys: map[string]Kind{
		"version": String, "requires": StringList, "prose": StringOrNull, "env": Object, "n": Number, "b": Bool, "any": Any,
	}}
	ok := map[string]any{"version": "1", "requires": []any{"a"}, "prose": nil, "env": map[string]any{}, "n": 1.0, "b": true, "any": []any{1.0}}
	if errs := s.Validate(ok); len(errs) != 0 {
		t.Fatalf("valid: %v", errs)
	}
	bad := map[string]any{"requires": []any{1.0}, "prose": 3.0, "extra": "x"}
	errs := s.Validate(bad)
	joined := ""
	for _, e := range errs {
		joined += e.Error() + "\n"
	}
	for _, want := range []string{`"version" is missing`, `"requires" must be a list of strings`, `"prose" must be a string or null`, `"extra" is not a pack key`} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
}

func TestParseFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "declared-checks.toml", "[[check]]\nid = \"x\"\n")
	v, path, err := Parse(dir, "declared-checks")
	if err != nil || filepath.Base(path) != "declared-checks.toml" {
		t.Fatalf("%v %s", err, path)
	}
	if _, ok := v.(map[string]any); !ok {
		t.Fatalf("%#v", v)
	}
}

// A key named twice in one mapping is refused in every format, never
// resolved to the last spelling.
func TestDuplicateKeysRefused(t *testing.T) {
	for f, body := range map[Format]string{
		JSON: `{"a": {"b": 1, "b": 2}}`,
		YAML: "a:\n  b: 1\n  b: 2\n",
		TOML: "[a]\nb = 1\nb = 2\n",
	} {
		if v, err := ParseBytes([]byte(body), f); err == nil {
			t.Errorf("%s: accepted %#v", f, v)
		}
	}
	for f, body := range map[Format]string{
		JSON: `{"a": [{"b": 1}, {"b": 2}], "c": {"b": [1, {"b": 3}]}}`,
		YAML: "a:\n  - b: 1\n  - b: 2\nc:\n  b: 1\n",
	} {
		if _, err := ParseBytes([]byte(body), f); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}
