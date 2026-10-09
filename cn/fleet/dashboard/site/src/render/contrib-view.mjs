// Rendering for pack-contributed metrics — the DOM half of `contributions.mjs`.
//
// A module of its own so the import graph stays a DAG: `contributions.mjs` composes
// display PARTS and needs `duration` from the shared vocabulary, so it depends on
// `ui.mjs`; these renderers need both, so they sit downstream of the two rather than
// inside either. Putting them in `ui.mjs` would have made the vocabulary depend on
// one surface's contract, and putting them in the views would have written them twice.
import {
  el, ago, duration,
} from './ui.mjs';
import {
  valueOf, windowDelta, listItems, valuesPath,
} from '../read/contributions.mjs';

// --- pack-contributed metrics -----------------------------------------------------

// Everything below renders what a PACK said about a repo, and every one of these
// renderers takes PARTS rather than text. That is the point of the contract: a pack
// supplies a value, a noun and a span, and the page decides how they are set. A pack
// that handed over a finished string would arrive flat, and no amount of styling
// afterwards could tell its number from its grammar.

// A pack's mark. Derived from the id rather than fetched: a pack ships a badge.svg,
// but reading one per pack per member would be a request each for decoration, which
// is exactly the trade this whole surface is built to refuse.
const PACK_HUES = ['var(--s-blue)', 'var(--s-aqua)', 'var(--s-violet)', 'var(--s-orange)', 'var(--s-yellow)'];
export const packBadge = (pack) => {
  let h = 0;
  for (const ch of pack) h = (h * 31 + ch.charCodeAt(0)) % 997;
  return el('span', {
    className: 'pack-badge',
    style: `background:${PACK_HUES[h % PACK_HUES.length]}`,
    textContent: (pack.replace(/^claudinite-/, '')[0] ?? '?').toUpperCase(),
    'aria-hidden': 'true',
  });
};

// THE MINI-CARD: one member's line from one pack, on the fleet page.
//
// It carries no column header and no pack name — the subrow has room for neither — so
// the phrase has to be a complete statement on its own. The pack is on the hover, and
// nothing depends on reading it.
//
// MONOCHROME, ALWAYS. A contribution never colours itself by severity however urgent
// its pack believes its number to be: colour on this grid is the engine's severity
// edge, and a pack that could paint itself red would be claiming attention it did not
// earn.
export function miniCard(parts, { title = '', glyph = null } = {}) {
  return el('div', { className: 'mini', title }, [
    glyph ? el('span', { className: 'mini-glyph', textContent: glyph, 'aria-hidden': 'true' }) : null,
    ...parts.flatMap((p, i) => [i ? ' ' : null, el('span', { className: p.t, textContent: p.text })]),
  ].filter((k) => k !== null));
}

// A card that could not make a line. Its own state, in words — never an empty box and
// never a zero.
export const miniAbsent = (text, title = '') =>
  el('div', { className: 'mini none', title, textContent: text });

// What each unreadable state SAYS. Four different facts that a lesser page would draw
// identically: the pack writes no file here, the page declined to spend a request,
// this figure has no value in a file that exists, and this descriptor is newer than
// the dashboard reading it.
export const CONTRIB_STATE_TEXT = {
  'no-file': 'no values yet',
  'not-read': 'not read',
  absent: 'not stated',
  'unknown-kind': 'newer than this page',
};

// --- the repo page's card ----------------------------------------------------------

