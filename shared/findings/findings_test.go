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

// A declared check's finding names its pack and line and prints its why
// and fix lines under the first, which stays the one-line form.
func TestPrintWhatWhyFix(t *testing.T) {
	var b bytes.Buffer
	Print(&b, []Finding{{Class: Coded, ID: "hello-declared", Pack: "hello", Path: "HELLO_DECLARED", Line: 1, Sentence: "HELLO_DECLARED exists", Why: "it proves the channel", Fix: "delete it"}})
	want := "finding hello/hello-declared HELLO_DECLARED:1: HELLO_DECLARED exists\n  why: it proves the channel\n  fix: delete it\n"
	if b.String() != want {
		t.Errorf("got %q", b.String())
	}
}
