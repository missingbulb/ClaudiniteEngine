// Package releasefiles is the release's file formats: manifest.json, the
// npm integrity string the member's pin carries, and npm-shaped tarballs.
//
// manifest.json keeps each binary entry on one line, in a fixed order, so
// the sh launcher can read its own platform's entry with a line pattern
// once the file's hash has matched the pin.
package releasefiles

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/missingbulb/ClaudiniteEngine/shared/version"
)

// Binary is one platform's entry.
type Binary struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Manifest is manifest.json.
type Manifest struct {
	V         int    `json:"v"`
	Version   string `json:"version"`
	BuiltAt   string `json:"builtAt"`
	Commit    string `json:"commit"`
	GoVersion string `json:"goVersion"`
	// UpdaterDigest is UpdaterDigest of the source the release was built
	// from: equal digests mean equal update paths, so a hop one release
	// completed proves the other's.
	UpdaterDigest string                     `json:"updaterDigest"`
	Binaries      map[string]Binary          `json:"binaries"`
	TestedPacks   map[string]json.RawMessage `json:"testedPacks"`
}

// BinaryName is the binary's file name on a platform.
func BinaryName(platform string) string {
	if strings.HasPrefix(platform, "windows-") {
		return "cn.exe"
	}
	return "cn"
}

func str(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// Format renders m in the fixed layout: known platforms in release order,
// then any others sorted, one binary per line.
func Format(m Manifest) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "{\n  \"v\": %d,\n  \"version\": %s,\n  \"builtAt\": %s,\n  \"commit\": %s,\n  \"goVersion\": %s,\n  \"updaterDigest\": %s,\n  \"binaries\": {\n",
		m.V, str(m.Version), str(m.BuiltAt), str(m.Commit), str(m.GoVersion), str(m.UpdaterDigest))
	var order []string
	seen := map[string]bool{}
	for _, p := range version.Platforms {
		if _, ok := m.Binaries[p]; ok {
			order = append(order, p)
			seen[p] = true
		}
	}
	var rest []string
	for p := range m.Binaries {
		if !seen[p] {
			rest = append(rest, p)
		}
	}
	sort.Strings(rest)
	order = append(order, rest...)
	for i, p := range order {
		e := m.Binaries[p]
		sep := ","
		if i == len(order)-1 {
			sep = ""
		}
		fmt.Fprintf(&b, "    %s: {\"file\": %s, \"sha256\": %s, \"size\": %d}%s\n", str(p), str(e.File), str(e.SHA256), e.Size, sep)
	}
	b.WriteString("  },\n  \"testedPacks\": {}\n}\n")
	return []byte(b.String())
}

// ParseManifest reads manifest.json.
func ParseManifest(raw []byte) (Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return m, fmt.Errorf("manifest.json: %w", err)
	}
	if m.V != 1 {
		return m, fmt.Errorf("manifest.json: version %d is not supported", m.V)
	}
	return m, nil
}

// HashFile returns a file's SHA-256 in hex and its size.
func HashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// ScanBinaries reads dist/bin/<platform>/<binary> for all five platforms.
func ScanBinaries(dist string) (map[string]Binary, error) {
	out := map[string]Binary{}
	for _, p := range version.Platforms {
		name := BinaryName(p)
		sum, size, err := HashFile(filepath.Join(dist, "bin", p, name))
		if err != nil {
			return nil, fmt.Errorf("platform %s: %w", p, err)
		}
		out[p] = Binary{File: name, SHA256: sum, Size: size}
	}
	return out, nil
}

// UpdaterSource is the source of every path that moves a member from one
// engine to the next: the launcher today, and lifecycle/, where phase 2's
// updater lives.
var UpdaterSource = []string{"launcher/launch", "lifecycle"}

// UpdaterDigest is the SHA-256, in hex, over every regular file of
// UpdaterSource under root, in sorted path order, each as its slash path,
// a NUL, its size in decimal, a NUL and its content.
func UpdaterDigest(root string) (string, error) {
	var files []string
	for _, src := range UpdaterSource {
		err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(src)), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type().IsRegular() {
				rel, err := filepath.Rel(root, p)
				if err != nil {
					return err
				}
				files = append(files, filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("updater source: %w", err)
		}
	}
	sort.Strings(files)
	h := sha256.New()
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", f, len(raw))
		h.Write(raw)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Integrity is the npm-style SHA-512 string the member's pin carries.
func Integrity(raw []byte) string {
	sum := sha512.Sum512(raw)
	return "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
}

// TarFile is one entry of an npm-shaped tarball.
type TarFile struct {
	Name string
	Mode int64
	Data []byte
}

// WriteTarball writes a gzipped tar with every entry under package/, as npm
// serves a package tarball.
func WriteTarball(path string, files []TarFile) error {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	mtime := time.Date(1985, 10, 26, 8, 15, 0, 0, time.UTC)
	for _, f := range files {
		if err := tw.WriteHeader(&tar.Header{Name: "package/" + f.Name, Mode: f.Mode, Size: int64(len(f.Data)), ModTime: mtime, Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		if _, err := tw.Write(f.Data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
