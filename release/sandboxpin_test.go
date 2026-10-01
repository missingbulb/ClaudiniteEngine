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
