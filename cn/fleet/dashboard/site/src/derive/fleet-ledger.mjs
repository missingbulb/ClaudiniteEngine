// The fleet page's top block, as data. Everything above the members grid — Start
// here, The machine, the Got/Cost/Speed ledger, the totals and the pulse — reduced to
// one object the view renders without deciding anything.
//
// WHY IT IS A MODULE AND NOT THE VIEW. Every figure on that block is a window against
// the previous window, and every one of them can be UNKNOWN: a member that does not
// fold, a fold that predates a field, a rate table nobody set. Those three states —
// a number, *not recorded*, and (for money) *unpriced* — have to be decided once,
// where they can be tested, rather than in the branch of a template.
//
// NOTHING HERE FETCHES. It reduces over the reads the sweep already made: each
// member's decoded fold, its issues and PRs, its declaration. The request budget is
// unchanged by every figure below.

import { priceWindow, RATES_KEY } from './pricing.mjs';
import { DAY_MS, dayKey, dayLadder } from './activity.mjs';
import { STALE_FRESHNESS } from './fleet.mjs';
import {
  isQueueItem, isParked, outcomeOf,
} from '../read/queue-vocabulary.mjs';

// The block's own window. Seven days against the seven before them, over a 14-day
// ladder — which is also the sparklines' and the pulse's span, so a figure, its delta
// and its shape all read the same stretch of days.
export const WINDOW_DAYS = 7;
export const LADDER_DAYS = 14;

// How long an open item sits before it is STUCK. Three days is the spec's own bound,
// and it is the figure that feeds Start here.
export const STUCK_DAYS = 3;

const ms = (t) => (t == null ? null : new Date(t).getTime());
const finite = (n) => (typeof n === 'number' && Number.isFinite(n) ? n : null);

// Sum a field across whatever knew it, or `null`. NEVER 0 for "nobody answered": the
// whole point of the fold's absence rule is lost the moment a page adds up an empty
// list and prints the total.
export function sumKnown(values) {
  const known = (values ?? []).map(finite).filter((n) => n !== null);
  return known.length ? known.reduce((a, b) => a + b, 0) : null;
}

// A quantile over a sample, by nearest rank. `null` on an empty sample — a median of
// nothing is not zero.
export function quantile(values, p) {
  const sorted = (values ?? []).map(finite).filter((n) => n !== null).sort((a, b) => a - b);
  if (!sorted.length) return null;
  const rank = Math.min(sorted.length - 1, Math.max(0, Math.ceil(p * sorted.length) - 1));
  return sorted[rank];
}

// --- the day ladder ----------------------------------------------------------------

// One row per day across the whole fleet: every scalar the ledger reads, summed over
// the members that knew it, plus the two sub-maps the pricing and the lead times need.
// A day nobody folded carries `null` everywhere and `folded: false`, which is what
// draws a blank column rather than a floor.
export function fleetDays(folding, { now, days = LADDER_DAYS } = {}) {
  const ladder = dayLadder(now, days);
  const SCALARS = [
    'sessions', 'userMessages', 'tokensIn', 'tokensOut', 'tokenSessions',
    'humanSeconds', 'agentSeconds',
    'commits', 'linesAdded', 'linesRemoved', 'releases',
  ];
  return ladder.map((day) => {
    const rows = folding.map((r) => r.usage?.days?.[day]).filter(Boolean);
    const out = { day, folded: rows.length > 0, members: rows.length };
    for (const field of SCALARS) out[field] = sumKnown(rows.map((row) => row[field]));
    // Both check scopes together, as `growthSeries` reads them: "how often did the
    // checks catch something today" is one question.
    const scopes = rows.flatMap((row) => Object.values(row.checks ?? {}));
    out.caught = scopes.length
      ? scopes.reduce((n, s) => n + (s.failures ?? 0) + (s.ciFailures ?? 0), 0) : null;
    out.checkRuns = scopes.length ? scopes.reduce((n, s) => n + (s.runs ?? 0), 0) : null;
    out.checkErrors = scopes.length ? scopes.reduce((n, s) => n + (s.errors ?? 0), 0) : null;
    // Kept as rows rather than summed: the pricing reduction wants the per-model split
    // and the lead times want one entry per PR, both across the whole window.
    out.tokensByModel = rows.map((row) => ({ tokensByModel: row.tokensByModel }));
    out.prs = rows.flatMap((row) => Object.entries(row.prs ?? {}));
    // Which member released, so the releases figure can name them rather than count.
    out.releasedBy = folding
      .filter((r) => finite(r.usage?.days?.[day]?.releases) > 0)
      .map((r) => r.repo);
    // …and which moved at all, for the pulse's hover.
    out.movedBy = folding.filter((r) => finite(r.usage?.days?.[day]?.sessions) > 0).map((r) => r.repo);
    return out;
  });
}

