# usage-triage

Weekly, and the only stage of the usage loop that changes anything.

The [usage review](../../../claudinite-growth/tasks/usage-review/README.md) compares
what each skill declares about its own usage against what the record shows, and
changes nothing. This task reads the findings that have stood two weeks with a
cause a diff can argue from, and proposes the change - one pull request per
subject, carrying the edit itself rather than a description of it, with automerge
`nothing`.

The owner merges it or declines it. Nothing in the loop merges on its own, so a
rule that turns out to be wrong costs a declined pull request and leaves no trace.

## Scope

`packs/` - the shelf. The same method over a member's own local packs is
[claudinite-growth's `usage-triage`](../../../claudinite-growth/tasks/usage-triage/README.md);
both load the same skill,
[triaging-usage-findings](../../../claudinite-growth/skills/triaging-usage-findings/SKILL.md),
which states the method and names no corpus.

## When it runs

Weekly, and only when a finding has lasted. A week with none opens no session at
all, which is what bounds the cost of the one agentic stage in the loop.

## What lands

The edit, and the element's provenance entry for it in the same diff - so the log
entry merges only if the change does, and a declined proposal leaves the element's
log untouched. That entry's `Source` names the usage rule by id and the finding's
subject, with the figures it was read from.
