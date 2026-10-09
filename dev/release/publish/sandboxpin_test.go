package publish

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/dev/test/scripttest"
)

func TestSandboxPinWritesTheRehearsalFixture(t *testing.T) {
	t.Parallel()
	const ver = "1.60930.1"
	pin := "sha512-" + strings.Repeat("A", 86) + "=="
	sandbox := t.TempDir()
	old := filepath.Join(sandbox, ".github", "workflows", "claudinite-update.yml")
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("name: old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := scripttest.Run(t, nil, "dev/release/publish/sandbox-pin.sh", ver, pin, sandbox); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	fixture := filepath.Join(t.TempDir(), "member")
	if out, err := scripttest.Run(t, nil, "dev/release/verify/fixtures/member-fixture.sh", fixture, ver, pin, "canary"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if out, err := exec.Command("diff", "-r", fixture, sandbox).CombinedOutput(); err != nil {
		t.Errorf("sandbox differs from the rehearsal fixture:\n%s", out)
	}
	settings, _ := os.ReadFile(filepath.Join(sandbox, ".claudinite", "settings.yaml"))
	for _, want := range []string{`package: "@claudinite/cli"`, `channel: "canary"`, `version: "` + ver + `"`, `manifest: "` + pin + `"`} {
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
	if _, err := os.Stat(filepath.Join(sandbox, ".github", "workflows", "claudinite-update.yml")); err == nil {
		t.Error("the pinned sandbox keeps the superseded update workflow")
	}
	for _, name := range []string{"claudinite-ci.yml", "claudinite-scheduler.yml", "claudinite-executor.yml"} {
		got, err := os.ReadFile(filepath.Join(sandbox, ".github", "workflows", name))
		dir := "../../../cn/tasks/workflows/templates"
		if name == "claudinite-ci.yml" {
			dir = "../../../cn/lifecycle/workflows/templates"
		}
		want, _ := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(got) != string(want) {
			t.Errorf(".github/workflows/%s is not the engine's template: %v", name, err)
		}
	}
	verifyOut, err := exec.Command("go", "run", "../../../cn", "verify", "--repo", sandbox).CombinedOutput()
	if err != nil || len(verifyOut) != 0 {
		t.Errorf("verify of the pinned sandbox: %v\n%s", err, verifyOut)
	}
	launch, _ := os.ReadFile(filepath.Join(sandbox, ".claudinite", "launch"))
	src, _ := os.ReadFile("../../../cn/launcher/launch")
	if string(launch) != string(src) {
		t.Error(".claudinite/launch is not the launcher verbatim")
	}
	for _, bad := range [][]string{{"1.1", pin}, {ver, "sha512-short"}, {ver}} {
		if out, err := scripttest.Run(t, nil, "dev/release/publish/sandbox-pin.sh", append(bad, t.TempDir())...); err == nil {
			t.Errorf("accepted %v:\n%s", bad, out)
		}
	}
	for _, channel := range []string{"rc", "latest", "@claudinite/cli-rc", ""} {
		if out, err := scripttest.Run(t, nil, "dev/release/verify/fixtures/member-fixture.sh", t.TempDir(), ver, pin, channel); err == nil {
			t.Errorf("member-fixture.sh accepted the channel %q:\n%s", channel, out)
		}
	}
}
