// The Node half of the pack-history parity face: the frozen engine's
// pack-versions.mjs walked over the repository in the working directory,
// printed in `cn pack history --json`'s shape. Arguments are pack ids,
// `--ref REF` and `--json`; no id is every pack on the shelf.
import { execFileSync } from 'node:child_process';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

const engine = process.env.CLAUDINITE_NODE_ENGINE;
if (!engine) throw new Error('CLAUDINITE_NODE_ENGINE is not set');
const pv = await import(pathToFileURL(join(engine, 'packs/claudinite-canon-curation/pack-versions.mjs')).href);
const git = (args) => execFileSync('git', args, { encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'] });

const argv = process.argv.slice(2);
let ref = 'HEAD';
const ids = [];
for (let i = 0; i < argv.length; i += 1) {
  if (argv[i] === '--ref') ref = argv[++i];
  else if (argv[i] !== '--json') ids.push(argv[i]);
}
const packs = (ids.length ? ids : pv.shelfPacks(git, ref)).map((id) => {
  const manifest = pv.manifestAt(git, ref, id);
  const record = pv.versionsPath(id);
  const bumps = pv.bumpCommits(git, ref, id);
  const versions = pv.versionHistory(git, ref, id).map((v) => ({ version: String(v.version), date: v.date, commits: v.commits }));
  const rows = pv.rowVersions(pv.fileAt(git, ref, record));
  return {
    id,
    manifest: manifest.path,
    version: String(pv.declaredPackVersion(manifest.text)),
    record,
    missing: versions.filter((v) => !rows.some((r) => String(r.version) === v.version)).map((v) => v.version),
    lastBump: bumps[0] ? { sha: bumps[0].sha, version: String(bumps[0].version), date: bumps[0].date } : null,
    shippingSince: bumps[0] ? pv.shippingChanges(git, bumps[0].sha, ref, id) : [],
    versions,
  };
});
process.stdout.write(`${JSON.stringify(packs, null, 2)}\n`);
