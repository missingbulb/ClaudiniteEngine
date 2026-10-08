// The repo page's top block, as data — the same ledger as the fleet's, scoped to one
// member ([docs/repo-page.md](../../docs/repo-page.md)).
//
// WHY IT IS A SEPARATE MODULE and not a parameter on the fleet's. Three of its figures
// are questions only a repo can ask: the queue's own outcome WORDS (an obsolete share
// is a fact about one repo's requests), this repo's corpus weight against the fleet
// mean, and the per-task expand. And its machine asks a different question one level
// down — not *is every member running* but *did THIS scheduler run when it should have,
// hour by hour* — which is a different cell, not a smaller one.
//
// Everything it shares with the fleet block it IMPORTS rather than re-derives, so the
// two pages cannot disagree about what a window is or when a figure is unknown.

import {
  fleetDays, windowsOf, mergedPrsIn, stuckItems, closedItems, figure, quantile,
  sumKnown, pulseOf, pricingNote, WINDOW_DAYS, LADDER_DAYS, STUCK_DAYS, fmtAge,
} from './fleet-ledger.mjs';
import { priceWindow } from './pricing.mjs';

// The queue's own outcome words, in the order the fold's own vocabulary spells them.
// Spelled here rather than imported from the fold: the page reads the file off its
// `fields` header and imports nothing from the writer, which is the property that lets
// a member on an older fold still render.
const QUEUE_OUTCOMES = Object.freeze(['done', 'delivered', 'obsolete', 'none']);

const DAY = 86400e3;
const ms = (t) => (t == null ? null : new Date(t).getTime());
const finite = (n) => (typeof n === 'number' && Number.isFinite(n) ? n : null);

export { STUCK_DAYS };

// --- the queue's own outcome words -------------------------------------------------

// What the queue closed in the window, split by the word each item closed wearing.
//
// THE WORDS ARE THE FIGURE, not the total. `obsolete` rising means requests are being
// filed that the queue then retires; `none` above zero means items closed without
// converging at all — `converge-item` was bypassed — and neither is visible in a count
// of closes.
export function queueCloses(days) {
  const out = Object.fromEntries(QUEUE_OUTCOMES.map((o) => [o, 0]));
  let seen = false;
  for (const row of days) {
    for (const counts of Object.values(row.queue ?? {})) {
      seen = true;
      for (const word of QUEUE_OUTCOMES) out[word] += counts[word] ?? 0;
    }
  }
  return seen ? out : null;
}

const totalCloses = (closes) => (closes === null ? null : QUEUE_OUTCOMES.reduce((n, w) => n + closes[w], 0));

// --- the per-task expand ------------------------------------------------------------

// One row per task with a queue history or a session in the window. This is the
// expansion only a repo page can offer, and the three rows beside the tasks are states
// rather than noise: `(none)` is the human-driven share, `(unresolved)` is a hole in
// the record, and an agentless task shows what it is rather than dashes with no reason.
export function perTask(days, rows, rates) {
  const tasks = new Map();
  const at = (key) => {
    if (!tasks.has(key)) {
      tasks.set(key, {
        key, closed: Object.fromEntries(QUEUE_OUTCOMES.map((o) => [o, 0])), closes: 0,
        sessions: null, tokensIn: null, tokensOut: null, userMessages: null,
        execFailed: 0, parks: 0, byModel: {},
      });
    }
    return tasks.get(key);
  };
  const add = (row, field, n) => { if (finite(n) !== null) row[field] = (row[field] ?? 0) + n; };

  for (const day of days) {
    for (const [key, counts] of Object.entries(day.queueByTask ?? {})) {
      const row = at(key);
      for (const word of QUEUE_OUTCOMES) { row.closed[word] += counts[word] ?? 0; row.closes += counts[word] ?? 0; }
    }
    for (const [key, cost] of Object.entries(day.taskCost ?? {})) {
      const row = at(key);
      add(row, 'sessions', cost.sessions);
      add(row, 'tokensIn', cost.tokensIn);
      add(row, 'tokensOut', cost.tokensOut);
      add(row, 'userMessages', cost.userMessages);
    }
    for (const [key, counts] of Object.entries(day.taskExecByTask ?? {})) {
      at(key).execFailed += (counts.failed ?? 0) + (counts.invalid ?? 0) + (counts['task-gone'] ?? 0);
    }
    for (const [key, kinds] of Object.entries(day.parksByTask ?? {})) {
      at(key).parks += Object.values(kinds).reduce((n, v) => n + (v ?? 0), 0);
    }
  }

  const declared = new Map((rows ?? []).map((r) => [r.key, r.declaration]));
  return [...tasks.values()].map((row) => {
    const declaration = declared.get(row.key) ?? null;
    // A task's spend cannot be priced without the per-model split, which is a fold
    // field and not a per-task one — so the cost column is the token count, and the
    // dollar column says why it is absent rather than dividing a total by a guess.
    return {
      ...row,
      model: declaration ? (declaration.agent_model ?? 'none · code-work') : (row.key.startsWith('(') ? null : 'not declared'),
      // The figure that turns "tokens down 48%" into "which task to move to a cheaper
      // model". Null rather than zero where the task closed nothing.
      tokensPerClose: row.closes && finite(row.tokensIn) !== null ? Math.round(row.tokensIn / row.closes) : null,
      obsoleteShare: row.closes ? (row.closed.obsolete + row.closed.none) / row.closes : null,
      priced: rates ? null : 'unpriced',
    };
  }).sort((a, b) => (b.tokensIn ?? -1) - (a.tokensIn ?? -1) || a.key.localeCompare(b.key));
}

