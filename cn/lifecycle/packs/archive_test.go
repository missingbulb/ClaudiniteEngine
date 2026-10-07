package packs

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/shared/packindex"
)

type tarEntry struct {
	name string
	mode int64
	body string
	typ  byte
	link string
}

// archive builds a pack archive the way tools/vendor does: sorted regular
// files, pack-relative paths, mtime 0.
func archive(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		if e.typ == 0 {
			e.typ = tar.TypeReg
		}
		if e.mode == 0 {
			e.mode = 0o644
		}
		h := &tar.Header{Name: e.name, Mode: e.mode, Size: int64(len(e.body)), Typeflag: e.typ, Linkname: e.link, Format: tar.FormatUSTAR}
		if e.typ != tar.TypeReg {
			h.Size = 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if e.typ == tar.TypeReg {
			_, _ = tw.Write([]byte(e.body))
		}
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func helloArchive(t *testing.T, version string) []byte {
	return archive(t,
		tarEntry{name: "RULES.md", body: "# hello\n\n- loaded\n"},
		tarEntry{name: "checks/hello.go", body: "package checks\n"},
		tarEntry{name: "pack.json", body: `{"version": "` + version + `", "minEngineVersion": "1.1.0"}`},
		tarEntry{name: "skills/hello/SKILL.md", body: "---\ndescription: hi\n---\n"},
		tarEntry{name: "tools/run.sh", mode: 0o755, body: "#!/bin/sh\n"},
	)
}

func entryFor(data []byte) packindex.Entry {
	sum := sha256.Sum256(data)
	return packindex.Entry{Version: "1.0", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))}
}

func TestVerifyArchive(t *testing.T) {
	data := helloArchive(t, "1.0")
	e := entryFor(data)
	if err := VerifyArchive(data, e); err != nil {
		t.Fatal(err)
	}
	if err := VerifyArchive(append(append([]byte{}, data...), 0), e); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Errorf("flipped size: %v", err)
	}
	bad := append([]byte{}, data...)
	bad[len(bad)/2] ^= 1
	if err := VerifyArchive(bad, e); err == nil {
		t.Error("a flipped byte verified")
	}
}

func listTree(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, filepath.ToSlash(rel)+" "+info.Mode().Perm().String())
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func TestUnpackReplacesTheTreeWholesale(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "packs", "hello")
	_ = os.MkdirAll(filepath.Join(dir, "old"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "old", "gone.md"), []byte("x"), 0o644)
	if err := Unpack(helloArchive(t, "1.0"), dir); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(listTree(t, dir), "\n")
	want := strings.Join([]string{"RULES.md -rw-r--r--", "checks/hello.go -rw-r--r--", "pack.json -rw-r--r--", "skills/hello/SKILL.md -rw-r--r--", "tools/run.sh -rwxr-xr-x"}, "\n")
	if got != want {
		t.Errorf("tree:\n%s\nwant\n%s", got, want)
	}
	siblings, _ := os.ReadDir(filepath.Dir(dir))
	if len(siblings) != 1 {
		t.Errorf("temp dirs left beside the tree: %v", siblings)
	}
	v, err := HeldVersion(dir)
	if err != nil || v != "1.0" {
		t.Errorf("held %q %v", v, err)
	}
}

func TestUnpackRefuses(t *testing.T) {
	cases := map[string][]byte{
		"absolute path":  archive(t, tarEntry{name: "/etc/x", body: "x"}),
		"dot dot":        archive(t, tarEntry{name: "a/../../x", body: "x"}),
		"symlink":        archive(t, tarEntry{name: "l", typ: tar.TypeSymlink, link: "/etc/passwd"}),
		"directory":      archive(t, tarEntry{name: "d/", typ: tar.TypeDir}),
		"device":         archive(t, tarEntry{name: "dev", typ: tar.TypeChar}),
		"setuid mode":    archive(t, tarEntry{name: "x", mode: 0o4755, body: "x"}),
		"writable mode":  archive(t, tarEntry{name: "x", mode: 0o666, body: "x"}),
		"duplicate":      archive(t, tarEntry{name: "x", body: "1"}, tarEntry{name: "x", body: "2"}),
		"not gzip":       []byte("plain"),
		"backslash path": archive(t, tarEntry{name: `a\b`, body: "x"}),
	}
	for name, data := range cases {
		dir := filepath.Join(t.TempDir(), "hello")
		_ = os.MkdirAll(dir, 0o755)
		_ = os.WriteFile(filepath.Join(dir, "keep"), []byte("k"), 0o644)
		if err := Unpack(data, dir); err == nil {
			t.Errorf("%s: unpacked", name)
		}
		if _, err := os.Stat(filepath.Join(dir, "keep")); err != nil {
			t.Errorf("%s: a refused archive changed the tree", name)
		}
	}
}

func TestUnpackRefusesOverTheTotalCap(t *testing.T) {
	old := maxUnpacked
	maxUnpacked = 10
	defer func() { maxUnpacked = old }()
	if err := Unpack(archive(t, tarEntry{name: "a", body: "123456"}, tarEntry{name: "b", body: "123456"}), filepath.Join(t.TempDir(), "p")); err == nil || !strings.Contains(err.Error(), "bytes") {
		t.Errorf("%v", err)
	}
}

func TestHeldVersion(t *testing.T) {
	dir := t.TempDir()
	if _, err := HeldVersion(dir); err == nil {
		t.Error("a tree without pack.json has a version")
	}
	_ = os.WriteFile(filepath.Join(dir, "pack.toml"), []byte("version = \"1\"\n"), 0o644)
	if v, err := HeldVersion(dir); err != nil || v != "1" {
		t.Errorf("pack.toml: %q %v", v, err)
	}
}

func TestTreeEquals(t *testing.T) {
	data := helloArchive(t, "1.0")
	dir := filepath.Join(t.TempDir(), "hello")
	if err := Unpack(data, dir); err != nil {
		t.Fatal(err)
	}
	if diff, err := TreeEquals(dir, data); err != nil || diff != "" {
		t.Fatalf("fresh tree: %q %v", diff, err)
	}
	// The same files under another gzip compression level are still equal.
	var re bytes.Buffer
	zr, _ := gzip.NewReader(bytes.NewReader(data))
	zw, _ := gzip.NewWriterLevel(&re, gzip.BestSpeed)
	_, _ = re.ReadFrom(bytes.NewReader(nil))
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(zr)
	_, _ = zw.Write(buf.Bytes())
	_ = zw.Close()
	if diff, err := TreeEquals(dir, re.Bytes()); err != nil || diff != "" {
		t.Errorf("recompressed: %q %v", diff, err)
	}
	_ = os.WriteFile(filepath.Join(dir, "RULES.md"), []byte("edited"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "extra.md"), []byte("x"), 0o644)
	_ = os.Remove(filepath.Join(dir, "pack.json"))
	_ = os.Chmod(filepath.Join(dir, "tools", "run.sh"), 0o644)
	diff, err := TreeEquals(dir, data)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"differing: RULES.md", "extra: extra.md", "missing: pack.json", "mode: tools/run.sh"} {
		if !strings.Contains(diff, want) {
			t.Errorf("diff %q lacks %q", diff, want)
		}
	}
}
