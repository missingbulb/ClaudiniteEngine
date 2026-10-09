// The Work board's explore panels — one per status, because the reader's next move
// differs by status ([docs/work-board.md](../../docs/work-board.md), *The explore panel*).
//
// EVERY PANEL ENDS IN `do`: one imperative. A panel that only describes is a longer
// hover; the point of opening one is to know what to do next.
//
// EVERY FIELD NAMES ITS SOURCE, and a field this page cannot read says *not read*
// rather than being left out — a missing row and an unreadable one are different facts,
// and only the second is worth a reader's attention. The size of a PR, the checks on
// its head sha and the drain tick's hour are all *not read* here by design: each costs
// a request per mark, and the board is built not to spend one.
//
// Pure. The comments a panel quotes are fetched by the view and handed in; nothing here
// performs I/O, so every panel is a function of what was read.

import {
  parseWorkItemBody, parseWorkItemTitle, statusOf, labelNames, outcomeOf,
} from '../read/queue-vocabulary.mjs';
import {
  PARK_PREFIX, CLAIM_MARKER, HANDOFF_MARKER,
} from '../read/queue-vocabulary.mjs';
import { holdsOnFailure } from './task-calendar.mjs';

const NOT_READ = 'not read';
const ms = (t) => (t == null ? null : new Date(t).getTime());
const stamp = (t) => (t ? new Date(t).toISOString().replace('T', ' ').slice(0, 16) : NOT_READ);
const days = (from, to) => (from == null || to == null ? null : Math.round((to - from) / 86400e3));

// One field of a panel. `value` may be null, and then `note` says why — which is what
// keeps *not read* and *nothing there* apart on the page.
const field = (label, value, note = null) => ({ label, value: value ?? null, note: value == null ? (note ?? NOT_READ) : note });

// The command that converges this item, spelled as the executing session's
// instructions spell it, so a reader can paste it.
export function convergeCommand(item, repo, outcome = 'done') {
  return `.claudinite/bin/cn work converge --issue ${item.number} \\\n`
    + `  --outcome ${outcome} --summary "<what happened>" \\\n`
    + `  --repo ${repo} --item-file <the issue as JSON>`;
}

// Which panel a mark opens. The board's own row kinds, mapped to the six the spec
// names — a park splits by kind because an approval IS its PR and the other three are
// a question waiting for an answer.
export function panelKind(row) {
  if (row.kind === 'pr') return 'pending-pr';
  if (row.kind === 'task') return 'scheduled-task';
  if (row.parkKind === 'failure') return 'failed-task';
  if (row.parkKind === 'approval') return 'pending-pr';
  if (row.parkKind) return 'park';
  if (row.kind === 'issue') return 'plain-issue';
  return 'stuck-item';
}

// --- the six panels -------------------------------------------------------------------

export function pendingPrPanel(row, { item, declaration, repo, prs = [], items = [], comments = null }) {
  const body = item ? parseWorkItemBody(item.body) : null;
  const converge = comments?.find((c) => /AUTOMERGE:/i.test(c.body ?? ''));
  const closes = row.closesIssue ?? (item?.number ?? null);
  const unblocks = items.filter((i) => i.state === 'open' && parseWorkItemBody(i.body).blockedBy.includes(closes));
  return {
    kind: 'pending-pr',
    title: `${row.gutter} — ${row.title}`,
    fields: [
      field('waits for', row.waits
        ? row.why
        : (converge ? `the run said: ${firstLine(converge.body)}` : null),
      row.waits ? null : 'the run\'s own AUTOMERGE verdict is in a comment this page has not fetched'),
      field('policy', declaration?.automerge ?? body?.merge ?? null, 'neither the task declaration nor the item carries a merge policy'),
      field('closes / ends', closes ? `#${closes}${body?.endsWhen ? ` · ends when #${body.endsWhen} closes` : ''}` : null,
        'this PR names no closing issue in its body'),
      field('unblocks', unblocks.length ? unblocks.map((i) => `#${i.number}`).join(', ') : null,
        'nothing open is Blocked-by what this closes'),
      // Two facts that each cost a request per mark; the board is built not to spend
      // one, so they are stated as unread rather than quietly omitted.
      field('size · CI', null, 'not read — the PR page and its head sha\'s checks are a request each'),
      field('left by', runRecord(comments), comments ? 'no claim or hand-off comment on the item' : 'the item\'s comments have not been fetched'),
    ],
    do: row.waits
      ? `Review and merge ${row.gutter}, or give its item an Automerge policy.`
      : `Nothing to do — it lands itself. Watch ${row.gutter} for CI.`,
  };
}

