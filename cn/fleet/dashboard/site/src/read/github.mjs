// The GitHub read client, and the only I/O in the dashboard. Every call is a READ;
// nothing here writes to a repo.
//
// ACCESS CONTROL IS THE VIEWER'S CREDENTIAL and there is nothing else — no backend,
// no shared token, no service account. Whichever provider `auth.mjs` used, the page
// calls api.github.com as that person, so a repo they cannot read stays unreadable
// and this page grants no access anyone did not already have.
//
// Caching is not an optimisation here, it is what makes a fleet view viable: see
// `cache.mjs` for why the three strategies differ. The rate-limit accounting below
// distinguishes calls that SPENT budget from 304s that did not, because "how much
// did that cost" is the question a viewer asks when a fleet sweep feels slow.

import {
  immutable, validated, ageing, rateState, projectIssue, projectPull, projectRun,
  withinMergedWindow, DAY_MS,
} from './cache.mjs';

const API = 'https://api.github.com';

export const rate = {
  remaining: null, limit: null, reset: null,
  spent: 0, revalidated: 0, served: 0, withheld: 0,
  // Set when GitHub says the budget is gone. Every later call in the same load reads
  // it and skips the request: twelve members' worth of calls that can only fail is
  // twelve more chances to trip the SECONDARY limit, which is measured in requests
  // made rather than in budget left.
  exhaustedUntil: null,
};

export const resetCounters = () => { rate.spent = 0; rate.revalidated = 0; rate.served = 0; rate.withheld = 0; };

// Told whenever GitHub restates the budget, so a display of it can be the LIVE figure
// rather than the one the load was planned on. A fleet sweep spends over the seconds
// it runs, and a readout taken before those reads answers "how many calls have I
// left" as of before everything that would change the answer.
let onRate = null;
export const onRateChange = (fn) => { onRate = fn; };

// The budget policy this page is reading under. `budget.mjs` decides it; everything
// here only obeys it. The default is the old unconditional behaviour, so a caller
// that never sets one behaves exactly as before.
export let policy = { mode: 'live', minAge: 0, historyTtl: DAY_MS, spendCeiling: Infinity, extras: true };
export const setPolicy = (p) => { policy = { ...policy, ...p }; };

// Refusing to spend is not an error in the data — it is this page choosing to keep
// the viewer's remaining requests. Rendered as a row that says so, never as a fault
// of the repo being read.
export class RateBudgetError extends Error {
  constructor(path) {
    super('not read — saving the remaining rate limit');
    this.status = 'budget';
    this.path = path;
  }
}

const budgetLeft = () => rate.spent < policy.spendCeiling;

const frozen = (now = Date.now()) => Number.isFinite(rate.exhaustedUntil) && rate.exhaustedUntil > now;

// Carry the last known budget across page loads. Without it every fresh tab starts
// blind, reads live, and only learns it was nearly out AFTER spending a sweep's worth
// of requests finding out.
export function restoreRate() {
  const s = rateState.get();
  if (!s) return null;
  rate.remaining = s.remaining ?? null;
  rate.limit = s.limit ?? null;
  rate.reset = s.reset ?? null;
  rate.exhaustedUntil = s.exhaustedUntil ?? null;
  return s;
}

export class GitHubError extends Error {
  constructor(status, path, detail) {
    super(`GitHub ${status} on ${path}${detail ? ` — ${detail}` : ''}`);
    this.status = status;
    this.path = path;
  }
}

// An ABSENT rate-limit header means "this response did not say", which is not zero.
// `Number(null)` is 0 and passes `isFinite`, so reading the header straight through
// makes any response without one report a budget of zero — the last such response
// wins and the page claims the viewer is rate-limited when they are not.
const headerNumber = (res, name) => {
  const raw = res.headers.get(name);
  if (raw === null || raw === '') return null;
  const n = Number(raw);
  return Number.isFinite(n) ? n : null;
};