// --- the machine, one level down -----------------------------------------------------

// Twenty-four hour squares, one per hour, filled where a scheduler run completed in it.
// An hour NEITHER source reached is drawn hollow, not red: the fold's watermark and the
// live listing's depth are both finite, and an unread hour is not a missed run.
export function schedulerHours(hourRows, { now, hours = 24 }) {
  const out = [];
  for (let i = hours - 1; i >= 0; i -= 1) {
    const at = now - i * 3600e3;
    const key = new Date(at).toISOString().slice(0, 13);
    const row = hourRows?.find((h) => h.hour === key) ?? null;
    const ran = finite(row?.scheduler);
    out.push({
      hour: key,
      state: row === null || row.source === 'none' ? 'unread' : (ran > 0 ? 'ran' : 'idle'),
      runs: ran,
      title: `${key.slice(11)}:00 — ${row === null || row.source === 'none' ? 'not read' : (ran > 0 ? `${ran} scheduler run(s)` : 'no scheduler run')}`,
    });
  }
  return out;
}

// The longest run of hours with no scheduler run in them, which is the figure the cell
// reports — an average of 24 hours would hide a six-hour hole entirely.
export function longestGap(squares) {
  let worst = { hours: 0, from: null };
  let run = 0;
  let start = null;
  for (const sq of squares) {
    if (sq.state === 'ran') { run = 0; start = null; continue; }
    if (sq.state === 'unread') continue;                  // unread is not a missed run
    if (run === 0) start = sq.hour;
    run += 1;
    if (run > worst.hours) worst = { hours: run, from: start };
  }
  return worst;
}

export function repoMachine({ hourRows, runSummary, ci, usage, strip, declaredTasks, now }) {
  const squares = schedulerHours(hourRows, { now });
  const gap = longestGap(squares);
  const ranAtAll = squares.some((s) => s.state === 'ran');
  const scheduler = {
    level: !ranAtAll && declaredTasks > 0 ? 'critical' : (gap.hours > 6 ? 'serious' : gap.hours > 2 ? 'you' : 'good'),
    squares,
    lastAt: runSummary?.lastAt ?? null,
    note: !ranAtAll
      ? (declaredTasks > 0 ? 'no scheduler run in 24 h on a repo declaring tasks' : 'no scheduler run in 24 h')
      : `${squares.filter((s) => s.state === 'ran').length} of 24 h${gap.hours > 1 ? ` · ${gap.hours} h gap at ${gap.from?.slice(11)}:00` : ''}`,
  };

  const failed = (hourRows ?? []).reduce((n, h) => n + (finite(h.failed) ?? 0), 0);
  const executorRuns = (hourRows ?? []).reduce((n, h) => n + (finite(h.executor) ?? 0), 0);
  const executor = {
    level: failed >= 1 ? 'you' : 'good',
    failed, runs: executorRuns, inFlight: runSummary?.inFlight ?? 0,
    note: runSummary?.inFlight ? `${runSummary.inFlight} in flight` : (failed ? 'a failed run is a dispatch that broke' : 'none failed'),
  };

  const generated = ms(usage?.generated);
  const foldAge = {
    level: generated === null ? 'none' : ((now - generated) > 6 * 3600e3 ? 'you' : 'good'),
    age: generated === null ? null : now - generated,
    note: generated === null ? 'no fold — every figure below it reads not recorded' : `stamped ${new Date(generated).toISOString().slice(11, 16)} UTC`,
  };

  const nextHour = strip?.hours?.find((h) => h.tasks.length) ?? null;
  const held = strip?.hours?.some((h) => h.held) ?? false;
  const wake = {
    level: strip && !strip.peak && declaredTasks > 0 ? 'serious' : (held ? 'critical' : 'good'),
    at: nextHour?.hour ?? null,
    tasks: nextHour?.tasks.length ?? 0,
    inMs: nextHour ? Date.parse(`${nextHour.hour}:00:00Z`) - now : null,
    note: strip
      ? (strip.peak ? `${strip.hours.reduce((n, h) => n + h.tasks.length, 0)} wakes in 24 h${held ? ' · a held task has no next' : ''}` : 'nothing wakes in the next 24 h')
      : 'not read — no roster',
  };

  return { scheduler, executor, ci, foldAge, wake };
}

