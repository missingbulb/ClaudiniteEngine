// The fleet view's derivation — many members' raw reads in, one ranked overview out.
// Pure: no clock, no I/O, no DOM.
//
// A fleet page answers a different question from the per-repo one. Per repo the
// question is "what is this scheduler doing"; across a fleet it is "WHERE DO I NEED
// TO LOOK", and a page that answers the first question twelve times over does not
// answer the second. So nothing here is a total for its own sake: every number is
// either something that needs a human, something that is about to, or the context
// needed to tell those apart.
//
// Three ideas do the work:
//
//   ATTENTION IS EARNED, NOT COUNTED. A member surfaces because something is true of
//   it — an item parked, a leash blown, a scheduler failing, a mount that stopped
//   converging — and each of those arrives as a REASON with a severity, not as a
//   number to be summed. The ranking is over the worst reason, so a member with one
//   parked item outranks one with forty healthy work items.
//
//   ABSENCE IS A STATE. A member that does not run Claudinite, one the viewer cannot
//   read, and one that is running fine are three different answers and never collapse
//   into "0". A fleet view whose blanks are ambiguous is worse than no fleet view.
//
//   ONE MEMBER'S FAILURE IS ONE ROW'S PROBLEM. Every member is summarised
//   independently and a read that failed becomes a row saying so, so a single private
//   repo or a rate-limit stumble cannot blank the page.

import { periodMs } from './task-calendar.mjs';
import {
  STATUS_BLOCKED, STATUS_READY, STATUS_RUNNING_EXECUTOR, STATUS_RUNNING_AGENT, URGENT, STATUS_NEEDS_HUMAN_APPROVAL, STATUS_NEEDS_HUMAN_ACTION,
} from '../read/queue-vocabulary.mjs';
import { outcomeOf, isParked } from '../read/queue-vocabulary.mjs';
import { isDormant } from '../read/dormancy.mjs';
import { verdictFor } from '../read/roster.mjs';
import { describeItem, isWorkItem, parseWorkItemTitle, taskDeclarationPaths, PARKED } from './model.mjs';
import { commitDays, commitClasses, DAY_MS } from './activity.mjs';
import { itemCandidate, pickCandidate } from './next-work.mjs';

// Severity ladder, worst first. The order IS the sort, so it is stated once here
// rather than implied by comparisons scattered through the render.
export const LEVELS = ['critical', 'serious', 'warning', 'info', 'ok'];
const levelRank = (l) => {
  const i = LEVELS.indexOf(l);
  return i === -1 ? LEVELS.length : i;
};

const ms = (t) => (t == null ? null : new Date(t).getTime());

// --- freshness, as the fleet's manager publishes it -------------------------------

// The Updates verdict for one member, from its row in the manager's roster artifact
// (`read/roster.mjs`) — the engine's own `fleet.Verdict`, judged by the sweep against
// what the member's own update would move it to. The page prices nothing itself: there
// is no canon to compare against, and pricing in the browser would mean npm packuments
// and signed pack indexes for every member on every load.
//
// The states are the verdict's own, by root cause: `fresh`, `behind` (the detail names
// each gap), `no-scheduler`, `no-stamp`, and `node` for a member the sweep does not
// compare. `unknown` is the page's, and it SAYS which absence it is — no roster in this
// deployment, no row for this repo, a row the sweep could not judge — rather than
// reading as current.
export const FRESHNESS_STATES = Object.freeze(['fresh', 'behind', 'no-scheduler', 'no-stamp', 'node']);

export const NO_ROSTER = 'unknown — this deployment runs no fleet-roster, so nothing published this member\'s freshness';

export function freshnessOf(verdict, { rostered = true } = {}) {
  if (!rostered) return { state: 'unknown', detail: NO_ROSTER };
  if (!verdict) return { state: 'unknown', detail: 'unknown — the fleet-roster artifact has no row for this repo' };
  if (verdict.dormant === true) return { state: 'dormant', detail: 'dormant — the fleet-roster does not measure it' };
  const f = verdict.freshness;
  if (f && FRESHNESS_STATES.includes(f.state)) return { state: f.state, detail: typeof f.detail === 'string' ? f.detail : '' };
  if (f?.state) return { state: 'unknown', detail: `unknown — the fleet-roster's state "${f.state}" is newer than this page` };
  if (verdict.error) return { state: 'unknown', detail: `unknown — the fleet-roster could not judge it: ${verdict.error}` };
  if (verdict.covered === false) return { state: 'unknown', detail: 'unknown — the fleet-roster read it as declaring no packs' };
  return { state: 'unknown', detail: 'unknown — the fleet-roster carries no freshness for it' };
}

// The states that are an update which did not land: one the nightly update has not
// caught up with, and two where nothing would ever converge the member.
export const STALE_FRESHNESS = Object.freeze(['behind', 'no-stamp', 'no-scheduler']);

