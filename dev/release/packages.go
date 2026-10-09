// Package release holds the release pipeline's decisions as data and pure
// functions, so the workflows and scripts read them rather than restate
// them: the npm packages and their trusted publishers, the release kinds,
// and the publish mode.
package release

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/version"
)

// CLI is the engine's one npm package: it carries the manifest and the
// launcher, and each platform's binary is CLI-<platform>. Its channels are
// npm dist-tags, all on the same versions.
const CLI = "@claudinite/cli"

// The dist-tags: a release publishes under TagRC or TagStaging, and
// promotion moves TagLatest onto an rc version it verified.
const (
	TagRC      = "rc"
	TagStaging = "staging"
	TagLatest  = "latest"
)

// Package is one npm package the pipeline publishes, with the (workflow,
// environment) pairs npmjs.com trusts to publish it.
type Package struct {
	Name       string
	Publishers []Publisher
}

// Publisher is one trusted publisher; DistTag also grants it npm dist-tag,
// which promotion needs and publishing does not.
type Publisher struct {
	Workflow    string
	Environment string
	DistTag     bool
}

var packageName = regexp.MustCompile(`^@claudinite/(cli|sdk)(-(linux|darwin|windows)-(x64|arm64))?$`)

// ValidPackageName reports whether name is one the pipeline may publish.
func ValidPackageName(name string) bool {
	return packageName.MatchString(name) && !strings.HasPrefix(name, "@claudinite/sdk-") && name != "@claudinite/cli-windows-arm64"
}

// CLIPackages are the six packages every release publishes: CLI, then its
// platform packages in manifest order.
func CLIPackages() []string {
	names := []string{CLI}
	for _, p := range version.Platforms {
		names = append(names, CLI+"-"+p)
	}
	return names
}

// ReleaseKind is what one release.yml dispatch builds, the dist-tag it
// publishes under, and the engine channel (settings' engine.channel) whose
// members take that tag.
type ReleaseKind struct {
	Tag       string
	Channel   string
	Platforms []string
}

// ReleaseKindOf maps release.yml's kind input: full is a release candidate
// on all five platforms, which canaries take from rc; staging is a quick
// linux-x64 build only the owner's opted-in repos take.
func ReleaseKindOf(kind string) (ReleaseKind, error) {
	switch kind {
	case "full":
		return ReleaseKind{Tag: TagRC, Channel: "canary", Platforms: version.Platforms}, nil
	case "staging":
		return ReleaseKind{Tag: TagStaging, Channel: "staging", Platforms: []string{"linux-x64"}}, nil
	}
	return ReleaseKind{}, fmt.Errorf("kind %q is neither full nor staging", kind)
}

// Packages are the @claudinite npm packages and the trusted publishers of
// each: the CLI packages and the SDK.
func Packages() []Package {
	release := Publisher{Workflow: "release.yml", Environment: "release"}
	var out []Package
	for _, n := range CLIPackages() {
		p := Package{Name: n, Publishers: []Publisher{release}}
		if n == CLI {
			p.Publishers = append(p.Publishers, Publisher{Workflow: "promote.yml", Environment: "promote", DistTag: true})
		}
		out = append(out, p)
	}
	return append(out, Package{Name: "@claudinite/sdk", Publishers: []Publisher{{Workflow: "promote.yml", Environment: "promote"}}})
}
