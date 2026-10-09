package settings

import (
	"strings"
	"testing"
)

// The fleet block marks a manager in every format; a file without one is
// no manager, and a key the block does not know is refused.
func TestTheFleetBlockMarksAManager(t *testing.T) {
	for f, raw := range map[Format]string{
		YAML: "fleet:\n  owner: acme\n  kind: user\n  exclude: [acme/sandbox]\n  packSeeds:\n    - id: basics\n  staleDays: 14\n",
		TOML: "[fleet]\nowner = \"acme\"\nkind = \"user\"\nexclude = [\"acme/sandbox\"]\npackSeeds = [{ id = \"basics\" }]\nstaleDays = 14\n",
		JSON: `{"fleet": {"owner": "acme", "kind": "user", "exclude": ["acme/sandbox"], "packSeeds": [{"id": "basics"}], "staleDays": 14}}`,
	} {
		p, err := ParseFile([]byte(raw), f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if p.Fleet == nil || p.Fleet["owner"] != "acme" || len(p.Fleet["exclude"].([]any)) != 1 || len(p.Fleet["packSeeds"].([]any)) != 1 {
			t.Errorf("%s: %#v", f, p.Fleet)
		}
	}
	if p, err := ParseFile([]byte("packs:\n  declared: [basics]\n"), YAML); err != nil || p.Fleet != nil {
		t.Errorf("no block: %#v %v", p.Fleet, err)
	}
	if p, err := ParseFile([]byte("fleet: {}\n"), YAML); err != nil || p.Fleet == nil {
		t.Errorf("an empty block is a manager: %#v %v", p.Fleet, err)
	}
	for _, raw := range []string{"fleet:\n  canonRepo: acme/canon\n", "fleet: [acme]\n", "fleet:\n  exclude: acme/x\n"} {
		if _, err := ParseFile([]byte(raw), YAML); err == nil || !strings.Contains(err.Error(), "fleet") {
			t.Errorf("%q: %v", raw, err)
		}
	}
}
