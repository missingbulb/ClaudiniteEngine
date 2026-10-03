// The Node half of the growth parity face: reads one fixture's input on
// stdin, answers it with the frozen engine's capture and prune cores
// (packs/claudinite-growth/capture-log.mjs and
// tasks/logs-prune/prune-logs.mjs, imported by path from
// CLAUDINITE_NODE_ENGINE) and prints the answer as JSON. The core is
// argv[2].
import { mkdtempSync, mkdirSync, writeFileSync, rmSync, utimesSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, relative } from 'node:path';
import { pathToFileURL } from 'node:url';

const engine = process.env.CLAUDINITE_NODE_ENGINE;
if (!engine) throw new Error('CLAUDINITE_NODE_ENGINE is not set');
const mod = (rel) => import(pathToFileURL(join(engine, 'packs/claudinite-growth', rel)).href);

const core = process.argv[2];
const input = JSON.parse(readFileSync(0, 'utf8'));

// A stream's lines as the capture reads them.
const lines = (capture, text) => capture.parseLines(text);

const cores = {
  async parselines() {
    const c = await mod('capture-log.mjs');
    return c.parseLines(input.text);
  },
  async bundle() {
    const c = await mod('capture-log.mjs');
    return c.bundleStreams(input.streams.map((t) => lines(c, t))).map(({ raw, ts }) => ({ raw, ts }));
  },
  async slice() {
    const c = await mod('capture-log.mjs');
    const bundled = c.bundleStreams(input.streams.map((t) => lines(c, t)));
    return { delta: c.sliceAfter(bundled, input.lastTs ?? null).map((l) => l.raw), max: c.maxTimestamp(bundled) };
  },
  async redactions() {
    const c = await mod('capture-log.mjs');
    let extra = input.extra ?? [];
    if (input.credentials !== undefined) {
      const home = mkdtempSync(join(tmpdir(), 'parity-growth-'));
      try {
        mkdirSync(join(home, '.claude'), { recursive: true });
        writeFileSync(join(home, '.claude', '.credentials.json'), JSON.stringify(input.credentials));
        extra = [...extra, ...c.credentialStoreValues(home)];
      } finally { rmSync(home, { recursive: true, force: true }); }
    }
    return c.buildRedactionValues(input.env ?? {}, extra);
  },
  async scrub() {
    const c = await mod('capture-log.mjs');
    return c.scrub(input.text, input.redactions ?? []);
  },
  async logname() {
    const c = await mod('capture-log.mjs');
    return c.logFilename(input.now, { pr: input.pr ?? null, issue: input.issue ?? null }, input.session);
  },
  async parsename() {
    const c = await mod('capture-log.mjs');
    return (input.names ?? []).map((n) => c.parseLogFilename(n));
  },
  async findtranscript() {
    const c = await mod('capture-log.mjs');
    const projects = mkdtempSync(join(tmpdir(), 'parity-growth-projects-'));
    try {
      for (const [rel, f] of Object.entries(input.files ?? {})) {
        const abs = join(projects, rel);
        mkdirSync(dirname(abs), { recursive: true });
        writeFileSync(abs, f.content ?? '{}\n');
        const at = new Date(f.mtime);
        utimesSync(abs, at, at);
      }
      const hit = c.findTranscript({ root: input.root, sessionId: input.sessionId ?? undefined, projects });
      return hit === null ? null : relative(projects, hit);
    } finally { rmSync(projects, { recursive: true, force: true }); }
  },
  async prune() {
    const p = await mod('tasks/logs-prune/prune-logs.mjs');
    return p.planPrune({ names: input.names, retentionDays: input.retentionDays, now: input.now });
  },
  async retention() {
    const p = await mod('tasks/logs-prune/prune-logs.mjs');
    return { days: p.resolveRetentionDays('declared' in input ? input.declared : undefined) };
  },
};

if (!cores[core]) throw new Error(`unknown core ${core}`);
process.stdout.write(`${JSON.stringify(await cores[core]())}\n`);
