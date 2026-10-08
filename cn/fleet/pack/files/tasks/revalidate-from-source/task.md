# revalidate-from-source worker

Reconcile the shelf's packs against the technologies they teach. A pack opts in by carrying an
`## Upstream` section in its `README.md`; this run reads what those sources have published since
each one's recorded anchor, and changes the pack's content where what it teaches has been dated.

**One PR against the default branch, for the whole run** — content changes and advanced anchors
together. Never merge it.

## 1. Take the opted-in set

Grep the corpus roots this repo curates for `^## Upstream$` in each `packs/<pack>/README.md`. That
set is the run's whole scope. A pack without the section is opted out: do not add one, do not
research it, do not mention it in the PR.

Each source is one line: what to watch, its URL, and `reconciled through <state>` — a date, a
version, a document revision. That trailing state is the window: you are asking what this source
has published **since** it.

## 2. Read each source

Fetch each source and collect what is new since its anchor. What counts as new is the source's own
unit: a published advisory, a release with breaking or security-relevant notes, a revised
specification or BCP, a deprecation announcement.

A source you **could not read** — the fetch was refused, the network policy blocks it, the URL is
gone — is *not checked*. Say so in the PR body, leave its anchor exactly as it stands, and
never infer what it would have said from search snippets or memory. A dead or moved URL is itself a
finding: propose the corrected one in the PR and leave the anchor behind it.

Never route around a refused fetch. If nothing in the run could be read, open no PR and say that in
the run's own outcome.

## 3. Judge each pack against what you read

The question is only ever: **does this date what the pack teaches?** Read the pack's `RULES.md`,
its skills and its checks against what published, and land in one of three places per source.

- **The pack's content is now wrong, incomplete, or advises something deprecated** → change it.
  Edit the rule, the skill or the check whose text is dated; add a rule only when the upstream
  change is a durable constraint on how work is done, never a restatement of the news. Record why
  on the element's provenance file (`provenance.mjs append`, kind `reworded`, `retired` or
  `born`). Never bump the pack's version or write a `VERSIONS.md` row -
  both are cut on the base branch once the correction lands, and a member receives it then.
- **Nothing published bears on the pack's content** → change nothing but the anchor.
- **Something published might bear on it, and deciding needs the owner** → change nothing, and put
  the question in the PR body with the source line that raised it.

Two things are out of scope however tempting: a member repository's own dependency versions (this
run reads the shelf, never a member), and a source that would be *nice* to watch but no pack
declared.

## 4. Advance the anchors

For every source you actually read, rewrite its `reconciled through <state>` to what you read up
to. That anchor is what the next run windows on, so it moves **only** for a source that was read
and judged in this run — never for one you skipped, and never past what you actually covered.

## 5. Open the PR

One PR, titled for the run, delivered the way [deliver-pr.md](../../../claudinite-tasks/public/deliver-pr.md)
says. Give the body one section per pack that had something to say:

- the source, and what published since its anchor;
- what changed in the pack, or `no change — anchor advanced`;
- any question left for the owner.

A pack whose sources all moved nothing needs no section — the anchor diff is its record. Close the
body with the sources that could not be read, if any.

`model: opus` — reading a specification revision against a pack's guidance, and deciding whether it
is a correction or noise, is the judgment this task exists for.