function noteRate(res) {
  const r = headerNumber(res, 'x-ratelimit-remaining');
  if (r !== null) rate.remaining = r;
  const l = headerNumber(res, 'x-ratelimit-limit');
  if (l !== null) rate.limit = l;
  const t = headerNumber(res, 'x-ratelimit-reset');
  if (t !== null) rate.reset = t;

  // A 403/429 with nothing left is the primary limit, and it stays spent until the
  // window rolls. `retry-after` is honoured when present because the secondary limit
  // sends that instead of a zeroed remaining.
  if ((res.status === 403 || res.status === 429)) {
    const retryAfter = headerNumber(res, 'retry-after');
    if (retryAfter !== null) rate.exhaustedUntil = Date.now() + retryAfter * 1000;
    else if (rate.remaining === 0 && Number.isFinite(rate.reset)) rate.exhaustedUntil = rate.reset * 1000;
  } else if (rate.remaining > 0) {
    rate.exhaustedUntil = null;
  }
  rateState.set({ remaining: rate.remaining, limit: rate.limit, reset: rate.reset, exhaustedUntil: rate.exhaustedUntil });
  // Never let a display's failure break a read: this is a readout, and the sweep
  // behind it matters more than the number beside it.
  try { onRate?.(rate); } catch { /* a broken readout is not a broken load */ }
}

// The one call GitHub does not charge for: `/rate_limit` reports the budget without
// spending any of it. Asked once at boot so the FIRST load of a tab is planned on the
// real number rather than on a guess — which is the whole difference between a fleet
// sweep that fits in an anonymous 60 and one that dies a third of the way through.
export async function preflightRate(token) {
  try {
    const res = await raw('/rate_limit', { token });
    noteRate(res);
    if (!res.ok) return null;
    const body = await res.json();
    const core = body?.resources?.core ?? body?.rate;
    if (core && Number.isFinite(core.remaining)) {
      rate.remaining = core.remaining;
      rate.limit = core.limit ?? rate.limit;
      rate.reset = core.reset ?? rate.reset;
      if (core.remaining > 0) rate.exhaustedUntil = null;
      rateState.set({ remaining: rate.remaining, limit: rate.limit, reset: rate.reset, exhaustedUntil: rate.exhaustedUntil });
    }
    return { remaining: rate.remaining, limit: rate.limit, reset: rate.reset };
  } catch {
    return null;   // an unreachable preflight plans nothing and breaks nothing
  }
}

async function raw(path, { token, accept = 'application/vnd.github+json', etag = null } = {}) {
  const res = await fetch(`${API}${path}`, {
    headers: {
      Accept: accept,
      'X-GitHub-Api-Version': '2022-11-28',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(etag ? { 'If-None-Match': etag } : {}),
    },
  });
  noteRate(res);
  return res;
}

async function fail(res, path) {
  let detail = '';
  try { detail = (await res.json())?.message ?? ''; } catch { /* not json */ }
  return new GitHubError(res.status, path, detail);
}

// A call whose result is live: cached with its ETag and revalidated every time. A
// 304 costs no rate-limit budget, so this is free freshness — never stale data.
async function conditional(path, token, { transform = (x) => x } = {}) {
  const hit = validated.get(path);

  // Under pressure a young entry is served with NO request at all. An ETag
  // revalidation costs no primary budget, but it is still a request against the
  // secondary limit and still a round trip — and when the budget is nearly gone,
  // "the page reloads for free" is worth more than a few minutes of freshness.
  if (hit && policy.minAge > 0 && Date.now() - (hit.at ?? 0) < policy.minAge) {
    rate.served += 1;
    return hit.data;
  }
  if (frozen() || !budgetLeft()) {
    if (hit) { rate.withheld += 1; return hit.data; }
    throw new RateBudgetError(path);
  }

  const res = await raw(path, { token, etag: hit?.etag });
  if (res.status === 304 && hit) {
    rate.revalidated += 1;
    validated.touch(path);
    return hit.data;
  }
  rate.spent += 1;
  if (!res.ok) throw await fail(res, path);
  const data = transform(await res.json());
  validated.set(path, res.headers.get('etag'), data);
  return data;
}

export const getRepo = (repo, token) =>
  conditional(`/repos/${repo}`, token, {
    // `stargazers_count` rides along in a response the page already makes: what KIND
    // of repo this is belongs in the fleet row's Status group, and asking separately
    // for it would be a per-member call the budget does not have.
    transform: (r) => ({
      default_branch: r.default_branch, private: r.private, full_name: r.full_name,
      stars: r.stargazers_count ?? null, archived: Boolean(r.archived),
    }),
  });

