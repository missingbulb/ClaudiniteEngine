// The work-item queue's vocabulary and grammar, as this page reads them: the labels an
// item's state is written in, every legacy spelling folded to today's, the title and
// body fields, the markers and leases. The queue itself is the engine's (`cn`), whose
// wire this is; the page reads GitHub's issues in a browser and so carries its own copy
// of the decode.
//
// Pure, and reaching no Node built-in: the page loads it unbundled.

// The title prefix. Disjoint from the slot mechanism's `[claudinite-task]` on
// purpose: the two mechanisms coexist per-repo behind `taskScheduler.dispatch`,
// and neither may read the other's issues (PRINCIPLES.md, S29).
export const WORK_PREFIX = '[claudinite-work]';

// --- the canonical vocabulary (PRINCIPLES.md, the migration of #1119) -------------
// Every label the machinery writes is one of three things — the item's single
// STATUS, its lifelong ORIGIN, or the URGENCY flag — and all of them live in the
// `task:` namespace. These are the spellings a reader compares against: decode
// first (`statusOf`, `originOf`), then compare, so an item filed by any engine
// version answers the same question the same way.
export const STATUS_PREFIX = 'task:status:';

export const PARK_PREFIX = `${STATUS_PREFIX}needs-human-`;

export const STATUS_BLOCKED = `${STATUS_PREFIX}blocked`;

export const STATUS_READY = `${STATUS_PREFIX}waiting-for-executor`;

export const STATUS_RUNNING_EXECUTOR = `${STATUS_PREFIX}running-executor`;

export const STATUS_RUNNING_AGENT = `${STATUS_PREFIX}running-agent`;

export const STATUS_NEEDS_HUMAN_ACTION = `${PARK_PREFIX}action`;

export const STATUS_NEEDS_HUMAN_DECISION = `${PARK_PREFIX}decision`;

export const STATUS_NEEDS_HUMAN_APPROVAL = `${PARK_PREFIX}approval`;

export const STATUS_NEEDS_HUMAN_FAILURE = `${PARK_PREFIX}failure`;

export const STATUS_DONE = `${STATUS_PREFIX}done`;

export const STATUS_REJECTED = `${STATUS_PREFIX}rejected`;

// The four statuses an OPEN item may wear before it parks or converges. An open
// item wearing no decodable status at all is off the state machine — a torn label
// swap's leavings, which the janitor repairs (docs/PRINCIPLES.md).
export const LIVE_STATUSES = Object.freeze([
  STATUS_BLOCKED, STATUS_READY, STATUS_RUNNING_EXECUTOR, STATUS_RUNNING_AGENT,
]);

// THE PARK KINDS. A park is ONE label - `task:status:needs-human-<kind>`
// — and the kind is what the human is being asked for, which is the whole
// difference between a queue a person can skim and one they have to read.
//
// The four are disjoint by REMEDY, not by cause:
//   action   — something outside the code must change: a secret set, a scope
//              granted, a routine's prompt or endpoint fixed, an item re-created
//              with the parameter it was missing. Mechanical; no judgement.
//   decision — the run stopped mid-flight and what happens next is a choice:
//              re-queue or abandon, does the half-done work stand, was the
//              ceiling violation acceptable.
//   approval — the run SUCCEEDED and deliberately left an unmerged PR. The only
//              park that is not a fault; the human merges it or closes it.
//   failure  — the run broke: a bug, a contract-forbidden shape, a malformed or
//              forged item. Someone diagnoses and fixes code.
// `failure` is the default a park falls back to, so an unclassified park reads as
// "diagnose me" rather than quietly joining the mechanical lane.
// Ordered as a decoder prefers them when an item somehow wears more than one:
// `failure` first, the conservative lane. The two-label era's sub-labels
// (`task:needs-human-<kind>`) are read by the grammar's `parkOf` and written by nobody.
export const PARK_KINDS = Object.freeze(['failure', 'action', 'decision', 'approval']);

export const PARK_STATUSES = Object.freeze(PARK_KINDS.map((k) => `${PARK_PREFIX}${k}`));

