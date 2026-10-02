package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// check sdk writes an SDK a pack repo's test module resolves offline by
// adding the stanza beside it, and the fake engine drives a check there.
func TestCheckSDKIsATestModulesReplaceTarget(t *testing.T) {
	bin := buildCN(t, "")
	dir := t.TempDir()
	sdk := filepath.Join(dir, "sdk")
	if out, errs, code := runCN(t, bin, nil, "", "check", "sdk", "--out", sdk); code != 0 || !strings.Contains(out, sdk) {
		t.Fatalf("exit %d %s %s", code, out, errs)
	}
	stanza, err := os.ReadFile(filepath.Join(sdk, "go.mod.stanza"))
	if err != nil || !strings.Contains(string(stanza), "replace claudinite.com/checksdk => "+sdk) {
		t.Fatalf("stanza %q %v", stanza, err)
	}
	pkg := filepath.Join(dir, "packs", "acme-pack", "checks")
	_ = os.MkdirAll(pkg, 0o755)
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "go.mod"), "module acme.test\n\ngo 1.22\n\n"+string(stanza))
	write(filepath.Join(pkg, "acme.go"), `package checks

import "claudinite.com/checksdk"

func Acme(repo checksdk.Repo) []checksdk.Finding {
	var out []checksdk.Finding
	for _, f := range repo.ChangedFiles() {
		out = append(out, checksdk.Finding{Path: f, Sentence: "changed"})
	}
	return out
}
`)
	write(filepath.Join(pkg, "acme_test.go"), `package checks

import (
	"testing"

	"claudinite.com/checksdk"
)

func TestAcme(t *testing.T) {
	f := &checksdk.Fake{ChangedFiles: []string{"a.md"}}
	if got := Acme(f.Repo(t.TempDir())); len(got) != 1 || got[0].Path != "a.md" {
		t.Fatalf("%+v", got)
	}
}
`)
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off", "GOTOOLCHAIN=local")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go test: %v\n%s", err, out)
	}
}
