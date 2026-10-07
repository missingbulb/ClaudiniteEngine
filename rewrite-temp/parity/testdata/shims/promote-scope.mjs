// The Node half of the promote-scope parity face: the frozen engine's
// promote-scope rule over the repository in the working directory, its
// context built against `--base REF` rather than the resolved default
// branch, printed and exited as its runCli does.
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

const engine = process.env.CLAUDINITE_NODE_ENGINE;
if (!engine) throw new Error('CLAUDINITE_NODE_ENGINE is not set');
const load = (p) => import(pathToFileURL(join(engine, p)).href);
const { default: rule } = await load('packs/claudinite-canon-curation/promote-scope.mjs');
const { readCorpusRoots } = await load('packs/claudinite-canon-curation/canon-config.mjs');
const { buildContext } = await load('engine/checks/helpers/repo-context.mjs');

const argv = process.argv.slice(2);
const base = argv[argv.indexOf('--base') + 1];
const ctx = buildContext({ root: process.cwd(), mode: 'changed', baseOverride: base });
if (!ctx.mergeBase) {
  console.error('promote-scope: no merge-base with the base branch — cannot scope the diff; refusing to certify.');
  process.exitCode = 2;
} else {
  const roots = readCorpusRoots((p) => ctx.read(p));
  const findings = rule.run(ctx);
  if (findings.length) {
    console.error(`promote-scope: FAIL — the promote phase may write only under ${roots.join(', ')}, but this branch also touches ${findings.length} path(s):`);
    for (const f of findings) console.error(`  - ${f.file}`);
    console.error('\nHome each promoted lesson in the corpus; leave anything that can only live elsewhere local. Do not reach past the corpus roots.');
    process.exitCode = 1;
  } else {
    console.log(`promote-scope: OK — every changed path is under ${roots.join(', ')}.`);
  }
}