// The head commit of the default branch — the cache key everything immutable hangs
// off. One cheap call buys the right to skip every content read when nothing landed.
//
// Its DATE is kept alongside the sha because the same response already carries it:
// "when did this repo last move" is a fleet question that would otherwise cost a
// per-member call, and a read already made is the only kind #995's budget allows.
export const getHead = (repo, branch, token) =>
  conditional(`/repos/${repo}/commits/${encodeURIComponent(branch)}`, token, {
    transform: (c) => ({ sha: c.sha, committedAt: c.commit?.committer?.date ?? c.commit?.author?.date ?? null }),
  });

export const getHeadSha = async (repo, branch, token) => (await getHead(repo, branch, token)).sha;

// A file's text AT A SHA, therefore immutable and cached forever. This is where the
// bulk of a repeat load's savings come from: a roster of task declarations is one
// call each on a cold cache and zero on a warm one.
//
// 404 is an ANSWER, not a failure — an unadopted repo has no declaration file — and
// it is cached as such, so a fleet sweep does not re-ask every member every time.
export async function getTextAtSha(repo, sha, path, token) {
  const hit = immutable.get(repo, sha, path);
  if (hit !== undefined) { rate.served += 1; return hit; }

  if (frozen() || !budgetLeft()) throw new RateBudgetError(path);
  const res = await raw(`/repos/${repo}/contents/${encodeURI(path)}?ref=${encodeURIComponent(sha)}`,
    { token, accept: 'application/vnd.github.raw' });
  rate.spent += 1;
  if (res.status === 404) { immutable.set(repo, sha, path, null); return null; }
  if (!res.ok) throw await fail(res, path);
  const text = await res.text();
  immutable.set(repo, sha, path, text);
  return text;
}

// The tree at a sha — immutable for the same reason, and the single most expensive
// response the page fetches on a large repo.
export async function listTreeAtSha(repo, sha, token) {
  const hit = immutable.get(repo, sha, '::tree');
  if (hit !== undefined && hit !== null) { rate.served += 1; return hit; }

  if (frozen() || !budgetLeft()) throw new RateBudgetError(`/repos/${repo}/git/trees`);
  const res = await raw(`/repos/${repo}/git/trees/${encodeURIComponent(sha)}?recursive=1`, { token });
  rate.spent += 1;
  if (!res.ok) throw await fail(res, `/repos/${repo}/git/trees`);
  const t = await res.json();
  const out = {
    paths: (t.tree ?? []).filter((n) => n.type === 'blob').map((n) => n.path),
    // The tree API caps at 100k entries; a truncated listing means the roster may be
    // missing tasks, which the page states rather than quietly implying completeness.
    truncated: Boolean(t.truncated),
  };
  immutable.set(repo, sha, '::tree', out);
  return out;
}

// PRs come back from the issues endpoint and are filtered out, so the FILTERED
// length says nothing about whether more pages exist — a page of pure PRs would
// read as the end of history. Pagination is decided by the raw page length, which
// is therefore carried alongside the projection, cached and all.
//
// The PRs are not thrown away either, and there are now two reasons to keep one: an
// OPEN pull request is work waiting on a person, which is what the fleet row's Work
// group reports, and a MERGED one inside the window is the lead-time series for the
// days the fold has not reached yet. Both arrive in a response the page was making
// anyway, and everything the page reads out of a PR body — the issue it closes — is
// parsed on the way in, so the body itself is still never stored.
//
// The window is applied at PROJECTION time, which bounds what is STORED rather than
// what is shown: a history page is cached for a day, so a row can age a day past the
// bound before it is dropped, and every reader windows the series for itself anyway.
const projectPage = (body, at = Date.now()) => {
  const list = Array.isArray(body) ? body : [];
  const prs = [];
  for (const entry of list) {
    if (!entry.pull_request) continue;
    const pull = projectPull(entry);
    const open = String(entry.state ?? '').toLowerCase() === 'open';
    if (open || withinMergedWindow(pull.merged_at, at)) prs.push(pull);
  }
  return {
    items: list.filter((i) => !i.pull_request).map(projectIssue),
    prs,
    raw: list.length,
  };
};

// What is open RIGHT NOW, asked as its own question. Conditional on every page, so a
// fleet load where nothing moved spends nothing and a load where something did is
// correct — which is the trade the history pages cannot make.
//
// `complete` is what licenses a caller to read ABSENCE as closed: a listing cut short
// by depth or by the budget says nothing about the items it never reached.
async function listOpen(repo, token, { pages, perPage }) {
  const items = [];
  const prs = [];
  let complete = false;
  for (let page = 1; page <= pages; page += 1) {
    const path = `/repos/${repo}/issues?state=open&sort=created&direction=desc&per_page=${perPage}&page=${page}`;
    const batch = await conditional(path, token, { transform: projectPage });
    items.push(...batch.items);
    prs.push(...(batch.prs ?? []));
    if (batch.raw < perPage) { complete = true; break; }
  }
  return { items, prs, complete };
}

