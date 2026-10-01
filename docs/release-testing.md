> Approved by Ariel on 2026-10-01. Source: [Claude Doc](https://claude.ai/code/artifact/f2b744d0-f95d-4a65-a6e2-45434b3e9aef). This repo copy is now the canonical version; change it here.

# Release testing for engine and pack updates

Sep 30, 2026 · @Ariel Raunstien

Every engine and pack release passes the same four stages, and the last gate before members is ClaudiniteCanary grown into a small fleet of real repos running the real update. The fleet replaces today's rehearsal, which drives the update from outside. The canaries install engines from an RC package on npm and everyone else from the main package, which only ever holds releases the canaries passed. Decisions, research, rejected alternatives and the security review are on the Record tab.

## At a glance

&#91;embedded content: release gates · 4 stages\]

Cheap tests run on every pull request; the expensive ones run once per candidate; the canaries prove it on real repos; only then does the stable channel open.

## What gets released, and what can break

Two things ship on separate paths and reach a member in separate PRs, so each is tested against what the member already holds of the other.

| Release | What ships | How it reaches a member | Held fixed while it lands |
| --- | --- | --- | --- |
| Engine | The `cn` binary for five platforms, its manifest, the embedded runner script and SDK, the updater and its verify | The engine update runs the new engine's verify against the repo, then opens a PR that moves the pin only | The member's committed packs and its own files |
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
| Key requests or license verification break | Paid work stops everywhere | Synthetic session probe and a scheduler run on canaries |

The gates below are layered so the cheap ones run on every pull request and the expensive, real ones run once per release.

## Engine release gates

An engine version is built once, published first to an RC package on npm, and published to the main package only after the canaries pass; the bits a canary tested are the bits every member gets, because promotion re-publishes the same signed binaries.

### On every pull request in ClaudiniteEngine

1. **Unit and contract tests.** Go tests, plus the SDK protocol suite: every message shape the binary sends and accepts, run against the SDK in the tree and the last three published SDKs. Why: a pack imports the SDK version it was written for, so the pipe must stay readable from both sides.
2. **Fixture rehearsals.** Today's `vendoring/rehearsal` harness, rebuilt around the binary: build a fixture member in a temp dir, run the real `cn update` with its verify, then `cn selftest` and `cn check world`. It keeps its fresh, current and stale modes. Why: seconds per run, and the first place an update path breaks.
3. **Shape corpus.** A fixture for every shape of `.claudinite-settings.json`, pack declaration and local pack that any earlier release of the current major accepted, each added when its release shipped. Every one must still load, run its checks, and raise only deprecation findings. Why: within a major the engine promises to only add, and nothing rewrites member files, so an old shape that stops working breaks members with no warning.
4. **Hook replay.** A corpus of real hook inputs (SessionStart, PreToolUse, PostToolUse, Stop) captured from our own repos' sessions is replayed through the PR's binary and through the current stable, with the same packs. Any verdict that changes fails unless the PR says which rule change caused it. Why: a guard that blocks ordinary work is the most expensive failure we can ship, and a before-and-after diff finds it without anyone writing an assertion for it.
5. **Latency budget.** The replay also times each hook. A p95 more than 20% or 50 ms slower than stable fails. Why: the binary runs on every tool call, so a slowdown is paid by every session all day.

### Per release candidate

The release workflow builds the five binaries once, then runs these jobs against the built files, with no publish credential in any of them.

1. **Platform smoke matrix.** On GitHub-hosted Linux x64 and arm64, macOS arm64 and x64, and Windows with Git Bash: run the real launcher against the built manifest, then `cn hook session-start`, one guarded tool call and `cn selftest` in a fixture member. Why: the design claims five platforms and two shells, and only a real machine of each kind proves it.
2. **Pack corpus matrix.** Run every pack's own test suite and adoption fixture against the candidate, for every pack version published in the last 90 days and the newest version of each pack. Compare against the same run on stable and fail only on new failures. Why: members update packs nightly, so what they hold is recent; comparing against stable separates a regression from a test that was already broken.
3. **Hop test.** The candidate's updater performs a real engine update away from itself (see the canary fleet section for how). Why: an updater that cannot update strands every member on that release, and no later fix can reach them.
4. **Secret scan.** The existing grep of the built binary for secret-shaped strings.

The candidate's `manifest.json` records the result of the pack corpus matrix as `testedPacks`, the oldest version of each pack the candidate was tested with. A member's engine update skips a candidate whose `testedPacks` does not cover a pack it holds; that member's pack update then brings the pack forward, and the engine follows on the next run. Why: this keeps the matrix finite without leaving a lagging member on an untested pair, and it needs no new PR type.

### After publish

The release job signs the candidate's `manifest.json` with our release key and publishes it with trusted publishing to a separate npm package, `@claudinite/cli-rc`, and its platform packages. The canaries' settings name the RC package as their channel, so they take the candidate at once and no other repo ever sees it. When it is promoted, as the staged rollout section describes, the promotion job checks the RC package's binaries against the signed manifest and publishes the same bytes, manifest and signature as that version of `@claudinite/cli`. The manifest names no package, so a member's pin covers exactly the binaries the canaries ran. Why two packages: the main package only ever holds releases that passed the canaries, so the updater, `cn init` and anyone installing from npm directly can simply take its newest version.

Nothing in a member uses npm's own version resolution, so `npm update` has nothing to act on: a member has no `package.json` entry for Claudinite, and the launcher downloads only the version and hash its pin names. The pin moves only through the update PR, and that PR's CI refuses a pin whose manifest signature does not verify, or which its own run's license key lists as held or revoked, whoever changed it. A person who installs `@claudinite/cli` globally gets a copy nothing in the repo runs.

### Major releases

A major is the one release that may stop accepting old shapes, and every member it touches needs a person, so it gets two more gates.

1. **Deprecation match.** The last release of the old major and the new major both run over the shape corpus. Every shape the new major rejects must have raised a deprecation finding under the old one. Why: a member should never meet a removal it was not warned about.
2. **Fixed from the report alone.** On the Main and Lagging canaries, verify blocks the update and reports what breaks. A Claude session is given only that report, fixes the repo, and the update must then pass. Why: the report is the whole instruction a customer gets, so it has to be enough.

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

Today's ClaudiniteCanary becomes a small fleet of repos on the **canary** channel, and each release is qualified by those repos running the real update, not by a harness imitating it.

Today a workflow in this repo checks out the canary with a cross-repo token and runs the update worker from the ref under test, because the canary's own copy of the worker predates the release. With the updater inside a published binary that reason goes away: the canary's pinned engine is exactly what a member runs, and it reads the candidate from the RC package its settings name, the way every member reads the main package. So the canary stops being driven and starts being a member.

| Canary | Holds | Proves |
| --- | --- | --- |
| **Main (today's ClaudiniteCanary**) | The default packs plus one to three others, a local pack, overrides and adoption answers, and a task that needs a workflow change | Engine and packs together on a real repo; a workflow patch arriving as an issue |
| **Lagging** | Reset each release to a tagged baseline: the oldest engine and packs still in the 90-day window, with files in the oldest shapes the major accepts | An update across many skipped versions onto old-shaped files, which must end in deprecation findings, not breaks |
| **Fresh** | Nothing; each release a workflow runs `cn init` and adopts the default packs on a scratch branch, then discards it | First adoption, the one path no existing member exercises |

The canaries do not try to cover every pack; each pack's own tests do that in the pack corpus matrix. The canaries prove the paths only a real repo exercises.

The disposable canaries are repos we create for this and nothing else. Each has the Claudinite App installed on a test subscription, so key requests and paid surfaces run for real.

### What each canary does for a candidate

The release workflow does not wait for the nightly run. It dispatches each canary's update workflow at once, and each canary then:

1. Runs its own `cn update engine` (or `cn update packs`), never with `--force`: it finds the candidate on the canary channel, verifies it, runs the new engine's verify against the repo, and opens the PR; CI runs; green auto-merges.
2. **Checks verify's accuracy.** Verify's verdict is compared with what happened: verify passed and CI went red, or verify blocked and the same update forced on a scratch branch goes green, both fail the candidate. Why: every member now relies on verify to stop a breaking update before it reaches main, so a verify that is wrong in either direction is a release bug.
3. Runs a scheduler cycle and executor loop on the new main: one mechanical task and one task with an agentic phase that fires a real Claude Code routine.
4. Runs a **synthetic session probe**: a scheduled Claude Code routine on the canary that starts a session, which requests its key through GitHub, makes a few ordinary edits and commands that must be allowed, one that a guard must block, and stops. The probe reports each expected verdict as a check run.
5. Runs `cn check world` on main and compares the findings with the run before the update. For an engine candidate any new finding other than a deprecation is a regression, since the packs did not change; for a pack candidate, each new finding must come from the pack that changed.
6. When the candidate needs a workflow change, the canary holding that task must receive the issue carrying the patch; a scripted commit applies it, and the task it holds off must turn back on.

Updates skip while main's CI is red, so a canary that goes red stops taking candidates without failing anything. The promotion job therefore counts a canary that has not taken the candidate as not passed, and alerts when a canary's main is red.

Each step is a named workflow in the canary, and its conclusion on the canary's commit is the record. The jobs that run \`cn\` or pack code hold no Checks write permission, and the session probe's verdicts come from the log \`cn\` writes, checked by a separate job, never from what the model in the session says. Why: anything that can write a green check run on a canary could otherwise promote a release, including a compromised candidate. A step counts toward promotion only after it has been reliable: a probe that fails on stable too is fixed or marked as a known skip with a reason, the way Node's CITGM keeps a `flaky` list, rather than blocking every release.

### The hop test

The updater that moves a member off a release is the one inside that release, so a release whose updater is broken can never be fixed from our side. Two runs cover it:

- **Into the candidate:** the canaries above update from stable, and Lagging from much older, using the old updater.
- **Out of the candidate:** the build records a digest of the updater's source. If an earlier release with the same digest already completed a hop, the hop is proven. If the updater changed, the workflow builds the same source again under the next version number, runs every per-candidate job on that second build too, and publishes it to the canary channel only; the canaries must update from the candidate to it, and that second build is the one promoted. Nothing but the disposable canaries ever runs a candidate. Why: most releases do not touch the updater and pay nothing, and when one does, the version members receive is one that has already been reached by the updater it replaces.

## Staged rollout and stopping a bad release

Which release a member may take is decided by the npm package it reads and by the release states in each run's license key, so promoting, pausing and revoking never change a published package.

### Channels and release states

A repo's channel is the npm package its settings name: `@claudinite/cli-rc` for the canaries, `@claudinite/cli` for everyone else. Promoting is publishing to the main package, and holding a candidate is not promoting it, so no channel pointer is needed. The states npm cannot express come with the short-lived key every Actions run and every session already gets from the license Worker: the key lists the versions currently held or revoked and the ones that are security fixes. The nightly engine update takes the newest version of its package whose manifest signature verifies and which the key does not list as held or revoked. A pause or revoke also marks the version deprecated on npm, which `cn init` and direct installers see.

Why the license key rather than a signed index of our own: the key is already signed, issued fresh for every run and session, and reaches web sessions through GitHub, so a state change reaches every member on its next run and nothing old can be replayed to hide it, the rollback and freeze attacks The Update Framework names. An index would need its own signing key, a serial, an expiry, a daily re-sign job and a copy on GitHub for web VMs to do the same. Why a signed manifest: npm gives no provenance for a private source repo (see the security section), so our signature is what proves a version is ours.

### Promotion

| Step | Engine | Pack |
| --- | --- | --- |
| Published | Canary channel; canaries dispatched at once | Canary channel; canaries dispatched at once |
| Minimum soak on canaries | 24 hours, so it covers one natural nightly run, one key lifetime and a full scheduler cycle | One forced canary cycle; no clock minimum |
| Promoted to stable when | Every canary check is green on the candidate, the hop test passed, and no open issue carries the `release-blocker` label | Every canary check is green and no `release-blocker` issue is open |
| Security fix | Skips the soak, never the canaries | Skips nothing; packs have no fast path |
| Who promotes | A promotion job that runs hourly and reads only the conclusions of the canaries' named workflows, run by GitHub Actions on the candidate's commit | The same job |

Promotion is automatic because the canaries' state is the evidence and the job only reads it. A person can hold a release by opening a `release-blocker` issue naming its version, in the private ClaudiniteEngine repo so no outsider can open one. Marking a release as a security fix needs Ariel's approval on a protected environment, and the license keys carry the flag, because that flag skips the soak. Why engines soak 24 hours and packs do not: an engine bug can surface on a clock (key expiry, the nightly cron), while a pack's failures show on its first cycle, and packs release far more often.

### Reaching stable members

Without outcome reports, stable members take a promoted release on their next nightly run, all on the same night. With outcome reports (a decision for you in the record), the rollout widens over three nights: repos whose id hashes into the first 10% take it the first night, 50% the second, all the third. Before each widening the promotion job reads the reports and pauses the rollout if the share of updates to the new version that verify blocks or whose PRs go red is more than twice the previous version's, counting each organization once and only once enough organizations have reported. A security fix is never paused by reports. Why the counting: reports are honest only about who sent them, so one account must not be able to stall every release. Why: a halt signal must be something a job can read, and today no signal from customer repos reaches us.

Customers can slow their own rollout further with the org policy's canary set from the commercialization design: their canary repos take a stable release first, and the rest follow once those have held it green.

### Stopping a bad release

Nothing ever moves a member's pin backward, so every fix rolls forward.

1. **Pause.** Add the version to the held list the license Worker puts in every key, and mark it deprecated on npm. Members that have not taken it stop taking it; members on it stay. An update PR already open for it fails, because its CI reads the held list from its own run's key.
2. **Revoke**, when running it does harm. The Worker lists the version as revoked in every key and npm marks it deprecated, and the engine design's revoked state applies: guards and checks keep running, paid work stops, and the next engine update moves the member off it.
3. **Roll forward.** Release the fix, or the last good source rebuilt under a new version, through the canaries as a security fix.

A pack version is revoked in its pack index; the pack update never installs a revoked version and treats a member holding one as due for the next version, which is the revert. Why roll forward only: the design's rule that pins only move forward is what stops a downgrade attack, and a revert released as a new version keeps it.

## Security of the release path

The release path is where one mistake reaches every member overnight, so each credential sits in one job that runs no code under test, and every member verifies what it receives against a key we hold offline-first.

| Threat | Control |
| --- | --- |
| A private source repo gets no npm provenance: npm generates it only for public repos, even with trusted publishing ([npm docs](https://docs.npmjs.com/trusted-publishers)). The engine design's "verify the provenance attestation" check cannot work. | The release job signs each release's `manifest.json` with our release key, and the updater and `cn init` accept a version only if that signature verifies against the root the binary embeds. The engine design's provenance step is replaced by this. |
| A new member starts on an untested or held version, since first adoption has no license key yet and a web VM cannot reach R2 | `cn init` takes the newest version of `@claudinite/cli`, which holds only promoted releases, skips any npm marks deprecated, and verifies its manifest signature. It adopts the pack versions the signed pack indexes on the vendored branch of the public ClaudinitePacks repo name, which a web VM can read, not the tip of main. |
| A stolen npm publish credential | Trusted publishing from protected jobs, no long-lived token, and both packages set to refuse token publishes. A version published outside our workflow has no manifest signature from our release key, so no updater or `cn init` accepts it. |
| Someone publishes straight to the main npm package | Only the promotion job is a trusted publisher for `@claudinite/cli`, and only the release job for `@claudinite/cli-rc`. Someone who still copies an unpromoted candidate to the main package ships only a version we signed that passed every per-candidate gate but not the canaries, and a pause stops it like any other release. |
| Code under test steals a release credential | Building, testing and the canary run in jobs with no secrets; they hand artifacts to the publish job by SHA-256. The publish job, which holds the release signing key, and the promotion job run no pack code and no candidate binary. |
| Code under test fakes a green canary to get itself promoted | The promotion job reads only the conclusions of named canary workflows run by GitHub Actions on the candidate's commit; jobs that run `cn` have no Checks write; the license App's check runs are ignored by app id. |
| A malicious or careless pack change | ClaudinitePacks PRs from forks run tests with no secrets; merging needs a code-owner review; the pack release goes through the canaries like any other. |
| An old copy of a pack index, or stale npm metadata, hides a revocation (rollback or freeze) | Engine holds and revocations arrive in each run's license key, issued fresh, so nothing cached can hide them. A pack index's serial only goes up, and the Worker puts the current serial into every Actions key; the updater refuses an older index. |
| The release signing key or a pack index signing key is stolen | Each key lives in the protected environment of the one job that signs with it and is certified by the offline root key the binary embeds, so it can be rotated without an engine release. Each certificate names its use and every verifier checks that use, so a stolen license issuing key (an online Worker secret) can sign neither a manifest nor an index; it could still forge the held and revoked lists, which stalls releases or hides a revocation but brings in no code we did not sign. The signing keys a member accepts are also named in each Actions key, so a stolen one can be retired before its certificate expires. |
| A canary leaks something | Disposable canaries hold only a test subscription and their own routine token, and no customer data. The release workflow reaches them through a GitHub App installed on the canaries alone, with Actions write and Checks read, replacing today's cross-repo fleet token. |
| Our release repos run the engine in jobs near real secrets | Jobs holding publish or signing credentials never run `cn`; that separation applies inside ClaudinitePacks and ClaudiniteEngine, which run the stable engine like any member. |
| The hook replay corpus exposes code | It is captured only from our own repos, secret-scanned before it is stored, and kept in the private engine repo. On ClaudinitePacks, guard replay runs only on PRs from the repo itself or after a maintainer adds a label, since a fork's judge code could otherwise read the corpus and print it. |

Outcome reports, if you choose them, add one field set to the update job's existing key request: the repository id, the engine and pack versions it tried to move from and to, whether verify passed or blocked, and whether the previous update PR merged, went red or is still open. A spike in verify blocks is the earliest signal a release carries, since it arrives before any PR exists. They carry no file contents, and the privacy policy and store listing change in the same release that starts sending them.
