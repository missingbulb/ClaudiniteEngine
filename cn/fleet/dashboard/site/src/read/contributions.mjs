// PACK-CONTRIBUTED METRICS — the reader for what a pack says about a repo.
//
// A pack contributes DATA, NEVER CODE. Two JSON files, both lifted as text over the
// API exactly as task declarations are, because there is no `import` when reading
// another repo: a DESCRIPTOR shipped with the pack (`packs/<id>/dashboard.json`,
// found by path convention — nothing registers it) declaring what the pack has to
// say, and a VALUES file in the member's own tree
// (`.claudinite/usage/<pack>-dashboard-values.json`) written by that pack's own
// machinery. The page executes nothing from either. The fleet view renders repos the
// viewer merely has read access to, so importing a member's modules would run twelve
// strangers' code in the viewer's browser with the viewer's token in scope.
//
// WHAT THIS MODULE DOES NOT DO: touch the DOM. It parses, validates, resolves a
// widget to its value and composes the PARTS of a phrase; `ui.mjs` renders them.
// That split is the whole reason a pack supplies values rather than a finished
// string — a string arrives flat, and the parts can be set in three registers.
//
// UNKNOWN IS NOT ZERO here as everywhere on this page. `undefined` means NOT READ (a
// withheld read, a failure); `null` means read and absent. They render differently
// and neither renders as a number.
import { duration } from '../render/ui.mjs';
import { readMember } from './member.mjs';

// The closed vocabulary. A descriptor naming anything outside it is not guessed at:
// the widget renders as one saying this dashboard predates its descriptor, which is
// what lets a member run a pack version newer than the deployed page.
export const KINDS = Object.freeze(['stat', 'event', 'window', 'list']);
export const SOURCES = Object.freeze(['generated', 'latest-release', 'repo-stars']);

// `list` is the one kind a fleet mini-card cannot take: it is many lines and the
// subrow gives each pack one.
export const FLEET_KINDS = Object.freeze(['stat', 'event', 'window']);

// Renderer-owned budgets. A pack cannot spend more of a shared surface than this by
// writing a longer string — everything past the cap is clipped with the elision
// shown, never dropped silently.
export const MAX_REPO_WIDGETS = 6;
export const MAX_LIST_ITEMS = 5;
export const MAX_LABEL = 34;
export const MAX_TEXT = 46;
export const MAX_NOUN = 16;

export const DESCRIPTOR_FILE = 'dashboard.json';
// The values roll forward from one run to the next, so they sit with the member's other
// rolling records and carry no GENERATED.
export const VALUES_DIR = '.claudinite/usage';
export const valuesPath = (pack) => `${VALUES_DIR}/${pack}-dashboard-values.json`;
// Where a pack wrote them before `.claudinite/usage/`, read until its writer has moved them.
// @legacy-tolerance advisory:legacy-shape-in-use retire:#2323
export const legacyValuesPath = (pack) => `.claudinite/local/dashboard/${pack}.GENERATED.json`;

const escapeRe = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

// THE TWO-ROOT FORM. The canon runs this from its own root (`packs/<id>/`); every
// member runs it out of a vendored mount (`.claudinite/shared/packs/<id>/`). A
// pattern anchored to either alone works everywhere except the tree it was written
// in. Matched against the tree listing the view already holds, so discovery costs no
// request at all — a pack contributes exactly when its file is in a listing already
// in hand.
export function descriptorPathIn(paths, pack) {
  const re = new RegExp(`^(\\.claudinite/shared/)?packs/${escapeRe(pack)}/${escapeRe(DESCRIPTOR_FILE)}$`);
  return (paths ?? []).find((p) => re.test(p)) ?? null;
}

// The declared pack ids, in declaration order. An entry is a bare id or an object
// carrying one; a local pack (`local/<name>`) ships no canon descriptor and is left
// to fall out of the path match rather than being special-cased here.
export const declaredPackIds = (declaration) =>
  (declaration?.packs ?? []).map((p) => (typeof p === 'string' ? p : p?.id)).filter(Boolean);

const clip = (s, max) => {
  const t = String(s ?? '').trim();
  return t.length > max ? `${t.slice(0, max - 1)}…` : t;
};