// The two windows the block compares, as day-row slices. `current` ends today
// inclusive; `previous` is the seven days before it.
export function windowsOf(rows, { now, windowDays = WINDOW_DAYS } = {}) {
  const from = dayKey(now - (windowDays - 1) * DAY_MS);
  const prevFrom = dayKey(now - (2 * windowDays - 1) * DAY_MS);
  return {
    current: rows.filter((r) => r.day >= from),
    previous: rows.filter((r) => r.day >= prevFrom && r.day < from),
    from,
    to: dayKey(now),
    prevFrom,
    prevTo: dayKey(now - windowDays * DAY_MS),
  };
}

// --- merged pull requests ----------------------------------------------------------

// Every PR merged inside the window, from the two sources that reach different depths
// and DEDUPED on its number, since a PR merged in the last day or two is in both.
//
// The live listing carries the merge date and the issue a PR closes but no lead times;
// the fold carries the lead times the reader needs and reaches further back. So a PR
// seen live is joined to the issue in the same read where one is there — which is what
// makes `issue → merged` answerable for the days the fold has not folded yet.
export function mergedPrsIn(reads, dayRows, { repoOf = (r) => r.repo } = {}) {
  const seen = new Map();
  const from = dayRows[0]?.day ?? null;
  const to = dayRows[dayRows.length - 1]?.day ?? null;

  for (const read of reads) {
    const issues = new Map((read.items ?? []).map((i) => [i.number, i]));
    for (const pr of read.prs ?? []) {
      const merged = pr.merged_at ?? null;
      if (!merged) continue;
      const day = merged.slice(0, 10);
      if (from && (day < from || day > to)) continue;
      const issue = pr.closesIssue ? issues.get(pr.closesIssue) : null;
      seen.set(`${repoOf(read)}#${pr.number}`, {
        repo: repoOf(read),
        number: pr.number,
        day,
        source: 'live',
        leadHours: hoursBetweenIso(pr.created_at, merged),
        issueLeadHours: issue ? hoursBetweenIso(issue.created_at, merged) : null,
        sessionToMergeHours: null,
      });
    }
  }

  for (const row of dayRows) {
    for (const [number, lead] of row.prs) {
      const key = `${row.day}#${number}`;
      // The fold's row is the fuller record — it carries the session lead time nothing
      // live can answer — so it wins wherever both sources have the same PR. Members
      // are not distinguishable in a fold row's key, so the day stands in for the repo;
      // a collision would need two members to merge the same PR number on one day, and
      // it costs one row of a sample rather than a figure.
      const live = [...seen.values()].find((p) => String(p.number) === String(number) && p.day === row.day);
      if (live) { Object.assign(live, { ...lead, source: 'fold' }); continue; }
      seen.set(key, { repo: null, number, day: row.day, source: 'fold', ...lead });
    }
  }
  return [...seen.values()];
}

const hoursBetweenIso = (from, to) => {
  const a = ms(from);
  const b = ms(to);
  if (a === null || b === null || !Number.isFinite(a) || !Number.isFinite(b) || b < a) return null;
  return Math.round(((b - a) / 3600e3) * 10) / 10;
};

