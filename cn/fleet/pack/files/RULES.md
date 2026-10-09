# fleet — managing a fleet of repos and curating the canon it draws on

## Acting on what a sweep reports

- **Acting on an `add-packs` work-list issue** — the work is a declaration and a reviewed PR **in
  the named member**, never here. A session in this repo is scoped to this repo, so if every item
  names another repo there is nothing here to do: say the scope is the blocker rather than writing
  to the issues, which are the only thing within reach and are not the work. (acting-add-packs)

- **Acting on a scanned pack suggestion** — it is a recommendation, never a verdict.
  A fingerprint is a way to *suspect* a pack is wanted; whether to declare it is the member's call,
  and closing the issue `not planned` is a standing answer the scan honours rather than re-opening.
  A **forced** addition is the other thing entirely — a decision already made — so adopt what
  its issue says instead of re-judging whether it was wanted. (acting-scanned-pack)

- **Reading `unknown` in a report** — it means the sweep could not look, not that it looked and
  found nothing. Never convert one into a verdict: a repo whose declaration could not be read is
  neither covered nor uncovered, an undecided fingerprint is not a non-match, and a member the
  pack-seed sweep could not reach is not converged. Fix the access and re-run.
  (reading-unknown-report)

- **Judging whether a member is behind** — ask what its own update would move it to: its engine
  pin against the published engine versions, each held pack against the published pack versions
  (`cn fleet judge <owner/name>` answers it, and `.claudinite/fleet/roster.GENERATED.json` holds the
  last sweep's answer for every member), never a commit, a canon repository or the age of any stamp.
  A member's own update acts on exactly that, so any other measure reports a gap the member will
  never close, or misses one it will. (judging-whether-member)

- **Answering why the fleet did not move** — read the member's own artifacts first: its
  declaration, its stamp, the runs on its head sha. This repo dispatches; each member updates
  itself, with its own token and its own delivery policy. Propose a settings change as a conclusion,
  never as the diagnosis. (answering-fleet-did)

## Running the manual levers

- **Pushing canon to the whole fleet now** — create the work item, from a checkout of this repo:
  (pushing-canon-whole)

  ```
  .claudinite/bin/cn work create \
    fleet/fleet-update
  ```

  Add `--context "REPOS=owner/a owner/b"` to narrow it (space-separated: a Context line splits on
  commas), `--context "DRY_RUN=true"` to see the list without dispatching, or
  `--context "INCLUDE_DORMANT=true"` to reach members that stopped their own scheduler on purpose.
  Both knobs are read from the item's Context and nowhere else — an item created without them runs
  unscoped and live. It queues one run per member and then FOLLOWS each to the published engine
  and pack versions, reporting per member whether it updated, was already current, or never got
  there — never a count of accepted dispatches. A member with nothing to do reads
  `already-current`, which is a success, so over-using it is wasteful rather than unsafe.

- **Adding a pack across the fleet** — create a `fleet-add-missing-packs` item with
  `--context "ADD_PACKS=…"` rather than editing anything. No pack is named anywhere in the fleet
  code: every id comes from config or from the item's own Context, which is what keeps the
  enforcer from becoming a second place packs are known. (adding-pack-across)

## Credentials

- **Granting or repairing `FLEET_GITHUB_TOKEN`** — a fine-grained PAT spanning the owner's
  repositories, granted exactly what `cn fleet token` prints — the only
  place the permissions are written, because a per-sweep subset is always a defensible answer and
  never the right one. Grant it whole: the token is granted once, for the fleet.
  (granting-repairing-fleetgithubtoken)

- **A sweep reporting `403` or `no-permission`** — the grant is short a permission, which the
  error names. It is a grant to fix once, so widen the token rather than re-running: no sweep
  retries, because a work list nobody will act on is not a green outcome. (sweep-reporting-403)

# Curating a canon's shelf

### Naming and placing content on the shelf

- **Wanting a rule about what the `packs/` tree may reference** — configure the basics pack's
  `config.barriers`, extending the mechanism generically if a capability is missing. Never
  standalone segregation-checking code. (wanting-rule-packs)

- **Wanting a growth action — an extraction, a conversion, a revalidation — over the shelf** —
  declare a task here loading `claudinite-growth`'s skill for the method; never widen a growth
  task, which runs in every member, where `packs/` is a read-only mount. (wanting-growth-action)

- **Naming a new canon pack** — name it for the surface it serves rather than the first feature
  you are building for it. (naming-new-canon)

- **Naming a pack whose subject is a Claudinite feature itself** — the `claudinite-` prefix
  (`claudinite-lifecycle`). `claudinite-growth` is grandfathered.
  (convertible → prose-to-checks) (naming-pack-subject)

- **Looking for a skill and not finding it in `.claude/skills/`** — read
  `packs/<pack>/skills/<name>/SKILL.md` out of the tracked tree. Mounting filters on the literal
  declaration, so an unmounted skill says nothing about whether its procedure applies.
  (looking-skill-finding)

- **A canon pack's prose naming another pack by literal path** — check the name resolves inside
  every consumer's vendored tree. (canon-packs-prose)


### Pack config and shared logic

- **Choosing a value right for nearly every project** — keep it in the pack's own code: ask
  nothing at adoption, write nothing into member config. Read config as optional; unset means the
  default, never "misconfigured". (choosing-value-right)

- **Wanting a `.claudinite-settings.json` entry's config validated** — a real JSON Schema the file
  points at with `$schema`. Never a coded per-pack validation vocabulary or a `configSchema` type
  system on the manifest. (wanting-claudinite-settings)

- **Wanting to share logic between two sibling packs** — never `engine/`, which breaks the
  package-manager model, and never a pack-to-pack dependency, which breaks independence. Prefer
  self-describing data; else duplicate, possibly with a drift guard. (wanting-share-logic)

### Pack modules and the engine they load against

- **Adding a module under `packs/`** — keep it import-light, and start work after evaluation
  completes (`check(…).catch(…)`), never in a top-level `await`. Discovery imports every
  `pack.mjs` before activation is consulted, so a CLI entry point re-imports mid-evaluation and
  Node exits 13. (adding-module-packs)


- **A pack that fails to load** — it fails the mount's self-test, the update refuses to land at
  all, and the member cannot receive the pack version that would have fixed it. (pack-fails-load)

### Writing and keeping checks

- **Adding or changing a check** — update the pack's catalog row, and re-run the suite against
  current `main` before merging: a whole-tree aggregate is judged post-merge, so a branch's own
  green never covers it. (adding-changing-check)

- **Writing a check's `fix` text** - name only remedies matching the enforced `on_fail`; sessions
  follow the words, not the field. An advisory's remedies are act on it or leave it,
  never a config-acceptance escape. (writing-checks-fix)


- **Writing a check that reads the session transcript** — screen the harness's plain-text
  pseudo-turns, not only tag-wrapped ones. `humanText` in
  `engine/checks/helpers/session-transcript.mjs` drops an entry starting with `<`, so a marker
  like `[Request interrupted by user for tool use]` reads as the owner's latest comment.
  (writing-check-reads)

- **Fixturing a check that fires at the Stop hook** — carry an interruption marker beside a real
  owner turn. A false positive there spends a whole cycle on something no edit can clear.
  (fixturing-check-fires)

- **A doc reached only by following a link out of `RULES.md` or a check's `doc:` line** — if it is
  a how-to wanted at authoring time, convert it into a skill invocable by description.
  (doc-reached-only)

- **Moving or renaming a file a check's `doc:` field points at** — grep for and re-verify every
  `doc:` by hand. Nothing opens the field until the check fires, so a stale pointer sits broken
  indefinitely. (moving-renaming-file)

- **A check built to catch a thing being missing or misnamed** — don't gate its relevance on the
  single signal it exists to validate, or the failure it catches also silences it. Use two
  independent signals, either sufficient. (check-built-catch)

- **Finding a check that watches only one of two structurally-identical surfaces** — widen it to
  the sibling in the same change rather than filing it separately. (finding-check-watches)

- **Writing a check that a command is still wired into a script or CI step** — match the
  invocation line, never a step's display label. A plain token grep passes on the label alone
  (`step "Normalizer self-test — some-cmd --flag"`) after the real command line is deleted, so
  strip a labeling helper's quoted argument before searching for a surviving invocation.
  (writing-check-command)

- **Writing a check that selects inputs by path pattern** — assert over the real tree that its
  scope is non-empty. A pattern left behind by a layout change matches nothing, reads as live, and
  fixtures spelling the same dead layout keep proving the matching. (writing-check-selects)

- **Naming a directory in a finding, a remedy or a doc pointer** — grep the tree for it before
  shipping. (naming-directory-finding)

- **Deciding whether an enforced check still earns its keep** — measure its blocking-firing rate
  against what it buys. A check whose firings are dominated by cases where the agent already did
  the right thing is a demotion candidate (check → prose-only). (deciding-whether-enforced)

### Writing pack prose and skills

- **Writing anything into a pack's `RULES.md` that describes rather than instructs** — how a
  mechanism works, or what the pack's own tasks do — not there, where every session in every
  declaring repo pays for it whether or not it is that session's work. Description belongs in the
  module header and the pack `README.md`; rationale and history on the element's provenance
  file; a worker's policy belongs in the `task.md` it loads. (writing-anything-packs)

- **Writing a pack's `README.md`** - how a repo uses the pack and its elements: when it
  activates, what each check demands, when a skill is reached for, what the task does. Never how
  an element came to be, what it replaced or how it is maintained: a date, a pull request
  number, an "until" or a "kept as it was" is an entry on the element's provenance file, and the
  maintainer's method is the growth skills'. (writing-packs-readme)

- **Changing a carrier on the shelf** - a rule, a skill's trigger, a check's gate or on_fail, a
  task's policy - lands with the entry on its provenance file in the same change; the forced
  `changing-pack-elements` skill names the kind. (changing-carrier-shelf)

- **A documented multi-step procedure the agent re-derives every run** — mechanize it into a
  script the agent runs once. That pattern, not the doc's polish, is the signal.
  (documented-multi-step)