// --- is anyone working here ------------------------------------------------------

// How long a member may go without GENUINE PROJECT WORK before the fleet view calls it
// sleepy. Short on purpose (owner, 2026-09-13): a repo crossing back and forth is fine,
// because this is a reading of the last fortnight rather than a verdict on the project.
export const SLEEPY_DAYS = 14;

// Sleepy, awake, or not classified. The input is `commitClasses` over the window
// commit listing, so the test is the claudinite-tasks pack's own substantive-commit
// one and a member reads quiet here exactly when its own preconditions read it quiet.
//
// THREE STATES, and the third is the point: a member whose commit listing was withheld
// under the page's budget is `unknown`, never `sleepy`. A repo nobody looked at and a
// repo nobody worked on are not the same fact, and only one of them is news.
export function sleepState(classes, { now } = {}) {
  if (!classes) return { state: 'unknown', days: SLEEPY_DAYS, lastMeaningfulAt: null };
  const since = now - SLEEPY_DAYS * DAY_MS;
  // The window the listing actually reached. A read that stopped at a full page covers
  // the recent end — which is the end this question is about — so a truncated listing
  // still answers it; one whose window starts INSIDE the fortnight does not.
  const from = classes.from == null ? null : new Date(classes.from).getTime();
  if (from != null && from > since) return { state: 'unknown', days: SLEEPY_DAYS, lastMeaningfulAt: classes.lastMeaningfulAt };
  const awake = classes.lastMeaningfulAt != null && classes.lastMeaningfulAt >= since;
  return {
    state: awake ? 'awake' : 'sleepy',
    days: SLEEPY_DAYS,
    lastMeaningfulAt: classes.lastMeaningfulAt,
    // The one exclusion the cheap test cannot apply, named where the state is decided
    // rather than in a footnote nobody reads beside the row it changes.
    caveat: 'commits are classified from the listing, so a commit that touched only .claudinite/ counts as work unless its message says otherwise',
  };
}

// --- one member -----------------------------------------------------------------

