---
name: writing-claudinite-skills
description: Authoring or changing a skill in the Claudinite corpus: SKILL.md conventions, where its checks and tests live, pack wiring. Use when creating or editing a canon pack's skills/<name>/.
metadata:
  body: workflow
  usage:
    expect: triggered
  force-load-on-file-edits-paths:
    - "packs/*/skills/**"
---

# Writing Claudinite skills

A corpus skill is a harness-matched trigger (frontmatter `name` + a tight `description`, plus `force-load-on-file-edits-paths` under `metadata` where an edit of the files it names must not start without it — the PreToolUse guard holds such an edit until the skill is loaded; never the harness's `paths`, which limits when the skill is offered at all — and `body: workflow` or `body: guidelines` under `metadata` too, saying which kind of body follows; the growth pack's `writing-pack-prose` says how to choose) plus the body the agent needs at usage time — well under 500 lines, and nothing a deterministic mechanism could carry instead (the promotion ladder in engine/checks/DESIGN.md decides). A skill may own the rules that validate its action — `checks.mjs` beside the SKILL.md, one module per rule, with their fixture tests under the owning pack's `test/` mirroring the skill's path; shape and the relevance-first gate in engine/checks/README.md. Ownership is placement: the skill lives in exactly one owning pack's `skills/` — there is no skill catalog; the pack's own README names what it bundles. Instruction-writing *quality* — how agents actually follow docs — is [authoring-agent-docs](../../../basics/skills/authoring-agent-docs/SKILL.md)' turf; this skill is the corpus mechanics. (1)

A skill only a task's worker or a person's `/name` reaches, such as a sweep's method or a growth stage's, sets the harness's `disable-model-invocation: true` in its frontmatter: its description then leaves every session's context, the `/name` still works, and the worker reads the SKILL.md by path. Keep every other description to what decides whether to reach for the skill; the method is the body's.
