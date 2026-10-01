// Command manifest writes a release's manifest.json, signs it into
// manifest.sig.json, verifies a signed manifest against the binaries on
// disk, writes the SHA256SUMS the release jobs hand each other, and prints
// a file's npm integrity string.
package main

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/release/releasefiles"
	"github.com/missingbulb/ClaudiniteEngine/shared/sign"
	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

const usage = `usage:
  manifest write --dist DIR --version V --commit SHA --source REPO
  manifest sign --dist DIR --key RELEASE.key --cert RELEASE.cert.json
  manifest verify --dist DIR --roots DIR
  manifest sums --dist DIR
  manifest integrity FILE
`

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dist := fs.String("dist", "", "")
	ver := fs.String("version", "", "")
	commit := fs.String("commit", "", "")
	key := fs.String("key", "", "")
	cert := fs.String("cert", "", "")
	roots := fs.String("roots", "", "")
	source := fs.String("source", "", "")
	if err := fs.Parse(args[1:]); err != nil {
		fmt.Fprintf(stderr, "manifest: %v\n%s", err, usage)
		return 2
	}
	var err error
	switch args[0] {
	case "write":
		err = write(*dist, *ver, *commit, *source)
	case "sign":
		err = signManifest(*dist, *key, *cert)
	case "verify":
		err = verify(*dist, *roots, stdout)
	case "sums":
		err = writeSums(*dist)
	case "integrity":
		if fs.NArg() != 1 {
			err = errors.New("integrity takes one file")
			break
		}
		var raw []byte
		if raw, err = os.ReadFile(fs.Arg(0)); err == nil {
			fmt.Fprintln(stdout, releasefiles.Integrity(raw))
		}
	default:
		fmt.Fprintf(stderr, "manifest: unknown command %q\n%s", args[0], usage)
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "manifest %s: %v\n", args[0], err)
		return 1
	}
	return 0
}

func builtAt() string {
	if s := os.Getenv("SOURCE_DATE_EPOCH"); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return time.Unix(n, 0).UTC().Format(time.RFC3339)
		}
	}
	return time.Now().UTC().Format(time.RFC3339)
}

func write(dist, ver, commit, source string) error {
	if dist == "" || commit == "" || source == "" {
		return errors.New("write needs --dist, --version, --commit and --source")
	}
	if _, err := version.Parse(ver); err != nil {
		return err
	}
	bins, err := releasefiles.ScanBinaries(dist)
	if err != nil {
		return err
	}
	digest, err := releasefiles.UpdaterDigest(source)
	if err != nil {
		return err
	}
	m := releasefiles.Manifest{V: 1, Version: ver, BuiltAt: builtAt(), Commit: commit, GoVersion: runtime.Version(), UpdaterDigest: digest, Binaries: bins}
	return os.WriteFile(filepath.Join(dist, "manifest.json"), releasefiles.Format(m), 0o644)
}

func signManifest(dist, keyPath, certPath string) error {
	if dist == "" || keyPath == "" || certPath == "" {
		return errors.New("sign needs --dist, --key and --cert")
	}
	rawKey, err := os.ReadFile(keyPath)
	if err != nil {
		return err
	}
	key, err := sign.ParsePrivateKey(string(rawKey))
	if err != nil {
		return fmt.Errorf("%s: %w", keyPath, err)
	}
	rawCert, err := os.ReadFile(certPath)
	if err != nil {
		return err
	}
	var cert sign.Certificate
	if err := json.Unmarshal(rawCert, &cert); err != nil {
		return fmt.Errorf("%s: %w", certPath, err)
	}
	if err := checkExpiry(cert, time.Now()); err != nil {
		return fmt.Errorf("%s: %w", certPath, err)
	}
	manifest, err := os.ReadFile(filepath.Join(dist, "manifest.json"))
	if err != nil {
		return err
	}
	out, err := json.MarshalIndent(sign.SignManifest(key, cert, manifest), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dist, "manifest.sig.json"), append(out, '\n'), 0o644)
}

