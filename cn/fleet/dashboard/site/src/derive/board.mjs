// The Work board, as data — lanes, marks and a schedule grid on a time axis
// ([docs/work-board.md](../../docs/work-board.md)). Pure: no clock of its own, no DOM.
//
// WHY A BOARD AND NOT A TABLE. The three table views answer *what is stuck*, one row
// per piece of work. They cannot answer the three questions this page is opened with —
// what will the next few days look like, where is the flow broken, and how much of it
// lands on a person — because each needs two things on one row, or a time under them.
//
// EVERY MARK IS READ, NEVER INFERRED. A label, a body line, a timestamp, or a task's
// own declaration; and a field the listing does not carry is `notRead`, never 0. That
// is the one rule this whole file is built to keep: the board's authority comes from
// every mark on it being traceable to a field a reader can go and look at.
//
// THE LANE IS THE FLOW. A row is one connected component of the edge graph — never a
// task (which hides the chain: an approval park is nothing without the PR it holds) and
// never a kind (which puts the two ends of one `Blocked-by` edge in different rows,
// and the edge is the finding).

import {
  isQueueItem, parseWorkItemBody, parseWorkItemTitle, outcomeOf, statusOf, labelNames,
} from '../read/queue-vocabulary.mjs';
import {
  STATUS_NEEDS_HUMAN_APPROVAL, STATUS_NEEDS_HUMAN_FAILURE, STATUS_RUNNING_AGENT,
  STATUS_RUNNING_EXECUTOR, STATUS_READY, PARK_PREFIX, PARK_KINDS, ORIGIN_AD_HOC, ASKED_FOR_ORIGINS,
} from '../read/queue-vocabulary.mjs';
import { nextAnchor } from './task-calendar.mjs';

// Every origin a person's action produces, as against the schedule's own.
const askedForOrigins = () => ASKED_FOR_ORIGINS;

const DAY = 86400e3;

// SEVEN BACK, TODAY, FOUR AHEAD. Four days ahead is the honest horizon — past it every
// prediction is "the same daily ticks again" — and seven back is the week of record the
// ledger above compares, which is what gives the task grid a past beside its future.
export const DAYS_BACK = 7;
export const DAYS_AHEAD = 4;

// How many rows a group draws before it collapses the rest into one line. Four is what
// fits above the fold beside the other three groups; the rest are named, never dropped.
export const GROUP_CAP = 4;

// When a plain issue is ROTTING. Fourteen days of nobody touching it is the bar the
// queue's own janitor uses, so the board and the queue agree about what stale means.
export const ROT_DAYS = 14;

const ms = (t) => (t == null ? null : new Date(t).getTime());
const dayOf = (t) => new Date(t).toISOString().slice(0, 10);

// The board's own axis. Every column carries the repo's daily anchor, because that is
// where every scheduled thing on the board sits.
export function axisOf(now, schedule, { back = DAYS_BACK, ahead = DAYS_AHEAD } = {}) {
  const today = Math.floor(now / DAY) * DAY;
  const hour = Number.isFinite(schedule?.dailyHour) ? schedule.dailyHour : 0;
  const days = [];
  for (let i = -back; i <= ahead; i += 1) {
    const start = today + i * DAY;
    days.push({
      day: dayOf(start),
      start,
      end: start + DAY,
      anchorAt: start + hour * 3600e3,
      past: i < 0,
      today: i === 0,
      future: i > 0,
    });
  }
  return { days, now, from: days[0].start, to: days[days.length - 1].end, dailyHour: hour };
}

// --- the edge graph ------------------------------------------------------------------

// Every edge the listings carry, in one direction each: from the thing that WAITS to
// the thing it waits on.
//
// `Refs #n` is deliberately absent: a PR's body is not stored by the page's projection
// (only the issue it closes is parsed out of it before it is dropped), so an edge that
// would need it is not read rather than guessed at.
export function edgesOf(items, prs) {
  const edges = [];
  for (const item of items) {
    const body = parseWorkItemBody(item.body);
    for (const target of body.blockedBy) edges.push({ from: item.number, to: target, kind: 'blocked-by' });
    if (body.endsWhen) edges.push({ from: item.number, to: body.endsWhen, kind: 'ends-when' });
  }
  for (const pr of prs) {
    if (pr.closesIssue) edges.push({ from: `pr:${pr.number}`, to: pr.closesIssue, kind: 'closes' });
  }
  return edges;
}