export const STATUS_LABELS = Object.freeze([
  ...LIVE_STATUSES, ...PARK_STATUSES, STATUS_DONE, STATUS_REJECTED,
]);

// THE ORIGIN (docs/PRINCIPLES.md) — who asked for this item, worn for the
// item's whole life beside whatever status it holds.
export const ORIGIN_PREFIX = 'task:origin:';

export const ORIGIN_PLANNED = `${ORIGIN_PREFIX}planned`;

// The two shapes a person's ask takes. `manual` is an occurrence of a DECLARED
// task somebody pulled the lever on — a wake, a hand-created item — so the queue
// already knows what work it is. `ad-hoc` is somebody's own issue, adopted as
// itself, which every time runs the same task (`implement-request`) over a
// different subject.
export const ORIGIN_MANUAL = `${ORIGIN_PREFIX}manual`;

export const ORIGIN_AD_HOC = `${ORIGIN_PREFIX}ad-hoc`;

export const ORIGIN_GITHUB = `${ORIGIN_PREFIX}github`;

export const ORIGIN_LABELS = Object.freeze([ORIGIN_PLANNED, ORIGIN_MANUAL, ORIGIN_AD_HOC, ORIGIN_GITHUB]);

// Every origin that is somebody asking. The scheduler files only ORIGIN_PLANNED,
// so this is its complement among the origins a person's action produces.
export const ASKED_FOR_ORIGINS = Object.freeze([ORIGIN_MANUAL, ORIGIN_AD_HOC]);

export const URGENT = 'task:urgent';

// --- the legacy spellings, written never and read forever ----------------------
// Every spelling any fielded engine has written. They are literals rather than
// aliases of the constants above precisely because those constants moved: a decode
// map built from them would have mapped today's spelling to itself and forgotten
// the vocabulary it exists to read (PRINCIPLES.md).
// @legacy-tolerance advisory:none retire:#1642
export const LEGACY_BLOCKED = 'task:blocked';

// @legacy-tolerance advisory:none retire:#1642
export const LEGACY_READY = 'task:ready';

// @legacy-tolerance advisory:none retire:#1642
export const LEGACY_EXECUTING = 'task:executing';

// @legacy-tolerance advisory:none retire:#1642
export const LEGACY_AGENT = 'task:agent';

// @deprecated The bare park of the two-label era. A park is ONE label now
// (`task:status:needs-human-<kind>`); this is still read — on its own it decodes to
// `failure`, the conservative lane — and still ensured, because an open item filed
// by a fielded engine wears it. Its one live writer is the session-side dispatch
// flow, which labels an anomaly with it for triage (`src/session/dispatch.mjs`).
export const NEEDS_HUMAN = 'needs-human';

// @deprecated Nothing writes this since the approval park: a run that left an
// unmerged PR no longer CLOSES as delivered, it parks at the approval lane and
// waits to be merged. Kept exported, kept in `QUEUE_LABELS`, and still read
// everywhere it was read — closed issues carrying it are stored data, and a decoder
// that stopped recognising it would turn every historical delivered run into an
// un-outcomed one.
export const OUTCOME_DELIVERED = 'outcome:delivered';

// @deprecated The pre-2026-08-19 terminal spellings. Kept exported so a fielded
// pack that imports them still loads, and READ wherever an outcome is decoded:
// labels are stored data on closed issues fleet-wide, so a decoder that stopped
// recognising these would turn every historical run into an un-outcomed one.
export const OUTCOME_DONE = 'outcome:done';

export const OUTCOME_OBSOLETE = 'outcome:obsolete';

// @deprecated The pre-#1119 terminal spellings in the `task:` namespace, the
// generation between `outcome:*` and today's statuses. Read forever, same reason.
// @legacy-tolerance advisory:none retire:#1642
export const LEGACY_TASK_DONE = 'task:done';

// @legacy-tolerance advisory:none retire:#1642
export const LEGACY_TASK_OBSOLETE = 'task:obsolete';

// The families a request may ask for in its `Model:` field. `none` is not among
// them: a request is implemented by a session, so an agentless family would name a
// run that cannot happen.
export const REQUEST_MODELS = Object.freeze(['opus', 'sonnet', 'haiku']);