// --- items nobody has moved --------------------------------------------------------

// Open work items untouched for three days, split by WHO clears them. The split is the
// point: a person clears a park, and the janitor's leash clears the rest, so only the
// first half is a claim on the reader's morning.
export function stuckItems(reads, now, { days = STUCK_DAYS } = {}) {
  const cutoff = now - days * DAY_MS;
  const forYou = [];
  const onMachine = [];
  for (const read of reads) {
    for (const item of read.items ?? []) {
      if (item.state !== 'open' || !isQueueItem(item)) continue;
      const touched = ms(item.updated_at) ?? ms(item.created_at);
      if (touched === null || touched > cutoff) continue;
      (isParked(item) ? forYou : onMachine).push({ repo: read.repo, number: item.number, title: item.title });
    }
  }
  return { forYou, onMachine, total: forYou.length + onMachine.length };
}

// --- work items closed with nobody in the loop -------------------------------------

// The queue's own half of the autonomy figure: items that closed inside the window
// having never been parked. `outcomeOf` decides what closed, `isParked` what still
// wants a person — an item CLOSED while parked is one a person had to touch.
export function closedItems(reads, from, to) {
  let completed = 0;
  let unattended = 0;
  for (const read of reads) {
    for (const item of read.items ?? []) {
      if (item.state !== 'closed' || !isQueueItem(item)) continue;
      const day = (item.closed_at ?? '').slice(0, 10);
      if (!day || day < from || day > to) continue;
      const outcome = outcomeOf(item);
      if (outcome !== 'done' && outcome !== 'delivered') continue;
      completed += 1;
      if (!isParked(item)) unattended += 1;
    }
  }
  return { completed, unattended };
}

// --- the block ---------------------------------------------------------------------

// A figure, in the one shape the ledger renders: a value that may be null, the delta
// against the previous window, and whether that delta's own *bad when* rule fired.
//
// `bad` is passed in per figure rather than derived, because it is a JUDGEMENT stated
// in the page's spec — merged down while sessions are flat, stuck items rising — and
// the module that computes a number is not the one that decides it is worrying.
export function figure(value, previous, { unit, sub = null, spark = null, bad = false, gap = null } = {}) {
  return {
    value,
    previous: finite(previous),
    delta: value === null || finite(previous) === null ? null : value - previous,
    unit,
    sub,
    spark,
    bad: Boolean(bad) && value !== null,
    // What to say INSTEAD of the number when there is none. A gap is a sentence, never
    // a dash on its own: *not recorded* is information, an em dash is a puzzle.
    gap: value === null ? (gap ?? 'not recorded') : null,
  };
}

// A 14-day series for one field, as the sparkline's two halves.
const sparkOf = (rows, field) => rows.map((r) => ({ day: r.day, value: finite(r[field]) }));

