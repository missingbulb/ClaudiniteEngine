package main

import (
	"reflect"
	"testing"
)

// Each retired spelling runs as today's, and a spelling still current
// passes through untouched.
func TestRetiredSpellingsRunAsTodays(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ from, to []string }{
		{[]string{"init", "--packs", "basics,acme-pack", "--channel", "canary"}, []string{"adopt", "basics,acme-pack", "--channel", "canary"}},
		{[]string{"init", "--packs=basics", "--repo", "r"}, []string{"adopt", "basics", "--repo", "r"}},
		{[]string{"settings", "answer", "acme-pack/goals", "n/a", "--repo", "r"}, []string{"adopt", "--answer", "acme-pack/goals=n/a", "--repo", "r"}},
		{[]string{"settings", "answer", "acme-pack/goals", "--", "- x", "--repo", "r"}, []string{"adopt", "--answer", "acme-pack/goals=- x", "--repo", "r"}},
		{[]string{"pack", "new", "mine", "--belongs", "b"}, []string{"adopt", "local/mine", "--belongs", "b"}},
		{[]string{"rules-index", "--repo", "r"}, []string{"adopt", "--repo", "r"}},
		{[]string{"tasks", "list"}, []string{"check", "list", "--tasks"}},
		{[]string{"tasks", "flat", "--write", "--repo", "r"}, []string{"adopt", "--repo", "r"}},
		{[]string{"schedule", "drain"}, []string{"execute", "dispatch"}},
		{[]string{"execute", "continue"}, []string{"execute", "dispatch", "--continue"}},
		{[]string{"provenance", "brief", "acme-pack", "a"}, []string{"provenance", "backfill", "acme-pack", "a"}},
		{[]string{"provenance", "apply", "acme-pack", "b.md", "--backfill"}, []string{"provenance", "backfill", "acme-pack", "--apply", "b.md", "--backfill"}},
	} {
		got, note := retired(c.from)
		if !reflect.DeepEqual(got, c.to) || note == "" {
			t.Errorf("%q ran as %q (%q), want %q", c.from, got, note, c.to)
		}
	}
	for _, args := range [][]string{
		{"adopt", "basics"}, {"rules-index", "--check"}, {"tasks", "flat", "--paths"}, {"provenance", "check", "acme-pack"}, {"schedule", "run"}, {"execute", "loop"},
	} {
		if got, note := retired(args); !reflect.DeepEqual(got, args) || note != "" {
			t.Errorf("%q rewritten to %q (%q)", args, got, note)
		}
	}
}
