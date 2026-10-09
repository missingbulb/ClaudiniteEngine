package gitcmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/cn/helpers/proc"
)

// A batch answers every name in the order asked, from one git process:
// blobs whatever their paths hold, a path or ref that is not there as
// Missing, and a name the batch cannot carry as Missing without asking.
func TestObjectsAnswerInOrderFromOneProcess(t *testing.T) {
	r, _ := clone(t)
	_ = os.MkdirAll(filepath.Join(r.Dir, "d"), 0o755)
	_ = os.WriteFile(filepath.Join(r.Dir, "d", "with space.txt"), []byte("spaced missing\n"), 0o644)
	_ = os.WriteFile(filepath.Join(r.Dir, "empty"), nil, 0o644)
	git(t, r.Dir, "add", "-A")
	git(t, r.Dir, "commit", "-q", "-m", "more")
	proc.Reset()
	objs, err := r.Objects("HEAD:d/with space.txt", "HEAD:nope", "HEAD:empty", "no-such-ref:a.txt", "HEAD:a\nb", "HEAD:d", "HEAD:a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got := proc.Total(proc.Counts()); got != 1 {
		t.Errorf("%d processes (%s), want 1", got, proc.Format(proc.Counts()))
	}
	want := []Object{{Type: "blob", Data: []byte("spaced missing\n")}, {Missing: true}, {Type: "blob", Data: []byte{}},
		{Missing: true}, {Missing: true}, {Type: "tree"}, {Type: "blob", Data: []byte("one\n")}}
	for i, w := range want {
		o := objs[i]
		if o.Missing != w.Missing || o.Type != w.Type || (w.Type == "blob" && string(o.Data) != string(w.Data)) {
			t.Errorf("answer %d: %+v, want %+v", i, o, w)
		}
	}
}

func TestShowReadsAFileInOneProcess(t *testing.T) {
	r, _ := clone(t)
	proc.Reset()
	data, ok, err := r.Show("HEAD", "a.txt")
	if err != nil || !ok || string(data) != "one\n" {
		t.Fatalf("%q %v %v", data, ok, err)
	}
	if _, ok, err := r.Show("HEAD", "nope"); ok || err != nil {
		t.Errorf("a missing path: %v %v", ok, err)
	}
	if _, _, err := r.Show("no-such-ref", "a.txt"); err == nil {
		t.Error("a missing ref read as a commit")
	}
	if got := proc.Total(proc.Counts()); got != 3 {
		t.Errorf("%d processes for three reads (%s)", got, proc.Format(proc.Counts()))
	}
}
