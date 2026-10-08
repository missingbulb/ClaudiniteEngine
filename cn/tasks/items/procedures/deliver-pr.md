# Delivering a scheduled task's pull request

How an executor subagent delivers the PR its task produced — the AGENT lane of the one
delivery procedure. The CODE lane is the engine's landing lane (`tasks/land`, which
Action-side runs go through); **the two must keep saying the same thing** — a nuance changed
in one changes in the other in the same change. Your GitHub writes go through the session's MCP tools, and
your pushes ride a credential whose events start workflows normally, so — unlike the code
lane — your PR's checks need no help from you to run.

## The branch and the pull request were chosen for you

**Push to the `Target-branch:` your item carries, and onto the `Target-pr:` where it
carries one.** The executor resolved both from the task's `expected_outcome` before
your session started: `fresh_pr` gave you a new branch and left the task's earlier pull
requests alone; `amend_existing_or_create_new_pr` gave you the branch of the task's
newest open pull request, or a fresh one only where the task had no open pull request
at all; `supersede_existing_pr` gave you a fresh branch and listed the earlier ones under
`Supersedes:`. Where there is a `Target-pr:`, push onto it and open nothing — the pull
request already exists and your push updates it. Where there is none, open your pull
request on that branch. Never mint a branch name, never search for an open pull
request to reuse, and never close an earlier run's pull request yourself: the converge
(`cn work converge`, handed `--pr`) closes what `Supersedes:` names once yours
exists, and a run that delivered nothing leaves them where they were.

**A `Target-pr:` that conflicts with its base is yours to resolve, not to walk away
from.** Merge the base branch into the target branch, resolve the conflicts, and carry
on with your own work on top - a task told to amend has no prerogative to open a second
pull request, and one that forks leaves the first open and accumulating beside the
second. If you cannot resolve it, stop and say so in your wrap-up comment: a parked item
naming the conflict is a correct outcome, and a second pull request is not one.

## Say which task wrote it

**Every commit you push on a task's behalf carries `Claudinite-Task: <pack>/<task>` on
its own line** — the pack and task id from the work item's first line. When you merge,
put the same line in the squash commit's message too: the merge commit is what lands on
the default branch, and it is the one the signal collectors read.

That trailer is how the scheduler tells the project moving from the machinery running. A
task's own delivery must never count as the repo activity that wakes the next task, and
a title is not a reliable way to say so — every new task's title is a new leak. Yours
says it in the commit.

## The task sets the ceiling; the repo decides the rest

Your task's declared `automerge` is a **ceiling, not a plan**.
On a request item the authorization is the item's **`Merge:` field** instead,
read within that ceiling: absent means `nothing`, `if-narrow` means the
`narrow-diff` composite, and any other value is the policy expression itself.
Whichever source it came from:

- **`nothing`** — open the PR and stop. Never arm auto-merge, never merge. Nothing below
  applies to you.
- **`anything`** — the task *may* land its PR. Whether it actually lands unreviewed is
  **this repo's** setting, read in step 1 — never the task's own knowledge. The same task
  lands itself on one repo and waits for an owner on another, and both are correct.
- **a policy list** (e.g. `['comment-only-changes', 'readme-changes']`, a folder
  scope such as `['under:product-wiki']`, or an intersection of the two,
  `['under:product-wiki && doc-changes']`) — the task may
  land its PR only when the diff sits inside the policy, and the policy engine decides
  that, never your reading of the diff. The engine has no session-side command that runs
  the policy engine over your branch yet, so no verdict is available to you: leave the PR
  open for review, say in your wrap-up that its policy could not be measured from the
  session, and stop. A diff waiting for a person is a correct outcome; arming on your own
  reading of the diff is not one.

## 1. Read the repo's delivery preference

It is the `delivery:` line `cn work validate` printed above this procedure:

- **`auto-merge`** — go to step 2.
- **`review`** — leave the PR open for the owner and stop. Never arm it, never merge it,
  and never read the standing PR as a failure: degrading an authorized landing to review is
  the repo's stated intent, and member config wins.

## 2. Arm auto-merge

Arm GitHub's native auto-merge on the PR — **squash**, always. Armed → done: GitHub lands
it once this repo's required checks pass.

## 3. When the arm is rejected

- **"Pull request is in clean status"** — the base branch requires nothing, so auto-merge
  has no queue to wait behind and this arm will be rejected every time. That is a repo
  *shape*, not an error — nothing needs fixing, and the merge is yours to make: once any
  checks that did start on the PR's head have **concluded green**, merge it yourself
  (squash). A repo with no PR checks at all merges as soon as the PR is mergeable.
- **Any other rejection** ("auto-merge is not allowed", "unstable status", …) — judge on
  evidence, exactly as the code lane's landing pass does. Read the workflow runs on the
  PR's **head sha**:
  - A run parked at `action_required` **never ran** — it is neither a pass nor a failure;
    ignore it and judge by the runs that actually executed. It can register before the
    runs that will execute, so a head whose only runs are parked is not yet judgeable:
    keep waiting for the real ones to appear.
  - Wait (within your run's time budget) for the real runs to conclude. Everything
    concluded, nothing failed, at least one succeeded → merge (squash).
  - Anything genuinely failed (`failure`, `timed_out`, `cancelled`, `startup_failure`),
    or the runs won't conclude inside your budget, or GitHub refuses the merge itself
    (a required gate you could not see) → **leave the PR open** and say why in your
    wrap-up comment, naming the repo settings a human should check: Settings → General →
    "Allow auto-merge", and Settings → Actions → General workflow-approval requirements
    (the usual source of the parked `action_required` run). A PR left open with its
    reason stated is a *delivered* outcome within the ceiling — the trail
    survives and the task's next cycle (or the owner) picks it up; a merge past a red or
    unseen check does not survive anything.

## Never

Merge with anything but squash; merge while a real check is failing or still running;
arm or merge when the repo said `review`; or
manufacture a merge to satisfy the ceiling — "no change" and "left open, reason stated"
are always legal outcomes.