// --- comment markers ----------------------------------------------------------
// The three comments the protocol reads back. They are HTML comments so a human
// reading the item sees prose, and they are here — with the labels and the body
// fields — because together they ARE the item's vocabulary, the one compatibility
// surface across engine versions (PRINCIPLES.md).
//
// The CLAIM comment carries who and when (executor identity is an unbounded set
// and must never become a label). The HANDOFF comment names the session and the
// invocation nonce. The EPISODE comment is the boundary the claim arbiter is
// scoped to: every claim before it is dead, and arbitrating over dead claims makes
// one outrank every future live claimant — the item then livelocks through reclaim
// cycles forever (F18). A reclaim, a revert and a hand re-queue each write one.
export const CLAIM_MARKER = '<!-- claudinite-claim -->';

export const HANDOFF_MARKER = '<!-- claudinite-handoff -->';

export const EPISODE_MARKER = '<!-- claudinite-episode -->';

// --- the machine block (docs/PRINCIPLES.md) ----------------------------------
// A one-issue request's item IS the issue somebody marked, so the item's fields
// share a body a person authored and keeps editing. They live in one delimited
// block, appended at adoption and rewritten in place after that: everything outside
// it belongs to the human, everything inside it to the machine, and a parser that
// read the whole body would take a sentence of prose for a field.
//
// A `[claudinite-work]` item has no block — its whole body is the machine's — so
// every reader here falls back to the whole text, which is what keeps items filed
// before the one-issue model draining unchanged.
export const MACHINE_BLOCK_START = '<!-- claudinite-item -->';

export const MACHINE_BLOCK_END = '<!-- /claudinite-item -->';

// The heading of the section a no-go roll keeps on the item: the last declined reason
// and the next wake.
export const LAST_VERDICT_HEADING = 'Last verdict';

// A dead executor claim is reclaimed after this much silence — the holder's own
// silence, measured from its last claim or heartbeat (#924), never the issue's
// `updated_at`. Long work is legal; a holder that stops beating is not alive.
export const EXECUTING_LEASH_MS = 60 * 60e3;

// An agent session silent this long is declared dead. A legitimately longer run
// must comment on its item to reset the activity clock — stated as an assumption
// rather than discovered as an incident.
export const AGENT_LEASH_MS = 3 * 3600e3;

// An item nothing picked up for this many of its own periods leaves the queue for
// triage; a blocked item whose blockers never resolve is surfaced (comment only)
// after this long.
export const STALE_READY_PERIODS = 2;

export const STUCK_BLOCKED_MS = 2 * 86400e3;

// --- the commit trailers ---------------------------------------------------------
// `Claudinite-Task: <pack>/<task>` says a scheduled task wrote this commit; the
// movement terms read it, so the writer classifies its own output and a task added
// tomorrow is classified correctly on its first run. `Claudinite-Automerge-Policy:
// <policy>` is the arming trailer the landing lane reads.
// A trailer is what survives a squash merge into the default branch, which is the
// commit the `commits` collector actually reads.
export const TASK_TRAILER = 'Claudinite-Task';

export const AUTOMERGE_TRAILER = 'Claudinite-Automerge-Policy';

// --- the declaration defaults (owner, 2026-09-03) --------------------------------------
// The one place a declaration's absent field becomes a value, so nothing downstream
// ever reads an undefined one: land nothing unreviewed, and no agent.
export const DEFAULT_AUTOMERGE = 'nothing';

export const DEFAULT_AGENT_MODEL = 'none';

// --- this pack's own parameters -------------------------------------------------------
// Whose parameter dormancy is, and the key it is declared under on that pack's entry in
// `.claudinite-settings.json` (`{ "id": "claudinite-tasks", "config": { "dormant": true } }`).
// Exported because a reader that has to name the pack to find its config should name
// it from here rather than spell the id a second time.
export const TASKS_PACK_ID = 'claudinite-tasks';

export const DORMANT_CONFIG_KEY = 'dormant';

