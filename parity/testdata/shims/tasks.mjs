// The Node half of the tasks parity face: reads one fixture on stdin, answers
// it with the frozen engine's own pure decision cores (imported by path from
// CLAUDINITE_NODE_ENGINE) and prints the answers as JSON. The kind is argv[2].
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

const engine = process.env.CLAUDINITE_NODE_ENGINE;
if (!engine) throw new Error('CLAUDINITE_NODE_ENGINE is not set');
const mod = (rel) => import(pathToFileURL(join(engine, 'packs/claudinite-tasks', rel)).href);

const kind = process.argv[2];
const fixture = JSON.parse(readFileSync(0, 'utf8'));
const termsOf = (spec) => new Map(Object.entries(spec ?? {}).map(([name, t]) => [name, {
  signals: t.signals ?? [], needsItem: t.needsItem === true, takesArg: t.takesArg === true,
}]));

const kinds = {
  async contract() {
    const c = await mod('src/contract/task-contract.mjs');
    const p = await mod('src/contract/precondition-policy.mjs');
    return fixture.declarations.map((d) => {
      const terms = termsOf(d.terms);
      const normalized = c.normalizeTaskDeclaration(d.declaration);
      return {
        normalized: normalized ?? null,
        problems: c.validateTaskDeclaration(d.declaration, terms),
        signals: c.taskSignalNames(normalized, terms).sort(),
        needsItem: p.preconditionNeedsItem(normalized?.preconditions ?? [], terms),
        cadence: c.taskCadence(normalized),
        scheduled: c.isScheduledTask(normalized),
        codeWork: c.declaresCodeWork(normalized),
      };
    });
  },
  async precondition() {
    const p = await mod('src/contract/precondition-policy.mjs');
    const pre = await mod('src/contract/precondition.mjs');
    return fixture.cases.map((k) => {
      const task = { decl: { preconditions: k.preconditions }, terms: new Map() };
      return p.evaluatePreconditions({
        preconditions: k.preconditions,
        signals: k.signals ?? {},
        config: k.config ?? {},
        item: k.item ?? null,
        windowDays: pre.windowDaysOf(task, k.signals ?? {}),
        now: k.now ?? null,
        partial: k.partial === true,
      });
    });
  },
  async policy() {
    const m = await mod('src/contract/merge-policy.mjs');
    const declared = () => {
      const rules = new Map();
      const errors = [];
      for (const pack of fixture.packs ?? []) {
        const where = `${pack.id}/${m.MERGE_RULES_FILE}`;
        if (!Array.isArray(pack.rules)) { errors.push(`${where}: the file must hold an array of rule objects`); continue; }
        for (const spec of pack.rules) {
          try {
            const rule = m.compileDeclaredRule(spec, where);
            if (m.BUILTIN_MERGE_RULES.has(rule.name) || m.COMPOSITE_POLICIES.has(rule.name) || rules.has(rule.name)) {
              throw new Error(`rule name "${rule.name}" is already taken — merge-rule names are one flat namespace`);
            }
            rules.set(rule.name, rule);
          } catch (e) {
            errors.push(e.message.startsWith(where) ? e.message : `${where}: ${e.message}`);
          }
        }
      }
      return { rules, errors };
    };
    const { rules, errors } = declared();
    return {
      ruleErrors: errors,
      normalized: (fixture.normalize ?? []).map((raw) => ({ policy: m.normalizePolicy(raw), expression: m.policyExpression(raw) })),
      classes: (fixture.classify ?? []).map((f) => m.classifyPath(f)),
      verdicts: (fixture.cases ?? []).map((k) => m.policyVerdict({
        policy: k.policy, entries: k.entries, declaredRules: rules, ruleErrors: errors,
      })),
    };
  },
  async grammar() {
    const g = await mod('public/work-item-grammar.mjs');
    const issues = fixture.issues ?? [];
    return {
      titles: (fixture.titles ?? []).map((t) => g.parseWorkItemTitle(t)),
      bodies: (fixture.bodies ?? []).map((b) => ({
        fields: g.parseWorkItemBody(b),
        context: g.parseContextLines(b),
        progress: g.parseProgressLines(b),
        lastVerdict: g.parseLastVerdict(b),
        request: g.parseRequestFields(b, { gated: true }),
        requestUngated: g.parseRequestFields(b),
        human: g.humanTextOf(b),
      })),
      issues: issues.map((i) => ({
        status: g.statusOf(i) ?? null,
        statuses: g.statusesOn(i),
        park: g.parkKindOf(i) ?? null,
        origin: g.originOf(i) ?? null,
        outcome: g.outcomeOf(i) ?? null,
        queueItem: g.isQueueItem(i),
        blockingPark: g.isBlockingPark(i),
        facts: g.itemFacts(i),
      })),
      built: (fixture.build ?? []).map((spec) => g.workItemBody(spec)),
      edits: (fixture.edits ?? []).map((e) => {
        if (e.op === 'notBefore') return g.withNotBefore(e.body, e.value);
        if (e.op === 'woken') return g.withWoken(e.body, e.value);
        if (e.op === 'endsWhen') return g.withEndsWhen(e.body, e.value);
        if (e.op === 'target') return g.withTarget(e.body, e.value);
        if (e.op === 'section') return g.withSection(e.body, e.heading, e.lines);
        throw new Error(`unknown edit ${e.op}`);
      }),
      trailers: (fixture.messages ?? []).map((m) => ({
        task: g.taskFromMessage(m.message) ?? null,
        stamped: g.withTaskTrailer(m.message, m.task ?? null),
      })),
      taskPaths: (fixture.taskPaths ?? []).map((p) => g.taskIdFromPath(p) ?? null),
    };
  },
  async schedule() {
    const run = await mod('src/schedule/run.mjs');
    const rules = await mod('src/schedule/repair-rules.mjs');
    const c = await mod('src/contract/task-contract.mjs');
    const tasksOf = (specs) => (specs ?? []).map((t) => ({
      pack: t.pack, id: t.id, taskPath: t.taskPath, decl: c.normalizeTaskDeclaration(t.decl), terms: new Map(),
    }));
    // The plan's ops differ only in which fields a kind carries; an absent,
    // null, false, empty or zero field is the same answer on both sides.
    const lean = (v) => {
      if (Array.isArray(v)) return v.map(lean);
      if (v === null || typeof v !== 'object') return v;
      const out = {};
      for (const [k, x] of Object.entries(v)) {
        if (x === null || x === undefined || x === false || x === '' || x === 0) continue;
        if (Array.isArray(x) && x.length === 0) continue;
        out[k] = lean(x);
      }
      return out;
    };
    const plans = [];
    for (const k of fixture.plans ?? []) {
      const tasks = tasksOf(k.tasks);
      const items = structuredClone(k.items ?? []);
      const { ops, asked } = await run.planSchedulerRun({
        tasks, items, requests: k.requests ?? [], now: k.now,
        schedule: { disabledTasks: k.disabled ?? [] },
        stateOf: (n) => k.states?.[n] ?? null,
        evaluate: async (t) => k.verdicts?.[`${t.pack}/${t.id}`] ?? { run: false, reason: 'unstated' },
        progressAt: (i) => k.progress?.[i.number] ?? null,
        resolutionOf: (n) => k.resolutions?.[n] ?? null,
        doneAfter: rules.doneRunLookup(k.done ?? []),
      });
      plans.push({ ops: lean(ops), asked, items: items.map((i) => ({ number: i.number, state: i.state, labels: i.labels })) });
    }
    return {
      plans,
      wakes: (fixture.wakes ?? []).map((k) => run.planWake(k.spec, tasksOf(k.tasks), k.items ?? [])),
      pickable: (fixture.pickable ?? []).map((k) => run.pickableCount(k.open, k.readied ?? [], {
        scheduledOf: (id) => (id in (k.scheduled ?? {}) ? k.scheduled[id] : null),
      })),
      blockers: (fixture.blockers ?? []).map((k) => [...run.blockersToResolve(k.items ?? [], k.requests ?? [],
        new Map(Object.entries(k.known ?? {}).map(([n, s]) => [Number(n), s])))].sort((a, b) => a - b)),
    };
  },
  async queue() {
    const pick = await mod('src/items/pick-order.mjs');
    const ready = await mod('src/schedule/readiness.mjs');
    const hb = await mod('src/items/heartbeat.mjs');
    const rec = await mod('src/items/run-record.mjs');
    const anchors = await mod('src/items/anchors.mjs');
    const scheduled = (map) => (id) => (id in (map ?? {}) ? map[id] : null);
    return {
      picks: (fixture.picks ?? []).map((k) => {
        const draws = [...(k.draws ?? [])];
        return pick.pickOrder(k.open, {
          taskAfter: (id) => k.taskAfter?.[id] ?? [],
          scheduledOf: scheduled(k.scheduled),
          random: () => draws.shift() ?? 0,
        }).map((i) => i.number);
      }),
      releasable: (fixture.releasable ?? []).map((k) => ready.isReleasable(k.item, {
        stateOf: (n) => k.states?.[n] ?? null,
        nowMs: Date.parse(k.now),
      })),
      liveness: (fixture.liveness ?? []).map((comments) => ({
        live: hb.lastLivenessAt(comments),
        progress: hb.lastProgressAt(comments),
      })),
      progress: (fixture.progress ?? []).map((k) => hb.withProgress(k.body, k.line)),
      beats: (fixture.beats ?? []).map((k) => (k.session === undefined
        ? hb.heartbeatComment(k) : hb.agentBeatComment(k))),
      records: (fixture.records ?? []).map((line) => ({
        exec: rec.parseTaskExec(line),
        run: rec.parseTaskRun(line),
        cost: rec.parseRunCost(line),
      })),
      rendered: {
        exec: (fixture.renderExec ?? []).map((r) => rec.renderTaskExec(r)),
        cost: (fixture.renderCost ?? []).map((r) => rec.renderRunCost(r)),
      },
      anchors: (fixture.anchors ?? []).map((k) => ({
        next: anchors.nextAnchor(k.frequency, new Date(k.now))?.toISOString() ?? null,
        period: anchors.periodMs(k.frequency),
      })),
    };
  },
  async outcome() {
    const v = await mod('src/session/verify-outcome.mjs');
    return fixture.cases.map((k) => v.verifyOutcome(k));
  },
};

if (!kinds[kind]) throw new Error(`unknown kind ${kind}`);
process.stdout.write(`${JSON.stringify(await kinds[kind]())}\n`);
