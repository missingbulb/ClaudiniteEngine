// Is a default-branch commit GENUINE PROJECT WORK, or the machinery moving? THE
// DASHBOARD'S OWN COPY of the engine's test, which its signal collectors gate a
// precondition on: the page classifies commits in a browser, so it carries the test it
// marks a member sleepy with.

import { TASK_TRAILER } from '../read/queue-vocabulary.mjs';

// `Claudinite-Task: <pack>/<task>` on its own line, anywhere in the message: the commit
// says itself that a scheduled task wrote it.
const TASK_TRAILER_RE = new RegExp(`^${TASK_TRAILER}:[ \\t]*(\\S+)[ \\t]*$`, 'm');
const taskFromMessage = (message) => TASK_TRAILER_RE.exec(message ?? '')?.[1] ?? null;

// Bot/CI housekeeping and Claudinite's own automated writes, by MESSAGE. The queue's
// own vocabulary (`[claudinite-task]`, `[claudinite-work]`) is excluded here so
// neither dispatch mechanism ever self-triggers: a queue-mode repo's work items are
// repo activity to every precondition watching issues.
export const HOUSEKEEPING = /\[skip ci\]|(^|\n)\s*baselin(e|ing)\b|claudinite[ -](baselin|maintenance|growth|task|work)|seed default-on/i;

// The exclusion the message cannot express: a commit that touched nothing outside
// `.claudinite/` moved the repo's own working rules, not the project. Every consumer
// means "genuine project work" by this — something shippable changed (store-release),
// there is a lesson to extract (growth-extract), a comment may have drifted
// (improve-comments) — and none of those is true of a corpus edit. Message and author
// cannot catch it: a human landing a lesson PR writes an ordinary message under their
// own login, so the growth lifecycle's own landed output re-armed it the next night
// and a repo could never go quiet (TLDR #319).
//
// An empty list is UNKNOWN, never "touched only .claudinite/" — a bare `every` is
// vacuously true on it and would silently retire the trigger for every commit whose
// detail read failed. Require at least one known path before the exclusion applies.
const CORPUS_ONLY = (files) => files.length > 0 && files.every((f) => f.startsWith('.claudinite/'));

// The test. `files` is the commit's changed paths where the caller resolved them, and
// `null` where it did not: a reader working from a commit LISTING has no file list and
// cannot afford a read per commit, so it gets the author, trailer and message
// exclusions and states that the corpus-only one did not run. The trailer is the
// AUTHORITY for anything written after it existed — a commit a scheduled task wrote
// says so itself — and the message and author exclusions stay because history
// predating the trailer still needs classifying and because they also cover
// non-task housekeeping.
export function isSubstantiveCommit(commit, files = null) {
  const login = commit?.author?.login ?? commit?.author ?? '';
  if (typeof login === 'string' && login.endsWith('[bot]')) return false;
  const message = commit?.commit?.message ?? commit?.message ?? '';
  if (taskFromMessage(message)) return false;
  if (files !== null && CORPUS_ONLY(files)) return false;
  return !HOUSEKEEPING.test(message);
}
