// The PAST-DATA plane: one file per repo, read once per head sha.
//
// Everything else this page shows is a live read — what is open right now, what ran in
// the last few dozen runs — and live reads are the one thing a browser dashboard
// cannot afford to make deep. Reaching a month back over the API would be a paginated
// crawl per repo, per load, against the viewer's own rate limit.
//
// So the depth comes from a file the repo already folds for itself:
// `.claudinite/usage/sessions-and-elements.json`, written by claudinite-tasks'
// usage-fold task. It is content at a sha, so it is cached like every other content
// read here — ZERO requests while the default branch has not moved — and one read
// answers every panel that looks further back than the live window.
//
// NOTHING HERE IMPORTS THE FOLD. The two packs are independent, and the file is
// self-describing precisely so a reader never has to: it carries a `fields` header
// naming the vocabulary each of its positional tuples is spelled in, and the decode
// below expands against THAT rather than against any list of its own. A field added or
// retired on the writing side reads correctly here with no change and no coordinated
// release; a version this code has never seen decodes as far as its own header goes.
//
// UNKNOWN IS NOT ZERO, all the way through. The fold leaves no key where a source
// could not answer — a day before a shallow checkout's history begins, a transcript
// shape carrying no token usage — and every series here carries that through as
// `null`. A chart breaks its line over a null; it never draws it at the floor.

import * as gh from './github.mjs';

export const USAGE_PATH = '.claudinite/usage/sessions-and-elements.json';
// Where the fold wrote it before `.claudinite/usage/`, read until the fold has moved it.
// @legacy-tolerance advisory:legacy-shape-in-use retire:#2323
export const LEGACY_USAGE_PATH = '.claudinite/local/usage.GENERATED.json';

// A rolling file's text at a sha, at its path or, where the member's fold has not moved
// it yet, the old one. The second read is spent only on a member that has not moved,
// and like the first it is cached under the sha.
export const readRollingText = async (getText, path, legacy) => (await getText(path)) ?? getText(legacy);
const textAtEither = (repo, sha, token, path, legacy) => readRollingText(
  (at) => gh.getTextAtSha(repo, sha, at, token), path, legacy,
);

const ms = (t) => (t == null ? null : new Date(t).getTime());

// --- decoding ---------------------------------------------------------------------

// One counter tuple → named counters, against the vocabulary the FILE declared. A
// `null` slot, or one past the end of a short tuple, yields no key: the writer had no
// opinion, and inventing a zero for it is the one thing this format exists to avoid.
export function decodeCounters(tuple, fields) {
  const row = {};
  (fields ?? []).forEach((f, i) => {
    const n = Array.isArray(tuple) ? tuple[i] : undefined;
    if (typeof n === 'number') row[f] = n;
  });
  return row;
}

// One row → named counters plus its sub-maps. Generic over the header: a key the file
// declares a vocabulary for is a counter group and is expanded; anything else (bare
// per-name counts, like the skill loads) passes through as it stands.
export function decodeRow(row, totalsFields, fields = {}) {
  const out = { ...decodeCounters(row?.totals, totalsFields) };
  for (const [key, value] of Object.entries(row ?? {})) {
    if (key === 'totals') continue;
    const vocab = fields[key];
    out[key] = vocab
      ? Object.fromEntries(Object.entries(value ?? {}).map(([k, t]) => [k, decodeCounters(t, vocab)]))
      : value;
  }
  return out;
}

// A whole file. Version 1 wrote fully-spelled objects and is returned as itself; every
// later version is tuples plus a header. An absent or unparsable file is `null` — the
// member is not folding, which is a state the page says out loud rather than a zero.
export function decodeUsage(doc) {
  if (!doc || typeof doc !== 'object') return null;
  const version = Number(doc.version ?? 1);
  const fields = doc.fields ?? {};
  const rows = (map, totalsFields) => Object.fromEntries(
    Object.entries(map ?? {}).map(([k, v]) => [k, version >= 2 ? decodeRow(v, totalsFields, fields) : v]),
  );
  return {
    version,
    // When the numbers were last CONFIRMED, which is not when the file last landed: a
    // quiet repo folds to the same numbers and opens no PR, so the commit date behind
    // this file can be older. A version that predates the stamp answers null, and the
    // page says "not stated" rather than guessing the head commit's date.
    generated: doc.generated ?? null,
    foldedThrough: doc.foldedThrough ?? null,
    hours: rows(doc.hours, fields.hour),
    days: rows(doc.days, fields.day),
    weeks: rows(doc.weeks, fields.week),
  };
}

