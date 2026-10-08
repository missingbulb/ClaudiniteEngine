// The dashboard's derivation layer — repo facts in, view rows out. Pure: it reads
// no clock, opens no socket, and touches no DOM, so the whole of what the page
// claims is testable in Node and shared verbatim with the browser.
//
// It states NONE of the queue's vocabulary itself. The label set, the title
// grammar and the leash constants are imported from the queue's published
// `public/`, so for those there is no second copy to drift. The anchor arithmetic
// is the exception: packs share no code, so `task-calendar.mjs` beside this file
// carries the dashboard's own copy.
//
// Everything this file imports is pure ESM with no `node:` imports, which is the
// property it depends on.

import { parseTaskDeclaration, applyTaskDefaults } from './declaration-text.mjs';
import {
  mostRecentAnchor, nextAnchor, periodMs, taskPeriodMs, cadenceOf, normalizeCadenceTerms,
  holdsOnFailure, holdsOnAnyPark,
  DUE_TERM, SCHEDULE_TERM,
} from './task-calendar.mjs';
import {
  EXECUTING_LEASH_MS, AGENT_LEASH_MS, STALE_READY_PERIODS, STUCK_BLOCKED_MS,
} from '../read/queue-vocabulary.mjs';
import {
  WORK_PREFIX, STATUS_BLOCKED, STATUS_READY, URGENT, STATUS_RUNNING_EXECUTOR, STATUS_RUNNING_AGENT, 
  STATUS_NEEDS_HUMAN_ACTION, STATUS_NEEDS_HUMAN_DECISION,
  STATUS_NEEDS_HUMAN_APPROVAL, STATUS_NEEDS_HUMAN_FAILURE, CLAIM_MARKER, HANDOFF_MARKER, EPISODE_MARKER,
} from '../read/queue-vocabulary.mjs';
import {
  outcomeOf as decodeOutcome, statusesOn, isParked, parkKindOf, triageLabelFor, isBlockingPark,
  parseLastVerdict, parseWorkItemTitle, parseWorkItemBody, taskIdFromPath, hasLabel, labelNames,
} from '../read/queue-vocabulary.mjs';

export {
  WORK_PREFIX, STATUS_BLOCKED, STATUS_READY, URGENT, STATUS_RUNNING_EXECUTOR, STATUS_RUNNING_AGENT,
  EXECUTING_LEASH_MS, AGENT_LEASH_MS, STUCK_BLOCKED_MS, STALE_READY_PERIODS,
  parseWorkItemTitle, nextAnchor, mostRecentAnchor, periodMs,
};

// How long a due item may sit blocked before the page calls the scheduler run out. The scheduler run
// is the repo's one cron, hourly; two fires of slack keeps a single late fire from
// reading as a fault. Not an engine constant because nothing engine-side measures
// this — the scheduler run readies due items on its next fire, whenever that is.
export const DUE_SLACK_MS = 2 * 3600e3;

const ms = (t) => (t == null ? null : new Date(t).getTime());

// --- where a declared pack's tasks live ---------------------------------------

// A pack contributes tasks only if the repo DECLARES it: presence on disk is not
// activation (core's rule), and the mount carries packs a repo never declared. So
// the roster is built from the declaration list, and a task directory belonging to
// an undeclared pack is skipped rather than rendered greyed-out.
//
// Two roots, because the same code reads the canon home and a member: the home
// runs from the repo root (`packs/<id>`), a member from the mount
// (`.claudinite/shared/packs/<id>`), and a local pack — declared `local/<name>` —
// from `../../.claudinite/local/packs/<name>` in both.
export function declaredPackDirs(config) {
  const dirs = new Map();
  for (const entry of config?.packs ?? []) {
    const id = typeof entry === 'string' ? entry : entry?.id;
    if (!id) continue;
    if (id.startsWith('local/')) {
      const name = id.slice('local/'.length);
      dirs.set(id, [`.claudinite/local/packs/${name}`]);
    } else {
      dirs.set(id, [`packs/${id}`, `.claudinite/shared/packs/${id}`]);
    }
  }
  return dirs;
}

