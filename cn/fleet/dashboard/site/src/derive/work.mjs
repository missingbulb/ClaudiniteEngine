// One table, three questions. Pure: no clock of its own, no I/O, no DOM.
//
// The repo page used to carry two tables — every declared task in one, every open work
// item in the other — and they were the same rows twice. A task's row could not say
// what its item was doing without repeating the item table, and an item's row could not
// say what it was FOR without repeating the roster. Worse, the reader had to join them
// by eye to answer the only question they open this page with: is anything stuck.
//
// So there is one row per piece of work, and the view decides which rows and which
// columns:
//
//   STUCK   — something has stopped. A park, a blown leash, an item off the state
//             machine, a task whose lane is held. This is the list to act on.
//   PENDING — work in flight or queued: an item exists and is moving.
//   ALL     — every declared task, including the ones that have never run, which is
//             the case a list built from items alone would silently omit.
//
// The classification is one function so the three views cannot disagree about what
// "stuck" means, and the ordering is severity-first so the top of any view is the worst
// thing true of this repo.

import {
  STATUS_BLOCKED, STATUS_READY, STATUS_RUNNING_EXECUTOR, STATUS_RUNNING_AGENT, STATUS_NEEDS_HUMAN_APPROVAL, STATUS_NEEDS_HUMAN_ACTION,
} from '../read/queue-vocabulary.mjs';
// `PARKED` is the page's own state key, not a label — a park is four labels and the
// page groups them into one (model.mjs).
import { PARKED } from './model.mjs';

export const VIEWS = Object.freeze(['stuck', 'pending', 'all']);

// The severity ladder, worst first — the same words and the same order the fleet page
// ranks members by, so a reader moving between the two pages is reading one scale.
const LEVELS = ['critical', 'serious', 'warning', 'info', 'ok'];
const levelRank = (l) => {
  const i = LEVELS.indexOf(l);
  return i === -1 ? LEVELS.length : i;
};

// The machine states an item can be in and still be moving. Anything else an OPEN item
// wears — a park, a torn label swap, no label at all — is a stop.
const MOVING = new Set([STATUS_BLOCKED, STATUS_READY, STATUS_RUNNING_EXECUTOR, STATUS_RUNNING_AGENT]);

// What is wrong with this row, worst first, or an empty list. Every entry mirrors
// something the engine will actually act on (or has stopped acting on), never a display
// heuristic — the warnings come from `describeItem`, which reads the real recovery
// rules, and the two added here are the two the item cannot state about itself.
export function troubles(row) {
  const out = [];
  const item = row.current;
  if (item) {
    if (item.state === PARKED) {
      out.push({
        level: item.blockingPark ? 'critical' : (item.triage === STATUS_NEEDS_HUMAN_APPROVAL ? 'warning' : 'serious'),
        text: item.warnings.find((w) => w.text.startsWith('parked'))?.text ?? 'parked for a human',
      });
    }
    for (const w of item.warnings) {
      if (w.text.startsWith('parked')) continue;      // already said, at its own severity
      out.push(w);
    }
    if (!MOVING.has(item.state) && item.state !== PARKED) {
      out.push({ level: 'warning', text: 'off the state machine — the janitor repairs it' });
    }
  }
  // A held lane is a fact about the TASK, not about the item holding it: nothing new
  // will be filed for this task until the park clears, and the item's own row says
  // only that it is parked.
  if (row.nextAsk?.kind === 'held') {
    out.push({ level: 'critical', text: 'the task\'s lane is held — nothing new is scheduled until this clears' });
  }
  return out.sort((a, b) => levelRank(a.level) - levelRank(b.level));
}

// Which view a row belongs to. `stuck` and `pending` are exclusive: an item that has
// stopped is not also in flight, whatever label it wears.
export function classify(row) {
  if (troubles(row).length) return 'stuck';
  return row.current ? 'pending' : 'idle';
}

