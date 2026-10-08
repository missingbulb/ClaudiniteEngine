// The shell: configure, authenticate, and route between the two views.
//
// TWO MODES, DECIDED BY THE DEPLOYMENT'S SHAPE. A config that says where more than one
// member comes from — an `owner` to enumerate, a roster artifact, an explicit list — is
// a FLEET deployment and opens on the overview; anything else is one repo's own page
// and opens straight on it, with no pointless one-row fleet view. Not a mode switch
// anyone sets: the roster source IS the mode, so there is nothing to keep in step.
//
// ROUTING WITHIN A MODE IS THE URL. `?repo=` is the deep dive, its absence the landing
// view, so every view is a link someone can send and the back button works.

import * as gh from './read/github.mjs';
import * as auth from './read/auth.mjs';
import { loadConfig, resolveRoster, isFleetConfig } from './read/config.mjs';
import { clearAll, stats } from './read/cache.mjs';
import { planPolicy, credentialAdvice, MINUTE_MS } from './read/budget.mjs';
import { SIGN_IN_VARS } from './read/signin-vars.mjs';
import { $, el, resetCountUps } from './render/ui.mjs';
import { loadRepo } from './views/view-repo.mjs';
import { loadFleet } from './views/view-fleet.mjs';

let CONFIG = null;
let ROSTER = [];
// The roster's own members the fleet does not act on — the deployment's exclude list,
// resolved to the names it matched. They are drawn, greyed, rather than dropped.
let IGNORED = [];

const showError = (msg) => $('errors').append(el('div', { className: 'err', textContent: msg }));
const showNotice = (msg) => $('errors').append(el('div', { className: 'notice', textContent: msg }));

// --- rate budget ----------------------------------------------------------------

// How this load will be paid for, decided BEFORE it starts. The budget is whatever
// GitHub last told us — carried across page loads, and confirmed by the free
// `/rate_limit` preflight when it has aged — so a fresh tab plans on the real number
// instead of discovering the limit by running into it.
async function planBudget(token) {
  const carried = gh.restoreRate();
  // The preflight costs no primary budget. Skipped only when the carried number is
  // young enough to still be true.
  if (!Number.isFinite(gh.rate.remaining) || Date.now() - (carried?.at ?? 0) > 2 * MINUTE_MS) {
    await gh.preflightRate(token);
  }
  const plan = planPolicy({
    remaining: gh.rate.remaining,
    limit: gh.rate.limit,
    reset: gh.rate.reset,
    memberCount: wantsFleet() ? ROSTER.length : 1,
  });
  gh.setPolicy(plan);
  return plan;
}

// The pill is the honest version of "why is this not fresh". A page that quietly
// serves an hour-old fleet without saying so is worse than one that refuses.
//
// It is LIVE, not a reading taken at the start. The plan is decided before the sweep
// and the sweep is what spends the budget, so a pill drawn once — as this was — shows
// the viewer the number from before every request that would have changed it. The
// figure a viewer opens this for is what is left NOW.
//
// The plan itself is not re-decided mid-load: this load reads under the policy it was
// planned with, and re-planning halfway would make a row's freshness depend on where
// in the sweep it happened to land. Only the number moves.
let currentPlan = null;
let pillQueued = false;

function renderRatePill(plan = currentPlan) {
  if (!plan) return;
  currentPlan = plan;
  const pill = $('rate');
  pill.hidden = false;
  pill.className = `pill${plan.mode === 'frozen' || plan.mode === 'scarce' ? ' stop-pill' : (plan.mode === 'low' || plan.mode === 'tight' ? ' warn-pill' : '')}`;
  const left = Number.isFinite(gh.rate.remaining) ? `${gh.rate.remaining}/${gh.rate.limit ?? '?'}` : 'budget unknown';
  const held = plan.minAge > 0 ? ` · cached ${Math.round(plan.minAge / MINUTE_MS)}m` : '';
  const spent = gh.rate.spent ? ` · −${gh.rate.spent} this load` : '';
  pill.textContent = `${left} · ${plan.tier}${held}${spent}`;
  pill.title = `${plan.reason}\nthis load so far: ${gh.rate.spent} spent, ${gh.rate.revalidated} revalidated free, ${gh.rate.served} from cache`;
}

// A fleet sweep can restate the budget eighty times in a few seconds. Redrawing the
// pill on each would be eighty layouts for a number the eye reads once, so the
// arrivals are coalesced onto the next frame.
function ratePillLater() {
  if (pillQueued || !currentPlan) return;
  pillQueued = true;
  const draw = () => { pillQueued = false; renderRatePill(); };
  if (typeof requestAnimationFrame === 'function') requestAnimationFrame(draw);
  else draw();
}

