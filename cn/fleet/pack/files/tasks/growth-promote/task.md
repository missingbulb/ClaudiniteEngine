# Growth — promote lessons into the canon (the central stage)

Read the target members' local packs, lift the portable lessons into the shared canon, and open a PR against the canon's default branch **for the owner to approve**. Declared only by the canon's home repo, so it runs **once, centrally**, never per project.

The run's **Context section is binding scope**: it names the exact target members (those whose local packs changed in the window). Work exactly that list — the fleet enumeration already happened in the precondition; **do not enumerate the fleet yourself or widen past the Context**.

## Conventions used in this doc

- **GitHub access is MCP-only** (`mcp__github__*`). You are outside each member repo — read its files over the API (get-file-contents, never a cross-repo checkout: the shell's git can reach another repo, and not doing so is the session's granted scope rather than a missing capability). The canon PR is opened over MCP too.
- **A member's local packs** are everything under `.claudinite/local/packs/` — its own packs, never the read-only mounted canon elsewhere under `.claudinite/`.
- **`main`** stands for the canon's default branch.
- **The canon shelf** is this repo's own `packs/` tree — the packs every member can declare. Together with any second corpus root the canon declares, it is the whole write surface below.

## What each run does

### 1. Read the target members' local **packs**

For each member named in Context, read its **local packs**. The candidate pool is a local pack's captured knowledge — its `RULES.md` **prose** AND its own **checks** (a `<pack>/*.mjs` rule module — its id, description, and failure message *are* a captured, enforceable lesson; the strongest promotion lifts it to a **canon check**, generalized, reusing the [prose-to-checks](../../../claudinite-growth/skills/prose-to-checks/SKILL.md) mechanics — or to canon prose when it doesn't generalize to a check). **Out of scope, staying put:** a lesson deliberately placed as a **code comment at its call site**, a project's task scripts, product source, and config. Never promote from product source. A genuinely portable rule that looks trapped in a code comment was mis-placed at extract time — leave it for a future extract pass to re-home into a pack. You don't need to tell new from old — the dedup step drops anything the canon already carries. No barrier holds tonight's extracts: read whatever is already **merged** on each member's default branch; anything landing mid-run is tomorrow's input.

### 2. Generalize each candidate — *before* judging whether it belongs

Generalize first, then decide worthiness — never the reverse. The act of generalizing is itself the portability probe — a rule that goes vacuous when lifted, or only makes sense with its project's context, isn't portable; leave it local. Work the rule standalone (origin naming and grouping carry no weight); strip the project (names, files, example stay in the origin repo — promote the distilled rule only); a **technology** mention isn't a disqualifier (lift it to the broadest scope that stays true — tech-specific is fine, *product*-specific is not); decide tool-vs-process and frame it for that home.

### 3. Then judge worthiness — deduped against the *entire* corpus