const TASK_PATH_RE = /^(.*)\/tasks\/([^/]+)\/task\.json$/;

// Every declared pack's task declarations, from one recursive tree listing.
// `paths` is the flat list of blob paths the tree API returned.
export function taskDeclarationPaths(paths, config) {
  const dirs = declaredPackDirs(config);
  const found = [];
  for (const path of paths ?? []) {
    const m = TASK_PATH_RE.exec(path);
    if (!m) continue;
    const [, packDir, task] = m;
    for (const [pack, roots] of dirs) {
      if (roots.includes(packDir)) found.push({ pack, task, path });
    }
  }
  return found.sort((a, b) => `${a.pack}/${a.task}`.localeCompare(`${b.pack}/${b.task}`));
}

// --- reading a task declaration -----------------------------------------------

// The page reads a declaration as TEXT: it renders other repos over the API, where
// there is nothing to import and no Node to import it with. A `task.json` parses
// whole and takes the contract's defaults for the fields it omits — the same
// door the engine's loader runs it through.
//
// A field this cannot read comes back NULL and renders as "unknown". It is never
// defaulted: a task whose declaration this misreads must look unreadable, because a
// plausible wrong cadence would silently move a next-anchor the whole roster is
// read for. An ABSENT `preconditions` is not an unread one: a declaration that was
// read and carries no key requires nothing, exactly as its author wrote it, and
// reads as `[]`. An absent `trigger` is not unread either — it is derived, as the
// contract's own door derives it.

// A declaration another repo still writes the retired `frequency` in carries no
// cadence onto the roster (#1732): the contract rejects such a declaration, so the
// task does not run at all in the repo that owns it, and a cadence read off the dead
// field would promise a next anchor nothing will ever reach.
//
// The cadence SPELLING door is a different thing and stays: `due:<cadence>` is the
// current term under the name it was introduced with, permanently accepted because a
// task declaration is member-owned data no vendoring pass rewrites. The page runs the
// same rewrite the contract's door runs, so both read one spelling.

// The retired empty precondition, which a declaration may still spell.
const NONE = 'none';
// THE TRIGGER, as the page reads it (#1725). `trigger` says whether the scheduler
// asks a task, and the declaration is the only thing that says so: nothing reads it
// off the shape of the conditions, here or at the contract's own door (#1789). So a
// declaration stating none reads as UNKNOWN rather than as either lane. It is one
// the engine no longer loads, and a page that guessed would show a roster row for a
// task that cannot run.
export const TRIGGER_SCHEDULE = 'schedule';
export const TRIGGER_REQUEST = 'request';
const withTrigger = (trigger) => (trigger === TRIGGER_SCHEDULE || trigger === TRIGGER_REQUEST ? trigger : null);

// A task may decline to run; whether it CAN is the difference between "did not run"
// being routine and being a fault, so the roster shows it. The cadence terms say WHEN
// a task runs, not whether, and `none` is the EMPTY precondition — so neither is a
// gate, and a task carrying only those answers no here, exactly as it should.
const CADENCE_TERM_NAMES = [SCHEDULE_TERM, DUE_TERM];
const termName = (t) => t.split(':')[0].trim();
const isGate = (entry) => String(entry ?? '').split('||')
  .some((t) => termName(t) !== NONE && !CADENCE_TERM_NAMES.includes(termName(t)));
const hasGate = (preconditions) => (preconditions ?? []).some(isGate);

