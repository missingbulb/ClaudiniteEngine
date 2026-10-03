package provenance

import "testing"

func TestADeclarationIsComparedByValueNotSpelling(t *testing.T) {
	escaped, ok := declarationOf(`[{"id": "x", "fix": "take it — now", "n": 1.0, "p": "a\/b"}]`, "x")
	if !ok {
		t.Fatal("escaped declaration not found")
	}
	literal, ok := declarationOf(`[{"id":"x","fix":"take it — now","n":1,"p":"a/b"}]`, "x")
	if !ok {
		t.Fatal("literal declaration not found")
	}
	if escaped != literal {
		t.Errorf("one value, two readings:\n%s\n%s", escaped, literal)
	}
	other, _ := declarationOf(`[{"id":"x","fix":"take it — later","n":1,"p":"a/b"}]`, "x")
	if other == literal {
		t.Error("a changed value reads as unchanged")
	}
}

func TestADeclarationIsFoundByItsExactID(t *testing.T) {
	if _, ok := declarationOf(`[{"ID":"x"},{"id":"y"}]`, "x"); ok {
		t.Error(`"ID" read as "id"`)
	}
	if _, ok := declarationOf(`{"id":"x"}`, "x"); ok {
		t.Error("a file that is not an array declared a check")
	}
}