Holding the generalized rule, decide if it earns a place by the bar [item-routing.md](../../item-routing.md) owns (don't restate its gates). Lean **inclusive** — keep anything a future project could use — but be strict about **duplication**: dedupe against **every** corpus doc, not only the one you'd expect to own it.

### 4. Triage the mechanism, then route

- **Descend the promotion ladder first** ([item-routing.md](../../item-routing.md)'s mechanism triage). Prose is the fallback, not the default: a lesson a check can carry is **authored as the check** in this PR — the rule module in the right pack, the failure message carrying the generalized lesson, **plus a fixture test proving it fires** on a violating input and stays quiet on a clean one. When a confident detection-plus-fixture can't be authored unattended, land the lesson as prose **and** open an issue marked `task:origin:ad-hoc` to author the check — never a silently-shipped broken check.
- **Route** each prose rule to its **one** owning doc ([item-routing.md](../../item-routing.md) owns the placement); match its voice and format; pick exactly one owner, never split or duplicate.
- **A technology with no home → mint a fingerprinted stub pack, in its own PR**, reusing the pack-writing mechanics [the extract-packs-from-a-project skill](../../skills/extract-packs-from-a-project/SKILL.md) owns - `packs/<tech>/` with `pack.json` (add the `marker`/`detect` fingerprint when the technology carries a reliable one, else `detect: null`), a `RULES.md` seeded from the lesson, a `README.md`, and its `packs/README.md` index row. A minted pack is **not** shipped from this run - a run delivers on one branch, and a new pack earns its own review surface - so name the technology and the lesson that justified it on the tracker, where the pack-discovery pass picks it up. Never mint from imagination and never park an empty one to fill later - a pack minted here is populated from the *real, portable lesson* that justified it. Do not pad it with speculative rules no project earned - the "written from imagination, not distilled from worked examples" failure; an ungroundable rule isn't written. (Authoring a *fuller* pack from a technology's usage across the whole fleet - rather than a single lesson - is the per-repo pack-discovery task's job, not this floor's.) The follow-ups a fresh stub invites - enriching it from known best practices, triaging which skills it should require, spinning up dependent/aspect packs - are **not** automated here: note them for the owner and leave them to a later, deliberate pass.
- **Write it terse — when + what.** State the relevance (*when* it applies) and the rule (*what* to do); add a *why* only to flag severity, in a few words.
- **Carry the lesson's provenance.** Read the element's file in the member's local pack (`provenance/<slug>.md`, named by the marker that ends the rule), reduce it for the canon's boundary - `cn provenance reduce <file>` (`--public` where this canon is public): handles to roles, session ids and quotes dropped, member locators generalized - and land the reduced text as the canon element's file, ending the promoted rule with its marker (a slug that collides on the shelf takes a suffix, and the marker follows it), then append `promoted · from a member's local pack (<this PR>)`. The member's file is never edited. A lesson landed as a check takes the check's file the same way.
- **Bounded write surface — the canon's corpus roots only** (CI-enforced on the PR: the shelf, plus any second root this canon declares). Edit only the one owning doc — or, for a check-conversion, only the new rule module, its registration in the pack registry, and its fixture test. Don't "improve" unrelated rules while you're in there. The two bounded exceptions (both within the shelf): the new-doc path [item-routing.md](../../item-routing.md) owns, and minting a stub pack.
- **Raise the `version` of each pack whose shipping files the change touches, and write no `VERSIONS.md` row** — the release publishes a version once, so changed content under a published number fails the shelf's CI; the row is `pack-version-history`'s to derive once this change has landed.
- **Deliver on the branch and pull request your item names** — push to `Target-branch:`, and onto `Target-pr:` where one is named (the round then joins the review already pending); open the PR against `main` on that branch where none is named — one PR for the whole run's accepted lessons. Never a direct push, never a search for an open pull request, never a branch of your own. Keep every commit and PR terse. **Put the issue reference in the commit message** (`Refs #<n>` for the tracker below).

### 5. Log the run to the tracker

Append a dated comment for every run that promoted at least one lesson, and for any notable rejection.

## Tracking issue

The standing self-improvement log is the issue titled exactly **`Claudinite tracker: Promote to Canon`**.

- **Find it by that exact title, never a fuzzy match or a hard-coded number** (a bare number can dangle).
- **Create it already closed if missing** — creation always lands an issue open and ignores a `state: closed` argument, so create it and close it in a second call. **Never open, close, or reopen it** afterward — its state carries no meaning; only the log does.
- Log each run as a **dated comment** — not a sub-issue, so history accumulates in one scrollable feed — the date, and per lesson: the origin repo, the **ladder rung it landed on** (check id / skill / owning doc, with the named reason when prose was the fallback), and the generalized rule (or, for a notable rejection, the reason and the existing rule that already covers it).

## What this task must never do

- **Never mine outside a local pack** — product source, task scripts, code comments, and config are out of scope; only a local pack's `RULES.md` prose and its own check modules are candidates.
- **Never promote a product-specific or one-off item** — it stays in that project's local packs.
- **Never weaken, restate, or duplicate an existing rule** — fold a genuine sharpening in per [item-routing.md](../../item-routing.md) instead.
- **Never widen past the Context member list**, and **never touch a member repo** — this task only reads members and writes the canon PR.
- **Every step is a judgment call** (portability, duplication, ownership, how to generalize without distorting), and a plausible-but-wrong *acceptance* is what the owner's review is least likely to catch.
