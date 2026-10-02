# parity

The harness that runs the frozen Node engine (missingbulb/Claudinite at the
commit `CLAUDINITE_NODE_ENGINE` checks out) and `cn` over the same input and
asserts they agree. Package documentation is in `engine.go`; each face's
fixture format is at the top of its test file.

| Face | Fixtures | Node side | cn side |
| --- | --- | --- | --- |
| scenarios | `testdata/scenarios/<name>/` | the engine's own commands | `cn` commands |
| differential | real trees in `CLAUDINITE_PARITY_TREES` | `check_the_world`, `check_the_work` | `cn check world`, `cn check work` |
| tasks | `testdata/tasks/<kind>/<name>.json` | `testdata/shims/tasks.mjs` | `cn tasks <kind> --world` |
| update | `testdata/update/<core>/<name>.json` | `testdata/shims/update.mjs` | `cn update decide <core> --world` |
| verify answers | `testdata/answered/<rule>/<case>.json` | `testdata/shims/answered.mjs`: the rule's `run` over `nodeFiles` | `cn verify` over `cnShape` with `cnFiles` laid over it |
| settings | `testdata/settings/<name>/`: a declaration and the tree it sits in | `testdata/shims/settings.mjs`: the reader, the registry and `legacy-shape-in-use` | `cn settings import`, then `cn verify` over the imported member |

A fixture's `expect` is always the Node engine's answer, written by
`CLAUDINITE_PARITY_RECORD=1` before the Go side existed.

## Divergences

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
| 12 | plan/min-engine-blocks, plan/two-part-min-engine | a minimum engine is `<day>.<n>.<patch>`; the two-part Node form is any engine |
| 32 | gap/unstamped, gap/old-engine, gap/mid-engine, applystage/record-asks, applystage/withheld, applystage/test-visible, terminal/apply-stage | no update migrates member files and none runs an agent stage |
| 39 | convergescope/mount-wiring | a pack PR carries the packs, the flat files and the CLAUDE.md import, never hook settings |
| 41 | convergescope/stamp-only, convergescope/checkout | the declaration is `.claudinite/settings.*`, whose bookkeeping edit is the engine pin |
| 63 | pulltext/* | the update opens one pull request per kind and supersedes, never amends |
| 72 | delivery/failed-forced | a failed self-test opens no pull request, forced or not |

A settings fixture accounts for each Node error and advisory with a line
the import or verify prints. Where `cn` decides otherwise on purpose it
carries the same `divergence` and `cn` fields; `cn.breaks` are verify
breaks Node never raised.

Settings face divergences: 3 of 18 fixtures.

| Row | Fixtures | Why |
| --- | --- | --- |
| 32 | local-module-manifest | a local `pack.mjs` is a module manifest no engine of cn's reads; verify breaks and the pack does not load |
| 73 | served-by-updates | Node errs on `servedBy`; cn has one update mechanism, so the import drops it |
| 79 | local-js-rules | cn runs no JavaScript check; verify breaks on each one in a local pack |
