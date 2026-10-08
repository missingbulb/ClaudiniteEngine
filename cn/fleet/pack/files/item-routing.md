# Evaluating and routing a new item

The shared method for three decisions every proposed addition to this corpus must pass through: **is it worthy** of inclusion at all, **which mechanism** should carry it, and — only for prose — **where does it go**. Any routine or person that adds an item — the [growth promote stage](tasks/growth-promote/task.md), an owner-requested retrospective pass, a hand edit — applies this same protocol, so the decision-making lives here once instead of being re-derived per caller.

The "corpus" throughout is the canon this pack is declared on — its `packs/` shelf and any second corpus root it declares. This doc is the method, not injected prose: it is read at decision time by whoever is adding an item (see [the pack README](README.md)), never `@import`ed into a session.

An "item" is one **distilled, portable rule** — not a transcript, not a narration of what happened. A candidate still phrased as "here's what we did" has not been distilled yet and is below the bar on form alone, before any of the tests below. This doc picks up once a distilled rule exists and decides its fate.

## Worthiness — does it earn a place at all

The corpus is read read-only by every consuming repo, so a **duplicative** item (already covered) or an **undistilled** one (a story or a truism, not a rule) pollutes shared canon and costs every future reader. The bar guards against *those* — it is **not** a bar on *scope*: an item useful to only one project today can still earn a place (see Portable). Admit an item only when **all** hold:

- **Distilled.** It is a single tight rule in the imperative, not a story, a log, or a restatement of a generic truism.
- **Portable — would a *future* project benefit?** Not "useful to every project today." An item can help only **one** project right now and still belong here, so long as another project — **especially one with a similar tech stack or process** — would benefit when it arrives. Being specific to a technology or a process is **not** disqualifying: generalize it and route it to that technology's or practice's home. What disqualifies is leaning on *this* project's non-transferable particulars — a product requirement, or a one-off incident in how it happened to call some API. The test, a genuinely hard one: *if another project with a similar tech stack came along, would it benefit from this?* Yes → portable; only-ever-this-product → local. A rule mined from the canon's **own** test suite is the hardest case: it reads as generic craft even when it actually depends on the canon's own shape, which no member shares. Before promoting one, grep the fleet's own callers of the pattern the rule is about; zero callers outside the canon's own conformance tests means the lesson is local however general its wording. (Worked example: a fixture-signing rule read as portable, was promoted to a testing pack, then reverted once every caller of the fixture helper turned out to be a conformance test of the canon itself.)
- **Durable and reusable.** It will still apply on a future, unseen task — not a one-off, not a situational detail, not something already implied by an existing rule.
- **Readable through the channel it lands in.** A rule whose premise is that the delivery channel
  itself failed cannot be carried by that channel. A promote run once admitted "the tell that the
  session-start hook did not fire" as pack prose — and pack prose is what that hook delivers, so in the
  one situation the rule describes it is not in context to be read. Detecting a channel's own failure
  belongs in code, not in the payload the channel carries; reject such an item here rather than
  rewording it.
