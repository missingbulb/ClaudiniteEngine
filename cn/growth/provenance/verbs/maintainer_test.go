package verbs

import (
	"strings"
	"testing"
)

func TestReduceTextDropsOnlyALongQuoteStandingAlone(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`said "a phrase long enough" then`, `said (quote dropped) then`},
		{`"a phrase long enough".`, `(quote dropped).`},
		{`a short "quote" stays`, `a short "quote" stays`},
		{`x"a phrase long enough" y`, `x"a phrase long enough" y`},
		{`"a phrase long enough"x`, `"a phrase long enough"x`},
		{`said “a phrase long enough” then`, `said (quote dropped) then`},
		{"ran session_01RLeyDsEzPVwpYR2Ved1u7E", "ran a session"},
	} {
		if got := ReduceText(c.in, false); got != c.want {
			t.Errorf("ReduceText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := ReduceText("@ariel (owner) in acme/widgets#42", true); got != "the owner in a member repository" {
		t.Errorf("public reduction = %q", got)
	}
	if got := ReduceText("@ariel (owner) in acme/widgets#42", false); got != "@ariel (owner) in acme/widgets#42" {
		t.Errorf("a private canon keeps handles and member refs, got %q", got)
	}
}

func TestSplitReaffirmationAndRelinked(t *testing.T) {
	reason, retire := splitReaffirmation("Shipped twice. Retire when tags are automatic. Revisit yearly.")
	if reason != "Shipped twice." || retire != "Retire when tags are automatic. Revisit yearly." {
		t.Errorf("split = %q / %q", reason, retire)
	}
	if reason, retire = splitReaffirmation("e.g. retire it. not a capital"); retire != "" || reason != "e.g. retire it. not a capital" {
		t.Errorf("a lower-case sentence stays in the reason, got %q / %q", reason, retire)
	}
	got := relinked("[a](docs/x.md) [b](https://x.y) [c](#top) [d](/abs)")
	if want := "[a](../docs/x.md) [b](https://x.y) [c](#top) [d](/abs)"; got != want {
		t.Errorf("relinked = %q, want %q", got, want)
	}
}

func TestApplyTwiceWritesNothingTheSecondTime(t *testing.T) {
	io := NewOverlay(Checkout{Root: t.TempDir()})
	file := "packs/acme/provenance/writing-widget.md"
	if err := io.Write(file, "## 2026-07-01 · born · first\n- **Mechanism:** prose\n"); err != nil {
		t.Fatal(err)
	}
	brief := "```entry-defaults\n- **Source:** #20\n```\n```entry writing-widget\n## 2026-07-21 · reworded · shorter\n- **Reason:** long\n```\n"
	if a := Apply(io, "packs/acme", brief, false); len(a.Problems) > 0 || len(a.Written) != 1 {
		t.Fatalf("first apply = %+v", a)
	}
	text, _ := io.Read(file)
	if !strings.Contains(text, "- **Source:** #20\n- **Reason:** long") {
		t.Errorf("the defaults fence's Source leads the entry in field order, got %q", text)
	}
	if a := Apply(io, "packs/acme", brief, false); len(a.Written) != 0 || len(a.Skipped) != 1 {
		t.Errorf("second apply = %+v", a)
	}
}