// The connected components of that graph. A lone node is a component of one, which is
// what lets a plain issue nobody links to still be a row when it earns one.
export function componentsOf(nodes, edges) {
  const parent = new Map(nodes.map((n) => [n, n]));
  const find = (n) => {
    if (!parent.has(n)) parent.set(n, n);
    while (parent.get(n) !== n) { parent.set(n, parent.get(parent.get(n))); n = parent.get(n); }
    return n;
  };
  const union = (a, b) => { const ra = find(a); const rb = find(b); if (ra !== rb) parent.set(ra, rb); };
  for (const e of edges) { if (parent.has(e.from) || parent.has(e.to)) union(e.from, e.to); }
  const groups = new Map();
  for (const n of nodes) {
    const root = find(n);
    if (!groups.has(root)) groups.set(root, []);
    groups.get(root).push(n);
  }
  return [...groups.values()];
}

// --- where a mark sits in time ---------------------------------------------------------

// An item's PREDICTED time, and why it is there. This is the board's single most
// load-bearing derivation, so each branch names the field it read:
//
//   a stamped `Not-before`     → that instant. The stamp IS the schedule.
//   blocked by a PR            → now, and "when you merge it" — a person's move.
//   blocked by an item that has a time → after it.
//   blocked by a plain issue nobody is scheduled to close → NO TIME, and the lane is
//                                drawn broken. That is the finding, not a gap.
//   ready                      → the next daily anchor, the tick it will be drained on.
//   parked or running          → now, where the person and the machine both are.
export function placeItem(item, { now, axis, byNumber, moversByNumber, depth = 0 }) {
  const status = statusOf(item);
  const body = parseWorkItemBody(item.body);
  if (status && status.startsWith(PARK_PREFIX)) return { at: now, why: 'parked', kind: 'park' };
  if (status === STATUS_RUNNING_AGENT || status === STATUS_RUNNING_EXECUTOR) {
    return { at: now, why: 'running', kind: 'running' };
  }
  const notBefore = ms(body.notBefore);
  if (notBefore !== null && Number.isFinite(notBefore)) {
    return { at: notBefore, why: `Not-before ${body.notBefore}`, kind: 'blocked-date' };
  }
  // An unreadable `Not-before` is FLAGGED, never guessed: a relative date a person
  // meant is a date the machine will treat as absent, and that is worth saying.
  if (body.notBefore) return { at: null, why: `a relative date the parser does not read: ${body.notBefore}`, kind: 'unreadable-date', flagged: true };

  for (const target of body.blockedBy) {
    const mover = moversByNumber.get(target);
    if (mover?.kind === 'pr') return { at: now, why: `#${target} lands when you merge PR #${mover.number}`, kind: 'blocked-pr' };
    const blocker = byNumber.get(target);
    if (!blocker) continue;
    if (!isQueueItem(blocker)) {
      // A plain issue no task and no PR will close. Nothing on the board moves it, so
      // the downstream item has no time at all and its lane is drawn broken.
      return { at: null, why: `no one is scheduled to close #${target}`, kind: 'broken', broken: true, blocker: target };
    }
    if (depth >= 4) break;                              // a cycle, or a chain past reading
    const upstream = placeItem(blocker, { now, axis, byNumber, moversByNumber, depth: depth + 1 });
    if (upstream.at === null) return { ...upstream, why: `after #${target}: ${upstream.why}` };
    return { at: upstream.at, why: `after #${target}`, kind: 'blocked-item' };
  }

  if (status === STATUS_READY) {
    return { at: nextDailyAnchor(now, axis), why: 'ready — the next scheduler tick', kind: 'ready' };
  }
  // An item on no queue mark at all: nothing will pick it up, and saying so is the
  // whole reason it is on the board.
  return { at: null, why: 'unmarked — no queue status, nothing will pick this up', kind: 'unmarked', flagged: true };
}

export const nextDailyAnchor = (now, axis) =>
  axis.days.find((d) => d.anchorAt > now)?.anchorAt ?? null;

// --- the Now group --------------------------------------------------------------------

