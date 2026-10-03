package addpacks_test

import (
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/fleet"
	"github.com/missingbulb/ClaudiniteEngine/fleet/addpacks"
	"github.com/missingbulb/ClaudiniteEngine/shared/packindex"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
)

func ids(packs []packindex.CatalogPack) []string {
	out := []string{}
	for _, p := range packs {
		out = append(out, p.ID+"@"+p.Version)
	}
	return out
}

// A member is measured against its own channel's view, stable when it
// names none; the union, which the floor and a force read, holds every id.
func TestAMemberIsMeasuredOnItsOwnChannel(t *testing.T) {
	cat := packindex.Catalog{Packs: []packindex.CatalogPack{
		{ID: "acme-pack", Version: "1.0", Channel: packindex.Stable},
		{ID: "acme-pack", Version: "1.1", Channel: packindex.Canary},
		{ID: "acme-skill", Version: "0.1", Channel: packindex.Canary},
	}}
	c := addpacks.CatalogCorpus(cat)
	stable := fleet.Member{Shape: fleet.ShapeCn}
	canary := fleet.Member{Shape: fleet.ShapeCn, Packs: settings.Packs{Channel: settings.ChannelCanary}}
	if got := ids(c.For(stable)); len(got) != 1 || got[0] != "acme-pack@1.0" {
		t.Errorf("a stable member reads %v", got)
	}
	if got := ids(c.For(canary)); len(got) != 2 || got[0] != "acme-pack@1.1" || got[1] != "acme-skill@0.1" {
		t.Errorf("a canary member reads %v", got)
	}
	if got := ids(c.Union); len(got) != 2 {
		t.Errorf("the union is %v", got)
	}
	if n, s, k := addpacks.Count(cat); n != 2 || s != 1 || k != 2 {
		t.Errorf("counted %d ids, %d on stable, %d on canary", n, s, k)
	}
	fixed := addpacks.FixedCorpus(shelf)
	if got := ids(fixed.For(canary)); len(got) != 1 || got[0] != "acme-pack@1.0.0" {
		t.Errorf("a fixed corpus reads %v", got)
	}
}
