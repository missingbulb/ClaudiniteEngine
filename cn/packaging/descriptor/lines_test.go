package descriptor

import "testing"

func TestKeyLinesAnchorOnTheStructure(t *testing.T) {
	cases := map[Format]string{
		JSON: "{\n  \"nested\": {\"marker\": 1},\n  \"list\": [\n    {\"id\": \"a\", \"note\": \"severity\"},\n    {\"id\": \"b\",\n     \"severity\": \"advisory\"}\n  ],\n  \"marker\": null\n}\n",
		YAML: "nested:\n  marker: 1\nlist:\n  - id: a\n    note: severity\n  - id: b\n    severity: advisory\nmarker: null\n",
		TOML: "marker = 0\n[nested]\nmarker = 1\n[[list]]\nid = \"a\"\nnote = \"severity\"\n[[list]]\nid = \"b\"\nseverity = \"advisory\"\n",
	}
	want := map[Format]map[string]int{
		JSON: {"marker": 8, "nested.marker": 2, "list.0.note": 4, "list.1.severity": 6},
		YAML: {"marker": 8, "nested.marker": 2, "list.0.note": 5, "list.1.severity": 7},
		TOML: {"marker": 1, "nested.marker": 3, "list.0.note": 6, "list.1.severity": 9},
	}
	for f, raw := range cases {
		got := KeyLines([]byte(raw), f)
		for k, line := range want[f] {
			if got[k] != line {
				t.Errorf("%s %s: line %d, want %d (all: %v)", f, k, got[k], line, got)
			}
		}
		if _, ok := got["list.0.severity"]; ok {
			t.Errorf("%s: a value reading severity is no key", f)
		}
	}
}
