// The ledger sheet's render primitives — the marks the identity
// ([docs/visual-identity.md](../../docs/visual-identity.md)) names, once each.
//
// ONE TICK VOCABULARY. The heartbeat, the wake strip, the pulse and the sparklines are
// the same square mark at four scales, so they are one function each rather than four
// chart types: a count is always a number and never a height, and the past is washed,
// now is solid and the future is hollow on every timeline here.
//
// NOTHING DECIDES ANYTHING. Every verdict — which delta is tinted, which square is
// critical, what a gap's sentence says — arrives already made, from
// [`fleet-ledger.mjs`](../derive/fleet-ledger.mjs) and [`fleet.mjs`](../derive/fleet.mjs). This file turns
// those into nodes.

import { el, refNodes } from './ui.mjs';

const NS = 'http://www.w3.org/2000/svg';

export const svgEl = (tag, attrs = {}) => {
  const n = document.createElementNS(NS, tag);
  for (const [k, v] of Object.entries(attrs)) if (v !== null && v !== undefined) n.setAttribute(k, String(v));
  return n;
};

const titled = (node, text) => {
  if (!text) return node;
  const label = svgEl('title');
  label.textContent = text;
  node.append(label);
  return node;
};

// A band of the sheet: the stub column naming it, its question in the small italic
// step, and its body. Bands are the sheet's only structure — there are no cards.
export function band(label, question, body, { className = '', aria = null } = {}) {
  return el('section', { className: `band ${className}`.trim(), ...(aria ? { ariaLabel: aria } : {}) }, [
    el('div', { className: 'stub' }, [
      el('span', { className: 'cap', textContent: label }),
      question ? el('span', { className: 'q', textContent: question }) : null,
    ]),
    el('div', { className: 'body' }, [].concat(body)),
  ]);
}

// The one object that sits ON the sheet, on warm paper, because it is the one thing
// addressed to the person. Nothing in it clips: it wraps.
//
// `queue` is what is behind this one, as something to USE rather than a count to read:
// the page cannot know the verdict the reader just reached on the candidate in front of
// them, so the next one has to be reachable without acting on this one. `seeAll` is the
// same queue as one link out — `queueUrl`'s.
export function slip({ headline, where, href, chip: chipText, more, queue = null, seeAll = null }) {
  const nav = queue && queue.total > 1
    ? el('span', { className: 'nav' }, [
      stepButton('‹', 'the one before this', queue, queue.index - 1),
      el('span', { className: 'at', textContent: `${queue.index + 1} / ${queue.total}` }),
      stepButton('›', 'the next one', queue, queue.index + 1),
      seeAll ? el('a', {
        className: 'see-all', href: seeAll, target: '_blank', rel: 'noopener',
        textContent: `see all ${queue.total}`,
      }) : null,
    ])
    : null;
  return el('div', { className: 'slip' }, [
    el('span', { className: 'hl', textContent: headline }),
    where ? el('a', { className: 'where', href: href ?? '#', target: '_blank', rel: 'noopener', textContent: where }) : null,
    chipText ? el('span', { className: 'chip', textContent: chipText }) : null,
    more ? el('span', { className: 'more', textContent: more }) : null,
    nav,
  ]);
}

// One step of the queue. It names the candidate it wants rather than a direction, so
// the page repaints from an index and holds no cursor of its own.
function stepButton(glyph, label, queue, to) {
  const button = el('button', {
    className: 'step', type: 'button', textContent: glyph, title: label,
    disabled: to < 0 || to >= queue.total,
    onclick: () => queue.onStep(to),
  });
  button.setAttribute('aria-label', label);
  return button;
}

// One of the machine's five cells: a status square, its label in condensed caps, the
// figure in mono with its unit in the text column, and one line naming the worst
// member — a name is what the reader acts on.
//
// `alarm` sets the figure in the critical colour and in bold. It is for a fault the
// caller has already judged FLEET-WIDE, never for a cell that merely reached its worst
// verdict: a row where every cell can shout has no top signal left.
export function machineCell({ level, label, value, unit, note, extra = null, alarm = false }) {
  return el('div', { className: alarm ? 'cell alarm' : 'cell' }, [
    el('div', { className: 'k' }, [
      el('i', { className: `sq ${level ?? 'none'}` }),
      el('span', { className: 'cap', textContent: label }),
    ]),
    el('div', { className: 'v' }, [
      el('span', { className: value === null ? 'gap' : '', textContent: value === null ? '—' : String(value) }),
      unit ? el('span', { className: 'u', textContent: unit }) : null,
    ]),
    extra,
    note ? el('div', { className: 'n', textContent: note }) : null,
  ]);
}