// --- the two workflow files -------------------------------------------------------------
// The vendored stubs' file names, identical in every member: the usage fold finds a
// repo's scheduler runs by the first, and every `workflow_dispatch` in the queue's chain
// names the second.
export const SCHEDULER_WORKFLOW_FILE = 'claudinite-scheduler.yml';

export const EXECUTOR_WORKFLOW_FILE = 'claudinite-executor.yml';

export const ENDS_WHEN_CLOSED = 'closed';

// The field's one pre-policy value, still the canonical spelling the legacy
// `yes`/`true` aliases collapse to on their way in.
export const MERGE_IF_NARROW = 'if-narrow';

// A canon pack's retired id, read as today's: a work item's title is stored data, and
// one filed before a rename still carries the old spelling.
const RENAMED_PACKS = new Map([['barriers', 'basics'], ['tidy-repo', 'basics'], ['static-website', 'public-website']]);
export const canonicalPackId = (id) => RENAMED_PACKS.get(id) ?? id;

// WHICH PARK IS A BROKEN RUN. A park is not live, so no park holds its task's lane by
// itself: the scheduler asks the task again on its own conditions, and only a task
// declaring `last-run-not-failed` stops past its own failure. What this predicate
// tells apart is the park a person DIAGNOSES from the three that are a person's
// inbox — a PR waiting to be approved, a choice waiting to be made, a secret waiting
// to be set — which is the split every renderer alarms on and the run-history term
// reads (`parkKindOf`).
//
// A park wearing NO sub-label counts as broken, which is what makes this safe on the
// way in: every item parked by an engine older than the sub-labels, and every kind
// word a future engine invents that this one does not know, reads as "diagnose me"
// rather than quietly joining the mechanical lane.
export const isBlockingPark = (item) => statusOf(item) === STATUS_NEEDS_HUMAN_FAILURE;

// --- the decode: legacy spellings are written never, read forever ---------------
// Labels are STORED DATA: open items filed by a fielded engine wear its spellings,
// closed items keep theirs forever, and members converge on their own schedules. So
// every reader here goes through one pass that maps every spelling ever written
// straight to today's — never through a literal comparison against one of them.
//
// The `task:*` entries come out when no OPEN item wears one (#1913); the two
// `outcome:*` entries never do, because a closed item's labels are stored data and
// dropping them would turn every historical run into an un-outcomed one.
// @legacy-tolerance advisory:none retire:#1913
const LEGACY_STATUS = new Map([
  [LEGACY_BLOCKED, STATUS_BLOCKED],
  [LEGACY_READY, STATUS_READY],
  [LEGACY_EXECUTING, STATUS_RUNNING_EXECUTOR],
  [LEGACY_AGENT, STATUS_RUNNING_AGENT],
  [LEGACY_TASK_DONE, STATUS_DONE], [OUTCOME_DONE, STATUS_DONE],
  [LEGACY_TASK_OBSOLETE, STATUS_REJECTED], [OUTCOME_OBSOLETE, STATUS_REJECTED],
]);

// @legacy-tolerance advisory:none retire:#1913
const LEGACY_PARK_RE = /^task:needs-human-(.+)$/;

// The park an issue's labels name, canonical, or null. Both shapes decode here:
// today's single `task:status:needs-human-<kind>` and the legacy pair
// (`needs-human` plus a sub-label). A kind nobody here knows — a bare legacy park,
// or a word a newer engine invented — reads as `failure`, the conservative lane.
function parkOf(names) {
  const kinds = [
    ...names.filter((n) => n.startsWith(PARK_PREFIX)).map((n) => n.slice(PARK_PREFIX.length)),
    ...names.map((n) => LEGACY_PARK_RE.exec(n)?.[1]).filter(Boolean),
  ];
  if (!kinds.length && !names.includes(NEEDS_HUMAN)) return null;
  return `${PARK_PREFIX}${PARK_KINDS.find((k) => kinds.includes(k)) ?? 'failure'}`;
}