export function fleetLedger(reads, { now, rates = null, windowDays = WINDOW_DAYS, days = LADDER_DAYS } = {}) {
  const readable = (reads ?? []).filter((r) => r && !r.error && r.declaration);
  const folding = readable.filter((r) => r.usage);
  const rows = fleetDays(folding, { now, days });
  const w = windowsOf(rows, { now, windowDays });

  const sum = (slice, field) => sumKnown(slice.map((r) => r[field]));
  const cur = (field) => sum(w.current, field);
  const prev = (field) => sum(w.previous, field);

  const merged = mergedPrsIn(readable, w.current);
  const mergedPrev = mergedPrsIn(readable, w.previous);
  const closed = closedItems(readable, w.from, w.to);
  const closedPrev = closedItems(readable, w.prevFrom, w.prevTo);
  const stuck = stuckItems(readable, now);

  const priced = priceWindow(w.current.flatMap((r) => r.tokensByModel), rates);
  const pricedPrev = priceWindow(w.previous.flatMap((r) => r.tokensByModel), rates);

  const leads = (list, field) => list.map((p) => p[field]).filter((n) => finite(n) !== null);
  const issueLead = leads(merged, 'issueLeadHours');
  const issueLeadPrev = leads(mergedPrev, 'issueLeadHours');
  const sessionLead = leads(merged, 'sessionToMergeHours');
  const sessionLeadPrev = leads(mergedPrev, 'sessionToMergeHours');

  const linesNet = (slice) => {
    const added = sum(slice, 'linesAdded');
    const removed = sum(slice, 'linesRemoved');
    return added === null || removed === null ? null : added - removed;
  };
  const releasedBy = [...new Set(w.current.flatMap((r) => r.releasedBy))].sort();
  const daysWithNone = w.current.filter((r) => !merged.some((p) => p.day === r.day)).length;
  const peakDay = w.current
    .map((r) => ({ day: r.day, n: merged.filter((p) => p.day === r.day).length }))
    .sort((a, b) => b.n - a.n || a.day.localeCompare(b.day))[0] ?? null;

  const got = [
    figure(merged.length, mergedPrev.length, {
      unit: 'merged PRs',
      sub: `${closed.unattended} work items closed with nobody in the loop`,
      spark: sparkPairs(rows, (day) => merged.concat(mergedPrev).filter((p) => p.day === day).length),
      // Fewer merges while the sessions that produce them held up is something
      // stalling, which is the one reading of a down week that wants a person.
      bad: mergedPrev.length > 0 && merged.length < mergedPrev.length * 0.7
        && (cur('sessions') ?? 0) >= (prev('sessions') ?? 0) * 0.9,
    }),
    figure(cur('caught'), prev('caught'), {
      unit: 'caught before merge',
      sub: 'Stop hook blocked, agent fixed',
      spark: sparkOf(rows, 'caught'),
      // The win counter: bad only when it is ZERO while the checks were running, which
      // is enforcement silently off rather than a clean week.
      bad: cur('caught') === 0 && (cur('checkRuns') ?? 0) > 0,
    }),
    figure(cur('releases'), prev('releases'), {
      unit: 'releases',
      sub: releasedBy.length ? releasedBy.map(shortRepo).join(' · ') : 'none in the window',
      spark: sparkOf(rows, 'releases'),
    }),
    figure(linesNet(w.current), linesNet(w.previous), {
      unit: 'lines, net',
      sub: 'added − removed on the default branch',
      spark: null,
      gap: 'not recorded — no fold reaches the git history',
    }),
  ];

  const cost = [
    figure(cur('tokensIn'), prev('tokensIn'), {
      unit: 'tokens in',
      sub: `${folding.length} of ${readable.length} folding · cache reads count as in`,
      spark: sparkOf(rows, 'tokensIn'),
      bad: (cur('tokensIn') ?? 0) > (prev('tokensIn') ?? 0) && merged.length < mergedPrev.length,
    }),
    figure(priced.usd === null ? null : Math.round(priced.usd), pricedPrev.usd === null ? null : Math.round(pricedPrev.usd), {
      unit: priced.ratesSet ? 'your rate table' : 'dollars',
      sub: pricingNote(priced),
      gap: priced.recorded
        ? (priced.ratesSet ? 'unpriced — no model in the window has a rate' : `unpriced — this deployment carries no \`${RATES_KEY}\` table`)
        : 'not recorded — this fold predates tokensByModel',
    }),
    figure(cur('humanSeconds'), prev('humanSeconds'), {
      unit: 'yours',
      sub: `${fmtCount(cur('userMessages'))} turns · ${fmtCount(cur('sessions'))} sessions · gaps > 10m dropped`,
      spark: sparkOf(rows, 'humanSeconds'),
      bad: (cur('humanSeconds') ?? 0) > (prev('humanSeconds') ?? 0) && merged.length <= mergedPrev.length,
      gap: 'not recorded — this fold predates humanSeconds',
    }),
  ];

  const mergedPerDay = w.current.length ? Math.round((merged.length / w.current.length) * 10) / 10 : null;
  const mergedPerDayPrev = w.previous.length ? Math.round((mergedPrev.length / w.previous.length) * 10) / 10 : null;

  const speed = [
    figure(quantile(issueLead, 0.5), quantile(issueLeadPrev, 0.5), {
      unit: 'issue → merged',
      sub: `median · p90 ${fmtHours(quantile(issueLead, 0.9))}`,
      spark: null,
      // A lead time getting longer is a figure; half again as long, or a p90 past
      // three days, is the one a person acts on.
      bad: worseBy(quantile(issueLead, 0.5), quantile(issueLeadPrev, 0.5), 1.5) || (quantile(issueLead, 0.9) ?? 0) > 72,
      gap: 'not recorded — no merged PR named a closing issue',
    }),
    figure(quantile(sessionLead, 0.5), quantile(sessionLeadPrev, 0.5), {
      unit: 'session → merged',
      sub: `median · p90 ${fmtHours(quantile(sessionLead, 0.9))} · CI, then you`,
      spark: null,
      bad: (quantile(sessionLead, 0.5) ?? 0) > 4,
      gap: 'not recorded — this fold predates prs',
    }),
    figure(mergedPerDay, mergedPerDayPrev, {
      unit: 'merged / day',
      sub: peakDay && peakDay.n ? `peak ${peakDay.day} · ${daysWithNone} days with none` : `${daysWithNone} days with none`,
      spark: null,
    }),
    figure(stuck.total, null, {
      unit: `stuck ${STUCK_DAYS} d+`,
      sub: `${stuck.forYou.length} parked for you · ${stuck.onMachine.length} on the machine`,
      spark: null,
      // The one figure on the block whose bad-when is a level rather than a move: an
      // item parked for a person for three days is what feeds Start here.
      bad: stuck.forYou.length > 0,
    }),
  ];

  return {
    window: { ...w, windowDays, folding: folding.length, members: readable.length, absent: readable.filter((r) => !r.usage).map((r) => r.repo).sort() },
    // The grid's rows with the ledger's columns rather than the queue's — the same
    // window, per member, so a fleet figure can be traced to the member carrying it.
    perMember: folding.map((r) => memberWindow(r, w)),
    days: rows,
    ledger: { got, cost, speed },
    totals: totalsOf({ merged, closed, priced, cur, stuck, rows: w.current }),
    merged,
    stuck,
    pricing: priced,
    pulse: pulseOf(rows, w),
  };
}

