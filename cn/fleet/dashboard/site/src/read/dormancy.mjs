// Is a member's SCHEDULER dormant? THE DASHBOARD'S OWN COPY of the scheduler's predicate,
// read off the member's own declaration: packs share no code, so this pack carries the
// one test the member's scheduler stops itself with.
//
// It reads three shapes: a cn member as `read/member.mjs` answers it, whose `dormant`
// its own `cn` wrote into the member file; and, for a Node member, a raw parsed
// `.claudinite-settings.json` and the normalized config `loadConfig` returns alike,
// because a cross-repo reader fetches another repo's declaration over the API with no
// engine loaded against that tree. Strictly `=== true`, so every malformed value reads
// as awake — the conservative direction.
import { TASKS_PACK_ID, DORMANT_CONFIG_KEY, canonicalPackId } from './queue-vocabulary.mjs';

// A declared id as it resolves today. A member's declaration can carry a spelling from
// before a rename, and the config of a pack a member writes into its own repo must keep
// resolving under every spelling it was ever written under.
const resolves = (id) => typeof id === 'string' && canonicalPackId(id) === TASKS_PACK_ID;

// This pack's parameters, from whichever shape the caller holds.
//
// The normalized view first: `loadConfig` folds every entry's `config` into `packConfig`
// keyed by the pack's own id, so a scheduler that already loaded its settings needs no
// second walk. Then the raw declaration, where the parameters sit on the entry object as
// the member wrote them — which is all a cross-repo reader ever has.
function packParameters(config) {
  if (config === null || typeof config !== 'object') return undefined;
  const folded = config.packConfig?.[TASKS_PACK_ID];
  if (folded !== null && typeof folded === 'object') return folded;
  for (const entry of Array.isArray(config.packs) ? config.packs : []) {
    if (entry !== null && typeof entry === 'object' && resolves(entry.id)) return entry.config;
  }
  return undefined;
}

// What the declaration says, before any judgement about whether it says it legally.
// `undefined` means the project never answered, which is the normal shape and means awake.
function declared(config) {
  if (config === null || typeof config !== 'object') return undefined;
  const own = packParameters(config)?.[DORMANT_CONFIG_KEY];
  if (own !== undefined) return own;
  // The retired top-level spelling, underneath the pack entry so a converged member is
  // never overridden by a stale key its migration left behind; the queue's own copy
  // carries the tolerance that retires it.
  return config.raw?.[DORMANT_CONFIG_KEY] ?? config[DORMANT_CONFIG_KEY];
}

// The predicate. Strictly `=== true`, so every malformed value below reads as awake —
// the conservative direction, since the alternative is silently stopping a project's
// entire scheduled workload on a typo.
export const isDormant = (config) => (config?.shape === 'cn' ? config.dormant === true : declared(config) === true);