const isObj = (v) => v !== null && typeof v === 'object' && !Array.isArray(v);
const ms = (t) => {
  if (t == null) return null;
  const n = typeof t === 'number' ? t : new Date(t).getTime();
  return Number.isFinite(n) ? n : null;
};

// --- the descriptor ----------------------------------------------------------------

// One widget, normalised. `kind` outside the vocabulary is KEPT rather than dropped:
// the reader must be able to say "this dashboard predates the descriptor" about a
// specific widget instead of silently rendering a shorter card.
function normaliseWidget(raw) {
  if (!isObj(raw) || typeof raw.id !== 'string' || !raw.id) return null;
  const glyph = typeof raw.glyph === 'string' ? [...raw.glyph.trim()] : [];
  return {
    id: raw.id,
    kind: typeof raw.kind === 'string' ? raw.kind : null,
    known: KINDS.includes(raw.kind),
    label: clip(raw.label ?? raw.id, MAX_LABEL),
    noun: clip(raw.noun ?? '', MAX_NOUN),
    // ONE grapheme. It is recognition only — ★ makes stars findable in a column of
    // numbers — so the phrase has to read without it, and a pack cannot buy a row of
    // emoji by writing a longer string.
    glyph: glyph.length === 1 ? glyph[0] : null,
    source: SOURCES.includes(raw.source) ? raw.source : 'generated',
  };
}

// Parse and STRUCTURALLY REVALIDATE a descriptor. The schema is what a canon check
// holds authors to; this is the page defending itself against whatever actually
// landed in a member's mount, which may have been written against a newer schema or
// by hand. A descriptor it cannot use becomes ONE named fault on that pack's card —
// never a broken page, and never an invented value.
export function parseDescriptor(text, pack) {
  let doc;
  try { doc = JSON.parse(text); } catch (e) { return { pack, fault: `its dashboard.json is not valid JSON — ${e.message}` }; }
  if (!isObj(doc)) return { pack, fault: 'its dashboard.json is not an object' };
  if (!Array.isArray(doc.widgets)) return { pack, fault: 'its dashboard.json declares no widgets array' };

  const widgets = new Map();
  for (const w of doc.widgets) {
    const n = normaliseWidget(w);
    if (n && !widgets.has(n.id)) widgets.set(n.id, n);
  }
  if (!widgets.size) return { pack, fault: 'its dashboard.json declares no usable widget' };

  // The views SELECT from the widget list by id, so the two surfaces cannot drift
  // into describing one figure differently. An id naming no widget is dropped: the
  // widget it pointed at may simply not exist in this version of the pack.
  const pick = (ids) => (Array.isArray(ids) ? ids : []).filter((id) => widgets.has(id));
  const member = typeof doc.fleet?.member === 'string' ? doc.fleet.member : null;
  const memberWidget = member && widgets.get(member);

  return {
    pack,
    fault: null,
    widgets,
    repo: pick(doc.repo).slice(0, MAX_REPO_WIDGETS),
    // A member mini-card must fit one line. A pack naming a `list` there gets no
    // mini-card rather than a truncated one — and its repo card is unaffected.
    member: memberWidget && (FLEET_KINDS.includes(memberWidget.kind) || !memberWidget.known) ? member : null,
    deployment: pick(doc.fleet?.deployment),
    // Which live sources this descriptor actually needs, so a caller reads none it
    // has no use for.
    needsGenerated: [...widgets.values()].some((w) => w.source === 'generated'),
    sources: new Set([...widgets.values()].map((w) => w.source)),
  };
}

// --- the values file ---------------------------------------------------------------

// A pack's own generated values. Deliberately not self-describing the way the usage
// fold's file is: its vocabulary is the DESCRIPTOR's, which the reader already holds,
// so the file is a plain id → value map and nothing has to agree twice.
export function parseValues(text) {
  if (text === null) return null;             // read, and the pack writes no file here
  try {
    const doc = JSON.parse(text);
    if (!isObj(doc)) return { generatedAt: null, values: {} };
    return { generatedAt: doc.generatedAt ?? null, values: isObj(doc.values) ? doc.values : {} };
  } catch {
    return { generatedAt: null, values: {}, fault: 'unreadable' };
  }
}

// --- resolving a widget to its value -----------------------------------------------

