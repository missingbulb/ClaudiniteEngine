<!-- GENERATED — do not hand-edit; every update rewrites it. Edit a skill's SKILL.md frontmatter. -->
# Skills mounted here, and what loads each one

A skill loads when the session's activity matches its description. A skill that names files
under `force-load-on-file-edits-paths` is also forced for them: a file tool aimed there is held
by the PreToolUse guard until the skill is loaded (the Skill tool, or a Read of its SKILL.md),
and an edit made another way is caught at Stop.

## By activity

| Skill | Pack | Loads when |
|---|---|---|
| `demo` | basics | A skill a declared pack bundles. |