// The heartbeat: one square per member, in the grid's own order. Colour is the verdict
// the caller already reached; the hover carries the member and its age.
export function beats(members) {
  return el('div', { className: 'beat' }, members.map((m) =>
    el('i', { className: m.level === 'good' ? '' : m.level, title: m.title })));
}

// The 24-hour wake strip. One tick per anchor, bucketed by hour, its height the bucket's
// count capped so a busy hour cannot dwarf the rest — the count itself is in the hover,
// never read off the height.
export function wakeTicks(strip, { width = 240, height = 30 } = {}) {
  const svg = svgEl('svg', {
    viewBox: `0 0 ${width} ${height}`, preserveAspectRatio: 'none',
    role: 'img', 'aria-label': 'Scheduled task wakes over the next 24 hours',
  });
  const base = height - 11;
  svg.append(svgEl('line', { x1: 0, y1: base, x2: width, y2: base, stroke: 'var(--ledger)', 'stroke-width': 1 }));
  const slot = width / (strip.hours.length || 1);
  const peak = Math.max(1, strip.peak);
  for (const [i, hour] of strip.hours.entries()) {
    for (const [j, task] of hour.tasks.entries()) {
      const h = Math.max(4, Math.round((hour.tasks.length / peak) * (base - 1)));
      svg.append(titled(svgEl('rect', {
        x: Math.round(i * slot + 1 + j * 4), y: base - h, width: 2.5, height: h,
        fill: task.held ? 'var(--critical)' : 'var(--machine)',
      }), `${hour.hour.slice(11)}:00 — ${task.repo ? `${task.repo} ` : ''}${task.key}${task.held ? ' (held)' : ''}`));
    }
  }
  const label = (x, text, anchor) => {
    const t = svgEl('text', { x, y: height - 1, ...(anchor ? { 'text-anchor': anchor } : {}) });
    t.textContent = text;
    return t;
  };
  svg.append(label(0, `now ${strip.from?.slice(11) ?? ''}:00`));
  svg.append(label(width, '+24h', 'end'));
  return el('div', { className: 'wake' }, [svg]);
}

// A ledger row: figure · text · delta · spark, on the fixed tracks that make the three
// columns line up down the whole sheet. The alignment IS the design.
export function figureRow(fig, { format = String, repo = null } = {}) {
  const value = fig.value === null
    ? el('div', { className: 'v gap', textContent: '—' })
    : el('div', { className: 'v', textContent: format(fig.value) });
  return el('div', { className: 'fig' }, [
    value,
    el('div', { className: 't' }, [
      el('span', { className: 'u', textContent: fig.unit }),
      // A gap is stated in the muted step ON ITS OWN LINE, and it REPLACES the
      // sub-line rather than joining it: a figure with no number has nothing for a
      // second actionable figure to sit beside, and appending the sentence to the
      // unit is what makes the row three lines tall and the ledger's tracks stop
      // lining up.
      fig.gap
        ? el('span', { className: 's gap', textContent: fig.gap })
        : (fig.sub ? el('span', { className: 's' }, refNodes(repo, fig.sub)) : null),
    ]),
    deltaCell(fig, format),
    el('div', { className: 'sp' }, fig.spark ? [sparkline(fig.spark)] : []),
  ]);
}

// A signed delta in mono, with the base under it. Set in ink UNLESS the figure's own
// bad-when rule fired: a merely-down week and a merely-up lead time are figures, not
// verdicts, and nothing green is coloured because nothing green needs a person.
export function deltaCell(fig, format = String) {
  if (fig.delta === null) return el('div', { className: 'd gap', textContent: '—' });
  const sign = fig.delta > 0 ? '+' : fig.delta < 0 ? '−' : '±';
  return el('div', { className: `d ${fig.bad ? 'bad' : ''}`.trim() }, [
    `${sign}${format(Math.abs(fig.delta))}`,
    el('span', { className: 'vs', textContent: `vs ${format(fig.previous)}` }),
  ]);
}

