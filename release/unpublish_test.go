package release

import (
	"os"
	"strings"
	"testing"
)

// tags is npm's dist-tags for @claudinite/cli, with latest on a version
// the tests do not unpublish.
const tags = `{"latest":"1.60930.1","rc":"1.60930.3"}`

// heldBy is every package of an unpublish holding vs, as npm view answers.
func heldBy(vs string) map[string]string {
	out := map[string]string{}
	for _, n := range CLIPackages() {
		out[n] = vs
	}
	return out
}

func TestUnpublishCommands(t *testing.T) {
	t.Parallel()
	u, err := UnpublishCommands(UnpublishInput{DistTags: tags, Version: "1.60930.3", Versions: heldBy(`["1.60930.2","1.60930.3"]`)})
	if err != nil || !u.Exists {
		t.Fatalf("%+v %v", u, err)
	}
	golden(t, "unpublish-commands.txt", strings.Join(u.Commands, "\n")+"\n")
	// A version in the retired <day>.<n>.0 form is still an npm version, and
	// one this action exists to remove.
	u, err = UnpublishCommands(UnpublishInput{DistTags: tags, Version: "61003.1.0", Versions: heldBy(`["61003.1.0","1.61004.1"]`)})
	if err != nil || len(u.Commands) != 6 || u.Commands[0] != "npm unpublish '@claudinite/cli@61003.1.0'" {
		t.Errorf("retired format: %+v %v", u, err)
	}
	for _, c := range u.Commands {
		if strings.Contains(c, "cli-rc") || strings.Contains(c, "sdk") {
			t.Errorf("a package outside the cli family: %s", c)
		}
	}
}

// Unpublishing a package's last version deletes the package, so one such
// package refuses the whole dispatch before any command is written.
func TestUnpublishCommandsRefusesAPackagesOnlyVersion(t *testing.T) {
	t.Parallel()
	in := UnpublishInput{DistTags: tags, Version: "1.60930.3", Versions: heldBy(`["1.60930.2","1.60930.3"]`)}
	in.Versions["@claudinite/cli-darwin-arm64"] = `"1.60930.3"`
	u, err := UnpublishCommands(in)
	if err == nil || len(u.Commands) != 0 || !strings.Contains(err.Error(), "@claudinite/cli-darwin-arm64@1.60930.3 is the only version") {
		t.Errorf("%+v %v", u, err)
	}
	u, err = UnpublishCommands(UnpublishInput{DistTags: tags, Version: "1.60930.3", Versions: heldBy(`["1.60930.3"]`)})
	if err == nil || len(u.Commands) != 0 || strings.Count(err.Error(), "is the only version") != 6 {
		t.Errorf("every package's only version: %+v %v", u, err)
	}
}

// A version npm does not have is not an error, as for deprecate; a
// platform package missing it is left out and named.
func TestUnpublishCommandsForAMissingVersion(t *testing.T) {
	t.Parallel()
	u, err := UnpublishCommands(UnpublishInput{DistTags: tags, Version: "1.60930.9", Versions: heldBy(`["1.60930.2","1.60930.3"]`)})
	if err != nil || u.Exists || len(u.Commands) != 0 || !strings.Contains(u.Notice, "1.60930.9") {
		t.Errorf("%+v %v", u, err)
	}
	in := UnpublishInput{DistTags: tags, Version: "1.60930.3", Versions: heldBy(`["1.60930.2","1.60930.3"]`)}
	in.Versions["@claudinite/cli-windows-x64"] = `{"error":{"code":"E404","summary":"not found"}}`
	u, err = UnpublishCommands(in)
	if err != nil || len(u.Commands) != 5 || !strings.Contains(u.Notice, "@claudinite/cli-windows-x64") {
		t.Errorf("a platform without it: %+v %v", u, err)
	}
}

