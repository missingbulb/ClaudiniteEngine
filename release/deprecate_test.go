package release

import (
	"os"
	"strings"
	"testing"
)

func TestDeprecateCommands(t *testing.T) {
	d, err := DeprecateCommands(DeprecateInput{Action: "hold", Version: "60930.3.0", Reason: "it's broken", RCVersions: `["0.0.0","60930.3.0"]`, StableVersions: `["0.0.0","60930.3.0"]`})
	if err != nil || !d.Exists {
		t.Fatalf("%+v %v", d, err)
	}
	golden(t, "deprecate-commands.txt", strings.Join(d.Commands, "\n")+"\n")

	d, err = DeprecateCommands(DeprecateInput{Action: "revoke", Version: "60930.3.0", Reason: "phase 2 gate", RCVersions: `"60930.3.0"`, StableVersions: ""})
	if err != nil || len(d.Commands) != 6 || !strings.Contains(d.Commands[0], `'revoked: phase 2 gate'`) {
		t.Errorf("rc only: %+v %v", d, err)
	}
	d, err = DeprecateCommands(DeprecateInput{Action: "hold", Version: "60930.3.0", Reason: "x", RCVersions: `["60930.3.0"]`, StableVersions: `{"error":{"code":"E404","summary":"not found"}}`})
	if err != nil || len(d.Commands) != 6 {
		t.Errorf("stable E404: %+v %v", d, err)
	}
	if _, err := DeprecateCommands(DeprecateInput{Action: "hold", Version: "60930.3.0", Reason: "x", RCVersions: `{"error":{"code":"E500","summary":"down"}}`}); err == nil || !strings.Contains(err.Error(), "E500") {
		t.Errorf("a failed read passed: %v", err)
	}
	for _, c := range d.Commands {
		if strings.Contains(c, "@claudinite/cli@") || strings.Contains(c, "@claudinite/cli-linux") {
			t.Errorf("a stable package for an rc-only version: %s", c)
		}
	}
}

// release lifts a hold or revocation: the same packages, an empty
// message, no reason needed.
func TestUndeprecateCommands(t *testing.T) {
	d, err := DeprecateCommands(DeprecateInput{Action: "release", Version: "60930.3.0", RCVersions: `["60930.3.0"]`})
	if err != nil || !d.Exists {
		t.Fatalf("%+v %v", d, err)
	}
	golden(t, "undeprecate-commands.txt", strings.Join(d.Commands, "\n")+"\n")
}

// A version npm does not have is not an error: nothing to hold.
func TestDeprecateCommandsForAMissingVersion(t *testing.T) {
	d, err := DeprecateCommands(DeprecateInput{Action: "hold", Version: "60930.9.0", Reason: "x", RCVersions: `["60930.3.0"]`})
	if err != nil || d.Exists || len(d.Commands) != 0 || !strings.Contains(d.Notice, "60930.9.0") {
		t.Errorf("%+v %v", d, err)
	}
}

func TestDeprecateCommandsRefuses(t *testing.T) {
	for name, in := range map[string]DeprecateInput{
		"promote":        {Action: "promote", Version: "60930.3.0", Reason: "x"},
		"no reason":      {Action: "hold", Version: "60930.3.0"},
		"a newline":      {Action: "hold", Version: "60930.3.0", Reason: "a\nb"},
		"not a version":  {Action: "hold", Version: "60930.3", Reason: "x"},
		"bad npm answer": {Action: "hold", Version: "60930.3.0", Reason: "x", RCVersions: "{"},
	} {
		if _, err := DeprecateCommands(in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestPromoteHasHoldAndRevoke(t *testing.T) {
	raw, err := os.ReadFile("../.github/workflows/promote.yml")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{
		"      action:\n", "          - promote\n          - hold\n          - revoke\n          - release\n", "      reason:\n",
		"  deprecate:\n    if: inputs.action == 'hold' || inputs.action == 'revoke' || inputs.action == 'release'\n",
		"go run ./release/pipeline deprecate-commands",
		"NPM_DEPRECATE_TOKEN: ${{ secrets.NPM_DEPRECATE_TOKEN }}",
		"::error::the promote environment has no NPM_DEPRECATE_TOKEN",
		`NODE_AUTH_TOKEN=$NPM_DEPRECATE_TOKEN sh "$RUNNER_TEMP/commands.sh"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("promote.yml lacks %q", want)
		}
	}
	jobs := strings.Split(s, "\n  deprecate:\n")
	if len(jobs) != 2 || !strings.Contains(strings.SplitN(jobs[1], "\n  publish:", 2)[0], "    environment: promote\n") {
		t.Error("the deprecate job does not run in the promote environment")
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