// The three quotients under the double rule. Each is built from figures already on the
// block, so it introduces no source — and INHERITS BOTH GAPS: a numerator or
// denominator that is not recorded makes the quotient not recorded, rather than a
// confident number resting on a hole.
export function totalsOf({ merged, closed, priced, cur, stuck }) {
  const tokensIn = cur('tokensIn');
  const human = cur('humanSeconds');
  const agent = cur('agentSeconds');
  const per = (n, d) => (n === null || !d ? null : n / d);
  return {
    costPerMerged: priced.usd === null || !merged.length ? null : Math.round((priced.usd / merged.length) * 10) / 10,
    tokensPerMerged: per(tokensIn, merged.length),
    autonomy: closed.completed ? closed.unattended / closed.completed : null,
    humanToAgent: human === null || agent === null || !human ? null : Math.round(agent / human),
    caught: cur('caught'),
    stuckForYou: stuck.forYou.length,
  };
}

// The 14-day session series, split into the two windows the identity draws in two
// weights, with today marked as its own state: it is not folded yet, and a dashed
// outline says so where a zero would say the fleet stopped.
export function pulseOf(rows, w) {
  const today = w.to;
  const days = rows.map((r) => ({
    day: r.day,
    sessions: r.sessions,
    members: r.movedBy,
    series: r.day === today ? 'today' : (r.day >= w.from ? 'current' : 'previous'),
  }));
  const known = days.map((d) => d.sessions).filter((n) => n !== null);
  const peak = known.length ? Math.max(...known) : null;
  const quiet = days.filter((d) => d.sessions === 0).map((d) => d.day);
  return { days, peak, quiet };
}

