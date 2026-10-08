// A canon shelf's version history, read off git: for each pack under packs/, the
// commit that last moved its manifest's version, the shipping files changed since,
// and the pull requests each version carried, with the versions its
// provenance/VERSIONS.md has no row for. A pack's content reaches a member only on a
// new version, so the per-version commits are what a VERSIONS.md row is made of.

import { spawnSync } from 'node:child_process';
import { compareVersions } from './order.mjs';

const SHELF = 'packs';
const RECORD = 'provenance/VERSIONS.md';
const MANIFESTS = ['pack.json', 'pack.yaml', 'pack.toml', 'pack.mjs'];
// The commits it stamped moved only version numbers, so they shipped nothing.
const BUMP_TASK = 'claudinite-canon-curation/pack-version-bump';
// The folders a pack's vendored set leaves out at its root; it also leaves out the
// Go tests beside its checks.
const DROPPED_AT_ROOT = new Set(['test', 'docs', 'provenance']);

const MANIFEST_VERSION = /(?:^|[\s{,])"?version"?:\s*['"]?(\d+(?:\.\d+)*)['"]?/m;
const ROW = /^\|\s*(\d+(?:\.\d+)*)\s*\|/;
const PULL = /\(#(\d+)\)\s*$/;

// Whether a repository path rides its pack's vendored set to a member.
export function isShipping(p) {
  const parts = p.split('/');
  if (parts.length < 3 || parts[0] !== SHELF || parts[1] === '') return false;
  if (parts.length > 3 && DROPPED_AT_ROOT.has(parts[2])) return false;
  return !(parts[2] === 'checks' && parts.length === 4 && parts[3].endsWith('_test.go'));
}

export function declaredVersion(text) {
  return MANIFEST_VERSION.exec(text ?? '')?.[1] ?? '';
}

export function rowVersions(text) {
  return (text ?? '').split('\n').map((l) => ROW.exec(l.trim())?.[1]).filter(Boolean);
}

const same = (a, b) => compareVersions(a, b) === 0;

function gitAt(root) {
  const run = (...args) => spawnSync('git', ['-C', root, ...args], { encoding: 'utf8', maxBuffer: 256 * 1024 * 1024 });
  const out = (...args) => {
    const r = run(...args);
    if (r.error) throw r.error;
    if (r.status !== 0) throw new Error(`git ${args[0]} exited ${r.status}: ${r.stderr.trim()}`);
    return r.stdout;
  };
  const fileAt = (ref, p) => {
    const r = run('show', `${ref}:${p}`);
    return r.status === 0 ? r.stdout : null;
  };
  return { out, fileAt };
}

function manifestAt(git, ref, id) {
  for (const f of MANIFESTS) {
    const p = `${SHELF}/${id}/${f}`;
    const text = git.fileAt(ref, p);
    if (text !== null) return { path: p, text };
  }
  return { path: '', text: '' };
}

function packIds(git, ref) {
  const ids = new Set();
  for (const p of git.out('ls-tree', '--name-only', '-r', ref, `${SHELF}/`).split('\n')) {
    const parts = p.split('/');
    if (parts.length === 3 && MANIFESTS.includes(parts[2])) ids.add(parts[1]);
  }
  return [...ids].sort();
}

// The first-parent commits that moved id's version, newest first; a pack's first
// commit counts.
function bumps(git, ref, id) {
  const log = git.out('log', '--first-parent', '--format=%H %cs', ref, '--', ...MANIFESTS.map((f) => `${SHELF}/${id}/${f}`));
  const out = [];
  for (const line of log.trim().split('\n')) {
    const [sha, date] = line.split(' ');
    if (!date) continue;
    const here = declaredVersion(manifestAt(git, sha, id).text);
    if (!here) continue;
    const before = declaredVersion(manifestAt(git, `${sha}^`, id).text);
    if (!before || !same(here, before)) out.push({ sha, version: here, date });
  }
  return out;
}

// The commits each bump shipped, oldest version first.
function versions(git, moves, id) {
  const out = [];
  for (let i = moves.length - 1; i >= 0; i -= 1) {
    const range = i < moves.length - 1 ? `${moves[i + 1].sha}..${moves[i].sha}` : moves[i].sha;
    const raw = git.out('log', '--first-parent', '--name-only',
      '--format=%x01%H%x00%s%x00%(trailers:key=Claudinite-Task,valueonly)%x00', range, '--', `${SHELF}/${id}/`);
    const commits = [];
    for (const block of raw.split('\x01')) {
      if (!block) continue;
      const [sha = '', subject = '', task = '', names = ''] = block.split('\x00');
      if (task.trim() === BUMP_TASK) continue;
      if (!names.split('\n').some((p) => isShipping(p.trim()))) continue;
      const pr = PULL.exec(subject);
      commits.push({ sha, subject, pr: pr ? Number(pr[1]) : null });
    }
    out.push({ version: moves[i].version, date: moves[i].date, commits: commits.reverse() });
  }
  return out;
}

function readPack(git, ref, id) {
  const manifest = manifestAt(git, ref, id);
  if (!manifest.path) throw new Error(`no manifest for pack "${id}" under ${SHELF}/ at ${ref}`);
  const pack = {
    id, manifest: manifest.path, version: declaredVersion(manifest.text),
    record: `${SHELF}/${id}/${RECORD}`, missing: [], lastBump: null, shippingSince: [], versions: [],
  };
  const moves = bumps(git, ref, id);
  if (moves.length) {
    pack.lastBump = moves[0];
    pack.shippingSince = git.out('diff', '--name-only', moves[0].sha, ref, '--', `${SHELF}/${id}/`)
      .split('\n').filter((p) => p && isShipping(p));
  }
  pack.versions = versions(git, moves, id);
  const rows = rowVersions(git.fileAt(ref, pack.record));
  pack.missing = pack.versions.map((v) => v.version).filter((v) => !rows.some((r) => same(r, v)));
  return pack;
}

// Every pack `ids` names at `ref`, or every pack on the shelf there when it names
// none. A shallow clone is refused: the walk would answer from its horizon.
export function readHistory(root, ref = 'HEAD', ids = []) {
  const git = gitAt(root);
  if (git.out('rev-parse', '--is-shallow-repository').trim() !== 'false') {
    throw new Error('the clone is shallow, so the version walk would answer from its horizon - fetch the full history first (git fetch --unshallow)');
  }
  return (ids.length ? ids : packIds(git, ref)).map((id) => readPack(git, ref, id));
}
