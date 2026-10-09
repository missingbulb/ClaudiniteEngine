# Fleet roster — one walk, two questions about every repo under the owner

**This task runs no agent.** Its `code_work` is `cn fleet roster`, the engine's sweep (the `fleet` and `fleet/roster` packages), which the executor runs with `FLEET_GITHUB_TOKEN` from the task's declared secrets. This file is the human-facing record of what that command does; there is no agent phase.

## What it does

Daily, over the `FLEET_GITHUB_TOKEN` PAT: read this repo's `fleet` settings block (`owner`, `exclude`) out of its own checkout, enumerate every repo that owner owns, and walk it **once** — one read of each repo's `.claudinite/` and its declaration, the manifest of each pack a member declares, and one scheduler-workflow read for each member the freshness question actually measures. What a member would move to is read once per sweep from npm and the pack indexes, the same reads that member's own `cn update` makes.

That one roster then answers two questions, each in its own section of the run summary:

| question | module | finding | where the answer goes |
|---|---|---|---|
| is this repo a **member**? | adoption | an uncovered repo under the owner | the run report's coverage section, which names it and how to bring it in (`cn adopt`) or leave it out (`exclude`) |
| is that membership still **meaning** anything? | freshness | a covered member whose mount has fallen behind | the run report's freshness section |
| what does every repository read as? | the roster artifact | one `fleet.Verdict` per enumerated repository, the manager's own row with `scope: home` | `.claudinite/fleet/roster.GENERATED.json` in this repo, which a fleet dashboard reads |

It **reports; it files nothing and repairs nothing.** A repo outside the fleet is shown, never nagged. The one file it writes is its own artifact, in this repo: `cn fleet roster` rewrites `.claudinite/fleet/roster.GENERATED.json` when any verdict moved (a recompute that differs only in its `generated` stamp writes nothing), and the executor lands it under `expected_outcome: amend_existing_or_create_new_pr` with the `fleet-roster-artifact` automerge policy, which names that one path, so one pull request accumulates the day's changes and merges without a person.

## Why one task

These were two — a daily `fleet-census` and a weekly `fleet-freshness` — and each carried its own enumeration, its own owner filter, its own empty-enumeration guard, its own home/canon/archived/fork/excluded skips and its own declaration read per repo. The freshness half's header said it *"takes coverage as given"*, but it could not: the census's verdict lived in another process on another cadence, so it re-derived the whole thing.

That produced divergence in the classification itself. `exclude` was applied at different points, so an excluded repo that still carried a declaration read **covered** to one sweep and **out of scope** to the other. And each half's `unknown` failed its own run knowing nothing of the other's, so one green run beside one red one told a reader nothing about which half of the fleet picture to trust.

