// The machinery panel, drawn. One renderer for both pages: the repo page draws it for
// one member and the fleet page draws the roll-up, and they are the same shapes — which
// is why this is its own module rather than two copies that drift.
//
// WHAT IT DRAWS, AND WHAT IT REFUSES TO.
//
// Every figure is a WINDOW AGAINST THE WINDOW BEFORE IT, because a cumulative total is
// a number a person reads as a report card and nothing measures. `windowFigure` is the
// page's own primitive for that and spells the previous figure out beside the arrow.
//
// A MISSING KEY IS *NOT RECORDED*. The derivation already answers null for a counter no
// day had an opinion on; `fmt` below renders that as an em dash and the note beside the
// tile says which source did not answer. Nothing here coerces a null to zero, and the
// one place a zero appears is where the window WAS folded and the word did not occur.
//
// ONE SCALE PER CHART. Two measures of different magnitudes get two charts, never two
// y-axes on one — so runs and closes are drawn separately rather than superimposed.
//
// PARKS AND LATENCIES ARE TABLES, not charts. The park kinds carry severity, and the
// page's status palette is reserved for severity; painting four park kinds as four
// categorical series would spend it on identity. A ruled table says the same thing and
// is also the table view the contrast rule obliges for the lighter series colours.

import {
  el, chartLegend, stackedColumns, windowFigure, emptyRow, OUTCOME_COLOR,
} from './ui.mjs';
import { detailTable, expander } from './sheet.mjs';
import { OUTCOMES, PARKS, LATENCIES, change } from '../derive/tasks-machine.mjs';

// A figure, or the em dash that means nobody recorded it. Never a zero standing in for
// an unknown — the distinction this whole panel exists to keep.
export const fmt = (n) => (typeof n === 'number' ? n.toLocaleString() : '—');

// Minutes as a person reads them. Under an hour stays minutes, because the latencies
// this panel reports are mostly single-digit and "0.1h" says less than "6m".
export const mins = (n) => {
  if (typeof n !== 'number') return '—';
  if (n < 60) return `${Math.round(n)}m`;
  if (n < 1440) return `${Math.round(n / 6) / 10}h`;
  return `${Math.round(n / 144) / 10}d`;
};

const money = (n, rate) => (typeof n === 'number' && typeof rate === 'number'
  ? `$${(n).toFixed(2)}`
  : null);

// The outcome words, in the colours the rest of the page already gives them. Colour
// follows the entity: `done` is the same green in this chart as in the queue panel
// above it, so the two read as one vocabulary and not two.
const OUTCOME_SERIES = [
  { label: 'done', color: OUTCOME_COLOR.done, value: (d) => d.done },
  { label: 'delivered', color: OUTCOME_COLOR.delivered, value: (d) => d.delivered },
  { label: 'obsolete', color: OUTCOME_COLOR.obsolete, value: (d) => d.obsolete },
  { label: 'no outcome', color: OUTCOME_COLOR.none, value: (d) => d.none },
];

const RUN_SERIES = [{ label: 'workflow runs', color: 'var(--s-blue)', value: (d) => d.runs }];

// The sentence every panel opens with: which two spans are being compared, and how much
// of them the file actually carried. A rate with an unstated denominator is the figure
// this panel exists not to print.
export function windowNote(m) {
  const span = `${m.from} → ${m.to}`;
  const before = `${m.previousFrom} → ${m.previousTo}`;
  const folded = m.foldedDays
    ? ` · ${m.foldedDays.current}/${m.span} day(s) folded this window, ${m.foldedDays.previous}/${m.span} before`
      + (typeof m.foldedDays.currentRuns === 'number' && m.foldedDays.currentRuns !== m.foldedDays.current
        ? ` (the run half reaches ${m.foldedDays.currentRuns} of them)` : '')
    : '';
  return `${span}, against ${before}${folded}`;
}

// --- the repo panel --------------------------------------------------------------------

