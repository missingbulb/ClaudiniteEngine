package seeds_test

import (
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/fleet"
	"github.com/missingbulb/ClaudiniteEngine/cn/fleet/seeds"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
)

const member = `# kept: the member's own words
engine:
  version: "1.2.3"
packs:
  channel: "stable"
  declared:
    - basics
# kept too
`

// A seed lands in the packs block alone: the member's comments and every
// other block read as they did.
func TestASeedChangesOnlyThePacksBlock(t *testing.T) {
	got, err := seeds.Splice(member, settings.YAML, []fleet.Seed{{ID: "acme-pack", Config: map[string]any{"repo": "acme/x"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, kept := range []string{"# kept: the member's own words\n", "engine:\n  version: \"1.2.3\"\n", "# kept too\n"} {
		if !strings.Contains(got, kept) {
			t.Errorf("lost %q:\n%s", kept, got)
		}
	}
	p, err := settings.ReadPacks([]byte(got), settings.YAML)
	if err != nil {
		t.Fatalf("%v:\n%s", err, got)
	}
	found := false
	for _, e := range p.Entries {
		if e.ID == "acme-pack" {
			found = true
			if e.Config["repo"] != "acme/x" {
				t.Errorf("config %v", e.Config)
			}
		}
	}
	if !found {
		t.Errorf("acme-pack not declared:\n%s", got)
	}
}

// A member that already configures a seeded pack keeps its config: the
// sweep seeds and never overrides.
func TestASeedNeverOverridesAMembersConfig(t *testing.T) {
	seed := fleet.Seed{ID: "acme-pack", Config: map[string]any{"repo": "acme/x"}}
	if v := seeds.Classify(true, true, seed, true); v.State != seeds.Set {
		t.Errorf("configured member: %v", v)
	}
	if v := seeds.Classify(true, false, seed, true); v.State != seeds.Writable {
		t.Errorf("bare declaration: %v", v)
	}
	if v := seeds.Classify(false, false, seed, false); v.State != seeds.NotVendored {
		t.Errorf("unvendored: %v", v)
	}
	if v := seeds.Classify(true, false, fleet.Seed{ID: "acme-pack"}, false); v.State != seeds.Set {
		t.Errorf("a bare seed on a declaring member: %v", v)
	}
}
