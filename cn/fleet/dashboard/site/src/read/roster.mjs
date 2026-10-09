// THE FLEET'S FRESHNESS, as the manager publishes it. A fleet manager's `cn fleet
// roster` lands `.claudinite/fleet/roster.GENERATED.json` daily: one
// verdict per repository its sweep enumerated — shape, dormancy, the pin and held
// versions, and freshness against what the member's own update would move it to. The
// page reads that file rather than pricing every member against npm and the pack shelf
// in the browser, and reads it like every content read: at the deployment's head sha,
// so a warm load spends nothing on it.
//
// Optional by design. A deployment that runs no fleet-roster has no file, and every
// member's Updates verdict then reads unknown rather than an invented "current".
//
// Spelled here, not imported: the page imports nothing from the engine. The file's
// shape is the engine's `fleet.Verdict`, and only the fields drawn are read.

export const ROSTER_PATH = '.claudinite/fleet/roster.GENERATED.json';

const isObject = (v) => v !== null && typeof v === 'object' && !Array.isArray(v);

// The roster file's text as `{ generated, owner, byRepo }`, keyed by `owner/name` in
// lower case — GitHub's names are case-insensitive and the enumeration and the sweep
// need not spell one alike — or null when it is not a roster this page can read.
export function parseRoster(text) {
  let doc;
  try { doc = JSON.parse(text); } catch { return null; }
  if (!isObject(doc) || !Array.isArray(doc.members)) return null;
  const byRepo = new Map();
  for (const v of doc.members) if (isObject(v) && typeof v.repo === 'string') byRepo.set(v.repo.toLowerCase(), v);
  return {
    generated: typeof doc.generated === 'string' ? doc.generated : null,
    owner: typeof doc.owner === 'string' ? doc.owner : null,
    byRepo,
  };
}

export const verdictFor = (roster, repo) => roster?.byRepo?.get(String(repo).toLowerCase()) ?? null;

// The deployment's roster, or null where it carries none, could not be read, or did
// not parse. Null is the stated "this deployment runs no fleet-roster", never a
// failure of the page.
export async function readRoster({ repo, token, gh }) {
  if (!repo) return null;
  try {
    const meta = await gh.getRepo(repo, token);
    const sha = await gh.getHeadSha(repo, meta.default_branch, token);
    const text = await gh.getTextAtSha(repo, sha, ROSTER_PATH, token);
    return text ? parseRoster(text) : null;
  } catch {
    return null;
  }
}
