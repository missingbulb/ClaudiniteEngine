package findings

import (
	"bytes"
	"testing"
)

func TestPrintAndBreaks(t *testing.T) {
	fs := []Finding{
		{Class: Deprecation, ID: "bin-ignore", Path: ".gitignore", Sentence: "Move it."},
		{Class: Break, ID: "settings-file", Path: ".claudinite", Sentence: "Add one."},
	}
	var b bytes.Buffer
	Print(&b, fs)
	want := "deprecation bin-ignore .gitignore: Move it.\nbreak settings-file .claudinite: Add one.\n"
	if b.String() != want {
		t.Errorf("got %q", b.String())
	}
	if !AnyBreak(fs) || AnyBreak(fs[:1]) {
		t.Error("AnyBreak")
	}
}