// --- the block ------------------------------------------------------------------------

export function repoLedger(read, { now, rates = null, windowDays = WINDOW_DAYS, days = LADDER_DAYS } = {}) {
  const folding = read?.usage ? [read] : [];
  const rows = fleetDays(folding, { now, days }).map((row) => decorate(row, read));
  const w = windowsOf(rows, { now, windowDays });
  const sum = (slice, field) => sumKnown(slice.map((r) => r[field]));
  const cur = (field) => sum(w.current, field);
  const prev = (field) => sum(w.previous, field);

  const merged = mergedPrsIn([read], w.current);
  const mergedPrev = mergedPrsIn([read], w.previous);
  const closes = queueCloses(w.current);
  const closesPrev = queueCloses(w.previous);
  const closed = closedItems([read], w.from, w.to);
  const stuck = stuckItems([read], now);
  const priced = priceWindow(w.current.flatMap((r) => r.tokensByModel), rates);
  const pricedPrev = priceWindow(w.previous.flatMap((r) => r.tokensByModel), rates);

  const leads = (list) => list.map((p) => p.issueLeadHours).filter((n) => finite(n) !== null);
  const issueLead = leads(merged);
  const issueLeadPrev = leads(mergedPrev);
  const sessionLead = merged.map((p) => p.sessionToMergeHours).filter((n) => finite(n) !== null);

  const got = [
    figure(merged.length, mergedPrev.length, {
      unit: 'merged PRs',
      sub: `${closed.unattended} with nobody in the loop`,
      spark: rows.map((r) => ({ day: r.day, value: merged.concat(mergedPrev).filter((p) => p.day === r.day).length })),
      bad: mergedPrev.length > 0 && merged.length < mergedPrev.length * 0.7
        && (cur('sessions') ?? 0) >= (prev('sessions') ?? 0) * 0.9,
    }),
    figure(totalCloses(closes), totalCloses(closesPrev), {
      unit: 'queue closes',
      sub: closes ? `${closes.done + closes.delivered} done · ${closes.obsolete} obsolete · ${closes.none} no outcome` : null,
      spark: rows.map((r) => ({ day: r.day, value: totalCloses(queueCloses([r])) })),
      // `none` above zero means items closed without converging at all, which is the
      // one closing state nobody chose.
      bad: Boolean(closes && (closes.none > 0
        || (closes.obsolete / Math.max(1, totalCloses(closes)) > (closesPrev ? closesPrev.obsolete / Math.max(1, totalCloses(closesPrev)) : 0) * 1.5))),
      gap: 'not recorded — this fold carries no queue rows yet',
    }),
    figure(cur('caught'), prev('caught'), {
      unit: 'caught before merge',
      sub: `of ${cur('checkRuns') ?? 0} Stop-hook runs`,
      spark: rows.map((r) => ({ day: r.day, value: r.caught })),
      bad: cur('caught') === 0 && (cur('checkRuns') ?? 0) > 0,
    }),
  ];

  const cost = [
    figure(cur('tokensIn'), prev('tokensIn'), {
      unit: 'tokens in',
      sub: `${cur('tokenSessions') ?? 0} of ${cur('sessions') ?? 0} sessions recorded`,
      spark: rows.map((r) => ({ day: r.day, value: r.tokensIn })),
      bad: (cur('tokensIn') ?? 0) > (prev('tokensIn') ?? 0) && merged.length < mergedPrev.length,
    }),
    figure(priced.usd === null ? null : Math.round(priced.usd), pricedPrev.usd === null ? null : Math.round(pricedPrev.usd), {
      unit: priced.ratesSet ? 'your rate table' : 'dollars',
      sub: pricingNote(priced),
      gap: priced.recorded ? 'unpriced — no model here has a rate' : 'not recorded — this fold predates tokensByModel',
    }),
  ];

  const closesPerDay = closes === null ? null : Math.round((totalCloses(closes) / Math.max(1, w.current.length)) * 10) / 10;
  const speed = [
    figure(quantile(issueLead, 0.5), quantile(issueLeadPrev, 0.5), {
      unit: 'issue → merged',
      sub: `median · p90 ${quantile(issueLead, 0.9) === null ? 'not recorded' : `${quantile(issueLead, 0.9)}h`}`,
      spark: null,
      bad: worseBy(quantile(issueLead, 0.5), quantile(issueLeadPrev, 0.5), 1.5),
      gap: 'not recorded — no merged PR named a closing issue',
    }),
    figure(closesPerDay, null, {
      unit: 'closes / day',
      sub: closes ? `${peakCloses(w.current)} · ${w.current.filter((r) => !totalCloses(queueCloses([r]))).length} days with none` : null,
      spark: null,
      gap: 'not recorded — this fold carries no queue rows yet',
    }),
    figure(stuck.total, null, {
      unit: `stuck ${STUCK_DAYS} d+`,
      sub: `${stuck.forYou.length} for you${stuck.forYou[0] ? ` · #${stuck.forYou[0].number}` : ''} · ${stuck.onMachine.length} on the machine`,
      spark: null,
      bad: stuck.forYou.length > 0,
    }),
  ];

  return {
    window: { ...w, windowDays, folded: folding.length > 0 },
    days: rows,
    ledger: { got, cost, speed },
    // The tail lines: one fact each that is nowhere else on the block, in the muted
    // step under its column rather than spending a whole row.
    tails: {
      got: `${cur('releases') ?? 0} release${cur('releases') === 1 ? '' : 's'} · lines, net: ${linesNet(w.current) ?? 'not recorded — shallow checkout'}`,
      cost: `${cur('userMessages') ?? 0} of your turns${cur('humanSeconds') === null ? ' · your minutes not recorded' : ''}`,
      speed: sessionLead.length
        ? `session → merged p50 ${quantile(sessionLead, 0.5)}h · p90 ${quantile(sessionLead, 0.9)}h — CI, then you`
        : 'session → merged: not recorded — this fold predates prs',
    },
    totals: {
      costPerMerged: priced.usd === null || !merged.length ? null : Math.round((priced.usd / merged.length) * 10) / 10,
      tokensPerMerged: cur('tokensIn') === null || !merged.length ? null : cur('tokensIn') / merged.length,
      autonomy: closed.completed ? closed.unattended / closed.completed : null,
      humanToAgent: cur('humanSeconds') && cur('agentSeconds') ? Math.round(cur('agentSeconds') / cur('humanSeconds')) : null,
      caught: cur('caught'),
    },
    perTask: perTask(w.current, null, rates),
    merged,
    stuck,
    pricing: priced,
    pulse: pulseOf(rows, w),
  };
}