// Everything a fleet row shows about one member, plus the reasons it needs looking
// at. `read` is what the loader managed to fetch; a member it could not read arrives
// with `error` set and every other field absent.
export function summariseMember(read, { now, roster = null } = {}) {
  const {
    repo, error = null, declaration = null, items = null, runs = null, paths = null,
    prs = null, head = null, stars = null, defaultBranch = null, commits = undefined,
    usage = null, windowCommits = undefined, archived = false, private: isPrivate = null,
    ignored = false, member = null,
  } = read ?? {};

  if (error) {
    // A read the page DECLINED to make is not the same fact as one it could not make:
    // the repo is fine, the budget is not, and the row must not read as a fault of the
    // member. It is raised a rung above a permissions miss because it is the one the
    // viewer can act on — sign in, or wait for the reset.
    const withheld = error?.status === 'budget';
    return {
      repo,
      status: withheld ? 'withheld' : 'unreadable',
      level: withheld ? 'warning' : 'info',
      error,
      // Not being able to read a member is not the member being broken. It is almost
      // always a private repo outside this viewer's grant, so it is reported at the
      // bottom of the page rather than raised as an alarm.
      reasons: [{ level: withheld ? 'warning' : 'info', text: describeReadError(error) }],
    };
  }

  // OUT OF THE FLEET, and drawn anyway. An archived repo is frozen by GitHub and an
  // ignored one was put on the deployment's exclude list: no fleet operation touches
  // either and no figure counts them, so every Claudinite verdict a row could carry
  // would be about a repo nothing is maintaining. The page still draws them — greyed,
  // with their core GitHub facts and the one action that brings them back — because a
  // repo the reader cannot find at all is indistinguishable from one that is gone
  // (owner, 2026-09-13).
  //
  // Archived wins the status when a repo is both: it is the one a person cannot undo
  // from Claudinite's side, so it is the action the row offers.
  if (archived || ignored) {
    return {
      repo,
      status: archived ? 'archived' : 'ignored',
      level: 'ok',
      outOfFleet: true,
      archived,
      ignored,
      private: isPrivate,
      stars,
      lastCommit: head?.committedAt ? ms(head.committedAt) : null,
      // Whether it runs Claudinite at all is read from the identity pass either way,
      // and is the one Claudinite fact a greyed row carries: it says what coming back
      // into the fleet would resume, not how that repo is doing.
      adoptedOnce: Boolean(declaration),
      reasons: [{
        level: 'info',
        text: archived
          ? 'archived on GitHub — nothing runs here until it is unarchived'
          : 'ignored by this fleet — no sweep reads it and no figure counts it',
      }],
    };
  }

  if (!declaration) {
    return {
      repo,
      status: 'not-adopted',
      private: isPrivate,
      level: 'info',
      reasons: [{ level: 'info', text: 'does not run Claudinite' }],
    };
  }

  const work = (items ?? []).filter(isWorkItem);
  const open = work.filter((i) => i.state === 'open');
  const closed = work.filter((i) => i.state === 'closed');

  const declaredTasks = paths ? taskDeclarationPaths(paths, declaration).length : null;
  const periodFor = () => null;   // the fleet row needs no per-task cadence
  const described = open.map((i) => describeItem(i, now, { periodFor }));

  const byState = { [STATUS_BLOCKED]: 0, [STATUS_READY]: 0, [STATUS_RUNNING_EXECUTOR]: 0, [STATUS_RUNNING_AGENT]: 0, [PARKED]: 0, other: 0 };
  for (const d of described) {
    if (byState[d.state] === undefined) byState.other += 1;
    else byState[d.state] += 1;
  }

  const parked = described.filter((d) => d.state === PARKED);
  // The triage split: a failure park — or one an older
  // engine left unclassified — is a broken run to diagnose (whether it also holds
  // the task's lane is the declaration's word, which this summary does not read);
  // an action or decision park is a person's inbox; an approval park is a run that
  // SUCCEEDED and waits on a merge. Three different alarms, because a page that
  // rings identically for all four teaches the reader to ignore the ring.
  const holding = parked.filter((d) => d.blockingPark);
  const approvals = parked.filter((d) => !d.blockingPark && d.triage === STATUS_NEEDS_HUMAN_APPROVAL);
  // The person's inbox, split by remedy because the two cost differently (`PARK_MINUTES`):
  // an action is a thing to change outside the code, a decision is a call to make. A park
  // that is neither, and is not blocking, cannot occur — `parkOf` sends every kind it
  // cannot decode to `failure` — so `decisions` is the residue rather than a third lane.
  const actions = parked.filter((d) => !d.blockingPark && d.triage === STATUS_NEEDS_HUMAN_ACTION);
  const decisions = parked.filter((d) => !d.blockingPark && ![STATUS_NEEDS_HUMAN_APPROVAL, STATUS_NEEDS_HUMAN_ACTION].includes(d.triage));
  const inbox = [...actions, ...decisions];
  const warned = described.filter((d) => d.state !== PARKED && d.warnings.length);

  const outcomes = { done: 0, delivered: 0, obsolete: 0, none: 0 };
  for (const i of closed) outcomes[outcomeOf(i) ?? 'none'] += 1;

  const lastActivity = closed
    .map((i) => ms(i.closed_at) ?? ms(i.updated_at))
    .filter(Boolean)
    .sort((a, b) => b - a)[0] ?? null;

  const runSummary = summariseRuns(runs ?? [], now, usage);
  const ci = ciStatus(runs ?? [], defaultBranch);
  // A DORMANT member declared its own scheduler stopped, and the fleet takes that at
  // its word (owner, 2026-09-13): its freshness is not measured and its scheduler is not
  // judged. Nothing converges it and no fleet-wide operation touches it, so both
  // verdicts would be findings nobody owns — which is a different thing from the row
  // being quiet about the repo, since the row says dormant where they would have sat.
  const dormant = isDormant(member ?? declaration);
  const freshness = dormant
    ? { state: 'dormant', detail: 'dormant — its scheduler is stopped by declaration, so its freshness is not measured' }
    : freshnessOf(verdictFor(roster, repo), { rostered: Boolean(roster) });
  const classes = commitClasses(windowCommits);
  const sleep = sleepState(classes, { now });

  const n = (count, word) => `${count} ${word}${count > 1 ? 's' : ''}`;
  // Every reason carries a `kind`, because the row shows some of these twice
  // otherwise: parks have their own column with an estimate beside them, freshness
  // is a badge on the pack count, and CI is a dot. The RANKING still reads all of
  // them — a member is ordered by everything true of it — and only the rendering
  // drops what already has a cell of its own.
  const reasons = [];
  if (holding.length) {
    reasons.push({ kind: 'park', level: 'critical', text: `${n(holding.length, 'item')} parked broken — a run to diagnose` });
  }
  if (inbox.length) {
    reasons.push({ kind: 'park', level: 'serious', text: `${n(inbox.length, 'item')} parked for a person — an action or a decision` });
  }
  if (approvals.length) {
    reasons.push({ kind: 'park', level: 'warning', text: `${n(approvals.length, 'PR')} waiting for approval` });
  }
  if (runSummary.consecutiveFailures > 0 && !dormant) {
    reasons.push({
      kind: 'scheduler',
      level: runSummary.consecutiveFailures > 1 ? 'critical' : 'serious',
      text: `scheduler last run failed${runSummary.consecutiveFailures > 1 ? ` (${runSummary.consecutiveFailures} in a row)` : ''}`,
    });
  }
  if (warned.length) {
    reasons.push({ kind: 'park', level: 'serious', text: `${n(warned.length, 'item')} tripping a recovery rule` });
  }
  if (dormant) {
    // Not a fault, and not a silence either: the reader is told why the two Claudinite
    // verdicts are absent from this row.
    reasons.push({ kind: 'mount', level: 'info', text: 'dormant — its scheduler is stopped by declaration, so its freshness and scheduler are not measured' });
  } else if (freshness.state === 'behind') {
    // Routine: the member's own nightly update is what lands it, and the detail names
    // every gap the roster found.
    reasons.push({ kind: 'mount', level: 'info', text: freshness.detail || 'behind the published versions' });
  } else if (freshness.state === 'no-stamp' || freshness.state === 'no-scheduler') {
    // Nothing will ever converge this member, which no amount of waiting fixes.
    reasons.push({ kind: 'mount', level: 'warning', text: freshness.detail || `freshness: ${freshness.state}` });
  }
  // A repo that declares tasks and has never produced a work item is not idle — its
  // scheduler is not running. That is invisible in every per-repo number here, which
  // is exactly why the fleet view is the place it shows up.
  if (declaredTasks !== null && declaredTasks > 0 && work.length === 0 && !dormant) {
    reasons.push({ kind: 'scheduler', level: 'serious', text: `${declaredTasks} task${declaredTasks > 1 ? 's' : ''} declared, no work item ever created` });
  }
  if (ci.state === 'failing') {
    // The member's own CI, said in the member's own words: this is not the scheduler
    // failing, and the two must not read as the same alarm.
    reasons.push({ kind: 'ci', level: 'warning', text: 'the repo\'s own CI is failing on its default branch' });
  }
  if (declaration.packs?.length === 0) {
    reasons.push({ kind: 'packs', level: 'warning', text: 'declares no packs' });
  }

  const level = reasons.length ? reasons.map((r) => r.level).sort((a, b) => levelRank(a) - levelRank(b))[0] : 'ok';

  return {
    repo,
    status: 'adopted',
    level,
    reasons,
    // GitHub's own two facts about the repository, carried through untouched: the row
    // shows `private` because who can see a member is part of recognising it.
    private: isPrivate,
    dormant,
    // Whether anything MEANINGFUL landed here lately — `sleepState` above. Distinct
    // from dormant in both directions: a sleepy member is fully in the fleet and every
    // fleet-wide operation still reaches it.
    sleep,
    packs: (declaration.packs ?? []).map((p) => (typeof p === 'string' ? p : p?.id)).filter(Boolean),
    declaredTasks,
    open: { total: open.length, byState, urgent: described.filter((d) => d.urgent).length },
    parked: parked.length,
    parkedHolding: holding.length,
    parkedApprovals: approvals.length,
    parkedActions: actions.length,
    parkedDecisions: decisions.length,
    parkedInbox: inbox.length,
    warned: warned.length,
    closedSeen: closed.length,
    outcomes,
    lastActivity,
    runs: runSummary,
    ci,
    stars,
    // The one item of this member's worth prodding a reader about, ranked by the same
    // rules the repo page's work table ranks by. Computed here because this is the only
    // place the described items exist, and nothing above re-reads them.
    top: pickCandidate(described.map((d) => itemCandidate(repo, d))),
    // What this member's own packs report, straight through from the read: the
    // summary judges nothing about it. A contribution never feeds a reason, a level
    // or the ranking — attention is earned by what the engine can defend, and a pack
    // cannot rank across a fleet it does not know.
    contributions: read.contributions ?? null,
    live: read.live ?? null,
    lastCommit: head?.committedAt ? ms(head.committedAt) : null,
    // Null all the way through when the read did not happen, so the row draws "not
    // read" rather than an empty quarter that reads as a repo nobody touched.
    commits: commitDays(commits, { now, classes }),
    work: humanWork(items, prs, now),
    freshness,
    // Which engine this member runs, and what it says it holds — from its own member
    // file, or a Node member's stamp — for the row's hover.
    shape: member?.shape ?? null,
    engine: member?.engine ?? null,
    schedule: declaration.taskScheduler ?? null,
  };
}