// What acts soonest, for ordering within a severity band. Acting now beats every date;
// a held or unreadable schedule sinks, because it has no next.
export function askOrder(row) {
  const ask = row.nextAsk ?? { kind: 'note' };
  if (ask.kind === 'ready' || ask.kind === 'running' || ask.kind === 'ready-soon') return 0;
  if (ask.at) return ask.at.getTime();
  return Infinity;
}

// Every piece of work as one row: the declared tasks, plus any open item whose task
// this repo no longer declares — which is exactly the item nobody will ever pick up,
// and precisely what a roster-only table cannot show.
//
// `rows` is `buildRoster`'s output; `open` is the described open items.
export function workRows(rows, open) {
  const declared = new Set(rows.map((r) => r.key));
  const orphans = open
    .filter((i) => !i.key || !declared.has(i.key))
    .map((i) => ({
      key: i.key ?? `#${i.number}`,
      pack: i.pack,
      task: i.task,
      declaration: null,
      // No declaration, so nothing about its cadence was read — unknown, not "none".
      frequency: null,
      cadence: null,
      scheduled: null,
      periodMs: null,
      // Not a schedule problem to be waited out: the item names a task this repo does
      // not declare, so nothing will ever pick it up until a person acts.
      nextAsk: { kind: 'note', note: 'no declared task — nothing will pick this up' },
      anchorNote: null,
      current: i,
      openCount: 1,
      lastClosed: null,
      history: [],
      undeclared: true,
    }));

  return [...rows.map((r) => ({ ...r, undeclared: false })), ...orphans].map((r) => {
    const t = troubles(r);
    return {
      ...r,
      troubles: t,
      // An undeclared item is a stop of its own kind, and it has no warning of its own
      // to carry it — the queue's recovery rules are about items whose task exists.
      level: r.undeclared ? 'serious' : (t.length ? t[0].level : 'ok'),
      view: r.undeclared ? 'stuck' : classify(r),
    };
  }).sort((a, b) => levelRank(a.level) - levelRank(b.level) || askOrder(a) - askOrder(b) || a.key.localeCompare(b.key));
}

// The rows one view shows. `all` is every row; the other two are their own kind.
export const rowsFor = (all, view) => (view === 'all' ? all : all.filter((r) => r.view === view));

// How many rows each view holds, so the switcher can say what it is offering instead of
// making the reader click to find out — and so a view with nothing in it can say
// "nothing is stuck" rather than rendering an empty table.
export function viewCounts(all) {
  return {
    stuck: all.filter((r) => r.view === 'stuck').length,
    pending: all.filter((r) => r.view === 'pending').length,
    all: all.length,
  };
}

// The view to open on: whatever is worst and non-empty. A page whose default view is
// empty on a healthy repo teaches its reader that the page is broken.
export const defaultView = (counts) => (counts.stuck ? 'stuck' : counts.pending ? 'pending' : 'all');

// --- what is waiting on a person ----------------------------------------------------

// The park split, in the vocabulary the fleet page's estimate already speaks. Kept here
// rather than recomputed in the view so the repo page's minutes and the fleet row's
// minutes are the same arithmetic over the same definitions.
export function attentionOf(open) {
  const parked = open.filter((i) => i.state === PARKED);
  return {
    broken: parked.filter((i) => i.blockingPark).length,
    approvals: parked.filter((i) => !i.blockingPark && i.triage === STATUS_NEEDS_HUMAN_APPROVAL).length,
    actions: parked.filter((i) => !i.blockingPark && i.triage === STATUS_NEEDS_HUMAN_ACTION).length,
    decisions: parked.filter((i) => !i.blockingPark
      && ![STATUS_NEEDS_HUMAN_APPROVAL, STATUS_NEEDS_HUMAN_ACTION].includes(i.triage)).length,
    tripping: open.filter((i) => i.state !== PARKED && i.warnings.length).length,
  };
}