// Issues, newest first. The LIST api and never the search index — search is
// eventually consistent, which is how a just-created item goes missing from a view
// whose whole job is showing what is happening right now.
//
// Two reads, because one cannot answer both questions:
//   - the OPEN set is asked for by state and revalidated on every page, always live;
//   - HISTORY is `state=all` newest-created-first, page 1 conditional and pages 2..n
//     under a TTL, default 24h.
//
// The history pages are settled only as a record of what EXISTED. Under `sort=created`
// an item created long ago sits on page 3 whatever it is doing today, so its cached
// copy keeps the state, labels and dates it wore when that page was fetched — which is
// how a closed item went on being reported as parked work. The open listing is
// therefore the authority on state: it replaces the history copy of anything it names,
// and a cached-open item a COMPLETE open listing does not name has closed since.
export async function listIssues(repo, token, { pages = 5, perPage = 100, historyTtl = null } = {}) {
  const ttl = historyTtl ?? policy.historyTtl ?? DAY_MS;
  const out = [];
  const prs = [];
  let scanned = 0;
  let complete = false;
  let fromCache = 0;

  const project = projectPage;

  // Correctness before depth: the open listing is read FIRST and is what survives a
  // squeeze, because history one page short costs a member's older record while a
  // missing open set costs a wrong answer about what is happening now.
  let live = null;
  try {
    live = await listOpen(repo, token, { pages, perPage });
  } catch (e) {
    if (!(e instanceof RateBudgetError)) throw e;
    rate.withheld += 1;
  }

  for (let page = 1; page <= pages; page += 1) {
    const path = `/repos/${repo}/issues?state=all&sort=created&direction=desc&per_page=${perPage}&page=${page}`;
    let batch;

    if (page === 1) {
      // Where a newly-created item lands, so it is never served from a TTL — it is
      // revalidated on every load, which is free when nothing changed. Withheld only
      // when the live set above got through: history alone answers nothing this page
      // asks, so a member with neither read is a member that could not be read.
      try {
        batch = await conditional(path, token, { transform: project });
      } catch (e) {
        if (!(e instanceof RateBudgetError) || !live) throw e;
        rate.withheld += 1;
        break;
      }
    } else {
      const ck = `issues:${repo}:p${page}`;
      const hit = ageing.get(ck, ttl);
      if (hit !== undefined) {
        batch = hit;
        fromCache += 1;
        rate.served += 1;
      } else if (frozen() || !budgetLeft()) {
        // History is the cheapest thing to go without: the open listing already holds
        // the live queue, so a withheld page 2 costs depth, never correctness.
        rate.withheld += 1;
        break;
      } else {
        const res = await raw(path, { token });
        rate.spent += 1;
        if (!res.ok) throw await fail(res, path);
        batch = project(await res.json());
        // Settled history: a full page is a closed window and a short one is the end
        // of it. Both are safe to keep for the TTL.
        ageing.set(ck, batch);
      }
    }

    out.push(...batch.items);
    prs.push(...(batch.prs ?? []));
    scanned += batch.raw;
    if (batch.raw < perPage) { complete = true; break; }
  }

  // A withheld open listing leaves the history pages saying what they last saw, which
  // is the old behaviour and the best this read can do without spending.
  if (!live) return { issues: out, prs, scanned, complete, fromCache };

  const liveOpen = new Map(live.items.map((i) => [i.number, i]));
  const seen = new Set();
  const issues = out.map((i) => {
    seen.add(i.number);
    const fresh = liveOpen.get(i.number);
    if (fresh) return fresh;
    // Closed since this page was cached. WHEN it closed is not something either read
    // can say, and a fabricated date would be read as fact — so the state moves and
    // `closed_at` stays whatever it was, which for an item cached open is unknown.
    if (i.state === 'open' && live.complete) return { ...i, state: 'closed' };
    return i;
  });
  // An open item older than the history actually scanned exists only in the live
  // listing. Appended rather than merged by date: it is older than everything the
  // history pages hold, which is why it was missing.
  issues.push(...live.items.filter((i) => !seen.has(i.number)));

  // The live listing is the whole OPEN set to the depth it reached; only where it fell
  // short does the history's own (staler) reading of open PRs still add depth.
  const open = live.complete
    ? live.prs
    : [...live.prs, ...prs.filter((p) => !live.prs.some((l) => l.number === p.number))];
  // The MERGED ones can only come from the history listing — the open listing is asked
  // by state and cannot answer them — so they are added whatever the open set reached.
  // A PR the history still has cached as open but which has since merged carries a
  // merge date, so it lands here rather than being reported as waiting on someone.
  const merged = prs.filter((p) => p.merged_at && !open.some((o) => o.number === p.number));

  return {
    issues,
    prs: [...open, ...merged],
    scanned,
    complete,
    fromCache,
  };
}

