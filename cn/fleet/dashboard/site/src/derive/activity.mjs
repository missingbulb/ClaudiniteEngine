// What the fleet DID — the counterpart to every other derivation here, which answers
// where to look. Pure: no clock, no I/O, no DOM.
//
// The page's panels are all fault-finding, and fault-finding has a blind spot: after
// a good week every count is zero, so a healthy fleet and a dead one render
// identically. Nothing on the page can tell them apart, because "nothing is wrong" and
// "nothing happened" are the same picture. This module is the other reading — closed
// work, scheduler runs, and which members moved at all — over a window of days.
//
// EVERY NUMBER COMES FROM A READ THE PAGE ALREADY MAKES. A per-member call to get a
// commit count would undo the budget work the whole client is built around, so the
// inputs are exactly the fleet loader's existing per-member reads: the first page of
// issues, the recent Actions runs, and the head commit whose date arrives with the sha
// the content cache is keyed by. Nothing here adds a request.
//
// WHAT THAT COSTS IS DEPTH, AND DEPTH IS DECLARED. One issue page is the most recent
// hundred issues by creation, and one runs page is the most recent thirty runs — so a
// day at the far end of the window is under-read rather than empty, and every series
// carries the horizon past which it stops being a count and starts being a floor.

import { isParked, outcomeOf } from '../read/queue-vocabulary.mjs';
import { isSubstantiveCommit } from './substantive-commit.mjs';
import { isWorkItem } from './model.mjs';

export const DAY_MS = 86400e3;

const ms = (t) => (t == null ? null : new Date(t).getTime());

// A UTC day. The queue's anchors and GitHub's timestamps are
// all UTC, so a local-time bucketing would put a member's midnight run on the wrong
// day depending on who is looking.
export const dayKey = (t) => new Date(t).toISOString().slice(0, 10);

// The window's days, oldest first, including today. Built from keys rather than from
// arithmetic on a rendered date so a DST-free UTC ladder is the only thing anyone has
// to trust.
export function dayLadder(now, days) {
  const start = Math.floor(now / DAY_MS) * DAY_MS - (days - 1) * DAY_MS;
  return Array.from({ length: days }, (_, i) => dayKey(start + i * DAY_MS));
}

// --- the daily series -----------------------------------------------------------

