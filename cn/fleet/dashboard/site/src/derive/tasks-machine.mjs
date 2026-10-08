// How well the task machinery ran, and what it cost — derived from one file and
// nothing else: the member's own `task-runs-and-costs.json`. Pure, like every
// derive module here: no clock of its own beyond the one it is handed, no I/O, no DOM.
//
// THE SESSIONS' PLANE IS NOT THIS ONE. `growthSeries` and `fleetCorpus` answer what the
// corpus did to the sessions; this answers what the scheduler and the executor did, and
// the two files are folded by two tasks on two watermarks. A member folding one and not
// the other is an ordinary state, so nothing here falls back to the other file — it
// says the machinery plane is unfolded and stops.
//
// NOTHING HERE IMPORTS THE FOLD, and nothing here names the file's vocabulary as a
// list of its own: the decode in `read/usage.mjs` expands each positional tuple against
// the header the file declares, and this module reads the named result. A counter added
// or retired on the writing side reaches the panel with no change here.
//
// THE THREE RULES THE PANEL RENDERS UNDER, enforced at this layer rather than at the
// render, so no caller can forget one:
//
//   A WINDOW AGAINST THE WINDOW BEFORE IT, never a cumulative total. Every figure this
//   returns is a pair, `current` and `previous`, over two adjacent equal spans.
//
//   A MISSING KEY IS *NOT RECORDED*, never zero. A sum is taken over the days that had
//   an opinion and is `null` when none did — which is a different fact from `0`, and the
//   one the whole positional-tuple format exists to keep. `sum` below is the only place
//   that rule is implemented.
//
//   AN UNFOLDED MEMBER IS NAMED AND COUNTED IN NOTHING. `fleetTasksMachine` splits the
//   readable members into those that fold this file and those that do not, and the
//   second list is a census, never a row of zeroes in the first.

const DAY_MS = 86400e3;

const dayKey = (t) => new Date(t).toISOString().slice(0, 10);

// A sum over the entries that HAD an opinion, or null when none did. The unknown-is-not-
// zero rule in one function, so no caller has to remember it.
const sum = (values) => {
  const known = values.filter((n) => typeof n === 'number');
  return known.length ? known.reduce((a, b) => a + b, 0) : null;
};

// The outcome and park vocabularies the queue already spells its labels in. Named here
// only to give the panel a stable COLUMN ORDER — a word the file carries that is not in
// either list is still counted, under `other`, rather than dropped.
export const OUTCOMES = ['done', 'delivered', 'obsolete', 'none'];
export const PARKS = ['failure', 'action', 'decision', 'approval'];

// The four latency slots, in the order an item passes through them. Each is a SAMPLE
// list in the file — never a quantile — because a week's p50 is not derivable from its
// days', so the quantile is taken here, over whatever window is being drawn.
export const LATENCIES = [
  { key: 'tickToItemMinutes', label: 'tick → item' },
  { key: 'itemToPickMinutes', label: 'item → pick' },
  { key: 'pickToHandOffMinutes', label: 'pick → hand-off' },
  { key: 'handOffToConvergeMinutes', label: 'hand-off → converge' },
];

// What the panel would draw if the file carried it, and does not. NOT a gap in this
// module — a gap in the source — so it is stated in the panel's own note rather than
// drawn as an empty series or, worse, a zero. Adding a counter to the file is the
// fold's change to make and not this panel's, so these stay named until it does.
export const UNRECORDED = [
  'janitor repairs — the fold counts a task\'s outcomes and parks, not which of them the janitor rewrote',
  'leash reclaims — an item reclaimed from a dead episode is folded as its eventual outcome, with no separate count',
];

