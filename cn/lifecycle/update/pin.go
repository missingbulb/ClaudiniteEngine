package update

import (
	"fmt"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/settings"
)

// pinRefusal names why a version may not be pinned: npm marks it held,
// revoked or deprecated. Empty means none of those.
func pinRefusal(p *npmreg.Packument, states States, ver string) string {
	if k, reason := states.Of(ver); k != "" {
		return fmt.Sprintf("%s: %s", k, reason)
	}
	if v, ok := p.Versions[ver]; ok && v.Deprecated != "" {
		return "deprecated on npm: " + v.Deprecated
	}
	return ""
}

// CheckPin verifies a pin as the updater would before proposing it: npm
// must not mark the version held, revoked or deprecated (a pin read from
// engine.releases has no such marks), and its manifest must verify against
// d.Roots and hash to e.Manifest. cn check world and
// Land both run it.
func CheckPin(d Deps, e settings.Engine) error {
	_, err := checkPin(d, e)
	return err
}

// checkPin is CheckPin, returning the verified release.
func checkPin(d Deps, e settings.Engine) (Fetched, error) {
	var p *npmreg.Packument
	if e.Releases == "" {
		var err error
		if p, err = d.Registry.Packument(e.Package); err != nil {
			return Fetched{}, err
		}
		if why := pinRefusal(p, StatesFromPackument(p), e.Version); why != "" {
			return Fetched{}, fmt.Errorf("%s %s is %s", e.Package, e.Version, why)
		}
	}
	got, err := Fetch(fetchInput(d, e, e.Version, p))
	if err != nil {
		return Fetched{}, err
	}
	if got.Integrity != e.Manifest {
		return Fetched{}, fmt.Errorf("engine.manifest %s is not the SHA-512 of %s %s's manifest.json (%s)", e.Manifest, e.Package, e.Version, got.Integrity)
	}
	return got, nil
}

// fetchInput is Fetch's input for version ver of the engine e names, from
// npm or from e's releases repository.
func fetchInput(d Deps, e settings.Engine, ver string, p *npmreg.Packument) FetchInput {
	return FetchInput{Registry: d.Registry, Package: e.Package, Version: ver, Packument: p,
		Releases: e.Releases, ReleasesHost: d.ReleasesHost,
		Roots: d.Roots, CacheRoot: d.CacheRoot, Platform: d.Platform, Now: d.Now()}
}
