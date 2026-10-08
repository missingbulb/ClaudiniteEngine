package publish

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/dev/test/scripttest"
)

func TestDeprecateCommands(t *testing.T) {
	t.Parallel()
	d, err := DeprecateCommands(DeprecateInput{Action: "hold", Version: "1.60930.3", Reason: "it's broken", Versions: heldBy(`["0.0.0","1.60930.3"]`)})
	if err != nil || !d.Exists {
		t.Fatalf("%+v %v", d, err)
	}
	scripttest.Golden(t, "deprecate-commands.txt", strings.Join(d.Commands, "\n")+"\n")

	d, err = DeprecateCommands(DeprecateInput{Action: "revoke", Version: "1.60930.3", Reason: "phase 2 gate", Versions: heldBy(`"1.60930.3"`)})
	if err != nil || len(d.Commands) != 6 || d.Commands[0] != `npm deprecate '@claudinite/cli@1.60930.3' 'revoked: phase 2 gate'` {
		t.Errorf("one version: %+v %v", d, err)
	}
	if _, err := DeprecateCommands(DeprecateInput{Action: "hold", Version: "1.60930.3", Reason: "x", Versions: heldBy(`{"error":{"code":"E500","summary":"down"}}`)}); err == nil || !strings.Contains(err.Error(), "E500") {
		t.Errorf("a failed read passed: %v", err)
	}
	for _, c := range d.Commands {
		if strings.Contains(c, "cli-rc") {
			t.Errorf("a retired rc package: %s", c)
		}
	}
}

// release lifts a hold or revocation: the same packages, an empty
// message, no reason needed.
func TestUndeprecateCommands(t *testing.T) {
	t.Parallel()
	d, err := DeprecateCommands(DeprecateInput{Action: "release", Version: "1.60930.3", Versions: heldBy(`["1.60930.3"]`)})
	if err != nil || !d.Exists {
		t.Fatalf("%+v %v", d, err)
	}
	scripttest.Golden(t, "undeprecate-commands.txt", strings.Join(d.Commands, "\n")+"\n")
}

// A staging build publishes linux-x64 alone; npm deprecate on a platform
// package without the version would fail the job, so those are left out.
func TestDeprecateCommandsForAStagingVersion(t *testing.T) {
	t.Parallel()
	in := DeprecateInput{Action: "hold", Version: "1.61005.2", Reason: "x", Versions: heldBy(`["1.61005.1"]`)}
	in.Versions["@claudinite/cli"] = `["1.61005.1","1.61005.2"]`
	in.Versions["@claudinite/cli-linux-x64"] = `["1.61005.1","1.61005.2"]`
	d, err := DeprecateCommands(in)
	if err != nil || len(d.Commands) != 2 || !strings.Contains(d.Commands[1], "@claudinite/cli-linux-x64@1.61005.2") || !strings.Contains(d.Notice, "@claudinite/cli-darwin-arm64") {
		t.Errorf("%+v %v", d, err)
	}
}

// A version npm does not have is not an error: nothing to hold.
func TestDeprecateCommandsForAMissingVersion(t *testing.T) {
	t.Parallel()
	d, err := DeprecateCommands(DeprecateInput{Action: "hold", Version: "1.60930.9", Reason: "x", Versions: heldBy(`["1.60930.3"]`)})
	if err != nil || d.Exists || len(d.Commands) != 0 || !strings.Contains(d.Notice, "1.60930.9") {
		t.Errorf("%+v %v", d, err)
	}
}