// Nearest-rank p50 and p90 over the samples handed in, plus how many there were. Fewer
// than two samples is reported as a count with no quantile: a p90 over one measurement
// is that measurement wearing a statistic's name.
export function quantiles(samples) {
  const xs = samples.filter((n) => typeof n === 'number').sort((a, b) => a - b);
  if (!xs.length) return { n: 0, p50: null, p90: null, max: null };
  const at = (q) => xs[Math.min(xs.length - 1, Math.ceil(q * xs.length) - 1)];
  return {
    n: xs.length,
    p50: xs.length >= 2 ? at(0.5) : null,
    p90: xs.length >= 2 ? at(0.9) : null,
    max: xs[xs.length - 1],
  };
}

// The day keys of the two adjacent windows ending today, as `[current, previous]`. Both
// are half-open on the right in the same direction, so a day belongs to exactly one.
export function windowDays(now, span) {
  const today = Math.floor(now / DAY_MS) * DAY_MS;
  const keys = (endExclusive) => Array.from({ length: span }, (_, i) =>
    dayKey(endExclusive - (span - i) * DAY_MS));
  return [keys(today + DAY_MS), keys(today + DAY_MS - span * DAY_MS)];
}

// --- one window, folded out of the day rows -----------------------------------------

// Everything one window has to say, from the day rows it covers. `rows` is already the
// decoded `{ day, row }` pairs for this window; a day the file never folded is simply
// absent from it, which is what makes every sum below null rather than zero.
function foldWindow(rows) {
  const totals = (field) => sum(rows.map(({ row }) => row?.[field]));

  // Per workflow. A workflow the window never saw contributes no row at all, so a fleet
  // where only the scheduler ran does not report the executor as having run zero times.
  const workflows = {};
  for (const { row } of rows) {
    for (const [name, counts] of Object.entries(row?.workflows ?? {})) {
      const into = (workflows[name] ??= { name, runs: [], jobs: [], minutesBilled: [], spend: [] });
      for (const f of ['runs', 'jobs', 'minutesBilled', 'spend']) into[f].push(counts?.[f]);
    }
  }

  // Per task: outcomes and parks, each word counted under its own key. A word neither
  // vocabulary names is kept rather than dropped — the queue's words are the file's, and
  // a new one should show up as itself and not vanish.
  const tasks = {};
  const bump = (key, group, word, n) => {
    if (typeof n !== 'number') return;
    const t = (tasks[key] ??= { key, outcomes: {}, parks: {} });
    t[group][word] = (t[group][word] ?? 0) + n;
  };
  for (const { row } of rows) {
    for (const [key, counts] of Object.entries(row?.queue ?? {})) {
      for (const [word, n] of Object.entries(counts ?? {})) bump(key, 'outcomes', word, n);
    }
    for (const [key, counts] of Object.entries(row?.parks ?? {})) {
      for (const [word, n] of Object.entries(counts ?? {})) bump(key, 'parks', word, n);
    }
  }

  // Latency samples, keyed by the item they came off so an item appearing in two day
  // rows — which the fold does not do, but which costs nothing to be right about —
  // contributes one sample per slot and not two.
  const samples = {};
  for (const { row } of rows) {
    for (const [item, slots] of Object.entries(row?.latency ?? {})) {
      (samples[item] ??= slots);
    }
  }

  // THE TWO HALVES ARE FOLDED SEPARATELY, on two watermarks, so "this window is folded"
  // is not one fact. A day row can carry the item half and not the run half — the fold
  // that first counted runs started later than the one counting items — and reading the
  // run figures off row-existence alone reports those days as the machinery having run
  // zero times. Each half therefore answers for itself.
  const runDays = rows.filter(({ row }) => typeof row?.runs === 'number').length;
  // The item half writes `queue` whenever it processed a day's closes and dropped the
  // map only when it was empty, so an absent `queue` on a day whose window shows the
  // half at all is a genuine zero; a window with no `queue` anywhere is unfolded.
  // `parks` rides the same pass off the same read, so it is seen exactly when `queue` is.
  const queueSeen = rows.some(({ row }) => row?.queue);

  return {
    runs: totals('runs'),
    jobs: totals('jobs'),
    minutesBilled: totals('minutesBilled'),
    // `spend` is only ever present where the file was priced; the sum stays null
    // otherwise, because a public repo bills nothing and a zero is a claim nobody made.
    spend: totals('spend'),
    apiCalls: totals('apiCalls'),
    workflows: Object.values(workflows)
      .map((w) => ({
        name: w.name,
        runs: sum(w.runs), jobs: sum(w.jobs), minutesBilled: sum(w.minutesBilled), spend: sum(w.spend),
      }))
      .sort((a, b) => a.name.localeCompare(b.name)),
    tasks,
    outcomes: mergeCounts(Object.values(tasks).map((t) => t.outcomes)),
    parks: mergeCounts(Object.values(tasks).map((t) => t.parks)),
    latency: Object.fromEntries(LATENCIES.map(({ key }) =>
      [key, quantiles(Object.values(samples).map((s) => s?.[key]))])),
    // How many of the window's days each half actually carried. The denominator every
    // figure above is read against, and the difference between a quiet week and an
    // unfolded one — stated per half, because the two are folded separately.
    foldedDays: rows.length,
    runDays,
    queueSeen,
  };
}

