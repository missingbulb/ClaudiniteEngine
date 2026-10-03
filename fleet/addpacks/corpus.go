package addpacks

import (
	"github.com/missingbulb/ClaudiniteEngine/fleet"
	"github.com/missingbulb/ClaudiniteEngine/shared/packindex"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
)

// Corpus is the catalog a sweep reads. A member is fingerprinted against
// Of its own channel, the channel its own update delivers from; the
// plausibility floor and a force's id check read Union, every id either
// channel offers.
type Corpus struct {
	Union []packindex.CatalogPack
	Of    func(channel string) []packindex.CatalogPack
}

// CatalogCorpus is a catalog as a sweep reads it: the canary view holds
// every id, so it is the union.
func CatalogCorpus(c packindex.Catalog) Corpus {
	return Corpus{Union: c.For(packindex.Canary), Of: c.For}
}

// FixedCorpus is one list every member reads, for a fixture.
func FixedCorpus(packs []packindex.CatalogPack) Corpus {
	return Corpus{Union: packs, Of: func(string) []packindex.CatalogPack { return packs }}
}

// MemberChannel is the channel a member reads pack versions from: its
// declaration's, stable when it names none.
func MemberChannel(m fleet.Member) string {
	if m.Packs.Channel == "" {
		return settings.ChannelStable
	}
	return m.Packs.Channel
}

// For is the corpus a member is fingerprinted against.
func (c Corpus) For(m fleet.Member) []packindex.CatalogPack {
	if c.Of == nil {
		return c.Union
	}
	return c.Of(MemberChannel(m))
}

// Count is the catalog's distinct pack ids and how many of them carry an
// entry on each channel.
func Count(c packindex.Catalog) (ids, stable, canary int) {
	on := map[string]map[string]bool{}
	for _, p := range c.Packs {
		if on[p.ID] == nil {
			on[p.ID] = map[string]bool{}
		}
		on[p.ID][p.Channel] = true
	}
	for _, ch := range on {
		if ch[packindex.Canary] {
			canary++
		}
		if ch[packindex.Stable] {
			stable++
		}
	}
	return len(on), stable, canary
}