func TestDeprecateCommandsRefuses(t *testing.T) {
	t.Parallel()
	for name, in := range map[string]DeprecateInput{
		"promote":        {Action: "promote", Version: "1.60930.3", Reason: "x"},
		"no reason":      {Action: "hold", Version: "1.60930.3"},
		"a newline":      {Action: "hold", Version: "1.60930.3", Reason: "a\nb"},
		"not a version":  {Action: "hold", Version: "60930.3", Reason: "x"},
		"bad npm answer": {Action: "hold", Version: "1.60930.3", Reason: "x", Versions: heldBy("{")},
	} {
		if _, err := DeprecateCommands(in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// promote.yml's deprecate job runs deprecate.sh in the promote
// environment, with the token npm deprecate needs.
func TestPromoteHasHoldAndRevoke(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(scripttest.Path(t, ".github/workflows/promote.yml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{
		"      action:\n", "          - promote\n          - hold\n          - revoke\n          - release\n", "      reason:\n",
		"  deprecate:\n    if: inputs.action == 'hold' || inputs.action == 'revoke' || inputs.action == 'release'\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("promote.yml lacks %q", want)
		}
	}
	job := scripttest.JobBlock(t, ".github/workflows/promote.yml", "deprecate")
	for _, want := range []string{
		"    environment: promote\n",
		"NPM_DEPRECATE_TOKEN: ${{ secrets.NPM_DEPRECATE_TOKEN }}",
		`run: dev/release/publish/deprecate.sh "$ACTION" "$VERSION" "$REASON"`,
	} {
		if !strings.Contains(job, want) {
			t.Errorf("the deprecate job lacks %q:\n%s", want, job)
		}
	}
	if strings.Contains(s, "by hand") || strings.Contains(s, "for a person") {
		t.Error("promote.yml still asks a person to run commands")
	}
	for _, job := range []string{"gate"} {
		if !strings.Contains(s, "  "+job+":\n    if: inputs.action == 'promote'\n") {
			t.Errorf("%s runs on hold and revoke", job)
		}
	}
}

// deprecate.sh writes the commands before it runs any, and without the
// token it fails naming the secret, the commands kept in the summary.
func TestDeprecateNeedsTheToken(t *testing.T) {
	t.Parallel()
	summary := filepath.Join(t.TempDir(), "summary")
	out, err := scripttest.Run(t, []string{"PATH=" + fakeViewNpm(t, `["1.60930.3"]`), "NPM_DEPRECATE_TOKEN=", "GITHUB_STEP_SUMMARY=" + summary},
		"dev/release/publish/deprecate.sh", "hold", "1.60930.3", "broken")
	if err == nil || !strings.Contains(out, "::error::the promote environment has no NPM_DEPRECATE_TOKEN") {
		t.Fatalf("err %v\n%s", err, out)
	}
	raw, _ := os.ReadFile(summary)
	if !strings.Contains(string(raw), "npm deprecate '@claudinite/cli@1.60930.3' 'held: broken'") {
		t.Errorf("the summary lacks the commands:\n%s", raw)
	}
	if strings.Contains(out, "deprecate called") {
		t.Errorf("npm deprecate ran without the token:\n%s", out)
	}
}

// A failed npm view other than npm's E404 fails the run before any
// command is written; an E404 is a package that holds nothing.
func TestDeprecateStopsOnAFailedRead(t *testing.T) {
	t.Parallel()
	out, err := scripttest.Run(t, []string{"PATH=" + fakeViewNpm(t, "E500"), "NPM_DEPRECATE_TOKEN=x", "GITHUB_STEP_SUMMARY=" + filepath.Join(t.TempDir(), "s")},
		"dev/release/publish/deprecate.sh", "hold", "1.60930.3", "broken")
	if err == nil || !strings.Contains(out, "nothing deprecated") || strings.Contains(out, "deprecate called") {
		t.Fatalf("err %v\n%s", err, out)
	}
	out, err = scripttest.Run(t, []string{"PATH=" + fakeViewNpm(t, "E404"), "NPM_DEPRECATE_TOKEN=x", "GITHUB_STEP_SUMMARY=" + filepath.Join(t.TempDir(), "s")},
		"dev/release/publish/deprecate.sh", "hold", "1.60930.3", "broken")
	if err != nil || strings.Contains(out, "deprecate called") {
		t.Fatalf("E404: err %v\n%s", err, out)
	}
}

// fakeViewNpm is PATH with a fake npm first: view answers every package's
// versions with versions, or fails with that npm error code (E404, E500),
// and any other call prints "<command> called" and fails.
func fakeViewNpm(t *testing.T, versions string) string {
	t.Helper()
	dir := t.TempDir()
	answer := "echo '" + versions + "'"
	if strings.HasPrefix(versions, "E") {
		answer = "echo 'npm error code " + versions + "' >&2; exit 1"
	}
	script := "#!/bin/sh\ncase $1 in\n  view) " + answer + " ;;\n  *) echo \"$1 called\" >&2; exit 99 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "npm"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir + string(os.PathListSeparator) + os.Getenv("PATH")
}