const mergeCounts = (maps) => {
  const out = {};
  for (const m of maps) for (const [word, n] of Object.entries(m ?? {})) out[word] = (out[word] ?? 0) + n;
  return out;
};

// A count off a merged word map, or null where the window's ITEM half folded no day.
// The difference matters: a folded week with no parks is `0` parks, an unfolded one is
// `null`, and only the first is a fact about the machinery. Gated on `queueSeen` rather
// than on any row existing, because a row can be the run half alone.
const countOf = (window, group, word) =>
  (window.queueSeen ? (window[group][word] ?? 0) : null);

// --- the repo panel -------------------------------------------------------------------

// One member's reliability and cost, as the panel draws it. `tasksUsage` is
// `readTasksUsage`'s decoded file, or null for a member that does not fold it.
export function tasksMachine(tasksUsage, { now, span = 7 } = {}) {
  const [currentDays, previousDays] = windowDays(now, span);
  const rowsFor = (days) => days
    .map((day) => ({ day, row: tasksUsage?.days?.[day] }))
    .filter(({ row }) => row);

  const current = foldWindow(rowsFor(currentDays));
  const previous = foldWindow(rowsFor(previousDays));

  // The task table: every task either window saw, with both windows' figures on one
  // row, so a task that stopped closing anything this week is visibly a row that went
  // to zero rather than a row that disappeared.
  const keys = [...new Set([...Object.keys(current.tasks), ...Object.keys(previous.tasks)])].sort();
  const tasks = keys.map((key) => {
    const side = (w) => {
      const t = w.tasks[key];
      return {
        outcomes: Object.fromEntries(OUTCOMES.map((o) => [o, t ? (t.outcomes[o] ?? 0) : null])),
        parks: Object.fromEntries(PARKS.map((p) => [p, t ? (t.parks[p] ?? 0) : null])),
        other: Object.entries(t?.outcomes ?? {}).filter(([w2]) => !OUTCOMES.includes(w2)),
        closed: t ? sum(Object.values(t.outcomes)) ?? 0 : null,
        parked: t ? sum(Object.values(t.parks)) ?? 0 : null,
      };
    };
    return { key, current: side(current), previous: side(previous) };
  });

  // The day series the sparkline and the column chart are drawn from: both windows, in
  // order, so the chart shows the comparison the tiles state. A day the file never
  // folded carries nulls and `source: 'none'`, which is what leaves the column blank
  // instead of drawing it at the floor.
  const days = [...previousDays, ...currentDays].map((day) => {
    const row = tasksUsage?.days?.[day];
    const outcomes = mergeCounts(Object.values(row?.queue ?? {}));
    const parks = mergeCounts(Object.values(row?.parks ?? {}));
    // `source` is what the column chart reads to tell a blank from a floor line, and the
    // two halves need their own: a day carrying the items and not the runs is a real
    // zero for one chart and NOT READ for the other, and one flag for the row would draw
    // a run chart claiming the machinery sat idle on days nothing counted its runs.
    return {
      day,
      source: row ? 'folded' : 'none',
      runSource: typeof row?.runs === 'number' ? 'folded' : 'none',
      window: currentDays.includes(day) ? 'current' : 'previous',
      runs: row ? (row.runs ?? null) : null,
      jobs: row ? (row.jobs ?? null) : null,
      minutesBilled: row ? (row.minutesBilled ?? null) : null,
      apiCalls: row ? (row.apiCalls ?? null) : null,
      ...Object.fromEntries(OUTCOMES.map((o) => [o, row ? (outcomes[o] ?? 0) : null])),
      parks: row ? PARKS.reduce((n, p) => n + (parks[p] ?? 0), 0) : null,
    };
  });

  return {
    folded: Boolean(tasksUsage),
    span,
    from: currentDays[0],
    to: currentDays[currentDays.length - 1],
    previousFrom: previousDays[0],
    previousTo: previousDays[previousDays.length - 1],
    foldedThrough: tasksUsage?.foldedThrough ?? null,
    generated: tasksUsage?.generated ?? null,
    // The rate every `spend` in the file was priced at, or null where the member
    // declares none. Stated beside the figure so a rate changed later cannot silently
    // re-price what it did not price.
    minuteRate: tasksUsage?.minuteRate ?? null,
    cost: {
      current: pick(current, ['runs', 'jobs', 'minutesBilled', 'spend', 'apiCalls']),
      previous: pick(previous, ['runs', 'jobs', 'minutesBilled', 'spend', 'apiCalls']),
      workflows: workflowRows(current, previous),
    },
    reliability: {
      current: {
        outcomes: Object.fromEntries(OUTCOMES.map((o) => [o, countOf(current, 'outcomes', o)])),
        parks: Object.fromEntries(PARKS.map((p) => [p, countOf(current, 'parks', p)])),
        closed: current.queueSeen ? sum(Object.values(current.outcomes)) ?? 0 : null,
        parked: current.queueSeen ? sum(Object.values(current.parks)) ?? 0 : null,
      },
      previous: {
        outcomes: Object.fromEntries(OUTCOMES.map((o) => [o, countOf(previous, 'outcomes', o)])),
        parks: Object.fromEntries(PARKS.map((p) => [p, countOf(previous, 'parks', p)])),
        closed: previous.queueSeen ? sum(Object.values(previous.outcomes)) ?? 0 : null,
        parked: previous.queueSeen ? sum(Object.values(previous.parks)) ?? 0 : null,
      },
      tasks,
    },
    latency: { current: current.latency, previous: previous.latency },
    days,
    foldedDays: {
      current: current.foldedDays, previous: previous.foldedDays,
      currentRuns: current.runDays, previousRuns: previous.runDays,
    },
    // Which of the panel's figures the file simply does not carry. Rendered as a stated
    // gap; a stated gap is information and an empty chart is not.
    unrecorded: UNRECORDED,
  };
}