export function machinePanel(m) {
  if (!m.folded) {
    return [el('div', { className: 'chart-card' }, [
      el('div', { className: 'k', textContent: 'this repo folds no machinery usage file' }),
      el('p', {
        className: 'sub',
        textContent: 'Everything in this panel comes from `.claudinite/usage/task-runs-and-costs.json`, '
          + 'which the engine\'s usage-fold task writes; the panel fills in from its first run, and '
          + 'nothing else on this page depends on it.',
      }),
    ])];
  }

  return [
    reliabilityCard(m),
    latencyCard(m.latency, m.span),
    costCard(m),
    taskCard(m),
    gapsCard(m),
  ];
}

function reliabilityCard(m) {
  const { current, previous } = m.reliability;
  const tile = (value, label, prev, note, better) =>
    windowFigure(fmt(value), label, change(value, prev), note, { better });

  return el('div', { className: 'chart-card wide' }, [
    el('div', { className: 'k', textContent: 'reliability — what the queue closed, and what it left for a person' }),
    el('div', { className: 'tiles' }, [
      tile(current.closed, 'work items closed', previous.closed,
        current.closed === null ? 'no day in this window is folded' : null, 'up'),
      tile(current.outcomes.done, 'closed done', previous.outcomes.done, null, 'up'),
      // More work needing a person is not progress, so the arrow's good direction flips.
      tile(current.parked, 'parked for a person', previous.parked, null, 'down'),
      tile(current.parks.failure, 'failure parks', previous.parks.failure,
        'the only park that holds its task\'s lane', 'down'),
    ]),
    chartLegend(OUTCOME_SERIES),
    stackedColumns(m.days, OUTCOME_SERIES, { detail: dayDetail }),
    el('div', {
      className: 'sub',
      textContent: `${windowNote(m)}. A day the fold has not reached is left blank rather than drawn `
        + 'at the floor — "not folded" and "nothing closed" are different facts.',
    }),
  ]);
}

// What a day was about, for the hover: the parks it collected, which the column itself
// has no room for and which is the reader's next question after "how many closed".
function dayDetail(d) {
  if (d.source === 'none') return 'not folded yet';
  const bits = [];
  if (typeof d.parks === 'number' && d.parks) bits.push(`${d.parks} park(s)`);
  if (typeof d.runs === 'number') bits.push(`${d.runs} run(s)`);
  if (typeof d.minutesBilled === 'number') bits.push(`${d.minutesBilled} billed min`);
  return bits.join(' · ') || null;
}

function latencyCard(latency, span) {
  // Quantiles over the window's SAMPLES, taken here rather than read off the file: a
  // week's p50 is not derivable from its days', which is why the fold carries samples.
  const rows = LATENCIES.map(({ key, label }) => {
    const c = latency.current[key];
    const p = latency.previous[key];
    return [
      label,
      { text: mins(c.p50), gap: c.p50 === null },
      { text: mins(c.p90), gap: c.p90 === null },
      { text: mins(c.max), gap: c.max === null },
      { text: String(c.n), gap: !c.n },
      { text: mins(p.p50), gap: p.p50 === null },
    ];
  });
  const total = LATENCIES.reduce((n, { key }) => n + latency.current[key].n, 0);

  return el('div', { className: 'chart-card wide' }, [
    el('div', { className: 'k', textContent: 'how long each leg took' }),
    detailTable([
      { label: 'leg' }, { label: 'p50', num: true }, { label: 'p90', num: true },
      { label: 'slowest', num: true }, { label: 'samples', num: true }, { label: 'p50 before', num: true },
    ], rows),
    el('div', {
      className: 'sub',
      textContent: total
        ? `${total} sample(s) across ${span} days. A leg whose far end never happened — an agentless `
          + 'item has no hand-off — contributes no sample, which is not a latency of zero. A quantile '
          + 'is withheld below two samples.'
        : 'No item closed in this window carried a label timeline the fold could read a latency off.',
    }),
  ]);
}