// The fields the roster reads, and only those, so a declaration carrying more
// renders the same as one carrying exactly these. Unparseable text reads as every
// field unknown.
export function parseDeclaration(text) {
  let decl;
  try {
    decl = parseTaskDeclaration(String(text ?? ''));
  } catch {
    decl = null;
  }
  const read = decl !== null && typeof decl === 'object' && !Array.isArray(decl);
  if (read) applyTaskDefaults(decl); else decl = {};
  const scalarOf = (v) => (['string', 'number', 'boolean'].includes(typeof v) ? v : null);
  // On a declaration that parsed, an absent `preconditions` is the empty expression;
  // one present but not a list of strings was not read, and neither was anything on
  // text that did not parse.
  const stated = !read ? null
    : decl.preconditions === undefined ? []
      : Array.isArray(decl.preconditions) && decl.preconditions.every((c) => typeof c === 'string') ? decl.preconditions : null;
  const preconditions = normalizeCadenceTerms(stated);
  return {
    id: scalarOf(decl.id),
    trigger: withTrigger(scalarOf(decl.trigger)),
    agent_model: scalarOf(decl.agent_model),
    expected_outcome: scalarOf(decl.expected_outcome),
    interrupt_policy: scalarOf(decl.interrupt_policy),
    code_work: scalarOf(decl.code_work),
    agent_execution_timeout: scalarOf(decl.agent_execution_timeout),
    preconditions,
    has_precondition: hasGate(preconditions),
  };
}

// --- a task's cadence ------------------------------------------------------------

// What a declaration says about WHEN its task runs, in the roster's terms: one
// display word, the period the task keeps, and whether the scheduler asks it at all.
// Two fields answer that, as they do for the scheduler — `trigger` says whether the
// task is asked, `cadenceOf` says how often it may then say yes — and four answers
// stay apart: a cadence; NO cadence term, a task asked at every tick that runs on
// its other conditions; a REQUEST task, which the scheduler never asks and which
// runs only from an item somebody creates (a mark, a wake, a chain link); and
// UNREADABLE, a declaration this could not lift — because a parse failure shown as
// "on movement" would hide behind a legitimate answer, and a task that reads exactly
// as its author wrote it must not show as unknown.
//
// Only a stated cadence is on the calendar, so only it has a next anchor: no-cadence
// and off-schedule have no next instant at all. `scheduled` is null, not false, where
// nothing was read.
//
// `holdsOnFailure` is the same read for the one other thing a declaration says about
// its own lane: whether a failure park stops the task (`last-run-not-failed`). No
// park holds a lane by itself, so a failure the declaration does not name is a break
// to diagnose beside a task still being asked on schedule.
export function describeCadence(preconditions, trigger) {
  if (!Array.isArray(preconditions)) {
    return { frequency: null, cadence: null, periodMs: null, scheduled: null, holdsOnFailure: null, holdsOnAnyPark: null, anchorNote: 'cadence unknown' };
  }
  const holds = holdsOnFailure(preconditions);
  const holdsAnywhere = holdsOnAnyPark(preconditions);
  const stated = withTrigger(trigger);
  // What the conditions say is known here; which lane the task is in is not, and the
  // two are separate facts. A declaration stating no trigger names neither lane.
  if (stated === null) {
    return { frequency: null, cadence: null, periodMs: null, scheduled: null, holdsOnFailure: holds, holdsOnAnyPark: holdsAnywhere, anchorNote: 'states no trigger, so nothing says whether the scheduler asks it' };
  }
  if (stated !== TRIGGER_SCHEDULE) {
    return { frequency: 'unscheduled', cadence: null, periodMs: null, scheduled: false, holdsOnFailure: holds, holdsOnAnyPark: holdsAnywhere, anchorNote: 'not on the schedule — runs only from an item somebody creates' };
  }
  const cadence = cadenceOf(preconditions);
  const period = taskPeriodMs({ preconditions });
  if (cadence === null) {
    return { frequency: 'on movement', cadence, periodMs: period, scheduled: true, holdsOnFailure: holds, holdsOnAnyPark: holdsAnywhere, anchorNote: 'no cadence term — asked at every tick' };
  }
  return { frequency: cadence.cadence, cadence, periodMs: period, scheduled: true, holdsOnFailure: holds, holdsOnAnyPark: holdsAnywhere, anchorNote: null };
}

// --- work items ----------------------------------------------------------------

