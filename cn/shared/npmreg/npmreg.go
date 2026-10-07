// Package npmreg reads the npm registry the way the launcher does: a
// package's packument, and tarballs by the exact URL shape the launcher
// builds, HTTPS only and under a size cap. It also owns the phase 2
// convention for release states carried in npm's deprecation message.
package npmreg

import (
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// DefaultRegistry is the public npm registry.
const DefaultRegistry = "https://registry.npmjs.org"

// DefaultMaxBytes is the launcher's download cap.
const DefaultMaxBytes = 64 << 20

// name is a package's unscoped name.
func name(pkg string) string { return strings.TrimPrefix(pkg, "@claudinite/") }

// TarballURL is the channel package's tarball, as the launcher spells it.
func TarballURL(registry, pkg, version string) string {
	return registry + "/" + pkg + "/-/" + name(pkg) + "-" + version + ".tgz"
}

// PlatformTarballURL is a platform package's tarball, as the launcher
// spells it.
func PlatformTarballURL(registry, pkg, platform, version string) string {
	return registry + "/" + pkg + "-" + platform + "/-/" + name(pkg) + "-" + platform + "-" + version + ".tgz"
}

// Kind is a release state read from a deprecation message.
type Kind string

const (
	Held       Kind = "held"
	Revoked    Kind = "revoked"
	Deprecated Kind = "deprecated"
)

// State is a version's deprecation, if any: Kind is empty for none.
type State struct {
	Kind   Kind
	Reason string
}

// ParseDeprecation reads npm's deprecated field: "held: <reason>" is a
// hold, "revoked: <reason>" a revocation, any other non-empty message a
// plain deprecation. It is how promote.yml marks held and revoked
// releases, and the only place an update reads them.
func ParseDeprecation(msg string) State {
	switch {
	case msg == "":
		return State{}
	case strings.HasPrefix(msg, "held:"):
		return State{Held, strings.TrimSpace(strings.TrimPrefix(msg, "held:"))}
	case strings.HasPrefix(msg, "revoked:"):
		return State{Revoked, strings.TrimSpace(strings.TrimPrefix(msg, "revoked:"))}
	}
	return State{Deprecated, msg}
}

// Version is one version's entry in a packument.
type Version struct {
	Version    string `json:"version"`
	Deprecated string `json:"deprecated"`
	Dist       struct {
		Tarball   string `json:"tarball"`
		Integrity string `json:"integrity"`
	} `json:"dist"`
}

// Packument is the registry's document for one package.
type Packument struct {
	Name     string             `json:"name"`
	DistTags map[string]string  `json:"dist-tags"`
	Versions map[string]Version `json:"versions"`
}

// Client reads one registry.
type Client struct {
	Registry string
	HTTP     *http.Client
	MaxBytes int64
}

// FromEnv is a client for CLAUDINITE_REGISTRY (default the public
// registry) under CLAUDINITE_MAX_DOWNLOAD_BYTES (default 64 MiB), the
// launcher's two overrides.
func FromEnv() (Client, error) {
	c := Client{Registry: DefaultRegistry, HTTP: &http.Client{Timeout: 5 * time.Minute}, MaxBytes: DefaultMaxBytes}
	if r := os.Getenv("CLAUDINITE_REGISTRY"); r != "" {
		c.Registry = strings.TrimRight(r, "/")
	}
	if m := os.Getenv("CLAUDINITE_MAX_DOWNLOAD_BYTES"); m != "" {
		n, err := strconv.ParseInt(m, 10, 64)
		if err != nil || n <= 0 {
			return Client{}, errors.New("CLAUDINITE_MAX_DOWNLOAD_BYTES must be a number")
		}
		c.MaxBytes = n
	}
	return c, nil
}

// NotServedError is a 404: for a tarball of a version the packument
// lists, npm's state in the minutes after a publish.
type NotServedError struct {
	URL    string
	Status string
}

func (e *NotServedError) Error() string { return e.URL + ": " + e.Status }

// ServeWait is how long after a publish npm may take to serve a version
// before its absence is an error, for every reader that waits on it: npm
// answers a publish 202 and can take minutes to list the version.
const ServeWait = 10 * time.Minute

// maxPackument bounds a packument, which grows with every version and is
// not under the tarball cap.
const maxPackument = 32 << 20

func (c Client) get(u string, max int64) ([]byte, error) {
	parsed, err := url.Parse(u)
	if err != nil || parsed.Scheme != "https" {
		return nil, fmt.Errorf("%s: only HTTPS is fetched", u)
	}
	resp, err := c.HTTP.Get(u)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.Request != nil && resp.Request.URL.Scheme != "https" {
		return nil, fmt.Errorf("%s: redirected off HTTPS", u)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, &NotServedError{URL: u, Status: resp.Status}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", u, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", u, err)
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("%s is larger than the %d-byte download cap", u, max)
	}
	return data, nil
}

// Packument reads pkg's packument.
func (c Client) Packument(pkg string) (*Packument, error) {
	raw, err := c.get(c.Registry+"/"+strings.Replace(pkg, "/", "%2f", 1), maxPackument)
	if err != nil {
		return nil, err
	}
	var p Packument
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("packument of %s: %w", pkg, err)
	}
	return &p, nil
}

// Download fetches one tarball by its exact URL.
func (c Client) Download(u string) ([]byte, error) {
	max := c.MaxBytes
	if max <= 0 {
		max = DefaultMaxBytes
	}
	return c.get(u, max)
}

// CheckIntegrity compares data's SHA-512 with an npm integrity string.
func CheckIntegrity(data []byte, integrity string) error {
	sum := sha512.Sum512(data)
	got := "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
	if got != integrity {
		return fmt.Errorf("SHA-512 %s does not match the registry's integrity %s", got, integrity)
	}
	return nil
}