function costCard(m) {
  const { current, previous } = m.cost;
  const spend = money(current.spend, m.minuteRate);

  const workflowRows = m.cost.workflows.map((w) => [
    w.name,
    { text: fmt(w.current?.runs), gap: !w.current },
    { text: fmt(w.current?.jobs), gap: !w.current },
    { text: fmt(w.current?.minutesBilled), gap: !w.current },
    { text: fmt(w.previous?.runs), gap: !w.previous },
  ]);

  return el('div', { className: 'chart-card wide' }, [
    el('div', { className: 'k', textContent: 'cost — what the scheduler and the executor were billed' }),
    el('div', { className: 'tiles' }, [
      windowFigure(fmt(current.runs), 'workflow runs', change(current.runs, previous.runs), null, 'down'),
      windowFigure(fmt(current.minutesBilled), 'billed minutes', change(current.minutesBilled, previous.minutesBilled),
        'Actions bills per job, rounded up', 'down'),
      // Unpriced is the ordinary state — a public repo bills nothing — and the key that
      // would price it is NAMED, so a reader knows what to set rather than what is broken.
      spend
        ? windowFigure(spend, 'spend', null, `at $${m.minuteRate}/min`)
        : el('div', { className: 'tile' }, [
          el('div', { className: 'v gap', textContent: '—' }),
          el('div', { className: 'k', textContent: 'spend' }),
          el('div', { className: 'sub', textContent: 'not recorded — no actionsMinuteRate in this pack\'s config' }),
        ]),
      windowFigure(fmt(current.apiCalls), 'API calls', change(current.apiCalls, previous.apiCalls),
        current.apiCalls === null ? 'no run in this window printed a cost record' : 'per run, from the cost records', 'down'),
    ]),
    // ONE SCALE. Runs get their own chart rather than a second axis on the outcome one:
    // a busy scheduler superimposed on closed work reads as productivity.
    //
    // It is drawn off `runSource`, not the row's own: the run half and the item half are
    // folded on separate watermarks, so a day carrying items and no run figure is NOT
    // READ here even though the outcome chart beside it has a real zero for that day.
    chartLegend(RUN_SERIES),
    stackedColumns(m.days.map((d) => ({ ...d, source: d.runSource })), RUN_SERIES, { detail: dayDetail }),
    workflowRows.length
      ? detailTable([
        { label: 'workflow' }, { label: 'runs', num: true }, { label: 'jobs', num: true },
        { label: 'billed min', num: true }, { label: 'runs before', num: true },
      ], workflowRows)
      : el('div', { className: 'sub', textContent: 'No workflow run is folded in either window.' }),
  ]);
}

// One row per task, both windows on it — so a task that stopped closing anything is a
// row that went to zero rather than a row that quietly left the table.
function taskCard(m) {
  const rows = m.reliability.tasks;
  const table = el('div', { className: 'detail' }, [
    detailTable([
      { label: 'task' },
      ...OUTCOMES.map((o) => ({ label: o, num: true })),
      ...PARKS.map((p) => ({ label: `${p} park`, num: true })),
      { label: 'closed before', num: true },
    ], rows.map((t) => [
      t.key,
      ...OUTCOMES.map((o) => ({ text: fmt(t.current.outcomes[o]), gap: t.current.outcomes[o] === null })),
      ...PARKS.map((p) => ({ text: fmt(t.current.parks[p]), gap: t.current.parks[p] === null })),
      { text: fmt(t.previous.closed), gap: t.previous.closed === null },
    ])),
  ]);
  table.hidden = true;

  return el('div', { className: 'chart-card wide' }, [
    el('div', {
      className: 'k',
      textContent: rows.length
        ? `${rows.length} task(s) produced a closed work item in one of the two windows`
        : 'no task closed a work item in either window',
    }),
    rows.length ? expander('per task', table) : null,
    rows.length ? table : null,
  ]);
}

// The gap note. A number the panel would draw and the file does not carry is STATED,
// never drawn as an empty series and never as a zero — and the fix is a counter in the
// fold, which is that task's change to make and not this panel's.
function gapsCard(m) {
  return el('div', { className: 'chart-card wide' }, [
    el('div', { className: 'k', textContent: 'what this panel cannot show' }),
    el('ul', { className: 'needs' }, m.unrecorded.map((g) => el('li', { className: 'sub', textContent: g }))),
    el('div', {
      className: 'sub',
      textContent: `Folded through ${m.foldedThrough ?? 'not stated'}`
        + `${m.generated ? `, last confirmed ${m.generated.slice(0, 16).replace('T', ' ')}Z` : ''}.`,
    }),
  ]);
}

