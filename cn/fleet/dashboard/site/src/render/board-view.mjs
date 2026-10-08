// The Work board's marks, drawn. One SVG that scales to its container; every shape here
// is named in the identity's mark table ([docs/visual-identity.md](../../docs/visual-identity.md)),
// and the decisions behind them were all made in [`board.mjs`](../derive/board.mjs).
//
// FORM BEFORE COLOUR on every mark, so the board reads in either theme and in
// greyscale: predicted and declined differ by HEIGHT rather than by a dash pattern
// invisible at three pixels, a park's kind is its glyph, and a plain issue is a diamond
// whatever state it is in.
//
// THE PAST IS WASHED, NOW IS SOLID, THE FUTURE IS HOLLOW — the same grammar the wake
// strip and the pulse obey, so one reader learns it once.

import { el, issueLink, issueUrl, searchUrl, splitRefs, isRef } from './ui.mjs';
import { svgEl } from './sheet.mjs';

const VIEW_W = 1000;
const GUTTER = 168;
const ROW_H = 36;
const HEADER_H = 30;
const PAD_TOP = 16;

const titled = (node, text) => {
  if (!text) return node;
  const label = svgEl('title');
  label.textContent = text;
  node.append(label);
  return node;
};

// Where an instant sits on the axis. Clamped to the lane, so a PR opened before the
// window still draws — with a `◂` note saying its real start is off the edge.
const scale = (axis) => (at) => {
  const span = axis.to - axis.from;
  const x = GUTTER + ((at - axis.from) / span) * (VIEW_W - GUTTER);
  return Math.max(GUTTER, Math.min(VIEW_W, x));
};