// The version latest points at is what every stable member is offered;
// unpublishing it would strand them, so promote another first.
func TestUnpublishCommandsRefusesLatest(t *testing.T) {
	t.Parallel()
	in := UnpublishInput{DistTags: `{"latest":"1.60930.3","rc":"1.60930.3"}`, Version: "1.60930.3", Versions: heldBy(`["1.60930.2","1.60930.3"]`)}
	if u, err := UnpublishCommands(in); err == nil || len(u.Commands) != 0 || !strings.Contains(err.Error(), "latest") {
		t.Errorf("%+v %v", u, err)
	}
	for _, unread := range []string{"", `{"error":{"code":"E500","summary":"down"}}`, "{"} {
		in.DistTags = unread
		if u, err := UnpublishCommands(in); err == nil || len(u.Commands) != 0 || !strings.Contains(err.Error(), "dist-tags") {
			t.Errorf("dist-tags %q: %+v %v", unread, u, err)
		}
	}
}

func TestUnpublishCommandsRefuses(t *testing.T) {
	t.Parallel()
	for name, in := range map[string]UnpublishInput{
		"a shell word":   {DistTags: tags, Version: "1.60930.3; rm -rf", Versions: heldBy(`["1.60930.3; rm -rf","1.60930.4"]`)},
		"a quote":        {DistTags: tags, Version: "1.60930.3'", Versions: heldBy(`["1.60930.3'","1.60930.4"]`)},
		"empty":          {DistTags: tags, Version: "", Versions: heldBy(`["","1.60930.4"]`)},
		"bad npm answer": {DistTags: tags, Version: "1.60930.3", Versions: map[string]string{"@claudinite/cli": "{"}},
		"failed read":    {DistTags: tags, Version: "1.60930.3", Versions: map[string]string{"@claudinite/cli": `{"error":{"code":"E500","summary":"down"}}`}},
	} {
		if _, err := UnpublishCommands(in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestPromoteHasUnpublish(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../.github/workflows/promote.yml")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{
		"          - revoke\n          - release\n          - unpublish\n",
		"  unpublish:\n    if: inputs.action == 'unpublish'\n",
		"go run ./release/pipeline unpublish-commands",
		`npm view @claudinite/cli dist-tags --json > "$RUNNER_TEMP/dist-tags.json"`,
		`out=$RUNNER_TEMP/versions/$name.json`,
		`npm view "$name" versions --json > "$out"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("promote.yml lacks %q", want)
		}
	}
	job := jobBlock(t, "../.github/workflows/promote.yml", "unpublish")
	for _, want := range []string{
		"    environment: promote\n",
		"NPM_DEPRECATE_TOKEN: ${{ secrets.NPM_DEPRECATE_TOKEN }}",
		"::error::the promote environment has no NPM_DEPRECATE_TOKEN",
		`NODE_AUTH_TOKEN=$NPM_DEPRECATE_TOKEN sh -e "$RUNNER_TEMP/commands.sh"`,
		"CONFIRM: ${{ inputs.confirm }}",
		`if [ "$CONFIRM" != "$VERSION" ]; then`,
	} {
		if !strings.Contains(job, want) {
			t.Errorf("the unpublish job lacks %q", want)
		}
	}
	// The refusal is the command list's own exit status, written before
	// any npm unpublish runs.
	if strings.Index(job, "unpublish-commands") > strings.Index(job, `sh -e "$RUNNER_TEMP/commands.sh"`) {
		t.Error("the unpublish job runs commands before writing them")
	}
	// The confirmation is checked before npm is asked anything.
	if strings.Index(job, `"$CONFIRM" != "$VERSION"`) > strings.Index(job, "npm view") {
		t.Error("the unpublish job reads npm before checking confirm")
	}
	// A failed npm view other than npm's E404 fails the job rather than
	// reading as a version npm does not have.
	if strings.Contains(job, "|| true") || !strings.Contains(job, "E404") {
		t.Error("the unpublish job swallows a failed npm view")
	}
}
