# canon-prose-to-checks worker

Mine the **existing** prose of the shelf's packs — each pack's `RULES.md` and the `SKILL.md`s beside
them — for always-testable rules that were never converted to checks, and convert the strongest ones.

A canon rule is the most expensive prose there is: every session in every declaring member pays for
it on every turn, whether or not it ever applies. Converting one to a check moves that cost to the
moment the rule is actually broken.

**The corpus is the roots this repo curates** — the `packs/` shelf, plus any root the settings'
`fleet.writePaths` names (the `promote-scope` check resolves them). Work the
*backlog*: prose a promotion run just wrote is that run's own upgrade pass, not this sweep's.

## The method lives in the skill

The conversion method — how to spot an always-testable rule in prose, judge convertibility, author
the check plus its **see-it-fail** fixture, and decide what stays prose — is owned by the
[**prose-to-checks** skill](../../../claudinite-growth/skills/prose-to-checks/SKILL.md). Follow it;
don't re-derive it here. This worker frames the unattended run around it and names the corpus.

## What a run does

1. **Pick convertible prose** under the corpus above — rules that govern **how we work** (not what a
   product does — see the skill's first gate), that are *always testable*, and that no existing check
   already covers. Converting one or two solid rules well beats churning many shakily.
2. **Convert per the skill** - author the check in its owning pack - a `declared-checks.json`
   entry, or a Go check under `checks/` on the check SDK - and add the fixture test that fires on a violating input and stays quiet on a clean
   one. Then apply the skill's **deletion test** to the prose the check now stands beside.
3. **Deliver as your instructions say to deliver a pull request**, under the title
   `Claudinite canon: prose to checks`. The commit references the tracking issue so the
   `task-lifecycle` gate passes, and the whole suite is green before you push.
4. **Say what converted in the PR body** — the prose converted and the check id it became, per
   conversion. That, and the commit, are the record; there is no standing issue. Never bump a pack's
   version or write a `VERSIONS.md` row — both are derived on the base branch after the change lands.

## What this task must never do

- **Never ship a check that can't be made confident** — the see-it-fail fixture is the gate; an
  unprovable rule stays prose. A canon check that reds on correct work costs every member repo.
- **Never convert a rule an existing check already covers** — dedupe against the check set first.
- **Never convert a statement of what a product does** — that is a requirement, and a pack is the
  wrong home for it. Found in pack prose it is already mis-homed: leave it and log it.
- **Never write outside the corpus roots above** — not a member's local packs, not `engine/`, not
  this repo's own `.claudinite/`.