// Work closed, runs finished and members moving, per day. `reads` is the fleet
// loader's raw per-member reads; a member that could not be read contributes nothing
// and is counted as unread rather than as a quiet day.
export function activitySeries(reads, { now, days = 14 } = {}) {
  const ladder = dayLadder(now, days);
  const index = new Map(ladder.map((d, i) => [d, i]));
  const blank = () => ladder.map((day) => ({
    day,
    work: { done: 0, delivered: 0, obsolete: 0, none: 0 },
    workClosed: 0,
    otherClosed: 0,
    runs: { success: 0, failure: 0, other: 0 },
    movers: new Set(),
  }));

  const series = blank();
  const readable = (reads ?? []).filter((r) => r && !r.error && r.declaration);
  // The oldest thing each read could see. A day before this is under-read for that
  // member — not quiet — and the series says so rather than drawing a confident zero.
  let issueHorizon = null;
  let runHorizon = null;
  const seen = (at, keep) => {
    const t = ms(at);
    if (t == null) return keep;
    return keep == null ? t : Math.max(keep, t);
  };

  for (const read of readable) {
    const items = read.items ?? [];
    let oldestIssue = null;
    for (const i of items) {
      const created = ms(i.created_at);
      if (created != null) oldestIssue = oldestIssue == null ? created : Math.min(oldestIssue, created);
      if (i.state !== 'closed') continue;
      const at = ms(i.closed_at) ?? ms(i.updated_at);
      const slot = at == null ? undefined : index.get(dayKey(at));
      if (slot === undefined) continue;
      series[slot].movers.add(read.repo);
      if (isWorkItem(i)) {
        series[slot].workClosed += 1;
        series[slot].work[outcomeOf(i) ?? 'none'] += 1;
      } else {
        series[slot].otherClosed += 1;
      }
    }
    // A member whose page-1 window does not reach the start of the ladder truncates
    // the series for everyone, so the horizon is the LATEST such floor across members.
    if (!read.itemsComplete && oldestIssue != null) issueHorizon = seen(oldestIssue, issueHorizon);

    let oldestRun = null;
    for (const r of read.runs ?? []) {
      const at = ms(r.created_at);
      if (at == null) continue;
      oldestRun = oldestRun == null ? at : Math.min(oldestRun, at);
      const slot = index.get(dayKey(at));
      if (slot === undefined) continue;
      series[slot].movers.add(read.repo);
      const bucket = r.status !== 'completed' ? 'other'
        : r.conclusion === 'success' ? 'success'
          : (r.conclusion === 'failure' || r.conclusion === 'timed_out' || r.conclusion === 'startup_failure') ? 'failure'
            : 'other';
      series[slot].runs[bucket] += 1;
    }
    if (oldestRun != null) runHorizon = seen(oldestRun, runHorizon);

    // The default branch's tip. One date, so it marks the day that member last
    // landed something — not a commit count, and never drawn as one.
    const tip = ms(read.head?.committedAt);
    const tipSlot = tip == null ? undefined : index.get(dayKey(tip));
    if (tipSlot !== undefined) series[tipSlot].movers.add(read.repo);
  }

  const rows = series.map((d) => ({ ...d, movers: [...d.movers].sort(), moved: d.movers.size }));
  const movedInWindow = new Set(rows.flatMap((d) => d.movers));

  return {
    days: rows,
    from: ladder[0],
    to: ladder[ladder.length - 1],
    members: readable.length,
    unread: (reads ?? []).filter((r) => r?.error).length,
    moved: [...movedInWindow].sort(),
    quiet: readable.map((r) => r.repo).filter((r) => !movedInWindow.has(r)).sort(),
    totals: {
      workClosed: rows.reduce((n, d) => n + d.workClosed, 0),
      otherClosed: rows.reduce((n, d) => n + d.otherClosed, 0),
      runs: rows.reduce((n, d) => n + d.runs.success + d.runs.failure + d.runs.other, 0),
      runsFailed: rows.reduce((n, d) => n + d.runs.failure, 0),
    },
    // The day before which the series is a floor rather than a count, per source. Null
    // means every read reached past the window and the whole ladder is a real count.
    horizon: {
      issues: issueHorizon != null && issueHorizon > now - days * DAY_MS ? dayKey(issueHorizon) : null,
      runs: runHorizon != null && runHorizon > now - days * DAY_MS ? dayKey(runHorizon) : null,
    },
  };
}

// --- what the machinery bought --------------------------------------------------