func verify(dist, rootsDir string, stdout io.Writer) error {
	if dist == "" || rootsDir == "" {
		return errors.New("verify needs --dist and --roots")
	}
	names, err := filepath.Glob(filepath.Join(rootsDir, "*.pub"))
	if err != nil {
		return err
	}
	var roots []ed25519.PublicKey
	for _, n := range names {
		raw, err := os.ReadFile(n)
		if err != nil {
			return err
		}
		p, err := sign.ParsePublicKey(string(raw))
		if err != nil {
			return fmt.Errorf("%s: %w", n, err)
		}
		roots = append(roots, p)
	}
	manifest, err := os.ReadFile(filepath.Join(dist, "manifest.json"))
	if err != nil {
		return err
	}
	rawSig, err := os.ReadFile(filepath.Join(dist, "manifest.sig.json"))
	if err != nil {
		return err
	}
	var signed sign.SignedManifest
	if err := json.Unmarshal(rawSig, &signed); err != nil {
		return fmt.Errorf("manifest.sig.json: %w", err)
	}
	body, err := sign.VerifyManifest(signed, manifest, roots, time.Now())
	if err != nil {
		return err
	}
	m, err := releasefiles.ParseManifest(manifest)
	if err != nil {
		return err
	}
	if len(m.Binaries) != len(version.Platforms) {
		return fmt.Errorf("manifest lists %d binaries, want %d", len(m.Binaries), len(version.Platforms))
	}
	for _, p := range version.Platforms {
		e, ok := m.Binaries[p]
		if !ok {
			return fmt.Errorf("manifest lacks %s", p)
		}
		sum, size, err := releasefiles.HashFile(filepath.Join(dist, "bin", p, e.File))
		if err != nil {
			return err
		}
		if sum != e.SHA256 || size != e.Size {
			return fmt.Errorf("%s: binary does not match its manifest entry", p)
		}
	}
	fmt.Fprintf(stdout, "verified %s: signed by release key %s, certified by root %s\n", m.Version, body.KeyID, body.Issuer)
	return nil
}

// renewMargin is how long before its certificate expires a release key
// stops signing, so a rotation due turns a release red early, not on the day.
const renewMargin = 14 * 24 * time.Hour

func checkExpiry(cert sign.Certificate, now time.Time) error {
	raw, err := sign.DecodeB64(cert.Payload)
	if err != nil {
		return fmt.Errorf("certificate payload: %w", err)
	}
	var body sign.Body
	if err := json.Unmarshal(raw, &body); err != nil {
		return fmt.Errorf("certificate body: %w", err)
	}
	notAfter, err := time.Parse(time.RFC3339, body.NotAfter)
	if err != nil {
		return fmt.Errorf("certificate notAfter: %w", err)
	}
	if notAfter.Sub(now) < renewMargin {
		return fmt.Errorf("the release key's certificate expires %s, less than 14 days from now; certify a new one (cmd/cn-keys/README.md, \"Rotating the release key\")", body.NotAfter)
	}
	return nil
}

// writeSums writes DIR/SHA256SUMS in sha256sum format over every file under
// bin/, npm/ and tarballs/ and the top-level manifest files, each path
// starting with DIR's own name, so `sha256sum -c` runs from DIR's parent.
func writeSums(dist string) error {
	if dist == "" {
		return errors.New("sums needs --dist")
	}
	base := filepath.Base(filepath.Clean(dist))
	var files []string
	for _, sub := range []string{"bin", "npm", "tarballs"} {
		err := filepath.WalkDir(filepath.Join(dist, sub), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type().IsRegular() {
				rel, err := filepath.Rel(dist, p)
				if err != nil {
					return err
				}
				files = append(files, filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	for _, f := range []string{"manifest.json", "manifest.sig.json"} {
		if _, err := os.Stat(filepath.Join(dist, f)); err == nil {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	var b strings.Builder
	for _, f := range files {
		sum, _, err := releasefiles.HashFile(filepath.Join(dist, filepath.FromSlash(f)))
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s  %s/%s\n", sum, base, f)
	}
	return os.WriteFile(filepath.Join(dist, "SHA256SUMS"), []byte(b.String()), 0o644)
}
