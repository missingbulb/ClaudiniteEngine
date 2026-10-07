package packseed

import "testing"

func TestParseDropsMalformedSeedsAndKeepsOnlyObjectConfigs(t *testing.T) {
	got := Parse(map[string]any{"packSeeds": []any{
		"bare", map[string]any{"id": " a "}, map[string]any{"id": ""}, map[string]any{"id": 3},
		map[string]any{"id": "b", "config": "x"}, map[string]any{"id": "c", "config": map[string]any{"k": 1.0}},
	}})
	if len(got) != 3 || got[0].ID != "a" || got[1].ID != "b" || got[1].Config != nil || got[2].Config["k"] != 1.0 {
		t.Fatalf("%+v", got)
	}
	if Parse(map[string]any{"packSeeds": "nope"}) != nil || Parse(nil) != nil {
		t.Fatal("a missing or non-list packSeeds is no seed")
	}
}

func TestAgreeIsValueEqualityIgnoringKeyOrder(t *testing.T) {
	a := map[string]any{"x": 1.0, "y": map[string]any{"b": []any{"<", 2.0}, "a": true}}
	b := map[string]any{"y": map[string]any{"a": true, "b": []any{"<", 2.0}}, "x": 1.0}
	if !Agree(a, b) {
		t.Fatal("the same value in another key order disagrees")
	}
	if Agree(a, map[string]any{"x": 1.0}) || Agree(nil, map[string]any{}) {
		t.Fatal("different values agree")
	}
	var none map[string]any
	if !Agree(none, nil) {
		t.Fatal("a nil config is none")
	}
	if Canonical(b) != `{"x":1,"y":{"a":true,"b":["<",2]}}` {
		t.Fatal(Canonical(b))
	}
}
