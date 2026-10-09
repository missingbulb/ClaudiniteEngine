---
name: extract-packs-from-a-project
description: Decompose a project into its pack facets and author the canon packs they earn. Use from a canon when a project shows a facet its shelf does not yet home.
disable-model-invocation: true
metadata:
  body: workflow
  usage:
    expect: judgment
---

# Extract packs from a project — the canon side

Grow the canon's pack library using a project as the evidence, and decide here — never in the project —
whether an insight becomes a pack every repo can declare. Projects recur **along axes**, not as wholes:
the same working style with a different product, the same technology under a different working style. So
split what you find by axis — a facet later projects will share becomes a **canon** pack, and what stays
specific to *this* project lands in **its own local pack**.

The deliverable is therefore **two kinds of pack**, never a single "how to work on this repo" document:

- **Canon pack seeds and refinements** — one `packs/<facet>/` per facet where this project has real
  portable insight, each written as if this project didn't exist, proposed to Claudinite.
- **The project's own local pack(s)** — under `.claudinite/local/packs/<pack>/` in the project's tree:
  the project-specific working style, concrete values (real commands, paths, names, metrics), and the
  project-specific rules that *don't* generalize — carried the same way any pack carries them (a check
  in the pack's `rules`, an activity skill, prose in `RULES.md`, a scheduled task), not a thin doc.
  This is the project's normalized capture surface; its `CLAUDE.md` is a thin routing map to it, loaded
  through the pack system rather than `@import`.

The failure mode this skill exists to prevent: investigating the repo and producing one competent "how
to work on this repo" document — real commands, real paths, this product's concepts — with the
reusable core never separated out and the project-specific core dumped as always-loaded prose. If
portable guidance ends up in a local pack (it should be a canon pack), or the project's file names end
up in a canon pack (they belong in the local pack), the split is wrong.

## 1. Gather the evidence

Read the project before writing a word: README and existing docs, `CLAUDE.md` and everything it
imports, dependency and build manifests, scripts and CI, the test layout, git history and recent
PRs/issues, and any conversation with the owner about how they want to work. You are collecting two
things, both grounded in what you actually find:

- the **facets** this project exhibits (step 2), and
- every **candidate rule** — each convention, procedure, or quality bar the project works by — ready to
  be sorted by owner (step 3).

Invent nothing. If a call that changes the outcome is genuinely open, ask the owner one focused
question (via the structured popup); otherwise take the sensible default and note it.

## 2. Decompose into facets, and check the shelf per facet

A project is a bundle of **facets**, each a candidate pack on its own axis:

- **Class — the working style.** How work runs end to end: the loop by which a change is proposed,
  verified, and shipped; the production bar; audience and longevity. Test: the pack still fits a
  project on a different stack, for a different product idea.
- **Technology — the platform.** True for any project on that platform, whatever its class or product.
  Test: the pack still fits when the product and the working style change. The first project on a new
  platform mints its pack — an iPhone app arriving tomorrow mints `iphone-app` the same way
  `chrome-extension` and `aws-sam` were minted.
- **Aspect — a separable slice of concern**, usually with its own on-switch. `chrome-extension-release`
  is the same technology as `chrome-extension` at a different moment: the release/store standard a
  project declares only once it ships. Split an aspect out of its parent facet when its rules activate
  at a different time or under a different decision than the parent's.
- **Domain — the audience or setting**, when it changes how work is done. A children's-app pack would
  carry design-review, requirement-processing, and compliance rules that hold whether the app is
  Flutter or web; a regulated-industry pack likewise. A domain that doesn't change the working rules
  isn't a facet worth a pack.

**The product itself is never a facet** — "this specific product" doesn't recur, by definition; its
rules land in the project's own local pack.

And a *statement of what the product does* isn't a pack rule at all — not even a local one. A pack, canon
or local, homes **how we work**: the conventions, gotchas and review discipline that recur across tasks
whatever the feature happens to be. The working-discipline rules bar the requirement-shaped rule outright;
here, catch it wearing a convention's clothes — a rule asserting which entities exist, what a surface must
render, or that a feature's parts are wired to each other. Pack-worthiness
is a question about the *work*, not about how load-bearing the code is — a real gap in product coverage is a
requirements gap, and answering it with a pack is how a feature's spec ends up somewhere nothing executes it.

