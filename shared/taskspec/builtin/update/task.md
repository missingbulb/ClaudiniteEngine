# Engine update — the agent stage

The deterministic half already ran. It moved this repo's engine pin on the
update pull request your item names (`Target-pr:`, on `Target-branch:`), and
staged under `.claudinite/cache/pending-workflows/` each workflow file the new
engine expects of this repo. The executor's job token pushed that branch, and
GitHub refuses any push of that token touching `.github/workflows/`, rejecting
the whole push. Your credential may write there. That is the only reason this
stage is yours: nothing here needs judgment.

The item's **Why the agent is here** names the staged files. That is binding
scope; do not widen it.

## 1. Move the staged workflow files

Work on `Target-branch:`, the update pull request's branch. Open no other pull
request and push to no other branch.

For each file `.claudinite/cache/pending-workflows/<name>`: move it to
`.github/workflows/<name>` (`git mv`, replacing the file there), and leave
`.claudinite/cache/pending-workflows/` empty.

**Do not edit the content, and do not judge it.** It is what the new engine
computed for this repo, its cron and stamped secrets included; a session
rewriting it is a second author of a file that has exactly one, and the landing
gate refuses anything under `.github/workflows/` that is not, byte for byte,
what the pinned engine expects. If the move is not obviously right (the branch
stages a file the item does not name, a name other than `claudinite-ci.yml`,
`claudinite-scheduler.yml` or `claudinite-executor.yml`, or the branch carries
changes beyond the pin, the member file and the staging), that is §3, not
something to improvise around.

Commit the move with `Claudinite-Task: engine/update` on its own line, and push
it to the branch.

## 2. Land it through the engine's landing path

The engine lands its update pull requests itself, and only on a CI run
dispatched for the purpose. Dispatch the workflow `claudinite-ci.yml` on
`Target-branch:` with the input `pr` set to the pull request's number. That run
checks the branch with the new engine and, once green, its `land` job runs
`cn update land`, which merges the pull request after checking that it moves
the pin and that what it changed under `.github/workflows/` is exactly what the
pinned engine expects. Never merge the pull request yourself and never arm
auto-merge: this task's `automerge` is `nothing`, because the engine's gate is
the landing.

Wait, within your time budget, for that dispatched run to conclude, and read
the pull request's state afterwards.

- Merged: converge the item `done`, passing `--pr` with the pull request's
  number.
- Anything else (the run failed or did not conclude in time, the land job
  refused, the move in §1 was not obviously right): §3.

## 3. Otherwise, leave it for a person

Leave the pull request open, add the label `task:status:needs-human-decision`
to it, and post one comment on it saying what is unresolved: which file, which
run, what refused. Then converge the item `decision`, passing `--pr` with the
pull request's number. A staged file you did not move stays owed: the next
update run finds it still staged and hands it to an agent stage again.
