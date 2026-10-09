// Landing the regenerated records: read the base branch's remote tip, write the new
// files as a commit on top of it, push that to the branch the executor
// resolved, and open the pull request there unless the executor named one to amend.
//
// The remote is reached only through the engine's `git` (fetch and push carry the
// job's token there and nowhere else) and the pull request only through its `openPr`
// action, which is also what tells the executor there is a pull request to land. The
// commit itself is local plumbing on a scratch index, so the checkout's own index and
// work tree — which the executor's other items share — are never touched.
import { execFileSync } from 'node:child_process';
import { rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { commitMessage, git as engineGit, github } from '@claudinite/sdk';

const BOT = {
  GIT_AUTHOR_NAME: 'claudinite[bot]', GIT_AUTHOR_EMAIL: 'claudinite@users.noreply.github.com',
  GIT_COMMITTER_NAME: 'claudinite[bot]', GIT_COMMITTER_EMAIL: 'claudinite@users.noreply.github.com',
};

const local = (root, args, opts = {}) => execFileSync('git', ['-C', root, ...args], {
  encoding: 'utf8', maxBuffer: 256 * 1024 * 1024, stdio: ['pipe', 'pipe', 'pipe'], ...opts,
});

async function remote(...args) {
  const r = await engineGit(...args);
  if (r.code !== 0) throw new Error(`git ${args[0]} exited ${r.code}: ${r.stderr.trim()}`);
  return r.stdout;
}

// The base branch's remote tip, fetched into the checkout's object store. Read from
// the remote, never from HEAD, which may be sitting on another item's branch.
export async function baseTip(root, base) {
  await remote('fetch', '--quiet', 'origin', base);
  return local(root, ['rev-parse', 'FETCH_HEAD']).trim();
}

// One file's content at a commit, or null when the path does not exist there.
export function readAt(root, sha, path) {
  try { return local(root, ['show', `${sha}:${path}`]); } catch { return null; }
}

// A ROLLING file's prior state - one whose next version is folded from its last, so
// losing it loses history. Read at `path`, or at `legacyPath` where the file has not
// moved yet; `moves` is what to hand `commitFiles` so the old bytes arrive at the new
// path before the fold writes on top of them.
export function readRollingAt(root, sha, path, legacyPath = null) {
  const text = readAt(root, sha, path);
  if (text !== null || !legacyPath) return { text, moves: {} };
  const legacy = readAt(root, sha, legacyPath);
  return legacy === null ? { text: null, moves: {} } : { text: legacy, moves: { [legacyPath]: path } };
}

const blobAt = (root, sha, path) => {
  try { return local(root, ['rev-parse', '--verify', '--quiet', `${sha}:${path}`]).trim() || null; } catch { return null; }
};

// `files` ({ path: content }) as a commit on `baseSha`. Each move whose `from` is on
// the base and whose `to` is not lands as its OWN commit first, the blob unchanged, so
// the history shows a pure rename carrying every byte the old path held; a move whose
// target already exists is skipped and the old file left where it is.
export function commitFiles(root, { baseSha, files, moves = {}, message, moveMessage }) {
  const index = join(tmpdir(), `claudinite-history-${process.pid}-${Date.now()}.index`);
  const env = { ...process.env, ...BOT, GIT_INDEX_FILE: index };
  const plumb = (args, opts = {}) => local(root, args, { env, ...opts });
  const commitTree = (parent, msg) => plumb(['commit-tree', plumb(['write-tree']).trim(), '-p', parent, '-m', msg]).trim();
  try {
    plumb(['read-tree', baseSha]);
    let parent = baseSha;
    const moved = [];
    for (const [from, to] of Object.entries(moves)) {
      const blob = blobAt(root, baseSha, from);
      if (!blob || blobAt(root, baseSha, to)) continue;
      plumb(['update-index', '--add', '--cacheinfo', `100644,${blob},${to}`]);
      plumb(['update-index', '--force-remove', from]);
      moved.push(`${from} -> ${to}`);
    }
    if (moved.length) parent = commitTree(parent, moveMessage(moved));
    for (const [path, content] of Object.entries(files)) {
      const blob = plumb(['hash-object', '-w', '--stdin'], { input: content }).trim();
      plumb(['update-index', '--add', '--cacheinfo', `100644,${blob},${path}`]);
    }
    return commitTree(parent, message);
  } finally {
    rmSync(index, { force: true });
  }
}

// Deliver `files` on the branch and pull request the executor resolved: `target.branch`
// is required, since an outcome that opens a pull request always resolves one, and
// `target.pr` names the open one to amend. Returns { branch, number, reused }.
export async function deliver({ root, base, target, files, moves = {}, subject, title, body }) {
  if (!target?.branch) {
    throw new Error('no branch to deliver on — the executor resolves it and hands it in as the target branch');
  }
  // The push below is a force-push: right for a branch this task alone writes, and
  // never for the base, whose history it would rewrite.
  if (target.branch === base) {
    throw new Error(`refusing to force-push to ${base}, the base branch — a delivery lands on the task's own branch`);
  }
  const baseSha = await baseTip(root, base);
  const commit = commitFiles(root, {
    baseSha, files, moves,
    message: commitMessage(subject),
    moveMessage: (moved) => commitMessage(`Move ${moved.length === 1 ? 'a rolling file' : 'rolling files'} to their new home, content unchanged`, moved.join('\n')),
  });
  await remote('push', '--quiet', '--force', 'origin', `${commit}:refs/heads/${target.branch}`);
  if (target.pr) return { branch: target.branch, number: Number(target.pr), reused: true };
  const pr = await github.openPr({ title, body, head: target.branch, base });
  return { branch: target.branch, number: pr?.number ?? null, reused: false };
}
