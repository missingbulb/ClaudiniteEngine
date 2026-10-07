// The Node half of the settings parity face: reads the member tree named by
// argv[2], a git checkout holding .claudinite-settings.json, the way the
// frozen engine reads it (repo-context.mjs's loadConfig, the registry's
// discoverPacks and isActive, installed-versions.mjs and the lifecycle
// pack's legacy-shape-in-use rule, imported by path from
// CLAUDINITE_NODE_ENGINE) and prints what it made of it as JSON.
import { join, relative, sep } from 'node:path';
import { pathToFileURL } from 'node:url';

const engine = process.env.CLAUDINITE_NODE_ENGINE;
if (!engine) throw new Error('CLAUDINITE_NODE_ENGINE is not set');
const mod = (rel) => import(pathToFileURL(join(engine, rel)).href);

const root = process.argv[2];
const { buildContext } = await mod('engine/checks/helpers/repo-context.mjs');
const { discoverPacks, isActive, packEntryId } = await mod('engine/pack_loader/pack-registry.mjs');
const { installedVersions } = await mod('engine/installed-versions.mjs');
const { onFailOf } = await mod('engine/checks/helpers/findings.mjs');
const { isDormant } = await mod('packs/claudinite-tasks/src/contract/dormancy.mjs');
const legacy = (await mod('packs/claudinite-lifecycle/worldRules/legacy-shape-in-use.mjs')).default;

const { packs, errors: packErrors } = await discoverPacks({ localRoot: root });
const ctx = buildContext({ root, mode: 'all' });
const config = ctx.config;

const token = (p) => (p.local ? `local/${p.id}` : p.id);
const byId = new Map(packs.map((p) => [p.id, p]));

// Declared order after canonicalization, each pack once.
const active = [];
for (const entry of Array.isArray(config.raw?.packs) ? config.raw.packs : []) {
  const id = packEntryId(entry);
  const p = byId.get(id);
  if (!p || !isActive(p, config) || active.includes(token(p))) continue;
  active.push(token(p));
}

const keyOf = (id) => (byId.get(id)?.local ? `local/${id}` : id);
const packConfig = {};
for (const [id, c] of Object.entries(config.packConfig ?? {})) packConfig[keyOf(id)] = c;

const rules = {};
for (const [id, v] of Object.entries(config.rules ?? {})) rules[id] = onFailOf(v) ?? v;

const accept = (config.accept ?? []).map((a) => {
  const out = {};
  for (const k of ['rule', 'path', 'reason', 'pack']) if (a?.[k] !== undefined) out[k] = a[k];
  return out;
});

const ts = config.taskScheduler ?? {};
const scheduler = {
  disabledTasks: Array.isArray(ts.disabledTasks) ? ts.disabledTasks : [],
  endpoints: ts.agenticTaskInvocationEndpoints ?? {},
  dormant: isDormant(config),
  requireReview: config.dailyClaudiniteUpdatesRequirePrReview === true,
};

// The blocking config class, as check_the_world collects it: the settings'
// own errors, the faults of the member's own packs, and each declared id no
// pack carries.
const inRoot = (dir) => dir && !relative(root, dir).startsWith('..') && !relative(root, dir).startsWith(sep);
const errors = [
  ...config.errors.map((e) => e.what),
  ...packErrors.filter((e) => inRoot(e.dir)).map((e) => e.what),
];
const knownIds = new Set(packs.map((p) => p.id));
for (const name of config.packs) {
  if (typeof name === 'string' && !knownIds.has(name)) errors.push(`declares unknown pack "${name}"`);
}

ctx.packs = packs.filter((p) => isActive(p, config));
const advisories = legacy.run(ctx).map((f) => f.what);

const installed = installedVersions(config.raw);

process.stdout.write(`${JSON.stringify({
  active, config: packConfig, sharedConstants: config.sharedConstants ?? [], rules, accept, scheduler, errors, advisories,
  installed,
}, null, 2)}\n`);
