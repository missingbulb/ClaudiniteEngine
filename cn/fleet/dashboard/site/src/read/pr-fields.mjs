// Which issue a merged pull request closes, by the keyword GitHub itself acts on. THE
// DASHBOARD'S OWN COPY of the usage fold's parse, which the fold files a merged PR's
// lead times under: the page drawing the same series for the days the fold has not reached yet must find the issue by the
// same rule, or the two halves of one series disagree about which PRs have a lead
// time at all.

// The FIRST match wins: a PR closing several issues has one issue it is *for*.
const CLOSES_RE = /(?:^|\n)[^\S\n]*(?:closes|fixes|resolves)[^\S\n]+#(\d+)\b/i;

export function closesIssueIn(body) {
  const n = Number(CLOSES_RE.exec(String(body ?? ''))?.[1]);
  return Number.isFinite(n) && n > 0 ? n : null;
}