export function failedTaskPanel(row, { item, repo, siblings = [], declaration = null, comments = null }) {
  const later = siblings
    .filter((i) => i.state === 'closed' && ms(i.closed_at) > ms(item?.created_at))
    .map((i) => `#${i.number} ${outcomeOf(i) ?? 'no outcome'}`);
  // No park holds a lane by itself: the declaration's own `last-run-not-failed` is
  // what stops a task past its failure, so the lane field reads the declaration —
  // and says so when it could not.
  const holds = Array.isArray(declaration?.preconditions) ? holdsOnFailure(declaration.preconditions) : null;
  const lane = holds === null ? null
    : !holds ? 'open — this declaration does not hold its lane on a failure, so the task is asked on schedule around the park'
      : later.length
        ? `the roster says this lane is HELD, and ${later.length} later occurrence(s) closed anyway: ${later.slice(0, 4).join(', ')}`
        : 'held — nothing new is scheduled for this task until it clears';
  return {
    kind: 'failed-task',
    title: `${row.gutter} — ${row.title}`,
    fields: [
      field('last run', runRecord(comments), comments ? 'no claim or hand-off comment on the item' : 'the item\'s comments have not been fetched'),
      field('what broke', comments ? firstLine(comments.find((c) => /converge|failed|error/i.test(c.body ?? ''))?.body) : null,
        comments ? 'no converge comment on the item' : 'the item\'s comments have not been fetched'),
      field('park history', comments ? janitorRules(comments).join(' · ') || null : null,
        comments ? 'no janitor rule comment on the item' : 'the item\'s comments have not been fetched'),
      // THE CONTRADICTION THIS PANEL EXISTS FOR: a declaration that holds its lane on
      // a failure, and later closed items of the same task as the record disagreeing.
      field('lane', lane, 'the task declaration has not been read — only its own `last-run-not-failed` holds a lane'),
    ],
    do: `Diagnose, then converge it:\n${convergeCommand(item ?? { number: row.gutter.replace('#', '') }, repo, 'done')}`,
  };
}

export function stuckItemPanel(row, { item, repo, items = [], prs = [], comments = null }) {
  const body = item ? parseWorkItemBody(item.body) : { blockedBy: [], notBefore: null, merge: null };
  const byNumber = new Map(items.map((i) => [i.number, i]));
  const chain = body.blockedBy.map((n) => {
    const blocker = byNumber.get(n);
    const pr = prs.find((p) => p.closesIssue === n && !p.merged_at);
    const mover = pr ? `PR #${pr.number} closes it when you merge` : (blocker && parseWorkItemTitle(blocker.title) ? 'its own task' : 'nobody');
    return `#${n} ${blocker ? (blocker.state === 'open' ? 'open' : 'closed') : NOT_READ} — moved by ${mover}`;
  });
  return {
    kind: 'stuck-item',
    title: `${row.gutter} — ${row.title}`,
    fields: [
      field('the chain', chain.length ? chain.join('\n') : null, 'this item is Blocked-by nothing'),
      field('placed at', row.at ? stamp(row.at) : null, row.why),
      field('the premise', item ? `filed ${stamp(item.created_at)} · ${days(ms(item.created_at), Date.now())} d old` : null),
      field('landing', body.merge ?? null, 'no Merge policy — its PR will wait for a person'),
      field('janitor', comments ? janitorRules(comments).join(' · ') || null : null,
        comments ? 'no janitor comment on the item' : 'the item\'s comments have not been fetched'),
    ],
    do: row.broken
      ? `Close #${row.blocker}, give it a task, or re-scope ${row.gutter} so it no longer waits on it.`
      : `Leave it — a future Not-before is the mechanism working. Re-scope only if the premise has moved.`,
  };
}

export function plainIssuePanel(row, { item, items = [], prs = [], now }) {
  const blocks = items.filter((i) => parseWorkItemBody(i.body).blockedBy.includes(item?.number));
  const labels = item ? labelNames(item) : [];
  const idle = days(ms(item?.updated_at), now);
  return {
    kind: 'plain-issue',
    title: `${row.gutter} — ${row.title}`,
    fields: [
      field('age · idle', item ? `${days(ms(item.created_at), now)} d old · idle ${idle} d` : null),
      field('blocks', blocks.length ? blocks.map((i) => `#${i.number}`).join(', ') : null, 'nothing is Blocked-by it'),
      field('who moves it', prs.some((p) => p.closesIssue === item?.number)
        ? `PR #${prs.find((p) => p.closesIssue === item.number).number}`
        : null,
      'no queue mark, no Task: field, and no open PR closes it — nothing scheduled will touch it'),
      field('rot', idle >= 14 ? `idle ${idle} d, past the 14 d bar` : `idle ${idle} d`),
      field('quick-win', labels.includes('quick-win')
        ? (blocks.length ? `yes — closing it releases ${blocks.length} row(s)` : 'yes — and it unblocks nothing')
        : null, 'not labelled quick-win'),
    ],
    do: blocks.length
      ? `Close ${row.gutter} — ${blocks.length} row(s) are waiting behind it.`
      : `Decide: close it, label it quick-win, or let it rot knowingly.`,
  };
}

