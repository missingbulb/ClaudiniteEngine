package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/sign"
	"github.com/missingbulb/ClaudiniteEngine/cn/shared/version"
	"github.com/missingbulb/ClaudiniteEngine/dev/release/releasefiles"
)

// Check verifies the downloaded @claudinite/cli tarballs of ver in dir
// before latest moves onto them: the manifest's signature against the
// roots in rootsDir, all five platforms listed and every binary against
// its manifest entry, the candidate checkout's head against the commit the
// manifest names, and stableTest, the candidate's `go test -tags stable
// ./shared/trust`. rootsDir is the dispatching checkout's, never the
// candidate's, so the candidate cannot bring its own trust. It returns
// pass or refuse, why, and the stable test's own verdict (pass, fail, or
// not-run when the bytes were refused first).
func Check(dir, ver, rootsDir, head string, stableTest func() error) (string, string, string) {
	if reason := checkBytes(dir, ver, rootsDir, head); reason != "" {
		return "refuse", reason, "not-run"
	}
	if err := stableTest(); err != nil {
		return "refuse", "go test -tags stable ./shared/trust does not pass at this commit: latest never points at a build embedding the development roots", "fail"
	}
	return "pass", fmt.Sprintf("%s: signature, five binaries and the stable build check verified", ver), "pass"
}

func checkBytes(dir, ver, rootsDir, head string) string {
	channel := filepath.Join(dir, "cli-"+ver+".tgz")
	manifest, err := tarFile(channel, "package/manifest.json")
	if err != nil {
		return err.Error()
	}
	rawSig, err := tarFile(channel, "package/manifest.sig.json")
	if err != nil {
		return err.Error()
	}
	roots, err := readRoots(rootsDir)
	if err != nil {
		return err.Error()
	}
	var signed sign.SignedManifest
	if err := json.Unmarshal(rawSig, &signed); err != nil {
		return "manifest.sig.json: " + err.Error()
	}
	if _, err := sign.VerifyManifest(signed, manifest, roots, time.Now()); err != nil {
		return "the manifest signature does not verify: " + err.Error()
	}
	m, err := releasefiles.ParseManifest(manifest)
	if err != nil {
		return err.Error()
	}
	if m.Version != ver {
		return fmt.Sprintf("the manifest is for %s, not %s", m.Version, ver)
	}
	var lacks []string
	for _, p := range version.Platforms {
		if _, ok := m.Binaries[p]; !ok {
			lacks = append(lacks, p)
		}
	}
	if len(lacks) > 0 {
		return fmt.Sprintf("%s is a staging build: its manifest lists no %s binary, and only a full release, built for all five platforms, is promoted", ver, strings.Join(lacks, ", "))
	}
	if len(m.Commit) < 7 || !strings.HasPrefix(head, m.Commit) {
		return fmt.Sprintf("the candidate checkout is at %s, but the manifest was built from %s; the v%s tag moved", head, m.Commit, ver)
	}
	for _, p := range version.Platforms {
		e := m.Binaries[p]
		bin, err := tarFile(filepath.Join(dir, "cli-"+p+"-"+ver+".tgz"), "package/bin/"+e.File)
		if err != nil {
			return p + ": " + err.Error()
		}
		sum := sha256.Sum256(bin)
		if hex.EncodeToString(sum[:]) != e.SHA256 || int64(len(bin)) != e.Size {
			return "the " + p + " binary does not match its manifest entry"
		}
	}
	return ""
}

func readRoots(dir string) ([]ed25519.PublicKey, error) {
	names, err := filepath.Glob(filepath.Join(dir, "*.pub"))
	if err != nil {
		return nil, err
	}
	var roots []ed25519.PublicKey
	for _, n := range names {
		raw, err := os.ReadFile(n)
		if err != nil {
			return nil, err
		}
		p, err := sign.ParsePublicKey(string(raw))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		roots = append(roots, p)
	}
	return roots, nil
}

// maxTarFile bounds one extracted file, above the launcher's 64 MiB cap.
const maxTarFile = 128 << 20

func tarFile(tgz, name string) ([]byte, error) {
	f, err := os.Open(tgz)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return tarFileFrom(f, tgz, name)
}

func tarFileFrom(r io.Reader, label, name string) ([]byte, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("%s holds no %s", label, name)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", label, err)
		}
		if h.Name == name && h.Typeflag == tar.TypeReg {
			data, err := io.ReadAll(io.LimitReader(tr, maxTarFile+1))
			if err != nil {
				return nil, err
			}
			if len(data) > maxTarFile {
				return nil, fmt.Errorf("%s: %s is too large", label, name)
			}
			return data, nil
		}
	}
}
