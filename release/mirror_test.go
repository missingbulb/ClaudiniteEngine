package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/shared/npmreg"
)

// fakeGH is a PATH whose gh appends its arguments to a log, answers
// release list with list, and fails a delete with deleteErr when set.
func fakeGH(t *testing.T, list, deleteErr string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "gh.log")
	script := "#!/bin/sh\necho \"$*\" >> " + log + "\n" +
		"case \"$1 $2\" in\n" +
		"  'release list') printf '%s\\n' " + shellQuote(list) + " ;;\n" +
		"  'release delete') if [ -n " + shellQuote(deleteErr) + " ]; then echo " + shellQuote(deleteErr) + " >&2; exit 1; fi ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir + string(os.PathListSeparator) + os.Getenv("PATH"), log
}


func mirrorSh(t *testing.T, path, token string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", append([]string{"mirror.sh"}, args...)...)
	cmd.Env = append(os.Environ(), "PATH="+path, "GH_TOKEN="+token)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestMirrorPutUploadsAndDropsTheStale(t *testing.T) {
	t.Parallel()
	dist := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dist, "tarballs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"cli-1.61005.9.tgz", "cli-linux-x64-1.61005.9.tgz", "cli-1.61005.8.tgz"} {
		if err := os.WriteFile(filepath.Join(dist, "tarballs", f), []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path, log := fakeGH(t, "v1.61004.3\nv1.61005.9", "")
	if out, err := mirrorSh(t, path, "t", "put", "1.61005.9", dist); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	raw, _ := os.ReadFile(log)
	calls := strings.Split(strings.TrimSpace(string(raw)), "\n")
	tb := filepath.Join(dist, "tarballs")
	if len(calls) != 3 ||
		!strings.HasPrefix(calls[0], "release create v1.61005.9 --repo missingbulb/ClaudiniteMirror --title 1.61005.9 --notes ") ||
		!strings.HasSuffix(calls[0], " "+filepath.Join(tb, "cli-1.61005.9.tgz")+" "+filepath.Join(tb, "cli-linux-x64-1.61005.9.tgz")) ||
		!strings.HasPrefix(calls[1], "release list --repo missingbulb/ClaudiniteMirror ") ||
		calls[2] != "release delete v1.61004.3 --repo missingbulb/ClaudiniteMirror --cleanup-tag --yes" {
		t.Errorf("gh calls:\n%s", raw)
	}
}

func TestMirrorRefuses(t *testing.T) {
	t.Parallel()
	path, _ := fakeGH(t, "", "")
	if out, err := mirrorSh(t, path, "", "drop", "1.61005.9"); err == nil || !strings.Contains(out, "MIRROR_TOKEN") {
		t.Errorf("no token: %v %s", err, out)
	}
	if out, err := mirrorSh(t, path, "t", "put", "1.61005.9", t.TempDir()); err == nil || !strings.Contains(out, "no tarballs") {
		t.Errorf("empty dist: %v %s", err, out)
	}
	gone, _ := fakeGH(t, "", "release not found")
	if out, err := mirrorSh(t, gone, "t", "drop", "1.61005.9"); err != nil {
		t.Errorf("dropping a dropped release: %v %s", err, out)
	}
	broken, _ := fakeGH(t, "", "HTTP 403")
	if out, err := mirrorSh(t, broken, "t", "drop", "1.61005.9"); err == nil || !strings.Contains(out, "HTTP 403") {
		t.Errorf("a refused delete: %v %s", err, out)
	}
}

// The repository mirror.sh writes is the one the launcher and cn update read.
func TestMirrorIsTheDefaultMirror(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("mirror.sh")
	if err != nil {
		t.Fatal(err)
	}
	repo := strings.TrimSuffix(strings.TrimPrefix(npmreg.DefaultMirror, "https://github.com/"), "/releases/download")
	if !strings.Contains(string(raw), "\nrepo="+repo+"\n") {
		t.Errorf("mirror.sh does not write %s, the repository of npmreg.DefaultMirror %s", repo, npmreg.DefaultMirror)
	}
}
