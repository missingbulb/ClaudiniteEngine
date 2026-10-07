package test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/missingbulb/ClaudiniteEngine/dev/internal/scripttest"
	relpublish "github.com/missingbulb/ClaudiniteEngine/dev/release/publish"
)

// runBlocks returns each step's run: body in a workflow file, by indentation:
// the text after "run:" and every following line indented deeper than the
// key.
func runBlocks(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(raw), "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimLeft(lines[i], " ")
		key := strings.TrimPrefix(trimmed, "- ")
		if !strings.HasPrefix(key, "run:") {
			continue
		}
		indent := len(lines[i]) - len(trimmed) + len(trimmed) - len(key)
		body := []string{strings.TrimPrefix(key, "run:")}
		for i+1 < len(lines) {
			next := lines[i+1]
			if strings.TrimSpace(next) != "" && len(next)-len(strings.TrimLeft(next, " ")) <= indent {
				break
			}
			body = append(body, next)
			i++
		}
		out = append(out, strings.Join(body, "\n"))
	}
	return out
}

// read is a repository file, named from the root, as text.
func read(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(scripttest.Path(t, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// A workflow is an entry point: each run: step is one command, a line with
// its backslash continuations, and whatever it does beyond that lives in a
// script or a Go command under dev/, where it can be run and tested.
func TestEveryRunStepIsOneCommand(t *testing.T) {
	t.Parallel()
	files, _ := filepath.Glob(scripttest.Path(t, ".github/workflows/*.yml"))
	if len(files) == 0 {
		t.Fatal("no workflows found")
	}
	for _, wf := range files {
		for _, b := range runBlocks(t, wf) {
			body := strings.TrimLeft(b, " ")
			body = strings.TrimLeft(strings.TrimPrefix(strings.TrimPrefix(body, "|"), ">"), "\n")
			var commands []string
			for _, l := range strings.Split(strings.ReplaceAll(body, "\\\n", " "), "\n") {
				if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
					commands = append(commands, l)
				}
			}
			if len(commands) > 1 {
				t.Errorf("%s: a run: step holds %d commands; move them into a script under dev/:\n%s", filepath.Base(wf), len(commands), b)
			}
		}
	}
}

// An expression expanded inside a run: body is spliced into the script
// before the shell parses it; through env: it arrives as data.
func TestNoExpressionInARunBody(t *testing.T) {
	t.Parallel()
	for _, wf := range []string{"../../.github/workflows/release.yml", "../../.github/workflows/promote.yml"} {
		blocks := runBlocks(t, wf)
		if len(blocks) == 0 {
			t.Fatalf("%s: no run: blocks found", wf)
		}
		for _, b := range blocks {
			if strings.Contains(b, "${{") {
				t.Errorf("%s: a run: body expands an expression; move it to env:\n%s", wf, b)
			}
		}
	}
}

var (
	stepUses   = regexp.MustCompile(`^\s*(?:- )?uses:\s*\S`)
	pinnedUses = regexp.MustCompile(`^\s*(?:- )?uses:\s*[^@\s]+@[0-9a-f]{40}(?:\s+#.*)?$`)
	// A job calling one of this repository's own workflows runs it from the calling commit.
	ownWorkflow = regexp.MustCompile(`^    uses: \./\.github/workflows/[a-z0-9-]+\.yml$`)
)

// unpinnedUse reports a step's uses: that names an action by anything but a commit SHA. A bare
// `uses:` key with no value is a workflow_dispatch input of that name, not a step.
func unpinnedUse(line string) bool {
	return stepUses.MatchString(line) && !pinnedUses.MatchString(line) && !ownWorkflow.MatchString(line)
}

func TestUnpinnedUse(t *testing.T) {
	t.Parallel()
	for line, want := range map[string]bool{
		"      - uses: actions/checkout@v4":                                                true,
		"      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1": false,
		"        uses: ./local-action":                                                     true,
		"    uses: ./.github/workflows/full.yml":                                           false,
		"    uses: acme/other/.github/workflows/full.yml@main":                             true,
		"      uses:": false,
	} {
		if got := unpinnedUse(line); got != want {
			t.Errorf("unpinnedUse(%q) = %v, want %v", line, got, want)
		}
	}
}

func TestEveryActionIsPinnedBySHA(t *testing.T) {
	t.Parallel()
	files, _ := filepath.Glob("../../.github/workflows/*.yml")
	if len(files) == 0 {
		t.Fatal("no workflows found")
	}
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		for i, l := range strings.Split(string(raw), "\n") {
			if unpinnedUse(l) {
				t.Errorf("%s:%d: not pinned by commit SHA: %s", f, i+1, strings.TrimSpace(l))
			}
		}
	}
}

func TestCIRunsActionlintPinnedBySHA(t *testing.T) {
	t.Parallel()
	if !strings.Contains(scripttest.JobBlock(t, ".github/workflows/ci.yml", "check"), "      - run: dev/test/check.sh\n") {
		t.Error("ci.yml does not run dev/test/check.sh")
	}
	raw := []byte(read(t, "dev/test/check.sh"))
	if !regexp.MustCompile(`go run github\.com/rhysd/actionlint/cmd/actionlint@[0-9a-f]{40}\b`).Match(raw) {
		t.Error("check.sh does not run actionlint at a commit SHA")
	}
	// The member workflow templates ship inside the binary; CI lints them
	// beside the engine's own.
	if !regexp.MustCompile(`actionlint@[0-9a-f]{40} \.github/workflows/\*\.yml cn/lifecycle/workflows/templates/\*\.yml`).Match(raw) {
		t.Error("check.sh's actionlint does not lint both .github/workflows and the member templates")
	}
	conf, err := os.ReadFile("../../.github/actionlint.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^\s+- macos-15-intel$`).Match(conf) {
		t.Errorf(".github/actionlint.yaml does not declare macos-15-intel:\n%s", conf)
	}
}

// release.yml's kind is a required choice: a full release candidate, or a
// staging build.
func TestReleaseTakesAKind(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	want := "      kind:\n        description: "
	i := strings.Index(string(raw), want)
	if i < 0 {
		t.Fatal("release.yml has no kind input")
	}
	input := string(raw)[i:]
	input = input[:strings.Index(input, "\n      dry_run:")]
	for _, w := range []string{"        required: true\n", "        type: choice\n", "        options:\n          - staging\n          - full"} {
		if !strings.Contains(input, w) {
			t.Errorf("the kind input lacks %q:\n%s", w, input)
		}
	}
	if strings.Contains(input, "default:") {
		t.Errorf("the kind input has a default:\n%s", input)
	}
}

// The build job tests initial adoption on its own dist; the hop runs
// straight after it; the publish job, which signs, waits for both, so
// nothing unproven is signed or published, and tags the commit itself. A
// staging build skips the hop, and only a staging build may.
func TestReleaseGatesThePublish(t *testing.T) {
	t.Parallel()
	const wf = ".github/workflows/release.yml"
	hop := scripttest.JobBlock(t, wf, "hop")
	if hop == "" {
		t.Fatal("release.yml has no hop job")
	}
	for _, want := range []string{"    needs: build\n", "    if: inputs.kind == 'full'\n", "    permissions:\n      contents: read\n", "dev/release/verify/hop.sh"} {
		if !strings.Contains(hop, want) {
			t.Errorf("the hop job lacks %q:\n%s", want, hop)
		}
	}
	for _, not := range []string{"secrets.", "environment:"} {
		if strings.Contains(hop, not) {
			t.Errorf("the hop job has %q", not)
		}
	}
	for _, gone := range []string{"sign", "smoke", "tag"} {
		if scripttest.JobBlock(t, wf, gone) != "" {
			t.Errorf("release.yml still has a %s job of its own", gone)
		}
	}
	build := scripttest.JobBlock(t, wf, "build")
	if !strings.Contains(build, "run: dev/build/next-version.sh\n") || !strings.Contains(read(t, "dev/build/next-version.sh"), `version.sh next --taken "$taken"`) {
		t.Errorf("the build job may pick a version npm already holds:\n%s", build)
	}
	if !strings.Contains(build, "run: dev/release/verify/adoption.sh --channel") || strings.Index(build, "dev/release/verify/adoption.sh") > strings.Index(build, "upload-artifact") {
		t.Errorf("the build job does not test initial adoption before it uploads the dist:\n%s", build)
	}
	if !strings.Contains(read(t, "dev/release/verify/adoption.sh"), "dev/release/verify/smoke-platform.sh --registry") {
		t.Error("adoption.sh does not run the smoke leg")
	}
	publish := scripttest.JobBlock(t, wf, "publish")
	for _, want := range []string{
		"    needs: [build, hop]\n",
		"    environment: release\n",
		"      id-token: write\n",
		"!cancelled()",
		"needs.build.result == 'success'",
		"      contents: write\n",
		"(needs.hop.result == 'success' || (inputs.kind == 'staging' && needs.hop.result == 'skipped'))",
		"CN_RELEASE_KEY: ${{ secrets.CN_RELEASE_KEY }}",
	} {
		if !strings.Contains(publish, want) {
			t.Errorf("the publish job lacks %q:\n%s", want, publish)
		}
	}
	if strings.Contains(publish, "always()") {
		t.Errorf("the publish job runs on always(), which a cancelled run satisfies:\n%s", publish)
	}
	// The order a signed candidate is published in: the bytes build hashed,
	// the signature, the key gone, the signature verified against the roots
	// a released cn trusts, the version's tag, then npm. The tag comes first
	// so a run that fails after npm took the version never leaves the next
	// run to compute it again.
	inOrder := func(where, text string, steps ...string) {
		last, prev := -1, "the start"
		for _, step := range steps {
			i := strings.Index(text, step)
			if i <= last {
				t.Errorf("%s does not run %q after %q", where, step, prev)
			}
			last, prev = i, step
		}
	}
	inOrder("the publish job", publish, "name: the files build hashed", "dev/release/create/sign-release.sh", "dev/release/publish/mode.sh", "dev/release/publish/tag.sh", "dev/release/publish/publish.sh", "pipeline npm-holds --dist dist")
	inOrder("sign-release.sh", read(t, "dev/release/create/sign-release.sh"), "dev/release/create/sign.sh", `rm -rf "$keys"`, "go run ./dev/release/create/manifest verify --dist \"${DIST:-dist}\" --roots cn/shared/trust/roots")
	inOrder("tag.sh", read(t, "dev/release/publish/tag.sh"), `git tag "v$1"`, `git push origin "v$1"`)
	if !strings.Contains(publish, "      - if: steps.mode.outputs.mode == 'real'\n        name: tag the commit\n") {
		t.Errorf("the publish job does not tag only after a real publish:\n%s", publish)
	}
	if !strings.Contains(publish, "      - if: steps.mode.outputs.mode == 'real'\n        name: npm holds the bytes this run built\n") {
		t.Errorf("the publish job does not check npm's integrity only after a real publish:\n%s", publish)
	}
	fromNPM := scripttest.JobBlock(t, wf, "from-npm")
	for _, want := range []string{
		"    if: ${{ !cancelled() && needs.publish.result == 'success' && needs.publish.outputs.mode == 'real' }}\n",
		"    permissions:\n      actions: write\n    steps:",
		"gh workflow run from-npm.yml --repo \"$GITHUB_REPOSITORY\" --ref \"v$VERSION\"",
	} {
		if !strings.Contains(fromNPM, want) {
			t.Errorf("the from-npm job lacks %q:\n%s", want, fromNPM)
		}
	}
	// A timed-out npm-holds prints the dispatch this job would have run;
	// the two must stay one command.
	dispatch := fromNPM[strings.Index(fromNPM, "gh workflow run"):]
	end := strings.Index(dispatch, `-f channel="$CHANNEL"`) + len(`-f channel="$CHANNEL"`)
	dispatch = strings.Join(strings.Fields(strings.ReplaceAll(dispatch[:end], "\\\n", "")), " ")
	dispatch = strings.NewReplacer(`"`, "", "$GITHUB_REPOSITORY", "o/r", "$VERSION", "1.61005.9", "$INTEGRITY", "sha512-x", "$CHANNEL", "canary").Replace(dispatch)
	if got := relpublish.FromNPMDispatch("o/r", "1.61005.9", "sha512-x", "canary"); got != dispatch {
		t.Errorf("npm-holds prints\n  %s\nbut the from-npm job runs\n  %s", got, dispatch)
	}
	if !strings.Contains(publish, "CHANNEL: ${{ needs.build.outputs.channel }}") ||
		!strings.Contains(publish, `pipeline npm-holds --dist dist --version "$VERSION" --channel "$CHANNEL" --repo "$GITHUB_REPOSITORY"`) {
		t.Errorf("the publish job does not hand npm-holds the channel and repository its way-on message names:\n%s", publish)
	}
	if strings.Contains(fromNPM, "npm-wait") || scripttest.JobBlock(t, wf, "smoke-published") != "" {
		t.Error("release.yml still waits for npm to serve its tarballs")
	}
}

// Every release publishes @claudinite/cli under its kind's dist-tag, and
// no workflow names the retired rc package.
func TestWorkflowsPublishOnePackageFamily(t *testing.T) {
	t.Parallel()
	files, _ := filepath.Glob("../../.github/workflows/*.yml")
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		for i, l := range strings.Split(string(raw), "\n") {
			if strings.Contains(l, "dev/release/publish/publish.sh") && !strings.Contains(l, "--tag ") {
				t.Errorf("%s publishes without an explicit --tag: %s", f, strings.TrimSpace(l))
			}
			if strings.Contains(l, "cli-rc") {
				t.Errorf("%s:%d names the retired @claudinite/cli-rc: %s", f, i+1, strings.TrimSpace(l))
			}
		}
	}
}

