// The one thing to do next. Pure: no clock of its own, no I/O, no DOM.
//
// Every other panel on both pages REPORTS. A reader who opens the page already knowing
// what they came for is served by that; a reader who opens it because it is morning is
// not, because a wall of accurate panels asks them to do the ranking. This module does
// the ranking and names ONE piece of work.
//
// It invents no judgement of its own. A candidate is the worst thing some other module
// already decided is wrong — an item's `troubles` (the queue's real recovery rules) or a
// member summary's `reasons` — so the block at the top of the page can never disagree
// with the row further down that says the same thing.
//
// TWO SHAPES, AND THE ITEM ONE WINS TIES. An item candidate names an issue: it is a
// link a reader can act through. A repo candidate — a failing scheduler, a mount behind
// canon — has no issue behind it and is the fallback, because "go look at this repo" is
// a weaker prod than "open this issue".

import { troubles } from './work.mjs';
import { WORK_PREFIX, PARKED } from './model.mjs';

const LEVELS = ['critical', 'serious', 'warning', 'info', 'ok'];
const levelRank = (l) => {
  const i = LEVELS.indexOf(l);
  return i === -1 ? LEVELS.length : i;
};

// Only a level that colours a repo is worth prodding about. `info` covers a mount one
// pack behind and a repo that does not run Claudinite — true, reported elsewhere, and
// not what anyone should open their morning with.
const PRODDABLE = new Set(['critical', 'serious', 'warning']);

export const issueUrl = (repo, number) => `https://github.com/${repo}/issues/${number}`;

// A filed item's title is its task key in the queue's own grammar, which the candidate
// already carries as `key` — so only a title a PERSON wrote survives here. An adopted
// marked issue keeps the words its author used, and those are the point of showing one.
const humanTitle = (title) => (title && !title.startsWith(WORK_PREFIX) ? title : null);

// What is wrong with one open work item, as a candidate — or null when nothing is.
// `item` is `describeItem`'s output. `row` is the work table's row where the caller has
// one, because a held lane is a fact about the TASK that the item cannot state.
export function itemCandidate(repo, item, row = null) {
  const t = troubles({ ...(row ?? {}), current: item });
  if (!t.length || !PRODDABLE.has(t[0].level)) return null;
  return {
    kind: 'item',
    repo,
    level: t[0].level,
    why: t[0].text,
    more: t.slice(1).map((w) => w.text),
    number: item.number,
    title: humanTitle(item.title),
    key: item.key ?? null,
    idleMs: item.idleMs ?? null,
    // The item's own park classification, and no price: what a park COSTS is the
    // attention estimate's arithmetic (`fleet.mjs`), and this module holds no rates.
    // Null for anything that is not a park, which is what that estimate leaves out.
    park: item.state === PARKED
      ? { blocking: Boolean(item.blockingPark), triage: item.triage ?? null }
      : null,
    url: issueUrl(repo, item.number),
  };
}

// The worst fault a repo carries that no item of its own can state — its scheduler
// failing, its mount behind, its own CI red. `park` reasons are excluded because they
// are counts OF the items, and an item candidate says the same thing with a link.
export function reasonCandidate(repo, reasons) {
  const worst = (reasons ?? [])
    .filter((r) => r.kind !== 'park' && PRODDABLE.has(r.level))
    .sort((a, b) => levelRank(a.level) - levelRank(b.level))[0];
  if (!worst) return null;
  return {
    kind: 'repo',
    repo,
    level: worst.level,
    why: worst.text,
    more: [],
    number: null,
    title: null,
    key: null,
    idleMs: null,
    // A broken scheduler is not a park and not a queue of work to get through; the
    // fleet's own estimate leaves it out and so does this.
    park: null,
    url: `https://github.com/${repo}`,
  };
}

// Worst first, and among equals the one that has been wrong longest — a park nobody has
// touched in a week is a better prod than one filed an hour ago, and both are better
// than a repo-level fault, which carries no link to act through.
export function rankCandidates(candidates) {
  return (candidates ?? []).filter(Boolean).slice().sort((a, b) =>
    levelRank(a.level) - levelRank(b.level)
    || (a.kind === b.kind ? 0 : (a.kind === 'item' ? -1 : 1))
    || (b.idleMs ?? -1) - (a.idleMs ?? -1)
    || a.repo.localeCompare(b.repo)
    || (a.number ?? 0) - (b.number ?? 0));
}

export const pickCandidate = (candidates) => rankCandidates(candidates)[0] ?? null;

// Every candidate a fleet offers: each member's own worst item, plus the faults its
// items cannot state. Members are `summariseMember`'s output; a member that could not
// be read contributes nothing, because a repo this viewer cannot see is not work they
// can pick up.
export function fleetCandidates(summaries) {
  return rankCandidates((summaries ?? []).flatMap((s) => (s?.status === 'adopted'
    ? [s.top ?? null, reasonCandidate(s.repo, s.reasons)]
    : [])));
}

// The same question for one repo, from the work table's own rows — so the block at the
// top of the repo page and the first row of its `stuck` view are the same verdict.
export function repoCandidates(repo, rows) {
  return rankCandidates((rows ?? []).map((r) => (r.current
    ? itemCandidate(repo, { ...r.current, key: r.current.key ?? r.key }, r)
    : null)));
}
