// How much of the viewer's GitHub rate limit this page is allowed to spend, and what
// it does instead when it cannot afford a live read. Pure: no clock of its own, no
// I/O, no DOM — `github.mjs` enforces what this decides.
//
// WHY A POLICY AT ALL, when `cache.mjs` already caches everything. Because the two
// credentials this page runs under differ by a factor of eighty-three:
//
//   unauthenticated   60 requests/hour, per IP address
//   a user token   5,000 requests/hour, per user (shared across every app they use)
//
// A fleet sweep costs roughly seven requests per member on a cold cache. Twelve
// members is ~84 — over the anonymous budget for the whole hour on the FIRST load, and the
// page then spends the rest of that hour rendering "unreadable" rows for repos that
// are perfectly fine. Caching alone does not save it, because the existing strategies
// all still make a request: an ETag revalidation is free of the PRIMARY limit but it
// is still a request, and a cold entry has nothing to revalidate.
//
// So the policy adds the one thing the cache layer cannot decide on its own: a
// STALENESS FLOOR. Under pressure, an entry younger than `minAge` is served without
// asking GitHub at all — no request, not even a conditional one. That is what makes
// "reload the page and it costs nothing for the next X minutes" true rather than
// merely cheap.
//
// The ladder is driven by HEADROOM — the remaining budget measured in whole page
// loads, not in requests — because "1,400 left" means something different to a
// one-repo page than to a forty-member fleet.

// Requests one member costs on a cold cache: repo metadata, head sha, the member file,
// the tree, the live open listing, one history page of issues, one runs page, and a
// year of commit activity for the row's graph. A warm member costs fewer, a Node member
// one more (the settings file its missing member file sends the read on to) and a
// not-adopted one four. The thresholds below are tuned against this figure, so it
// stays the cn member's cost rather than following the rarer shape up.
export const COST_PER_MEMBER = 8;

// The page's own fixed overhead: the viewer, plus the deployment's fleet-roster, read
// once at its head sha and free on a warm load.
export const COST_FIXED = 3;

export const MINUTE_MS = 60e3;
export const HOUR_MS = 3600e3;
export const DAY_MS = 24 * HOUR_MS;

// What a dashboard load must never do: spend the viewer's LAST requests. Whatever is
// held back here is what is left for the rest of their hour — another tab, a CLI, the
// next page they open. Ten percent, floored at five so an anonymous 60 keeps six, and
// capped at fifty so a 5,000 budget does not hold back five hundred.
export const reserveFor = (limit) =>
  (Number.isFinite(limit) && limit > 0 ? Math.min(50, Math.max(5, Math.ceil(limit * 0.1))) : 5);

// The tier a limit implies. Read off `x-ratelimit-limit` rather than off whether a
// token was supplied, because those disagree in exactly the case that matters: a
// token the server rejected reports the anonymous limit while the page still believes
// it is signed in.
export function classify(limit) {
  if (!Number.isFinite(limit)) return 'unknown';
  if (limit <= 60) return 'anonymous';
  if (limit <= 1000) return 'workflow';      // an Actions GITHUB_TOKEN, per repository
  return 'user';                             // a user or installation token
}

export const estimateCost = (memberCount) => COST_FIXED + COST_PER_MEMBER * Math.max(1, memberCount);

// The modes, worst last. Each names what the page will DO, not how alarmed it is.
export const MODES = ['live', 'tight', 'low', 'scarce', 'frozen'];

const CEIL_MIN_AGE = HOUR_MS;   // a reset is at most an hour out, so nothing is held longer

// The decision. `remaining`/`limit`/`reset` are the last thing GitHub said (any of
// them may be null, which is "we have not asked yet"), `memberCount` sizes the load,
// and `now` is supplied so this stays pure.
//
// Returns:
//   mode          which rung of the ladder
//   minAge        a cached entry younger than this is served WITHOUT a request
//   historyTtl    how long settled issue history stays good
//   spendCeiling  how many rate-limit-spending requests this load may make
//   extras        whether DECORATION may be read at all. A figure that tells you a
//                 repo has been busy is worth a request when there are thousands
//                 left and worth none when the next fault-finding read might not fit
//   reason        one sentence, shown to the viewer — the page never silently throttles
export function planPolicy({ remaining = null, limit = null, reset = null, memberCount = 1, now = Date.now() } = {}) {
  const cost = estimateCost(memberCount);
  const tier = classify(limit);
  const msToReset = Number.isFinite(reset) ? Math.max(0, reset * 1000 - now) : null;
  const untilReset = Math.min(msToReset ?? CEIL_MIN_AGE, CEIL_MIN_AGE);

  // Nothing known yet — the first load of the first tab. Read live; the very first
  // response teaches us the budget and every load after this one is planned.
  if (!Number.isFinite(remaining)) {
    return {
      mode: 'live', tier, cost, minAge: 0, historyTtl: DAY_MS, spendCeiling: Infinity, extras: true,
      reason: 'budget not known yet — reading live',
    };
  }

  const reserve = reserveFor(limit);
  const spendable = Math.max(0, remaining - reserve);
  const headroom = spendable / cost;

  if (spendable <= 0) {
    return {
      mode: 'frozen', tier, cost, minAge: untilReset, historyTtl: 30 * DAY_MS, spendCeiling: 0, extras: false,
      reason: `rate limit spent — showing cached data only${msToReset ? `, for another ${Math.ceil(msToReset / MINUTE_MS)}m` : ''}`,
    };
  }
  if (headroom < 1) {
    return {
      mode: 'scarce', tier, cost, minAge: untilReset, historyTtl: 30 * DAY_MS, spendCeiling: spendable, extras: false,
      reason: `only ${remaining} requests left — cached data is served as-is until the limit resets`,
    };
  }
  if (headroom < 6) {
    return {
      mode: 'low', tier, cost, minAge: 30 * MINUTE_MS, historyTtl: 7 * DAY_MS, spendCeiling: spendable, extras: false,
      reason: `${remaining} requests left — anything read in the last 30m is served from cache`,
    };
  }
  if (headroom < 20) {
    return {
      mode: 'tight', tier, cost, minAge: 5 * MINUTE_MS, historyTtl: 3 * DAY_MS, spendCeiling: spendable, extras: true,
      reason: `${remaining} requests left — anything read in the last 5m is served from cache`,
    };
  }
  return {
    mode: 'live', tier, cost, minAge: 0, historyTtl: DAY_MS, spendCeiling: spendable, extras: true,
    reason: `${remaining} requests left — everything revalidated`,
  };
}

// What the viewer is told about their credential. The anonymous case is the one worth
// a sentence: the owner's instinct is that being logged in to github.com in this
// browser counts, and it does not — api.github.com never sees that session.
export function credentialAdvice(tier, { hasToken = false } = {}) {
  // No credential at all is anonymous whatever the headers said — and the headers may
  // have said nothing, because the very first load has not spoken to GitHub yet. That
  // first load is exactly when this sentence is worth reading.
  if (tier === 'anonymous' || (tier === 'unknown' && !hasToken)) {
    return {
      level: 'serious',
      text: 'Not signed in to this page — GitHub allows 60 requests per hour per IP address, which one fleet sweep exceeds. '
        + 'Sign in for 5,000/hour. Being logged in to github.com in this browser does not count: the API never '
        + 'receives that session.',
    };
  }
  return null;
}