// An item is a filed `[claudinite-work]` issue OR an adopted marked issue — the
// one-issue request model's other shape, which keeps the person's own title. One
// definition, shared with the queue's own reader.
export { isQueueItem as isWorkItem } from '../read/queue-vocabulary.mjs';

// THE PAGE'S FIVE STATE KEYS. Four are the engine's own status labels; the fifth is
// this page's own word, because a park is four labels and the page groups them into
// one column, routing by kind separately (`triageOf`). It is a display key and never
// a label: nothing writes `parked` to an issue.
const STATE_KEY = new Map([
  [STATUS_BLOCKED, STATUS_BLOCKED], [STATUS_READY, STATUS_READY],
  [STATUS_RUNNING_EXECUTOR, STATUS_RUNNING_EXECUTOR], [STATUS_RUNNING_AGENT, STATUS_RUNNING_AGENT],
]);

// The one state an open item is in, decoded from whatever spelling filed it, or
// `unlabelled` for an item wearing none — which is not a display quirk but the
// torn-label-swap leavings the janitor repairs, so it gets its own rendered state
// rather than being folded into "blocked".
export const PARKED = 'parked';

export function stateOf(item) {
  if (item?.state === 'closed') return 'closed';
  if (isParked(item)) return PARKED;
  const worn = statusesOn(item).filter((s) => STATE_KEY.has(s));
  if (worn.length === 1) return STATE_KEY.get(worn[0]);
  if (worn.length > 1) return 'torn';
  return 'unlabelled';
}

// What the park is asking for, or null for an item that is not parked. A park whose
// kind cannot be decoded — an older engine's bare one, a word a newer engine
// invented — reads as `failure`, the lane that says "someone diagnose this".
export const triageOf = (item) => (isParked(item) ? triageLabelFor(parkKindOf(item)) : null);

const TRIAGE_TEXT = {
  [STATUS_NEEDS_HUMAN_APPROVAL]: 'a PR to approve',
  [STATUS_NEEDS_HUMAN_ACTION]: 'something to change outside the code',
  [STATUS_NEEDS_HUMAN_DECISION]: 'a decision to make',
  [STATUS_NEEDS_HUMAN_FAILURE]: 'a break to diagnose',
};

// The outcome as its canonical word ('done' | 'delivered' | 'obsolete' | null) —
// the engine's decoder, which maps every legacy spelling straight to today's.
export const outcomeOf = decodeOutcome;

// How long the item has sat where it is. Every transition is a label write, so
// `updated_at` is the last touch — the same quantity the janitor's rules count.
const idleMs = (item, now) => ms(now) - (ms(item?.updated_at) ?? ms(item?.created_at) ?? ms(now));

