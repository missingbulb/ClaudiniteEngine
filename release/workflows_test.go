package release

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
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

// An expression expanded inside a run: body is spliced into the script
// before the shell parses it; through env: it arrives as data.
func TestNoExpressionInARunBody(t *testing.T) {
	t.Parallel()
	for _, wf := range []string{"../.github/workflows/release.yml", "../.github/workflows/promote.yml"} {
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
	files, _ := filepath.Glob("../.github/workflows/*.yml")
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
	raw, err := os.ReadFile("../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`go run github\.com/rhysd/actionlint/cmd/actionlint@[0-9a-f]{40}\b`).Match(raw) {
		t.Error("ci.yml does not run actionlint at a commit SHA")
	}
	// The member workflow templates ship inside the binary; CI lints them
	// beside the engine's own.
	if !regexp.MustCompile(`actionlint@[0-9a-f]{40} \.github/workflows/\*\.yml lifecycle/workflows/templates/\*\.yml`).Match(raw) {
		t.Error("ci.yml's actionlint does not lint both .github/workflows and the member templates")
	}
	conf, err := os.ReadFile("../.github/actionlint.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^\s+- macos-15-intel$`).Match(conf) {
		t.Errorf(".github/actionlint.yaml does not declare macos-15-intel:\n%s", conf)
	}
}

// jobBlock is one job's text in a workflow: from "  <name>:" to the next
// job at the same indentation.
func jobBlock(t *testing.T, wf, name string) string {
	t.Helper()
	raw, err := os.ReadFile(wf)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	i := strings.Index(s, "\n  "+name+":\n")
	if i < 0 {
		return ""
	}
	rest := s[i+1:]
	if j := regexp.MustCompile(`\n  [a-z][a-z0-9-]*:\n`).FindStringIndex(rest[1:]); j != nil {
		rest = rest[:j[0]+1]
	}
	return rest
}

// release.yml's kind is a required choice: a full release candidate, or a
// staging build.
func TestReleaseTakesAKind(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../.github/workflows/release.yml")
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
	const wf = "../.github/workflows/release.yml"
	hop := jobBlock(t, wf, "hop")
	if hop == "" {
		t.Fatal("release.yml has no hop job")
	}
	for _, want := range []string{"    needs: build\n", "    if: inputs.kind == 'full'\n", "    permissions:\n      contents: read\n", "release/hop.sh"} {
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
		if jobBlock(t, wf, gone) != "" {
			t.Errorf("release.yml still has a %s job of its own", gone)
		}
	}
	build := jobBlock(t, wf, "build")
	if !strings.Contains(build, `release/version.sh next --taken "$RUNNER_TEMP/taken.json"`) {
		t.Errorf("the build job may pick a version npm already holds:\n%s", build)
	}
	if !strings.Contains(build, "release/smoke-platform.sh --registry") || strings.Index(build, "release/smoke-platform.sh") > strings.Index(build, "upload-artifact") {
		t.Errorf("the build job does not test initial adoption before it uploads the dist:\n%s", build)
	}
	publish := jobBlock(t, wf, "publish")
	for _, want := range []string{
		"    needs: [build, hop]\n",
		"    environment: release\n",
		"      id-token: write\n",
		"!cancelled()",
		"needs.build.result == 'success'",
		"      contents: write\n",
		"(needs.hop.result == 'success' || (inputs.kind == 'staging' && needs.hop.result == 'skipped'))",
		"CN_RELEASE_KEY: ${{ secrets.CN_RELEASE_KEY }}",
		"go run ./release/manifest verify --dist dist --roots shared/trust/roots",
	} {
		if !strings.Contains(publish, want) {
			t.Errorf("the publish job lacks %q:\n%s", want, publish)
		}
	}
	if strings.Contains(publish, "always()") && !strings.Contains(publish, "      - if: always()\n        run: rm -f") {
		t.Errorf("the publish job runs on always(), which a cancelled run satisfies:\n%s", publish)
	}
	// The order a signed candidate is published in: the bytes build hashed,
	// the signature, the key gone, the signature verified, the version's tag,
	// then npm. The tag comes first so a run that fails after npm took the
	// version never leaves the next run to compute it again.
	order := []string{"name: the files build hashed", "release/sign.sh", "run: rm -f \"$RUNNER_TEMP/release.key\"", "go run ./release/manifest verify", "publish-mode", "git push origin \"v$VERSION\"", "release/mirror.sh put \"$VERSION\" dist", "release/publish.sh", "pipeline npm-holds --dist dist"}
	last, prev := -1, "the start"
	for _, step := range order {
		i := strings.Index(publish, step)
		if i <= last {
			t.Errorf("the publish job does not run %q after %q", step, prev)
		}
		last, prev = i, step
	}
	if !strings.Contains(publish, "      - if: steps.mode.outputs.mode == 'real'\n        name: mirror the tarballs for the minutes npm answers 404\n        env:\n          GH_TOKEN: ${{ secrets.MIRROR_TOKEN }}\n") {
		t.Errorf("the publish job does not mirror only a real publish, with the mirror's token:\n%s", publish)
	}
	if !strings.Contains(publish, "      - if: steps.mode.outputs.mode == 'real'\n        name: tag the commit\n") {
		t.Errorf("the publish job does not tag only after a real publish:\n%s", publish)
	}
	if !strings.Contains(publish, "      - if: steps.mode.outputs.mode == 'real'\n        name: npm holds the bytes this run built\n") {
		t.Errorf("the publish job does not check npm's integrity only after a real publish:\n%s", publish)
	}
	fromNPM := jobBlock(t, wf, "from-npm")
	for _, want := range []string{
		"    if: ${{ !cancelled() && needs.publish.result == 'success' && needs.publish.outputs.mode == 'real' }}\n",
		"    permissions:\n      actions: write\n    steps:",
		"gh workflow run from-npm.yml --repo \"$GITHUB_REPOSITORY\" --ref \"v$VERSION\"",
	} {
		if !strings.Contains(fromNPM, want) {
			t.Errorf("the from-npm job lacks %q:\n%s", want, fromNPM)
		}
	}
	if strings.Contains(fromNPM, "npm-wait") || jobBlock(t, wf, "smoke-published") != "" {
		t.Error("release.yml still waits for npm to serve its tarballs")
	}
}

// Every release publishes @claudinite/cli under its kind's dist-tag, and
// no workflow names the retired rc package.
func TestWorkflowsPublishOnePackageFamily(t *testing.T) {
	t.Parallel()
	files, _ := filepath.Glob("../.github/workflows/*.yml")
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		for i, l := range strings.Split(string(raw), "\n") {
			if strings.Contains(l, "release/publish.sh") && !strings.Contains(l, "--tag ") {
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
	const wf = "../.github/workflows/promote.yml"
	job := jobBlock(t, wf, "promote")
	for _, want := range []string{
		"    needs: [gate, check, full]\n",
		"    if: needs.check.outputs.check == 'pass' && needs.full.result == 'success'\n",
		"    environment: promote\n",
		"      id-token: write\n",
		"npm install -g npm@11.21.0",
		`npm dist-tag add "@claudinite/cli@$VERSION" latest`,
	} {
		if !strings.Contains(job, want) {
			t.Errorf("the promote job lacks %q:\n%s", want, job)
		}
	}
	raw, _ := os.ReadFile(wf)
	for _, not := range []string{"npm publish", "release/publish.sh", "secrets.NPM_TOKEN"} {
		if strings.Contains(string(raw), not) {
			t.Errorf("promote.yml has %q", not)
		}
	}
}

// Only stable waits on the full check, run on the promoted version's own
// commit; a release, staging or rc, never calls it.
func TestOnlyStableWaitsOnTheFullCheck(t *testing.T) {
	t.Parallel()
	full := jobBlock(t, "../.github/workflows/promote.yml", "full")
	for _, want := range []string{
		"    uses: ./.github/workflows/full.yml\n",
		"      ref: v${{ inputs.version }}\n",
	} {
		if !strings.Contains(full, want) {
			t.Errorf("promote.yml's full job lacks %q:\n%s", want, full)
		}
	}
	raw, _ := os.ReadFile("../.github/workflows/full.yml")
	if strings.Count(string(raw), "ref: ${{ inputs.ref }}") != 4 {
		t.Errorf("full.yml does not check out inputs.ref in each of its four jobs")
	}
	rel, _ := os.ReadFile("../.github/workflows/release.yml")
	if strings.Contains(string(rel), "full.yml") {
		t.Error("release.yml calls the full check, which only stable waits on")
	}
}

// The live-packs rehearsal runs in the release straight after the build, holding
// promotion through a release-blocker issue, and in its own workflow nightly
// and on demand, never on a pull request or a push.
func TestLivePacksRuns(t *testing.T) {
	t.Parallel()
	const wf = "../.github/workflows/release.yml"
	job := jobBlock(t, wf, "live-packs")
	if job == "" {
		t.Fatal("release.yml has no live-packs job")
	}
	invocation := regexp.MustCompile(`(?m)^\s*- run: release/rehearse\.sh --mode live-packs\b`)
	for _, want := range []*regexp.Regexp{
		regexp.MustCompile(`(?m)^    needs: build$`),
		regexp.MustCompile(`(?m)^    if: inputs.kind == 'full'$`),
		invocation,
		regexp.MustCompile(`(?m)^\s*go run \./release/pipeline blocker-issue --gate live-packs\b`),
		regexp.MustCompile(`(?m)^\s*gh issue create .*--label release-blocker`),
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
	own := jobBlock(t, "../.github/workflows/live-packs.yml", "live-packs")
	if !invocation.MatchString(own) {
		t.Errorf("live-packs.yml does not run the mode:\n%s", own)
	}
	raw, err := os.ReadFile("../.github/workflows/live-packs.yml")
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

// from-npm.yml waits on npm through release/npm-wait.sh, which keeps the
// launcher's URLs untouched until npm serves them, then installs the
// release on a clean repo and opens a release-blocker issue on failure.
func TestFromNPMWaitsThroughNPMWait(t *testing.T) {
	t.Parallel()
	job := jobBlock(t, "../.github/workflows/from-npm.yml", "from-npm")
	if job == "" {
		t.Fatal("from-npm.yml has no from-npm job")
	}
	if !regexp.MustCompile(`(?m)^\s*run: sh release/npm-wait\.sh --package @claudinite/cli --version `).MatchString(job) {
		t.Errorf("from-npm does not wait through release/npm-wait.sh:\n%s", job)
	}
	for _, want := range []string{"release/smoke-platform.sh --registry https://registry.npmjs.org", "release/pipeline blocker-issue", "      issues: write\n"} {
		if !strings.Contains(job, want) {
			t.Errorf("from-npm lacks %q:\n%s", want, job)
		}
	}
	if strings.Contains(job, "curl ") {
		t.Errorf("from-npm looks at npm itself:\n%s", job)
	}
	// Once npm serves the release the mirror's copy goes, and failing to
	// drop it is no reason to hold the release back.
	drop := strings.Index(job, "run: release/mirror.sh drop \"$VERSION\"")
	if drop < 0 || drop < strings.Index(job, "release/smoke-platform.sh") || !strings.Contains(job, "          GH_TOKEN: ${{ secrets.MIRROR_TOKEN }}\n        run: release/mirror.sh drop") {
		t.Errorf("from-npm does not drop the mirror's copy after installing from npm:\n%s", job)
	}
	if !strings.Contains(job, "        if: failure() && steps.drop.outcome != 'failure'\n") {
		t.Errorf("a failed drop opens a release-blocker issue:\n%s", job)
	}
}
