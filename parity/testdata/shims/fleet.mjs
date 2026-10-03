// The Node half of the fleet parity face: reads one fixture's input on
// stdin, answers it with the frozen engine's sheepdog modules
// (packs/claudinite-fleet-sheepdog, imported by path from
// CLAUDINITE_NODE_ENGINE, where their engine/ imports resolve) and the
// tasks pack's fleet signal reader, and prints the answer as JSON. The
// core is argv[2].
//
// Where a module takes a `gh`, the shim builds a fake from the fixture's
// `calls` table, keyed "METHOD path" (the path as the module spells it,
// query and all): a value is one response `{status, json}`, or a list
// answered in order with the last repeated. A path the table does not
// name answers 404. Every call made is recorded as [method, path, body].
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

const engine = process.env.CLAUDINITE_NODE_ENGINE;
if (!engine) throw new Error('CLAUDINITE_NODE_ENGINE is not set');
const sheepdog = (rel) => import(pathToFileURL(join(engine, 'packs/claudinite-fleet-sheepdog', rel)).href);
const tasksPack = (rel) => import(pathToFileURL(join(engine, 'packs/claudinite-tasks', rel)).href);
const addPacks = (rel) => sheepdog(`tasks/fleet-add-missing-packs/${rel}`);

// A fixture's packs as the canon clone loads them: each relevance detector
// rebuilt from its data, as a catalog entry carries it.
async function catalogPacks() {
  const d = await import(pathToFileURL(join(engine, 'engine/pack_loader/relevance-detector.mjs')).href);
  return (input.packs ?? []).map((p) => ({ ...p, relevanceDetector: d.relevanceDetectorFromData(p.relevanceDetector ?? null) }));
}

const core = process.argv[2];
const input = JSON.parse(readFileSync(0, 'utf8'));

function fakeGh(table = {}) {
  const calls = [];
  const used = {};
  const gh = async (path, { method = 'GET', body } = {}) => {
    const key = `${method} ${path}`;
    calls.push(body === undefined ? [method, path] : [method, path, body]);
    const entry = table[key];
    if (entry === undefined) return { status: 404, json: null };
    if (!Array.isArray(entry)) return { status: entry.status, json: entry.json ?? null };
    const i = Math.min(used[key] ?? 0, entry.length - 1);
    used[key] = (used[key] ?? 0) + 1;
    return { status: entry[i].status, json: entry[i].json ?? null };
  };
  return { gh, calls };
}

