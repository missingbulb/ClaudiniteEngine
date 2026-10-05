// Package release holds the release pipeline's decisions as data and pure
// functions, so the workflows and scripts read them rather than restate
// them: the npm packages and their trusted publishers, the release kinds,
// the publish mode, and the text the pipeline posts.
package release

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/shared/version"
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

// Placeholders are the 7 packages npm-bootstrap.yml reserves at 0.0.0: the
// CLI packages and the SDK.
func Placeholders() []Package {
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

// BootstrapComment is the handover npm-bootstrap.yml posts on #2 once the
// placeholders exist: what Ariel sets on npmjs.com, which has no CLI or API
// for trusted publishers.
func BootstrapComment() string {
	var b strings.Builder
	b.WriteString("The 7 `@claudinite` packages are reserved at `0.0.0`. Each needs its trusted publishers and token refusal set on npmjs.com:\n\n")
	for _, p := range Placeholders() {
		for _, pub := range p.Publishers {
			fmt.Fprintf(&b, "- [ ] `%s` trusted publisher: GitHub Actions, organization or user `missingbulb`, repository `ClaudiniteEngine`, workflow filename `%s`, environment `%s`", p.Name, pub.Workflow, pub.Environment)
			if pub.DistTag {
				b.WriteString(", Allow npm dist-tag")
			}
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "- [ ] `%s` Publishing access: Require two-factor authentication and disallow tokens\n", p.Name)
	}
	b.WriteString("- [ ] delete `NPM_BOOTSTRAP_TOKEN` from Actions secrets and revoke it on npm\n\n")
	b.WriteString("Every box of a package is on its page at `https://www.npmjs.com/package/<name>/access`. " +
		"Until a package's publisher is attached, a real `release.yml` publish or `promote.yml` promotion of it is a red run whose last line names its box here; " +
		"before the placeholders existed those runs were dry runs. npm has no CLI or API for trusted publishers (https://docs.npmjs.com/trusted-publishers), so these are clicks.\n")
	return b.String()
}