// --- the machinery's own plane, beside the sessions' -------------------------------
//
// A SECOND FILE, read exactly the same way. `task-runs-and-costs.json` carries what
// this repo's scheduled machinery cost and how well it ran — runs, jobs, billed
// minutes and spend per workflow, API calls and wall time per run, and per task the
// outcomes, parks and latency samples its closed work items answer.
//
// It is a separate read because it is a separate file, written by a separate fold on
// a separate watermark: a member folding one and not the other is an ordinary state,
// and a page that asked for one file and inferred the other would report a member
// mid-rollout as broken. Both are content at a sha, so the second costs one request
// the first time a member's branch moves and none afterwards.
//
// NOTHING HERE KNOWS THE FILE'S VOCABULARY. The decode below is `decodeRow` against
// the header the file declares for itself, the same generic expansion the session
// file gets — which is what lets a field added or retired on the writing side read
// correctly here with no change and no coordinated release. Rendering is not this
// module's business and is not here yet.
export const TASKS_USAGE_PATH = '.claudinite/usage/task-runs-and-costs.json';
// @legacy-tolerance advisory:legacy-shape-in-use retire:#2323
export const LEGACY_TASKS_USAGE_PATH = '.claudinite/local/tasks-usage.GENERATED.json';

export function decodeTasksUsage(doc) {
  if (!doc || typeof doc !== 'object') return null;
  const fields = doc.fields ?? {};
  const rows = (map, totalsFields) => Object.fromEntries(
    Object.entries(map ?? {}).map(([k, v]) => [k, decodeRow(v, totalsFields, fields)]),
  );
  return {
    version: Number(doc.version ?? 1),
    generated: doc.generated ?? null,
    foldedThrough: doc.foldedThrough ?? null,
    // The rate every `spend` in the file was priced at, or null where the member
    // declares none — in which case no row carries a spend and the page says *not
    // recorded* rather than drawing a zero.
    minuteRate: typeof doc.minuteRate === 'number' ? doc.minuteRate : null,
    hours: rows(doc.hours, fields.hour),
    days: rows(doc.days, fields.day),
    weeks: rows(doc.weeks, fields.week),
  };
}

// Read one repo's machinery aggregate at a sha. A 404 is an ANSWER — this member
// does not fold this file — and it is cached as one.
export async function readTasksUsage(repo, sha, token) {
  try {
    const text = await textAtEither(repo, sha, token, TASKS_USAGE_PATH, LEGACY_TASKS_USAGE_PATH);
    if (!text) return null;
    return decodeTasksUsage(JSON.parse(text));
  } catch {
    return null;
  }
}

// Read one repo's aggregate at a sha. A 404 is an ANSWER — this repo does not fold —
// and it is cached as one, so a fleet sweep does not re-ask every member every load.
export async function readUsage(repo, sha, token) {
  try {
    const text = await textAtEither(repo, sha, token, USAGE_PATH, LEGACY_USAGE_PATH);
    if (!text) return null;
    return decodeUsage(JSON.parse(text));
  } catch {
    // A read this page declined to make, or one that failed, is not a repo that does
    // not fold. Both answer null here and the caller distinguishes them by whether it
    // asked at all — the panels say "not read", never "nothing happened".
    return null;
  }
}

// --- ladders ------------------------------------------------------------------------

const DAY_MS = 86400e3;
const HOUR_MS = 3600e3;

export const dayKey = (t) => new Date(t).toISOString().slice(0, 10);
export const hourKey = (t) => new Date(t).toISOString().slice(0, 13);

export function dayLadder(now, days) {
  const start = Math.floor(now / DAY_MS) * DAY_MS - (days - 1) * DAY_MS;
  return Array.from({ length: days }, (_, i) => dayKey(start + i * DAY_MS));
}

export function hourLadder(now, hours) {
  const start = Math.floor(now / HOUR_MS) * HOUR_MS - (hours - 1) * HOUR_MS;
  return Array.from({ length: hours }, (_, i) => hourKey(start + i * HOUR_MS));
}

// A field off a row, as a number or `null`. The whole unknown-is-not-zero rule in one
// function, so no caller has to remember it.
const at = (row, field) => (typeof row?.[field] === 'number' ? row[field] : null);

// --- what the corpus is doing to the sessions ---------------------------------------

