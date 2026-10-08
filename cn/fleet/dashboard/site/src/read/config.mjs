// Where the page gets its deployment-specific facts. Everything host-specific lives
// in a `dashboard.config.json` beside the page, never in the code, and `cn fleet
// create-dashboard-artifact` writes it from the fleet manager's fleet block and its
// repository variables.
//
// THE ROSTER IS NOT A LIST. A fleet deployment names an `owner` and the page enumerates
// that owner's repos AS THE VIEWER, so the membership is decided at read time by what
// this person can actually see. That is what keeps a fleet page from leaking a repo's
// existence to someone without access, and it is why no repo list is baked into any
// file here: `owner` and `exclude` are the whole of a fleet's config.
//
// Absent config is a valid local run, not a broken one: it means whatever repo the URL
// names, and a gate that says sign-in has not been configured. So every key here is
// optional and every miss is a plain default.
//
// Shape:
//   {
//     "mode":        "fleet",                            // what the build writes; null reads as one repo's page
//     "clientId":    "Iv1.abc123",                       // GitHub App / OAuth App client id
//     "exchangeUrl": "https://…/github-oauth",           // the code→token endpoint
//     "redirectUri": "https://owner.github.io/Repo/",    // defaults to this page's URL
//     "scope":       "repo",                             // classic OAuth Apps only
//     "owner":       "octo",                             // a fleet: whose repos, as the viewer
//     "exclude":     ["octo/old"],                       // …and which of them are not members
//     "defaultRepo": "owner/a",
//     "deploymentRepo": "owner/site",                    // the repo the site is published from
//     "rates":       { "claude-opus-5": { "in": 15, "cacheRead": 1.5, "out": 75 } }
//   }
//
// One key is not a deployment's to set: `devToken` (`read/auth.mjs`), which the build
// never writes.

export const DEFAULTS = {
  // WHICH DASHBOARD THIS IS: `fleet`, the overview across a roster, which is what the
  // build writes. Null is what a locally-served checkout with no config file at all
  // reads, and `isFleetConfig` renders that as one repo's page.
  mode: null,
  clientId: null,
  exchangeUrl: null,
  redirectUri: null,
  scope: null,
  // Whose repos this deployment covers, enumerated as the viewer, and which of them are
  // not in the fleet. Both optional: unset means this is one repo's own page.
  owner: null,
  exclude: [],
  defaultRepo: null,
  // The repository the site is published from, which the build knows and the page
  // cannot: where a fleet deployment's roster artifact and its own cards are read.
  deploymentRepo: null,
  // USD per MILLION tokens, per model, per counter — the one deployment-specific fact
  // behind every dollar figure the page shows. Null is a supported state and not a
  // broken one: an unpriced page still counts tokens, and says which key to set.
  rates: null,
};

export async function loadConfig(url = './dashboard.config.json') {
  try {
    const res = await fetch(url, { cache: 'no-store' });
    if (!res.ok) return { ...DEFAULTS };
    return { ...DEFAULTS, ...(await res.json()) };
  } catch {
    return { ...DEFAULTS };
  }
}

// Whether this deployment is a FLEET one. It READS the stated mode rather than inferring
// it from the roster: what a deployment covers is a thing it declares, not a thing
// deduced from which other keys happen to be present.
export const isFleetConfig = (config) => config?.mode === 'fleet';

// Whether a NAME is on the deployment's exclude list — the fleet's "ignore this repo".
// It takes either spelling, because a manager writes whichever reads naturally in its
// own fleet block, and either case, because GitHub's names are case-insensitive and
// the build lowers the list.
//
// IGNORED IS A STATE, NOT A FILTER (owner, 2026-09-13). Every repo the viewer can see
// belongs on the fleet page, including the ones the fleet does not act on: an ignored
// repo and an archived one are drawn greyed, with their core GitHub facts and a way
// back into the fleet, rather than left off a page whose reader then cannot tell them
// from a repo that does not exist. What ignoring buys is that no fleet OPERATION
// touches them and no figure counts them — which is the sweeps' business, not the
// page's.
export const ignored = (fullName, exclude = []) => {
  const names = exclude.map((e) => String(e).toLowerCase());
  const full = String(fullName).toLowerCase();
  return names.includes(full) || names.includes(full.split('/')[1]);
};

// The roster, resolved: `owner` enumerated live as the viewer. `gh` is injected so this
// is testable without a network and so config.mjs owes the GitHub client nothing.
export async function resolveRoster(config, token, gh) {
  const exclude = config?.exclude ?? [];
  // The ignored NAMES travel beside the roster rather than being subtracted from it:
  // the page draws them, greyed, and needs to know which they are. Archived is not
  // here — it is GitHub's own flag, read per repo with everything else about it.
  const markIgnored = (repos) => repos.filter((r) => ignored(r, exclude));

  if (!config?.owner) return { repos: [], ignored: [], source: 'none', complete: true };
  try {
    const { repos, complete } = await gh.listOwnerRepos(config.owner, token);
    // A FORK is still not a member: it is someone else's project, and its work is
    // upstream's. Everything else the viewer can see is on the page.
    const names = repos.filter((r) => !r.fork).map((r) => r.full_name).sort();
    return {
      repos: names,
      ignored: markIgnored(names),
      source: 'owner',
      // Whether the enumeration reached the end of the account. A truncated one is
      // said out loud rather than rendered as a fleet that happens to be that size.
      complete,
    };
  } catch (error) {
    return { repos: [], ignored: [], source: 'owner', complete: false, error };
  }
}