// Every distinct status an issue's labels decode to. One entry per status, so an
// item mid-flip — wearing a legacy spelling beside its canonical one — reads as the
// ONE status it is, and only genuinely conflicting labels read as more than one
// (the dashboard's `torn`).
export function statusesOn(issue) {
  const names = labelNames(issue);
  const out = new Set();
  const park = parkOf(names);
  if (park) out.add(park);
  for (const n of names) {
    if (STATUS_LABELS.includes(n) && !n.startsWith(PARK_PREFIX)) out.add(n);
    else if (LEGACY_STATUS.has(n)) out.add(LEGACY_STATUS.get(n));
  }
  return [...out];
}

// THE status an issue wears, canonical, or null for one wearing none. A park wins
// over anything else present: a torn transition that left a state label beside a
// park must read as parked, or the queue would pick up an item a human owns.
export function statusOf(issue) {
  const worn = statusesOn(issue);
  return worn.find((s) => s.startsWith(PARK_PREFIX))
    ?? STATUS_LABELS.find((s) => worn.includes(s))
    ?? null;
}

export const isStatus = (issue, status) => statusOf(issue) === status;

export const isParked = (issue) => (statusOf(issue) ?? '').startsWith(PARK_PREFIX);

export const parkKindOf = (issue) =>
  (isParked(issue) ? statusOf(issue).slice(PARK_PREFIX.length) : null);

// The origin an issue wears, or null. Unlike the status there is no legacy
// spelling to fold in: `origin:schedule` is inert stored data (see ORIGIN_SCHEDULE),
// and reading it as an origin would put a marker nothing writes back into play.
export const originOf = (issue) =>
  labelNames(issue).find((n) => ORIGIN_LABELS.includes(n)) ?? null;

// Every spelling that MEANS `status` — what a transition out of it has to clear,
// since the item may wear any engine's. Leaving a park clears every park spelling
// whatever its kind: a re-queue takes the item out of the human's hands entirely.
export function spellingsOf(status) {
  if (String(status ?? '').startsWith(PARK_PREFIX)) {
    return [...PARK_STATUSES, ...PARK_KINDS.map((k) => `task:needs-human-${k}`), NEEDS_HUMAN];
  }
  const legacy = [...LEGACY_STATUS].filter(([, canonical]) => canonical === status).map(([l]) => l);
  return [status, ...legacy];
}

// A kind word (from a worker's own triage marker, or a call site) to the park it
// names. Anything unrecognised is a `failure`: a worker that misspells its class has
// a bug, which is exactly what that lane means.
export const triageLabelFor = (kind) =>
  (PARK_KINDS.includes(kind) ? `${PARK_PREFIX}${kind}` : STATUS_NEEDS_HUMAN_FAILURE);

// The stored-data rename rule, decode side: every spelling ever written maps
// STRAIGHT to the canonical word, in one pass — including `outcome:delivered`,
// which nothing writes any more but closed issues carry forever.
const OUTCOME_WORDS = new Map([[STATUS_DONE, 'done'], [STATUS_REJECTED, 'obsolete']]);

// The one outcome an issue's labels carry, as the canonical word ('done',
// 'delivered', 'obsolete') or null. Everything that tallies or renders outcomes
// decodes through here, so a spelling change is one map entry and not a sweep.
export function outcomeOf(issue) {
  // `statusesOn` rather than `statusOf`: a closed item's terminal write is its
  // outcome even when a park label stands beside it, and park precedence is a
  // question about LIVE items — what the queue may pick up — not about history.
  const worn = statusesOn(issue);
  for (const [status, word] of OUTCOME_WORDS) if (worn.includes(status)) return word;
  return hasLabel(issue, OUTCOME_DELIVERED) ? 'delivered' : null;
}

// GitHub hands labels back as objects on the issues API and as bare strings in
// some fixtures; accept either.
export const labelNames = (issue) =>
  (issue?.labels ?? []).map((l) => (typeof l === 'string' ? l : l?.name)).filter(Boolean);

export const hasLabel = (issue, name) => labelNames(issue).includes(name);

// pack and task ids are single path segments; the qualifier is whatever follows.
const TITLE_RE = /^\[claudinite-work\]\s+([^/\s]+)\/([^/\s]+)(?:\s+(\S.*))?$/;

