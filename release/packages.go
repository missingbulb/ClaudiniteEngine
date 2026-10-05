// Package release holds the release pipeline's decisions as data and pure
// functions, so the workflows and scripts read them rather than restate
// them: the npm packages and their trusted publishers, the publish mode,
// and the text the pipeline posts.
package release

import (
	"regexp"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

// Package is one npm package the pipeline publishes, with the one
// (workflow, environment) pair npmjs.com trusts to publish it.
type Package struct {
	Name        string
	Channel     string // "rc" or "stable"
	Workflow    string
	Environment string
}

var packageName = regexp.MustCompile(`^@claudinite/(cli|cli-rc|sdk)(-(linux|darwin|windows)-(x64|arm64))?$`)

// ValidPackageName reports whether name is one the pipeline may publish.
func ValidPackageName(name string) bool {
	return packageName.MatchString(name) && !strings.HasPrefix(name, "@claudinite/sdk-") && name != "@claudinite/cli-windows-arm64" && name != "@claudinite/cli-rc-windows-arm64"
}

// ChannelOf is "rc" for @claudinite/cli-rc and its platform packages, else
// "stable".
func ChannelOf(name string) string {
	if name == "@claudinite/cli-rc" || strings.HasPrefix(name, "@claudinite/cli-rc-") {
		return "rc"
	}
	return "stable"
}

// Packages are the 13 @claudinite npm packages and the trusted publisher of
// each: each channel's manifest package and five platform packages, and the
// SDK.
func Packages() []Package {
	var out []Package
	for _, base := range []string{"@claudinite/cli-rc", "@claudinite/cli"} {
		names := []string{base}
		for _, p := range version.Platforms {
			names = append(names, base+"-"+p)
		}
		if base == "@claudinite/cli" {
			names = append(names, "@claudinite/sdk")
		}
		for _, n := range names {
			out = append(out, publisherOf(n))
		}
	}
	return out
}

func publisherOf(name string) Package {
	if ChannelOf(name) == "rc" {
		return Package{Name: name, Channel: "rc", Workflow: "release.yml", Environment: "release"}
	}
	return Package{Name: name, Channel: "stable", Workflow: "promote.yml", Environment: "promote"}
}