// Fourteen marks at row scale: the previous window dimmed, this window in the
// machine's blue, and NO MARK AT ALL for a day nobody folded — a blank, never a floor.
export function sparkline(series, { width = 74, height = 28 } = {}) {
  const svg = svgEl('svg', { viewBox: `0 0 ${width} ${height}`, 'aria-hidden': 'true' });
  const known = series.map((d) => d.value).filter((n) => typeof n === 'number');
  const peak = known.length ? Math.max(...known, 1) : 1;
  const slot = width / (series.length || 1);
  const barW = Math.max(2, slot - 1);
  const half = Math.floor(series.length / 2);
  for (const [i, day] of series.entries()) {
    if (typeof day.value !== 'number') continue;
    const h = Math.max(1, Math.round((day.value / peak) * height));
    svg.append(titled(svgEl('rect', {
      x: Math.round(i * slot), y: height - h, width: barW, height: h,
      fill: i < half ? 'var(--dim)' : 'var(--machine)',
    }), `${day.day} · ${day.value}`));
  }
  return svg;
}

// The block's one chart at readable size: sessions per day across the fleet. Today is a
// dashed outline because it is not folded yet, and a day no member folded is a blank.
export function pulseChart(pulse, { height = 30 } = {}) {
  const width = 1000;
  const svg = svgEl('svg', {
    viewBox: `0 0 ${width} ${height}`, preserveAspectRatio: 'none', role: 'img',
    'aria-label': 'Sessions per day across the fleet, 14 days — last week dimmed, this week in the machine\'s blue',
  });
  svg.append(svgEl('line', { x1: 0, y1: height, x2: width, y2: height, stroke: 'var(--ledger)' }));
  const slot = width / (pulse.days.length || 1);
  const barW = Math.max(4, slot * 0.62);
  const peak = Math.max(1, pulse.peak ?? 1);
  for (const [i, day] of pulse.days.entries()) {
    const x = Math.round(i * slot + (slot - barW) / 2);
    if (day.sessions === null) continue;
    const h = Math.max(1, Math.round((day.sessions / peak) * (height - 1)));
    const hover = `${day.day} · ${day.sessions} session(s)${day.members.length ? ` · ${day.members.map((r) => r.split('/')[1] ?? r).join(', ')}` : ''}`;
    const bar = day.series === 'today'
      ? svgEl('rect', {
        x, y: 1, width: barW, height: height - 2,
        fill: 'none', stroke: 'var(--muted)', 'stroke-dasharray': '3 3',
      })
      : svgEl('rect', {
        x, y: height - h, width: barW, height: h,
        fill: day.series === 'previous' ? 'var(--dim)' : 'var(--machine)',
      });
    svg.append(titled(bar, hover));
  }
  return svg;
}

// A detail under the double rule — a ruled table, never a second card.
export function detailTable(headers, rows) {
  const cell = (c, i) => {
    const spec = typeof c === 'object' && c !== null ? c : { text: String(c) };
    const td = el('td', {
      className: `${headers[i]?.num && !spec.gap ? 'num' : ''} ${spec.gap ? 'gap' : ''}`.trim(),
      textContent: spec.text,
    });
    if (spec.colSpan) td.colSpan = spec.colSpan;
    return td;
  };
  return el('table', {}, [
    el('thead', {}, [el('tr', {}, headers.map((h) =>
      el('th', { className: `cap${h.num ? ' num' : ''}`, textContent: h.label })))]),
    el('tbody', {}, rows.map((cells) => el('tr', {}, cells.map(cell)))),
  ]);
}

// The expand link the totals row carries, and the ruled table it discloses.
export function expander(label, target) {
  const button = el('button', { className: 'expand', type: 'button', textContent: `${label} ▾` });
  button.setAttribute('aria-expanded', 'false');
  button.addEventListener('click', () => {
    target.hidden = !target.hidden;
    button.setAttribute('aria-expanded', String(!target.hidden));
    button.textContent = target.hidden ? `${label} ▾` : `${label} ▴`;
  });
  return button;
}
