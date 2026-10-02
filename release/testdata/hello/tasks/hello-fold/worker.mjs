import { writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { commitMessage, engine, git, github, log } from '@claudinite/sdk';

const run = async (...args) => {
  const r = await git(...args);
  if (r.code !== 0) throw new Error(`git ${args.join(' ')}: ${r.stderr.trim()}`);
  return r.stdout.trim();
};

export async function worker(params) {
  log(`sdk ${engine().version}, HELLO_FOLD_SECRET ${params.secrets.HELLO_FOLD_SECRET ? 'handed over' : 'missing'}`);
  const { branch, pr } = params.target;
  if (!branch) {
    log('no target branch: nothing to fold');
    return;
  }
  const head = await run('rev-parse', 'HEAD');
  const was = await run('rev-parse', '--abbrev-ref', 'HEAD');
  // One executor run drains several items from this checkout: leave it as found.
  try {
    await run('checkout', '-B', branch);
    writeFileSync(join(params.root, 'HELLO_FOLD.json'), `${JSON.stringify({ item: params.item.number, foldedFrom: head }, null, 2)}\n`);
    await run('add', 'HELLO_FOLD.json');
    await run('-c', 'user.name=claudinite', '-c', 'user.email=claudinite@users.noreply.github.com', '-c', 'commit.gpgsign=false',
      'commit', '-q', '-m', commitMessage('hello: fold the main branch', `Folded from ${head}.`));
    await run('push', '--force', 'origin', `HEAD:refs/heads/${branch}`);
  } finally {
    await git('checkout', '-q', was === 'HEAD' ? head : was);
  }
  if (pr) {
    log(`amended #${pr}`);
    return;
  }
  const opened = await github.openPr({ title: 'hello: fold the main branch', body: `Folded from ${head}.`, head: branch });
  log(`opened #${opened.number}`);
}