// --- the sentences a figure carries -------------------------------------------------

const shortRepo = (repo) => String(repo ?? '').split('/')[1] ?? repo;

const worseBy = (now, before, factor) =>
  finite(now) !== null && finite(before) !== null && before > 0 && now > before * factor;

const fmtCount = (n) => (n === null ? 'no' : String(n));

export const fmtHours = (h) => {
  if (finite(h) === null) return 'not recorded';
  if (h < 1) return `${Math.round(h * 60)}m`;
  if (h < 48) return `${Math.round(h * 10) / 10}h`;
  return `${Math.round((h / 24) * 10) / 10} d`;
};

// The dollar figure's own assumption, inline where the identity asks for it: whose
// rates, how concentrated the spend is, and how much of it nothing could price.
export function pricingNote(priced) {
  if (!priced.recorded) return 'no fold in the window carries a per-model split';
  if (!priced.ratesSet) return `no ${RATES_KEY} table configured · ${fmtTokens(priced.tokens)} unpriced`;
  const parts = [];
  if (priced.top) parts.push(`${Math.round(priced.top.share * 100)}% ${shortModel(priced.top.model)}`);
  parts.push(priced.unpricedTokens
    ? `${fmtTokens(priced.unpricedTokens)} unpriced (${priced.unpricedModels.map(shortModel).join(', ')})`
    : '0 unpriced');
  return parts.join(' · ');
}

// A model id is a long word in a small step; the family is what a reader compares.
const shortModel = (id) => String(id ?? '').replace(/^claude-/, '').replace(/-\d{8}$/, '');

export function fmtTokens(n) {
  if (finite(n) === null) return 'not recorded';
  if (n >= 1e9) return `${Math.round((n / 1e9) * 10) / 10}B`;
  if (n >= 1e6) return `${Math.round((n / 1e6) * 10) / 10}M`;
  if (n >= 1e3) return `${Math.round((n / 1e3) * 10) / 10}k`;
  return String(n);
}

// Two 7-day halves of a per-day count the caller derives, in the sparkline's shape.
function sparkPairs(rows, countFor) {
  return rows.map((r) => ({ day: r.day, value: countFor(r.day) }));
}


// --- the machine --------------------------------------------------------------------

// The scheduler's declared cadence, which every heartbeat square is judged against.
// Hourly is what the stub workflow fires at, and it is stated here rather than read
// per member because the stub is the same file in every repo.
export const SCHEDULER_CADENCE_MS = 3600e3;

const level = (...verdicts) => ['critical', 'serious', 'you', 'machine', 'good'].find((l) => verdicts.includes(l)) ?? 'none';

// How many members can be stale before it is the FLEET that has stopped updating rather
// than a few stragglers: the square root of its size (owner, 2026-09-07). It scales the
// way an alarm should — three of thirteen is a bad night, eight of thirteen is
// Claudinite itself not landing anywhere, and the two must not read alike.
const fleetWideBound = (total) => Math.sqrt(Math.max(0, total ?? 0));

// Which stale member to name when only a few are: one nothing will ever converge
// before one the nightly update has merely not caught up with, since that is the one
// whose failure explains the rest.
const STALE_RANK = { 'no-stamp': 0, 'no-scheduler': 1, behind: 2 };

