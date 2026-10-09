// Package ghrelease spells the URLs of an engine release served from a
// GitHub repository's releases, the source a member's engine.releases
// names in place of npm: tag v<version>, its assets named exactly as npm's
// tarballs, and release.json on the latest release naming the newest
// build. The launcher spells the same tarball URLs.
package ghrelease

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// DefaultHost is where releases are read from.
const DefaultHost = "https://github.com"

// LatestFile is the asset on every release naming that release, read at
// the repository's latest release.
const LatestFile = "release.json"

// Host is CLAUDINITE_GITHUB (default DefaultHost), the launcher's override.
func Host() string {
	if h := os.Getenv("CLAUDINITE_GITHUB"); h != "" {
		return strings.TrimRight(h, "/")
	}
	return DefaultHost
}

func host(h string) string {
	if h == "" {
		return DefaultHost
	}
	return h
}

// name is a package's unscoped name, which its tarballs are named after.
func name(pkg string) string { return strings.TrimPrefix(pkg, "@claudinite/") }

// TarballURL is pkg's manifest tarball, named as npm names it, on repo's
// release v<version>.
func TarballURL(h, repo, pkg, version string) string {
	return fmt.Sprintf("%s/%s/releases/download/v%s/%s-%s.tgz", host(h), repo, version, name(pkg), version)
}

// PlatformTarballURL is pkg's platform tarball on repo's release
// v<version>.
func PlatformTarballURL(h, repo, pkg, platform, version string) string {
	return fmt.Sprintf("%s/%s/releases/download/v%s/%s-%s-%s.tgz", host(h), repo, version, name(pkg), platform, version)
}

// LatestURL is release.json on repo's latest release.
func LatestURL(h, repo string) string {
	return fmt.Sprintf("%s/%s/releases/latest/download/%s", host(h), repo, LatestFile)
}

// Latest is release.json: the release's version, its pin (the SHA-512
// integrity of its manifest.json) and the engine commit it was built from.
type Latest struct {
	Version  string `json:"version"`
	Manifest string `json:"manifest"`
	Commit   string `json:"commit"`
}

var (
	versionRe  = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	manifestRe = regexp.MustCompile(`^sha512-[A-Za-z0-9+/]{86}==$`)
	commitRe   = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// ParseLatest reads and validates release.json. It is a pointer, not a
// trust root: whatever it names is verified by its signature on download.
func ParseLatest(raw []byte) (Latest, error) {
	var l Latest
	if err := json.Unmarshal(raw, &l); err != nil {
		return Latest{}, fmt.Errorf("%s: %w", LatestFile, err)
	}
	if !versionRe.MatchString(l.Version) || !manifestRe.MatchString(l.Manifest) || !commitRe.MatchString(l.Commit) {
		return Latest{}, fmt.Errorf("%s must name a version, a sha512-... manifest and a 40-hex commit", LatestFile)
	}
	return l, nil
}

// FormatLatest is release.json's bytes.
func FormatLatest(l Latest) ([]byte, error) {
	if _, err := ParseLatest(mustJSON(l)); err != nil {
		return nil, err
	}
	return append(mustJSON(l), '\n'), nil
}

func mustJSON(l Latest) []byte {
	b, _ := json.Marshal(l)
	return b
}