// The growth series: what came back out of the checks each day, and what the sessions
// spent. Every field is null where the fold had no opinion, and a day with
// no row at all is null throughout rather than a zero — a repo that folded nothing for
// a week did not have a quiet week, it had an unfolded one.
export function growthSeries(usage, { now, days = 30 } = {}) {
  const ladder = dayLadder(now, days);
  const rows = ladder.map((day) => {
    const row = usage?.days?.[day];
    if (!row) return { day, missing: true, sessions: null, checkRuns: null, checkFailures: null, findings: null, tokensIn: null, tokensOut: null, tokenSessions: null, commits: null, linesAdded: null, linesRemoved: null, releases: null };
    // Both scopes together: "how many times did the checks run today" is one question,
    // and splitting it by scope here would put the world sweep's weekly rhythm into a
    // daily line that is otherwise the Stop hook's.
    const scopes = Object.values(row.checks ?? {});
    const sum = (f) => (scopes.length ? scopes.reduce((n, s) => n + (s[f] ?? 0), 0) : null);
    const findings = Object.values(row.checkFindings ?? {});
    return {
      day,
      missing: false,
      sessions: at(row, 'sessions'),
      checkRuns: sum('runs'),
      checkFailures: sum('failures'),
      findings: findings.length ? findings.reduce((n, f) => n + (f.blocking ?? 0) + (f.advisory ?? 0), 0) : null,
      tokensIn: at(row, 'tokensIn'),
      tokensOut: at(row, 'tokensOut'),
      tokenSessions: at(row, 'tokenSessions'),
      commits: at(row, 'commits'),
      linesAdded: at(row, 'linesAdded'),
      linesRemoved: at(row, 'linesRemoved'),
      releases: at(row, 'releases'),
    };
  });

  // Which of the optional series this repo's file actually carries. A page that cannot
  // tell "nothing happened" from "nothing recorded it" is the failure mode the whole
  // format is shaped against, so the answer is carried explicitly and rendered as a
  // stated gap rather than an empty chart.
  const has = (field) => rows.some((r) => r[field] !== null);
  return {
    days: rows,
    from: ladder[0],
    to: ladder[ladder.length - 1],
    folded: rows.some((r) => !r.missing),
    carries: {
      checks: has('checkRuns'),
      tokens: has('tokensIn'),
      commits: has('commits'),
      releases: has('releases'),
    },
    totals: {
      checkRuns: total(rows, 'checkRuns'),
      checkFailures: total(rows, 'checkFailures'),
      tokensIn: total(rows, 'tokensIn'),
      tokensOut: total(rows, 'tokensOut'),
      linesAdded: total(rows, 'linesAdded'),
      linesRemoved: total(rows, 'linesRemoved'),
      commits: total(rows, 'commits'),
      releases: total(rows, 'releases'),
    },
  };
}

// A total over the days that HAD an opinion, or null when none did. Never a sum that
// silently treats unknown days as zeroes.
function total(rows, field) {
  const known = rows.map((r) => r[field]).filter((n) => n !== null);
  return known.length ? known.reduce((a, b) => a + b, 0) : null;
}

// --- what the queue closed ----------------------------------------------------------

// Per-day outcome counts, from the file, with TODAY topped up from the live issue page
// the caller already fetched. The fold runs hourly and its own read is watermarked, so
// the last hour or two of closes is exactly what it has not seen — and that is the part
// a person looking at this page cares most about.
//
// `items` is the live closed-item list, each `{ closedAt, outcome }`; the top-up
// REPLACES the folded row for any day the live window fully covers rather than adding
// to it, because a day the fold has already partly counted would otherwise double.
export function queueSeries(usage, { now, days = 14, items = [], liveFrom = null } = {}) {
  const ladder = dayLadder(now, days);
  const live = {};
  for (const item of items) {
    const t = ms(item.closedAt);
    if (t == null) continue;
    const day = dayKey(t);
    (live[day] ??= {})[item.outcome ?? 'none'] = ((live[day] ?? {})[item.outcome ?? 'none'] ?? 0) + 1;
  }
  const liveDay = liveFrom == null ? null : dayKey(liveFrom);

  return ladder.map((day) => {
    const folded = usage?.days?.[day]?.queue ?? null;
    const useLive = liveDay !== null && day >= liveDay;
    if (useLive) {
      const counts = live[day] ?? {};
      return { day, source: 'live', ...outcomeRow(counts) };
    }
    if (!folded) return { day, source: 'none', done: null, delivered: null, obsolete: null, none: null };
    const counts = {};
    for (const row of Object.values(folded)) {
      for (const [word, n] of Object.entries(row)) counts[word] = (counts[word] ?? 0) + n;
    }
    return { day, source: 'folded', ...outcomeRow(counts) };
  });
}