// Promotion moves latest and republishes nothing.
func TestPromoteMovesLatest(t *testing.T) {
	t.Parallel()
	const wf = ".github/workflows/promote.yml"
	job := scripttest.JobBlock(t, wf, "promote")
	for _, want := range []string{
		"    needs: [gate, check, full]\n",
		"    if: needs.check.outputs.check == 'pass' && needs.full.result == 'success'\n",
		"    environment: promote\n",
		"      id-token: write\n",
		"npm install -g npm@11.21.0",
		`run: dev/release/publish/latest.sh "$VERSION"`,
	} {
		if !strings.Contains(job, want) {
			t.Errorf("the promote job lacks %q:\n%s", want, job)
		}
	}
	if !strings.Contains(read(t, "dev/release/publish/latest.sh"), `npm dist-tag add "@claudinite/cli@$version" latest`) {
		t.Error("latest.sh does not move latest")
	}
	raw, _ := os.ReadFile(scripttest.Path(t, wf))
	for _, not := range []string{"npm publish", "dev/release/publish/publish.sh", "secrets.NPM_TOKEN"} {
		if strings.Contains(string(raw), not) {
			t.Errorf("promote.yml has %q", not)
		}
	}
}

// Only stable waits on the full check, run on the promoted version's own
// commit; a release, staging or rc, never calls it.
func TestOnlyStableWaitsOnTheFullCheck(t *testing.T) {
	t.Parallel()
	full := scripttest.JobBlock(t, ".github/workflows/promote.yml", "full")
	for _, want := range []string{
		"    uses: ./.github/workflows/full.yml\n",
		"      ref: v${{ inputs.version }}\n",
	} {
		if !strings.Contains(full, want) {
			t.Errorf("promote.yml's full job lacks %q:\n%s", want, full)
		}
	}
	raw, _ := os.ReadFile("../../.github/workflows/full.yml")
	if strings.Count(string(raw), "ref: ${{ inputs.ref }}") != 4 {
		t.Errorf("full.yml does not check out inputs.ref in each of its four jobs")
	}
	rel, _ := os.ReadFile("../../.github/workflows/release.yml")
	if strings.Contains(string(rel), "full.yml") {
		t.Error("release.yml calls the full check, which only stable waits on")
	}
}