// The pack half is canonicalized on the way out. A work item's title is STORED
// DATA — it sits on an open GitHub issue that outlives any one converge — so items
// filed before a pack was renamed still carry the old spelling. Read literally, the
// scheduler run would not recognise its own live item, would file a second one beside it, and
// would leave the first orphaned in the queue with nothing ever draining it.
export function parseWorkItemTitle(title) {
  const m = TITLE_RE.exec(String(title ?? '').trim());
  return m ? { pack: canonicalPackId(m[1]), task: m[2], qualifier: m[3]?.trim() || null } : null;
}

export const isWorkItemTitle = (title) => parseWorkItemTitle(title) !== null;

// The `<pack>/<task>` id a WORKER PATH names — the identity half a marked issue's
// title cannot carry, read off the path its machine block names. Two
// shapes, because tasks have two homes: the `tasks/` slot a declared pack contributes,
// and the queue's own built-in root. The `.claudinite/shared/` prefix is optional in
// both — a member's mount is there and the canon runs its own tree.
//
// THE BUILT-IN ROOT HAS TWO SPELLINGS, and both are permanent. The surface moved from
// `engine/scheduler/` to the tasks pack (#1317), so a live item minted before the move
// still names the engine path while every new one names the pack path; the wire id
// either produces is unchanged, which is what the move promised. Stored data is
// renamed on the DECODE side or it stops decoding, and here that failure is silent:
// this is the fallback for a marked issue, whose title is the requester's own words,
// so a path that yields null leaves the item unattributable rather than rejected.
//
// It is a PARSE, not a lookup: anything that must know the task exists at HEAD
// resolves the path against the discovered task set instead (the executor does).
const PACK_TASK_PATH_RE = /^(?:\.claudinite\/shared\/)?packs\/([^/]+)\/tasks\/([^/]+)\/[^/]+$/;

const BUILT_IN_TASK_PATH_RE = /^(?:\.claudinite\/shared\/)?(?:engine\/scheduler|packs\/claudinite-tasks)\/queue\/tasks\/([^/]+)\/[^/]+$/;

// The built-in spec's `public/` home, which new items name. `taskIdFromPath` is the
// DECODE side, so this is read forever beside the two above: an item minted today
// outlives any number of moves, and an undecodable path leaves it unattributable.
export const BUILT_IN_PUBLIC_TASK_PATH_RE = /^(?:\.claudinite\/(?:shared|local)\/)?packs\/claudinite-tasks\/public\/(implement-request)\.md$/;

export function taskIdFromPath(path) {
  const p = String(path ?? '');
  const pack = PACK_TASK_PATH_RE.exec(p);
  if (pack) return { pack: canonicalPackId(pack[1]), task: pack[2] };
  const builtIn = BUILT_IN_TASK_PATH_RE.exec(p) ?? BUILT_IN_PUBLIC_TASK_PATH_RE.exec(p);
  return builtIn ? { pack: 'engine', task: builtIn[1] } : null;
}

// A Merge/Automerge field value, fenced to what the policy engine can read: the
// canonical expression, or null for anything else. Fencing here is about SHAPE
// only — a well-formed policy naming a rule nobody defines rides through and
// fails closed at the verdict, loudly, which is better feedback than silently
// ignoring the ask. A policy of `nothing` is the default already, so it reads
// as absent.
//
// The grammar mirrors the policy engine's normalizePolicy - the semantic
// authority — and cannot import it: this module is deliberately pure (see the
// header), so the drift guard is a test that runs a value matrix through both
// sides.
const RULE_NAME = /^[a-z0-9]+(-[a-z0-9]+)*$/;

const PATH_SEGMENT = /^[A-Za-z0-9._-]+$/;

const policyName = (name) => {
  if (!name.startsWith('under:')) return RULE_NAME.test(name);
  const segments = name.slice('under:'.length).replace(/\/+$/, '').split('/');
  return segments.every((seg) => PATH_SEGMENT.test(seg) && seg !== '.' && seg !== '..');
};

