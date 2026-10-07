// The Node half of the verify-answers parity face: reads one case's Node
// member files ({path: text}) on stdin, runs the frozen engine's
// world rule argv[2], from whichever pack carries it, over them and prints the
// findings' files as JSON.
import { existsSync, readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

const engine = process.env.CLAUDINITE_NODE_ENGINE;
if (!engine) throw new Error('CLAUDINITE_NODE_ENGINE is not set');
const id = process.argv[2];
const files = JSON.parse(readFileSync(0, 'utf8'));
const paths = Object.keys(files).sort();
let config = {};
try { config = JSON.parse(files['.claudinite-settings.json'] ?? '{}'); } catch { config = {}; }
const ctx = {
  root: '/member',
  files: paths,
  tracked: paths,
  read: (p) => (Object.hasOwn(files, p) ? files[p] : null),
  exists: (p) => Object.hasOwn(files, p),
  config,
  packs: [],
};
const module = readdirSync(join(engine, 'packs')).map((p) => join(engine, 'packs', p, 'worldRules', `${id}.mjs`)).find((f) => existsSync(f));
if (!module) throw new Error(`no pack carries worldRules/${id}.mjs`);
const { default: rule } = await import(pathToFileURL(module).href);
if (rule.id !== id) throw new Error(`${id}.mjs declares ${rule.id}`);
const found = await rule.run(ctx);
process.stdout.write(`${JSON.stringify(found.map((f) => f.file ?? null))}\n`);