// How many runs a caller asks for. ONE number for both views, because the cache is
// keyed by URL: asking for 30 here and 40 there makes two entries for one question,
// so opening a member from the fleet page re-fetched a list it already held and kept
// a second near-identical copy in a ~5MB quota. Whatever depth the deeper view needs
// is what the shallower one asks for too.
export const RUNS_PER_PAGE = 40;

export const listRuns = (repo, token, perPage = RUNS_PER_PAGE) =>
  conditional(`/repos/${repo}/actions/runs?per_page=${perPage}`, token, {
    transform: (r) => (r.workflow_runs ?? []).map(projectRun),
  });

// A year of daily commit counts in one response — 52 weeks, each with its 7 days.
// The fleet row's commit graph is the only reader, and reading it any other way costs
// a pagination loop over `/commits` per member.
//
// Cached on a TTL rather than revalidated, because it is the one read here that is
// DECORATION: it says how busy a repo has been, and a few hours old is the same
// answer. Under budget pressure it is not read at all — `withheld` is a state the
// graph renders as "not read", never as a quiet repo.
//
// GitHub computes these statistics lazily and answers `202` with an empty body while
// it does. That is "ask again later", not "no commits": it is returned as null and
// NOT cached, so the next load asks again instead of showing an empty year.
export const COMMIT_ACTIVITY_TTL = 6 * 3600e3;

export async function commitActivity(repo, token) {
  const ck = `commit-activity:${repo}`;
  const hit = ageing.get(ck, COMMIT_ACTIVITY_TTL);
  if (hit !== undefined) { rate.served += 1; return hit; }

  if (frozen() || !budgetLeft() || !policy.extras) { rate.withheld += 1; return undefined; }

  const path = `/repos/${repo}/stats/commit_activity`;
  const res = await raw(path, { token });
  rate.spent += 1;
  if (res.status === 202) return null;               // still being computed — ask again next load
  if (res.status === 204) { ageing.set(ck, []); return []; }   // a repo with no commits at all
  if (!res.ok) throw await fail(res, path);
  const weeks = (await res.json()) ?? [];
  const out = Array.isArray(weeks)
    ? weeks.map((w) => ({ week: w.week, days: Array.isArray(w.days) ? w.days : [] }))
    : [];
  ageing.set(ck, out);
  return out;
}

// The repo's latest release — the one platform fact a release pack wants that no file
// in its tree carries and no task should have to mirror.
//
// TTL-cached rather than ETag-revalidated, and the reason is the NEGATIVE: `conditional`
// throws on a 404 and caches nothing, so a fleet of repos that have never released
// would spend one request each, every load, forever. A repo with no releases is an
// ANSWER (`null`) and is cached as one, exactly as a missing declaration file is.
//
// DECORATION by the budget's reckoning, as the commit graphs are: it is the only
// per-member REQUEST pack contributions add, so it withholds itself before anything
// the queue depends on. A withheld read answers `undefined` — not read — which is
// never the same as the `null` above.
export const LATEST_RELEASE_TTL = 6 * 3600e3;

export async function latestRelease(repo, token) {
  const ck = `latest-release:${repo}`;
  const hit = ageing.get(ck, LATEST_RELEASE_TTL);
  if (hit !== undefined) { rate.served += 1; return hit; }

  if (frozen() || !budgetLeft() || !policy.extras) { rate.withheld += 1; return undefined; }

  const path = `/repos/${repo}/releases/latest`;
  const res = await raw(path, { token });
  rate.spent += 1;
  if (res.status === 404) { ageing.set(ck, null); return null; }
  if (!res.ok) throw await fail(res, path);
  const r = (await res.json()) ?? {};
  const out = {
    text: r.tag_name ?? r.name ?? null,
    at: r.published_at ?? r.created_at ?? null,
    url: r.html_url ?? null,
  };
  ageing.set(ck, out);
  return out;
}

