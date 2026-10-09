# Growth — discover canon packs (the fleet sweep)

One weekly pass over **every member**: read what the fleet is actually built on, subtract what the shared canon already homes, and open a PR **for the owner to approve** authoring the pack that is missing. Declared only by the canon's home repo, so it runs **once, centrally**, never per project.

**Your scope is every COVERED member** — one whose declaration parses and names at least one pack. Enumerate them once at the start of the run and work that list; a member with no declared packs is not running Claudinite in any usable sense and is not swept. Finding nothing unhomed is a perfectly good, common outcome.

## Conventions used in this doc

- **GitHub access is MCP-only** (`mcp__github__*`). You are outside each member repo — read its files over the API (get-file-contents, never a cross-repo checkout: the shell's git can reach another repo, and not doing so is the session's granted scope rather than a missing capability). The canon PR is opened over MCP too.
- **`main`** stands for the canon's default branch.
- **The canon shelf** is this repo's own `packs/` tree — the packs every member can declare.

## What each run does

### 1. Manifest each member's stack

For each member named in Context, catalogue what it is built on and how it ships — grounded in that repo's files (dependency/build manifests, lockfiles, toolchain/config, CI and release workflows, packaging/signing scripts, source structure, docs), citing the concrete evidence. Keep one manifest **per member**, tagged with the repo it came from. Do **not** yet consult the canon shelf or decide anything about packs — this step only observes.

**Never infer from "projects like this usually…"; if the member's own files don't show it, it is not in that member's manifest.** If something is present but appears vestigial or aspirational (declared but unused), include it and say so. Be **comprehensive over concise**: a later step filters and decides, so a true item you omit is lost, while an over-included one is cheaply dropped. When unsure whether something rises to an entry, include it with the `?` flag.

Catalogue across **three axes**. Put each item under the single axis that fits best; when it genuinely spans two, place it under the primary and cross-note the other.

- **Technologies** — languages and their versions, runtimes, frameworks, build systems, and the major libraries that shape how you write and build there (the load-bearing ones, not every transitive dependency).
- **External services / APIs** the member integrates with.
- **Deployment / distribution mechanisms** — how it ships and to where.

For **each** item report: **name**; **axis**; **evidence** (the file(s), and what they show); **what it is in that member** (one line); **prominence** — one of `core` (the project is built on it), `supporting` (used but peripheral), `vestigial` (present but apparently unused); and a **`?` flag** if you are uncertain the item is real or correctly characterised. Prominence is a factual read of how central the item is *in that member* — **not** a judgment about whether it deserves any downstream treatment. Output each member's manifest as Markdown grouped under the three axis headings, one bullet per item with the fields labelled.

### 2. Fold the members into one fleet view

Merge the per-member manifests into a single list of technologies, keyed by the technology, each carrying **which members use it** and at what prominence. This fold is the whole reason the task is central: the same technology seen in four members is **one** entry, recognised on first sight, so there is no cross-run dedup state and no duplicate proposal.

Drop anything no member uses at `core` or `supporting` prominence — a technology only some member's vestigial config mentions has taught the fleet nothing.

### 3. Find the canon gap

Hold the fleet view against the **canon shelf**. A candidate is a technology (or a domain the fleet shares) that:

- **no canon pack homes** — and **a stub pack counts as homed**: if `packs/` already has any pack owning it, the gap is closed. The action for a member using a homed technology without declaring its pack is to note that ("member X should declare pack Y"), never to author a second pack; **and**
- carries **real, portable working knowledge** the members actually demonstrate — a build/config gotcha, a toolchain rule, a procedure that recurs across them. Judge worthiness by the bar [item-routing.md](../../item-routing.md) owns; don't restate its gates.

Prefer a candidate **more than one member** uses — that is the evidence a pack is portable rather than one repo's local business. A single-member technology can still qualify when the knowledge is plainly general, but it is the weaker case: one repo's own business is not canon material, and belongs in that repo's local packs instead.

### 4. Author the pack — from the members' real usage

Author `packs/<name>/` using the pack-writing mechanics [the extract-packs-from-a-project skill](../../skills/extract-packs-from-a-project/SKILL.md) owns - descend the promotion ladder, add the `marker`/`detect` fingerprint when the technology carries a reliable one (`detect: null` otherwise), write `RULES.md`/`pack.json`/`README.md`, and add its `packs/README.md` index row. Apply that method; do not re-derive it.

Two rules this task most easily breaks, because it is writing from *other* repos' evidence:

