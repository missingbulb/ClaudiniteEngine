package builtin

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/checks/declared"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/findings"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packseed"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packset"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/transcript"
)

// A fleet manager states a seeded pack's config twice in one file: on its
// own entry for the pack, what a session here runs, and in its fleet
// block's packSeeds, what the pack-seed sweep writes
// into every member. The sweep seeds and never overrides, so a seed that
// disagrees with what the manager runs reaches the whole fleet once and
// sticks. It names no pack: for each seeded id the manager also declares,
// the two configs must be the same value, defaults spelled out on both
// sides. A pack the fleet hands out and the manager does not run is out
// of scope.
var fleetPackSeedAgrees = declared.Builtin{
	ID:     "fleet-pack-seed-agrees",
	Pack:   packset.FleetPack,
	OnFail: "block",
	Tags:   []string{"world", "builtin", packset.FleetPack},
	Doc:    "cn fleet",
	Why:    "the pack-seed sweep writes a seed into every member and never overrides an existing entry, so a seed that disagrees with what the enforcer runs reaches the whole fleet once and sticks",
}

func init() { register(&fleetPackSeedAgrees, runFleetPackSeedAgrees) }

func runFleetPackSeedAgrees(ctx *declared.Ctx, _ *transcript.Session) []findings.Finding {
	if ctx.Config.Fleet == nil {
		return nil
	}
	declared := map[string]bool{}
	for _, p := range ctx.Config.Packs {
		if p.Kind != packset.Local && p.Kind != packset.Temp {
			declared[p.ID] = true
		}
	}
	text, _ := ctx.Read(ctx.Config.SettingsPath)
	var out []findings.Finding
	for _, seed := range packseed.Parse(ctx.Config.Fleet) {
		if !declared[seed.ID] {
			continue
		}
		var seeded, own any
		if seed.Config != nil {
			seeded = seed.Config
		}
		if c, ok := ctx.Config.PackConfig[seed.ID]; ok {
			own = c
		}
		if packseed.Agree(seeded, own) {
			continue
		}
		out = append(out, fleetPackSeedAgrees.Finding(ctx.Config.SettingsPath, lineNaming(text, seed.ID),
			fmt.Sprintf("the %q config seeded to the fleet (%s) is not the one this repo declares for itself (%s)", seed.ID, packseed.Canonical(seeded), packseed.Canonical(own)),
			fmt.Sprintf("make the packSeeds entry for %q and this repo's own %q entry carry the same config — spell out defaults on both sides rather than leaving one implicit, and correct it before the next sweep, which writes the seed into each member once and never revisits it", seed.ID, seed.ID)))
	}
	return out
}

// lineNaming is the first line naming id as a whole token, 0 for none.
func lineNaming(text, id string) int {
	re := regexp.MustCompile(`(^|[^a-z0-9-])` + regexp.QuoteMeta(id) + `([^a-z0-9-]|$)`)
	for i, l := range strings.Split(text, "\n") {
		if re.MatchString(l) {
			return i + 1
		}
	}
	return 0
}