// The default branch's commits in a window, as the LISTING gives them: sha, date,
// message and the author's login. That is exactly what the substantive-commit test
// needs bar one exclusion — the corpus-only one, which reads each commit's file list
// and would cost a request per commit — so the classification made from this is the
// cheap one, and `fleet.mjs` states the gap rather than implying the full test ran.
//
// One page, so a member that landed more than `per_page` commits in the window is
// read to a HORIZON rather than completely: `complete` says which, and a caller draws
// the unreached days as unread rather than as quiet ones.
//
// Priced as decoration beside the commit graph it feeds: withheld under budget
// pressure, and a withheld read is `undefined` — not read — never an empty window.
export const WINDOW_COMMITS_TTL = 3600e3;

export async function listCommitsSince(repo, branch, sinceIso, token, perPage = 100) {
  const ck = `window-commits:${repo}:${branch}:${sinceIso}`;
  const hit = ageing.get(ck, WINDOW_COMMITS_TTL);
  if (hit !== undefined) { rate.served += 1; return hit; }

  if (frozen() || !budgetLeft() || !policy.extras) { rate.withheld += 1; return undefined; }

  const path = `/repos/${repo}/commits?sha=${encodeURIComponent(branch)}`
    + `&since=${encodeURIComponent(sinceIso)}&per_page=${perPage}`;
  const res = await raw(path, { token });
  rate.spent += 1;
  // An empty repository answers 409, and a branch the viewer cannot resolve 404:
  // both are "no commits to read", not a failure of the page.
  if (res.status === 409 || res.status === 404) {
    const empty = { since: sinceIso, commits: [], complete: true };
    ageing.set(ck, empty);
    return empty;
  }
  if (!res.ok) throw await fail(res, path);
  const list = (await res.json()) ?? [];
  const out = {
    since: sinceIso,
    commits: (Array.isArray(list) ? list : []).map((c) => ({
      sha: c.sha,
      at: c.commit?.committer?.date ?? c.commit?.author?.date ?? null,
      message: c.commit?.message ?? '',
      author: c.author?.login ?? null,
    })),
    // Whether the window was read to its start. A full page means there may be more.
    complete: (Array.isArray(list) ? list.length : 0) < perPage,
  };
  ageing.set(ck, out);
  return out;
}

export const listComments = (repo, number, token) =>
  conditional(`/repos/${repo}/issues/${number}/comments?per_page=100`, token);

// Every repo an owner has, as the VIEWER sees them — which is the whole access story
// of a roster read this way: a repo the viewer cannot see is not in their fleet, and no
// list stored anywhere can leak one to them.
//
// Conditional per page, so a fleet whose membership has not changed revalidates for
// free. `/users/{owner}/repos` answers for a user and for an organization alike, so
// there is no account-type probe to get wrong.
//
// Pages are capped: a roster this page can sweep is a few dozen members, and an owner
// with hundreds of repos is not a fleet — the cap is stated by the caller rather than
// silently truncating into a page that looks complete.
export async function listOwnerRepos(owner, token, { pages = 3, perPage = 100 } = {}) {
  const out = [];
  let complete = false;
  for (let page = 1; page <= pages; page += 1) {
    const batch = await conditional(
      `/users/${encodeURIComponent(owner)}/repos?per_page=${perPage}&sort=pushed&page=${page}`,
      token,
      {
        transform: (list) => (Array.isArray(list) ? list : []).map((r) => ({
          full_name: r.full_name, archived: Boolean(r.archived), fork: Boolean(r.fork), pushed_at: r.pushed_at ?? null,
        })),
      },
    );
    out.push(...batch);
    if (batch.length < perPage) { complete = true; break; }
  }
  return { repos: out, complete };
}

// Who the viewer is. Also the cheapest possible credential check — a bad token fails
// here rather than three calls into a fleet sweep. Takes no repo: identity is not
// scoped to one.
export const getViewer = (token) =>
  conditional('/user', token, { transform: (u) => ({ login: u.login, avatar_url: u.avatar_url, html_url: u.html_url }) });
