// The Node half of the update parity face: reads one fixture's input on
// stdin, answers it with the frozen engine's update decision cores
// (packs/claudinite-lifecycle/updates/ and tasks/update/worker.mjs,
// imported by path from CLAUDINITE_NODE_ENGINE) and prints the answer as
// JSON. The core is argv[2].
import { execFileSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { readFileSync } from 'node:fs';

const engine = process.env.CLAUDINITE_NODE_ENGINE;
if (!engine) throw new Error('CLAUDINITE_NODE_ENGINE is not set');
const mod = (rel) => import(pathToFileURL(join(engine, 'packs/claudinite-lifecycle', rel)).href);

const core = process.argv[2];
const input = JSON.parse(readFileSync(0, 'utf8'));

// A git checkout holding head at HEAD and working laid over it, for
// changesTestsCouldSee; a null working file is a deletion.
function checkout(head, working) {
  const dir = mkdtempSync(join(tmpdir(), 'parity-update-'));
  const git = (...args) => execFileSync('git', ['-C', dir, ...args], { stdio: 'ignore' });
  git('init', '-q', '-b', 'main');
  const lay = (files) => {
    for (const [p, c] of Object.entries(files ?? {})) {
      const abs = join(dir, p);
      if (c === null) { rmSync(abs, { force: true }); continue; }
      mkdirSync(dirname(abs), { recursive: true });
      writeFileSync(abs, c);
    }
  };
  lay(head);
  git('add', '-A');
  git('-c', 'user.name=parity', '-c', 'user.email=p@x', 'commit', '-q', '--allow-empty', '-m', 'head');
  lay(working);
  return dir;
}

const cores = {
  async plan() {
    const m = await mod('updates/pack-update.mjs');
    return m.planPackUpdates(input.packs, input.declared, input.installed ?? null,
      { today: input.today, engineVersion: input.engineVersion });
  },
  async gap() {
    const m = await mod('updates/engine-update.mjs');
    return m.engineRecordsInGap(input.installed ?? null, { today: input.today });
  },
  async delivery() {
    const m = await mod('updates/engine-update.mjs');
    return m.deliveryDecision(input);
  },
  async applystage() {
    const m = await mod('updates/pack-update.mjs');
    return m.applyStageFor(input.specs ?? [], input.withheld ?? [], input.testVisible ?? []);
  },
  async terminal() {
    const m = await mod('updates/terminals.mjs');
    return m.terminalFor(input.outcome ?? null);
  },
  async convergescope() {
    const m = await mod('updates/converge-scope.mjs');
    let testVisible = null;
    if (input.checkout) {
      const dir = checkout(input.checkout.head, input.checkout.working);
      try { testVisible = m.changesTestsCouldSee(dir); } finally { rmSync(dir, { recursive: true, force: true }); }
    }
    return {
      bookkeeping: (input.files ?? []).map((f) => m.isConvergeBookkeeping(f)),
      stampOnly: (input.edits ?? []).map((e) => m.stampOnlySettingsEdit(e.before ?? null, e.after ?? null)),
      testVisible,
    };
  },
  async pulltext() {
    const m = await mod('tasks/update/worker.mjs');
    return {
      text: input.terminal ? m.updatePullText(input.terminal, { engine: input.engine, packs: input.packs }) : null,
      amends: (input.amends ?? []).map((a) => m.amendsPull(a.targetPr ?? null, a.pull ?? null)),
      marker: m.REHEARSAL_MARKER,
    };
  },
};

if (!cores[core]) throw new Error(`unknown core ${core}`);
process.stdout.write(`${JSON.stringify(await cores[core]())}\n`);
