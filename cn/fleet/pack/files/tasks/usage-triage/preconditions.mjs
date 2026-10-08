// usage-triage's gate: is there a finding worth spending an opus session on?
//
// A finding earns a proposal only once it has stood two weeks AND its cause is
// well enough known that a diff can argue from it. Everything else - a young
// finding, a finding whose cause the record cannot settle - stays in the review
// file and on the dashboard, where evidence belongs until it is more than
// evidence. A week with none opens no session at all, which is what bounds this
// stage's cost.
//
// Read off the review's own file, which the checkout already carries: no API call
// on a term asked at every tick.
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

export const REVIEW_PATH = '.claudinite/usage/element-review-findings.json';
// Where the review wrote it before `.claudinite/usage/`, read until the review has moved it.
// @legacy-tolerance advisory:legacy-shape-in-use retire:#2323
export const LEGACY_REVIEW_PATH = '.claudinite/local/usage-review.GENERATED.json';
export const ACTIONABLE = ['known', 'probable'];

export function lastingFindings(root) {
  for (const path of [REVIEW_PATH, LEGACY_REVIEW_PATH]) {
    try {
      const file = JSON.parse(readFileSync(join(root, path), 'utf8'));
      return (file.findings ?? []).filter((f) => f.lasting && ACTIONABLE.includes(f.cause));
    } catch { /* not at this path */ }
  }
  return [];
}

export const terms = {
  'lasting-usage-finding': {
    signals: [],
    holds() {
      const root = process.env.CLAUDINITE_REPO_ROOT || process.cwd();
      const found = lastingFindings(root);
      if (!found.length) {
        return { holds: false, reason: `no finding in ${REVIEW_PATH} has stood two weeks with a cause a diff could argue from` };
      }
      const subjects = [...new Set(found.map((f) => f.subject))];
      return {
        holds: true,
        reason: `${found.length} lasting findings over ${subjects.length} subjects`,
        // Binding scope for the run: the subjects, so the session proposes about
        // these and re-derives nothing about which findings count.
        context: [`Lasting findings, by subject: ${subjects.join(', ')}`],
      };
    },
  },
};