// The per-task sub-maps the fleet's day rows do not carry, because the fleet sums them
// away: on one repo the task is the axis, so they are kept per key.
function decorate(row, read) {
  const source = read?.usage?.days?.[row.day] ?? null;
  return {
    ...row,
    queue: source?.queue ? { one: mergeCounts(source.queue) } : null,
    queueByTask: source?.queue ?? {},
    taskCost: source?.taskCost ?? {},
    taskExecByTask: source?.taskExec ?? {},
    parksByTask: source?.parks ?? {},
  };
}

const mergeCounts = (byTask) => {
  const out = {};
  for (const counts of Object.values(byTask)) {
    for (const [k, v] of Object.entries(counts)) out[k] = (out[k] ?? 0) + (v ?? 0);
  }
  return out;
};

const worseBy = (now, before, factor) =>
  finite(now) !== null && finite(before) !== null && before > 0 && now > before * factor;

const linesNet = (slice) => {
  const added = sumKnown(slice.map((r) => r.linesAdded));
  const removed = sumKnown(slice.map((r) => r.linesRemoved));
  return added === null || removed === null ? null : (added - removed).toLocaleString('en-US');
};

const peakCloses = (slice) => {
  const best = slice.map((r) => ({ day: r.day, n: totalCloses(queueCloses([r])) ?? 0 })).sort((a, b) => b.n - a.n)[0];
  return best && best.n ? `peak ${best.day.slice(5)}` : 'no peak';
};

// A name still exported is not a removal: a member's local pack may import the mount
// by this name, and the canon cannot see whether one does. Always `null` — the fold
// no longer writes the per-pack split it summarised.
// @legacy-tolerance advisory:none retire:#1989
export const heaviestPack = () => null;

export { fmtAge };