// A read that failed says something different depending on how. 404 on a repo the
// roster names is almost always "not shared with this viewer", which is a permissions
// fact rather than a fault.
function describeReadError(error) {
  const status = error?.status;
  if (status === 'budget') return 'not read — this page is holding back your last requests; sign in or wait for the reset';
  if (status === 404) return 'not visible to you — the roster names it, your credential cannot see it';
  if (status === 403) return 'forbidden — rate limit, or your credential lacks access';
  if (status === 401) return 'your credential was rejected';
  return error?.message ? `unreadable — ${error.message}` : 'unreadable';
}

// The repo's OWN CI, which is a different question from the scheduler's health: one
// says whether this project builds, the other whether Claudinite is running here. They
// are read from the same list and must never be conflated — a green scheduler on a
// repo whose tests are red is not a healthy member.
//
// The default branch's most recent completed run is the answer; a `schedule` run is
// excluded because that is the queue's own scheduler run, reported separately.
export function ciStatus(runs, defaultBranch) {
  const mine = (runs ?? []).filter((r) => r.event !== 'schedule'
    && (!defaultBranch || !r.head_branch || r.head_branch === defaultBranch));
  const inFlight = mine.some((r) => r.status === 'in_progress' || r.status === 'queued');
  const last = mine.find((r) => r.status === 'completed');
  if (!last) return { state: inFlight ? 'running' : 'unknown', at: null, name: null };
  const state = last.conclusion === 'success' ? 'passing'
    : (last.conclusion === 'cancelled' || last.conclusion === 'skipped') ? 'unknown'
      : 'failing';
  return { state: inFlight ? 'running' : state, at: ms(last.created_at), name: last.name ?? null, url: last.html_url ?? null };
}

