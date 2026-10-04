package release

import (
	"fmt"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

// UnpublishInput is an unpublish dispatch of promote.yml and what npm says
// each package of UnpublishPackages holds: its `npm view <pkg> versions
// --json`, keyed by package name.
type UnpublishInput struct {
	Version  string
	Versions map[string]string
}

// Unpublishing is what the unpublish job does: the npm unpublish commands
// for every rc package holding the version, or, when @claudinite/cli-rc has
// no such version, a notice and nothing.
type Unpublishing struct {
	Exists   bool
	Commands []string
	Notice   string
}

// UnpublishPackages are the packages an unpublish removes a version from:
// @claudinite/cli-rc and its platform packages, in deprecate's order.
func UnpublishPackages() []string {
	names := []string{"@claudinite/cli-rc"}
	for _, p := range version.Platforms {
		names = append(names, "@claudinite/cli-rc-"+p)
	}
	return names
}

// UnpublishCommands renders the removal of an rc version from npm. It
// refuses, writing no command, when the version is the only one a package
// holds: npm deletes a package whose last version is unpublished.
func UnpublishCommands(in UnpublishInput) (Unpublishing, error) {
	if _, err := version.Parse(in.Version); err != nil {
		return Unpublishing{}, err
	}
	held := map[string]map[string]bool{}
	for _, n := range UnpublishPackages() {
		vs, err := npmVersions(in.Versions[n])
		if err != nil {
			return Unpublishing{}, fmt.Errorf("%s: %w", n, err)
		}
		held[n] = vs
	}
	base := UnpublishPackages()[0]
	if !held[base][in.Version] {
		return Unpublishing{Notice: fmt.Sprintf("%s has no version %s; nothing to unpublish", base, in.Version)}, nil
	}
	var only, missing []string
	u := Unpublishing{Exists: true}
	for _, n := range UnpublishPackages() {
		switch {
		case !held[n][in.Version]:
			missing = append(missing, n)
		case len(held[n]) == 1:
			only = append(only, fmt.Sprintf("%s@%s is the only version %s holds", n, in.Version, n))
		default:
			u.Commands = append(u.Commands, "npm unpublish "+shellQuote(n+"@"+in.Version))
		}
	}
	if len(only) > 0 {
		return Unpublishing{}, fmt.Errorf("refusing to unpublish anything, since unpublishing a package's last version deletes the package: %s", strings.Join(only, "; "))
	}
	if len(missing) > 0 {
		u.Notice = fmt.Sprintf("%s has no version %s; left out", strings.Join(missing, ", "), in.Version)
	}
	return u, nil
}
