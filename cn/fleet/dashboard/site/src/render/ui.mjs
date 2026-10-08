// Shared rendering vocabulary. Both views draw from this so a state, a duration or a
// severity looks and reads the same whether you are looking at one repo or twelve —
// a fleet page whose "blocked" chip differs from the repo page's is a page you have
// to learn twice.

import {
  STATUS_BLOCKED, STATUS_READY, STATUS_RUNNING_EXECUTOR, STATUS_RUNNING_AGENT,
} from '../read/queue-vocabulary.mjs';
// `PARKED` is the page's own key rather than a label: a park is four labels, and
// the page groups them into one column (model.mjs).
import { PARKED } from '../derive/model.mjs';

export const $ = (id) => document.getElementById(id);

export const el = (tag, props = {}, kids = []) => {
  const n = Object.assign(document.createElement(tag), props);
  for (const k of [].concat(kids)) if (k !== null && k !== undefined && k !== '') n.append(k);
  return n;
};

// --- formatting ---------------------------------------------------------------

const DUR = [['d', 86400e3], ['h', 3600e3], ['m', 60e3]];

export function duration(msVal) {
  if (msVal == null || !Number.isFinite(msVal)) return '—';
  const v = Math.abs(msVal);
  for (const [suffix, unit] of DUR) if (v >= unit) return `${Math.floor(v / unit)}${suffix}`;
  return '<1m';
}

export const ago = (at, now) => {
  const t = at == null ? null : (typeof at === 'number' ? at : new Date(at).getTime());
  return t ? `${duration(now - t)} ago` : '—';
};
export const until = (date, now) => (date ? `in ${duration(date.getTime() - now)}` : '—');
export const stamp = (iso) => (iso ? new Date(iso).toISOString().replace('T', ' ').slice(0, 16) : '—');

// --- state and severity ---------------------------------------------------------

export const STATE_UI = {
  [STATUS_BLOCKED]:     { cls: 'blocked',   label: 'blocked' },
  [STATUS_READY]:       { cls: 'ready',     label: 'ready' },
  [STATUS_RUNNING_EXECUTOR]:   { cls: 'executing', label: 'executing' },
  [STATUS_RUNNING_AGENT]:       { cls: 'agent',     label: 'agent' },
  [PARKED]: { cls: 'human',     label: 'needs human' },
  torn:          { cls: 'torn',      label: 'torn labels' },
  unlabelled:    { cls: 'torn',      label: 'no state label' },
  closed:        { cls: 'idle',      label: 'closed' },
};

// The order the queue's states are shown in everywhere: the sequence an item moves
// through, so a row reads left to right as progress.
export const STATE_ORDER = [STATUS_BLOCKED, STATUS_READY, STATUS_RUNNING_EXECUTOR, STATUS_RUNNING_AGENT, PARKED];

// Keyed by the canonical outcome words `outcomeOf` decodes to, so a spelling
// migration in the labels never reaches this table.
export const OUTCOME_COLOR = {
  done: 'var(--good)',
  delivered: 'var(--s-violet)',
  obsolete: 'var(--muted)',
  none: 'var(--critical)',
};

export const STATE_COLOR = {
  [STATUS_BLOCKED]: 'var(--s-blue)',
  [STATUS_READY]: 'var(--s-aqua)',
  [STATUS_RUNNING_EXECUTOR]: 'var(--s-yellow)',
  [STATUS_RUNNING_AGENT]: 'var(--s-violet)',
  [PARKED]: 'var(--critical)',
};

// Severity always ships as colour PLUS a glyph and words — the status palette is
// reserved and never carries meaning on its own.
export const LEVEL_GLYPH = { critical: '●', serious: '▲', warning: '▲', info: '·', ok: '✓' };

export function chip(state) {
  const ui = STATE_UI[state] ?? { cls: 'idle', label: state ?? 'idle' };
  return el('span', { className: `chip ${ui.cls}` }, [el('i', { className: 'dot' }), ui.label]);
}

// A reason naming an item by number — "blocked on #12, #13" — is drawn on the page of
// the repo that number belongs to, so it is given one to link against there. The fleet
// page's reasons count members and name none, and pass no repo.
export const reasonNodes = (reasons, repo = null) =>
  reasons.map((r) => el('span', { className: `warn ${r.level}` },
    refNodes(repo, `${LEVEL_GLYPH[r.level] ?? '▲'} ${r.text}`)));

export const warnNodes = reasonNodes;

// --- marks ----------------------------------------------------------------------