// Why a pull request waits for a person — three different reasons drawn as one amber
// flag, and named apart in the panel because the reader's next move differs.
export function prWaits(pr, { item = null, declaration = null } = {}) {
  const merge = item ? parseWorkItemBody(item.body).merge : null;
  if (declaration && declaration.automerge === 'nothing') {
    return { waits: true, why: `${declaration.key ?? 'its task'} declares automerge: nothing` };
  }
  if (item && !merge) return { waits: true, why: 'its item carries no Merge policy' };
  if (!item) return { waits: true, why: 'no work item names this PR — nothing will land it' };
  // The run's own converge verdict lives in a comment this listing does not fetch, so
  // a PR whose item DOES authorize a merge is "still open" rather than "waits for you".
  return { waits: false, why: `its item authorizes ${merge}`, notRead: 'the run\'s own AUTOMERGE verdict is in a comment this page has not fetched' };
}

// --- the Scheduled group's task × day grid ---------------------------------------------

const OUTCOME_CELL = { done: 'ran', delivered: 'ran', obsolete: 'declined' };

// One row per scheduled task, one cell per day. A cell's state is form plus the
// semantic set, never colour alone.
//
// A FAILURE PARK LANDS ON ITS OWN TASK'S ROW, followed by whatever the task did after
// it — so where the declaration holds its lane on a failure, a hatched cell followed
// by filled ones is the record disagreeing with the roster's claim that the lane is
// held, read left to right on one line. There is no separate held-lanes row, because
// the contradiction is a row fact.
//
// A task gets its OWN row when it can fire on any day of the axis — a daily cadence,
// an elapsed one of a day or under, or no cadence term at all (asked at every tick).
// Anything on a longer cadence shares one row: the reader's question of a weekly
// task is whether it fired at all this week, not which weekday it prefers.
const onOwnRow = (row) => row.periodMs === null || row.periodMs <= DAY;

export function scheduleGrid(rows, items, axis, { now, schedule }) {
  // A `request` task is never on the schedule (`scheduled: false`), and a
  // declaration this could not read (`scheduled: null`) is not put on it either — a
  // row there would be a claim about a future nothing was read for.
  const scheduled = rows.filter((r) => r.scheduled);
  const daily = scheduled.filter(onOwnRow);
  const others = scheduled.filter((r) => !onOwnRow(r));

  const cellsFor = (row) => axis.days.map((day) => {
    const occurrences = items.filter((i) => {
      const parsed = parseWorkItemTitle(i.title);
      const key = parsed ? `${parsed.pack}/${parsed.task}` : null;
      if (key !== row.key) return false;
      const when = i.state === 'closed' ? ms(i.closed_at) : ms(i.updated_at);
      return when !== null && when >= day.start && when < day.end;
    });

    if (day.future) {
      const at = nextAnchorFor(row, now);
      if (at === null || at < day.start || at >= day.end) return { day: day.day, state: 'none', count: 0 };
      // Predicted and WILL DECLINE are told apart by height, never by dash pattern,
      // which at three pixels is invisible.
      const willDecline = row.lastClosed?.outcome === 'obsolete';
      return { day: day.day, state: willDecline ? 'will-decline' : 'predicted', count: 0 };
    }

    const open = occurrences.filter((i) => i.state === 'open');
    const park = open.find((i) => statusOf(i) === STATUS_NEEDS_HUMAN_FAILURE);
    if (park) return { day: day.day, state: 'failure-park', count: 1, number: park.number };
    const approval = open.find((i) => statusOf(i) === STATUS_NEEDS_HUMAN_APPROVAL);
    if (approval) return { day: day.day, state: 'parked', count: 1, number: approval.number };
    const running = open.find((i) => [STATUS_RUNNING_AGENT, STATUS_RUNNING_EXECUTOR].includes(statusOf(i)));
    if (running) return { day: day.day, state: 'running', count: 1, number: running.number };

    const closed = occurrences.filter((i) => i.state === 'closed');
    if (!closed.length) return { day: day.day, state: 'none', count: 0 };
    const states = closed.map((i) => OUTCOME_CELL[outcomeOf(i)] ?? 'declined');
    return {
      day: day.day,
      state: states.includes('ran') ? 'ran' : 'declined',
      count: closed.length,
      numbers: closed.map((i) => i.number),
    };
  });

  const gridRows = daily.map((row) => ({ key: row.key, task: row.task, pack: row.pack, cells: cellsFor(row), row }));
  if (others.length) {
    gridRows.push({
      key: 'other-cadences',
      task: `${others.length} on a longer cadence`,
      pack: null,
      collapsed: others.map((r) => r.key),
      cells: axis.days.map((day) => {
        const merged = others.map((row) => cellsFor(row).find((c) => c.day === day.day));
        const live = merged.filter((c) => c && c.state !== 'none');
        if (!live.length) return { day: day.day, state: 'none', count: 0 };
        const worst = ['failure-park', 'parked', 'running', 'ran', 'declined', 'predicted', 'will-decline']
          .find((s) => live.some((c) => c.state === s));
        return { day: day.day, state: worst, count: live.reduce((n, c) => n + (c.count || 1), 0) };
      }),
    });
  }
  return gridRows;
}

