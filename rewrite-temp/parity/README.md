# parity

The harness that runs the frozen Node engine (missingbulb/Claudinite at the
commit `CLAUDINITE_NODE_ENGINE` checks out) and `cn` over the same input and
asserts they agree. The decision faces (tasks, update and growth) are answered by `../cndecide`, built beside `cn`, so the shipped binary carries none of them. Package documentation is in `engine.go`; each face's
fixture format is at the top of its test file.

| Face | Fixtures | Node side | cn side |
| --- | --- | --- | --- |
| scenarios | `testdata/scenarios/<name>/` | the engine's own commands | `cn` commands |
| differential | real trees in `CLAUDINITE_PARITY_TREES` | `check_the_world`, `check_the_work` | `cn check world`, `cn check work` |
| tasks | `testdata/tasks/<kind>/<name>.json` | `testdata/shims/tasks.mjs` | `cn tasks <kind> --world` |
| update | `testdata/update/<core>/<name>.json` | `testdata/shims/update.mjs` | `cndecide update decide <core> --world` |
| verify answers | `testdata/answered/<rule>/<case>.json` | `testdata/shims/answered.mjs`: the rule's `run` over `nodeFiles` | `cn verify` over `cnShape` with `cnFiles` laid over it |
| settings | `testdata/settings/<name>/`: a declaration and the tree it sits in | `testdata/shims/settings.mjs`: the reader, the registry and `legacy-shape-in-use` | `fromnode import`, then `cn verify` over the imported member |
| growth | `testdata/growth/<core>/<name>.json` | `testdata/shims/growth.mjs`: `capture-log.mjs` and `prune-logs.mjs` | `cndecide growth decide <core> --world` |
| member | `testdata/member/flat-member/<name>.json` | none: no Node engine writes the member file | `cn tasks flat --write` against a hand-written file |
| from-node | real Node trees in `CLAUDINITE_PARITY_TREES`, moved in `../fromnode`'s `TestMoveOverRealMembers` | none: no Node answer exists for a `cn` tree | `fromnode` over a copy, then `cn verify`: nothing, and `fromnode leftovers` no break |

A fixture's `expect` is always the Node engine's answer, written by
`CLAUDINITE_PARITY_RECORD=1` before the Go side existed.

The adoption slice added `scenarios/skills-index/*` (the skills index's
bytes: none, canon, canon-and-local, path-scoped, pipe-in-description),
`scenarios/lifecycle-adoption/*` (`adoption-answers-pending`,
`interview-answer-stale` and `seeded-file-stale`, eleven cases) and
`answered/skills-index-current/*` (four cases), with no divergence.

