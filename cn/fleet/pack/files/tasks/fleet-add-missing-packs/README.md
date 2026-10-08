# Fleet: get every member declaring the packs it is missing

**This task runs no agent.** Its `code_work` is `cn fleet add-packs`, the engine's sweep (the `fleet/addpacks` package), which the executor runs with `FLEET_GITHUB_TOKEN` from the task's declared secrets. This file is the human-facing record of what that command does; there is no agent phase on the enforcer side. The *agentic* half of the job belongs to each member's own **adopt-requested-packs** task (claudinite-growth) — see "The fan-out model" below.

## Two first stages, one parameter set

The task is parameterised over the two ways a pack comes to be missing (no parameter has a default; both call sites say everything):

| run | parameters | first stage |
|---|---|---|
| weekly (scheduled) | `--scan-for-needed-packs=true --repos=all-covered-members`, on the `code_work` line in [`task.json`](task.json) | the **scan**: fingerprint every covered member's tree against every pack the shelf's signed `catalog.json` offers on **each member's** channel, and *suspect* what its declaration does not carry |
| forced (hand-created item) | the item's Context, one `--context` line each: `SCAN_FOR_NEEDED_PACKS=false`, `REPOS=Alpha Beta`, `ADD_PACKS=<ids>`, `PACK_CONFIG=<pack>.<key>=<v>`, `PACK_ANSWER=<pack>.<question>=<answer>` (values space-separated — the bag splits on commas) | the **force**: the owner names the packs, repos, config and interview answers — nothing is suspected, because it was decided |

A force **refuses itself entirely** — before any issue is written — on a repo this fleet ignores, a pack id the catalog does not offer, a repo that is not a covered member or is dormant, `all-covered-members` as a target, or **any adoption-interview question the overrides did not answer**: an answer is the owner's to give, never one this task may infer.

## The fan-out model

Both stages end the same way, per member with work (the protocol `cn fleet protocol` prints, which the member's half holds its own copy to):

1. **Converge one work-list issue in that member**, unlabelled and keyed by its title — `Add packs: requested for this repo` (a decision, carrying the exact declaration entries as JSON, config and answers included) or `Add packs: suspected from this repo’s shape` (a suspicion, carrying the evidence and the fingerprints the REST sweep could not decide).
2. **Mark it** `task:origin:ad-hoc`, with `Task: claudinite-lifecycle/adopt-requested-packs` in the body. The issue then *is* that member's work item: its own scheduler run adopts it, its own executor picks it up. A second work list in the same member names the first in `Blocked-by:`, so the two run one after the other rather than putting two sessions on one declaration.
3. **Nudge it** by dispatching that member's scheduler — an ordinary run, no `wake`. This is latency only: a member that refuses the dispatch adopts the mark on its own next hour.

The member's agent reads the issue it is running on, its own executor confirms/adopts with the repo checked out, and one reviewed PR lands *there*. Nothing here writes to any member's tree, and no agent anywhere needs cross-repo access — the one fleet credential is `FLEET_GITHUB_TOKEN`, granted as `cn fleet token` states it.

## Convergence

- A **requested** issue closes `completed` once the member's declaration carries everything its JSON block asks for — checked on every weekly visit, whichever run opened it.
- A **suspected** issue closes `completed` once the member declares the packs (or its shape stops suggesting them), and a `not planned` close is a standing decline the scan honours rather than re-suggesting weekly.
- A member with a still-open work list is **nudged again** on the weekly visit, and a work list whose ask has actually changed has its status cleared — the sanctioned re-ask, and the retry loop for a member whose earlier adoption run parked.

## Failure is loud

A member that could not be swept is `unknown` — never "fitted" — and that fails the run; the executor parks the item. A member whose scheduler refused the nudge dispatch is only reported: since the work list is a marked issue, the dispatch decides when it is adopted, never whether.

## Why the declaration reads as it does

Carried over from the declaration's comments when it became `task.json`.

fleet task: fleet-add-missing-packs — get every member declaring the packs it
is missing. The fourth fleet question, now fully on the FAN-OUT model (#749).

`agent_model: 'none'` — this task runs no agent, HERE. Its first incarnation ended
in an enforcer-side agent stage that ran adopt-pack against members, and it failed
in production the first time it ran: the enforcer's executor is (correctly) scoped
to the enforcer repo alone, and "the enforcer's session is provisioned with the
fleet" turned out to be an assumption, not a fact. The agentic work moved to where
the access already is: each member's own adopt-requested-packs task
(claudinite-growth). This task DISPATCHES — it converges a work-list issue in
the member and fires that member's scheduler — and the member EXECUTES, with the
repo checked out, under its own declaration's guards. No agent anywhere needs
cross-repo access.

TWO WAYS A PACK COMES TO BE MISSING, and the task is PARAMETERISED over them
rather than split in two, because they differ only in how the work list is made:
  scan_for_needed_packs=true   nobody has decided anything: fingerprint every
                               member's shape and SUSPECT what its declaration
                               does not carry. What the weekly run sends.
  ADD_PACKS=<ids>              the owner already decided: REQUEST these packs,
                               with this config and these interview answers, in
                               these named repos. What a FORCED run sends, through
                               the item's Context.
Both converge the same protocol issues and fire the same member-side task.

WHERE THE FLEET REACH COMES FROM: FLEET_GITHUB_TOKEN, the account-spanning PAT —
with Actions WRITE, because firing another repo's scheduler is an Actions write.
No session scope, no executor grant: the one credential is the PAT, and the only
things that cross a repo boundary are an issue and a workflow_dispatch.

Fire weekly unconditionally. Every input lives OUTSIDE this repo — another
member's tree, another member's declaration — and no per-repo collector can
see any of them, so there is no signal that would say in advance whether the
answer changed. Cheap to no-op: a fleet that declares what it should converges
nothing and fires nobody.

A hand-created item runs against this too, and it says yes — so what makes a
forced run different is its Context, not a bypass: the parameters there are
what run this task as something other than its weekly self.
The weekly run's PARAMETERS, sent explicitly on the command line rather than
defaulted inside the sweep: the declaration is where a reader looks
first to learn what the cadence does, so what the cadence does is written here.
`all-covered-members` is a keyword the caller sends, not a fallback the sweep
assumes — no call site can reach the whole fleet by omission.
One tree listing per member plus a bounded handful of content reads per
content-reading fingerprint, plus the per-member issue convergence and one
dispatch POST per member with findings, all serial with a secondary rate limit.
The same 900s the other sweeps carry: ~10x the expected walk.