// A thin proportional bar. Segments are `[label, count, color]`; zero-count segments
// are dropped rather than drawn as slivers.
export function segmentBar(segments, { width = 108, title = (l, n) => `${n} ${l}` } = {}) {
  const bar = el('div', { className: 'bar', style: `width:${width}px` });
  let any = false;
  for (const [label, count, color] of segments) {
    if (!count) continue;
    any = true;
    bar.append(el('i', { style: `flex:${count};background:${color}`, title: title(label, count) }));
  }
  if (!any) bar.append(el('i', { className: 'bar-empty', style: 'flex:1' }));
  return bar;
}

// --- the member row's compact marks ----------------------------------------------

// A fleet grid is wide, and two of these fold a whole column each back into a mark
// the row reads at the same glance. (A third did the same for the star count, until
// stars became `git-github`'s contribution and moved to the member's subrow.)

// CI as a dot with its age under it. The dot is never the whole message — it carries
// a `title` and an `aria-label` in words, because a colour alone is unreadable to a
// reader who cannot see the difference between this green and this red.
//
// The age is bare: "7h", not "7h ago". In a column of them the word is on every row
// and carries no information, and the header says what the number is.
export function ciMark(ui, when) {
  return el('div', { className: 'ci-mark' }, [
    el('i', {
      className: `dot ${ui.cls}`,
      role: 'img',
      title: `CI ${ui.label}`,
      'aria-label': `CI ${ui.label}`,
    }),
    el('div', { className: 'sub', textContent: when }),
  ]);
}

// What is waiting on a person here, as a bar and one line. THE THIRD FOLD, and the
// one the grid most needed: the sentences `attentionBreakdown` writes are the only
// prose in a row of marks, so the column took every pixel the other nine could spare
// and still wrapped each sentence to one word per line.
//
// The bar weighs LEVELS, not kinds. Two serious kinds drawn as two adjacent segments
// of one colour is a boundary that means nothing, and severity is the one thing the
// line below cannot show. Its hover names each level in words, because the palette
// never carries meaning on its own.
//
// The kinds survive on that line — "9 actions" is not "9 items needing something
// changed outside the code", which is why the sentences are on the mark's own hover
// rather than deleted. A reader who cannot tell what an action is has somewhere to go.
const ATTENTION_LEVELS = [
  ['critical', 'var(--critical)'],
  ['serious', 'var(--serious)'],
  ['warning', 'var(--warning)'],
];

export function attentionMark(rows, { width = 92 } = {}) {
  if (!rows.length) return el('div', {}, [el('span', { className: 'sub', textContent: 'nothing waiting' })]);
  const weigh = (level) => rows.filter((r) => r.level === level).reduce((n, r) => n + r.count, 0);
  const sentences = rows.map((r) => r.text).join('\n');
  return el('div', { className: 'attn', title: sentences, 'aria-label': sentences }, [
    segmentBar(ATTENTION_LEVELS.map(([level, color]) => [level, weigh(level), color]), { width }),
    el('div', { className: 'sub', textContent: rows.map((r) => `${r.count} ${r.short}`).join(' \u00b7 ') }),
  ]);
}

// The pack count, with the member's freshness worn as a badge on it. Two facts that
// are read together — how much Claudinite is declared here, and whether what is
// declared is current — and almost always the badge says the same thing, so it earns a
// corner rather than a column. The detail is the hover: the roster's own sentence.
export function packMark(count, freshness) {
  const badge = FRESHNESS_BADGE[freshness?.state] ?? FRESHNESS_BADGE.unknown;
  const title = freshness?.detail || badge.title;
  return el('div', { className: 'pack-mark', title }, [
    el('div', { className: 'n num', textContent: count == null ? '—' : String(count) }),
    el('div', { className: `badge ${badge.cls}`, textContent: badge.glyph, role: 'img', 'aria-label': title }),
  ]);
}

// `unknown` is not `fresh`: with no roster published there is nothing that judged it,
// and a tick there would claim a check that never happened.
const FRESHNESS_BADGE = {
  fresh: { glyph: '✓', cls: 'ok', title: 'at the published versions' },
  behind: { glyph: '⏱', cls: 'info', title: 'behind the published versions' },
  'no-stamp': { glyph: '?', cls: 'warning', title: 'declares packs but has never been vendored' },
  'no-scheduler': { glyph: '?', cls: 'warning', title: 'no scheduler workflow, so nothing will converge it' },
  node: { glyph: '·', cls: 'idle', title: 'runs the Node engine, which the roster does not compare' },
  dormant: { glyph: '·', cls: 'idle', title: 'dormant — not measured' },
  unknown: { glyph: '·', cls: 'idle', title: 'freshness unknown — this deployment runs no fleet-roster' },
};