// `live` carries the two platform facts, each from a read the page ALREADY makes for
// every member: `stars` off the repo metadata, `release` off the releases listing.
// Both may be `undefined` — not read — and that is not the same as absent.
export function valueOf(widget, { values, live = {} } = {}) {
  if (!widget.known) return { state: 'unknown-kind' };
  if (widget.source === 'repo-stars') {
    if (live.stars === undefined) return { state: 'not-read' };
    return live.stars === null ? { state: 'absent' } : { state: 'ok', value: { value: live.stars } };
  }
  if (widget.source === 'latest-release') {
    if (live.release === undefined) return { state: 'not-read' };
    return live.release === null ? { state: 'absent' } : { state: 'ok', value: live.release };
  }
  if (values === undefined) return { state: 'not-read' };
  if (values === null) return { state: 'no-file' };
  const v = values.values?.[widget.id];
  return isObj(v) ? { state: 'ok', value: v } : { state: 'absent' };
}

// --- composing the phrase ----------------------------------------------------------

// THREE REGISTERS, and they are why a pack supplies values rather than a string. The
// quantity reads first and is what differs between members; the noun says what of;
// the connective is present for grammar and gets out of the way.
const q = (text) => ({ t: 'q', text: String(text) });
const n = (text) => ({ t: 'n', text: String(text) });
const c = (text) => ({ t: 'c', text: String(text) });

const num = (v) => (typeof v === 'number' && Number.isFinite(v) ? String(v) : null);

// One member's mini-card, as parts. Returns null when the widget cannot make a line —
// the caller then renders the state it is in, never a half-phrase.
export function fleetPhrase(widget, value, now) {
  if (!value) return null;
  switch (widget.kind) {
    case 'stat': {
      const v = num(value.value);
      return v === null ? null : [q(v), n(widget.noun || widget.label)].filter((p) => p.text);
    }
    case 'event': {
      const at = ms(value.at);
      const text = clip(value.text, MAX_TEXT);
      if (at === null && !text) return null;
      // The AGE leads, because on a grid the question a released-thing answers is how
      // stale it is; the tag itself follows as what was released.
      return [
        at === null ? null : q(duration(now - at)),
        at === null ? null : c('ago ·'),
        text ? n(text) : null,
      ].filter(Boolean);
    }
    case 'window': {
      const v = num(value.value);
      if (v === null) return null;
      // NO DELTA. On a repo card the previous window is the only comparison there is;
      // on the grid the comparison is the column itself, across members, so the card
      // spends its characters on the span it covers instead.
      const span = clip(value.window ?? '', 8);
      return [
        q(v),
        widget.noun ? n(widget.noun) : null,
        span ? c('in last') : null,
        span ? q(span) : null,
      ].filter(Boolean);
    }
    default:
      return null;
  }
}

// The plain-text form of the same parts — the mini-card's `title`, and what a test
// asserts against.
export const phraseText = (parts) => (parts ?? []).map((p) => p.text).join(' ');

// A list widget's items, capped, with the overflow COUNTED rather than dropped: a
// reader must never be shown a short list that looks complete.
export function listItems(value) {
  const items = Array.isArray(value?.items) ? value.items.filter(isObj) : [];
  return {
    shown: items.slice(0, MAX_LIST_ITEMS).map((i) => ({
      text: clip(i.text, MAX_TEXT),
      // Anything that is not an https URL renders as plain text rather than as a link
      // the page would be lending its origin to.
      url: typeof i.url === 'string' && /^https:\/\//.test(i.url) ? i.url : null,
      at: ms(i.at),
    })),
    omitted: Math.max(0, items.length - MAX_LIST_ITEMS),
  };
}

// A window's change against the window before it — the repo card's half of the story.
export function windowDelta(value) {
  const cur = num(value?.value);
  const prev = num(value?.previous);
  if (cur === null || prev === null) return null;
  const by = Number(cur) - Number(prev);
  return { dir: by > 0 ? 'up' : by < 0 ? 'down' : 'flat', by: Math.abs(by), previous: Number(prev) };
}

// --- reading a repo's contributions ------------------------------------------------