// Only a stated cadence is on the calendar; a task with none has no instant to
// predict, so its future cells stay empty rather than guessed.
const nextAnchorFor = (row, now) => {
  if (row.nextAsk?.at) return ms(row.nextAsk.at);
  if (!row.cadence) return null;
  const at = nextAnchor(row.cadence.cadence, now);
  return at ? ms(at) : null;
};

// --- tomorrow's human workload -----------------------------------------------------------

// The Now group's header sentence, DERIVED and never a tile: the PRs waiting now, plus
// the scheduled runs whose own declaration cannot land the PR they will open. It is the
// `automerge` field read against each task's cadence, not a guess about tomorrow — and
// a task with no cadence term is counted apart, because "a day" is a promise its
// declaration never made.
export function workloadLine(waitingPrs, rows, { schedule, now }) {
  const producers = rows.filter((r) => r.scheduled
    && (r.declaration?.automerge === 'nothing' || r.declaration?.automerge == null));
  const daily = producers.filter((r) => r.periodMs !== null && r.periodMs <= DAY);
  const rest = producers.filter((r) => r.periodMs !== null && r.periodMs > DAY);
  const movement = producers.filter((r) => r.periodMs === null);
  const parts = [`${waitingPrs} open PR${waitingPrs === 1 ? '' : 's'}`];
  parts.push(waitingPrs ? 'every one waits for a person' : 'nothing waits for a person');
  if (daily.length) parts.push(`+${daily.length} a day from ${daily.map((r) => r.task).slice(0, 2).join(', ')}${daily.length > 2 ? ` +${daily.length - 2}` : ''}`);
  if (rest.length) parts.push(`${rest.length} more on longer cadences`);
  if (movement.length) parts.push(`${movement.length} more on movement`);
  return parts.join(' · ');
}

// --- the quiet tail -------------------------------------------------------------------

// One ruled line, not a group: the plain issues on no edge, counted by what matters.
export function quietTail(issues, edges, now) {
  const onEdge = new Set(edges.flatMap((e) => [e.from, e.to]));
  const quiet = issues.filter((i) => !isQueueItem(i) && !onEdge.has(i.number));
  const has = (i, label) => labelNames(i).includes(label);
  const rotting = quiet.filter((i) => (now - (ms(i.updated_at) ?? now)) >= ROT_DAYS * DAY);
  return {
    total: quiet.length,
    rotting: rotting.length,
    quickWin: quiet.filter((i) => has(i, 'quick-win')).length,
    needsDecision: quiet.filter((i) => has(i, 'needs-decision')).length,
    items: quiet.map((i) => ({
      number: i.number, title: i.title,
      idleDays: Math.floor((now - (ms(i.updated_at) ?? now)) / DAY),
      quickWin: has(i, 'quick-win'),
      needsDecision: has(i, 'needs-decision'),
    })).sort((a, b) => b.idleDays - a.idleDays),
  };
}

// --- the whole board --------------------------------------------------------------------