// A member's last 90 days of commits, as a filled area over time.
//
// A curve rather than a grid of day-squares: at this size the question a reader is
// asking is "is this repo being worked on, and was it always" — a shape answers that
// across the column in one glance, where 90 separate squares have to be counted.
//
// Drawn WEEKLY. A quarter at daily resolution is a sawtooth — an ordinary repo's
// weekend is a trough and its Tuesday a spike — and a sawtooth has no shape to read.
// The daily counts still supply the total, the peak and the hover.
//
// Three empty states, and they are three different facts. A null series is a read
// that did not happen (withheld for budget, or GitHub still computing); a null DAY is
// outside the year of statistics the API returns; and zeroes are a repo that was
// genuinely quiet. The curve breaks over unread days rather than drawing them at the
// floor, so a gap in the data never renders as a quiet stretch.
export function commitGraph(series, { width = 108, height = 26, note = null } = {}) {
  if (!series) return el('div', { className: 'sub', textContent: 'not read' });

  const NS = 'http://www.w3.org/2000/svg';
  const svgEl = (tag, attrs = {}) => {
    const n = document.createElementNS(NS, tag);
    for (const [k, v] of Object.entries(attrs)) n.setAttribute(k, String(v));
    return n;
  };

  const { days, buckets, total, peak, unread } = series;
  const svg = svgEl('svg', {
    viewBox: `0 0 ${width} ${height}`, preserveAspectRatio: 'none',
    class: 'commits', role: 'img',
    'aria-label': `${total} commits over ${days.length} days, busiest day ${peak}`,
  });

  const step = buckets.length > 1 ? width / (buckets.length - 1) : width;
  // Scaled to the row's own busiest WEEK. A shared scale across members was the
  // alternative and it is worse: one member vendoring a tree flattens every other row
  // to a floor, which is the reading — "nothing happens in these repos" — the column
  // exists to disprove. The hover carries the numbers, so the scale is never guessed.
  const top = buckets.reduce((n, b) => Math.max(n, b.count ?? 0), 0);
  const y = (count) => height - 1 - (top > 0 ? (count / top) * (height - 2) : 0);

  // Contiguous runs of READ weeks. A run of one still draws, so a single week
  // surrounded by unread ones is visible rather than dropped. `pick` is which series
  // this pass draws and `cls` how: the total is an area, the meaningful line a plain
  // stroke over it.
  const draw = (pick, cls, area) => {
    let run = [];
    const flush = () => {
      if (!run.length) { return; }
      const line = run.map((p, i) => `${i ? 'L' : 'M'}${p.x.toFixed(2)},${p.y.toFixed(2)}`).join(' ');
      if (area) {
        svg.append(svgEl('path', {
          d: `${line} L${run[run.length - 1].x.toFixed(2)},${height} L${run[0].x.toFixed(2)},${height} Z`,
          class: 'commit-area',
        }));
      }
      svg.append(svgEl('path', { d: line, class: cls }));
      run = [];
    };
    buckets.forEach((b, i) => {
      const v = pick(b);
      if (v == null) { flush(); return; }
      run.push({ x: i * step, y: y(v) });
    });
    flush();
  };
  draw((b) => b.count, 'commit-line', true);
  // The SECOND series: the commits that were genuine project work rather than the
  // machinery moving. Drawn on the same scale, over the span the commit listing
  // reached and breaking where it did not — the gap between the two lines is the
  // machinery's own share, which is the reading the fleet's owner asked for.
  const meaningful = buckets.reduce((n, b) => n + (b.meaningful ?? 0), 0);
  const classified = buckets.some((b) => b.meaningful != null);
  if (classified) draw((b) => b.meaningful, 'commit-line meaningful', false);

  const title = svgEl('title');
  title.textContent = [
    `${total} commit${total === 1 ? '' : 's'} over ${days.length} days, by week`,
    classified ? `${meaningful} meaningful where classified — the lower line` : 'meaningful commits not classified — the listing was not read',
    peak ? `busiest day ${peak}, busiest week ${top}` : null,
    unread ? `${unread} day(s) outside the year GitHub reports` : null,
  ].filter(Boolean).join(' · ');
  svg.append(title);

  return el('div', { className: 'commit-graph' }, [
    svg,
    el('div', {
      className: 'sub',
      textContent: [total === 0 && !unread ? 'none' : String(total), note].filter(Boolean).join(' · '),
    }),
  ]);
}

// --- tables ---------------------------------------------------------------------