const outcomeRow = (counts) => ({
  done: counts.done ?? 0,
  delivered: counts.delivered ?? 0,
  obsolete: counts.obsolete ?? 0,
  none: counts.none ?? 0,
});

// --- what ran, hour by hour ----------------------------------------------------------

// The two-day run picture: scheduler runs, executor runs, and the sessions that
// actually opened, per UTC hour.
//
// TWO SOURCES, EACH WHERE IT IS BETTER. The live runs listing the page already fetched
// is the freshest and reaches back however far its last few dozen runs go; the folded
// hours reach three days but stop at the fold's watermark. So the live list wins for
// every hour it fully covers — that is every hour AFTER its oldest run, since the hour
// that run landed in is only partly covered — and the file answers the rest.
//
// The sessions and the per-task detail have no live source at all: nothing in a run
// listing says whether an agent ran or what it did. Those come from the file or they
// are null, and a row says which.
export function hourSeries(usage, { now, hours = 48, runs = [] } = {}) {
  const ladder = hourLadder(now, hours);
  const liveCounts = {};
  let oldest = null;
  for (const run of runs) {
    const t = ms(run.created_at);
    if (t == null) continue;
    oldest = oldest === null ? t : Math.min(oldest, t);
    const kind = runKind(run);
    if (!kind) continue;
    const row = (liveCounts[hourKey(t)] ??= { scheduler: 0, executor: 0, failed: 0 });
    row[kind] += 1;
    if (run.status === 'completed' && failedRun(run)) row.failed += 1;
  }
  // The oldest run's own hour is only partly in the list, so the live window starts at
  // the next one — otherwise a busy hour truncated by the page size would read as a
  // quiet one and overwrite a complete folded row with it.
  const liveFrom = oldest === null ? null : hourKey(oldest + HOUR_MS);

  return ladder.map((hour) => {
    const folded = usage?.hours?.[hour] ?? null;
    const useLive = liveFrom !== null && hour >= liveFrom;
    const counts = useLive
      ? (liveCounts[hour] ?? { scheduler: 0, executor: 0, failed: 0 })
      : (folded ? { scheduler: folded.scheduler ?? 0, executor: folded.executor ?? 0, failed: folded.failed ?? 0 } : null);
    return {
      hour,
      source: useLive ? 'live' : (folded ? 'folded' : 'none'),
      scheduler: counts?.scheduler ?? null,
      executor: counts?.executor ?? null,
      failed: counts?.failed ?? null,
      // Sessions are only ever the fold's answer.
      agentic: folded ? (folded.agentic ?? null) : null,
      tasks: folded?.taskExec ? taskDetail(folded.taskExec) : [],
    };
  });
}

// Which of the two workflows a run belongs to, or null for the repo's own CI. Two
// independent signals, either sufficient: the workflow's PATH (what the engine names
// the file) and its display name. A run listed from a cache entry written before the
// path was projected still classifies, and a member who renamed the display name still
// classifies — which is the point of not resting it on one.
const SCHEDULER_RE = /claudinite-scheduler\.yml$|^claudinite scheduler$/i;
const EXECUTOR_RE = /claudinite-executor\.yml$|^claudinite executor$/i;

export function runKind(run) {
  const path = String(run?.path ?? '');
  const name = String(run?.name ?? '').trim();
  if (SCHEDULER_RE.test(path) || SCHEDULER_RE.test(name)) return 'scheduler';
  if (EXECUTOR_RE.test(path) || EXECUTOR_RE.test(name)) return 'executor';
  return null;
}

const failedRun = (run) => run.conclusion !== 'success' && run.conclusion !== 'skipped' && run.conclusion !== 'cancelled';

// The per-task executions an hour saw, flattened for a hover: `[{ key, statuses }]`,
// worst first so a failure is the first thing read.
export function taskDetail(taskExec) {
  return Object.entries(taskExec ?? {})
    .map(([key, row]) => ({
      key,
      statuses: Object.entries(row).filter(([, n]) => n > 0).map(([s, n]) => ({ status: s, count: n })),
      failed: (row.failed ?? 0) + (row.invalid ?? 0),
    }))
    .filter((t) => t.statuses.length)
    .sort((a, b) => b.failed - a.failed || a.key.localeCompare(b.key));
}
