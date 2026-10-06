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

## 2. Get it green, pass the engine's gate, and merge it

GitHub refuses a merge that changes `.github/workflows/` when the job token
asks, so the engine's own `land` job skips this pull request and the merge is
yours, behind the engine's gate. This task's `automerge` is `nothing` because
no delivery lane may merge it: the only authority to merge is that gate,
`cn update land --check`, at the head it passed.

1. Dispatch the workflow `claudinite-ci.yml` on `Target-branch:` with the input
   `pr` set to the pull request's number. Its `check` job runs the new engine
   over the branch.
2. Wait, within your time budget, for every CI run on the branch's head (the
   commit you pushed in §1) to conclude. Each must be green.
3. Run the gate. It reads git alone, so it needs no GitHub token: fetch
   `main` and `Target-branch:` from `origin`, check that the fetched branch is
   the head you read from the pull request (if not, it moved: wait for CI on
   the new head, or §3), and run
   `.claudinite/bin/cn update land --check --base <main's commit> --head <head sha>`.
   It writes nothing; it prints `ok: <head sha> may land <version>` and exits 0
   only when that head moves main's pin and changes, under
   `.github/workflows/`, exactly what the pinned engine expects.
4. Squash-merge the pull request at that head sha (pass the sha, so a moved
   branch refuses the merge), titled `Claudinite engine <version>`, the
   version the gate printed. Delete its branch. Dispatch `claudinite-ci.yml`
   on `main` with no input, so the next update finds main's CI run.
5. Converge the item `done`, passing `--pr` with the pull request's number.

Never merge on a red or unconcluded run, on a refused gate, or at a head other
than the one the gate passed, and never arm auto-merge. Any of those, or a
move in §1 that was not obviously right, is §3.

## 3. Otherwise, leave it for a person

Leave the pull request open, add the label `task:status:needs-human-decision`
to it, and post one comment on it saying what is unresolved: which file, which
run, what refused. Then converge the item `decision`, passing `--pr` with the
pull request's number. A staged file you did not move stays owed: the next
update run finds it still staged and hands it to an agent stage again.