// Every declared pack that carries a descriptor in this tree, with its values.
//
// COSTS NOTHING TO DISCOVER: the caller already holds the declaration and the tree
// listing at this sha, so which packs contribute is a match against a listing in
// hand. What it spends is at most two CONTENT reads per contributing pack — both at
// a sha, therefore cached forever and free while the branch has not moved — plus, if
// any widget asks for one, the live sources the caller passes in.
//
// A read the budget declined is not a pack with nothing to say: it answers
// `values: undefined` and the card says the page declined to spend.
export async function readContributions({ repo, sha, token, declaration, paths, gh }) {
  if (!paths || !declaration) return [];

  const found = declaredPackIds(declaration)
    .map((pack) => ({ pack, path: descriptorPathIn(paths, pack) }))
    .filter((f) => f.path);

  return (await Promise.all(found.map(async ({ pack, path }) => {
    let text;
    try { text = await gh.getTextAtSha(repo, sha, path, token); } catch { return { pack, withheld: true }; }
    // The listing said it was there, so a null here means the tree and the contents
    // API disagree — which is a fault about this pack, not about the page.
    if (text === null) return { pack, fault: 'its dashboard.json is in the tree but could not be fetched' };

    const descriptor = parseDescriptor(text, pack);
    if (descriptor.fault) return { pack, descriptor };

    let values;
    if (descriptor.needsGenerated) {
      try { values = parseValues(await readValuesText({ repo, sha, token, paths, gh }, pack)); } catch { values = undefined; }
    }
    return { pack, descriptor, values };
  }))).filter(Boolean);
}

// A pack's values text, at its path or, until its writer has moved it, the old one.
// The listing says which is there, so a member that has moved spends no second read.
async function readValuesText({ repo, sha, token, paths, gh }, pack) {
  const at = paths.includes(valuesPath(pack)) || !paths.includes(legacyValuesPath(pack)) ? valuesPath(pack) : legacyValuesPath(pack);
  return gh.getTextAtSha(repo, sha, at, token);
}

// Which live sources a repo's contributions need at all, so a view reads none it has
// no use for — a fleet with no release pack anywhere never asks for a release.
export const liveSourcesNeeded = (contributions) => {
  const out = new Set();
  for (const c of contributions ?? []) for (const s of c.descriptor?.sources ?? []) if (s !== 'generated') out.add(s);
  return out;
};

// A repo's contributions read from scratch — its own metadata, head, declaration and
// tree — for a repo the caller has not already swept. The fleet page uses it for the
// DEPLOYMENT's own cards, which come from repos that may or may not be members.
//
// Everything it touches is either conditional or keyed by a sha, and on a fleet page
// the deployment repo is usually a member already read this load, so in practice this
// is served from cache rather than spent.
export async function readRepoContributions({ repo, token, gh }) {
  try {
    const meta = await gh.getRepo(repo, token);
    const sha = await gh.getHeadSha(repo, meta.default_branch, token);
    const { member, fault } = await readMember({ repo, sha, token, gh });
    if (fault) return { repo, contributions: [], error: new Error(fault) };
    if (!member) return { repo, contributions: [] };
    const declaration = member.declaration;
    const tree = await gh.listTreeAtSha(repo, sha, token);
    const contributions = await readContributions({ repo, sha, token, declaration, paths: tree?.paths ?? null, gh });
    const needed = liveSourcesNeeded(contributions);
    const live = {
      stars: meta.stars,
      release: needed.has('latest-release') ? await gh.latestRelease(repo, token).catch(() => undefined) : undefined,
    };
    for (const c of contributions) c.live = live;
    return { repo, contributions };
  } catch (error) {
    return { repo, contributions: [], error };
  }
}

// The deployment-scope cards: every contribution, from the repo the deployment is
// published from (and the repo a single-repo page shows, where that is another one),
// that declares a `fleet.deployment` list. A repo named twice is read once.
export async function readDeploymentContributions({ config, token, gh }) {
  const repos = [...new Set([config?.defaultRepo, config?.deploymentRepo].filter(Boolean))];
  const reads = await Promise.all(repos.map((repo) => readRepoContributions({ repo, token, gh })));
  return reads.flatMap((r) => r.contributions
    .filter((c) => c.descriptor?.deployment?.length)
    .map((c) => ({ ...c, from: r.repo })));
}
