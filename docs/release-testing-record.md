> Approved by Ariel on 2026-10-01. Source: [Claude Doc](https://claude.ai/code/artifact/f2b744d0-f95d-4a65-a6e2-45434b3e9aef). This repo copy is now the canonical version; change it here.

# Release testing: record

Every choice below was made by Claude while drafting on 2026-09-30 and is waiting for Ariel's review; none is settled until Ariel says so.

## Decisions to confirm

| # | Decision | Why | Changes the engine design? |
| --- | --- | --- | --- |
| 1 | The canary stops being driven by a workflow in the source repo and becomes an ordinary member on a canary channel | Today's harness exists because the canary's vendored worker predates the release; in a binary the updater is exactly what members run | Yes: replaces the Gate step of Engine release |
| 2 | Channels are the two npm packages; held, revoked and security-fix states ride in the license keys | The packages already decide who gets what (decision 20); the keys are signed, fresh per run and reach web sessions | Yes: the key gains release states |
| 3 | A signed manifest replaces the npm provenance check | npm does not generate provenance for a private source repo, so the check in the engine design cannot pass | Yes: the engine update and security table |
| 4 | The engine manifest records `testedPacks`; the engine update waits for packs that are older | Keeps the pack corpus matrix finite and never lands an untested pair | Yes: engine update rule |
| 5 | The hop test: skipped when the updater's source digest is unchanged, else a rebuild under the next version is what ships | Proves every shipped updater can update away from itself at almost no cost | Yes: release flow |
| 6 | Promotion is automatic from canary state; a `release-blocker` issue holds it | Releases are daily; a person as the gate becomes the bottleneck, and the label keeps a veto | No |
| 7 | Engines soak 24 hours on canaries; packs need one forced cycle | Engine bugs can surface on a clock (key lifetime, cron); pack failures show at once | No |
| 8 | A security fix skips the soak, never the canaries | Speed matters then, but an untested fix can be worse than the bug | No |
| 9 | Fixes roll forward only; a revoked pack version is treated as due for the next version | Keeps the design's pins-only-move-forward rule, which blocks downgrade attacks | No |
| 10 | The pack corpus covers 90 days of pack versions | Members update packs nightly; older holders update packs first by decision 4 | No |
| 11 | Hook latency budget: p95 no more than 20% or 50 ms slower than stable | The binary runs on every tool call | No |
| 12 | The hook replay corpus is captured from our own repos only | We may not read customer sessions; ours are real and ours to keep | No |
| 13 | Three disposable canaries (Main, Lagging, Fresh). Ariel decided: our own repos are production and never take candidates; Main holds the default packs plus one to three others, and there is no Minimal canary; covering every pack on canaries is not attainable | Each covers a path no fixture can: a real repo, skipped versions, first adoption | No |
| 14 | A GitHub App installed only on the canaries replaces the cross-repo fleet token | Least privilege; no token that can write to any other repo | No |

**15.** From the security review, revised by decision 21: `cn init` takes the newest `@claudinite/cli`, skips versions npm marks deprecated, verifies the manifest signature, and adopts the pack versions the signed pack indexes in ClaudinitePacks name, not the tip of main. This changes the engine design's Initial bootstrap.

**16–19.** From the engine design change of 2026-09-30 (its record row 32: no update rewrites member files, a major is fixed by a person in a Claude session with no migration command, updates skip while main is red and run verify first):

- **16.** A shape corpus replaces migration tests: every member-file shape the current major has accepted must still load and raise only deprecation findings. Why: nothing rewrites member files any more, so compatibility is the only protection.
- **17.** Verify's accuracy is a canary check in both directions. Why: every member now depends on verify to stop a breaking update.
- **18.** A major must have warned, by a deprecation finding, about every shape it removes, and a Claude session given only verify's report must be able to fix a canary; majors soak seven days and are announced. Why: each member needs a person's time.
- **19.** A canary that stops updating because its main is red counts as not passed and alerts. Why: updates skip silently while main is red.

Decision 13 changed with it: the Lagging canary now proves old shapes still work rather than that migrations run in one pass.

**20.** (Superseded by decision 23.) Ariel's suggestion, 2026-09-30: candidates go to a separate npm package, `@claudinite/cli-rc`, and promotion publishes the same binaries to `@claudinite/cli`. Why: the main package only ever holds releases that passed the canaries. It also made the engine index unnecessary; see decision 21.

**21.** Ariel's question, 2026-09-30: the signed engine channel index is dropped. Canaries read the RC package and everyone else the main package; the release job signs each manifest in place of npm provenance; held, revoked and security-fix states ride in the license keys, and in npm deprecation for `cn init`. Why: the two packages already decide who gets what, and the keys are signed, fresh per run and reach web sessions, so the index, its signing key, serial, expiry, daily re-sign and GitHub copy only duplicated them. Pack indexes stay; they belong to the engine design.

**22.** Ariel's decision, 2026-10-02: the per-candidate platform smoke matrix, both before publish and against the published package, runs on linux-x64 only; linux-arm64, darwin-x64, darwin-arm64 and windows-x64 under Git Bash are off. The build still produces and publishes all five platforms, since promotion copies the same bytes. Revive the full matrix once there are paying customers ([#47](https://github.com/missingbulb/claudiniteengine/issues/47)). Ariel's words: "turn off all smoke tests except one 'default'. Open an issue to revive then in the future, once we have customers."

**23.** Ariel, 2026-10-05: one package with dist-tags (rc, staging, latest) replaces the separate RC package; promotion moves `latest`. Accepted risk: a dist-tag is an unsigned pointer, so anyone with npm write on the package could point `latest` at a version we signed that never passed the canaries. `release.yml` takes a required kind: a full release builds all five platforms, runs every gate and publishes under `rc`; a staging build is linux-x64 alone, skips the reproducibility check, the hop and live-packs, and publishes under `staging`, which only repos whose settings choose the staging engine channel read. Promotion refuses a staging build. Supersedes decision 20 and the "npm dist-tags as channels" alternative.

**Needs your call:** outcome reports. Recommended: yes. The update job would add, to the key request it already makes, the repository id, versions moved from and to, and whether the previous update PR merged or went red. That turns stable rollout into three nights (10%, 50%, all) that pause on their own when update PRs go red. Without it, every stable member takes a release the same night and we learn about a failure only from support. It is a change to what the product sends, so the privacy policy changes with it.

## Research

Two Fable researchers read the primary sources below on 2026-09-30.

| System | What it does | What we took |
| --- | --- | --- |
| [Rust crater](https://rustc-dev-guide.rust-lang.org/tests/crater.html) | Builds every crate with the old and new compiler and triages only the differences, grouped by root cause | Differential runs: fail on new failures, not absolute ones |
| [Node.js CITGM](https://github.com/nodejs/citgm/blob/main/README.md) | Runs about 70 real modules' own tests against a Node build, base and proposal, and diffs; a `lookup.json` marks flaky or skipped modules with reasons | The pack corpus matrix; a known-skip list with reasons |
| [TypeScript bot](https://github.com/microsoft/TypeScript/wiki/Triggering-TypeScript-Bot) | On request, runs the top 400 repos and a curated user suite, PR against main, and posts the diff | Hook replay posts the verdict diff on the PR |
| [Homebrew test-bot](https://docs.brew.sh/BrewTestBot) | Tests dependents of a changed formula, opt in by label | Neighbour check for packs |
| [Babel e2e](https://raw.githubusercontent.com/babel/babel/main/.github/workflows/e2e-tests.yml) | Publishes to a local registry and installs into real downstreams | Considered; see alternatives |
| [Dependabot smoke tests](https://github.com/dependabot/smoke-tests) | Fixture repos with recorded HTTP, outputs compared with the recording | Fixture rehearsals stay hermetic |
| [Terraform plugin protocol](https://developer.hashicorp.com/terraform/plugin/terraform-plugin-protocol) | Providers declare a protocol version; tests run a matrix of CLI versions | SDK contract tests against past SDKs; engine floor matrix |
| [Kubernetes skew policy](https://kubernetes.io/releases/version-skew-policy/) and [release-blocking jobs](https://github.com/kubernetes/sig-release/blob/master/release-blocking-jobs.md) | A written skew contract tested by upgrade jobs; only owned jobs that pass reliably may block | `testedPacks` states the other half of `minEngineVersion`; only reliable canary checks gate |
| [VS Code](https://github.com/microsoft/vscode/wiki/Running-the-Endgame) | Daily Insiders beside Stable; recovery releases for a stated list of harms | Canary channel; a stated revoke rule |
| [Chrome channels](https://developer.chrome.com/docs/web-platform/chrome-release-channels) | Canary, Dev, Beta, then Stable at 1 to 5% rising to 100%, paused on trouble, never downgraded | Channels and roll-forward only |
| [Firefox Balrog](https://mozilla-balrog.readthedocs.io/en/latest/database.html) | The update server holds the rollout rate and can point a channel at no update | Release states live outside the package, in the license keys |
| [npm dist-tags](https://docs.npmjs.com/adding-dist-tags-to-packages) and [unpublish policy](https://docs.npmjs.com/policies/unpublish) | Tags point at versions; unpublish is limited to 72 hours and burns the number | We never unpublish; see alternatives on tags |
| [npm trusted publishing](https://docs.npmjs.com/trusted-publishers) | OIDC publish with no token; provenance only from public repos | Decisions 3 and 20 |
| [The Update Framework](https://theupdateframework.io/docs/security/) | Short-expiry, monotonic, signed metadata against rollback and freeze attacks | Pack index serial; engine states in fresh per-run keys |
| [Google Play staged rollout](https://support.google.com/googleplay/android-developer/answer/6346149) and [App Store phased release](https://www.developer.apple.com/help/app-store-connect/update-your-app/release-a-version-update-in-phases) | Percentage rollout that can halt but not rewind; fix by a new release | Pause, revoke, roll forward |
| [Tailscale](https://tailscale.com/kb/1067/update) | Auto-update waits about a week after release, faster for security fixes | Soak with a security fast path |
| [Renovate](https://docs.renovatebot.com/configuration-options/) and [Dependabot cooldown](https://docs.github.com/en/code-security/dependabot/working-with-dependabot/dependabot-options-reference) | Consumers wait a minimum age before adopting a release | See alternatives |
| [Argo Rollouts analysis](https://argoproj.github.io/argo-rollouts/features/analysis/) | A metric crossing a limit aborts the rollout automatically | Outcome reports as a machine-read halt signal |

## Alternatives not chosen

| Alternative | Why not |
| --- | --- |
| Keep today's rehearsal, driven from the source repo with a cross-repo token | Its reason, a vendored worker older than the release, is gone; it never exercised the real download and verification |
| npm dist-tags as channels | Superseded by decision 23, which chose them. Moving a tag needs npm write access outside publish, tags are unsigned, and npm has no revoked or security-fix state |
| Prerelease versions (`-rc.1`) for candidates | The final version would be a new build, so members would get bits no canary ran |
| A local registry for candidates, as Babel does | Skips the real npm path and the web VM allowlist the engine design depends on; fine inside fixture tests only |
| A crater-style run over customer repos | We cannot read member repos, and should not |
| Consumer-side minimum release age as the main brake | It delays everyone equally and reads no signal; the central soak does it once. The org canary set remains for customers who want more |
| Automatic rollback to the previous version | Needs pins to move backward, which is what a downgrade attack needs |
| A person approves every promotion | Daily releases make the person the bottleneck; the blocker label keeps the veto |
| Percentage rollout without outcome reports | It would widen blind, with nothing to pause on |
| A signed engine channel index on R2, copied to ClaudinitePacks for web VMs | The first draft's choice. Once candidates had their own npm package it only duplicated the license keys, at the cost of a signing key, a serial, an expiry and a daily re-sign job |

## Security review

A Fable reviewer read the design against the engine design and today's canary on 2026-09-30. All ten findings were applied to the Design tab.

| Severity | Finding | Change made |
| --- | --- | --- |
| High | Anything that can write a check run on a canary (the candidate itself, pack code, the license App, a prompt-injected probe) could promote a release | Promotion reads only named Actions workflow conclusions; `cn` jobs have no Checks write; probe verdicts come from `cn`'s log |
| High | One root certifies both license issuing keys (online Worker secrets) and the index key, so a stolen license key could sign an index | Certificates name their use and verifiers check it; the accepted index key is named in each Actions key |
| High | `cn init` still pinned npm's newest and adopted packs from main, outside the index, since a web VM cannot reach R2 | Signed indexes are also committed to ClaudinitePacks; init verifies them and pins stable |
| Medium | The committed serial only moves when an update merges, so a quiet member accepts old indexes for up to seven days | The Worker puts the current serial in every Actions key |
| Medium | Guard replay on fork PRs would let a fork's judge read and print the private corpus | Same-repo PRs or a maintainer label only |
| Medium | An outsider could block all promotion with the label; the security flag can be set by whoever cuts a release | Label lives in the private engine repo and names a version; the security flag needs Ariel's environment approval |
| Medium | The hop test promoted a second build that only the canaries ran; our own repos could be stranded by a broken updater | All per-candidate jobs rerun on the second build; only disposable canaries ever run a candidate |
| Medium | One organization's reports could stall every rollout | Count each organization once, require a minimum sample, never pause a security fix on reports |
| Low | A held version's already-open update PR would still auto-merge | Update PR CI re-reads the index and fails an uninstallable pin |
| Low | The engine design still says the update verifies npm provenance; a missed daily re-sign would silently freeze fixes | Decision 3 replaces that step; a failed re-sign alerts |

The reviewer confirmed these hold: immutable archives and packages with promotion moving only a signed pointer, forward-only pins, the index's manifest hash replacing provenance, secret-free build and canary jobs, the canary-only App, and a 24-hour soak covering one key lifetime and one cron.

Decision 21 later removed the engine index. Its findings carry over: the use-named certificates now cover the release signing key and the pack index keys, the serial finding now covers pack indexes only, and a held version's open update PR fails on the held list in its run's key.

## Open questions

- A person cannot stop a wrong guard mid-session: sessions do not see revocations until the update PR merges, and CI refuses a pin change not opened by the update bot. Is a documented guard off switch needed?
- What a member does when a run gets no license key, a lapsed or offline one: proposed, it takes no engine update, which the engine design already says for lapsed members.
- The canaries need an internal subscription plan in the license service.
- The session probe and the agentic canary task spend inference on every release.
- Desktop sessions on macOS and Windows get the smoke matrix but no real session probe.
- A member whose pack update is also stuck stays on its old engine under decision 4; is that acceptable?
