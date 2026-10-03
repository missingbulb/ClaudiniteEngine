@claudinite/cli-rc 61003.2.0 fails the live-packs rehearsal against the real shelf

`release/rehearse.sh --mode live-packs` failed for 61003.2.0: built from this release's source and signed with the development keys, it did not adopt, load, check or update the real packs from packs.claudinite.com and ClaudinitePacks' vendored branch as a member would.

While this issue is open with the `release-blocker` label, `promote.yml` refuses to promote 61003.2.0. Close it once the failure is understood: a shelf-side cause is fixed in ClaudinitePacks, an engine-side one ships as a new version.

Run: https://github.com/missingbulb/ClaudiniteEngine/actions/runs/2

The last 60 lines of the rehearsal's log:

~~~~
rehearse: live-packs 1: cn init through npx vendored 8 packs
rehearse: FAIL: live-packs 2: check world on the adopted tree: aws-sam/handler-path
~~~~