// A term is `&&`-joined names, optionally reject:-prefixed. The value is returned
// CANONICAL — whitespace around `&&` collapsed — so an item body, and the trailer
// stamped from it, stay whitespace-free whatever a person typed.
function policyFieldValue(raw) {
  if (raw == null) return null;
  const value = String(raw).trim();
  if (['if-narrow', 'yes', 'true'].includes(value.toLowerCase())) return MERGE_IF_NARROW;
  if (value.toLowerCase() === 'anything') return 'anything';
  const terms = value.split(';').map((term) => {
    const prefix = term.trim().startsWith('reject:') ? 'reject:' : '';
    const names = term.trim().slice(prefix.length).split('&&').map((n) => n.trim());
    return { prefix, names, canonical: prefix + names.join('&&') };
  });
  const wellFormed = terms.length > 0
    && terms.every(({ names }) => names.every((n) => policyName(n) && n !== 'nothing'))
    && terms.some(({ prefix }) => !prefix);
  return wellFormed ? terms.map((t) => t.canonical).join(';') : null;
}

const BLOCK_RE = /<!-- claudinite-item -->\n?([\s\S]*?)\n?<!-- \/claudinite-item -->/;

// The machine's half of a body, or null where there is no block at all.
export const machineBlockOf = (body) => BLOCK_RE.exec(String(body ?? ''))?.[1] ?? null;

// The machine's half to read fields out of: the block where there is one, the whole
// body otherwise.
export const itemFieldText = (body) => machineBlockOf(body) ?? String(body ?? '');

// The human's half of a body — everything the machine block is not. A marked
// issue's PARAMETERS are read from here and never from the block: re-asking clears
// the status and leaves the previous run's block standing, and a parser that read
// the whole body would find that stale copy beside the person's own field.
export const humanTextOf = (body) => String(body ?? '').replace(BLOCK_RE, '').trim();

const NOT_BEFORE_RE = /^Not-before:[ \t]*(.*)$/m;

const BLOCKED_BY_RE = /^Blocked-by:[ \t]*(.*)$/m;

const REQUEST_RE = /^Request:[ \t]*#?(\d+)/m;

const MODEL_RE = /^Model:[ \t]*(\S+)/m;

const MERGE_RE = /^Merge:[ \t]*(\S+)/m;

const ENDS_WHEN_RE = /^Ends-when:[ \t]*#(\d+)[ \t]+(\S+)[ \t]*$/m;

const TARGET_BRANCH_RE = /^Target-branch:[ \t]*(\S+)[ \t]*$/m;

const TARGET_PR_RE = /^Target-pr:[ \t]*#?(\d+)[ \t]*$/m;

const SUPERSEDES_RE = /^Supersedes:[ \t]*(.*)$/m;

const WOKEN_RE = /^Woken:[ \t]*(\S+)[ \t]*$/m;