const repoParam = () => new URL(location.href).searchParams.get('repo');
// The fleet view is what a fleet DEPLOYMENT opens on, decided by the config's shape
// rather than by how many members the enumeration happened to return: a fleet whose
// roster read failed, or one that is momentarily down to a single readable member, is
// still a fleet page and must say so rather than silently becoming that member's own.
const wantsFleet = () => !repoParam() && isFleetConfig(CONFIG);
const currentRepo = () => repoParam() || CONFIG?.defaultRepo || ROSTER[0] || '';

function go(repo) {
  const url = new URL(location.href);
  if (repo) url.searchParams.set('repo', repo);
  else url.searchParams.delete('repo');
  history.pushState(null, '', url);
  render();
}

// --- chrome ---------------------------------------------------------------------

// Where the credential is kept, offered beside every sign-in and left reachable from
// the account menu afterwards: the viewer is the only party who knows whether this
// browser is theirs alone, and the answer is worth changing later without signing out
// to do it.
function rememberBox() {
  const input = el('input', { type: 'checkbox', checked: auth.isRemembered() });
  input.addEventListener('change', () => auth.setRemember(input.checked));
  return el('label', {
    className: 'remember',
    title: 'Keep the credential in this browser after the tab closes. Leave it off on a shared machine.',
  }, [input, ' Remember me']);
}

// The gate's controls, which are the only sign-in surface the page has — and signing
// in is the only route to a credential. A deployment whose owner has not configured
// the pair cannot sign anybody in, so the gate says THAT, naming the two variables:
// the alternative was a paste box telling every viewer to go and mint a PAT, which is
// worse than the thing sign-in replaced and was what a deployment shipped with until
// somebody turned sign-in on. An unconfigured dashboard is not finished being set up,
// and the screen is the one place the person who can finish it is standing.
function renderGate() {
  const box = $('signin-controls');
  box.replaceChildren();

  if (auth.isOAuthConfigured(CONFIG)) {
    for (const id of ['signin-why', 'signin-hint']) $(id).hidden = false;
    $('signin-heading').textContent = wantsFleet() ? 'Sign in to read this fleet' : 'Sign in to read this repository';
    $('signin-how').textContent = 'GitHub asks you to authorize this page once. Everything afterwards '
      + 'runs as you, with exactly the repositories your account can already see.';
    box.append(
      el('button', { className: 'primary', textContent: 'Sign in with GitHub', onclick: () => auth.beginSignIn(CONFIG) }),
      rememberBox(),
    );
    return;
  }
  // The standing prose answers "what does this page do with my credential", which is
  // not the question in front of a viewer who cannot get one. It comes back for every
  // deployment that can.
  for (const id of ['signin-why', 'signin-hint']) $(id).hidden = true;
  $('signin-heading').textContent = 'This dashboard is not finished being set up';
  $('signin-how').textContent = 'Sign in is the only way in, and this deployment has not configured it, so there is '
    + `nothing here to sign in with yet. Its owner sets the repository variables ${SIGN_IN_VARS.clientId} and `
    + `${SIGN_IN_VARS.exchangeUrl} — the pack README's "Turning sign-in on" is that checklist.`;
}

// Who is signed in, and the levers that belong to them rather than to the view.
function renderAccount(viewer) {
  $('account').hidden = !viewer;
  if (!viewer) return;
  $('account-avatar').src = viewer.avatar_url;
  $('account-avatar').alt = viewer.login;
  $('account-login').textContent = viewer.login;
}

// The breadcrumb is the way back out of a deep dive, and it only exists when there is
// somewhere to go back to.
function renderCrumb(repo) {
  const crumb = $('crumb');
  crumb.replaceChildren();
  if (!repo || ROSTER.length <= 1) { crumb.hidden = true; return; }
  crumb.hidden = false;
  crumb.append(
    el('a', { href: '?', textContent: '← Fleet', onclick: (e) => { e.preventDefault(); go(null); } }),
    el('span', { className: 'sep', textContent: '/' }),
    el('select', {
      title: 'Switch repository',
      onchange: (e) => go(e.target.value),
    }, [...new Set([repo, ...ROSTER])].sort().map((n) =>
      el('option', { value: n, textContent: n, selected: n === repo }))),
  );
}

// Switching views is not a change in a number, it is a DIFFERENT set of numbers under
// the same labels — so the counters forget rather than tween from one to the other and
// draw a movement nothing made.
const showView = (which, repo = null) => {
  if ($('fleet-view').hidden !== (which !== 'fleet')) resetCountUps();
  $('fleet-view').hidden = which !== 'fleet';
  $('repo-view').hidden = which !== 'repo';
  $('signin-view').hidden = which !== 'signin';
  if (which === 'signin') $('crumb').hidden = true;
  // The heading says what you are looking at. A repo-mode deployment titled "Claudinite
  // Fleet Status" is the page telling its one member it is something else.
  const title = which === 'fleet' ? 'Claudinite Fleet Status'
    : (which === 'signin' ? 'Claudinite' : (repo ? repo.split('/')[1] ?? repo : 'Claudinite'));
  $('title').textContent = title;
  document.title = which === 'fleet' ? 'Claudinite Fleet Status'
    : (which === 'signin' ? 'Claudinite' : `${title} · Claudinite`);
};

