// The Node half of the dashboard parity face: reads one fixture's input on
// stdin, answers it with the frozen dashboard pack's pure modules
// (packs/claudinite-dashboard, imported by path from CLAUDINITE_NODE_ENGINE,
// where their engine/ imports resolve), and prints the answer as JSON. The
// core is argv[2]:
//
//   descriptor  parseDescriptor(text, pack) and the descriptor-usable
//               findings over that one file at packs/<pack>/dashboard.json,
//               in the shape `cn dashboard descriptor --json` prints
//   usable      the descriptor-usable rule's run over a tree of files
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

const engine = process.env.CLAUDINITE_NODE_ENGINE;
if (!engine) throw new Error('CLAUDINITE_NODE_ENGINE is not set');
const dashboard = (rel) => import(pathToFileURL(join(engine, 'packs/claudinite-dashboard', rel)).href);

const core = process.argv[2];
const input = JSON.parse(readFileSync(0, 'utf8'));

// A finding as both engines compare it: the file and the two sentences.
const shown = (f) => ({ file: f.file, what: f.what, fix: f.fix });

async function usable(files) {
  const rule = (await dashboard('worldRules/descriptor-usable.mjs')).default;
  const paths = Object.keys(files).sort();
  return rule.run({ allFiles: paths, read: (f) => files[f] ?? null }).map(shown);
}

const cores = {
  async descriptor() {
    const c = await dashboard('src/read/contributions.mjs');
    const d = c.parseDescriptor(input.text, input.pack);
    const problems = (await usable({ [`packs/${input.pack}/dashboard.json`]: input.text })).map(({ what, fix }) => ({ what, fix }));
    if (d.fault) return { pack: d.pack, widgets: null, repo: null, fleet: null, fault: d.fault, problems };
    return {
      pack: d.pack,
      widgets: [...d.widgets.values()],
      repo: d.repo,
      fleet: { member: d.member, deployment: d.deployment },
      fault: null,
      problems,
    };
  },
  async usable() {
    return usable(input.files);
  },
};

const answer = cores[core];
if (!answer) throw new Error(`no dashboard core ${core}`);
process.stdout.write(`${JSON.stringify(await answer())}\n`);
