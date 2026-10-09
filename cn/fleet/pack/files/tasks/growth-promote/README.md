# growth-promote

## Why the declaration reads as it does

Carried over from the declaration's comments when it became `task.json`.

growth-promote — the growth lifecycle's CENTRAL stage. Reads the participating
members' local packs, generalizes the portable lessons, and opens an owner-gated
PR against the canon. A fleet-reaching task: it runs on the canon home repo's own
scheduler, and its precondition reads the `fleet` signal — the members aggregate
over the fleet PAT.


"Central, once" is enforced by declaration cardinality, not orchestrator wiring:
a canon is declared by its one home repo, so this task exists nowhere else. No
barrier — promote reads whatever is already MERGED on members' default branches,
so a lesson extracted tonight is promoted on the same run when its auto-merge
landed in time, else the next one.
THE CROSS-REPO HALF IS THIS REPO'S OWN scheduler anchor, not a field here. Promote reads what
has already merged on MEMBERS' default branches, and `schedule_after:` only ever matches an item
in this repo's own queue — so the members-extract-then-canon-promotes ordering has no declarable
form and lives in the anchor: a canon home sets its daily hour after its members'.

The WITHIN-repo half is declarable, and is: a canon home is a member too, so its own extract must
settle before promote reads the local packs it just wrote. A canon that does not declare
claudinite-growth queues no such item, and the ordering is then vacuous rather than blocking.
Fire on the members whose local packs moved in the window — the term beside
this file (preconditions.mjs) both decides that and names them, so the
executor's Context binds the worker to exactly that set.
This task reads every member's local packs, which an ordinary session in this
repo does not reach. Reach is a property of WHICH endpoint the hand-off calls,
so a task needing more than an ordinary session names one; the key resolves in
this repo's own config, and until it is configured the hand-off converges the
item to triage naming what is missing.