// The five cells, each already carrying its verdict and the one line naming the worst
// member — a name is what the reader acts on, where a count is something to go and
// look up. Except when the fault is the fleet's: past `fleetWideBound` the line names
// the count instead, because no single member is the thing to go and look at.
export function machinePanel(summaries, reads, { now, rostered = false, strip = null } = {}) {
  // THE MACHINE IS THE AWAKE FLEET. A dormant member declared its scheduler stopped
  // (owner, 2026-09-13), so it is out of every cell here: it has no heartbeat to be
  // late, nothing any update will converge, and no anchor that will fire. Counting one
  // in a denominator makes an obedient repo read as a missing member.
  const all = (summaries ?? []).filter((s) => s?.status === 'adopted');
  const adopted = all.filter((s) => !s.dormant);
  const dormant = all.filter((s) => s.dormant).map((s) => s.repo);

  // HEARTBEAT — one square per member, in the grid's own order.
  const beats = adopted.map((s) => {
    const last = s.runs?.lastAt ?? null;
    const age = last === null ? null : now - last;
    const verdict = !s.runs?.everRan ? 'critical' : (age > 2 * SCHEDULER_CADENCE_MS ? 'you' : 'good');
    return {
      repo: s.repo,
      age,
      level: verdict,
      title: `${shortRepo(s.repo)} — ${last === null ? 'never ran' : `${fmtAge(age)} ago${s.runs?.lastAtSource === 'folded' ? ', to the hour' : ''}`}`,
    };
  });
  const late = beats.filter((b) => b.level === 'you');
  const never = beats.filter((b) => b.level === 'critical');
  const heartbeat = {
    level: level(never.length ? 'critical' : null, late.length ? 'you' : null, 'good'),
    onTime: beats.length - late.length - never.length,
    total: beats.length,
    beats,
    note: [
      ...late.map((b) => `${shortRepo(b.repo)} ${fmtAge(b.age)} late`),
      ...never.map((b) => `${shortRepo(b.repo)} never`),
    ].join(' · ') || 'every member ran inside its cadence',
  };

  // EXECUTOR — failures over the last 24 hours, fleet-wide, worst member named.
  const since = hourKeysSince(now, 24);
  const perMember = adopted.map((s) => {
    const read = (reads ?? []).find((r) => r?.repo === s.repo);
    const hours = since.map((h) => read?.usage?.hours?.[h]).filter(Boolean);
    return {
      repo: s.repo,
      failed: hours.reduce((n, h) => n + (h.failed ?? 0), 0),
      runs: hours.reduce((n, h) => n + (h.executor ?? 0), 0),
    };
  });
  const failed = perMember.reduce((n, m) => n + m.failed, 0);
  const worstExec = [...perMember].sort((a, b) => b.failed - a.failed)[0] ?? null;
  const executor = {
    level: level(failed >= 3 ? 'serious' : null, failed > 0 ? 'you' : null, 'good'),
    failed,
    runs: perMember.reduce((n, m) => n + m.runs, 0),
    inFlight: adopted.reduce((n, s) => n + (s.runs?.inFlight ?? 0), 0),
    note: failed && worstExec?.failed ? `worst ${shortRepo(worstExec.repo)}` : 'none failed',
  };

  // FOLD AGE — the OLDEST member's stamp, since the block's every figure is only as
  // fresh as the stalest fold behind it.
  const stamps = (reads ?? [])
    .filter((r) => r?.usage?.generated)
    .map((r) => ({ repo: r.repo, age: now - (ms(r.usage.generated) ?? now) }));
  const oldest = [...stamps].sort((a, b) => b.age - a.age)[0] ?? null;
  const folding = (reads ?? []).filter((r) => r?.usage).length;
  const readable = (reads ?? []).filter((r) => r && !r.error && r.declaration).length;
  const foldAge = {
    level: oldest === null ? 'none' : (oldest.age > 6 * 3600e3 ? 'you' : 'good'),
    age: oldest?.age ?? null,
    note: `${folding} of ${readable} fold`,
    worst: oldest?.repo ?? null,
  };

  // UPDATES — the members Claudinite's own nightly update did not land on, and the
  // band's TOP SIGNAL (owner, 2026-09-07): a fleet whose members have stopped moving is
  // the whole machine failing, where a late scheduler or a failed executor run is one
  // member having a bad hour. Judged on the outcome — the manager's roster, which
  // measured each member against what its own update would move it to — whatever
  // stopped it. Unknown where the deployment publishes no roster, and unknown is SAID
  // rather than read as current.
  const stale = [...adopted.filter((s) => STALE_FRESHNESS.includes(s.freshness?.state))]
    .sort((a, b) => STALE_RANK[a.freshness.state] - STALE_RANK[b.freshness.state]);
  const fleetWide = rostered && stale.length > fleetWideBound(adopted.length);
  const updates = {
    level: rostered ? level(fleetWide ? 'critical' : null, stale.length ? 'you' : null, 'good') : 'none',
    stale: rostered ? stale.length : null,
    total: adopted.length,
    fleetWide,
    note: !rostered ? 'unknown — this deployment runs no fleet-roster'
      : fleetWide ? `${stale.length} of ${adopted.length} members are not at the published versions`
        : stale.length ? `${shortRepo(stale[0].repo)} worst` : 'every member current',
  };

  // NEXT WAKE — when the fleet next acts, and the 24-hour strip behind it.
  //
  // A NAMED GAP RATHER THAN AN EMPTY STRIP where no roster reached this page. An
  // anchor comes from a task's own declaration, and the fleet sweep reads each
  // member's task PATHS (that is what `declaredTasks` counts) but not their contents —
  // reading them would be one content request per task per member, which is the one
  // property this whole block is built not to spend. An empty strip would read as
  // "nothing wakes", which is the serious verdict; the absence has to say it is an
  // absence.
  const declaring = adopted.filter((s) => (s.declaredTasks ?? 0) > 0).length;
  const nextHour = strip?.hours?.find((h) => h.tasks.length) ?? null;
  const wake = strip === null
    ? {
      level: 'none', at: null, members: 0, tasks: 0, read: false,
      note: declaring
        ? `not read — ${declaring} member(s) declare tasks, their anchors are in declarations this page does not fetch`
        : 'not read — no roster reached this page',
    }
    : {
      level: !strip.peak && declaring ? 'serious' : (strip.hours.some((h) => h.held) ? 'critical' : 'good'),
      at: nextHour?.hour ?? null,
      members: nextHour ? new Set(nextHour.tasks.map((t) => t.repo).filter(Boolean)).size : 0,
      tasks: nextHour?.tasks.length ?? 0,
      read: true,
      note: !strip.peak
        ? (declaring ? `${declaring} member(s) declare tasks and nothing wakes in 24 h` : 'no task declares an anchor')
        : `${strip.hours.reduce((n, h) => n + h.tasks.length, 0)} wakes in 24 h`,
    };

  // Named rather than merely absent: the reader is told the panel is about the awake
  // fleet, and which members it left alone.
  return { updates, heartbeat, executor, foldAge, wake, dormant };
}