const widgetBody = (widget, resolved, now) => {
  if (resolved.state !== 'ok') {
    return [
      el('div', { className: 'v', textContent: CONTRIB_STATE_TEXT[resolved.state] ?? 'not read' }),
      el('div', { className: 'k', textContent: widget.label }),
    ];
  }
  const v = resolved.value;

  if (widget.kind === 'stat') {
    return [
      el('div', { className: 'v num' }, [
        widget.glyph ? el('span', { className: 'w-glyph', textContent: widget.glyph, 'aria-hidden': 'true' }) : null,
        String(v.value ?? '—'),
      ]),
      el('div', { className: 'k' }, [widget.label, v.unit ? el('span', { className: 'unit', textContent: ` · ${v.unit}` }) : null]),
    ];
  }

  if (widget.kind === 'event') {
    const at = v.at ? new Date(v.at).getTime() : null;
    const text = v.text ?? '—';
    return [
      el('div', { className: 'v text' }, [
        v.url ? el('a', { href: v.url, textContent: text, target: '_blank', rel: 'noopener noreferrer' }) : text,
      ]),
      el('div', { className: 'k' }, [widget.label, at ? el('span', { className: 'unit', textContent: ` · ${ago(at, now)}` }) : null]),
    ];
  }

  if (widget.kind === 'window') {
    const d = windowDelta(v);
    // The delta is spelled out, never left as a bare arrow: a change with nothing to
    // compare against is the vanity total this whole surface refuses.
    return [
      el('div', { className: 'v num' }, [
        String(v.value ?? '—'),
        d ? el('span', {
          className: `delta ${d.dir === 'flat' ? 'flat' : d.dir === 'up' ? 'up' : 'down'}`,
          title: `${d.previous} in the window before`,
          textContent: `${d.dir === 'up' ? '▲' : d.dir === 'down' ? '▼' : '—'} ${d.previous}`,
        }) : null,
      ]),
      el('div', { className: 'k' }, [widget.label, v.window ? el('span', { className: 'unit', textContent: ` · last ${v.window}` }) : null]),
    ];
  }

  // list
  const { shown, omitted } = listItems(v);
  return [
    el('div', { className: 'k', textContent: widget.label }),
    el('ul', {}, shown.map((i) => el('li', {}, [
      el('span', { className: 't' }, [i.url ? el('a', { href: i.url, textContent: i.text, target: '_blank', rel: 'noopener noreferrer' }) : i.text]),
      el('span', { className: 'at', textContent: i.at ? duration(now - i.at) : '' }),
    ]))),
    // COUNTED, never dropped: a short list that looks complete is worse than no list.
    omitted ? el('div', { className: 'more', textContent: `+${omitted} not shown` }) : null,
    shown.length ? null : el('div', { className: 'sub', textContent: 'nothing in the window' }),
  ];
};

// One pack's card on the repo page. A descriptor the page cannot use becomes ONE
// named line here rather than a missing card, so a pack that ships a broken
// descriptor is visibly broken instead of invisibly absent.
export function packCard(contribution, now, { ids = null } = {}) {
  const { pack, descriptor, values, withheld } = contribution;

  const head = (note) => el('div', { className: 'pack-head' }, [
    packBadge(pack),
    el('span', { className: 'id', textContent: pack }),
    contribution.from ? el('span', { className: 'sub from', textContent: contribution.from.split('/')[1] ?? contribution.from }) : null,
    note ? el('span', { className: 'src nw', textContent: note }) : null,
  ]);

  if (withheld) {
    return el('article', { className: 'pack-card' }, [head(null),
      el('p', { className: 'sub', textContent: 'Not read — the page declined to spend a request on this load.' })]);
  }
  if (descriptor?.fault) {
    return el('article', { className: 'pack-card' }, [head(null),
      el('p', { className: 'sub', textContent: `This pack contributes nothing here: ${descriptor.fault}.` })]);
  }

  const widgets = (ids ?? descriptor.repo).map((id) => descriptor.widgets.get(id)).filter(Boolean);
  const source = descriptor.needsGenerated
    ? (values?.generatedAt ? ago(new Date(values.generatedAt).getTime(), now) : (values === undefined ? 'not read' : 'no values yet'))
    : 'live';

  return el('article', { className: 'pack-card' }, [
    head(source),
    el('div', { className: 'widgets' }, widgets.map((w) => {
      const resolved = valueOf(w, { values, live: contribution.live ?? {} });
      return el('div', { className: `w${w.kind === 'list' ? ' list' : ''}${resolved.state === 'ok' ? '' : ' absent'}` },
        widgetBody(w, resolved, now));
    })),
    // Named once, at the card, rather than on every widget that wanted it — and it
    // names the FILE, because "which task writes this" is the next question.
    values === null && descriptor.needsGenerated
      ? el('p', { className: 'sub', textContent: `Nothing has written ${valuesPath(pack)} in this repo. A normal state, not a fault.` })
      : null,
  ]);
}