// What is waiting on a PERSON here, as opposed to on the queue: issues that are not
// Claudinite work items, and open pull requests. Both come out of the issue page the
// row already reads, so this costs nothing and stops at that window — an issue older
// than the window is not counted, and the panel says the window is a window.
export function humanWork(items, prs, now) {
  const open = (items ?? []).filter((i) => i.state === 'open' && !isWorkItem(i));
  const oldest = (list) => list.map((i) => ms(i.created_at)).filter(Boolean).sort((a, b) => a - b)[0] ?? null;
  const openPrs = (prs ?? []).filter((p) => !p.draft);
  return {
    issues: open.length,
    issuesOldest: oldest(open),
    prs: openPrs.length,
    drafts: (prs ?? []).length - openPrs.length,
    prsOldest: oldest(openPrs),
    now,
  };
}

// The scheduler's own health. Only the workflow that drives the queue matters here —
// a failing unrelated CI run is the repo's business, not the fleet scheduler's — so
// runs are filtered to scheduled ones before anything is counted.
//
// THE HEARTBEAT NEEDS A SECOND SOURCE. The runs listing is one page across every
// workflow the repo has — the scheduler, the executor and the member's own CI — so on
// a busy member that page covers a few hours, and a member whose last scheduler run is
// older than it is indistinguishable from one that has never run. "Never ran" is
// exactly the critical verdict, so the difference is not one to guess at.
//
// The fold's hour tier is that second source: three days of per-hour scheduler counts,
// appended past its own watermark from a listing it pages properly. It is coarser —
// an hour, not a timestamp — so it is used as a FLOOR: it can only push `lastAt`
// further back than the live page reached, and can only turn "never ran" into "ran".
// A member with no fold reads its heartbeat from the live page alone and says so.
export function summariseRuns(runs, now, usage = null) {
  const scheduled = runs.filter((r) => r.event === 'schedule');
  const completed = scheduled.filter((r) => r.status === 'completed');
  const inFlight = runs.filter((r) => r.status === 'in_progress' || r.status === 'queued').length;

  // Consecutive failures from the most recent backwards: one failure is noise, a run
  // of them is a member that has stopped working.
  let consecutiveFailures = 0;
  for (const r of completed) {
    if (r.conclusion === 'success') break;
    if (r.conclusion === 'cancelled' || r.conclusion === 'skipped') continue;
    consecutiveFailures += 1;
  }

  const liveLastAt = scheduled.map((r) => ms(r.created_at)).filter(Boolean).sort((a, b) => b - a)[0] ?? null;
  const foldedLastAt = lastFoldedScheduler(usage);
  const lastAt = liveLastAt !== null && (foldedLastAt === null || liveLastAt >= foldedLastAt)
    ? liveLastAt : foldedLastAt;

  return {
    scheduled: scheduled.length,
    inFlight,
    consecutiveFailures,
    lastAt,
    // Where that timestamp came from, and therefore how precise it is: a live run is
    // exact, a folded hour is the hour it started in. `null` when neither answered.
    lastAtSource: lastAt === null ? null : (lastAt === liveLastAt ? 'live' : 'folded'),
    // Whether the fold was read at all, so a member with none can say its heartbeat
    // rests on one page of runs rather than implying the two agreed.
    foldRead: Boolean(usage?.hours),
    // No scheduled run in EITHER source is not "healthy" — it is "we have never seen
    // this member's scheduler", which the row must be able to say. A member with no
    // fold can still only be reporting the page's own depth, which `foldRead` states.
    everRan: scheduled.length > 0 || foldedLastAt !== null,
  };
}

