# canon-rule-revalidation

## Why the declaration reads as it does

**The shelf's environmental claims are revalidated here because only here can they be fixed.** A
member vendors a canon rule read-only and receives none of the evidence behind it, so a claim that
has gone stale is re-probed in the repo that owns the pack. The member-side twin
(`claudinite-growth/rule-revalidation`) does the same for that repo's own local packs, through the
same `revalidating-rules` skill; the corpus each worker names is the whole difference.

**Weekly, and no repo-movement gate.** The trigger is a *platform* moving, which no repo-side signal
can see — so the cadence stands alone. The member-side twin states `repo-active` because a local
pack nobody works with costs nobody anything; that reasoning does not carry here, where every
declaring member reads the shelf on every session whether or not this repo was touched this week.

**`amend_existing_or_create_new_pr`.** Each run re-probes the whole corpus and holds no state
between runs, so successive rounds belong on one pull request that accumulates the review rather
than a fresh one per week that buries the round before it.

**`under:packs` is the whole write surface.** A correction lands in a pack's prose, sometimes in a
check's `fix` text or a fixture beside it, and sometimes in the `references.md` entry that recorded
what would retire the rule — all inside one pack's directory, and more than one kind of file, so the
bound is the tree.

**The evidence requirement is the real gate, not the policy.** A correction here reaches every
member, and a reviewer cannot re-derive a probe from the diff — so the worker's PR body must carry
every verdict and the probe behind it, including the ones that changed nothing. That, and the
skill's refusal to disprove a claim a session merely could not probe, are what keep a thinly
evidenced rewrite from riding an in-scope diff onto the shelf.
