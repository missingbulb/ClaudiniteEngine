package update

import (
	"fmt"

	"github.com/missingbulb/ClaudiniteEngine/shared/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/shared/settings"
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
// must not mark the version held, revoked or deprecated, and its manifest
// must verify against d.Roots and hash to e.Manifest. cn check world and
// Land both run it.
func CheckPin(d Deps, e settings.Engine) error {
	p, err := d.Registry.Packument(e.Package)
	if err != nil {
		return err
	}
	if why := pinRefusal(p, StatesFromPackument(p), e.Version); why != "" {
		return fmt.Errorf("%s %s is %s", e.Package, e.Version, why)
	}
	got, err := Fetch(FetchInput{Registry: d.Registry, Package: e.Package, Version: e.Version, Packument: p,
		Roots: d.Roots, CacheRoot: d.CacheRoot, Platform: d.Platform, Now: d.Now()})
	if err != nil {
		return err
	}
	if got.Integrity != e.Manifest {
		return fmt.Errorf("engine.manifest %s is not the SHA-512 of %s %s's manifest.json (%s)", e.Manifest, e.Package, e.Version, got.Integrity)
	}
	return nil
}