// The last N whole UTC hours, as the fold's own hour keys.
export function hourKeysSince(now, hours) {
  return Array.from({ length: hours }, (_, i) =>
    new Date(now - i * 3600e3).toISOString().slice(0, 13));
}

export function fmtAge(msVal) {
  if (finite(msVal) === null) return 'never';
  if (msVal < 60e3) return '<1m';
  if (msVal < 3600e3) return `${Math.round(msVal / 60e3)}m`;
  if (msVal < 86400e3) return `${Math.floor(msVal / 3600e3)}h ${Math.round((msVal % 3600e3) / 60e3)}m`;
  return `${Math.floor(msVal / 86400e3)}d`;
}


// One member's own share of the window. Every field is `null` where that member's fold
// could not answer, never 0: a member folding without token records is a different row
// from one that spent nothing.
export function memberWindow(read, w) {
  const rows = w.current.map((d) => read.usage?.days?.[d.day]).filter(Boolean);
  const sum = (field) => sumKnown(rows.map((row) => row[field]));
  const scopes = rows.flatMap((row) => Object.values(row.checks ?? {}));
  return {
    repo: read.repo,
    sessions: sum('sessions'),
    turns: sum('userMessages'),
    tokensIn: sum('tokensIn'),
    caught: scopes.length ? scopes.reduce((n, s) => n + (s.failures ?? 0) + (s.ciFailures ?? 0), 0) : null,
  };
}
