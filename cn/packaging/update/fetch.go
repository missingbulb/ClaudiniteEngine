package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/paths"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/ghrelease"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/npmreg"
	"github.com/missingbulb/ClaudiniteEngine/cn/packaging/sign"
)

// FetchInput is what Fetch needs; cn supplies the embedded roots.
type FetchInput struct {
	Registry  npmreg.Client
	Package   string
	Version   string
	Packument *npmreg.Packument
	// Releases, when set, is the GitHub repository whose releases serve
	// the version in place of npm, read from ReleasesHost; no packument is
	// read, and the manifest's signature alone vouches for the download.
	Releases, ReleasesHost string
	Roots                  []ed25519.PublicKey
	// CacheRoot is the launcher's cache folder, .../claudinite.
	CacheRoot string
	Platform  string
	Now       time.Time
}

// Fetched is a verified release placed in the cache.
type Fetched struct {
	Version  string
	Manifest []byte
	// Integrity is the manifest's npm integrity string: the new pin.
	Integrity string
	// KeyID is the release key that signed the manifest.
	KeyID  string
	Binary string
	// Launcher is the release's launcher, package/launch in the channel
	// tarball, or nil for a release that carries none; SignedLauncher is
	// whether the signed manifest hashes it.
	Launcher       []byte
	SignedLauncher bool
}

func binaryName(platform string) string {
	if strings.HasPrefix(platform, "windows-") {
		return "cn.exe"
	}
	return "cn"
}

type manifest struct {
	Version  string `json:"version"`
	Launcher string `json:"launcher"`
	Binaries map[string]struct {
		File   string `json:"file"`
		SHA256 string `json:"sha256"`
		Size   int64  `json:"size"`
	} `json:"binaries"`
}