// The newest hour the fold recorded a scheduler run in, as that hour's start. A floor,
// never a claim about the minute: the tier counts runs per hour and says nothing about
// where inside one they landed.
export function lastFoldedScheduler(usage) {
  const hours = usage?.hours;
  if (!hours) return null;
  const newest = Object.entries(hours)
    .filter(([, row]) => typeof row?.scheduler === 'number' && row.scheduler > 0)
    .map(([hour]) => hour)
    .sort()
    .pop();
  return newest ? ms(`${newest}:00:00Z`) : null;
}

// --- the fleet ------------------------------------------------------------------

// Worst first, then by how much is waiting. Ties break on the repo name so the order
// is stable across loads — a table that reshuffles under you is unreadable.
export function rankMembers(summaries) {
  return [...summaries].sort((a, b) =>
    levelRank(a.level) - levelRank(b.level)
    || (b.parked ?? 0) - (a.parked ?? 0)
    || (b.warned ?? 0) - (a.warned ?? 0)
    || (b.open?.total ?? 0) - (a.open?.total ?? 0)
    || a.repo.localeCompare(b.repo));
}

// The headline numbers. Each is a count of MEMBERS needing something, not a count of
// things — "3 members need you" is actionable where "47 open items" is not.
export function rollUp(summaries) {
  const adopted = summaries.filter((s) => s.status === 'adopted');
  // The fleet's machinery figures are about the AWAKE members: a dormant one declared
  // its scheduler stopped, so counting it as failing, never-run or behind reports an
  // obedient repo as a fault (owner, 2026-09-13).
  const awake = adopted.filter((s) => !s.dormant);
  const outcomes = { done: 0, delivered: 0, obsolete: 0, none: 0 };
  for (const s of adopted) for (const [k, v] of Object.entries(s.outcomes ?? {})) outcomes[k] += v;

  return {
    members: summaries.length,
    adopted: adopted.length,
    notAdopted: summaries.filter((s) => s.status === 'not-adopted').length,
    unreadable: summaries.filter((s) => s.status === 'unreadable').length,
    withheld: summaries.filter((s) => s.status === 'withheld').length,
    needAttention: adopted.filter((s) => levelRank(s.level) <= levelRank('serious')).length,
    parkedMembers: adopted.filter((s) => s.parked > 0).length,
    parkedItems: adopted.reduce((n, s) => n + s.parked, 0),
    parkedHolding: adopted.reduce((n, s) => n + (s.parkedHolding ?? 0), 0),
    parkedInbox: adopted.reduce((n, s) => n + (s.parkedInbox ?? 0), 0),
    parkedActions: adopted.reduce((n, s) => n + (s.parkedActions ?? 0), 0),
    parkedDecisions: adopted.reduce((n, s) => n + (s.parkedDecisions ?? 0), 0),
    parkedApprovals: adopted.reduce((n, s) => n + (s.parkedApprovals ?? 0), 0),
    warnedItems: adopted.reduce((n, s) => n + (s.warned ?? 0), 0),
    warnedMembers: adopted.filter((s) => s.warned > 0).length,
    failingMembers: awake.filter((s) => s.runs?.consecutiveFailures > 0).length,
    neverRan: awake.filter((s) => s.runs && !s.runs.everRan).length,
    behindMembers: awake.filter((s) => STALE_FRESHNESS.includes(s.freshness?.state)).length,
    // The two states the grid marks, counted so a filter can say how many it will show.
    dormantMembers: adopted.filter((s) => s.dormant).length,
    sleepyMembers: adopted.filter((s) => s.sleep?.state === 'sleepy').length,
    privateMembers: adopted.filter((s) => s.private).length,
    openItems: adopted.reduce((n, s) => n + (s.open?.total ?? 0), 0),
    inFlight: adopted.reduce((n, s) => n + (s.runs?.inFlight ?? 0), 0),
    declaredTasks: adopted.reduce((n, s) => n + (s.declaredTasks ?? 0), 0),
    outcomes,
  };
}

// --- what a person is actually holding -------------------------------------------

// What one parked item costs a person, in minutes, by WHAT THE PARK ASKS FOR (owner,
// 2026-08-22). Per kind rather than one flat rate, because the four parks are disjoint
// by remedy and cost accordingly: diagnosing a break is not merging a PR, and averaging
// them into one number made the figure mean four things at once.
//
// Keyed by the count names below, which are the triage labels one layer up:
// `broken` is `task:needs-human-failure` PLUS any park whose kind cannot be decoded
// (`parkOf` falls back to `failure`), so an unclassified park is priced as the thing
// it is treated as everywhere else.
export const PARK_MINUTES = Object.freeze({
  broken: 15,
  actions: 10,
  decisions: 3,
});