export const head = (table, cols) => {
  table.replaceChildren();
  table.append(el('thead', {}, [el('tr', {}, cols.map((c) => el('th', { textContent: c })))]));
  return table.appendChild(el('tbody'));
};

export const emptyRow = (span, text) =>
  el('tr', {}, [el('td', { colSpan: span, className: 'empty', textContent: text })]);

// A number on this page is always something to OPEN, so every `#N` the page draws is
// an anchor to it. GitHub's `/issues/<n>` redirects to the pull request when the
// number is one, so a single form covers both and a sentence naming an issue and the
// PR that closes it needs no telling apart.
export const issueUrl = (repo, n) => `https://github.com/${repo}/issues/${n}`;

export const issueLink = (repo, n) =>
  el('a', { href: issueUrl(repo, n), target: '_blank', rel: 'noopener', textContent: `#${n}` });

// A sentence broken into its prose and its `#N` runs, in order — the one place the
// page decides what counts as naming an issue. The board's SVG text splits with it too.
export const splitRefs = (text) => String(text ?? '').split(/(#\d+)/).filter((part) => part !== '');
export const isRef = (part) => /^#\d+$/.test(part);

// A sentence that names issues or pull requests by number, as nodes — the prose
// between the numbers unchanged, each number a link. Callers that would have passed a
// string to `textContent` pass this as children instead. With no repo to link against
// — the fleet page, where a number belongs to no one member — it is the sentence.
export const refNodes = (repo, text) => (repo
  ? splitRefs(text).map((part) => (isRef(part) ? issueLink(repo, part.slice(1)) : part))
  : [String(text ?? '')]);

// The whole queue as ONE URL, so the reader can work it where they act on it rather
// than a slip at a time. GitHub has no syntax for "these issue numbers", but a bare
// number is a search term an issue's own number matches, which is what narrows the
// listing to the set. Inside one repo that is the repo's own issues listing; across
// members it is the cross-repository search, every member named. Null when nothing in
// the queue carries a number — a repo-level fault has none.
// A GitHub issue search on one repo, from its terms. One encoder, because the board's
// cell links and the queue's `see all` are the same URL with different terms in it.
export const searchUrl = (repo, terms) =>
  `https://github.com/${repo}/issues?q=${encodeURIComponent(terms.join(' ')).replace(/%20/g, '+')}`;

export const queueUrl = (candidates) => {
  const numbered = (candidates ?? []).filter((c) => c?.number != null);
  if (!numbered.length) return null;
  const numbers = numbered.map((c) => String(c.number));
  const repos = [...new Set(numbered.map((c) => c.repo))];
  const q = (terms) => encodeURIComponent(terms.join(' ')).replace(/%20/g, '+');
  return repos.length === 1
    ? searchUrl(repos[0], ['is:issue', 'state:open', ...numbers])
    : `https://github.com/search?type=issues&q=${q(['is:issue', 'state:open', ...repos.map((r) => `repo:${r}`), ...numbers])}`;
};

export const repoLink = (repo) =>
  el('a', { href: `https://github.com/${repo}`, target: '_blank', rel: 'noopener', textContent: repo });

// --- the lead block --------------------------------------------------------------

// The one thing to do, as a card. Both pages render it from the same function so the
// fleet's prod and a repo's prod are the same object said the same way.
//
// It NAMES the work rather than counting it: a count is something to read, and a
// title with a link is something to open. The severity is the card's left edge rather
// than its words, so an uncoloured card is legible as "nothing is red" at a glance.
//
// `candidate` is `next-work.mjs`'s pick, or null. `rest` is how many candidates sit
// behind it — one piece of work is a prod, and the queue behind it is context.
//
// `minutes` is priced by the CALLER, from the attention estimate's own rates: this
// module renders a figure and never computes one, so the card cannot publish a number
// the tiles below would total differently. `note` is whatever that pricing has to
// disclaim about it.
export function leadCard(candidate, { rest = 0, onRepo = null, minutes = null, note = null } = {}) {
  if (!candidate) {
    return el('div', { className: 'chart-card lead-card lvl-ok' }, [
      el('div', { className: 'lead-why', textContent: 'Nothing is waiting on you.' }),
      el('div', { className: 'sub', textContent: 'Nothing read here is parked, failing or behind — the panels below are the picture, not a list to work through.' }),
    ]);
  }

  const where = [];
  if (onRepo) {
    where.push(el('a', {
      href: `?repo=${encodeURIComponent(candidate.repo)}`,
      className: 'name',
      textContent: candidate.repo,
      onclick: (e) => { e.preventDefault(); onRepo(candidate.repo); },
    }));
  } else {
    where.push(el('span', { className: 'name', textContent: candidate.repo }));
  }
  if (candidate.number != null) {
    where.push(issueLink(candidate.repo, candidate.number));
    if (candidate.key) where.push(el('span', { className: 'sub', textContent: candidate.key }));
  }

  // What it costs a person, beside the reason rather than under it: "one item is
  // parked" is a fact, "fifteen minutes" is a decision about the next fifteen minutes.
  // Work the estimate does not cover — a broken scheduler is not a queue to get
  // through — says so rather than showing a zero.
  const cost = minutes != null
    ? el('div', { className: 'lead-est', title: note ?? '' }, [
      el('span', { className: 'v', textContent: `${minutes} min` }),
      el('span', { className: 'k', textContent: note ? 'of your time, at least' : 'of your time' }),
    ])
    : el('div', { className: 'lead-est none' }, [
      el('span', { className: 'v', textContent: '—' }),
      el('span', { className: 'k', textContent: 'no time estimate' }),
    ]);

  const kids = [
    el('div', { className: 'lead-top' }, [
      el('div', { className: 'lead-why', textContent: candidate.why }),
      cost,
    ]),
    el('div', { className: 'lead-where' }, where),
  ];
  if (candidate.title) kids.push(el('div', { className: 'sub', textContent: candidate.title }));

  // How long it has been wrong, when the item knows — an idle time is the queue's own
  // measure and is absent on a repo-level fault, which is a state rather than an event.
  const detail = [
    candidate.idleMs != null ? `untouched for ${duration(candidate.idleMs)}` : null,
    ...candidate.more,
  ].filter(Boolean);
  if (detail.length) kids.push(el('div', { className: 'sub', textContent: detail.join(' · ') }));

  kids.push(el('a', {
    className: 'lead-act', href: candidate.url, target: '_blank', rel: 'noopener',
    textContent: candidate.number != null ? `Open #${candidate.number}` : 'Open the repo',
  }));
  if (rest > 0) {
    kids.push(el('div', { className: 'sub', textContent: `${rest} more after this one.` }));
  }
  return el('div', { className: `chart-card lead-card lvl-${candidate.level}` }, kids);
}

// --- counting up ----------------------------------------------------------------

// A fleet load repaints on EVERY member's read landing, so a headline number is
// rebuilt a dozen times in a couple of seconds and each rebuild replaces the digits
// outright. The eye reads that as flicker rather than as arrival: you cannot tell
// whether 7 became 9 or whether two different numbers were drawn.
//
// So a number that CHANGED tweens from what the reader was last shown to what it is
// now, and only that. A first paint has nothing to count from and simply appears; a
// value that did not move is not re-animated; anything non-numeric — `3/12`, `—` —
// has nothing to interpolate and is set outright.
//
// `key` is the identity across paints, because the node is not: `replaceChildren`
// discards the element the last tween was writing into. It is the tile's own label,
// which is what makes two tiles two counters.
const displayed = new Map();
const inFlight = new Map();

const COUNT_MS = 400;

// Motion here is decoration on a page whose job is fault-finding, so a reader who has
// asked the platform for less of it gets the number and none of the movement.
const stillness = () => typeof matchMedia === 'function'
  && matchMedia('(prefers-reduced-motion: reduce)').matches;

// Decelerating, so the counter reads as landing on its value rather than as stopping
// mid-climb.
const ease = (t) => 1 - (1 - t) ** 3;

export function countUp(node, key, value) {
  const stop = inFlight.get(key);
  if (stop) { stop(); inFlight.delete(key); }

  const to = Number(value);
  const from = displayed.get(key);
  const set = (n) => { node.textContent = String(n); };

  if (!Number.isFinite(to) || typeof requestAnimationFrame !== 'function' || stillness()) {
    set(value);
    displayed.set(key, Number.isFinite(to) ? to : null);
    return node;
  }
  if (!Number.isFinite(from) || from === to) {
    set(to);
    displayed.set(key, to);
    return node;
  }

  const t0 = performance.now();
  let frame = 0;
  const step = (t) => {
    // Clamped at BOTH ends. A rAF callback is handed the timestamp of the frame it
    // belongs to, which can predate the `performance.now()` taken while scheduling it
    // — and a fractionally negative progress run through an ease that overshoots
    // backwards drew a counter passing through −12 on its way up from zero.
    const p = Math.min(1, Math.max(0, (t - t0) / COUNT_MS));
    const at = Math.round(from + (to - from) * ease(p));
    set(at);
    displayed.set(key, p < 1 ? at : to);
    if (p < 1) frame = requestAnimationFrame(step);
    else inFlight.delete(key);
  };
  // The landing value is recorded up front, so a tween cut short by the next paint
  // still leaves the counter's memory on the number that paint asked for.
  displayed.set(key, from);
  set(from);
  frame = requestAnimationFrame(step);
  inFlight.set(key, () => cancelAnimationFrame(frame));
  return node;
}

// Forget every counter — the next paint's numbers then appear rather than climb.
// What a cleared cache and a switched view have in common: the figures after are not
// a continuation of the figures before, and tweening between them would draw a
// change that did not happen.
export const resetCountUps = () => { for (const stop of inFlight.values()) stop(); inFlight.clear(); displayed.clear(); };

// --- tiles ----------------------------------------------------------------------

// `[value, label, color?, hint?]`. Colour is applied only when the tile is reporting
// something — a zero never gets an alarm colour, so a coloured tile always means
// "look at this". `hint` may be nodes rather than a string where the tile itemises
// what its number is made of.
export function tiles(node, rows) {
  node.replaceChildren(...rows.map(([v, k, color, hint]) => el('div', { className: 'tile' }, [
    countUp(el('div', { className: 'v num', style: color ? `color:${color}` : '' }), k, v),
    el('div', { className: 'k', textContent: k }),
    hint == null || hint === '' ? null
      : (typeof hint === 'string' ? el('div', { className: 'sub', textContent: hint }) : hint),
  ])));
}

// --- grouped table heads --------------------------------------------------------

// A header BAND above the column names, so a wide row reads as a few questions rather
// than as a wall of columns. `groups` is `[title, [col, …]]`; a group whose title is
// empty spans its columns unlabelled, which is what the identity column at the left
// edge wants — it belongs to no question.
//
// `group-band`, not `band`: the sheet's own band ([`sheet.mjs`](sheet.mjs)) is a grid
// component, and a `<tr>` that matched it was laid out as a two-column grid — which
// drops `colSpan` on the floor and stacks the titles on top of each other.
export const groupedHead = (table, groups) => {
  table.replaceChildren();
  table.append(el('thead', {}, [
    el('tr', { className: 'group-band' }, groups.map(([title, cols]) =>
      el('th', { colSpan: cols.length, className: title ? 'group' : 'group blank', textContent: title }))),
    el('tr', {}, groups.flatMap(([, cols], gi) => cols.map((c, ci) =>
      el('th', { className: ci === 0 && gi > 0 ? 'group-start' : '', textContent: c })))),
  ]));
  return table.appendChild(el('tbody'));
};

export const columnCount = (groups) => groups.reduce((n, [, cols]) => n + cols.length, 0);

// Which cells start a group, so the body can carry the same vertical rule the band
// draws. Returns the flat column indexes a `groupedHead(groups)` would open a group at.
export const groupStarts = (groups) => {
  const out = [];
  let i = 0;
  for (const [gi, [, cols]] of groups.entries()) {
    if (gi > 0) out.push(i);
    i += cols.length;
  }
  return out;
};

// --- the day chart --------------------------------------------------------------

// A stacked column per day. SVG rather than divs because the whole point is comparing
// heights across a fortnight, and one element per segment with a `<title>` gives the
// hover text for free.
//
// The scale is stated, never implied: an unlabelled column chart invites reading two
// panels' bars against each other when their maxima differ. `unit` is what one column
// spans, so the peak of an hourly chart reads per hour rather than per day.
export function stackedColumns(days, series, { height = 84, label = (d) => d.day, detail = null, unit = 'day' } = {}) {
  const NS = 'http://www.w3.org/2000/svg';
  const svgEl = (tag, attrs = {}) => {
    const n = document.createElementNS(NS, tag);
    for (const [k, v] of Object.entries(attrs)) n.setAttribute(k, String(v));
    return n;
  };
  // `append` returns nothing, so the title is built and filled before it goes in.
  const titled = (node, text) => {
    const t = svgEl('title');
    t.textContent = text;
    node.append(t);
    return node;
  };

  const totals = days.map((d) => series.reduce((n, s) => n + (s.value(d) || 0), 0));
  const peak = Math.max(1, ...totals);
  const cols = Math.max(1, days.length);
  const width = 100;                       // a viewBox unit grid; the CSS sizes it
  // The gap is a FRACTION of a column, never a fixed 3 units: at a fortnight of columns
  // three units is a hairline, and at two days of hourly ones the gaps alone exceed the
  // chart and every bar is drawn at a negative width. Scaling it keeps the same look at
  // fourteen columns and keeps the arithmetic valid at any count.
  const gap = Math.min(3, (width / cols) * 0.25);
  const colW = (width - gap * (cols - 1)) / cols;

  const svg = svgEl('svg', {
    viewBox: `0 0 ${width} ${height}`, preserveAspectRatio: 'none',
    class: 'chart', role: 'img',
    'aria-label': `${days.length} ${unit}s, peak ${peak} on ${days.length ? label(days[totals.indexOf(peak)]) : '—'}`,
  });

  // What a column is ABOUT, appended to every segment's hover. A stacked bar answers
  // "how much"; the reader's next question is always "of what", and on this page that
  // means the task names behind the hour — which the tooltip can carry and the chart
  // itself has no room for.
  const more = (d) => {
    const text = detail?.(d);
    return text ? `\n${text}` : '';
  };

  days.forEach((d, i) => {
    const x = i * (colW + gap);
    let y = height;
    let drawn = false;
    for (const s of series) {
      const v = s.value(d) || 0;
      if (!v) continue;
      const h = (v / peak) * (height - 2);
      y -= h;
      drawn = true;
      svg.append(titled(svgEl('rect', { x, y, width: colW, height: h, fill: s.color, class: 'col' }),
        `${label(d)} — ${v} ${s.label}${more(d)}`));
    }
    if (!drawn) {
      // An empty column is not one thing. A period that was READ and held nothing is a
      // floor line; one nothing has answered for yet is left blank, because drawing it
      // at the floor would claim a quiet hour the page cannot vouch for.
      const unread = d.source === 'none';
      svg.append(titled(svgEl('rect', {
        x, y: height - 1, width: colW, height: 1, fill: unread ? 'transparent' : 'var(--rule)',
      }), `${label(d)} — ${unread ? 'not read' : 'nothing'}${more(d)}`));
    }
  });

  return el('div', { className: 'chart-wrap' }, [
    svg,
    el('div', { className: 'chart-axis' }, [
      el('span', { className: 'sub', textContent: days.length ? label(days[0]) : '' }),
      el('span', { className: 'sub', textContent: `peak ${peak}/${unit}` }),
      el('span', { className: 'sub', textContent: days.length ? label(days[days.length - 1]) : '' }),
    ]),
  ]);
}

export const chartLegend = (series) =>
  el('div', { className: 'legend' }, series.map((s) =>
    el('span', {}, [el('i', { className: 'sw', style: `background:${s.color}` }), s.label])));

// --- two series, two scales -------------------------------------------------------

// A line per series, each scaled to ITS OWN maximum, with both maxima printed. Check
// runs are two figures a day and the runs that caught something are single ones; on a
// shared axis the second line is the x-axis and the panel says nothing. On separate
// scales it says what each is doing over the month, which is the only question this
// panel is for.
//
// THE SCALES ARE STATED, never implied. Two lines at the same height mean nothing
// alike, so each axis label carries its series' colour and its own peak, and the panel
// is the one place on this page where a reader must not compare heights across lines.
//
// A NULL BREAKS THE LINE. A day the fold had no opinion on is not a day with zero of
// something — a repo that had not folded yet, a shallow checkout that could not see
// that far — and drawing it at the floor would be the page inventing a quiet day.
export function dualAxisChart(rows, left, right, { height = 96, label = (r) => r.day } = {}) {
  const NS = 'http://www.w3.org/2000/svg';
  const svgEl = (tag, attrs = {}) => {
    const n = document.createElementNS(NS, tag);
    for (const [k, v] of Object.entries(attrs)) n.setAttribute(k, String(v));
    return n;
  };

  const width = 100;                       // a viewBox unit grid; the CSS sizes it
  const step = rows.length > 1 ? width / (rows.length - 1) : width;
  const svg = svgEl('svg', {
    viewBox: `0 0 ${width} ${height}`, preserveAspectRatio: 'none', class: 'chart dual', role: 'img',
    'aria-label': `${left.label} and ${right.label} over ${rows.length} days, each on its own scale`,
  });

  const peaks = [left, right].map((s) => rows.reduce((n, r) => Math.max(n, s.value(r) ?? 0), 0));

  [left, right].forEach((s, si) => {
    const peak = peaks[si] || 1;
    let run = [];
    const flush = () => {
      if (run.length > 1) {
        svg.append(svgEl('path', {
          d: run.map((p, i) => `${i ? 'L' : 'M'}${p.x.toFixed(2)},${p.y.toFixed(2)}`).join(' '),
          class: 'series-line', stroke: s.color,
        }));
      } else if (run.length === 1) {
        // A single read day surrounded by unread ones still has to be visible: a line
        // needs two points, so it is drawn as a dot rather than dropped.
        svg.append(svgEl('circle', { cx: run[0].x.toFixed(2), cy: run[0].y.toFixed(2), r: 1.2, fill: s.color }));
      }
      run = [];
    };
    rows.forEach((r, i) => {
      const v = s.value(r);
      if (v === null || v === undefined) { flush(); return; }
      run.push({ x: i * step, y: height - 2 - (v / peak) * (height - 6) });
    });
    flush();
  });

  const axis = (s, peak, cls) => el('span', { className: `sub axis ${cls}` }, [
    el('i', { className: 'sw', style: `background:${s.color}` }),
    `${s.label} · peak ${s.format ? s.format(peak) : peak}/day`,
  ]);

  return el('div', { className: 'chart-wrap' }, [
    el('div', { className: 'chart-axis' }, [axis(left, peaks[0], 'left'), axis(right, peaks[1], 'right')]),
    svg,
    el('div', { className: 'chart-axis' }, [
      el('span', { className: 'sub', textContent: rows.length ? label(rows[0]) : '' }),
      el('span', { className: 'sub', textContent: rows.length ? label(rows[rows.length - 1]) : '' }),
    ]),
  ]);
}

// --- moving between views ----------------------------------------------------------

// FLIP, hand-rolled: measure where each row is, repaint, then animate it from where it
// WAS to where it is now. The page carries no dependencies and this is about forty
// lines, so it stays that way.
//
// Why animate at all on a fault-finding page: switching between stuck / pending / all
// is not a new table, it is the SAME rows filtered — and a repaint that swaps one wall
// of text for another makes the reader re-find the row they were looking at. Watching
// it move keeps it identified.
//
// Rows are matched across the repaint by `data-k`, because the node is not the same
// node: the repaint replaces the body outright.
//
// A row that LEAVES simply goes. A `<tr>` cannot be animated out of a table's flow
// without collapsing the layout around it, and a half-height ghost row is worse than a
// clean removal — the arriving and moving rows already carry the change.
//
// Motion is decoration on a page whose job is fault-finding, so a reader who asked the
// platform for less of it gets the repaint and none of the movement.
const MOVE_MS = 260;
const ENTER_MS = 200;

export function flipRows(body, paint) {
  const before = new Map();
  if (typeof body.getBoundingClientRect === 'function') {
    for (const row of body.children) {
      if (row.dataset?.k) before.set(row.dataset.k, row.getBoundingClientRect().top);
    }
  }

  paint();

  if (stillness() || typeof Element === 'undefined' || typeof Element.prototype.animate !== 'function') return;
  for (const row of body.children) {
    const k = row.dataset?.k;
    const from = k === undefined ? undefined : before.get(k);
    if (from === undefined) {
      row.animate([{ opacity: 0, transform: 'translateY(6px)' }, { opacity: 1, transform: 'none' }],
        { duration: ENTER_MS, easing: 'ease-out' });
      continue;
    }
    const dy = from - row.getBoundingClientRect().top;
    if (Math.abs(dy) < 1) continue;
    row.animate([{ transform: `translateY(${dy}px)` }, { transform: 'none' }],
      { duration: MOVE_MS, easing: 'cubic-bezier(.2,.7,.3,1)' });
  }
}

// --- windowed figures -----------------------------------------------------------

// A number with its change against the window before it. The arrow is never the whole
// message — the previous window's figure is spelled out, because a delta with nothing
// to compare it against is the vanity total this panel exists to avoid.
//
// Which DIRECTION is good is the caller's to say: more completed work is progress and
// more items needing a person is not, and a green up-arrow on the second would read as
// a boast about the fleet needing more hand-holding.
export function windowFigure(value, label, change, note, { better = 'up' } = {}) {
  const arrow = change?.dir === 'up' ? '▲' : change?.dir === 'down' ? '▼' : '—';
  const sense = !change || change.dir === 'flat' ? 'flat' : (change.dir === better ? 'good' : 'bad');
  return el('div', { className: 'tile' }, [
    countUp(el('div', { className: 'v num' }), label, value),
    el('div', { className: 'k', textContent: label }),
    change
      ? el('div', { className: `sub delta ${sense}`, textContent: `${arrow} ${change.by} vs the week before` })
      : null,
    note ? el('div', { className: 'sub', textContent: note }) : null,
  ]);
}