// --- the fleet roll-up ------------------------------------------------------------------

// The same two windows across every member the sweep could read, plus the census of who
// cannot answer. The census is the denominator every figure above it is read against.
export function fleetMachinePanel(f, onOpen = null) {
  if (!f.folding) {
    return [el('div', { className: 'chart-card' }, [
      el('div', { className: 'k', textContent: 'no member folds a machinery usage file yet' }),
      el('p', {
        className: 'sub',
        textContent: `${f.readable} readable member(s), none of them folding `
          + '`.claudinite/usage/task-runs-and-costs.json` — everything here waits on the '
          + 'engine\'s usage-fold task.',
      }),
    ])];
  }

  const { current, previous } = f.cost;
  const rel = f.reliability;

  return [
    el('div', { className: 'chart-card wide' }, [
      el('div', { className: 'k', textContent: 'the fleet\'s machinery, this window against the one before' }),
      el('div', { className: 'tiles' }, [
        windowFigure(fmt(current.runs), 'workflow runs', change(current.runs, previous.runs),
          `in ${f.folding} folding member(s)`, 'down'),
        windowFigure(fmt(current.minutesBilled), 'billed minutes', change(current.minutesBilled, previous.minutesBilled),
          f.rates.length ? `priced in ${f.rates.length} member(s)` : 'no member declares a minute rate', 'down'),
        windowFigure(fmt(rel.current.outcomes.done), 'items closed done',
          change(rel.current.outcomes.done, rel.previous.outcomes.done), null, 'up'),
        windowFigure(fmt(rel.current.parks.failure), 'failure parks',
          change(rel.current.parks.failure, rel.previous.parks.failure),
          'each one holds its task\'s lane', 'down'),
      ]),
      el('div', {
        className: 'sub',
        textContent: `${windowNote(f)} · ${f.folding}/${f.readable} member(s) fold this file`
          + `${f.absent.length ? `; not folding: ${f.absent.map((r) => r.split('/')[1] ?? r).join(', ')}` : ''}. `
          + 'A member that folds nothing is named here and counted in no figure above.',
      }),
    ]),
    latencyCard(f.latency, f.span),
    fleetMembers(f, onOpen),
  ];
}

function fleetMembers(f, onOpen) {
  const table = el('table', {});
  const thead = el('thead', {}, [el('tr', {}, [
    'member', 'runs', 'billed min', 'closed', 'done', 'parked', 'failure parks', 'API calls',
  ].map((label, i) => el('th', { className: `cap${i ? ' num' : ''}`, textContent: label })))]);
  const tbody = el('tbody', {});
  table.append(thead, tbody);

  for (const { repo, machine } of f.members) {
    const name = el('td', { className: 'name nw', textContent: repo });
    if (onOpen) {
      name.replaceChildren(el('a', { href: `?repo=${repo}`, textContent: repo,
        onclick: (e) => { e.preventDefault(); onOpen(repo); } }));
    }
    const num = (v) => el('td', { className: `num${typeof v === 'number' ? '' : ' gap'}`, textContent: fmt(v) });
    tbody.append(el('tr', {}, [
      name,
      num(machine.cost.current.runs),
      num(machine.cost.current.minutesBilled),
      num(machine.reliability.current.closed),
      num(machine.reliability.current.outcomes.done),
      num(machine.reliability.current.parked),
      num(machine.reliability.current.parks.failure),
      num(machine.cost.current.apiCalls),
    ]));
  }
  // The census, as rows that say so rather than rows of zeroes.
  for (const repo of f.absent) {
    tbody.append(el('tr', {}, [
      el('td', { className: 'name nw dim', textContent: repo }),
      el('td', { className: 'dim', colSpan: 7, textContent: 'folds no machinery usage file' }),
    ]));
  }
  if (!f.members.length && !f.absent.length) tbody.append(emptyRow(8, 'No member could be read.'));

  return el('div', { className: 'chart-card wide' }, [
    el('div', { className: 'k', textContent: 'per member' }),
    el('div', { className: 'panel scroll' }, [table]),
  ]);
}