// An approval is priced by SIZE, not by kind: a two-line docs merge and a nine-file
// refactor are the same label and not the same read.
export const APPROVAL_RATE = Object.freeze({ minutes: 1, lines: 200 });

// Minutes to approve a PR of `changedLines`, or the rate's own floor when the size is
// unknown — which is every approval today, and deliberately not hidden. The page never
// reads a PR's size: `projectPull` keeps no line counts and the PRs arrive on the
// issues listing, which carries none, so a size would cost one extra request per open
// approval against `budget.mjs`'s ladder. Unknown stays unknown rather than becoming a
// zero or a guessed average: the floor is the smallest thing the rate can honestly say,
// which is what makes the total a LOWER BOUND rather than an estimate that might be high.
export const approvalMinutes = (changedLines = null) =>
  (Number.isFinite(changedLines) && changedLines > 0
    ? Math.max(1, Math.ceil((changedLines / APPROVAL_RATE.lines) * APPROVAL_RATE.minutes))
    : APPROVAL_RATE.minutes);

// ONE parked item's own minutes, from the rates above — so a figure shown against a
// single item is one term of that sum rather than a second estimate. `park` is the
// item's own classification and nothing else: whether it blocks, and its triage label.
// Anything that is not a park has no figure at all here, by the same rule that keeps
// scheduler faults and recovery-rule trips out of the total.
export function parkMinutes(park) {
  if (!park) return null;
  // An undecodable park is priced as the thing it is treated as everywhere else.
  if (park.blocking || !park.triage) return PARK_MINUTES.broken;
  if (park.triage === STATUS_NEEDS_HUMAN_APPROVAL) return approvalMinutes();
  if (park.triage === STATUS_NEEDS_HUMAN_ACTION) return PARK_MINUTES.actions;
  return PARK_MINUTES.decisions;
}

// The one caveat a single item's figure carries, and only the approvals carry it: the
// page never reads a PR's size, so an approval is charged the rate's floor and its
// figure can only be low. The same sentence `estimateNote` appends to a total.
export const parkMinutesNote = (park) =>
  (parkMinutes(park) != null && !park.blocking && park.triage === STATUS_NEEDS_HUMAN_APPROVAL
    ? 'PR size unread, so a lower bound'
    : null);

// The vocabulary both the per-member row and the fleet rollup speak, so one function
// itemises both. Each field is a count of THINGS waiting; the two callers differ only
// in whether their things are one member's or the whole fleet's.
const attentionCounts = ({
  broken = 0, decisions = 0, actions = 0, approvals = 0, tripping = 0,
  schedulersFailing = 0, schedulersNeverRan = 0,
} = {}) => ({ broken, decisions, actions, approvals, tripping, schedulersFailing, schedulersNeverRan });

// One member's attention, in the same shape the rollup uses.
export const memberAttention = (s) => attentionCounts({
  broken: s.parkedHolding ?? 0,
  decisions: s.parkedDecisions ?? 0,
  actions: s.parkedActions ?? 0,
  approvals: s.parkedApprovals ?? 0,
  tripping: s.warned ?? 0,
});

// The fleet's. Schedulers are counted in MEMBERS here rather than in items, because a
// scheduler is a member-shaped thing — there is one per repo.
export const fleetAttention = (roll) => attentionCounts({
  broken: roll.parkedHolding,
  decisions: roll.parkedDecisions,
  actions: roll.parkedActions,
  approvals: roll.parkedApprovals,
  tripping: roll.warnedItems,
  schedulersFailing: roll.failingMembers,
  schedulersNeverRan: roll.neverRan,
});

// Minutes of a person's time the parked work represents. ONLY the parks: every minute
// in here is attributable to an item wearing a `task:status:needs-human-<kind>` label,
// which is a thing a person can see on the issue and go and do.
//
// Two things a reader might expect are deliberately outside it, for the same reason.
// Scheduler faults are not a queue of work to get through. And a recovery-rule trip is
// not waiting on a person at all: it wears no label, it is derived from an item's state
// and age against the engine's own leashes, and what clears it is the janitor or the
// next scheduler run. Both are still REPORTED, in `attentionBreakdown` beside the
// figure — they are just not minutes of anyone's time.
export function estimateMinutes(counts) {
  const c = attentionCounts(counts);
  return c.broken * PARK_MINUTES.broken
    + c.actions * PARK_MINUTES.actions
    + c.decisions * PARK_MINUTES.decisions
    + c.approvals * approvalMinutes();
}