function footer(parts) {
  const c = stats();
  // The bar carries the short version; the footer below carries the full accounting.
  $('footnote').textContent = [
    ...parts,
    `this load: ${gh.rate.spent} API calls, ${gh.rate.revalidated} revalidated free, ${gh.rate.served} from cache`
      + (gh.rate.withheld ? `, ${gh.rate.withheld} withheld to save rate limit` : ''),
    `cache ${(c.bytes / 1024).toFixed(0)}KB in ${c.entries} entries`,
    `read ${new Date().toISOString().replace('T', ' ').slice(0, 16)}Z`,
  ].join(' · ');
}

// --- render ---------------------------------------------------------------------

let inFlight = false;

async function render() {
  if (inFlight) return;
  inFlight = true;
  $('errors').replaceChildren();
  $('reload').disabled = true;

  const token = auth.currentToken();
  // The roster is resolved inside the load, not at boot: an `owner` deployment
  // enumerates as the VIEWER, so it needs the credential — and the enumeration is
  // ETag-revalidated, which makes re-resolving it per load free once warm.
  const roster = await resolveRoster(CONFIG, token, gh);
  ROSTER = roster.repos;
  IGNORED = roster.ignored ?? [];
  const plan = await planBudget(token);
  renderRatePill(plan);
  const advice = credentialAdvice(plan.tier, { hasToken: Boolean(token) });
  if (advice) showNotice(advice.text);
  if (plan.mode === 'frozen' || plan.mode === 'scarce') showNotice(plan.reason);

  // A credential GitHub no longer accepts is not a degraded session to carry on in: it
  // is the same state as never having signed in, so it goes back to the gate rather
  // than leaving a signed-out page rendering someone's fleet from cache.
  let viewer = null;
  try {
    viewer = await gh.getViewer(token);
  } catch (e) {
    if (e.status === 401) {
      inFlight = false;
      $('reload').disabled = false;
      signOut('That credential is no longer valid — sign in again.');
      return;
    }
  }
  renderAccount(viewer);

  try {
    if (wantsFleet()) {
      renderCrumb(null);
      showView('fleet');
      $('footnote').textContent = `Reading ${ROSTER.length} members…`;
      if (roster.error) showError('The owner\'s repositories could not be listed — sign in with an account that can see them.');
      else if (!roster.complete) showNotice('This owner has more repositories than one enumeration reaches; the fleet below is the most recently pushed of them.');
      const fleet = await loadFleet({
        repos: ROSTER,
        ignored: IGNORED,
        token,
        config: CONFIG,
        onOpen: go,
        onError: showError,
        // The fleet is read in passes across the whole roster rather than member by
        // member (`fleet-sweep.mjs`), so the line names the pass as well as its
        // position in it — otherwise a counter that reaches the roster's length four
        // times over reads as the page starting again.
        onProgress: ({ label, done, total, repo }) => {
          $('footnote').textContent = `${label} — ${done}/${total}${repo ? ` — ${repo}` : ''}…`;
        },
      });
      footer([
        `${ROSTER.length} members${roster.source === 'owner' ? ` under ${CONFIG.owner}, as you can see them` : ''}`,
        // The third freshness on this page, beside the load's own and each member's
        // fold: the Updates verdicts are only as current as the roster that carries them.
        fleet?.roster
          ? `freshness from the fleet-roster ${fleet.roster.generated ? `of ${fleet.roster.generated.replace('T', ' ').slice(0, 16)}` : 'at an unstated time'}`
          : 'no fleet-roster in this deployment — Updates read unknown',
      ]);
    } else {
      const repo = currentRepo();
      if (!/^[^/\s]+\/[^/\s]+$/.test(repo)) {
        showError('No repo selected. Add ?repo=owner/name, or configure an owner for a fleet.');
        $('footnote').textContent = '';
        return;
      }
      renderCrumb(repo);
      showView('repo', repo);
      $('footnote').textContent = `Reading ${repo}…`;
      const r = await loadRepo({ repo, token, config: CONFIG, onError: showError });
      footer([
        `${repo} @ ${r.branch} (${r.sha.slice(0, 7)})`,
        `${r.taskCount} declared tasks`,
        `${r.itemCount} work items in the ${r.issuePage.complete
          ? `full issue history (${r.issuePage.scanned} issues)`
          : `most recent ${r.issuePage.scanned} issues — older history not scanned`}`,
        // The past-data plane's own freshness, which is NOT this load's: the panels
        // reaching further back than the live window are only as current as the repo's
        // last fold, and a page that showed one timestamp for both would be claiming
        // the older half is as fresh as the newer.
        r.usage
          ? `usage folded ${r.generated ? r.generated.replace('T', ' ').slice(0, 16) : 'at an unstated time'}`
          : 'no usage fold — past-data panels are limited to the live window',
      ]);
    }
    $('account').open = false;
  } catch (e) {
    showError(e.message ?? String(e));
    if (e.status === 401 || e.status === 403) {
      showError('Your account cannot read this. Sign out and sign in with one that can.');
    }
    $('footnote').textContent = '';
  } finally {
    inFlight = false;
    $('reload').disabled = false;
    // The last word on what this load cost, after the coalesced updates have stopped.
    renderRatePill();
  }
}