const pick = (row, fields) => Object.fromEntries(fields.map((f) => [f, row[f]]));

// The per-workflow rows, both windows on one row — same reason the task table pairs
// them: a workflow that stopped running is a row that went to zero, not one that left.
function workflowRows(current, previous) {
  const names = [...new Set([
    ...current.workflows.map((w) => w.name), ...previous.workflows.map((w) => w.name),
  ])].sort();
  const find = (w, name) => w.workflows.find((x) => x.name === name) ?? null;
  return names.map((name) => ({ name, current: find(current, name), previous: find(previous, name) }));
}

// --- the fleet roll-up -----------------------------------------------------------------

// The same two windows across every member the sweep could read. One row per member,
// plus the fleet's own totals, plus the census of who cannot answer.
//
// `reads` is the fleet loader's raw per-member reads; each carries `tasksUsage`, which
// is null for a member that does not fold this file, could not be read, or predates it.
export function fleetTasksMachine(reads, { now, span = 7 } = {}) {
  const readable = (reads ?? []).filter((r) => r && !r.error && r.declaration);
  const folding = readable.filter((r) => r.tasksUsage);
  const absent = readable.filter((r) => !r.tasksUsage).map((r) => r.repo).sort();

  const members = folding
    .map((r) => ({ repo: r.repo, machine: tasksMachine(r.tasksUsage, { now, span }) }))
    .sort((a, b) => (b.machine.cost.current.runs ?? -1) - (a.machine.cost.current.runs ?? -1)
      || a.repo.localeCompare(b.repo));

  // Fleet totals: summed across the members that had an opinion, and null when none
  // did. An absent member is in `absent` and in no sum here — that census is what keeps
  // every figure below honest about its own denominator.
  const across = (side, field) => sum(members.map((m) => m.machine.cost[side][field]));
  const outcomes = (side, word) => sum(members.map((m) => m.machine.reliability[side].outcomes[word]));
  const parks = (side, word) => sum(members.map((m) => m.machine.reliability[side].parks[word]));

  const costSide = (side) => Object.fromEntries(
    ['runs', 'jobs', 'minutesBilled', 'spend', 'apiCalls'].map((f) => [f, across(side, f)]));
  const reliabilitySide = (side) => ({
    outcomes: Object.fromEntries(OUTCOMES.map((o) => [o, outcomes(side, o)])),
    parks: Object.fromEntries(PARKS.map((p) => [p, parks(side, p)])),
  });

  // The fleet's latency quantile is taken over every member's SAMPLES pooled, never
  // over their p50s: an average of medians is not a median, and the samples are in the
  // file precisely so the quantile can be taken at the window being drawn.
  const [currentDays, previousDays] = windowDays(now, span);
  const pooled = (days) => Object.fromEntries(LATENCIES.map(({ key }) => {
    const xs = [];
    for (const r of folding) {
      for (const day of days) {
        for (const slots of Object.values(r.tasksUsage.days?.[day]?.latency ?? {})) {
          if (typeof slots?.[key] === 'number') xs.push(slots[key]);
        }
      }
    }
    return [key, quantiles(xs)];
  }));

  // The fleet's rates, named rather than averaged: two members priced differently have
  // no one rate, and a mean of two rates is a number nothing measures.
  const rates = [...new Set(folding.map((r) => r.tasksUsage.minuteRate).filter((n) => typeof n === 'number'))];

  return {
    span,
    from: currentDays[0],
    to: currentDays[currentDays.length - 1],
    previousFrom: previousDays[0],
    previousTo: previousDays[previousDays.length - 1],
    cost: { current: costSide('current'), previous: costSide('previous') },
    reliability: { current: reliabilitySide('current'), previous: reliabilitySide('previous') },
    latency: { current: pooled(currentDays), previous: pooled(previousDays) },
    members,
    folding: folding.length,
    readable: readable.length,
    // Named, and counted in nothing above.
    absent,
    rates,
    unrecorded: UNRECORDED,
  };
}

// --- what the render needs -------------------------------------------------------------

// A figure and its change against the window before it, in the shape `windowFigure`
// takes. Unknown propagates: no previous means no delta, and neither side is invented.
export function change(current, previous) {
  if (typeof current !== 'number' || typeof previous !== 'number') return null;
  const delta = current - previous;
  return {
    dir: delta > 0 ? 'up' : delta < 0 ? 'down' : 'flat',
    by: delta === 0 ? 'no change' : `${delta > 0 ? '+' : '−'}${Math.abs(delta).toLocaleString()}`,
    delta,
    previous,
  };
}
