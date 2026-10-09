// WHAT A MEMBER SAYS ABOUT ITSELF, read from the page. One reader, because the page
// asks it from three places (the repo view, the fleet view, the contributions panel),
// and one shape out of it whichever engine the member runs:
//
//   { shape: 'cn' | 'node', settingsPath, declared: [{ id, config }], dormant (null: unstated),
//     engine: { version } | null, held: { <pack>: <version> }, declaration }
//
// A `cn` member states itself in `.claudinite/cache/member.GENERATED.json`, which its
// own `cn` writes beside the two other flat files from whichever settings file it keeps
// (YAML, TOML or JSON — none of which this page parses). A `node` member is read at its
// `.claudinite-settings.json`, with the Node stamp's rules spelled below. A repo with
// neither does not run Claudinite, which the caller reports as such.
//
// A `cn` member is read at its member file ONLY: a member whose file is missing or
// unreadable is not re-derived from its settings, because the page has no parser for
// two of the three formats and a guess at the third would be a second writer.
//
// `declaration` is the shape the rest of the page reads a member's packs from, `{ packs:
// [{ id, config }] }`: the parsed Node file as it stands, or the member file's declared
// list under the same key.
import { canonicalPackId } from './queue-vocabulary.mjs';
import { isDormant } from './dormancy.mjs';

// Spelled here, not imported: the page renders other repos in the viewer's browser and
// imports nothing from the engine. The engine's site tests hold the member file's
// path to cn's own.
export const MEMBER_PATH = '.claudinite/cache/member.GENERATED.json';
// Where `cn` wrote it before `.claudinite/cache/`, read only when the new path is absent.
// @legacy-tolerance advisory:rules-index-current retire:#45
export const LEGACY_MEMBER_PATH = '.claudinite/flat/member.GENERATED.json';
export const NODE_SETTINGS_PATH = '.claudinite-settings.json';

const isObject = (v) => v !== null && typeof v === 'object' && !Array.isArray(v);
const entryId = (e) => (typeof e === 'string' ? e : (isObject(e) && typeof e.id === 'string' ? e.id : null));

// A version as the Node stamp holds it: a date-anchored `<day>.<n>` string, or a legacy
// non-negative integer. Anything else is not a version, and an entry carrying one is
// read as unstamped rather than as a zero.
const DATE_ANCHORED = /^([1-9]\d{4,5})\.([1-9]\d*)$/;
const isStampVersion = (v) => (typeof v === 'number'
  ? Number.isInteger(v) && v >= 0
  : typeof v === 'string' && DATE_ANCHORED.test(v.trim()));

// The Node stamp, by the frozen engine's own rules: the top-level `engineVersion`, and
// each object entry's own `version` keyed by its id under today's spelling, the current
// spelling winning where a declaration mid-rename carries both.
export function nodeStamp(raw) {
  const engine = isStampVersion(raw?.engineVersion) ? raw.engineVersion : null;
  const stamped = {};
  for (const entry of Array.isArray(raw?.packs) ? raw.packs : []) {
    if (isObject(entry) && typeof entry.id === 'string' && isStampVersion(entry.version)) stamped[entry.id] = entry.version;
  }
  const held = {};
  for (const [id, version] of Object.entries(stamped)) {
    const to = canonicalPackId(id);
    if (to !== id && Object.hasOwn(stamped, to)) continue;
    held[to] = version;
  }
  return { engine: engine === null ? null : { version: engine }, held };
}

// A Node member from its parsed settings file.
export function memberFromNode(raw) {
  if (!isObject(raw)) return null;
  const { engine, held } = nodeStamp(raw);
  const declared = (Array.isArray(raw.packs) ? raw.packs : [])
    .map((e) => ({ id: entryId(e), config: isObject(e) && isObject(e.config) ? e.config : undefined }))
    .filter((e) => e.id !== null);
  return { shape: 'node', settingsPath: NODE_SETTINGS_PATH, declared, dormant: isDormant(raw), engine, held, declaration: raw };
}

// A cn member from its member file's text, or `{ fault }` naming why it cannot be read.
export function memberFromFile(text) {
  let doc;
  try { doc = JSON.parse(text); } catch { return { fault: `${MEMBER_PATH} is not valid JSON` }; }
  if (!isObject(doc) || !isObject(doc.packs) || !Array.isArray(doc.packs.declared)) {
    return { fault: `${MEMBER_PATH} carries no packs.declared list` };
  }
  const declared = doc.packs.declared
    .map((e) => ({ id: entryId(e), config: isObject(e?.config) ? e.config : undefined }))
    .filter((e) => e.id !== null);
  const held = {};
  for (const [id, v] of Object.entries(isObject(doc.held) ? doc.held : {})) if (typeof v === 'string') held[id] = v;
  return {
    shape: 'cn',
    settingsPath: typeof doc.settings?.path === 'string' ? doc.settings.path : null,
    declared,
    dormant: typeof doc.dormant === 'boolean' ? doc.dormant : null,
    engine: typeof doc.engine?.version === 'string' ? { version: doc.engine.version } : null,
    held,
    declaration: { packs: declared.map(({ id, config }) => (config === undefined ? { id } : { id, config })) },
  };
}

// The member at `sha`: `{ member }`, `{ member: null }` for a repo that does not run
// Claudinite, or `{ member: null, fault }` for one whose declaration could not be read.
// `paths`, where the caller already holds the tree listing, decides which file is there
// without a request; without it the member file is asked first, since a cn member is
// the case every member converges to, and its 404 is cached under the sha like every
// content read.
export async function readMember({ repo, sha, token, gh, paths = null }) {
  const listed = (p) => paths === null || paths.includes(p);
  for (const at of [MEMBER_PATH, LEGACY_MEMBER_PATH]) {
    if (!listed(at)) continue;
    const text = await gh.getTextAtSha(repo, sha, at, token);
    if (text) {
      const m = memberFromFile(text);
      return m.fault ? { member: null, fault: m.fault } : { member: m };
    }
  }
  if (listed(NODE_SETTINGS_PATH)) {
    const text = await gh.getTextAtSha(repo, sha, NODE_SETTINGS_PATH, token);
    if (text) {
      let raw;
      try { raw = JSON.parse(text); } catch { return { member: null, fault: `${NODE_SETTINGS_PATH} is present but is not valid JSON` }; }
      const m = memberFromNode(raw);
      return m ? { member: m } : { member: null, fault: `${NODE_SETTINGS_PATH} is not a JSON object` };
    }
  }
  return { member: null };
}