- **Not already covered.** The insight does not already exist **anywhere** in the corpus, even worded differently or owned by a different file. This is checked against the *entire* corpus, not just the file it would land in — see [Dedupe against the whole corpus](#dedupe-against-the-whole-corpus).

Reject on any miss — but the misses that matter are *undistilled*, *already-covered*, or *genuinely one-off*, not merely *narrow*. When you're torn over a real, non-duplicate rule, lean toward **keeping** it — the goal is a broad library. When you're torn over whether something is a duplicate, or a rule at all, leave it out. The failure to avoid is padding the canon with restatements of what's already there; admitting a narrow-but-reusable rule is not that failure.

### Dedupe against the whole corpus

Before admitting anything, read across the corpus, not only the file you expect to own the item. The same insight is frequently already present under a different heading or in an adjacent group. If it sharpens an **existing** rule, fold it in with a minimal edit rather than adding a second bullet that says nearly the same thing — and never weaken or restate what is already there in the process.

**Sweep last, and case-insensitively.** A dedupe pass is only evidence about the text it actually read, so it runs **after** every re-home, merge and edit the change makes — a sweep run early and never re-run reports clean on content that has since moved under it. Measured on a seven-PR promote consolidation: the sweep ran *before* two lessons were re-homed out of a folded-away skill file into a pack's `RULES.md`, and **three** copies of the same rule reached the owner, who found them by reading. The re-run then surfaced a further pair the pattern had missed because it was case-sensitive and the survivors were capitalised. So: sweep as the last step, match case-insensitively, and state the similarity threshold you swept at so the "no remaining near-duplicates" claim is checkable.

## Mechanism triage — prose is the fallback, not the default

A worthy item next descends the **promotion ladder** ([the engine's checks design](../../engine/checks/DESIGN.md) owns it): platform setting (always paired with its verifying check) → PreToolUse hook → conformance check → skill → prose. The *first* rung that can carry the item wins — a rule about repo state becomes a check whose failure message *is* the lesson; a bad action to prevent becomes a guard; a procedure or knowledge with a nameable trigger becomes (or joins) a skill. Only an item none of those can carry — judgment, in-flight behavior, signature-less knowledge — proceeds to the file routing below, and its landing **names that reason** in the promote log so conversion rates stay auditable.

**"No universal signature" alone never routes an item to prose.** A rule converts to a check when a *narrow, file-scoped* signature covers the **grounded cases** — the concrete shapes the rule was distilled from — even though no signature covers every conceivable violation. Gate it relevance-first so it stays inert off-target, and document the genuinely signature-less residue in the rule's own comments as out of scope. The prose rung is only for an item whose grounded cases *themselves* have no reliable signature (pure judgment, runtime/in-flight behavior).

## Routing — picking the right owner (rung-5 items only)

Routing is robust when it keys on **stable groups**, not on the current roster of individual packs (which grows over time). Every rung-5 item belongs to exactly one of three:

1. **The pack shelf** — a durable, project-agnostic rule lands in exactly one pack's `RULES.md`. Pick the owner by reading the shelf's catalog and each candidate's `ruleRoutingGuidance` (`belongs` / `excludes`) — **never from memory**, since the set of packs grows. The axis a pack is partitioned on is its *subject*: a practice (engineering, git, testing, investigation, discipline), an aspect, a domain, or a **technology** — one pack per technology, and a technology mention in the rule is what routes it there rather than what disqualifies it.
2. **The per-user group** — interaction preferences and trigger phrases. This group does **not live in the canon at all**: an item of this kind routes out to the person it belongs to, never onto the shelf.
3. **An unhomed technology** — when **no pack homes that technology** (not even a stub), the item doesn't land in whatever pack is nearest; it **mints a fingerprinted stub pack** (a `packs/<tech>/`, delivered in its own PR), so the next project on that stack self-declares it. [promote](tasks/growth-promote/task.md)'s stub-minting floor owns the mechanics.

Pick **exactly one** owner. If an item plausibly fits two packs, choose the single best one — never split one item across packs, and never duplicate it into both.

### When nothing fits

An item that matches **no** existing pack is **not** a reject signal by itself — a genuinely new category of lesson is meant to be learnable, so a worthy item that no pack owns earns its own home. Resolve it in this order:

- **Route into an existing owner** when one genuinely fits — the first choice. If the item belongs to a pack's subject and merely sits slightly off-center, a small stretch toward that owner beats spinning up a new pack. When the nearest owner is merely too *narrowly stated*, widen its `ruleRoutingGuidance.belongs` rather than opening a pack beside it.
- **Open a new pack** when the item clears the [Worthiness](#worthiness--does-it-earn-a-place-at-all) bar in full — distilled, portable, durable and reusable, and not already covered anywhere in the corpus — **and** no existing pack's subject genuinely fits it. That is the whole bar: nothing more is required. Do **not** also demand that you expect the subject to recur, that forcing the item into the closest pack would *distort* it, or default to rejecting when the new-pack call is a close one — those extra hurdles are dropped. Creating a pack obliges registering it where the shelf is indexed, in the **same** change, so it never lands orphaned (see [Keep the write surface bounded](#keep-the-write-surface-bounded)).

The loosening above is **only** to the new-subject hurdle, never to the per-item bar. An item that misses [Worthiness](#worthiness--does-it-earn-a-place-at-all) — a one-off, non-portable or project-specific, a restatement of a generic truism, or already covered anywhere in the corpus — is rejected whether or not a pack already exists for it; for the per-item admission decision, the default-to-reject-when-unsure tiebreak still stands.

## Keep the write surface bounded

Once worthiness and routing are settled, the edit touches **only** the single owning file: add or sharpen one terse rule in that file's existing voice and format, and change nothing else. Do not "improve" unrelated rules while you are in there. The sole exception is the new-pack path above, whose surface is itself bounded to the new pack plus its index registration.