The growth slice added the `growth` face (the capture's line reader,
bundle, slice, redactions, scrub, log names and transcript discovery, and
the prune's plan and retention), `scenarios/growth-capture/*` (thirteen
cases: the branch's files, bytes and commit subjects),
`scenarios/session-end/*` (seven) and `scenarios/provenance/*` (33: `mark`,
`append`, `check`, `history`). One scenario diverges:
`provenance/mark-local-prefix`, record row 90 (cn resolves `local/<name>`,
which Node's tool refused). A second came later:
`provenance/append-unknown-kind`, record row 125 (cn's kind vocabulary
reads the shelf's `ported`, `gate-changed` and `hardened`). A third:
`provenance/brief-go-check`, record row 126 (`brief` reads a pack's Go
check as its carrier and follows it back to the module it was ported
from, where Node's tool found no carrier at all).

Phase 7 added `scenarios/provenance/*`'s maintainer verbs (nineteen more,
52 in all: `reduce`, `apply`, `convert-references` and `brief`, whose
repos carry authors, an origin, a sweep, a moved rule, a rule reworded in
place, a promoted pack and a declaration re-spelled to the same value).

## Divergences

Scenarios face divergences: 1 of 262 fixtures.

A coded scenario whose `cn` world findings differ on purpose carries
`"divergence": "record-<row>"` and `cn`'s findings as `"cnWorld"`, its
`world` staying the Node engine's: `flat-declarations/fires` is record row 137 (cn generates under
`.claudinite/cache/`, where the Node engine wrote `.claudinite/flat/`).

Growth face divergences: 0 of 57 fixtures.

The dashboard face, its `descriptor` and `usable` cores and
`scenarios/lifecycle-dashboard/*` were deleted when `cn dashboard
descriptor`, the built-in `descriptor-usable` and the flat dashboard file
left the engine for the claudinite-single-repo-dashboard pack; its
`flat-member` core is the member face. The flat comparison leaves out
Node's dashboards document, which cn no longer writes, and
`flat-declarations/fires` names cn's task file alone.

Tasks face divergences: 1 of 10 fixtures. `contract/invalid` is record row
89: `log-past-retention` is a built-in term, so the contract's lists of
built-ins name it.

A ported pack file ClaudinitePacks changed on purpose after the freeze is
listed in `diverged.txt` with its record row; cn reads it at the frozen
shelf's content, so both engines judge one input, and the pack's own
`test/` cases prove the new content. ClaudinitePacks#20, which moved the
task runner out of `claudinite-tasks` and its pack workers onto
`@claudinite/sdk`, is listed under record row 5; the manifests it touched
are not, since cn reads their new keys.

Where `cn` decides otherwise on purpose, an update fixture carries
`"divergence": "record-<row>"`, the design record row that says why, and
`cn`'s answer as `"cn"`. A verify-answers case carries the same field where
the Node rule fires and no verify rule does on purpose: `served-by-alias`
and `retired-schedule-key` under `legacy-shape-in-use`, record row 32 (no
update migrates member files, so cn reads no retired declaration key).

Update face divergences: 19 of 40 fixtures.

| Row | Fixtures | Why |
| --- | --- | --- |
| 8 | plan/downgrade-refused | a pin only moves forward: the plan keeps the newer held version |
| 12 | plan/min-engine-blocks | a minimum engine is `<major>.<day>.<n>` |
| 149 | plan/two-part-min-engine | a minimum that is no `<major>.<day>.<n>` version, a Node engine's two-part one included, blocks the plan: no engine loads that pack |
| 32 | gap/unstamped, gap/old-engine, gap/mid-engine, applystage/record-asks, applystage/withheld, applystage/test-visible, terminal/apply-stage | no update migrates member files, and the one agent stage an update runs moves the engine's staged workflows only (row 141), never a plan's apply stage |
| 39 | convergescope/mount-wiring | a pack PR carries the packs, the flat files and the CLAUDE.md import, never hook settings |
| 41 | convergescope/stamp-only, convergescope/checkout | the declaration is `.claudinite/settings.*`, whose bookkeeping edit is the engine pin |
| 63 | pulltext/* | the update opens one pull request per kind and supersedes, never amends |
| 72 | delivery/failed-forced | a failed self-test opens no pull request, forced or not |

A settings fixture accounts for each Node error and advisory with a line
the import or verify prints. Where `cn` decides otherwise on purpose it
carries the same `divergence` and `cn` fields; `cn.breaks` are verify
breaks Node never raised.

Settings face divergences: 4 of 19 fixtures.

| Row | Fixtures | Why |
| --- | --- | --- |
| 32 | local-module-manifest | a local `pack.mjs` is a module manifest no engine of cn's reads; verify breaks and the pack does not load |
| 73 | served-by-updates | Node errs on `servedBy`; cn has one update mechanism, so the import drops it |
| 79 | local-js-rules | cn runs no JavaScript check; verify breaks on each one in a local pack |
| 80 | renamed-ids-config | Node's reader lets the last entry for an id replace the config wholesale, losing basics' own; the import does what Node's barriers-absorbed record writes, nesting barriers' config under `config.barriers` and merging |

The fleet slice added the `fleet` face: the token grant, the manager's
config, dormancy, dispatch classes, the update's scope, freshness, the
roster's views, the four reports, the adoption issues' convergence, the
follow loop and the signal reader; 15b added the add-packs sweep's
parameters, force, work-list protocol and mark, fingerprint fit, scan and
the pack-seed classification and write. cn has no canon repo: current is what
each member's own update would move it to, and a repo once named canon is
judged as any other.

Fleet face divergences: 27 of 147 fixtures.

| Row | Fixtures | Why |
| --- | --- | --- |
| 94 | config/canon-repo, freshness/engine-behind, freshness/fresh, freshness/non-version-skipped, freshness/pack-behind, freshness/pack-canon-lacks, freshness/packs-only-stamp, reports/freshness-full, reports/update-dry-run, reports/update-live, reports/update-nothing-dispatched, reports/verdict-not-current, scope/canon, signal/canon-skipped, views/canon | there is no canon: freshness is judged against the published engine and pack versions a member's own update reads, and a repo Node set aside as canon is measured like any member |
| 95 | freshness/no-stamp | a cn member's held versions are its engine pin and its vendored manifests, so the no-stamp detail names `.claudinite/settings.*` |
| 95 | force/resolve-targets, force/resolve-targets-refused, scan/run-scan, scan/run-scan-scoped | a member's shape is read from its `.claudinite/` listing before the Node file, one call more per member, and an uncovered repo is named as lacking both |
| 98 | adoption/create-refused, adoption/open-new | the adoption issue asks for `cn init` and names `.claudinite/settings.*` |
| 99 | scan/fit-summary, scan/fit-summary-scoped-clean, scan/suspected-body | the corpus is the shelf's signed catalog, not a canon clone, and the undecided fingerprints are settled by running them over the member's checkout rather than by a Node module the fleet no longer ships |
| 101 | seeds/with-seeds, seeds/with-seeds-not-object | a seed is spliced into the member's settings file in its own format, comments and every byte outside `packs` kept, rather than round-tripped through two-space JSON |

The 15b chunk also added `scenarios/lifecycle-fleet/*` (six cases:
`fleet-pack-seed-agrees` over a seed that agrees, disagrees, is declared on
one side only, is not declared, is malformed, or is absent), with no
divergence. The settings file's line is not compared in a finding there:
the two engines write the declaration in different formats, so the same
entry sits on different lines. Porting the sheepdog lists its tasks under
record row 93 and its skill under row 94 in `diverged.txt`.

The 16a chunk added the `dashboard` face (37 descriptors read by the page
and judged by `descriptor-usable`, six trees under that rule, the page's
dormancy over 21 declarations, and four `flat-member` files no Node engine
writes), `fleet/detector/*` (22 relevance detectors validated as the shelf's
catalog reader does), `fleet/dormancy/page-corpus` (the sheepdog's reader
over the page's corpus, which agrees) and `scenarios/lifecycle-dashboard/*`
(six cases: `descriptor-usable` over a local, clean, mounted, undeclared,
untracked and shelf descriptor), with no divergence. Since no Node engine writes the member file,
cn's repo in a scenario or a differential tree carries the one cn writes,
kept out of git beside the settings file it states, and the flat
comparison leaves it out of cn's side.

The 16b chunk ported `claudinite-dashboard`, the thirteenth pack in
`ported.txt`, and added no fixture: the dashboard face stays at 48 and the
`lifecycle-dashboard` scenarios at six, with no divergence. The pack's two
task declarations are listed in `diverged.txt`, `deploy-oauth-exchange`'s
under record row 5 and `publish-pages`' under row 112, so cn's flat files
read them at the frozen content. The pack's own suite holds its copies of
the engine's predicates to `cn` (record row 110): 544 tests with
`CLAUDINITE_CN` set, of which 13 skip without it.