// The sentence published beside the figure, so a reader can check the arithmetic
// instead of trusting it — and so the one thing the number cannot know says so itself.
// One definition, because both surfaces show the same total and a note that differed
// between them would be two claims about one calculation.
export const estimateNote = (counts) => {
  const c = attentionCounts(counts);
  const parts = [];
  if (c.broken) parts.push(`${PARK_MINUTES.broken}×${c.broken} broken`);
  if (c.actions) parts.push(`${PARK_MINUTES.actions}×${c.actions} action`);
  if (c.decisions) parts.push(`${PARK_MINUTES.decisions}×${c.decisions} decision`);
  if (c.approvals) parts.push(`${APPROVAL_RATE.minutes}×${c.approvals} approval`);
  if (!parts.length) return 'nothing parked';
  // The caveat rides with the approvals, because it is only about them: with no PR
  // size the rate can charge nothing above its floor, so the total can only be low.
  return parts.join(' + ') + (c.approvals ? ' · PR sizes unread, so a lower bound' : '');
};

// WHAT the human attention is, not how much of it there is. "3 members need you"
// is the length of this morning's list; it does not say whether that is three merges
// to approve or three broken lanes, and those are not the same morning.
//
// The split already exists one level down — `summariseMember` separates a failure
// park from an inbox park from an approval park precisely because a page that rings
// identically for all of them teaches its reader to ignore the ring — and the rollup
// used to throw it away and re-merge them into one word.
//
// Ordered worst first, and a kind nobody is waiting on is absent rather than zero.
//
// A row is read two ways. Where there is room for prose — the lead card, the fleet
// tiles — `text` is printed as it stands. Where there is not, the member row's cell in
// a ten-column grid, `ui.mjs`'s `attentionMark` draws the row instead, and it needs the
// pieces rather than the sentence: `kind` to address a row without matching its words,
// `count` and `short` to write a line narrow enough to fit. So the sentence is composed
// here from the same two pieces the mark uses, and the two can never disagree.
export function attentionBreakdown(counts) {
  const c = attentionCounts(counts);
  const row = (kind, level, count, [one, many], [shortOne, shortMany], say) => count && {
    kind,
    level,
    count,
    short: count === 1 ? shortOne : shortMany,
    text: `${count} ${count === 1 ? one : many} ${say}`.trim(),
  };
  return [
    row('broken', 'critical', c.broken, ['task', 'tasks'], ['broken', 'broken'], 'broken'),
    row('schedulersFailing', 'critical', c.schedulersFailing, ['scheduler', 'schedulers'], ['scheduler failing', 'schedulers failing'], 'failing'),
    row('decisions', 'serious', c.decisions, ['item', 'items'], ['decision', 'decisions'], 'needing a decision'),
    row('actions', 'serious', c.actions, ['item', 'items'], ['action', 'actions'], 'needing something changed outside the code'),
    row('tripping', 'serious', c.tripping, ['item', 'items'], ['tripping', 'tripping'], 'tripping a recovery rule'),
    row('approvals', 'warning', c.approvals, ['item', 'items'], ['approval', 'approvals'], 'needing approval'),
    row('schedulersNeverRan', 'serious', c.schedulersNeverRan, ['scheduler', 'schedulers'], ['never ran', 'never ran'], 'never ran'),
  ].filter(Boolean);
}

// Which packs the fleet uses, and how widely. Answers the question the canon actually
// asks of its fleet — "who would a change to this pack reach" — which no single
// member's page can.
export function packSpread(summaries) {
  const counts = new Map();
  for (const s of summaries) {
    for (const p of s.packs ?? []) counts.set(p, (counts.get(p) ?? 0) + 1);
  }
  return [...counts.entries()]
    .map(([pack, members]) => ({ pack, members }))
    .sort((a, b) => b.members - a.members || a.pack.localeCompare(b.pack));
}

// Which tasks are running across the fleet, from the work items themselves. A task
// declared by a shared pack runs in many members, and its fleet-wide behaviour —
// especially a task that is parked in several at once — is a canon problem rather
// than any one member's.
export function taskSpread(reads, now) {
  const byTask = new Map();
  for (const read of reads) {
    for (const item of (read.items ?? []).filter(isWorkItem)) {
      const parsed = parseWorkItemTitle(item.title);
      if (!parsed) continue;
      const key = `${parsed.pack}/${parsed.task}`;
      if (!byTask.has(key)) byTask.set(key, { key, pack: parsed.pack, task: parsed.task, members: new Set(), open: 0, parked: 0, done: 0, failed: 0 });
      const row = byTask.get(key);
      row.members.add(read.repo);
      if (item.state === 'open') {
        row.open += 1;
        if (isParked(item)) row.parked += 1;
      } else {
        const outcome = outcomeOf(item);
        if (outcome === 'done' || outcome === 'delivered') row.done += 1;
        else if (outcome !== 'obsolete') row.failed += 1;
      }
    }
  }
  return [...byTask.values()]
    .map((r) => ({ ...r, members: r.members.size }))
    .sort((a, b) => b.parked - a.parked || b.members - a.members || a.key.localeCompare(b.key));
}

export { URGENT };
