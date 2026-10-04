package release

import (
	"os"
	"strings"
	"testing"
)

// rcVersions is every rc package holding vs, as npm view answers.
func rcVersions(vs string) map[string]string {
	out := map[string]string{}
	for _, n := range UnpublishPackages() {
		out[n] = vs
	}
	return out
}

func TestUnpublishCommands(t *testing.T) {
	u, err := UnpublishCommands(UnpublishInput{Version: "1.60930.3", Versions: rcVersions(`["1.60930.2","1.60930.3"]`)})
	if err != nil || !u.Exists {
		t.Fatalf("%+v %v", u, err)
	}
	golden(t, "unpublish-commands.txt", strings.Join(u.Commands, "\n")+"\n")
	for _, c := range u.Commands {
		if strings.Contains(c, "@claudinite/cli@") || strings.Contains(c, "@claudinite/cli-linux") {
			t.Errorf("a stable package: %s", c)
		}
	}
}

// Unpublishing a package's last version deletes the package, so one such
// package refuses the whole dispatch before any command is written.
func TestUnpublishCommandsRefusesAPackagesOnlyVersion(t *testing.T) {
	in := UnpublishInput{Version: "1.60930.3", Versions: rcVersions(`["1.60930.2","1.60930.3"]`)}
	in.Versions["@claudinite/cli-rc-darwin-arm64"] = `"1.60930.3"`
	u, err := UnpublishCommands(in)
	if err == nil || len(u.Commands) != 0 || !strings.Contains(err.Error(), "@claudinite/cli-rc-darwin-arm64@1.60930.3 is the only version") {
		t.Errorf("%+v %v", u, err)
	}
	u, err = UnpublishCommands(UnpublishInput{Version: "1.60930.3", Versions: rcVersions(`["1.60930.3"]`)})
	if err == nil || len(u.Commands) != 0 || strings.Count(err.Error(), "is the only version") != 6 {
		t.Errorf("every package's only version: %+v %v", u, err)
	}
}

// A version npm does not have is not an error, as for deprecate; a
// platform package missing it is left out and named.
func TestUnpublishCommandsForAMissingVersion(t *testing.T) {
	u, err := UnpublishCommands(UnpublishInput{Version: "1.60930.9", Versions: rcVersions(`["1.60930.2","1.60930.3"]`)})
	if err != nil || u.Exists || len(u.Commands) != 0 || !strings.Contains(u.Notice, "1.60930.9") {
		t.Errorf("%+v %v", u, err)
	}
	in := UnpublishInput{Version: "1.60930.3", Versions: rcVersions(`["1.60930.2","1.60930.3"]`)}
	in.Versions["@claudinite/cli-rc-windows-x64"] = `{"error":{"code":"E404","summary":"not found"}}`
	u, err = UnpublishCommands(in)
	if err != nil || len(u.Commands) != 5 || !strings.Contains(u.Notice, "@claudinite/cli-rc-windows-x64") {
		t.Errorf("a platform without it: %+v %v", u, err)
	}
}

func TestUnpublishCommandsRefuses(t *testing.T) {
	for name, in := range map[string]UnpublishInput{
		"not a version":  {Version: "60930.3", Versions: rcVersions(`["60930.3","60930.4"]`)},
		"bad npm answer": {Version: "1.60930.3", Versions: map[string]string{"@claudinite/cli-rc": "{"}},
		"failed read":    {Version: "1.60930.3", Versions: map[string]string{"@claudinite/cli-rc": `{"error":{"code":"E500","summary":"down"}}`}},
	} {
		if _, err := UnpublishCommands(in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestPromoteHasUnpublish(t *testing.T) {
	raw, err := os.ReadFile("../.github/workflows/promote.yml")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{
		"          - revoke\n          - release\n          - unpublish\n",
		"  unpublish:\n    if: inputs.action == 'unpublish'\n",
		"go run ./release/pipeline unpublish-commands",
		`npm view "$name" versions --json > "$RUNNER_TEMP/versions/$name.json"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("promote.yml lacks %q", want)
		}
	}
	job := strings.SplitN(strings.SplitN(s, "\n  unpublish:\n", 2)[1], "\n  publish:", 2)[0]
	for _, want := range []string{
		"    environment: promote\n",
		"NPM_DEPRECATE_TOKEN: ${{ secrets.NPM_DEPRECATE_TOKEN }}",
		"::error::the promote environment has no NPM_DEPRECATE_TOKEN",
		`NODE_AUTH_TOKEN=$NPM_DEPRECATE_TOKEN sh "$RUNNER_TEMP/commands.sh"`,
	} {
		if !strings.Contains(job, want) {
			t.Errorf("the unpublish job lacks %q", want)
		}
	}
	// The refusal is the command list's own exit status, written before
	// any npm unpublish runs.
	if strings.Index(job, "unpublish-commands") > strings.Index(job, `sh "$RUNNER_TEMP/commands.sh"`) {
		t.Error("the unpublish job runs commands before writing them")
	}
}