// The live-packs rehearsal runs in the release straight after the build, holding
// promotion through a release-blocker issue, and in its own workflow nightly
// and on demand, never on a pull request or a push.
func TestLivePacksRuns(t *testing.T) {
	t.Parallel()
	const wf = ".github/workflows/release.yml"
	job := scripttest.JobBlock(t, wf, "live-packs")
	if job == "" {
		t.Fatal("release.yml has no live-packs job")
	}
	invocation := regexp.MustCompile(`(?m)^\s*- run: dev/release/verify/rehearse\.sh --mode live-packs\b`)
	for _, want := range []*regexp.Regexp{
		regexp.MustCompile(`(?m)^    needs: build$`),
		regexp.MustCompile(`(?m)^    if: inputs.kind == 'full'$`),
		invocation,
		regexp.MustCompile(`(?m)^\s*run: dev/release/verify/blocker-issue\.sh --gate live-packs\b`),
	} {
		if !want.MatchString(job) {
			t.Errorf("the live-packs job lacks %s:\n%s", want, job)
		}
	}
	for _, not := range []string{"secrets.", "environment:"} {
		if strings.Contains(job, not) {
			t.Errorf("the live-packs job has %q", not)
		}
	}
	if !regexp.MustCompile(`(?m)^\s*gh issue create .*--label release-blocker`).MatchString(read(t, "dev/release/verify/blocker-issue.sh")) {
		t.Error("blocker-issue.sh opens no release-blocker issue")
	}
	own := scripttest.JobBlock(t, ".github/workflows/live-packs.yml", "live-packs")
	if !strings.Contains(own, "      - run: dev/release/verify/live-packs.sh\n") || !strings.Contains(read(t, "dev/release/verify/live-packs.sh"), "sh dev/release/verify/rehearse.sh --mode live-packs\n") {
		t.Errorf("live-packs.yml does not run the mode:\n%s", own)
	}
	raw, err := os.ReadFile("../../.github/workflows/live-packs.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, trigger := range []string{"\n  schedule:\n", "\n  workflow_dispatch:\n"} {
		if !strings.Contains(string(raw), trigger) {
			t.Errorf("live-packs.yml lacks the trigger %q", trigger)
		}
	}
	for _, trigger := range []string{"\n  push:", "\n  pull_request:"} {
		if strings.Contains(string(raw), trigger) {
			t.Errorf("live-packs.yml has the trigger %q", trigger)
		}
	}
}

// from-npm.yml waits on npm through dev/release/verify/npm-wait.sh, which keeps the
// launcher's URLs untouched until npm serves them, then installs the
// release on a clean repo and opens a release-blocker issue on failure.
func TestFromNPMWaitsThroughNPMWait(t *testing.T) {
	t.Parallel()
	job := scripttest.JobBlock(t, ".github/workflows/from-npm.yml", "from-npm")
	if job == "" {
		t.Fatal("from-npm.yml has no from-npm job")
	}
	if !regexp.MustCompile(`(?m)^\s*run: sh dev/release/verify/npm-wait\.sh --package @claudinite/cli --version `).MatchString(job) {
		t.Errorf("from-npm does not wait through dev/release/verify/npm-wait.sh:\n%s", job)
	}
	for _, want := range []string{"dev/release/verify/smoke-platform.sh --registry https://registry.npmjs.org", "dev/release/verify/blocker-issue.sh --leg", "      issues: write\n"} {
		if !strings.Contains(job, want) {
			t.Errorf("from-npm lacks %q:\n%s", want, job)
		}
	}
	if strings.Contains(job, "curl ") {
		t.Errorf("from-npm looks at npm itself:\n%s", job)
	}
}
