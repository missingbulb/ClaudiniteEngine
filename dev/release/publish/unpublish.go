package publish

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/dev/release"
)

// UnpublishInput is an unpublish dispatch of promote.yml and what npm says:
// each CLI package's `npm view <pkg> versions --json`, keyed by package
// name, and `npm view @claudinite/cli dist-tags --json`.
type UnpublishInput struct {
	Version  string
	Versions map[string]string
	DistTags string
}

// Unpublishing is what the unpublish job does: the npm unpublish commands
// for every CLI package holding the version, or, when @claudinite/cli has
// no such version, a notice and nothing.
type Unpublishing struct {
	Exists   bool
	Commands []string
	Notice   string
}

// npmVersion is the shape of an npm version string: no format this engine
// reads is required, since a version in a retired format is one an
// unpublish exists to remove.
var npmVersion = regexp.MustCompile(`^[0-9A-Za-z.+-]+$`)

// UnpublishCommands renders the removal of a version from npm. It refuses,
// writing no command, when the version is the only one a package holds,
// since npm deletes a package whose last version is unpublished; when it
// is the version latest points at, which every member on the stable
// channel is offered; and when npm's dist-tags cannot be read.
func UnpublishCommands(in UnpublishInput) (Unpublishing, error) {
	if !npmVersion.MatchString(in.Version) {
		return Unpublishing{}, fmt.Errorf("version %q is not an npm version: want letters, digits, '.', '+' and '-'", in.Version)
	}
	held := map[string]map[string]bool{}
	for _, n := range release.CLIPackages() {
		vs, err := npmVersions(in.Versions[n])
		if err != nil {
			return Unpublishing{}, fmt.Errorf("%s: %w", n, err)
		}
		held[n] = vs
	}
	if !held[release.CLI][in.Version] {
		return Unpublishing{Notice: fmt.Sprintf("%s has no version %s; nothing to unpublish", release.CLI, in.Version)}, nil
	}
	var tags map[string]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(in.DistTags)), &tags); err != nil {
		return Unpublishing{}, fmt.Errorf("%s's dist-tags are unreadable, so whether %s is latest is unknown: %.80s", release.CLI, in.Version, in.DistTags)
	}
	if tags[release.TagLatest] == in.Version {
		return Unpublishing{}, fmt.Errorf("refusing to unpublish %s, the version %s's latest tag points at: promote another version first", in.Version, release.CLI)
	}
	var only, missing []string
	u := Unpublishing{Exists: true}
	for _, n := range release.CLIPackages() {
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
