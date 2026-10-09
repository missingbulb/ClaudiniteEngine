// Package testgit builds the repositories tests run git against by writing
// their objects and refs as files, so a fixture repository costs one git
// process (the one writing its index) rather than one per commit and push.
package testgit

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// Author is who every commit written here is by.
const Author = "t <t@x>"

// Init makes dir an empty repository whose HEAD is branch.
func Init(t testing.TB, dir, branch string) {
	t.Helper()
	initAt(t, filepath.Join(dir, ".git"), branch, false)
}

// InitBare makes dir an empty bare repository whose HEAD is branch.
func InitBare(t testing.TB, dir, branch string) {
	t.Helper()
	initAt(t, dir, branch, true)
}

func initAt(t testing.TB, gitDir, branch string, bare bool) {
	t.Helper()
	for _, d := range []string{"objects/info", "objects/pack", "refs/heads", "refs/tags"} {
		if err := os.MkdirAll(filepath.Join(gitDir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	config := fmt.Sprintf("[core]\n\trepositoryformatversion = 0\n\tfilemode = true\n\tbare = %t\n", bare)
	if !bare {
		config += "\tlogallrefupdates = true\n"
	}
	writeFile(t, filepath.Join(gitDir, "config"), config)
	writeFile(t, filepath.Join(gitDir, "HEAD"), "ref: refs/heads/"+branch+"\n")
}

// Commit records the files under dir, as git add -A would stage them, as a
// commit on branch whose parent is HEAD's commit (none on an unborn HEAD),
// points HEAD at branch, and returns the commit. The index is left as it
// was: Index writes it once the history is built.
func Commit(t testing.TB, dir, branch, message string) string {
	t.Helper()
	gitDir := filepath.Join(dir, ".git")
	tree, _ := writeTree(t, gitDir, dir, true)
	body := "tree " + tree + "\n"
	if parent, err := Resolve(gitDir, "HEAD"); err == nil {
		body += "parent " + parent + "\n"
	}
	stamp := fmt.Sprintf("%s %d +0000", Author, time.Now().Unix())
	body += "author " + stamp + "\ncommitter " + stamp + "\n\n" + message + "\n"
	sha := writeObject(t, gitDir, "commit", []byte(body))
	SetRef(t, gitDir, "refs/heads/"+branch, sha)
	writeFile(t, filepath.Join(gitDir, "HEAD"), "ref: refs/heads/"+branch+"\n")
	return sha
}

// Index writes dir's index from its HEAD commit, the files on disk taken as
// they are, as git leaves it after a commit.
func Index(t testing.TB, dir string) {
	t.Helper()
	cmd := exec.Command("git", "reset", "-q")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git reset: %v\n%s", err, out)
	}
}

// SetRef points ref (refs/...) of the repository at gitDir at sha.
func SetRef(t testing.TB, gitDir, ref, sha string) {
	t.Helper()
	p := filepath.Join(gitDir, filepath.FromSlash(ref))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, p, sha+"\n")
}

// Resolve reads the commit ref (HEAD, or a full refs/... name) names in
// the repository at gitDir, following a symbolic ref, from the loose ref
// or packed-refs.
func Resolve(gitDir, ref string) (string, error) {
	for range 5 {
		b, err := os.ReadFile(filepath.Join(gitDir, filepath.FromSlash(ref)))
		if errors.Is(err, os.ErrNotExist) && ref != "HEAD" {
			return packed(gitDir, ref)
		}
		if err != nil {
			return "", err
		}
		s := strings.TrimSpace(string(b))
		target, symbolic := strings.CutPrefix(s, "ref: ")
		if !symbolic {
			return s, nil
		}
		ref = target
	}
	return "", fmt.Errorf("%s: symbolic refs nest too deep", ref)
}

func packed(gitDir, ref string) (string, error) {
	b, err := os.ReadFile(filepath.Join(gitDir, "packed-refs"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		if sha, name, ok := strings.Cut(line, " "); ok && name == ref {
			return sha, nil
		}
	}
	return "", fmt.Errorf("%s: no such ref", ref)
}

// writeTree writes the tree of the files under path and returns it, or
// reports that it is empty, which git records no tree for.
func writeTree(t testing.TB, gitDir, path string, top bool) (string, bool) {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	type entry struct{ mode, name, key, sha string }
	var out []entry
	for _, e := range entries {
		name := e.Name()
		if top && name == ".git" {
			continue
		}
		if name == ".gitignore" {
			t.Fatalf("testgit commits every file and reads no %s", filepath.Join(path, name))
		}
		p := filepath.Join(path, name)
		info, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case info.IsDir():
			if sha, empty := writeTree(t, gitDir, p, false); !empty {
				out = append(out, entry{"40000", name, name + "/", sha})
			}
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, entry{"120000", name, name, writeObject(t, gitDir, "blob", []byte(target))})
		default:
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			mode := "100644"
			if info.Mode()&0o111 != 0 {
				mode = "100755"
			}
			out = append(out, entry{mode, name, name, writeObject(t, gitDir, "blob", b)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	var tree bytes.Buffer
	for _, e := range out {
		raw, _ := hex.DecodeString(e.sha)
		tree.WriteString(e.mode + " " + e.name + "\x00")
		tree.Write(raw)
	}
	return writeObject(t, gitDir, "tree", tree.Bytes()), len(out) == 0
}

func writeObject(t testing.TB, gitDir, kind string, body []byte) string {
	t.Helper()
	raw := append([]byte(fmt.Sprintf("%s %d\x00", kind, len(body))), body...)
	sum := sha1.Sum(raw)
	sha := hex.EncodeToString(sum[:])
	p := filepath.Join(gitDir, "objects", sha[:2], sha[2:])
	if _, err := os.Stat(p); err == nil {
		return sha
	}
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	_, _ = w.Write(raw)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, z.Bytes(), 0o444); err != nil {
		t.Fatal(err)
	}
	return sha
}

func writeFile(t testing.TB, p, body string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
