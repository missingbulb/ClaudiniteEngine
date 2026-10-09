package checks

import (
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/builtin"
	"github.com/missingbulb/ClaudiniteEngine/cn/capabilities/checks/declared"
)

const fleetSettings = settingsYAML + `fleet:
  owner: acme
  packSeeds:
    - id: claudinite-lifecycle
      config:
        delivery: pr
    - id: elsewhere
      config:
        a: 1
`

// A seed the manager also declares must carry the config the manager runs;
// a seed it does not declare is out of scope, and a repo with no fleet
// block runs no such check.
func TestFleetPackSeedAgrees(t *testing.T) {
	t.Parallel()
	differs := repo{settings: fleetSettings}
	expect(t, differs.run(t, "fleet-pack-seed-agrees"),
		want{path: ".claudinite/settings.yaml", line: 6, what: `^the "claudinite-lifecycle" config seeded to the fleet \(\{"delivery":"pr"\}\) is not the one this repo declares for itself \(null\)$`})
	agrees := repo{settings: strings.Replace(fleetSettings, "    - claudinite-lifecycle\n", "    - id: claudinite-lifecycle\n      config:\n        delivery: pr\n", 1)}
	expect(t, agrees.run(t, "fleet-pack-seed-agrees"))
	dir := repo{}.build(t)
	set, err := loadSet(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range set.Builtins {
		if b.ID == "fleet-pack-seed-agrees" {
			t.Error("a repo with no fleet block runs fleet-pack-seed-agrees")
		}
	}
}

func loadSet(dir string) (*declared.Set, error) {
	return declared.LoadSet(dir, "0.0.0", builtin.All()...)
}
