package packset

import (
	"path/filepath"
	"testing"
)

// Without Memoize every Load reads the tree; once it is called, the
// process keeps each answer (and every Remember value) for good.
func TestMemoizeKeepsAnswersForTheProcess(t *testing.T) {
	defer Forget()
	repo := member(t, "    - alpha\n")
	write(t, filepath.Join(Tree(repo, "alpha"), "pack.json"), `{"version": "1.0"}`)
	if s, _ := Load(repo, "0.0.0", true); tokens(s) != "alpha" {
		t.Fatalf("%q", tokens(s))
	}
	write(t, filepath.Join(repo, ".claudinite/settings.yaml"), "engine:\n  version: \"1.1.0\"\npacks:\n  declared: []\n")
	if s, _ := Load(repo, "0.0.0", true); tokens(s) != "" {
		t.Errorf("unmemoized Load kept %q", tokens(s))
	}
	calls := 0
	count := func() any { calls++; return calls }
	if a, b := Remember("k", count), Remember("k", count); a != 1 || b != 2 {
		t.Errorf("Remember memoized before Memoize: %v then %v", a, b)
	}
	Memoize()
	first, _ := Load(repo, "0.0.0", true)
	write(t, filepath.Join(repo, ".claudinite/settings.yaml"), "engine:\n  version: \"1.1.0\"\npacks:\n  declared:\n    - alpha\n")
	if s, _ := Load(repo, "0.0.0", true); tokens(s) != tokens(first) {
		t.Errorf("memoized Load read the tree again: %q", tokens(s))
	}
	if s, _ := Load(repo, "0.0.0", false); tokens(s) != "alpha" {
		t.Errorf("another key shared the answer: %q", tokens(s))
	}
	first3, again := Remember("k", count), Remember("k", count)
	if first3 != 3 || again != 3 {
		t.Errorf("Remember did not keep its value: %v then %v", first3, again)
	}
}
