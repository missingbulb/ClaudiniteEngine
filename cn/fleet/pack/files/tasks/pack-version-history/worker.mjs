// The pack-version-history entry point: write the rows each pack's `VERSIONS.md` is
// missing and deliver the records that changed on a pull request that lands itself.
//
// Which commits each version shipped is `history.mjs`'s to answer; this file renders
// the rows and hands the changed records to `deliver.mjs`. A recompute
// that changes no record opens nothing.

import { execFileSync } from 'node:child_process';
import { git as engineGit } from '@claudinite/sdk';
import { deliver, readAt } from './deliver.mjs';
import { readHistory } from './history.mjs';
import { compareVersions } from './order.mjs';

export const TASK_ID = 'fleet/pack-version-history';

const DEFAULT_HEADER = [
  '# Version history',
  '',
  'One row per version of this pack, newest first: the pull requests that landed between the',
  'previous version and it. The weekly history task writes the rows a version is missing and',
  'leaves every row that already stands.',
  '',
  '| Version | Date | What changed |',
  '|---|---|---|',
];

const ROW = /^\|\s*(\d+(?:\.\d+)*)\s*\|/;
const SEPARATOR = /^\|(\s*-+\s*\|)+\s*$/;

// One generated row: the pull requests the version shipped, oldest first, each as its
// squash subject, which carries the pull request's number.
export function renderRow({ version, date, commits }) {
  const what = commits.length
    ? commits.map((c) => c.subject.replace(/\|/g, '\\|')).join('; ')
    : '_no pull request is attributed to this version_';
  return `| ${version} | ${date} | ${what} |`;
}

// The record's text with a row for each version `pack.missing` names. Every row and
// the header already in the file stand as written; a file with no table gains the
// default header.
export function renderHistory(existingText, pack) {
  const lines = (existingText ?? '').replace(/\n+$/, '').split('\n');
  const sep = lines.findIndex((l) => SEPARATOR.test(l.trim()));
  const header = sep < 0 ? DEFAULT_HEADER : lines.slice(0, sep + 1);
  const rows = (sep < 0 ? [] : lines.slice(sep + 1))
    .map((line) => ({ version: ROW.exec(line.trim())?.[1], line }))
    .filter((r) => r.version);
  const missing = new Set(pack.missing);
  for (const v of pack.versions) {
    if (missing.has(v.version)) rows.push({ version: v.version, line: renderRow(v) });
  }
  rows.sort((a, b) => compareVersions(b.version, a.version));
  return `${[...header, ...rows.map((r) => r.line)].join('\n')}\n`;
}

// Which records would change at `ref`, as `{ path: text }`.
export function planHistory(root, ref) {
  const files = {};
  for (const pack of readHistory(root, ref)) {
    if (!pack.missing.length) continue;
    const before = readAt(root, ref, pack.record);
    const after = renderHistory(before, pack);
    if (after !== before) files[pack.record] = after;
  }
  return files;
}

// The base branch's remote tip with the history behind it: a quiet pack's last version
// can be months back, and an Actions checkout is one commit deep.
async function fetchBase(root, base) {
  const shallow = execFileSync('git', ['-C', root, 'rev-parse', '--is-shallow-repository'], { encoding: 'utf8' }).trim() === 'true';
  const r = await engineGit('fetch', '--quiet', ...(shallow ? ['--unshallow'] : []), 'origin', base);
  if (r.code !== 0) throw new Error(`fetching ${base} failed: ${r.stderr.trim()}`);
  return execFileSync('git', ['-C', root, 'rev-parse', 'FETCH_HEAD'], { encoding: 'utf8' }).trim();
}

export async function worker({ root, defaultBranch, target, log }) {
  const base = defaultBranch ?? 'main';
  const tip = await fetchBase(root, base);
  const files = planHistory(root, tip);
  const changed = Object.keys(files);
  if (!changed.length) {
    log(`${base} at ${tip.slice(0, 10)}: every record already carries a row per version - nothing to deliver`);
    return;
  }
  for (const path of changed) log(`${path}: regenerated`);

  const pr = await deliver({
    root, base, target,
    files,
    subject: 'Claudinite: pack version history',
    title: 'Claudinite: pack version history',
    body: [
      'Wrote the rows each pack\'s `VERSIONS.md` was missing: a row per version naming the pull',
      'requests that landed between the previous version and it. Rows already present stand as',
      'written.',
      '',
      `Records touched: ${changed.map((p) => `\`${p}\``).join(', ')}.`,
    ].join('\n'),
  });
  log(`${changed.length} record(s) - ${pr.reused ? 'updated' : 'opened'} PR ${pr.number !== null ? `#${pr.number}` : `on ${pr.branch}`}`);
}
