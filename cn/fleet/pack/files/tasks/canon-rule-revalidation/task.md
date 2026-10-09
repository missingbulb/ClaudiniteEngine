# canon-rule-revalidation worker

Re-run the probe behind every rule on the shelf that asserts a fact about the environment — the shape
a harness call accepts, what the Action's token may reach, that an MCP tool exists, how a platform
behaves — and correct what no longer holds.

Nothing in this repo turns red when a platform moves under such a rule, and the members that vendor
it cannot fix it: they receive the rule and no evidence behind it. A stale canon claim is therefore
paid for once per session per member until it is re-probed here.

**The corpus is the roots this repo curates** — the `packs/` shelf, plus any root the settings'
`fleet.writePaths` names (the `promote-scope` check resolves them).

## The method lives in the skill

How a claim is judged revalidatable, how it is probed, what the four verdicts mean, how an
element's provenance file is reaffirmed and what the run appends to it - and how an empty file met
on the way is filled first - are owned by the [**revalidating-rules**
skill](../../../claudinite-growth/skills/revalidating-rules/SKILL.md). Follow it; don't re-derive it
here. This worker frames the unattended run around it and names the corpus.

## What a run does

1. **Enumerate** every revalidatable claim under the corpus above, by the skill's test. Most of a
   pack is judgment prose and out of scope, so this set is far smaller than the corpus.
2. **Probe each**, per the skill's two probe rules, and record what you ran and what came back. The
   probes that matter most are the ones a member could never run: this session's reach is the canon
   home's, and a claim about a member's own environment is that member's to revalidate.
3. **Correct what is stale**, as far as each probe reaches and no further.
4. **Deliver as your instructions say to deliver a pull request**, under the title
   `Claudinite canon: rule revalidation`. The commit references the tracking issue so the
   `task-lifecycle` gate passes, and the whole suite is green before you push.
5. **Report every verdict in the PR body** — every claim probed, its verdict and the probe behind it.
   This task rewrites the rules every member's sessions obey, on evidence a reviewer cannot re-derive
   from the diff, so the body carries that evidence in full whether or not anyone reads it. Never
   bump a pack's version or write a `VERSIONS.md` row — both are derived on the base branch after the
   change lands. There is no standing issue.

A run that found everything still true opens no PR at all, and says so in its run log.

## What this task must never do

- **Never rewrite a rule into "you cannot do X" because this session could not** — an unrunnable
  probe is `unprobed`, and the skill's second probe rule is absolute. Removing a capability from
  every member's sessions on one session's missing reach is the worst outcome available here.
- **Never retire a rule on this run's own verdict** — a workaround whose problem no longer
  reproduces is proposed in the PR for the owner.
- **Never write outside the corpus roots above** — not a member's local packs, not `engine/`, not
  this repo's own `.claudinite/`.
