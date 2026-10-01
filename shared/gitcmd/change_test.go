package gitcmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestListFilesAndAttributes(t *testing.T) {
	r, _ := clone(t)
	write(t, r.Dir, ".gitignore", "ignored.txt\n")
	write(t, r.Dir, ".gitattributes", "vendor/** linguist-vendored\n")
	write(t, r.Dir, "vendor/x.js", "x\n")
	git(t, r.Dir, "add", "-A")
	git(t, r.Dir, "commit", "-q", "-m", "attrs")
	write(t, r.Dir, "ignored.txt", "i\n")
	write(t, r.Dir, "new file.txt", "n\n")
	_ = os.Remove(filepath.Join(r.Dir, "a.txt"))
	tracked, untracked := r.ListFiles()
	if want := []string{".gitattributes", ".gitignore", "a.txt", "vendor/x.js"}; !reflect.DeepEqual(tracked, want) {
		t.Errorf("tracked %q, want %q (a file deleted from the tree stays in the index)", tracked, want)
	}
	if want := []string{"new file.txt"}; !reflect.DeepEqual(untracked, want) {
		t.Errorf("untracked %q, want %q", untracked, want)
	}
	got := r.CheckAttr(tracked, "linguist-vendored", "linguist-generated")
	if !got["vendor/x.js"] || len(got) != 1 {
		t.Errorf("attributes: %v", got)
	}
}

func TestBaseRefAndRefresh(t *testing.T) {
	r, bare := clone(t)
	if got := r.BaseRef(); got != "origin/main" {
		t.Errorf("base ref %q", got)
	}
	other := filepath.Join(t.TempDir(), "other")
	git(t, filepath.Dir(other), "clone", "-q", bare, other)
	write(t, other, "b.txt", "b\n")
	git(t, other, "add", "b.txt")
	git(t, other, "commit", "-q", "-m", "second")
	git(t, other, "push", "-q", "origin", "main")
	upstream := git(t, other, "rev-parse", "HEAD")

	t.Setenv(NoFetchEnv, "1")
	r.RefreshBaseRef("origin/main")
	if git(t, r.Dir, "rev-parse", "origin/main") == upstream {
		t.Fatal("fetched with the skip set")
	}
	t.Setenv(NoFetchEnv, "")
	r.RefreshBaseRef("origin/main")
	if git(t, r.Dir, "rev-parse", "origin/main") != upstream {
		t.Fatal("did not fetch")
	}
	marker := filepath.Join(r.Dir, ".git", refreshFile)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("no marker: %v", err)
	}
	// Inside the window a second refresh fetches nothing.
	write(t, other, "c.txt", "c\n")
	git(t, other, "add", "c.txt")
	git(t, other, "commit", "-q", "-m", "third")
	git(t, other, "push", "-q", "origin", "main")
	r.RefreshBaseRef("origin/main")
	if git(t, r.Dir, "rev-parse", "origin/main") != upstream {
		t.Error("fetched inside the window")
	}
	old := time.Now().Add(-FetchWindow - time.Minute).UnixMilli()
	write(t, r.Dir, ".git/"+refreshFile, `{"ref":"origin/main","at":`+strconv.FormatInt(old, 10)+`}`)
	r.RefreshBaseRef("origin/main")
	if git(t, r.Dir, "rev-parse", "origin/main") == upstream {
		t.Error("did not fetch once the window passed")
	}
	r.RefreshBaseRef("main")
	if !r.IsAncestor("main", "origin/main") || r.IsAncestor("origin/main", "main") {
		t.Error("ancestry")
	}
	git(t, r.Dir, "remote", "remove", "origin")
	if got := r.BaseRef(); got != "main" {
		t.Errorf("without origin: base ref %q", got)
	}
}

func TestTheChange(t *testing.T) {
	r, _ := clone(t)
	write(t, r.Dir, "a.txt", "one\ntwo\nthree\n")
	write(t, r.Dir, "gone.txt", "g\n")
	git(t, r.Dir, "add", "-A")
	git(t, r.Dir, "commit", "-q", "-m", "base")
	base := git(t, r.Dir, "rev-parse", "HEAD")
	git(t, r.Dir, "checkout", "-q", "-b", "change")
	write(t, r.Dir, "a.txt", "one\n2\nthree\nfour\n")
	git(t, r.Dir, "rm", "-q", "gone.txt")
	git(t, r.Dir, "commit", "-q", "-am", "edit\n\nthe body")
	git(t, r.Dir, "checkout", "-q", "-b", "side", base)
	write(t, r.Dir, "side.txt", "s\n")
	git(t, r.Dir, "add", "side.txt")
	git(t, r.Dir, "commit", "-q", "-m", "side")
	git(t, r.Dir, "checkout", "-q", "change")
	git(t, r.Dir, "merge", "-q", "--no-ff", "-m", "Merge side", "side")
	write(t, r.Dir, "wip.txt", "w\n")
	git(t, r.Dir, "add", "wip.txt")

	if got := r.AbbrevHead(); got != "change" {
		t.Errorf("head %q", got)
	}
	vs, del := r.DiffLists(base, base)
	if want := []string{"a.txt", "side.txt", "wip.txt"}; !reflect.DeepEqual(vs, want) {
		t.Errorf("changed %q, want %q", vs, want)
	}
	if want := []string{"gone.txt"}; !reflect.DeepEqual(del, want) {
		t.Errorf("deleted %q, want %q", del, want)
	}
	if _, del := r.DiffLists(base, ""); del != nil {
		t.Errorf("deleted with no merge base: %q", del)
	}
	if got, want := r.AddedLines(base, "a.txt"), []Line{{2, "2"}, {4, "four"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("added %v, want %v", got, want)
	}
	if got, want := r.RemovedLines(base, "a.txt"), []Line{{2, "two"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("removed %v, want %v", got, want)
	}
	msgs := r.CommitMessages(base)
	if len(msgs) != 3 || msgs[0] != "Merge side" || !strings.Contains(strings.Join(msgs, "|"), "edit\nthe body") {
		t.Errorf("messages %q", msgs)
	}
	ms := r.Merges(base)
	if len(ms) != 1 || ms[0].Subject != "Merge side" || ms[0].Sha == "" {
		t.Errorf("merges %v", ms)
	}
	if text, ok := r.ShowText(base, "gone.txt"); !ok || text != "g\n" {
		t.Errorf("show %q %v", text, ok)
	}
	if _, ok := r.ShowText(base, "nope.txt"); ok {
		t.Error("show of a missing path")
	}
}