export function buildBoard({ rows = [], items = [], prs = [], now, schedule = null } = {}) {
  const axis = axisOf(now, schedule);
  const open = items.filter((i) => i.state === 'open');
  const byNumber = new Map(items.map((i) => [i.number, i]));
  const moversByNumber = new Map(prs.filter((p) => p.closesIssue).map((p) => [p.closesIssue, { kind: 'pr', number: p.number }]));
  const declarationFor = new Map(rows.map((r) => [r.key, { ...r.declaration, key: r.key }]));
  const edges = edgesOf(open, prs);

  // NOW — every open PR, wired to what it closes.
  const nowRows = prs.filter((p) => !p.merged_at).map((pr) => {
    const item = pr.closesIssue ? byNumber.get(pr.closesIssue) : null;
    const parsed = item ? parseWorkItemTitle(item.title) : null;
    const declaration = parsed ? declarationFor.get(`${parsed.pack}/${parsed.task}`) : null;
    const flag = prWaits(pr, { item, declaration });
    return {
      id: `pr:${pr.number}`,
      kind: 'pr',
      gutter: `#${pr.number}${pr.closesIssue ? ` → #${pr.closesIssue}` : ''}`,
      title: pr.title,
      openedAt: ms(pr.created_at),
      waits: flag.waits,
      why: flag.why,
      notRead: flag.notRead ?? null,
      marks: [{ kind: 'bar', from: ms(pr.created_at), to: now, flag: flag.waits }],
    };
  }).sort((a, b) => (b.waits ? 1 : 0) - (a.waits ? 1 : 0) || a.openedAt - b.openedAt);

  // FLOWS — the items somebody asked for, and the plain issues that block them, at
  // their predicted time.
  const askedFor = askedForOrigins();
  const wasAsked = (i) => labelNames(i).some((n) => askedFor.includes(n));
  const asked = open.filter((i) => isQueueItem(i) && wasAsked(i));
  const parkedOrRunning = open.filter((i) => isQueueItem(i) && !wasAsked(i)
    && (statusOf(i)?.startsWith(PARK_PREFIX) || [STATUS_RUNNING_AGENT, STATUS_RUNNING_EXECUTOR].includes(statusOf(i))));
  const flowItems = [...asked, ...parkedOrRunning];
  const flowRows = flowItems.map((item) => {
    const place = placeItem(item, { now, axis, byNumber, moversByNumber });
    const status = statusOf(item);
    const parkKind = status?.startsWith(PARK_PREFIX) ? status.slice(PARK_PREFIX.length) : null;
    return {
      id: `item:${item.number}`,
      kind: 'item',
      gutter: `#${item.number}`,
      title: item.title,
      at: place.at,
      why: place.why,
      place: place.kind,
      broken: Boolean(place.broken),
      blocker: place.blocker ?? null,
      flagged: Boolean(place.flagged),
      parkKind: parkKind && PARK_KINDS.includes(parkKind) ? parkKind : (parkKind ? 'failure' : null),
      marks: [markFor(place, parkKind, now)],
      finding: place.broken || place.flagged ? place.why : null,
    };
  }).sort((a, b) => (b.broken ? 1 : 0) - (a.broken ? 1 : 0)
    || (a.at ?? Infinity) - (b.at ?? Infinity)
    || a.gutter.localeCompare(b.gutter));

  const grid = scheduleGrid(rows, items, axis, { now, schedule });
  const quiet = quietTail(items.filter((i) => i.state === 'open'), edges, now);
  const waiting = nowRows.filter((r) => r.waits).length;

  const capped = (list) => ({ shown: list.slice(0, GROUP_CAP), more: Math.max(0, list.length - GROUP_CAP), all: list });

  return {
    axis,
    groups: [
      { id: 'now', title: 'Now', count: nowRows.length, sentence: workloadLine(waiting, rows, { schedule, now }), ...capped(nowRows) },
      {
        id: 'flows', title: 'Flows', count: flowRows.length,
        sentence: flowRows.length
          ? `blocked, parked, waiting — placed at their predicted run time${flowRows.some((r) => r.broken) ? ' · a broken lane has none' : ''}`
          : 'nothing is blocked, parked or waiting',
        ...capped(flowRows),
      },
      {
        id: 'scheduled', title: 'Scheduled', count: grid.length,
        sentence: `the daily anchor is ${String(axis.dailyHour).padStart(2, '0')}:00 UTC`,
        shown: grid, more: 0, all: grid, grid: true,
      },
    ],
    quiet,
    edges,
  };
}

// One mark, from a placement. Form before colour: a park's kind is its glyph, a
// running item is filled, a queued one is hollow, and an item on no mark is dashed.
function markFor(place, parkKind, now) {
  if (place.kind === 'park') return { kind: 'park', at: now, park: parkKind?.replace(`${PARK_PREFIX}`, '') ?? 'failure' };
  if (place.kind === 'running') return { kind: 'running', at: now };
  if (place.at === null) return { kind: 'unmarked', at: null };
  return { kind: 'queued', at: place.at, broken: Boolean(place.broken) };
}