export function scheduledTaskPanel(row, { siblings = [], declaration = null, cost = null }) {
  const recent = siblings
    .filter((i) => i.state === 'closed')
    .sort((a, b) => ms(b.closed_at) - ms(a.closed_at))
    .slice(0, 8)
    .map((i) => `${(i.closed_at ?? '').slice(5, 10)} ${outcomeOf(i) ?? 'none'}`);
  return {
    kind: 'scheduled-task',
    title: row.task ?? row.key,
    fields: [
      field('last occurrences', recent.length ? recent.join(' · ') : null, 'no closed occurrence in the window'),
      field('next anchor', row.row?.nextAsk?.at ? stamp(row.row.nextAsk.at) : null, row.row?.anchorNote ?? 'no anchor — the declaration names no cadence'),
      field('model', declaration?.agent_model ?? 'none · code-work'),
      field('expected outcome', declaration?.expected_outcome ?? null, 'the declaration names none'),
      field('automerge', declaration?.automerge ?? null, 'the declaration names none — its PRs wait for a person'),
      field('cost per run', cost && cost.sessions ? `${Math.round((cost.tokensIn ?? 0) / cost.sessions).toLocaleString('en-US')} tok/session` : null,
        'this fold carries no per-task cost yet'),
    ],
    do: 'Nothing — it is scheduled. Change its cadence or model in its own task.json.',
  };
}

export function parkPanel(row, { item, repo, comments = null }) {
  const body = item ? parseWorkItemBody(item.body) : {};
  return {
    kind: 'park',
    title: `${row.gutter} — ${row.title}`,
    fields: [
      field('the ask', comments ? firstLine(comments[comments.length - 1]?.body) : null,
        comments ? 'the item carries no comment stating the ask' : 'the item\'s comments have not been fetched'),
      field('kind', row.parkKind ? `needs-human-${row.parkKind}` : null),
      // The one thing that would close this without a person, if it exists.
      field('would close itself when', body.endsWhen ? `#${body.endsWhen} closes` : null,
        'no Ends-when condition — only a person clears this'),
    ],
    do: `Answer it, then converge:\n${convergeCommand(item ?? { number: 0 }, repo, row.parkKind === 'approval' ? 'done' : row.parkKind ?? 'done')}`,
  };
}

// --- the run record a panel quotes ------------------------------------------------------

// The claim → hand-off → converge beats, from the markers the queue's own protocol
// writes. Read from the item's comments, which the view fetches for the opened mark.
export function runRecord(comments) {
  if (!comments) return null;
  const beat = (marker, name) => {
    const hit = comments.find((c) => (c.body ?? '').includes(marker));
    return hit ? `${name} ${stamp(hit.created_at)}` : null;
  };
  const beats = [beat(CLAIM_MARKER, 'claimed'), beat(HANDOFF_MARKER, 'handed off')].filter(Boolean);
  return beats.length ? beats.join(' → ') : null;
}

// The janitor names its own rule in the comment it leaves, which is what makes a park's
// history readable at all.
export const janitorRules = (comments) => (comments ?? [])
  .filter((c) => /janitor|rule [A-H]\b/i.test(c.body ?? ''))
  .map((c) => firstLine(c.body));

const firstLine = (body) => {
  const line = String(body ?? '').split('\n').map((l) => l.replace(/<!--.*?-->/g, '').trim()).find(Boolean);
  return line ? line.slice(0, 200) : null;
};

// --- the whole panel --------------------------------------------------------------------

export function buildPanel(row, context) {
  switch (panelKind(row)) {
    case 'pending-pr': return pendingPrPanel(row, context);
    case 'failed-task': return failedTaskPanel(row, context);
    case 'scheduled-task': return scheduledTaskPanel(row, context);
    case 'plain-issue': return plainIssuePanel(row, context);
    case 'park': return parkPanel(row, context);
    default: return stuckItemPanel(row, context);
  }
}