const cores = {
  async token() {
    const t = await sheepdog('fleet-token.mjs');
    const grantFor = {};
    for (const s of input.sweeps ?? []) grantFor[s] = t.fleetTokenGrantFor(s);
    const permissionFor = {};
    const hint = {};
    for (const p of input.paths ?? []) {
      permissionFor[p] = t.permissionForEndpoint(p)?.permission ?? null;
      hint[p] = t.forbiddenHint(p);
    }
    return {
      grant: t.fleetTokenGrant(),
      grantFor,
      permissionFor,
      hint,
      missing: (input.missing ?? []).map(({ sweep, detail }) => t.missingFleetTokenError(sweep, detail).message),
      handover: t.fleetTokenHandoverStep(),
    };
  },
  async config() {
    const c = await sheepdog('fleet-config.mjs');
    try {
      const { owner, exclude, canonRepo, packSeeds } = c.parseSheepdogConfig(input.cfg, input.home);
      const out = { owner, exclude: [...exclude], packSeeds };
      // The default canonRepo is a derived name, not something the config
      // said; it is compared only where the config names one.
      const entry = (input.cfg?.packs ?? []).find((e) => e?.id === 'claudinite-fleet-sheepdog');
      if (entry?.config?.canonRepo !== undefined) out.canonRepo = canonRepo;
      return out;
    } catch (e) {
      return { error: e.message };
    }
  },
  async dormancy() {
    const d = await sheepdog('dormancy.mjs');
    return input.configs.map((cfg) => d.isDormant(cfg));
  },
  async dispatch() {
    const a = await sheepdog('fleet-api.mjs');
    return input.statuses.map((s) => a.classifyDispatch(s));
  },
  async scope() {
    const u = await sheepdog('tasks/fleet-update/force-fleet-update.mjs');
    const filter = u.parseRepoFilter(input.filter, input.owner);
    const exclude = new Set((input.exclude ?? []).map((s) => s.toLowerCase()));
    return {
      filter: filter === null ? null : [...filter],
      scopes: input.repos.map((r) => {
        const s = u.classifyScope(r, { canonRepo: input.canonRepo, exclude, filter });
        return s === null ? null : { fullName: r.full_name.toLowerCase(), ...s };
      }),
    };
  },
  async freshness() {
    const f = await sheepdog('tasks/fleet-roster/freshness.mjs');
    return f.classifyFreshness({ hasScheduler: input.hasScheduler, installed: input.installed, canon: input.canon });
  },
  async views() {
    const r = await sheepdog('tasks/fleet-roster/check-fleet-roster.mjs');
    return { coverage: r.coverageView(input.roster), freshness: r.freshnessView(input.roster) };
  },
  async reports() {
    switch (input.kind) {
      case 'coverage': {
        const a = await sheepdog('tasks/fleet-roster/adoption-issues.mjs');
        return a.renderCoverageSummary(input.args);
      }
      case 'freshness': {
        const f = await sheepdog('tasks/fleet-roster/freshness.mjs');
        return f.renderFreshnessSummary(input.args);
      }
      case 'update': {
        const u = await sheepdog('tasks/fleet-update/force-fleet-update.mjs');
        return u.renderUpdateReport({ ...input.args, filter: input.args.filter ? new Set(input.args.filter) : null });
      }
      case 'verdict': {
        const u = await sheepdog('tasks/fleet-update/force-fleet-update.mjs');
        return u.runVerdict(input.args);
      }
      default:
        throw new Error(`unknown report ${input.kind}`);
    }
  },
  async adoption() {
    const a = await sheepdog('tasks/fleet-roster/adoption-issues.mjs');
    const { gh, calls } = fakeGh(input.calls);
    try {
      const actions = await a.convergeAdoption(gh, input.home, {
        uncovered: input.uncovered,
        coveredSet: new Set(input.covered ?? []),
        ignoredSet: new Set(input.ignored ?? []),
      });
      return { actions, calls };
    } catch (e) {
      return { error: e.message, calls };
    }
  },
  async follow() {
    const fl = await sheepdog('tasks/fleet-update/follow-to-current.mjs');
    // The poll script: each member's declarations in poll order, the last
    // repeated; an entry is { engine } (a declaration stamping that engine
    // and no pack), { error } (the read throws) or { gone: true } (no file).
    const polls = {};
    const readDeclaration = async (_gh, fullName) => {
      const script = input.script[fullName] ?? [];
      const i = Math.min(polls[fullName] ?? 0, script.length - 1);
      polls[fullName] = (polls[fullName] ?? 0) + 1;
      const step = script[i];
      if (step.error) throw new Error(step.error);
      if (step.gone) return null;
      return { engineVersion: step.engine, packs: [] };
    };
    const table = {};
    for (const m of input.members) {
      table[`GET /repos/${m.fullName}/contents/.github/workflows/claudinite-scheduler.yml`] = { status: 200, json: {} };
      const runs = input.runs?.[m.fullName];
      table[`GET /repos/${m.fullName}/actions/workflows/claudinite-scheduler.yml/runs?event=workflow_dispatch&per_page=20`] = runs === 'error'
        ? { status: 500, json: null }
        : { status: 200, json: { workflow_runs: runs ? [{ created_at: input.startedAt }] : [] } };
    }
    const { gh } = fakeGh(table);
    let clock = 0;
    const sleeps = [];
    const logs = [];
    const followed = await fl.followToCurrent(gh, input.members.map((m) => ({ ...m })), {
      canon: { engine: async () => input.canonEngine, pack: async () => null },
      readDeclaration,
      budgetMs: input.budgetMs,
      now: () => clock,
      sleep: async (ms) => { sleeps.push(ms); clock += ms; },
      log: (line) => logs.push(line),
    });
    return {
      followed: followed.map((f) => (f.outcome === fl.NEVER_STARTED || f.outcome === fl.UNKNOWN
        ? { fullName: f.fullName, outcome: f.outcome, detail: f.detail }
        : { fullName: f.fullName, outcome: f.outcome })),
      sleeps,
      logs,
      pollDelays: [0, 1, 2, 3, 4, 9].map((r) => fl.pollDelay(r)),
    };
  },
  async bag() {
    const b = await sheepdog('param-bag.mjs');
    return b.parseParamBag(input.raw);
  },
  async signal() {
    const s = await tasksPack('src/signals/fleet.mjs');
    const { gh } = fakeGh(input.calls);
    return s.readFleet(gh, { owner: input.owner, canonRepo: input.canonRepo, sinceIso: input.sinceIso });
  },

  // 15b: the add-packs task's two halves and the pack-seed sweep.
  async params() {
    const p = await addPacks('params.mjs');
    return input.cases.map(({ argv, params }) => {
      try {
        return p.parseParams({ argv, params });
      } catch (e) {
        return { error: e.message };
      }
    });
  },
  async force() {
    const f = await addPacks('force-add-packs.mjs');
    const packs = await catalogPacks();
    const byId = new Map(packs.map((p) => [p.id, p]));
    const { gh, calls } = fakeGh(input.calls);
    const withCalls = async (fn) => {
      try {
        return { result: await fn(), calls };
      } catch (e) {
        return { error: e.message, calls };
      }
    };
    switch (input.fn) {
      case 'qualify':
        return input.cases.map(([name, owner]) => f.qualify(name, owner));
      case 'unknownPacks':
        return f.unknownPacks(input.addPacks, packs);
      case 'unansweredQuestions':
        return f.unansweredQuestions(input.addPacks, packs, input.packAnswers);
      case 'entryFor':
        return input.cases.map((c) => f.entryFor(c.id, c));
      case 'requestedBody':
        return f.requestedBody({ ...input.args, packsById: byId });
      case 'forceSummary':
        return f.renderForceSummary(input.args);
      case 'resolveTargets':
        return withCalls(() => f.resolveTargets(gh, {
          repos: input.repos, owner: input.owner, addPacks: input.addPacks,
          reposByName: new Map(Object.entries(input.reposByName ?? {})),
        }));
      case 'convergeRequested':
        return withCalls(() => f.convergeRequestedIssue(gh, input.fullName, { body: input.body }));
      case 'closeSatisfied':
        return withCalls(() => f.closeSatisfiedRequest(gh, input.fullName, { decl: input.decl, open: input.open }));
      default:
        throw new Error(`unknown force fn ${input.fn}`);
    }
  },
  async protocol() {
    const p = await addPacks('protocol.mjs');
    return {
      constants: {
        label: p.LABEL, requestedTitle: p.REQUESTED_TITLE, suspectedTitle: p.SUSPECTED_TITLE,
        mark: p.MARK, memberTaskId: p.MEMBER_TASK_ID,
      },
      entriesIn: (input.bodies ?? []).map((b) => p.entriesIn(b)),
      withTargeting: (input.targeting ?? []).map((t) => p.withTargeting(t.body, { blockedBy: t.blockedBy ?? null })),
    };
  },
  async mark() {
    const m = await addPacks('mark-work-list.mjs');
    const { gh, calls } = fakeGh(input.calls);
    switch (input.fn) {
      case 'markedBody':
        return input.cases.map((c) => m.markedBody(c.body, c.existing ?? null, { blockedBy: c.blockedBy ?? null }));
      case 'otherOpenWorkList':
        return input.cases.map((c) => m.otherOpenWorkList(c.open, c.title));
      case 'remark':
        try {
          return { written: await m.remarkWorkList(gh, input.fullName, input.existing, input.body), calls };
        } catch (e) {
          return { error: e.message, calls };
        }
      default:
        throw new Error(`unknown mark fn ${input.fn}`);
    }
  },
  async scan() {
    const s = await addPacks('scan-for-needed-packs.mjs');
    const packs = await catalogPacks();
    const byId = new Map(packs.map((p) => [p.id, p]));
    const { gh, calls } = fakeGh(input.calls);
    switch (input.fn) {
      case 'suspectedBody':
        return s.suspectedBody({ fits: input.fits, undecided: input.undecided, packsById: byId });
      case 'fitSummary':
        return s.renderFitSummary(input.args);
      case 'convergeSuspected':
        try {
          return { result: await s.convergeSuspectedIssue(gh, input.fullName, { fits: input.fits, body: input.body }), calls };
        } catch (e) {
          return { error: e.message, calls };
        }
      case 'runScan':
        try {
          const r = await s.runScan({
            gh, home: input.home, owner: input.owner, canonRepo: input.canonRepo,
            exclude: new Set(input.exclude ?? []), packs, repos: input.repos ?? null,
          });
          return {
            findings: r.findings, unknown: r.unknown, toFire: r.toFire, actions: r.actions,
            fitted: r.fitted, dormant: r.dormant, outOfScope: r.outOfScope, calls,
          };
        } catch (e) {
          return { error: e.message, calls };
        }
      default:
        throw new Error(`unknown scan fn ${input.fn}`);
    }
  },
  async fit() {
    const f = await addPacks('fingerprint-fit.mjs');
    const r = await addPacks('remote-context.mjs');
    const packs = await catalogPacks();
    switch (input.fn) {
      case 'fitCandidates':
        return f.fitCandidates(packs, input.declared).map((p) => p.id);
      case 'undeclaredFits':
        // An answer "throw:<message>" throws, as a broken predicate would.
        return f.undeclaredFits({
          packs, declared: input.declared,
          evaluate: async (pack) => {
            const a = input.answers[pack.id];
            if (typeof a === 'string' && a.startsWith('throw:')) throw new Error(a.slice('throw:'.length));
            return a;
          },
        });
      case 'remote': {
        const { gh, calls } = fakeGh(input.calls);
        const evaluate = r.makeRemoteEvaluator(gh, input.repo, input.ref, { tracked: input.tracked, truncated: input.truncated, budget: input.budget });
        const verdicts = {};
        for (const p of packs) verdicts[p.id] = await evaluate(p);
        return { verdicts, calls };
      }
      case 'tree': {
        const { gh, calls } = fakeGh(input.calls);
        try {
          return { result: await r.fetchTree(gh, input.repo, input.ref), calls };
        } catch (e) {
          return { error: e.message, calls };
        }
      }
      default:
        throw new Error(`unknown fit fn ${input.fn}`);
    }
  },
  async seeds() {
    const s = await sheepdog('tasks/fleet-pack-seeds/check-fleet-pack-seeds.mjs');
    switch (input.fn) {
      case 'classify':
        return input.cases.map((c) => s.classifySeed(c));
      case 'withSeeds':
        try {
          return s.withSeeds(input.text, input.seeds);
        } catch (e) {
          return { error: e.message };
        }
      default:
        throw new Error(`unknown seeds fn ${input.fn}`);
    }
  },
};

if (!cores[core]) throw new Error(`unknown core ${core}`);
process.stdout.write(`${JSON.stringify(await cores[core]())}\n`);