// The window's work, and the same window before it. Two rules shape what may appear
// here, and both are the owner's:
//
//   NO VANITY TOTAL. Every figure is bounded by a window, because a number that only
//   grows says nothing about today. The comparison is against the preceding window of
//   the same length, so "more than last week" is a fact and not a feeling.
//
//   NOTHING INVENTED. No estimated hours, no saved-effort multiplier, no score. Each
//   figure is a count of things that are individually true and individually clickable.
//   A quantity nothing on this page measures does not get a tile with a guess in it.
export function fleetBenefits(reads, { now, windowDays = 7 } = {}) {
  const span = windowDays * DAY_MS;
  const readable = (reads ?? []).filter((r) => r && !r.error && r.declaration);

  const inWindow = (t, from, to) => t != null && t >= from && t < to;

  const window = (from, to) => {
    let completed = 0;
    let unattended = 0;
    let parked = 0;
    let runs = 0;
    let runsFailed = 0;
    const members = new Set();

    for (const read of readable) {
      for (const i of (read.items ?? []).filter(isWorkItem)) {
        // Decoded, never a literal label test: a member's items wear whatever
        // spelling the engine that filed them wrote (work-item's `statusOf`).
        const wasParked = isParked(i);
        // An item that needed a person is counted whether or not it has closed —
        // being parked IS the event, and the ones still sitting there are the whole
        // point. `updated_at` is when its label was last written, which is the
        // closest thing the queue leaves to "when it was handed over".
        if (wasParked && inWindow(ms(i.updated_at), from, to)) parked += 1;

        if (i.state !== 'closed') continue;
        const at = ms(i.closed_at) ?? ms(i.updated_at);
        if (!inWindow(at, from, to)) continue;
        const outcome = outcomeOf(i);
        if (outcome === 'done' || outcome === 'delivered') {
          completed += 1;
          members.add(read.repo);
          // The parked label is the record of a human being pulled in: the queue puts
          // it on when a run cannot finish itself, and takes it off only when the item
          // is re-queued by hand. An item that closed without wearing it therefore ran
          // start to finish with nobody in the loop AT THE POINT IT CLOSED — an item
          // triaged and re-queued earlier in its life counts here too, and the panel
          // says so rather than claiming more than the labels can carry.
          if (!wasParked) unattended += 1;
        }
      }
      for (const r of read.runs ?? []) {
        if (!inWindow(ms(r.created_at), from, to)) continue;
        if (r.status !== 'completed') continue;
        runs += 1;
        if (r.conclusion !== 'success' && r.conclusion !== 'skipped' && r.conclusion !== 'cancelled') runsFailed += 1;
      }
    }
    return { completed, unattended, parked, runs, runsFailed, members: members.size };
  };

  const current = window(now - span, now + 1);
  const previous = window(now - 2 * span, now - span);

  // NO `converged` FIGURE. It counted members whose declaration carried a converge
  // datetime inside the window, and #1252 deleted that datetime — it recorded the
  // last full re-vendor rather than the last converge, so the tile was already
  // counting the wrong thing. Nothing this sweep reads can answer "converged in the
  // last N days": a member's installed versions say WHAT it holds, never when it
  // took it. The panel names the gap instead of approximating it, which is the rule
  // this figure broke twice over (#1001, #1008).
  return {
    windowDays,
    current,
    previous,
    members: readable.length,
  };
}

// A window-over-window change, in the only form that is honest when the previous
// window may itself be under-read: the delta, and which way it points.
export function delta(current, previous) {
  if (!Number.isFinite(current) || !Number.isFinite(previous)) return { dir: 'flat', by: 0 };
  const by = current - previous;
  return { dir: by > 0 ? 'up' : by < 0 ? 'down' : 'flat', by: Math.abs(by) };
}

// --- one member's commit history -------------------------------------------------

// The last N days of a member's commits, as a flat oldest-first ladder — the shape a
// GitHub-style square grid draws straight from.
//
// The input is `/stats/commit_activity`: 52 weeks, each a UTC-Sunday start and seven
// daily counts. This is the ONE read on the fleet page that is not derived from
// something already fetched, which is why it is priced as decoration everywhere it
// appears — and why its three empty answers must stay distinguishable. `null` is "not
// read" (withheld, or GitHub still computing the statistics) and is NOT the same fact
// as an array of zeroes, which is a repo that genuinely did nothing.
//
// A day the year does not cover is `null` rather than `0` for the same reason: a
// window wider than the data is under-read at its far end, not quiet there.
export function commitDays(weeks, { now, days = 90, classes = null } = {}) {
  if (!Array.isArray(weeks)) return null;

  const byDay = new Map();
  for (const w of weeks) {
    const start = Number(w?.week);
    if (!Number.isFinite(start)) continue;
    (w.days ?? []).forEach((count, i) => {
      byDay.set(dayKey(start * 1000 + i * DAY_MS), Number(count) || 0);
    });
  }

  const ladder = dayLadder(now, days);
  // `meaningful` is the second series, and it is null wherever the commit LISTING did
  // not reach — a shorter window than the statistics cover, or a page that filled up
  // before the window's start. Null there, not zero: "we did not classify this day" is
  // not "nothing meaningful happened".
  const rows = ladder.map((day) => ({
    day,
    count: byDay.has(day) ? byDay.get(day) : null,
    meaningful: classes?.byDay?.has(day) ? classes.byDay.get(day).meaningful : null,
  }));
  const counted = rows.filter((r) => r.count != null);
  return {
    days: rows,
    buckets: bucketWeekly(rows),
    total: counted.reduce((n, r) => n + r.count, 0),
    peak: counted.reduce((n, r) => Math.max(n, r.count), 0),
    // Days inside the window the year of statistics did not reach. Stated rather than
    // drawn as blank squares that read as quiet ones.
    unread: rows.length - counted.length,
    // How far the second series reaches, so the graph can draw it over its own span
    // and the hover can say what the rest of the line is not claiming.
    classified: classes ? { from: classes.from, complete: classes.complete } : null,
  };
}