<example>
One extension project can exhibit four facets at once: `spec-driven-product` (class),
`chrome-extension` + `node` (technologies), `chrome-extension-release` (aspect, once it ships) — and
its local pack holds only what none of them own.
</example>

Then check the canon shelf **per facet** (`packs/` here; the mounted `.claudinite/packs/` in a
consuming repo — a facet already owned by a *canon* pack is declared, not re-authored locally):

- **Covered** → declare it (technology facets are usually already declared from the fingerprint) and
  move on — unless this project's evidence **sharpens or contradicts** the pack, which becomes a
  refinement PR to that pack, not a fork. Each further project of a facet is another exemplar: the
  pack should read as the intersection-plus-union of all of them, and a rule only one exemplar
  supports should say so or wait.
- **Uncovered, with real portable insight in the evidence** → this skill mints it (steps 3–4).
- **Uncovered, but this project has nothing durable to say about it yet** → leave it; a pack of
  placeholders helps nobody.

Write each facet down — a short name plus one defining sentence — before drafting anything; every
sorting decision in step 3 tests against them.

## 3. Sort every candidate rule to the single facet that owns it

For each rule from step 1, apply the **portability strip per facet**: delete from the rule everything
the facet doesn't keep, and see whether something meaningful survives.

- Delete the product and the stack, keep the loop — survives? → the **class** pack (canon).
- Delete the product and the working style, keep the platform — survives? → the **technology** (or
  **aspect**) pack (canon).
- Delete the stack, keep the audience/setting — survives? → the **domain** pack (canon).
- Survives nothing → project-specific: the project's **own local pack**.

Route each rule to **exactly one owner** — the facet whose whole population the rule serves. "True for
any Node service" belongs to the canon `node` pack, not to this project's class pack, even if every
project of the class happens to use Node. Rewrite what lands in a canon pack project-agnostically and
parameterized, the way research-project does ("read *input* as whatever it means for your project").

Then gate **every** pack-bound rule — canon or local — on the promotion ladder
(engine/checks/DESIGN.md): a rule a deterministic **check** could enforce, or a
procedure with a nameable trigger a **skill** could carry, becomes that (in a canon seed it's flagged
as a check/skill candidate; in a local pack you author the check outright in the pack's `rules`, with
a red-first fixture) rather than settling as prose. What remains in a `RULES.md` is the always-relevant
judgment core of its facet — write more checks and less prose. Dedupe against the corpus: a rule the
basics pack, an existing canon pack, or a skill already owns is not pack material at all.

## 4. Write each pack

Mint a pack for a facet only when its surviving rules clear the bar — several durable rules, not one
stray (a single rule joins the nearest existing pack, or waits as a handoff note). Each
`packs/<facet>/` needs four things (mirror `research-project/` and `chrome-extension/`):

- **`RULES.md`** — the playbook, addressed to the *next* project sharing the facet, on day one. Open
  with the facet's one-line definition and "a default to adapt, not a contract". Principle-first, each
  rule carrying its why. It loads at session start for every declaring project, so every line pays
  rent — cover the facet, not everything you noticed.
- **`pack.json`**: the manifest. Class/domain/aspect packs: `always: false` and no `relevanceDetector`
  (declaration is authoritative). Technology packs: add the `relevanceDetector`
  fingerprint when the repo carries a reliable one, so `--init` seeds the pack into a fresh
  declaration; the marker only *suspects* a pack is wanted, it never forces its declaration.
  Discovery is structural — the directory is the registration.
- **`README.md`** - the pack's rule table (section ≤5 words | how enforced), and how a repo uses the
  pack; never where it was distilled from - that is the pack's provenance.
- **`provenance/`** - `_pack.md` with a `born` entry whose `Source` is the evidence set (the
  project, the documents, the dates), and one file per rule with its `born` entry, each ending its
  rule with the marker that names it: `cn provenance mark` creates the files and proposes the
  slugs, `append` writes the entries (the growth pack's changing-pack-elements skill has the
  grammar).