// Warnings, each mirroring a real recovery rule rather than a display heuristic, so
// what the page flags is what the engine will actually act on — and how long the
// viewer waits for it.
//
// `isOpen(number)` answers whether a `Blocked-by` issue is still open — true, false,
// or null for one outside what the caller fetched. Unknown is never alarmed on.
export function warningsFor(item, now, { periodFor = () => null, isOpen = () => null } = {}) {
  const out = [];
  const state = stateOf(item);
  const idle = idleMs(item, now);
  if (state === STATUS_RUNNING_EXECUTOR && idle >= EXECUTING_LEASH_MS) {
    out.push({ level: 'serious', text: 'executing past the leash — the next scheduler run reclaims it' });
  }
  if (state === STATUS_RUNNING_AGENT && idle >= AGENT_LEASH_MS) {
    out.push({ level: 'serious', text: 'agent claim past the leash — the janitor reclaims it' });
  }
  if (state === STATUS_READY) {
    const per = periodFor(`${parseWorkItemTitle(item.title)?.pack}/${parseWorkItemTitle(item.title)?.task}`) ?? 86400e3;
    if (idle >= STALE_READY_PERIODS * per) out.push({ level: 'serious', text: 'ready but unpicked for ~2 periods' });
  }
  if (state === STATUS_BLOCKED) {
    // The standing-item model: blocked is the queue's healthy quiet state, not a
    // fault. A rolled item waiting out its Not-before never warns; what does warn is
    // the two things the engine would actually act on — dependencies unresolved past
    // the janitor's threshold, and an item DUE that the scheduler run has failed to ready.
    const { notBefore, blockedBy } = parseWorkItemBody(item.body);
    const wake = ms(notBefore);
    const depStates = blockedBy.map((n) => isOpen(n));
    if (wake !== null && wake > ms(now)) {
      // waiting out its stamped wake — healthy, whatever its age
    } else if (depStates.some((s) => s === true)) {
      if (idle >= STUCK_BLOCKED_MS) {
        out.push({ level: 'warning', text: `blocked on ${blockedBy.map((n) => `#${n}`).join(', ')} for over 2 days — the janitor flags stuck dependencies` });
      }
    } else if (!depStates.some((s) => s === null)) {
      // Nothing blocks it any more: the next scheduler run readies it. Due only measures from
      // the stamped wake — with dependencies the closing time is not on this item,
      // and a guess would alarm on an item that became due minutes ago.
      if (wake !== null && ms(now) - wake >= DUE_SLACK_MS) {
        out.push({ level: 'serious', text: 'due but not readied — is the scheduler run running?' });
      } else if (wake === null && blockedBy.length === 0 && idle >= DUE_SLACK_MS) {
        out.push({ level: 'serious', text: 'due but not readied — is the scheduler run running?' });
      }
    }
  }
  if (state === PARKED) {
    // What the park is asking for — an approval waiting on a reviewer is not the
    // same alarm as a broken run. Whether the break also holds the task's lane is
    // the declaration's word, not the park's, and the roster's next ask says it.
    const t = triageOf(item);
    out.push({
      level: isBlockingPark(item) ? 'critical' : 'warning',
      text: t ? `parked for a human — ${TRIAGE_TEXT[t]}` : 'parked for a human — unclassified',
    });
  }
  if (state === 'torn') out.push({ level: 'warning', text: 'wearing more than one state label' });
  if (state === 'unlabelled') out.push({ level: 'warning', text: 'open with no state label' });
  return out;
}

// An open item, decorated with everything the queue lane renders.
export function describeItem(item, now, opts = {}) {
  // A marked issue keeps the person's own title, so its task comes from the worker
  // path its machine block names — without that the page would render every request
  // run as an item belonging to no task at all.
  const parsed = parseWorkItemTitle(item.title)
    ?? (taskIdFromPath(parseWorkItemBody(item.body).taskPath) ? { ...taskIdFromPath(parseWorkItemBody(item.body).taskPath), qualifier: null } : null)
    ?? { pack: null, task: null, qualifier: null };
  const body = parseWorkItemBody(item.body);
  const state = stateOf(item);
  return {
    number: item.number,
    // The issue's own title, kept as GitHub has it: a surface that NAMES one item —
    // rather than counting them — has nothing else to call it by.
    title: item.title,
    key: parsed.pack && parsed.task ? `${parsed.pack}/${parsed.task}` : null,
    ...parsed,
    state,
    outcome: outcomeOf(item),
    triage: triageOf(item),
    blockingPark: state === PARKED && isBlockingPark(item),
    urgent: hasLabel(item, URGENT),
    labels: labelNames(item),
    notBefore: body.notBefore,
    blockedBy: body.blockedBy,
    taskPath: body.taskPath,
    // When somebody created or force-woke this item, or null for one the scheduler filed.
    woken: body.woken ?? null,
    // The roll's record: when the last ask declined, why, and the stamped wake.
    lastVerdict: parseLastVerdict(item.body),
    createdAt: item.created_at,
    updatedAt: item.updated_at,
    closedAt: item.closed_at ?? null,
    idleMs: idleMs(item, now),
    comments: item.comments ?? 0,
    warnings: warningsFor(item, now, opts),
  };
}

// --- the roster ----------------------------------------------------------------

// The conditions a roster entry's declaration states. A declaration that was read
// (`parseDeclaration`'s object) carries `preconditions` as the list it states, `[]`
// for an absent key, or null for a field it could not read; an entry that never got
// a declaration (null — an orphan, a file the API did not return) is unknown whole.
// The key absent on a declaration object is the empty expression here too, so a
// caller building the object by hand reads the same way: only null means "not read".
const statedPreconditions = (declaration) => {
  if (declaration === null || typeof declaration !== 'object') return null;
  return declaration.preconditions === undefined ? [] : declaration.preconditions;
};

// The trigger the same entry states. Null for an entry that never got a declaration,
// and for one whose declaration names no legal trigger: neither lane is a fact here.
const statedTrigger = (declaration) => {
  if (declaration === null || typeof declaration !== 'object') return null;
  return withTrigger(declaration.trigger);
};

// One row per DECLARED task, whether or not it has ever run — a task that has never
// produced an item is exactly the interesting case, and an issue-derived list would
// omit it entirely.
export function buildRoster({ tasks = [], items = [], now, schedule, isOpen }) {
  const byKey = new Map();
  for (const it of items) {
    const p = parseWorkItemTitle(it.title);
    if (!p) continue;
    const key = `${p.pack}/${p.task}`;
    if (!byKey.has(key)) byKey.set(key, []);
    byKey.get(key).push(it);
  }

  return tasks.map((t) => {
    const key = `${t.pack}/${t.task}`;
    const mine = (byKey.get(key) ?? []).slice()
      .sort((a, b) => ms(b.created_at) - ms(a.created_at));
    const open = mine.filter((i) => i.state === 'open');
    const closed = mine.filter((i) => i.state === 'closed');
    const read = describeCadence(statedPreconditions(t.declaration), statedTrigger(t.declaration));

    // Only a stated cadence is on the calendar; every other reading carries its own
    // note instead, so an unreadable declaration yields no anchor rather than a guess.
    const anchorNote = read.anchorNote;
    const next = read.cadence === null ? null : nextAnchor(read.cadence.cadence, now);

    const current = open.length
      ? describeItem(open[0], now, { periodFor: () => read.periodMs, isOpen })
      : null;

    return {
      key,
      pack: t.pack,
      task: t.task,
      path: t.path,
      declaration: t.declaration ?? null,
      frequency: read.frequency,
      cadence: read.cadence,
      scheduled: read.scheduled,
      holdsOnFailure: read.holdsOnFailure,
      holdsOnAnyPark: read.holdsOnAnyPark,
      nextAnchor: next,
      anchorNote,
      periodMs: read.periodMs,
      current,
      nextAsk: nextAskOf(current, next, anchorNote, read.holdsOnFailure, read.holdsOnAnyPark),
      openCount: open.length,
      lastClosed: closed.length ? describeItem(closed[0], now) : null,
      history: closed.map((i) => describeItem(i, now)),
    };
  });
}

// What will actually happen to this task next, derived from the standing item where
// one exists — the calendar answers only when no item does (the next instantiation).
// This is where the standing-item model's facts become the roster's advice:
//   - the stamped Not-before IS the schedule (S28), so it wins over the
//     computed anchor;
//   - a park holds the task's lane only where the declaration says so — with
//     `last-run-not-failed` for the failure park, or `last-run-not-parked` for
//     all four. Showing an anchor there would promise a run the task itself
//     declines, and showing `held` anywhere else would hide a run the scheduler
//     files on schedule around the park;
//   - every park the declaration does not name consumed its occurrence and leaves
//     the lane open, so the next anchor stands beside it.
function nextAskOf(current, anchor, anchorNote, holdsOnFailure, holdsOnAnyPark) {
  if (!current) return anchor ? { kind: 'anchor', at: anchor } : { kind: 'note', note: anchorNote };
  if (current.state === STATUS_READY) return { kind: 'ready', urgent: current.urgent };
  if (current.state === STATUS_RUNNING_EXECUTOR || current.state === STATUS_RUNNING_AGENT) {
    return { kind: 'running', phase: current.state === STATUS_RUNNING_AGENT ? 'agent' : 'executor' };
  }
  if (current.state === PARKED) {
    if (holdsOnAnyPark === true || (current.blockingPark && holdsOnFailure === true)) return { kind: 'held' };
    return anchor ? { kind: 'anchor', at: anchor } : { kind: 'note', note: anchorNote };
  }
  if (current.state === STATUS_BLOCKED) {
    if (current.notBefore) return { kind: 'wake', at: new Date(current.notBefore) };
    return current.blockedBy.length ? { kind: 'deps', on: current.blockedBy } : { kind: 'ready-soon' };
  }
  // torn / unlabelled — off the state machine until the janitor repairs it.
  return { kind: 'off-machine' };
}

// --- what is about to happen ------------------------------------------------------

// The next 24 hours as UTC hour buckets, each carrying the task rows whose next ask
// lands in it. The strip this draws answers a question no single row can: whether the
// day's scheduled work is spread out or piled into one hour.
//
// FLEET OR REPO, the same reduction: the caller hands in whatever roster rows it has,
// one member's or every member's, and a row carries the repo it came from where that
// matters.
//
// A `held` next ask is placed at NOW and marked critical rather than left out. A task
// whose blocking park stops it being scheduled at all has no future anchor, and
// dropping it would draw the emptiest strip on the worst-off repo — the one case where
// an empty hour must not read as a quiet one.
export const WAKE_STRIP_HOURS = 24;

export function wakeStrip(rows, now, { hours = WAKE_STRIP_HOURS } = {}) {
  const start = new Date(now);
  start.setUTCMinutes(0, 0, 0);
  const key = (t) => new Date(t).toISOString().slice(0, 13);
  const buckets = Array.from({ length: hours }, (_, i) => ({
    hour: key(start.getTime() + i * 3600e3),
    tasks: [],
    held: 0,
  }));
  const byHour = new Map(buckets.map((b) => [b.hour, b]));

  for (const row of rows ?? []) {
    const ask = row?.nextAsk;
    if (!ask) continue;
    const held = ask.kind === 'held';
    // Only the two kinds that name a MOMENT land on a strip of hours: an anchor (the
    // calendar's next fire) and a wake (a stamped Not-before, which IS the schedule).
    // `ready`, `running` and `deps` are happening or waiting on something other than
    // the clock, and belong to the row rather than to a future hour.
    const at = held ? start.getTime() : (ask.kind === 'anchor' || ask.kind === 'wake' ? ms(ask.at) : null);
    if (at === null) continue;
    const bucket = byHour.get(key(at));
    if (!bucket) continue;                      // past the strip's far end — not this day's business
    bucket.tasks.push({ key: row.key, repo: row.repo ?? null, held });
    if (held) bucket.held += 1;
  }

  const peak = buckets.reduce((n, b) => Math.max(n, b.tasks.length), 0);
  return { from: buckets[0]?.hour ?? null, hours: buckets, peak };
}

// Outcome tallies over the closed items the scan actually saw. `scanned` travels
// with them: every count here is over a window, and a window is not "all of it".
export function outcomeTally(rows) {
  const t = { done: 0, delivered: 0, obsolete: 0, none: 0 };
  for (const r of rows) for (const h of r.history) t[h.outcome ?? 'none'] += 1;
  return t;
}

// --- the protocol comments ------------------------------------------------------

// Which of the three markers a comment carries. The markers are HTML comments so a
// human reading the issue sees prose; the page shows the protocol beat instead.
export function commentKind(body) {
  const b = String(body ?? '');
  if (b.includes(EPISODE_MARKER)) return 'episode';
  if (b.includes(HANDOFF_MARKER)) return 'handoff';
  if (b.includes(CLAIM_MARKER)) return 'claim';
  return null;
}