// The same window in weeks, which is the resolution a QUARTER of history is legible
// at. Drawn daily, an ordinary repo's weekend is a trough and a Tuesday a spike, so a
// 90-point curve across a column is a sawtooth that hides the only thing the column is
// for: whether this repo is being worked on, and whether it always was.
//
// The daily counts are kept — they are the total, the peak and the hover — and only
// the drawn line is smoothed. Weeks run back from today, so the OLDEST bucket is the
// short one, and a week is null only when every day in it was unread; a week that was
// partly read is a real sum over what there was.
const WEEK = 7;

export function bucketWeekly(rows) {
  const out = [];
  for (let end = rows.length; end > 0; end -= WEEK) {
    const slice = rows.slice(Math.max(0, end - WEEK), end);
    const read = slice.filter((r) => r.count != null);
    const classified = slice.filter((r) => r.meaningful != null);
    out.unshift({
      from: slice[0].day,
      to: slice[slice.length - 1].day,
      days: slice.length,
      count: read.length ? read.reduce((n, r) => n + r.count, 0) : null,
      // The same rule as `count`, one series down: a week nothing classified is null,
      // and the drawn line breaks there rather than dropping to the floor.
      meaningful: classified.length ? classified.reduce((n, r) => n + r.meaningful, 0) : null,
    });
  }
  return out;
}

// --- meaningful against machinery ------------------------------------------------

// Which of a member's window commits were GENUINE PROJECT WORK, per day. The test is
// the queue's own (`isSubstantiveCommit`), so a member reads as quiet
// here exactly when its own preconditions read the repo as not having moved — a second
// notion of "meaningful" would mark a member sleepy on the commits its scheduler counts
// as movement.
//
// THE CHEAP TEST, and the page says so. The listing carries the message and the
// author, which is every exclusion but one: the corpus-only exclusion (a commit that
// touched nothing outside `.claudinite/`) needs each commit's file list, one request
// per commit, which this page's budget does not have. What survives it is the
// update's own commits, and those carry the housekeeping marker in their messages
// anyway — so the gap is narrow, and it is stated rather than implied away.
//
// `window` is github.mjs's `listCommitsSince` answer, or `undefined` when that read
// was withheld — which returns null here, the "not classified" state every consumer
// keeps distinct from "nothing meaningful happened".
export function commitClasses(window) {
  if (!window || !Array.isArray(window.commits)) return null;
  const byDay = new Map();
  let lastMeaningfulAt = null;
  for (const c of window.commits) {
    const at = ms(c.at);
    if (at == null) continue;
    const day = dayKey(at);
    if (!byDay.has(day)) byDay.set(day, { total: 0, meaningful: 0 });
    const row = byDay.get(day);
    row.total += 1;
    if (!isSubstantiveCommit(c)) continue;
    row.meaningful += 1;
    if (lastMeaningfulAt === null || at > lastMeaningfulAt) lastMeaningfulAt = at;
  }
  return {
    byDay,
    // The window's own start, and whether the read reached it. An incomplete read is
    // a HORIZON: the days before its oldest commit were not classified at all.
    from: window.since ?? null,
    complete: window.complete !== false,
    oldestSeen: window.commits.length
      ? Math.min(...window.commits.map((c) => ms(c.at)).filter((t) => t != null))
      : null,
    lastMeaningfulAt,
    meaningful: [...byDay.values()].reduce((n, r) => n + r.meaningful, 0),
    total: window.commits.length,
  };
}
