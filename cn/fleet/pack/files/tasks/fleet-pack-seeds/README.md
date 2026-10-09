# Fleet pack seeds — does every member declare what this fleet standardizes on?

**This task runs no agent.** Its `code_work` is `cn fleet pack-seeds`, the engine's sweep (the `fleet/seeds` package), which the executor runs with `FLEET_GITHUB_TOKEN` from the task's declared secrets. This file is the human-facing record of what that command does; there is no agent phase.

## Why it exists

Some packs need a parameter **no member can derive**, because the answer is a fact about the *fleet* rather than about that repo — where its people's files live, which repo holds something shared, what the group calls itself.

Canon cannot supply it: a bootstrap run does not know **which fleet** it is bootstrapping into, and one fleet's value hardcoded in shared code is exactly the coupling packs exist to prevent. The enforcer can — it *is* the fleet. So this repo's `fleet` settings block lists what its members should declare:

```yaml
fleet:
  packSeeds:
    - id: <a pack>
      config: { … }
```

and the sweep converges that list across every member.

## It names no pack

Every id and every config comes from `packSeeds`. The task and its sweep carry the **mechanism** — "these declarations, in every member" — and the fleet supplies the content. A sweep that named a pack would make the enforcer a second place packs are known, and the next fleet would inherit a choice that was never theirs. A fleet with no `packSeeds` is an ordinary fleet: the sweep says so and stops before walking anything.

## What it does

Daily, over the `FLEET_GITHUB_TOKEN` PAT: read this repo's `fleet` block (`owner`, `exclude`, `packSeeds`), enumerate every repo that owner owns, and for each **covered** member read its `.claudinite/settings.*` and check whether each seeded pack's code is in its mount. Then, per seed:

| state | what happens |
|---|---|
| `set` | the member already declares that pack — read and left alone |
| `writable` | it does not (or declares it with no config) and the pack's code is present → **one commit** |
| `not-vendored` | its mount does not carry the pack yet → **waits**, no write |
| `node` | a Node engine member (a root `.claudinite-settings.json`) → **waits** for its move to `cn`, never written from the fleet |
| dormant / uncovered / archived / excluded / fork | reported under its own state, never written to |

Every repo under the owner lands in the summary under exactly one state. There is **no issue** in either direction: the finding *is* the fix, and it is applied.

## Seed, never override

A member that already declares the pack keeps its entry, and one that already carries a config for it keeps that config. Both are that repo's decisions, and the fleet's list is a **floor, not a ceiling** — the same contract the `declarePacks` migration op keeps, for the same reason.

## The mount gate

A declared pack whose code is **not in the member's mount** is a blocking `config` error there ("declares unknown pack"), and a member's mount carries only what that member declared as of its last update. So a seed is written only where the pack's code is already on disk - `.claudinite/shared/packs/<id>/`'s manifest.

`not-vendored` is a **wait, not a finding**: members update nightly, and each is written the first run after its own mount carries the pack. For a pack arriving with canon, the migration record that ships it declares it and re-vendors the mount in one transactional commit, so most members never pass through this state at all.

## The write

One PUT to the member's default branch, guarded by the blob sha the read returned (the file moving under the run is a 409, which fails that member and is retried next run). It deliberately does *not* ride the maintenance-branch lane the update delivers migrations on: there is no code in it, nothing to review, and it is idempotent. It edits the declaration in its own format, YAML, TOML or JSON: the `packs` block is spliced by the same writers `cn adopt` uses, and every byte outside it — comments included — is left as it was.

`expected_outcome: no_code_changes` is therefore not a contradiction: the ceiling describes what a task may do to **its own** repo, and this task opens no PR here at all.

## A dormant member is not written to

The run covers every member, and a member declaring `tasks.dormant` ([the scheduler's gate](../../../claudinite-growth/skills/writing-tasks/SKILL.md)) is one the sweep writes nothing to — it is read, classified `dormant`, and named in the summary under that state. It declared itself out of the recurring work, and a commit landed in it from the outside is exactly the upkeep it opted out of; its frozen mount would leave it un-writable indefinitely anyway.

## Not a fleet mechanism

Its *implementation* reads and writes every repo under the owner, but its declaration, scheduling and lifecycle are those of **any pack task**: it is active because this repo carries a `fleet` block, and it runs on this repo's ordinary scheduler. It declares no `fleet` signal and no `fleet` session scope — the cross-repo reach lives in the implementation, never in the wiring.

## Failure is loud

A member whose declaration cannot be read, or written (an unusable token, a protected default branch, a 409), is classified `unknown`: it is named in the summary and the sweep exits non-zero. The executor treats a non-zero code-work subprocess as a failed task and parks the item, so a missing **Contents write** scope escalates rather than silently leaving members undeclared.

## Why the declaration reads as it does

Carried over from the declaration's comments when it became `task.json`.

fleet task: fleet-pack-seeds — does every member declare the packs this fleet
standardizes on? `code_work: 'cn fleet pack-seeds'`: the whole pass is deterministic
code the executor runs as code-work — no agent, no dispatch issue. The sweep reads
every covered member's declaration and adds the seeds it lacks.

WHY: some packs need a parameter no member can derive, because the answer is a fact
about the FLEET rather than about that repo. Canon cannot supply it — a bootstrap run
does not know which fleet it is bootstrapping into — and one fleet's value hardcoded
in shared code is exactly the coupling packs exist to prevent. The enforcer can: it
IS the fleet. So this repo's `packSeeds` config lists what its members should
declare, and the sweep converges that list across them.

IT NAMES NO PACK. Every id comes from this repo's own config; the task and its sweep
carry the mechanism only. A fleet standardizing on different packs changes its
config, not this pack.

THE ONE SWEEP IN THIS PACK THAT WRITES. The others report a condition and converge an
issue for a human; a seed carries no human decision (the fleet already made it, in
this repo's config), so an issue asking someone to copy it into every member would be
ceremony around a mechanical edit. Hence `expected_outcome: 'no_code_changes'`: the write goes to
OTHER repos, not this one, and the outcome ceiling describes what a task may do to its
OWN repo — this task opens no PR here at all.

CLASSIFICATION (the same note the other sweeps
carry): an ORDINARY PACK TASK, not a fleet mechanism. Its *implementation* reaches
every repo under the owner over a PAT, but its declaration, scheduling and lifecycle
are exactly those of any pack task — it is active because this repo carries a
`fleet` block, and it runs however this repo's tasks run. Hence no
`fleet` signal: that describes how a task is WIRED, and nothing about this task's
wiring is fleet-shaped.

A daily sweep with nothing repo-side to gate on: what it converges is other
repos' declarations, and it no-ops on a fleet already converged.
Two or three REST reads per member (declaration, whether each seeded pack is on its
disk, and one PUT for the members being written) plus the enumeration, all serial,
with a secondary rate limit making it slower still. The same 900s the other sweeps
carry, for the same reason: ~10x the expected walk while staying inside the hourly
cadence.
