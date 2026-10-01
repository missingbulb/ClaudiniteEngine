package packs

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/missingbulb/ClaudiniteEngine/shared/packindex"
	"github.com/missingbulb/ClaudiniteEngine/shared/packset"
)

// maxUnpacked bounds the files of one archive, uncompressed.
var maxUnpacked int64 = 64 << 20

// File is one file of a pack's vendored set.
type File struct {
	Data       []byte
	Executable bool
}

// VerifyArchive checks an archive's SHA-256 and size against its index
// entry, before anything reads it.
func VerifyArchive(data []byte, e packindex.Entry) error {
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != e.SHA256 || int64(len(data)) != e.Size {
		return fmt.Errorf("archive sha256: %s is %d bytes with sha256 %s, but the index names %d bytes with sha256 %s", e.Version, len(data), got, e.Size, e.SHA256)
	}
	return nil
}

// ReadArchive reads an archive's files. Only regular files with relative,
// clean, forward-slash paths and mode 0644 or 0755 are accepted, at most
// maxUnpacked bytes in all.
func ReadArchive(data []byte) (map[string]File, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("archive: %w", err)
	}
	tr := tar.NewReader(gz)
	files := map[string]File{}
	var total int64
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("archive: %w", err)
		}
		name := h.Name
		if h.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("archive: %s is not a regular file", name)
		}
		if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, `\`) || path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") {
			return nil, fmt.Errorf("archive: %q is not a clean pack-relative path", name)
		}
		mode := h.Mode & 0o7777
		if mode != 0o644 && mode != 0o755 {
			return nil, fmt.Errorf("archive: %s has mode %04o; only 0644 and 0755 are accepted", name, mode)
		}
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("archive: %s appears twice", name)
		}
		total += h.Size
		if h.Size < 0 || total > maxUnpacked {
			return nil, fmt.Errorf("archive: more than %d bytes unpacked", maxUnpacked)
		}
		body, err := io.ReadAll(io.LimitReader(tr, h.Size+1))
		if err != nil {
			return nil, fmt.Errorf("archive: %w", err)
		}
		if int64(len(body)) != h.Size {
			return nil, fmt.Errorf("archive: %s is truncated", name)
		}
		files[name] = File{Data: body, Executable: mode == 0o755}
	}
	for name := range files {
		for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
			if _, clash := files[dir]; clash {
				return nil, fmt.Errorf("archive: %s is both a file and a folder", dir)
			}
		}
	}
	return files, nil
}

// Unpack replaces dir with the archive's files: they are written into a
// sibling temporary folder, which then takes dir's place by rename, so a
// file the new version dropped is gone and a reader never sees a
// half-written tree. A refused archive leaves dir as it was.
func Unpack(data []byte, dir string) error {
	files, err := ReadArchive(data)
	if err != nil {
		return err
	}
	return WriteTree(files, dir)
}

// WriteTree replaces dir with files, as Unpack does.
func WriteTree(files map[string]File, dir string) error {
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(parent, "."+filepath.Base(dir)+".new-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	for name, f := range files {
		p := filepath.Join(tmp, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if f.Executable {
			mode = 0o755
		}
		if err := os.WriteFile(p, f.Data, mode); err != nil {
			return err
		}
		if err := os.Chmod(p, mode); err != nil {
			return err
		}
	}
	var old string
	if _, err := os.Lstat(dir); err == nil {
		old = filepath.Join(parent, "."+filepath.Base(dir)+".old-"+filepath.Base(tmp))
		if err := os.Rename(dir, old); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, dir); err != nil {
		if old != "" {
			_ = os.Rename(old, dir)
		}
		return err
	}
	if old != "" {
		return os.RemoveAll(old)
	}
	return nil
}

// HeldVersion is the version of the pack a vendored tree holds, read from
// its pack.json.
func HeldVersion(dir string) (string, error) {
	m, err := packset.ReadManifest(dir)
	if err != nil {
		return "", fmt.Errorf("%s: %w", dir, err)
	}
	return m.Version, nil
}

// ReadTree reads every file under dir, by forward-slash relative path.
func ReadTree(dir string) (map[string]File, error) {
	files := map[string]File{}
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", rel)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = File{Data: data, Executable: info.Mode().Perm()&0o111 != 0}
		return nil
	})
	return files, err
}

// TreeEquals compares the files under dir with the archive's, file by
// file (the gzip bytes may differ across zlibs): it returns "" when they
// are the same set, else the differences, one per line.
func TreeEquals(dir string, archive []byte) (string, error) {
	have, err := ReadTree(dir)
	if err != nil {
		return "", err
	}
	return FilesEqual(have, archive)
}

// FilesEqual is TreeEquals over files already read, such as a tree at a
// git commit.
func FilesEqual(have map[string]File, archive []byte) (string, error) {
	want, err := ReadArchive(archive)
	if err != nil {
		return "", err
	}
	var diffs []string
	for name, w := range want {
		h, ok := have[name]
		switch {
		case !ok:
			diffs = append(diffs, "missing: "+name)
		case !bytes.Equal(h.Data, w.Data):
			diffs = append(diffs, "differing: "+name)
		case h.Executable != w.Executable:
			diffs = append(diffs, "mode: "+name)
		}
	}
	for name := range have {
		if _, ok := want[name]; !ok {
			diffs = append(diffs, "extra: "+name)
		}
	}
	sort.Strings(diffs)
	return strings.Join(diffs, "\n"), nil
}