// WHERE A MARK GOES. A mark is a thing in GitHub, so clicking one opens that thing —
// it does not fill a panel elsewhere on the page for the reader to read and then click
// through anyway. What the mark is worth knowing BEFORE clicking is the hover's job.
//
// A grid cell is the only mark that can stand for more than one item. One is a link to
// it; several have no single target, so they open the search naming exactly those
// numbers — without `state:open`, since a cell of closed occurrences would match none.
// A cell with no number at all is the future, and the future has nothing to open.
function markHref(subject, repo) {
  if (!repo) return null;
  const cell = subject?.cell;
  if (cell) {
    const numbers = cell.number != null ? [cell.number] : (cell.numbers ?? []);
    if (!numbers.length) return null;
    return numbers.length === 1 ? issueUrl(repo, numbers[0]) : searchUrl(repo, numbers.map(String));
  }
  const number = String(subject?.gutter ?? '').match(/#(\d+)/)?.[1];
  return number ? issueUrl(repo, number) : null;
}

// A mark, wired: named for a reader who cannot see it, wrapped in its link when it has
// one, and told to fill the tip while the pointer is on it. `place` returns what should
// go in the SVG, which is the anchor when there is one and the mark itself otherwise.
function place(node, subject, ctx, label) {
  node.setAttribute('aria-label', label);
  // `aria-label` rather than a `<title>` child: a native tooltip on the same mark would
  // race the hover card and win, being the browser's own.
  if (ctx.tip) {
    node.addEventListener('mouseenter', (ev) => showTip(ctx, subject, ev));
    node.addEventListener('mousemove', (ev) => moveTip(ctx, ev));
    node.addEventListener('mouseleave', () => hideTip(ctx));
    node.addEventListener('focus', (ev) => showTip(ctx, subject, ev));
    node.addEventListener('blur', () => hideTip(ctx));
  }
  const href = markHref(subject, ctx.repo);
  if (!href) return node;
  const a = svgEl('a', { href, target: '_blank', rel: 'noopener', class: 'mark-link' });
  a.append(node);
  return a;
}

function showTip(ctx, subject, ev) {
  const content = ctx.tipFor(subject);
  if (!content || (Array.isArray(content) && !content.length)) return hideTip(ctx);
  ctx.tip.replaceChildren(...[].concat(content));
  ctx.tip.hidden = false;
  moveTip(ctx, ev);
}

// Follows the pointer rather than anchoring to the mark: a mark is five pixels wide and
// a card anchored to one sits under the pointer as often as beside it.
const TIP_GAP = 14;

function moveTip(ctx, ev) {
  if (ctx.tip.hidden || ev?.clientX == null || !ctx.tip.style) return;
  // Flipped to the other side of the pointer rather than clamped to the edge: a card
  // pinned against the right margin sits ON the marks it is describing.
  const box = ctx.tip.getBoundingClientRect?.() ?? { width: 0, height: 0 };
  const w = globalThis.innerWidth ?? 0;
  const h = globalThis.innerHeight ?? 0;
  const right = ev.clientX + TIP_GAP + box.width > w;
  const below = ev.clientY + TIP_GAP + box.height > h;
  ctx.tip.style.left = `${Math.max(TIP_GAP, right ? ev.clientX - TIP_GAP - box.width : ev.clientX + TIP_GAP)}px`;
  ctx.tip.style.top = `${Math.max(TIP_GAP, below ? ev.clientY - TIP_GAP - box.height : ev.clientY + TIP_GAP)}px`;
}

const hideTip = (ctx) => { ctx.tip.hidden = true; ctx.tip.replaceChildren(); };

export function renderBoard(board, { repo = null, tip = null, tipFor = () => null } = {}) {
  const ctx = { repo, tip, tipFor };
  const { axis } = board;
  const x = scale(axis);
  const rows = board.groups.reduce((n, g) => n + HEADER_H + g.shown.length * ROW_H + (g.more ? 20 : 0), 0);
  const height = PAD_TOP + rows + 12;
  const svg = svgEl('svg', {
    viewBox: `0 0 ${VIEW_W} ${height}`, class: 'board', role: 'img',
    'aria-label': 'Work board — twelve day columns, seven back and four ahead, with one lane per flow',
  });

  // --- the axis -------------------------------------------------------------------
  const nowX = x(axis.now);
  svg.append(svgEl('rect', { x: GUTTER, y: PAD_TOP - 8, width: nowX - GUTTER, height: height - PAD_TOP, fill: 'var(--wash)' }));
  for (const day of axis.days) {
    const dx = x(day.start);
    svg.append(svgEl('line', { x1: dx, y1: PAD_TOP - 8, x2: dx, y2: height - 12, stroke: 'var(--ledger)', 'stroke-width': 1 }));
    const label = svgEl('text', { x: dx + 3, y: PAD_TOP - 1, class: 'axis-day' });
    label.textContent = day.today ? 'today' : day.day.slice(5);
    svg.append(label);
    // The anchor tick — where every scheduled thing on this board sits.
    const ax = x(day.anchorAt);
    svg.append(svgEl('line', { x1: ax, y1: PAD_TOP - 6, x2: ax, y2: PAD_TOP - 2, stroke: 'var(--muted)', 'stroke-width': 1 }));
  }
  svg.append(svgEl('line', { x1: nowX, y1: PAD_TOP - 8, x2: nowX, y2: height - 12, stroke: 'var(--ink)', 'stroke-width': 1 }));
  const flag = svgEl('text', { x: nowX + 4, y: height - 3, class: 'axis-now' });
  flag.textContent = `now · ${new Date(axis.now).toISOString().slice(0, 16).replace('T', ' ')} UTC`;
  svg.append(flag);

  // --- the groups -----------------------------------------------------------------
  let y = PAD_TOP + 6;
  for (const group of board.groups) {
    // The title in the gutter, the group's ONE derived sentence in the lane. Nothing
    // else: every other count about a group is a hover.
    svg.append(text(6, y + 12, `${group.title.toUpperCase()} · ${group.count}`, 'group'));
    svg.append(text(GUTTER + 6, y + 12, group.sentence, 'group-note'));
    svg.append(svgEl('line', { x1: 0, y1: y + 18, x2: VIEW_W, y2: y + 18, stroke: 'var(--rule)' }));
    y += HEADER_H;

    for (const row of group.shown) {
      const mid = y + ROW_H / 2;
      if (group.grid) drawGridRow(svg, row, axis, x, mid, ctx);
      else drawLaneRow(svg, row, axis, x, mid, ctx);
      y += ROW_H;
    }
    if (group.more) {
      svg.append(text(GUTTER + 6, y + 12, `${group.more} more row${group.more === 1 ? '' : 's'}`, 'more'));
      y += 20;
    }
  }
  return svg;
}

function text(px, py, content, cls) {
  const t = svgEl('text', { x: px, y: py, class: cls });
  t.textContent = content;
  return t;
}

// The SVG counterpart of `refNodes`: one text element whose `#N` runs are anchors, so
// a gutter naming a PR and the issue it closes opens either one. With no repo to link
// against — a board drawn outside a member — it is the plain text element above.
function refText(px, py, content, cls, repo) {
  if (!repo) return text(px, py, content, cls);
  const t = svgEl('text', { x: px, y: py, class: cls });
  for (const part of splitRefs(content)) {
    const span = svgEl('tspan');
    span.textContent = part;
    if (!isRef(part)) { t.append(span); continue; }
    const link = svgEl('a', { href: issueUrl(repo, part.slice(1)), target: '_blank', rel: 'noopener' });
    link.append(span);
    t.append(link);
  }
  return t;
}

// A lane row: its gutter name, its marks, and — spent only on a FINDING — one line of
// text in the serious tint. Every age, count and policy sentence is in the hover.
function drawLaneRow(svg, row, axis, x, mid, ctx) {
  svg.append(refText(6, mid + 4, row.gutter, 'gutter', ctx.repo));

  for (const mark of row.marks) {
    if (mark.kind === 'bar') {
      const from = Math.max(mark.from, axis.from);
      const bar = svgEl('rect', {
        x: x(from), y: mid - 5, width: Math.max(3, x(mark.to) - x(from)), height: 10, rx: 2,
        fill: 'var(--machine-wash)', stroke: 'var(--machine)', 'stroke-width': 1, class: 'mark',
      });
      svg.append(place(bar, row, ctx, `${row.gutter} — ${row.title}, open since ${new Date(mark.from).toISOString().slice(0, 10)}`));
      if (mark.from < axis.from) svg.append(text(GUTTER - 8, mid + 4, '◂', 'edge'));
      // The one warm thing on the board, and a separate mark rather than an end-cap so
      // it is findable from across the sheet.
      if (mark.flag) {
        // Wired like the bar it caps, not merely drawn: it sits ON the bar's end, so a
        // flag the pointer can reach but the card cannot is a hole in the row's hover.
        svg.append(place(svgEl('polygon', {
          points: `${x(mark.to)},${mid - 9} ${x(mark.to) - 5},${mid - 17} ${x(mark.to) + 5},${mid - 17}`,
          fill: 'var(--you)', class: 'mark',
        }), row, ctx, `waits for a person — ${row.why}`));
      }
      continue;
    }
    if (mark.at === null) {
      // No time at all: the lane is drawn broken, from the gutter to the break.
      const bx = x(axis.now);
      svg.append(svgEl('line', {
        x1: GUTTER, y1: mid, x2: bx - 14, y2: mid,
        stroke: row.broken ? 'var(--critical)' : 'var(--muted)', 'stroke-width': 1, 'stroke-dasharray': '4 3',
      }));
      svg.append(text(bx - 10, mid + 4, '»', row.broken ? 'break' : 'edge'));
      const node = svgEl('circle', {
        cx: bx + 6, cy: mid, r: 5, fill: 'none', stroke: 'var(--ink-2)', 'stroke-dasharray': '2 2', class: 'mark',
      });
      svg.append(place(node, row, ctx, `${row.gutter} — ${row.why}`));
      continue;
    }
    const cx = x(mark.at);
    let node;
    if (mark.kind === 'running') {
      node = svgEl('circle', { cx, cy: mid, r: 5, fill: 'var(--machine)', class: 'mark' });
    } else if (mark.kind === 'park') {
      node = parkMark(cx, mid, mark.park);
    } else {
      node = svgEl('circle', { cx, cy: mid, r: 5, fill: 'none', stroke: 'var(--ink-2)', 'stroke-width': 1.4, class: 'mark' });
    }
    svg.append(place(node, row, ctx, `${row.gutter} — ${row.title}. ${row.why}`));
  }

  if (row.finding) svg.append(refText(GUTTER + 6, mid + 16, row.finding, 'finding', ctx.repo));
}

// A park's KIND is its glyph, so the four are told apart without colour.
const PARK_GLYPH = { action: '×', approval: '–', decision: '?', failure: '' };
function parkMark(cx, cy, kind) {
  const g = svgEl('g', { class: 'mark' });
  const hue = kind === 'approval' ? 'var(--you)' : kind === 'failure' ? 'var(--critical)' : 'var(--serious)';
  g.append(svgEl('rect', {
    x: cx - 6, y: cy - 6, width: 12, height: 12,
    fill: kind === 'failure' ? 'var(--wash)' : 'none', stroke: hue, 'stroke-width': 1.4,
  }));
  if (kind === 'failure') {
    // Hatched, so the worst park is distinguishable with no colour at all.
    for (let i = -6; i <= 6; i += 4) {
      g.append(svgEl('line', { x1: cx + i, y1: cy - 6, x2: cx + i + 6, y2: cy + 6, stroke: hue, 'stroke-width': 0.8 }));
    }
  } else {
    const glyph = svgEl('text', { x: cx, y: cy + 4, class: 'glyph', fill: hue, 'text-anchor': 'middle' });
    glyph.textContent = PARK_GLYPH[kind] ?? '?';
    g.append(glyph);
  }
  return g;
}

// A task's row in the schedule grid: one cell per day, at the day's anchor.
const CELL_W = 18;
const CELL_H = 12;

function drawGridRow(svg, row, axis, x, mid, ctx) {
  svg.append(text(6, mid + 4, row.task, 'gutter'));
  for (const cell of row.cells) {
    const day = axis.days.find((d) => d.day === cell.day);
    if (!day || cell.state === 'none') continue;
    const cx = x(day.anchorAt);
    const subject = { ...row, kind: 'task', cell };
    const label = `${row.task} · ${cell.day} · ${cell.state}${cell.count > 1 ? ` ×${cell.count}` : ''}`;
    svg.append(place(gridCell(cell, cx, mid), subject, ctx, label));
    if (cell.count > 1) {
      const n = svgEl('text', {
        x: cx, y: mid + 3.5, class: 'cell-count', 'text-anchor': 'middle',
        fill: cell.state === 'ran' ? 'var(--sheet)' : 'var(--ink-2)',
      });
      n.textContent = String(cell.count);
      svg.append(n);
    }
  }
}

function gridCell(cell, cx, cy) {
  const g = svgEl('g', { class: 'mark' });
  const box = (attrs) => svgEl('rect', { x: cx - CELL_W / 2, y: cy - CELL_H / 2, width: CELL_W, height: CELL_H, ...attrs });
  switch (cell.state) {
    case 'ran': return g.append(box({ fill: 'var(--good)' })), g;
    case 'declined': return g.append(box({ fill: 'none', stroke: 'var(--muted)' })), g;
    case 'parked': return g.append(box({ fill: 'var(--you)' })), g;
    case 'running': return g.append(svgEl('circle', { cx, cy, r: 5, fill: 'var(--machine)' })), g;
    case 'predicted': return g.append(box({ fill: 'none', stroke: 'var(--machine)' })), g;
    // Half-height, never a dash: at three pixels a dash pattern is invisible.
    case 'will-decline':
      g.append(svgEl('rect', { x: cx - CELL_W / 2, y: cy - CELL_H / 4, width: CELL_W, height: CELL_H / 2, fill: 'none', stroke: 'var(--muted)' }));
      return g;
    case 'failure-park':
    default: {
      g.append(box({ fill: 'var(--wash)', stroke: 'var(--critical)', 'stroke-width': 1.2 }));
      for (let i = -CELL_W / 2; i <= CELL_W / 2; i += 4) {
        g.append(svgEl('line', { x1: cx + i, y1: cy - CELL_H / 2, x2: cx + i + CELL_H, y2: cy + CELL_H / 2, stroke: 'var(--critical)', 'stroke-width': 0.7 }));
      }
      return g;
    }
  }
}

// The quiet tail: one ruled line, not a group, with the three counts that matter and a
// disclosure naming the issues behind them.
export function quietLine(quiet, { repo = null } = {}) {
  const line = (i) => ` · ${i.title} — idle ${i.idleDays} d${i.quickWin ? ' · quick-win' : ''}${i.needsDecision ? ' · needs-decision (a decision park by another name)' : ''}`;
  const list = el('div', { className: 'detail quiet-list', hidden: true }, [
    el('ul', {}, quiet.items.slice(0, 40).map((i) => el('li', {}, [
      repo ? issueLink(repo, i.number) : `#${i.number}`,
      line(i),
    ]))),
  ]);
  const button = el('button', { className: 'expand', type: 'button', textContent: 'show ▾' });
  button.setAttribute('aria-expanded', 'false');
  button.addEventListener('click', () => {
    list.hidden = !list.hidden;
    button.setAttribute('aria-expanded', String(!list.hidden));
    button.textContent = list.hidden ? 'show ▾' : 'show ▴';
  });
  return el('div', { className: 'quiet' }, [
    el('div', { className: 'quiet-head' }, [
      el('span', { textContent: `${quiet.total} issue${quiet.total === 1 ? '' : 's'}, quiet — plain, on no edge` }),
      el('span', { className: 'n', textContent: `${quiet.rotting} rotting · ${quiet.quickWin} quick-win · ${quiet.needsDecision} needs-decision` }),
      quiet.total ? button : null,
    ]),
    list,
  ]);
}