One walk means one membership verdict per repo, one report, and one failure boundary. See [#788](https://github.com/missingbulb/Claudinite/issues/788).

## The freshness classification

For each measured member, by **root cause**, in this precedence:

| state | meaning | what fixes it |
|---|---|---|
| `node` | a root `.claudinite-settings.json`: a Node engine member, covered and not compared | moving it to `cn` (the move-member-off-node skill); until then its versions are the Node engine's, which nothing here reads |
| `no-stamp` | declares packs but carries no readable engine pin and no declared pack holds a manifest version — never vendored | run the adoption flow (the `adopt-claudinite` skill, which runs `cn adopt`). Until then the declaration names packs whose code is not present. |
| `no-scheduler` | no `claudinite-scheduler.yml`, so no cron, so it will never update itself; every other symptom is downstream of this | `cn adopt` writes the scheduler; confirm the workflow is enabled in the Actions tab. |
| `behind` | its own update would move it: npm offers a newer engine than its pin, or a pack index offers a newer version of a pack it holds | read the member's recent `Claudinite scheduler` runs: a disabled workflow (GitHub disables cron after 60 days of no activity), a failing update task, or an update PR that never merges all look like this. The gap closes only when its update lands. |
| `fresh` | its own update would move nothing | — |

A **dormant** member carries no verdict from this table at all: it is named under `dormant` and measured by nothing. None of the remedies above would apply anyway, because nothing there is meant to be running.

### What `behind` measures, and what it deliberately does not

**What the member's own update would decide**, and nothing else: the newest allowed engine for its pin's package (held, revoked and deprecated versions skipped) and the newest allowed version of each pack it holds, on its channel, for its engine. That is the definition of current a member acts on, so the sweep and the member cannot disagree about it. There is no canon repository to compare against.

A pack the shelf does not offer has no newer version to be behind, so it contributes no gap; an absent number never reads as zero.

## Who is measured by which question

Every repo lands in exactly one bucket per question, and the two disagree on purpose:

- **The enforcer** is censused by neither — it is named in both summaries and swept by its own scheduler.
- **A Node member** (a root `.claudinite-settings.json`) is a covered member to the census and is named under `node` by the freshness half, never measured.
- **An ignored repo** (`config.exclude`) is out of both questions and is never read: the walk skips it before the declaration fetch, so the sweep does not learn whether it mounts Claudinite, and each half names it once under `ignored`. "Ignore all aspects" is the owner's word (2026-09-13), and a verdict — even a favourable one — is an aspect.
- **A dormant member** (`dormant` on its `claudinite-tasks` entry, [the scheduler's gate](../../../claudinite-growth/skills/writing-tasks/SKILL.md)) is a **covered** member to the census and is **not measured** by the freshness half: its mount probe is not even paid for. Nothing converges it and no fleet-wide operation touches it, so a version gap there is a finding nobody owns; what the report owes the reader is the fact that it was left alone. The test is the member's `claudinite-tasks` entry's `config.dormant`, strictly `true`, the same predicate its own scheduler stops on.

## Why daily, and what it costs

The freshness question was weekly because drift is measured in days and a daily re-ask could not change its answer. Merged, that argument buys nothing: the walk runs daily for the coverage question regardless, and gating half the task on a cadence it computed itself would reimplement dueness — which the engine owns (the scheduler run instantiates a task's item when its anchor comes) and is not something a task can ask about from inside itself.

So the freshness probe runs daily too, at roughly **one extra REST read per declared pack and one per covered member** on the six days that used to be coverage-only. **This merge is not an API-call saving and is not claimed as one.** What it buys is one roster instead of two that can disagree; the freshness verdict refreshing within a day rather than a week is the side benefit.

## Not a fleet mechanism

Its *implementation* scans every repo under the owner, but its declaration, scheduling and lifecycle are those of **any pack task**: it is active because this repo carries a `fleet` block, and it runs on this repo's ordinary scheduler. It declares no `fleet` signal and no `fleet` session scope — the cross-repo reach lives in the implementation, never in the wiring.

## Failure is loud, and now per-question

A repo whose **declaration** cannot be read or parsed is `unknown` to **both** questions — it is the input they share. A repo whose **freshness read** fails (the scheduler read, npm, a pack index) is `unknown` to the **freshness** question alone: the coverage question already read that declaration successfully and keeps its verdict.

Either kind fails the run: the sweep exits non-zero with both halves' unknowns named together. The executor treats a non-zero code-work subprocess as a failed task and parks the item; a 403 the token's grant explains prints `claudinite-needs-human: action`; the executor parks the item `needs-human-failure` and names the `action` kind in the park comment, so an unusable token or scope escalates rather than silently shrinking the fleet. When the fleet check finds the owner on no fleet plan, or the job has no Actions OIDC token to ask with, the command reads no member, says why and parks the same way; a license server that does not answer leaves the run unverified rather than parked.

## Why the declaration reads as it does

`code_work: "cn fleet roster"` with `code_work_required_secrets: ["FLEET_GITHUB_TOKEN"]`: the whole pass is deterministic engine code the executor runs as code-work, and the PAT reaches that one step alone. The artifact lands through the job's own token: a shell `code_work` has no SDK, so the executor commits the change it leaves in the checkout with the task's trailers, pushes it to the branch it resolved and opens or amends the pull request there. `automerge: ["fleet-roster-artifact"]` is the prediction of that diff, the one roster file added or modified, so a diff reaching any other path parks for a person.

This is an **ordinary pack task**, not a fleet mechanism. Its *implementation* scans every repo under the owner over a PAT, but its declaration, scheduling and lifecycle are exactly those of any pack task: it is active because this repo carries a `fleet` block, and it runs however this repo's tasks run. The cross-repo reach lives in the implementation, never in the declaration.

A daily sweep with nothing repo-side to gate on: coverage and freshness are facts about OTHER repos, and the walk no-ops cheaply on a converged fleet. The 900s bound is ~10x the expected walk while staying well inside the hourly scheduler cadence, so a hung sweep is killed long before the next run could collide with it.