// Fetch downloads a version's manifest, signature and this platform's
// binary by the launcher's tarball URLs, checks the channel tarball against
// the packument's integrity when it comes from npm, the signature against the roots and the binary
// against its manifest entry, and only then places manifest.json (0444) and
// the binary (0555) in <cache>/<version>/ beside manifest.sig.json, where the launcher looks, so a
// session on the merged pin downloads nothing. A refusal places nothing and
// names the check that failed.
func Fetch(in FetchInput) (Fetched, error) {
	channelURL := npmreg.TarballURL(in.Registry.Registry, in.Package, in.Version)
	binURL := npmreg.PlatformTarballURL(in.Registry.Registry, in.Package, in.Platform, in.Version)
	integrity := ""
	if in.Releases != "" {
		channelURL = ghrelease.TarballURL(in.ReleasesHost, in.Releases, in.Package, in.Version)
		binURL = ghrelease.PlatformTarballURL(in.ReleasesHost, in.Releases, in.Package, in.Platform, in.Version)
	} else {
		v, ok := in.Packument.Versions[in.Version]
		if !ok {
			return Fetched{}, fmt.Errorf("%s has no %s", in.Package, in.Version)
		}
		integrity = v.Dist.Integrity
	}
	channel, err := in.Registry.Download(channelURL)
	if err != nil {
		return Fetched{}, fmt.Errorf("download: %w", err)
	}
	if in.Releases == "" {
		if err := npmreg.CheckIntegrity(channel, integrity); err != nil {
			return Fetched{}, fmt.Errorf("tarball integrity: %s %s: %w", in.Package, in.Version, err)
		}
	}
	rawManifest, err := tarFile(channel, "package/manifest.json")
	if err != nil {
		return Fetched{}, fmt.Errorf("%s: %w", channelURL, err)
	}
	rawSig, err := tarFile(channel, "package/manifest.sig.json")
	if err != nil {
		return Fetched{}, fmt.Errorf("manifest signature: %s: %w", channelURL, err)
	}
	var signed sign.SignedManifest
	if err := json.Unmarshal(rawSig, &signed); err != nil {
		return Fetched{}, fmt.Errorf("manifest signature: manifest.sig.json: %w", err)
	}
	body, err := sign.VerifyManifest(signed, rawManifest, in.Roots, in.Now)
	if err != nil {
		return Fetched{}, fmt.Errorf("manifest signature: %s %s: %w", in.Package, in.Version, err)
	}
	var m manifest
	if err := json.Unmarshal(rawManifest, &m); err != nil {
		return Fetched{}, fmt.Errorf("manifest.json: %w", err)
	}
	if m.Version != in.Version {
		return Fetched{}, fmt.Errorf("manifest.json is for %s, not %s", m.Version, in.Version)
	}
	entry, ok := m.Binaries[in.Platform]
	if !ok || entry.File != binaryName(in.Platform) {
		return Fetched{}, fmt.Errorf("%s %s lists no binary for %s", in.Package, in.Version, in.Platform)
	}
	platformTgz, err := in.Registry.Download(binURL)
	if err != nil {
		return Fetched{}, fmt.Errorf("download: %w", err)
	}
	bin, err := tarFile(platformTgz, "package/bin/"+entry.File)
	if err != nil {
		return Fetched{}, fmt.Errorf("%s: %w", binURL, err)
	}
	sum := sha256.Sum256(bin)
	if hex.EncodeToString(sum[:]) != entry.SHA256 || int64(len(bin)) != entry.Size {
		return Fetched{}, fmt.Errorf("binary hash: the %s binary of %s %s does not match its manifest entry", in.Platform, in.Package, in.Version)
	}
	// A launcher the signed manifest hashes is the release's own. One it
	// does not is vouched for only by npm's integrity, which cn adopt may
	// take and an update may not; from a releases repository it is not
	// vouched for at all.
	launcher, _ := tarFile(channel, "package/launch")
	signedLauncher := false
	if m.Launcher != "" {
		ls := sha256.Sum256(launcher)
		if hex.EncodeToString(ls[:]) != m.Launcher {
			return Fetched{}, fmt.Errorf("launcher hash: the launcher of %s %s does not match its manifest", in.Package, in.Version)
		}
		signedLauncher = true
	} else if in.Releases != "" {
		launcher = nil
	}
	dir := filepath.Join(in.CacheRoot, in.Version)
	if err := paths.EnsurePrivateDir(in.CacheRoot); err != nil {
		return Fetched{}, fmt.Errorf("cache: %w", err)
	}
	if err := paths.EnsurePrivateDir(dir); err != nil {
		return Fetched{}, fmt.Errorf("cache: %w", err)
	}
	if err := paths.PlaceReadOnly(dir, entry.File, bin, 0o555); err != nil {
		return Fetched{}, fmt.Errorf("cache: %w", err)
	}
	if err := paths.PlaceReadOnly(dir, "manifest.json", rawManifest, 0o444); err != nil {
		return Fetched{}, fmt.Errorf("cache: %w", err)
	}
	if err := paths.PlaceReadOnly(dir, "manifest.sig.json", rawSig, 0o444); err != nil {
		return Fetched{}, fmt.Errorf("cache: %w", err)
	}
	ms := sha512.Sum512(rawManifest)
	return Fetched{
		Version:        in.Version,
		Manifest:       rawManifest,
		Integrity:      "sha512-" + base64.StdEncoding.EncodeToString(ms[:]),
		KeyID:          body.KeyID,
		Binary:         filepath.Join(dir, entry.File),
		Launcher:       launcher,
		SignedLauncher: signedLauncher,
	}, nil
}

// maxTarFile bounds one extracted file.
const maxTarFile = 128 << 20

func tarFile(tgz []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("holds no %s", name)
		}
		if err != nil {
			return nil, err
		}
		if h.Name == name && h.Typeflag == tar.TypeReg {
			data, err := io.ReadAll(io.LimitReader(tr, maxTarFile+1))
			if err != nil {
				return nil, err
			}
			if len(data) > maxTarFile {
				return nil, fmt.Errorf("%s is too large", name)
			}
			return data, nil
		}
	}
}
