// The Node half of the usage parity face: runs ClaudinitePacks' usage-fold
// worker (packs/claudinite-tasks/tasks/usage-fold/worker.mjs, imported by
// path from CLAUDINITE_PACKS_TREE) over the member checkout one world on
// stdin names. The SDK's engine end is the packs repo's own
// tools/test/sdk-stand-in.mjs, the REST API is the harness's server at the
// world's api, and the clock is pinned to its now. Prints what the run
// said, { opened, error, logs }; the harness reads the files it pushed.
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

const tree = process.env.CLAUDINITE_PACKS_TREE;
if (!tree) throw new Error('CLAUDINITE_PACKS_TREE is not set');
const world = JSON.parse(readFileSync(0, 'utf8'));

const API = 'https://api.github.com';
const NOW = new Date(world.now).getTime();
const RealDate = Date;
class PinnedDate extends RealDate {
  constructor(...args) {
    if (args.length === 0) super(NOW);
    else super(...args);
  }

  static now() { return NOW; }
}
globalThis.Date = PinnedDate;

const realFetch = globalThis.fetch;
globalThis.fetch = (url, init) => realFetch(String(url).replace(API, world.api), init);

const { installSdk, gitIn } = await import(pathToFileURL(join(tree, 'tools/test/sdk-stand-in.mjs')).href);
const opened = [];
const sdk = installSdk({
  params: {
    root: world.root, repo: world.repo, defaultBranch: 'main', pack: world.pack, task: world.task,
    automerge: world.automerge ?? null, target: { mode: null, branch: world.target?.branch ?? null, pr: world.target?.pr ?? null },
  },
  answers: {
    git: gitIn(world.root),
    config: () => world.packConfig ?? {},
    packs: () => world.packs ?? [],
    'github.openPr': (args) => { opened.push(args); return { number: 41 }; },
  },
});

const { worker } = await import(pathToFileURL(join(tree, 'packs/claudinite-tasks/tasks/usage-fold/worker.mjs')).href);
let error = null;
try {
  await worker(sdk.params);
} catch (err) {
  error = err?.message ?? String(err);
}
process.stdout.write(`${JSON.stringify({ opened, error, logs: sdk.params.lines })}\n`);
