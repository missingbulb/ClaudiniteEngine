> Approved by Ariel on 2026-10-01. Source: [Claude Doc](https://claude.ai/code/artifact/f2b744d0-f95d-4a65-a6e2-45434b3e9aef). This repo copy is now the canonical version; change it here.

# Release testing for engine and pack updates

Sep 30, 2026 · @Ariel Raunstien

Every engine and pack release passes the same four stages, and the last gate before members is a small fleet of canary repos running the real update. The canaries install engines from the `rc` dist-tag of `@claudinite/cli` and everyone else from `latest`, which only ever moves to releases the canaries passed. Decisions, research, rejected alternatives and the security review are on the Record tab.

## At a glance

&#91;embedded content: release gates · 4 stages\]

Cheap tests run on every pull request; the expensive ones run once per candidate; the canaries prove it on real repos; only then does the stable channel open.

## What gets released, and what can break

Two things ship on separate paths and reach a member in separate PRs, so each is tested against what the member already holds of the other.

| Release | What ships | How it reaches a member | Held fixed while it lands |
| --- | --- | --- | --- |
| Engine | The `cn` binary for five platforms, its manifest, the embedded runner script and SDK, the updater and its verify | The engine update runs the new engine's verify against the repo, then opens a PR that moves the pin and carries the launcher that engine ships (Record decision 155) | The member's committed packs and its own files |
| Pack | One pack's vendored set at one version: rules, skills, declared checks, coded checks, tasks | The pack update runs the pinned engine's checks with the new packs, then opens a PR with pack changes only | The member's pinned engine |

No update rewrites a member's files. Within a major the engine only adds and deprecates, so a release must keep every file shape the major has accepted working; a major removes old shapes, and a person fixes what verify reports in a Claude session.

The failures worth a gate, by who pays for them:

| Failure | Who pays | Caught by |
| --- | --- | --- |
| A guard blocks ordinary tool calls, or a hook crashes or slows down | Every session on the release: coding stops | Hook replay, synthetic session probe, latency budget |
| The engine does not start on one platform or shell | Every desktop on that platform | Platform smoke matrix |
| The updater in a release cannot update away from itself | Every member on it, permanently, since the fix cannot arrive | Hop test |
| Verify passes a repo the update then breaks, or blocks one it would not | Broken members, or members stuck on old versions | Verify accuracy on every canary |
| A release within a major stops accepting a file shape it accepted before | Members whose files have that shape | Shape corpus, Lagging canary |
| A major removes a shape without having warned about it, or verify's report is not enough to fix the repo | Every member, each needing a person | Deprecation match and a canary fixed from the report alone |
| A new engine breaks a pack version members still hold | Members whose packs lag | Pack corpus matrix |
| A pack uses engine behavior newer than its `minEngineVersion` | Members on older engines | Engine floor matrix |
| A check fires falsely and turns CI red | Every member: their updates stop, since updates skip while main is red | Differential findings on canaries |
| A workflow patch in an update's issue does not apply or does not turn its task back on | Members who need that task | A canary applies the patch |

The gates below are layered so the cheap ones run on every pull request and the expensive, real ones run once per release.

## Engine release gates

An engine version is built once and published to `@claudinite/cli` under the `rc` dist-tag, and promotion moves the package's `latest` tag onto it only after the canaries pass; the bits a canary tested are the bits every member gets, because promotion republishes nothing.

### On every pull request in ClaudiniteEngine

1. **Unit and contract tests.** Go tests, plus the SDK protocol suite: every message shape the binary sends and accepts, run against the SDK in the tree and the last three published SDKs. Why: a pack imports the SDK version it was written for, so the pipe must stay readable from both sides.
2. **Fixture rehearsals.** Today's `vendoring/rehearsal` harness, rebuilt around the binary: build a fixture member in a temp dir, run the real `cn update` with its verify, then `cn selftest` and `cn check world`. It keeps its fresh, current and stale modes. Why: seconds per run, and the first place an update path breaks.
3. **Shape corpus.** A fixture for every shape of `.claudinite/settings.*`, pack declaration and local pack that any earlier release of the current major accepted, each added when its release shipped. Every one must still load, run its checks, and raise only deprecation findings. Why: within a major the engine promises to only add, and nothing rewrites member files, so an old shape that stops working breaks members with no warning.
4. **Hook replay.** A corpus of real hook inputs (SessionStart, PreToolUse, PostToolUse, Stop) captured from our own repos' sessions is replayed through the PR's binary and through the current stable, with the same packs. Any verdict that changes fails unless the PR says which rule change caused it. Why: a guard that blocks ordinary work is the most expensive failure we can ship, and a before-and-after diff finds it without anyone writing an assertion for it.
5. **Latency budget.** The replay also times each hook. A p95 more than 20% or 50 ms slower than stable fails. Why: the binary runs on every tool call, so a slowdown is paid by every session all day.

### Release kinds

`release.yml` takes a required `kind`:

- **full** builds the five binaries, checks the linux-x64 build reproduces, runs every job below and publishes under the `rc` dist-tag. It is the only kind that can be promoted.
- **staging** builds linux-x64 alone, runs only the secret scan and the platform smoke, and publishes under the `staging` dist-tag, which only repos whose settings set `engine.channel: "staging"` read; the owner opts his own repos in to try a change on real work within minutes. Its manifest lists one platform, so promotion refuses it. Right after the tag and before npm, `dev/release/publish/distro.sh` uploads the build's two linux-x64 tarballs as release `v<version>` of the repository `dev/release/publish/staging-distro` names (ClaudiniteStagingDistro), marked latest, with a `release.json` naming the version, its pin and the engine commit. A repo whose settings set `engine.releases` to that repository fetches the build from there as soon as the upload ends, and its nightly update reads its candidate from that `release.json` (Record decision 154).

Both kinds tag the commit just before publishing, so a run that fails after npm took its version never hands that version to the next run. Right after publishing they check that npm's version documents name the tarballs they built, then start `from-npm.yml`, which installs the release from npm once npm serves its tarballs, minutes later, and opens a release-blocker issue if that fails. The release does not wait for it (Record decision 24).

### Working on a staging build from a session

A session in a repo whose `engine.releases` names the staging distro, waiting on a staging build of engine commit `S`, waits on the build rather than on the release run: an until-loop whose condition is that `https://github.com/missingbulb/ClaudiniteStagingDistro/releases/latest/download/release.json` names `"commit": "S"`. That URL answers anonymously from Actions and from web sessions. Once it does, the session writes the `version` and `manifest` it names into its working tree's `engine.version` and `engine.manifest`, runs `sh .claudinite/launch env install`, and the relinked `.claudinite/bin/cn` is the new build for the rest of the session. It does not commit that pin, since the pin guard refuses a person's pin move: `main` takes the build through its update PR, at once by dispatching the repo's update workflow, or at the next nightly run.

### Per release candidate

A full release builds the five binaries once, then runs these jobs against the built files, with no publish credential in any of them.

1. **Platform smoke matrix.** On GitHub-hosted Linux x64 only: run the real launcher against the built manifest, then `cn hook session-start`, one guarded tool call and `cn selftest` in a fixture member. The build still produces and publishes all five platforms. Linux arm64, macOS arm64 and x64, and Windows with Git Bash are off until there are paying customers (Record decision 22, [#47](https://github.com/missingbulb/claudiniteengine/issues/47)). Why: the design claims five platforms and two shells, and only a real machine of each kind proves it; until there are customers, one leg is enough.
2. **Pack corpus matrix.** Run every pack's own test suite and adoption fixture against the candidate, for every pack version published in the last 90 days and the newest version of each pack. Compare against the same run on stable and fail only on new failures. Why: members update packs nightly, so what they hold is recent; comparing against stable separates a regression from a test that was already broken.
3. **Hop test.** The candidate's updater performs a real engine update away from itself (see the canary fleet section for how). Why: an updater that cannot update strands every member on that release, and no later fix can reach them.
4. **Secret scan.** The existing grep of the built binary for secret-shaped strings.

The candidate's `manifest.json` records the result of the pack corpus matrix as `testedPacks`, the oldest version of each pack the candidate was tested with. A member's engine update skips a candidate whose `testedPacks` does not cover a pack it holds; that member's pack update then brings the pack forward, and the engine follows on the next run. Why: this keeps the matrix finite without leaving a lagging member on an untested pair, and it needs no new PR type.

### After publish

Once the smoke and the hop pass, the release workflow's publish job signs the candidate's `manifest.json` with our release key and publishes `@claudinite/cli` and its platform packages with trusted publishing, every package under the same explicit dist-tag: `rc` for a full release, `staging` for a staging build. A member's settings name its engine channel, and each channel reads tags: `stable` (the default) takes `latest`, `canary` takes the newer of `latest` and `rc`, `staging` the newest of all three. A version no tag points at is never a candidate. The canaries are on `canary`, so they take the candidate at once and no stable repo ever sees it. When it is promoted, as the staged rollout section describes, the promotion job downloads the version's tarballs, checks its binaries against the signed manifest, refuses a staging build, runs the full check (`full.yml`) on the version's own commit, and only then runs `npm dist-tag add @claudinite/cli@<version> latest`; a staging or rc release never waits on the full check. The manifest names no package, so a member's pin covers exactly the binaries the canaries ran. Why one package with tags: promotion then moves a pointer instead of republishing bytes under a second name, and a staging channel costs a tag instead of five more packages; the accepted risk is in the record.

Nothing in a member uses npm's own version resolution, so `npm update` has nothing to act on: a member has no `package.json` entry for Claudinite, and the launcher downloads only the version and hash its pin names. The pin moves only through the update PR, and that PR's CI refuses a pin whose manifest signature does not verify, or whose version npm's deprecation message marks held or revoked, whoever changed it. A person who installs `@claudinite/cli` globally gets a copy nothing in the repo runs.

### Major releases

A major is the one release that may stop accepting old shapes, and every member it touches needs a person, so it gets two more gates.

1. **Deprecation match.** The last release of the old major and the new major both run over the shape corpus. Every shape the new major rejects must have raised a deprecation finding under the old one. Why: a member should never meet a removal it was not warned about.
2. **Fixed from the report alone.** On the Sandbox and Lagging canaries, verify blocks the update and reports what breaks. A Claude session is given only that report, fixes the repo, and the update must then pass. Why: the report is the whole instruction a customer gets, so it has to be enough.

A major also soaks seven days on the canary channel, and its removals are announced on a pinned issue in ClaudinitePacks when it reaches that channel. Why: each member needs a person's time, and people need notice.

## Pack release gates

A pack version is tested on the oldest engine it claims to support and on every engine a member could be pinned to, then released to the canary channel before stable.

### On every pull request in ClaudinitePacks

1. **The pack's own tests** against the repo's pinned engine, as the engine design already says. Fork PRs run them too, with no secrets.
2. **Adoption and update fixtures.** A fixture member adopts the pack fresh, and a second fixture updates to it from the pack's current stable version, both through the real `cn adopt` and `cn update packs`, ending in `cn selftest` and `cn check world`. Why: adoption and update are different paths, and the update's own pre-PR check runs only on the second.
3. **Hook replay for guards.** When the PR changes a hook judge or a declared check that runs as a guard, the replay corpus runs with the pack before and after the change, and every changed verdict is listed on the PR. Why: a reviewer sees exactly which ordinary tool calls the new rule now blocks.

### Per pack release

When a version bump merges, the release workflow runs before it uploads anything:

1. **Engine floor matrix.** The pack's tests and fixtures run on three engines: exactly its `minEngineVersion`, the current stable, and the current canary. Why: `minEngineVersion` is written by hand, and running on that exact version is the only proof it is honest. Stable and canary are the engines members will actually run it on in the next few days.
2. **Neighbour check.** The fixture member declares the new version together with the stable versions of every pack it `requires` and every pack in the canary fleet's declarations. Why: packs interact through shared checks and ordering, and a pack alone is not how any member runs it.

Every gate above runs on the vendored set the release workflow builds, the exact bytes members receive, not on the pack's source directory. The workflow then checks that the R2 archive and the vendored branch hold identical bytes. The archive is uploaded once and never overwritten, and the pack's signed index, in both places, lists the new version with `channel: canary`. Promotion rewrites that entry to `stable`; the vendored set does not change.

## The canary fleet

A small fleet of repos runs on the **canary** channel, and each release is qualified by those repos running the real update, not by a harness imitating it.

The updater is inside the published binary, so nothing drives a canary from outside: each canary's pinned engine is exactly what a member runs, and its `canary` engine channel takes the candidate from the `rc` dist-tag, the way every stable member reads `latest`.

| Canary | Holds | Proves |
| --- | --- | --- |
| **Sandbox** (`ClaudiniteSandbox`) | The default packs plus one to three others, a local pack, overrides and adoption answers, and a task that needs a workflow change | Engine and packs together on a real repo; a workflow change riding the engine update PR and moved by its agent stage |
| **Lagging** | Reset each release to a tagged baseline: the oldest engine and packs still in the 90-day window, with files in the oldest shapes the major accepts | An update across many skipped versions onto old-shaped files, which must end in deprecation findings, not breaks |
| **Fresh** | Nothing; each release a workflow runs `cn adopt` and adopts the default packs on a scratch branch, then discards it | First adoption, the one path no existing member exercises |

The canaries do not try to cover every pack; each pack's own tests do that in the pack corpus matrix. The canaries prove the paths only a real repo exercises.

`dev/release/publish/canaries.json` registers three canaries: Lagging (`ClaudiniteCanaryLagging`), Fresh (`ClaudiniteCanaryFresh`) and Sandbox (`ClaudiniteSandbox`). Each names no workflow until the canary App (#21) can read their runs, and the gate counts only canaries that name one, so until then it answers `no-canaries` and promotion needs the dispatch's confirmation. The registration is there so a canary added later cannot be forgotten silently. The first jobs are fixed now:

- **Lagging**: the `v1`–`v3` shapes of `cn/packaging/verify/testdata/shapes`. It is the only place old shapes run live; every release must update it to deprecations, never breaks, and `cn selftest --repo` must fail no probe on it.
- **Fresh**: `cn adopt` on a scratch branch each release, then `cn verify` and `cn selftest --repo` over the result.
- **Sandbox**: the scheduler files `engine/update` and the executor drains it, the path `rehearse.sh --mode update` steps 10 and 11 prove against stubs.

The disposable canaries are repos we create for this and nothing else. Each has the Claudinite App installed on a test subscription, so key requests and paid surfaces run for real.

### What each canary does for a candidate

The release workflow does not wait for the nightly run. It dispatches each canary's update workflow at once, and each canary then:

1. Runs its own `cn update engine` (or `cn update packs`), never with `--force`: it finds the candidate on the canary channel, verifies it, runs the new engine's verify against the repo, and opens the PR; CI runs; green auto-merges.
2. **Checks verify's accuracy.** Verify's verdict is compared with what happened: verify passed and CI went red, or verify blocked and the same update forced on a scratch branch goes green, both fail the candidate. Why: every member now relies on verify to stop a breaking update before it reaches main, so a verify that is wrong in either direction is a release bug.
3. Runs a scheduler cycle and executor loop on the new main: one mechanical task and one task with an agentic phase that fires a real Claude Code routine.
4. Runs a **synthetic session probe**: a scheduled Claude Code routine on the canary that starts a session, which requests its key through GitHub, makes a few ordinary edits and commands that must be allowed, one that a guard must block, and stops. The probe reports each expected verdict as a check run.
5. Runs `cn check world` on main and compares the findings with the run before the update. For an engine candidate any new finding other than a deprecation is a regression, since the packs did not change; for a pack candidate, each new finding must come from the pack that changed.
6. When the candidate needs a workflow change, the canary's engine update PR must carry it staged, its agent stage must move it into `.github/workflows/` unedited, and the PR must land through `claudinite-ci.yml`.

Updates skip while main's CI is red, so a canary that goes red stops taking candidates without failing anything. The promotion job therefore counts a canary that has not taken the candidate as not passed, and alerts when a canary's main is red.

Each step is a named workflow in the canary, and its conclusion on the canary's commit is the record. The jobs that run \`cn\` or pack code hold no Checks write permission, and the session probe's verdicts come from the log \`cn\` writes, checked by a separate job, never from what the model in the session says. Why: anything that can write a green check run on a canary could otherwise promote a release, including a compromised candidate. A step counts toward promotion only after it has been reliable: a probe that fails on stable too is fixed or marked as a known skip with a reason, the way Node's CITGM keeps a `flaky` list, rather than blocking every release.

### The hop test

The updater that moves a member off a release is the one inside that release, so a release whose updater is broken can never be fixed from our side. Two runs cover it:

- **Into the candidate:** the canaries above update from stable, and Lagging from much older, using the old updater.
- **Out of the candidate:** the build records a digest of the updater's source. If an earlier release with the same digest already completed a hop, the hop is proven. If the updater changed, the workflow builds the same source again under the next version number, runs every per-candidate job on that second build too, and publishes it to the canary channel only; the canaries must update from the candidate to it, and that second build is the one promoted. Nothing but the disposable canaries ever runs a candidate. Why: most releases do not touch the updater and pay nothing, and when one does, the version members receive is one that has already been reached by the updater it replaces.

## Staged rollout and stopping a bad release

Which release a member may take is decided by the dist-tags its engine channel reads and by the release states npm's deprecation messages carry (`held: <reason>`, `revoked: <reason>`), so promoting, pausing and revoking never change a published version.

### Channels and release states

A repo's channel is its settings' `engine.channel`: `canary` for the canaries, `staging` for the owner's opted-in repos, and `stable`, the default, for everyone else. Each reads `@claudinite/cli`'s dist-tags, as the publish paragraph above lists. Promoting is moving `latest`, and holding a candidate is not promoting it. A pause or a revoke marks the version deprecated on npm with a `held:` or `revoked:` message. The nightly engine update takes the newest version its channel's tags offer whose manifest signature verifies and whose message marks it neither held nor revoked, and `cn adopt` and direct installers see the same mark.

Why npm's deprecation message rather than a signed index of our own: since 2026-10-05 a single repo asks no license (design record row 131), so no signed key reaches every run to carry the states, and npm already reaches every member. The cost is that the states are unsigned and a stale or replayed npm answer can hide a hold or a revocation, the rollback and freeze attacks The Update Framework names; a signed index would need its own signing key, a serial, an expiry, a daily re-sign job and a copy on GitHub for web VMs. Why a signed manifest: npm gives no provenance for a private source repo (see the security section), so our signature is what proves a version is ours.

### Promotion

| Step | Engine | Pack |
| --- | --- | --- |
| Published | Canary channel; canaries dispatched at once | Canary channel; canaries dispatched at once |
| Minimum soak on canaries | 24 hours, so it covers one natural nightly run, one key lifetime and a full scheduler cycle | One forced canary cycle; no clock minimum |
| Promoted to stable when | Every canary check is green on the candidate, the hop test passed, and no open issue carries the `release-blocker` label | Every canary check is green and no `release-blocker` issue is open |
| Security fix | Skips the soak, never the canaries | Skips nothing; packs have no fast path |
| Who promotes | A promotion job that runs hourly and reads only the conclusions of the canaries' named workflows, run by GitHub Actions on the candidate's commit | The same job |

Promotion is automatic because the canaries' state is the evidence and the job only reads it. A person can hold a release by opening a `release-blocker` issue naming its version, in the private ClaudiniteEngine repo so no outsider can open one. Marking a release as a security fix would skip the soak, so it needs Ariel's approval on a protected environment; nothing carries the flag yet. Why engines soak 24 hours and packs do not: an engine bug can surface on a clock (key expiry, the nightly cron), while a pack's failures show on its first cycle, and packs release far more often.

### Reaching stable members

Without outcome reports, stable members take a promoted release on their next nightly run, all on the same night. With outcome reports (a decision for you in the record), the rollout widens over three nights: repos whose id hashes into the first 10% take it the first night, 50% the second, all the third. Before each widening the promotion job reads the reports and pauses the rollout if the share of updates to the new version that verify blocks or whose PRs go red is more than twice the previous version's, counting each organization once and only once enough organizations have reported. A security fix is never paused by reports. Why the counting: reports are honest only about who sent them, so one account must not be able to stall every release. Why: a halt signal must be something a job can read, and today no signal from customer repos reaches us.

Customers can slow their own rollout further with the org policy's canary set from the commercialization design: their canary repos take a stable release first, and the rest follow once those have held it green.

### Stopping a bad release

Nothing ever moves a member's pin backward, so every fix rolls forward.

1. **Pause.** Mark the version `held: <reason>` with npm deprecate. Members that have not taken it stop taking it; members on it stay. An update PR already open for it is closed, because the update reads the held state from npm.
2. **Revoke**, when running it does harm. The Worker lists the version as revoked in every key and npm marks it deprecated, and the engine design's revoked state applies: guards and checks keep running, paid work stops, and the next engine update moves the member off it.
3. **Roll forward.** Release the fix, or the last good source rebuilt under a new version, through the canaries as a security fix.

`promote.yml` carries these as dispatch actions: `hold` and `revoke` mark the version deprecated on npm, `release` clears the mark, and `unpublish` removes a version from `@claudinite/cli` and its platform packages altogether. `unpublish` needs the dispatch's `confirm` to repeat the version, and refuses, before removing anything, when npm cannot be read, the version is the one `latest` points at, or it is the only one any of those packages holds, since npm deletes a package whose last version is unpublished. Both stop at the first npm command that fails.

A pack version is revoked in its pack index; the pack update never installs a revoked version and treats a member holding one as due for the next version, which is the revert. Why roll forward only: the design's rule that pins only move forward is what stops a downgrade attack, and a revert released as a new version keeps it.

## Security of the release path

The release path is where one mistake reaches every member overnight, so each credential sits in one job that runs no code under test, and every member verifies what it receives against a key we hold offline-first.

| Threat | Control |
| --- | --- |
| A private source repo gets no npm provenance: npm generates it only for public repos, even with trusted publishing ([npm docs](https://docs.npmjs.com/trusted-publishers)). The engine design's "verify the provenance attestation" check cannot work. | The release job signs each release's `manifest.json` with our release key, and the updater and `cn adopt` accept a version only if that signature verifies against the root the binary embeds. The engine design's provenance step is replaced by this. |
| A new member starts on an untested or held version, since a web VM cannot reach R2 | `cn adopt` on the stable channel takes the version `latest` points at, which only promotion moves, skips any npm marks deprecated, and verifies its manifest signature. It adopts the pack versions the signed pack indexes on the vendored branch of the public ClaudinitePacks repo name, which a web VM can read, not the tip of main. |
| A stolen npm publish credential | Trusted publishing from protected jobs, no long-lived token, and both packages set to refuse token publishes. A version published outside our workflow has no manifest signature from our release key, so no updater or `cn adopt` accepts it. |
| Someone points `latest` at an unpromoted version | Only the release job is a trusted publisher of the engine's packages, and only the promotion job may also move dist-tags on `@claudinite/cli`. A dist-tag is an unsigned pointer, so anyone with npm write on the package could still point `latest` at a version we signed that never passed the canaries (accepted in the record); it reaches stable members as a release that passed every per-candidate gate but not the canaries, and a pause stops it like any other release. |
| Code under test steals a release credential | Building, testing and the canary run in jobs with no secrets; they hand artifacts to the publish job by SHA-256. The publish job, which holds the release signing key, and the promotion job run no pack code and no candidate binary. |
| Code under test fakes a green canary to get itself promoted | The promotion job reads only the conclusions of named canary workflows run by GitHub Actions on the candidate's commit; jobs that run `cn` have no Checks write; the license App's check runs are ignored by app id. |
| A malicious or careless pack change | ClaudinitePacks PRs from forks run tests with no secrets; merging needs a code-owner review; the pack release goes through the canaries like any other. |
| An old copy of a pack index, or stale npm metadata, hides a revocation (rollback or freeze) | Partly open since 2026-10-05: engine holds and revocations arrive only in npm's deprecation messages, which nothing signs, so a stale npm answer can hide one. A pack index is signed, and the pack update skips while the CDN and the branch indexes name different serials. |
| The release signing key or a pack index signing key is stolen | Each key lives in the protected environment of the one job that signs with it and is certified by the offline root key the binary embeds, so it can be rotated without an engine release. Each certificate names its use and every verifier checks that use, so a stolen license issuing key (an online Worker secret) can sign neither a manifest nor an index; it could forge a fleet's plan, which buys a fleet run but brings in no code we did not sign. A stolen release or index key stays trusted until its certificate expires or the roots change. |
| A canary leaks something | Disposable canaries hold only a test subscription and their own routine token, and no customer data. The release workflow reaches them through a GitHub App installed on the canaries alone, with Actions write and Checks read, replacing today's cross-repo fleet token. |
| Our release repos run the engine in jobs near real secrets | Jobs holding publish or signing credentials never run `cn`; that separation applies inside ClaudinitePacks and ClaudiniteEngine, which run the stable engine like any member. |
| The hook replay corpus exposes code | It is captured only from our own repos, secret-scanned before it is stored, and kept in the private engine repo. On ClaudinitePacks, guard replay runs only on PRs from the repo itself or after a maintainer adds a label, since a fork's judge code could otherwise read the corpus and print it. |

Outcome reports, if you choose them, add one field set to the update job's existing key request: the repository id, the engine and pack versions it tried to move from and to, whether verify passed or blocked, and whether the previous update PR merged, went red or is still open. A spike in verify blocks is the earliest signal a release carries, since it arrives before any PR exists. They carry no file contents, and the privacy policy and store listing change in the same release that starts sending them.