// The `Blocked-by` numbers a body names, from a work item's body or from an
// ORDINARY issue's — a request marked for implementation states what it waits on in
// the same field spelling, and adoption carries it onto the item it births.
export function parseBlockedBy(body) {
  const bb = BLOCKED_BY_RE.exec(String(body ?? ''))?.[1] ?? '';
  return [...bb.matchAll(/#(\d+)/g)].map((m) => Number(m[1]));
}

// Parse an item body back into the facts the scheduler run and the executor read. A body
// with no first line, or whose fields are absent, yields nulls — absence is
// meaningful everywhere here and is never filled in with a default.
export function parseWorkItemBody(body) {
  const text = itemFieldText(body);
  const taskPath = text.split('\n').map((l) => l.trim()).find((l) => l !== '') ?? null;
  const nb = NOT_BEFORE_RE.exec(text)?.[1]?.trim() || null;
  const blockedBy = parseBlockedBy(text);
  const request = REQUEST_RE.exec(text) ? Number(REQUEST_RE.exec(text)[1]) : null;
  // An unrecognised family reads as absent rather than as itself: the item's model
  // is behaviour-defining, so the only values that leave this parser are ones the
  // engine can actually dispatch at.
  const askedModel = MODEL_RE.exec(text)?.[1] ?? null;
  const model = REQUEST_MODELS.includes(askedModel) ? askedModel : null;
  // Same fencing as the model: an authorization that does not read as a policy
  // expression reads as absent, and absent is the safe end of this field — a run
  // that cannot read its permission opens a pull request and waits.
  const merge = policyFieldValue(MERGE_RE.exec(text)?.[1] ?? null);
  // Same fencing again, and here it is what keeps the janitor honest: a condition
  // it cannot evaluate must read as "no end condition", never as one that is met.
  const ends = ENDS_WHEN_RE.exec(text);
  const endsWhen = ends && ends[2] === ENDS_WHEN_CLOSED ? Number(ends[1]) : null;
  const targetBranch = TARGET_BRANCH_RE.exec(text)?.[1] ?? null;
  const targetPr = TARGET_PR_RE.exec(text) ? Number(TARGET_PR_RE.exec(text)[1]) : null;
  const supersedes = [...(SUPERSEDES_RE.exec(text)?.[1] ?? '').matchAll(/#(\d+)/g)].map((m) => Number(m[1]));
  const woken = WOKEN_RE.exec(text)?.[1] ?? null;
  return { taskPath, notBefore: nb, blockedBy, request, model, merge, endsWhen, targetBranch, targetPr, supersedes, woken };
}

// The item's own `### Context` bullets, in order — the binding scope a hand-created
// item was born with. Read back rather than kept only for the agent to read,
// because an operator's PARAMETERS ride here: `create-work-item --context
// "REPOS=Alpha Beta"` is how a forced run says what it is running on, and the
// executor hands these lines to code-work as `CLAUDINITE_CONTEXT`.
//
// A section runs to the next `### ` heading or to the end of the body — the same
// bounds `withSection` writes to — and only `- ` bullets count, so the prose
// framing around a section contributes nothing.
function sectionLines(body, heading) {
  const lines = String(body ?? '').split('\n');
  const at = lines.findIndex((l) => l.trim() === `### ${heading}`);
  if (at === -1) return [];
  const out = [];
  for (const line of lines.slice(at + 1)) {
    if (line.startsWith('### ')) break;
    const m = /^-[ \t]+(.*)$/.exec(line);
    if (m) out.push(m[1].trim());
  }
  return out;
}

export function parseLastVerdict(body) {
  const lines = sectionLines(body, LAST_VERDICT_HEADING);
  // The reason may carry the separator itself, so the split is on the FIRST match.
  const first = /^(.*?) — the precondition declined: ([\s\S]*)$/.exec(lines[0] ?? '');
  if (!first) return null;
  const until = lines.map((l) => /^Asked again at (.*?)\.?$/.exec(l)).find(Boolean)?.[1] ?? null;
  return { at: first[1], reason: first[2], until };
}

// IS THIS ISSUE AN ITEM? Two shapes: a filed `[claudinite-work]` issue, or an ordinary
// issue somebody marked `task:origin:ad-hoc` that has been ADOPTED — the mark alone
// is a request awaiting adoption, not yet an item, and reading it as one would have
// the janitor's stateless-repair rule park the person's issue for having no status.
//
// WHAT SAYS IT WAS ADOPTED IS EITHER ARTIFACT, and that is the point: adoption
// writes the machine block AND the first status, and the mark beside them is a
// LABEL a person can take off at any moment. Gated on the mark alone, an adopted
// item whose requester removed it drops out of every read of the queue at once —
// the executor never picks it, the precondition never gets to see the withdrawal
// and decline it, and the janitor's rules cannot sweep what they cannot list, so
// the item sits `waiting-for-executor` forever with nothing left to move it.
//
// It lives with the vocabulary rather than with the listing that applies it because
// the dashboard asks it in a BROWSER, where the listing's GitHub port does not load.
export const isQueueItem = (issue) =>
  String(issue?.title ?? '').startsWith(WORK_PREFIX)
  || machineBlockOf(issue?.body) !== null
  || (labelNames(issue).includes(ORIGIN_AD_HOC) && statusOf(issue) !== null);

