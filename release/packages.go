// Package release holds the release pipeline's decisions as data and pure
// functions, so the workflows and scripts read them rather than restate
// them: the npm packages and their trusted publishers, the publish mode,
// and the text the pipeline posts.
package release

import (
	"fmt"
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

// Placeholders are the 13 packages npm-bootstrap.yml reserves at 0.0.0:
// each channel's manifest package and five platform packages, and the SDK.
func Placeholders() []Package {
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

// BootstrapComment is the handover npm-bootstrap.yml posts on #2 once the
// placeholders exist: what Ariel sets on npmjs.com, which has no CLI or API
// for trusted publishers.
func BootstrapComment() string {
	var b strings.Builder
	b.WriteString("The 13 `@claudinite` packages are reserved at `0.0.0`. Each needs its trusted publisher and token refusal set on npmjs.com:\n\n")
	for _, p := range Placeholders() {
		fmt.Fprintf(&b, "- [ ] `%s` trusted publisher: GitHub Actions, organization or user `missingbulb`, repository `ClaudiniteEngine`, workflow filename `%s`, environment `%s`\n", p.Name, p.Workflow, p.Environment)
		fmt.Fprintf(&b, "- [ ] `%s` Publishing access: Require two-factor authentication and disallow tokens\n", p.Name)
	}
	b.WriteString("- [ ] delete `NPM_BOOTSTRAP_TOKEN` from Actions secrets and revoke it on npm\n\n")
	b.WriteString("Both boxes of a package are on its page at `https://www.npmjs.com/package/<name>/access`. " +
		"Until a package's publisher is attached, a real `release.yml` or `promote.yml` publish of it is a red run whose last line names its box here; " +
		"before the placeholders existed those runs were dry runs. npm has no CLI or API for trusted publishers (https://docs.npmjs.com/trusted-publishers), so these are clicks.\n")
	return b.String()
}
