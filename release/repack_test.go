package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// tarEntries reads every file of a tarball: name -> sha256.
func tarEntries(t *testing.T, tgz string) map[string]string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("tar", "-xzf", tgz, "-C", dir).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v %s", tgz, err, out)
	}
	got := map[string]string{}
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			raw, _ := os.ReadFile(p)
			sum := sha256.Sum256(raw)
			rel, _ := filepath.Rel(dir, p)
			got[rel] = hex.EncodeToString(sum[:])
		}
		return nil
	})
	return got
}

func tarJSON(t *testing.T, tgz string) map[string]any {
	t.Helper()
	raw, err := exec.Command("tar", "-xzOf", tgz, "package/package.json").Output()
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func signedDist(t *testing.T) string {
	t.Helper()
	dist, _ := unsignedDist(t)
	if out, err := runScript(t, append(devKeyEnv(t), "DIST="+dist), "release/sign.sh"); err != nil {
		t.Fatalf("sign.sh: %v\n%s", err, out)
	}
	return dist
}

func TestRepackKeepsTheBytesAndRenamesOnly(t *testing.T) {
	dist := signedDist(t)
	out := filepath.Join(t.TempDir(), "stable")
	if o, err := runScript(t, nil, "release/repack.sh", filepath.Join(dist, "tarballs"), out); err != nil {
		t.Fatalf("repack.sh: %v\n%s", err, o)
	}
	rcs, _ := filepath.Glob(filepath.Join(dist, "tarballs", "*.tgz"))
	stables, _ := filepath.Glob(filepath.Join(out, "*.tgz"))
	if len(rcs) != 6 || len(stables) != 6 {
		t.Fatalf("%d rc tarballs, %d stable", len(rcs), len(stables))
	}
	for _, rc := range rcs {
		base := filepath.Base(rc)
		stable := filepath.Join(out, strings.Replace(base, "cli-rc", "cli", 1))
		a, b := tarEntries(t, rc), tarEntries(t, stable)
		for name, sum := range a {
			if name == "package/package.json" {
				continue
			}
			if b[name] != sum {
				t.Errorf("%s: %s differs or is missing", base, name)
			}
		}
		if len(a) != len(b) {
			t.Errorf("%s: %d files, stable %d", base, len(a), len(b))
		}
		ja, jb := tarJSON(t, rc), tarJSON(t, stable)
		wantName := strings.Replace(ja["name"].(string), "@claudinite/cli-rc", "@claudinite/cli", 1)
		if jb["name"] != wantName {
			t.Errorf("%s: name %v, want %s", base, jb["name"], wantName)
		}
		delete(ja, "name")
		delete(jb, "name")
		if !reflect.DeepEqual(ja, jb) {
			t.Errorf("%s: package.json differs beyond name: %v vs %v", base, ja, jb)
		}
	}
	if _, ok := tarEntries(t, filepath.Join(out, "cli-1.61001.1.tgz"))["package/manifest.sig.json"]; !ok {
		t.Error("the stable manifest package lacks manifest.sig.json")
	}
}

func TestRepackRefusesAnUnsignedChannel(t *testing.T) {
	dist, _ := unsignedDist(t)
	if o, err := runScript(t, nil, "release/repack.sh", filepath.Join(dist, "tarballs"), filepath.Join(t.TempDir(), "stable")); err == nil || !strings.Contains(o, "manifest.sig.json") {
		t.Fatalf("repacked an unsigned channel (%v):\n%s", err, o)
	}
}