- **Ground every rule in a member's real files, and cite which member.** Never author from imagination.
- **Never pad** with speculative best-practice rules the evidence doesn't demonstrate, and **never open an empty stub to fill later**. A small honest pack beats a padded one; a rule you can't ground, you don't write.

Strip the member (names, paths, and product concepts stay in the origin repo — a canon pack is written as if no particular repo existed); a technology mention is the point, a *product* mention is a defect.

**One pack per run.** A new pack earns its own review surface, and a run delivers on one branch, so a run that finds two gaps authors the stronger one and names the other on the tracker for the next pass.

### 5. Open the PR

Deliver on the branch your item names — push to `Target-branch:` and open the PR against `main` on it, the way your instructions say to deliver a pull request. Never a direct push, never a search for an open pull request, never a branch of your own. Write surface is **`packs/` only**: the new pack's files and the `packs/README.md` index row. Don't "improve" unrelated packs while you're in there. Keep every commit and PR terse, and **put the issue reference in the commit message** (`Refs #<n>` for the tracker below), in the commit itself and not only the PR body.

### 6. Tell the members that evidenced the pack

A pack is authored because a member's real files demonstrated the knowledge — so that member is the first repo that should be running it, and the sweep is the only thing that knows why. For **each** member the fleet view records at `core` or `supporting` prominence for the technology, open **one issue in that member repo**, titled exactly `Adopt canon pack: <name>`. Find it by that exact title first and leave an existing one alone — one issue per member per pack, converged, never a second copy on a later run.

The body carries what the member cannot re-derive: **the evidence** (that repo's own files, the ones the pack was grounded in), **the canon PR** link, **the blocker** (nothing to do until that PR merges *and* this repo's next update carries the pack — declaring a pack whose code is not on disk is a blocking `config` error), and **the action** once both hold: the `adopt-pack` skill, which declares it, asks its adoption questions and re-vendors.

Two things it is not:

- **Not the `add-packs` protocol.** Don't use either of its converged titles — those issues are a member's `adopt-requested-packs` task's input, and pointing it at a pack no member's mount can carry yet is exactly the blocking error above. This is a plain issue for a human.
- **Not a substitute for the fingerprint sweep.** `fleet-add-missing-packs` will suspect the pack in a member once it is in canon and its `detect` fingerprint matches. This issue is the immediate signal, carrying the evidence that sweep never had — and the only signal at all for a pack whose fingerprint is `detect: null`.

A member the fleet view shows only at `vestigial` prominence gets no issue: it did not evidence the pack.

If an issue cannot be created (no Issues write on that member), that is a **tracker line, not a failed run** — the PR is the deliverable and it already exists. Name the member and what failed, and open nothing here.

The "member X should declare existing pack Y" note from step 3 stays a tracker note: the pack is already in canon, so the fingerprint sweep converges an actionable issue for it on its own.

### 7. Log the run to the tracker

The standing log is the issue titled exactly **`Claudinite tracker: Discover Canon Packs`**.

- **Find it by that exact title, never a fuzzy match or a hard-coded number** (a bare number can dangle).
- **Create it already closed if missing** — creation always lands an issue open and ignores a `state: closed` argument, so create it and close it in a second call. **Never open, close, or reopen it** afterward — its state carries no meaning; only the log does.
- Log every run as a **dated comment**: the members swept; each pack authored (technology, the members that evidenced it, the rungs its rules landed on); the adopt issue opened in each of those members (or why it could not be); each "member X should declare existing pack Y" note; and, for a candidate you rejected, its name and why. "Nothing unhomed this run" is worth a line too.

## What this task must never do

- **Never widen past the Context member list**, and **never write to a member's tree** — the only thing this task writes in a member is the step 6 adopt issue, in a member that evidenced the pack it just authored.
- **Never author a pack for a technology the canon already homes** — a stub counts as homed; note the "declare the existing pack" action instead.
- **Never author from imagination or pad, and never open an empty stub** — every rule traces to a named member's real files.
- **Never write a member's product names, paths, or concepts into a canon pack** — a canon pack reads as if no particular repo existed; repo-specific knowledge stays in that repo's own local packs, which growth-extract writes.
- **Never write outside `packs/`** — no engine, no docs, no change to any member's files.
- **Every step is a judgment call** (is this technology genuinely unhomed, has the fleet earned a shared pack for it, what does the evidence actually support), and you are authoring from *other* repos' evidence with no owner able to re-derive each call.