// --- entering and leaving ---------------------------------------------------------

// The two transitions across the gate, in one place each so no caller has to remember
// the other half. Leaving drops the CACHE as well as the credential: everything stored
// was read as that person — a private repo's issues included — and data that outlives
// the sign-out on a shared machine is not a sign-out.
function enter() {
  showView(wantsFleet() ? 'fleet' : 'repo');
  render();
}

function signOut(why = null) {
  auth.signOut();
  clearAll();
  resetCountUps();
  renderAccount(null);
  renderCrumb(null);
  $('errors').replaceChildren();
  if (why) showError(why);
  renderGate();
  showView('signin');
}

// --- boot ---------------------------------------------------------------------

async function boot() {
  CONFIG = await loadConfig();

  // Handle an OAuth landing before anything reads the credential.
  const back = await auth.completeSignIn(CONFIG);
  if (back.status === 'error') showError(back.message);

  // A locally-served checkout arrives with its developer's own token in the config the
  // dev server synthesized. Nothing published can carry one (`auth.adoptDevCredential`).
  auth.adoptDevCredential(CONFIG);

  renderAccount(null);
  renderGate();

  // The pill tracks the budget as the sweep spends it, rather than reporting what it
  // was before the sweep began.
  gh.onRateChange(ratePillLater);

  $('reload').addEventListener('click', render);
  // Each heading can say why its numbers are worth reading. The text lives in the
  // markup beside the heading it explains, so there is no second copy in the code.
  for (const b of document.querySelectorAll('button.info')) {
    b.addEventListener('click', (e) => {
      // A heading may explain itself in more than one paragraph (`info-x`, `info-x-2`,
      // …); they open and close together, since they are one explanation. Counted up
      // until one is missing rather than checked against a fixed pair — a third
      // paragraph used to be written, sit in the markup and never open.
      const bodies = [];
      for (let i = 1; ; i += 1) {
        const body = $(`info-${b.dataset.info}${i === 1 ? '' : `-${i}`}`);
        if (!body) break;
        bodies.push(body);
      }
      if (!bodies.length) return;
      // A button inside a collapsed section's summary must explain without also
      // toggling the section open or shut.
      e.preventDefault();
      const show = bodies[0].hidden;
      for (const body of bodies) body.hidden = !show;
      b.setAttribute('aria-expanded', String(show));
    });
  }
  // Clearing the cache is a cold read, not a sign-out: `cache.mjs` owns its own
  // versioned prefix precisely so this sweep cannot reach a remembered credential.
  $('purge').addEventListener('click', () => { clearAll(); resetCountUps(); render(); });
  $('signout').addEventListener('click', () => signOut());
  // A panel that floats over the page has to close the way one does — clicking away
  // from it, not only by finding the control that opened it again.
  document.addEventListener('click', (e) => {
    const account = $('account');
    if (account.open && !account.contains(e.target)) account.open = false;
  });
  $('theme').addEventListener('click', () => {
    const dark = matchMedia('(prefers-color-scheme: dark)').matches;
    const cur = document.documentElement.dataset.theme || (dark ? 'dark' : 'light');
    document.documentElement.dataset.theme = cur === 'dark' ? 'light' : 'dark';
  });
  // Back/forward move between the fleet and a deep dive, because the views are URLs.
  // Behind the gate they move nothing: there is one screen until there is a credential.
  addEventListener('popstate', () => { if (auth.currentToken()) render(); });

  // NOTHING IS READ WITHOUT A CREDENTIAL. Every call this page makes is made as the
  // viewer, so a page with no credential has nothing it could show and no useful
  // anonymous budget to show it with — the deep-dive link in a `?repo=` URL survives the
  // sign-in, because the gate leaves the URL alone.
  if (auth.currentToken()) enter();
  else showView('signin');
}

boot();
