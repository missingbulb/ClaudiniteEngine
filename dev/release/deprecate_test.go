package release

import (
	"os"
	"strings"
	"testing"
)

func TestDeprecateCommands(t *testing.T) {
	t.Parallel()
	d, err := DeprecateCommands(DeprecateInput{Action: "hold", Version: "1.60930.3", Reason: "it's broken", Versions: heldBy(`["0.0.0","1.60930.3"]`)})
	if err != nil || !d.Exists {
		t.Fatalf("%+v %v", d, err)
	}
	golden(t, "deprecate-commands.txt", strings.Join(d.Commands, "\n")+"\n")

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
	golden(t, "undeprecate-commands.txt", strings.Join(d.Commands, "\n")+"\n")
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

func TestPromoteHasHoldAndRevoke(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../.github/workflows/promote.yml")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{
		"      action:\n", "          - promote\n          - hold\n          - revoke\n          - release\n", "      reason:\n",
		"  deprecate:\n    if: inputs.action == 'hold' || inputs.action == 'revoke' || inputs.action == 'release'\n",
		"go run ./dev/release/pipeline deprecate-commands",
		"NPM_DEPRECATE_TOKEN: ${{ secrets.NPM_DEPRECATE_TOKEN }}",
		"::error::the promote environment has no NPM_DEPRECATE_TOKEN",
		`NODE_AUTH_TOKEN=$NPM_DEPRECATE_TOKEN sh -e "$RUNNER_TEMP/commands.sh"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("promote.yml lacks %q", want)
		}
	}
	job := jobBlock(t, "../../.github/workflows/promote.yml", "deprecate")
	if !strings.Contains(job, "    environment: promote\n") {
		t.Error("the deprecate job does not run in the promote environment")
	}
	// A staging version is on fewer packages: each package's versions are
	// read, and a failed read other than npm's E404 fails the job.
	for _, want := range []string{`npm view "$name" versions --json > "$out"`, "--versions-dir", "E404"} {
		if !strings.Contains(job, want) {
			t.Errorf("the deprecate job lacks %q", want)
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
