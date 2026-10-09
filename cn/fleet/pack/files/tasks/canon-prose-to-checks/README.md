# canon-prose-to-checks

## Why the declaration reads as it does

**The shelf is swept here because the shelf is curated here.** The same conversion runs in a member
repo over its own local packs (`claudinite-growth/prose-to-checks-sweep`), and that task writes
`.claudinite/local/packs/` exclusively — a member never improves the canon it mounts, since the next
converge replaces that tree whole. The two tasks share the method through the `prose-to-checks`
skill and differ only in the corpus their worker names.

**Weekly, and silent on a silent repo.** The backlog only changes when someone writes prose into the
shelf, so `repo-active` beside the cadence keeps the sweep asleep on a canon nobody is working in —
a task's own output does not count as movement, so a shelf that only this lane touched stays quiet.

**`amend_existing_or_create_new_pr`, not a fresh PR per cycle.** Each run recomputes the whole
backlog, so successive rounds belong on one pull request that accumulates the review; a fresh PR per
week buries whatever actually needed attention under the newest one.

**`under:packs` is the whole write surface.** A conversion writes the rule module or its
declaration, the fixture test and the prose it replaces - three kinds of file, all inside one pack's
directory — so the bound is the tree rather than a kind. Narrowing it by a kind class would park
every run the moment its own fixture test joined the diff.