- **Index entries** — a row in packs/README.md and, for a new pack kind, the
  matching line in the corpus map (README.md — there is no agent-facing corpus index).

The acid test before proposing any pack: **a reader must not be able to tell which project it was
extracted from.** Any surviving repo path, command line, or product noun marks a rule that belonged in
the overlay.

## 5. What stays in the project

The project-specific residue is not the canon's to take, and not the canon's to write: it lands in a
**local pack** under `.claudinite/local/packs/<pack>/` in the project's own tree, seeded by that
repo's adoption and filled by its own capture runs. What follows is the shape of that residue, because
the sort in step 3 cannot say "this stays local" without knowing what local looks like. Most projects
need **one** general pack (name it for the project); segregate a **second** only along the repo's own
**project structure** — a segment of the repo's tree whose work is its own territory (a `client` pack
for work under `client/`) — never around a technology or a methodology: those axes belong to the canon
(their portable half is a canon seed above; the project-specific residue lands in the structural pack
that owns the work). Each local pack is a real pack:

- **`pack.json`**: `{ "ruleRoutingGuidance": … }`. The id
  is the directory name, the prose the `RULES.md` beside it, the skills its `skills/`
  subdirectories and the checks its `worldRules/` and `workRules/` modules, all by convention - none of them is declared.
  A local pack is declared by hand, never fingerprinted or seeded (`detect`/`marker` stay null), as its
  namespaced token `local/<name>` in `.claudinite-settings.json`; its id
  must be unique and may not shadow a canon pack.
- **`RULES.md`** — the always-loaded judgment core and the project's concrete values (real
  setup/run/verify commands, real paths, inputs, metrics, invariants). Keep it terse; anything a check
  or skill can carry doesn't belong here, and anything inferable from the code is omitted.
- **Checks** - the project-specific deterministic rules as `.mjs` modules under
  `worldRules/` or `workRules/`, each with a red-first fixture (`test/pack.test.mjs`) runnable by the project's own test suite.
  Local check modules stay dependency-free (they must load without the gitignored mount): return plain
  finding objects rather than importing the engine's helpers.
- **Skills** (`skills/<name>/SKILL.md`) — the project's activity-scoped procedures, bundled in the pack;
  a local pack may also *require* a canon skill by name.
- **Scheduled tasks** (`tasks/<name>/task.json` + its `task.md`) — first-class like every other kind of
  pack content: the repo's own scheduler discovers a local pack's tasks in the same scan that finds a canon pack's,
  gated by the project's declaration exactly the same way. So project-specific scheduled work belongs
  in a local task declaration (a self-contained module honoring the task contract, with its
  co-located worker doc), not an out-of-repo routine and not a cron of its own.

The project's `CLAUDE.md` becomes a thin **routing map** to its local packs (and its genuine design
docs) — it does **not** `@import` the pack prose; the pack system injects the active local packs'
`RULES.md` at session start, the same as the canon packs'.

## 6. Deliver through the owner's approval gates

- **Every canon pack seed or refinement → a PR against the canon.** Minting or changing a pack every
  repo can declare is the owner's call, and the PR is that gate — no corpus change lands unattended.
- **Write nothing in the project.** Its residue is its own to keep and its own runs to write; this
  method reads a project and delivers to the canon. Where the project's own session is the one
  holding the residue - its `CLAUDE.md` already written out, waiting to become its local pack —
  that is the growth pack's `extract-from-instructions`, run from the member, not this skill.
- **A project declares a new canon pack** only once it has merged and that repo's mount re-synced —
  declaring a canon id the mounted registry doesn't know is an unknown-pack settings error. Tell the
  project, don't do it for it.
- **Existing projects sharing a facet keep their local packs for now.** Once the canon pack lands,
  each one's dedup stage prunes what the canon newly owns; don't pre-trim against an unmerged pack.

Close with a short report: each facet and its one-line definition, with its shelf verdict (declared /
refined / minted-to-canon / kept-local / left alone); the sort tally (rules sent to each canon pack /
the local pack / as a check vs. a skill vs. prose); and links to the PRs.
