package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSandboxPinWritesTheRehearsalFixture(t *testing.T) {
	const ver = "60930.1.0"
	pin := "sha512-" + strings.Repeat("A", 86) + "=="
	sandbox := t.TempDir()
	if out, err := runScript(t, nil, "release/sandbox-pin.sh", ver, pin, sandbox); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	fixture := filepath.Join(t.TempDir(), "member")
	if out, err := runScript(t, nil, "release/member-fixture.sh", fixture, ver, pin, "@claudinite/cli-rc"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if out, err := exec.Command("diff", "-r", fixture, sandbox).CombinedOutput(); err != nil {
		t.Errorf("sandbox differs from the rehearsal fixture:\n%s", out)
	}
	settings, _ := os.ReadFile(filepath.Join(sandbox, ".claudinite", "settings.yaml"))
	for _, want := range []string{`package: "@claudinite/cli-rc"`, `version: "` + ver + `"`, `manifest: "` + pin + `"`} {
		if !strings.Contains(string(settings), want) {
			t.Errorf("settings.yaml lacks %s:\n%s", want, settings)
		}
	}
	if ignore, err := os.ReadFile(filepath.Join(sandbox, ".claudinite", ".gitignore")); err != nil || string(ignore) != "bin/\n" {
		t.Errorf(".claudinite/.gitignore: %q %v", ignore, err)
	}
	if _, err := os.Stat(filepath.Join(sandbox, ".gitignore")); err == nil {
		t.Error("the fixture writes into the repo root's .gitignore, the old shape")
	}
	for _, name := range []string{"claudinite-update.yml", "claudinite-ci.yml"} {
		got, err := os.ReadFile(filepath.Join(sandbox, ".github", "workflows", name))
		want, _ := os.ReadFile(filepath.Join("../lifecycle/workflows/templates", name))
		if err != nil || string(got) != string(want) {
			t.Errorf(".github/workflows/%s is not the engine's template: %v", name, err)
		}
	}
	verifyOut, err := exec.Command("go", "run", "../cmd/cn", "verify", "--repo", sandbox).CombinedOutput()
	if err != nil || len(verifyOut) != 0 {
		t.Errorf("verify of the pinned sandbox: %v\n%s", err, verifyOut)
	}
	launch, _ := os.ReadFile(filepath.Join(sandbox, ".claudinite", "launch"))
	src, _ := os.ReadFile("../launcher/launch")
	if string(launch) != string(src) {
		t.Error(".claudinite/launch is not the launcher verbatim")
	}
	for _, bad := range [][]string{{"1.1", pin}, {ver, "sha512-short"}, {ver}} {
		if out, err := runScript(t, nil, "release/sandbox-pin.sh", append(bad, t.TempDir())...); err == nil {
			t.Errorf("accepted %v:\n%s", bad, out)
		}
	}
}
