// How the page gets a credential: signing in, and nothing else.
//
// WHAT IS NOT POSSIBLE, because it is the first thing anyone asks for: a static
// page CANNOT reuse the viewer's existing github.com login. Session cookies belong
// to github.com and are not sent to api.github.com, and the API does not accept
// cookie auth cross-origin under any circumstances. "Already logged in" is not a
// credential a browser can spend, so there is no configuration that avoids an
// explicit sign-in of some kind.
//
// What IS possible is making that sign-in a BUTTON rather than a paste box: the
// viewer clicks Sign in, GitHub asks them to authorize (once), and everything after
// runs as them, with exactly the repos and permissions their account already has.
// That is the honest version of "the current user's logged-in permissions", and it
// is the only route in — a paste box asks a viewer to go and mint a PAT, which is
// worse than the thing this replaced, and it was what every deployment offered
// until its owner configured the pair below.
//
// The one piece that cannot live here: exchanging the returned `code` for a token
// needs the app's client secret, and GitHub's token endpoint sends no CORS headers,
// so a browser cannot make that call even if the secret were public. It therefore
// happens at a configured `exchangeUrl` — a few lines of serverless function whose
// only job is that swap. It never sees repo data: once the token is back, the page
// talks to GitHub directly.
//
// A deployment that has not configured the pair cannot sign anybody in, and the gate
// says so rather than offering a worse credential (`app.mjs`, `renderGate`).

const STATE_KEY = 'claudinite-dashboard:oauth-state';
const TOKEN_KEY = 'claudinite-dashboard:token';
const REMEMBER_KEY = 'claudinite-dashboard:remember';
// The query the viewer signed in from — `?repo=` — which GitHub does not carry back:
// it returns to the bare `redirect_uri` with only `code` and `state`.
const RETURN_KEY = 'claudinite-dashboard:oauth-return';
const CALLBACK_PARAMS = ['code', 'state', 'error', 'error_description', 'error_uri'];

// WHERE THE CREDENTIAL LIVES IS THE VIEWER'S CALL, asked as `Remember me` beside the
// sign-in it applies to. Unremembered it goes to sessionStorage and dies with the
// tab; remembered it goes to localStorage and survives a browser restart, which is
// what stops a daily visitor from re-signing in every morning. The page cannot know
// whether the machine is shared, so it does not decide on the viewer's behalf — it
// defaults to the safe store and offers the other one in a control that is visible
// whichever way it is set.
//
// Nothing here bounds a remembered token's life. A GitHub App user token expires on
// GitHub's own schedule and comes back 401, which `signOut`s it. localStorage is the
// durable half of a credential whose lifetime GitHub owns, not a second lifetime this
// page grants.
const readFrom = (s, k) => { try { return s.getItem(k) || ''; } catch { return ''; } };
const writeTo = (s, k, v) => { try { if (v) s.setItem(k, v); else s.removeItem(k); } catch { /* private mode */ } };

export const isRemembered = () => readFrom(localStorage, REMEMBER_KEY) === '1';

const store = {
  // Read both, whichever the last sign-in chose — and read the durable one second, so
  // a token this tab obtained wins over a stale remembered one if both somehow exist.
  get() { return readFrom(sessionStorage, TOKEN_KEY) || readFrom(localStorage, TOKEN_KEY); },
  // Written to one store and cleared from the other, so the choice has exactly one
  // copy of the credential behind it and flipping it can never leave a forgotten one.
  set(t, remember = isRemembered()) {
    const durable = Boolean(t) && remember;
    writeTo(localStorage, TOKEN_KEY, durable ? t : '');
    writeTo(sessionStorage, TOKEN_KEY, durable ? '' : t);
  },
};

export const currentToken = () => store.get();
export const signOut = () => store.set('');

// Toggling after signing in moves the credential the viewer already has rather than
// applying to the next sign-in only: a `Remember me` that silently needs a sign-out
// and a sign-in to take effect is a lie about what the box did. The flag itself is
// remembered — it has to survive the redirect to GitHub and back, and a viewer who
// asked to be remembered once is not asking to be asked again.
export function setRemember(on) {
  const token = store.get();
  writeTo(localStorage, REMEMBER_KEY, on ? '1' : '');
  store.set(token, Boolean(on));
}

const randomState = () => {
  const b = new Uint8Array(16);
  crypto.getRandomValues(b);
  return [...b].map((x) => x.toString(16).padStart(2, '0')).join('');
};

export function isOAuthConfigured(config) {
  return Boolean(config?.clientId && config?.exchangeUrl);
}

// Send the viewer to GitHub. `state` is stored and checked on return — without it,
// a third party could hand the viewer a crafted callback URL and have the page
// exchange a code that is not theirs.
export function beginSignIn(config) {
  const state = randomState();
  try { sessionStorage.setItem(STATE_KEY, state); } catch { /* private mode */ }
  const here = new URLSearchParams(location.search);
  for (const k of CALLBACK_PARAMS) here.delete(k);
  try { sessionStorage.setItem(RETURN_KEY, here.toString()); } catch { /* private mode */ }
  const url = new URL('https://github.com/login/oauth/authorize');
  url.searchParams.set('client_id', config.clientId);
  url.searchParams.set('redirect_uri', config.redirectUri ?? location.origin + location.pathname);
  url.searchParams.set('state', state);
  // A GitHub App's user token carries the app's installed permissions, so no scope
  // is requested there. A classic OAuth App needs to ask, and read-only is the
  // narrowest that can see private repos' issues and runs.
  if (config.scope) url.searchParams.set('scope', config.scope);
  location.assign(url.toString());
}

// Handle a `?code=` landing. Returns:
//   { status: 'signed-in' }              — a token was obtained and stored
//   { status: 'none' }                   — this was not a callback; carry on
//   { status: 'error', message }         — a callback that failed, with the reason
//
// The URL is scrubbed either way: a `code` left in the address bar gets copied into
// chat messages and browser history, and it is a credential until it is spent.
export async function completeSignIn(config, { search = location.search, replaceUrl = true } = {}) {
  const params = new URLSearchParams(search);
  const code = params.get('code');
  const returnedState = params.get('state');
  const oauthError = params.get('error');

  const scrub = () => {
    let back = null;
    try { back = sessionStorage.getItem(RETURN_KEY); sessionStorage.removeItem(RETURN_KEY); } catch { /* private mode */ }
    if (!replaceUrl) return;
    const url = new URL(location.href);
    for (const k of CALLBACK_PARAMS) url.searchParams.delete(k);
    for (const [k, v] of new URLSearchParams(back ?? '')) if (!url.searchParams.has(k)) url.searchParams.set(k, v);
    history.replaceState(null, '', url);
  };

  if (oauthError) {
    scrub();
    return { status: 'error', message: params.get('error_description') || oauthError };
  }
  if (!code) return { status: 'none' };

  let expected = null;
  try { expected = sessionStorage.getItem(STATE_KEY); } catch { /* private mode */ }
  try { sessionStorage.removeItem(STATE_KEY); } catch { /* ignore */ }

  // A mismatch is not a retryable error — it means this callback was not started by
  // this tab, so the code is not spent and the viewer is told to start again.
  if (!expected || expected !== returnedState) {
    scrub();
    return { status: 'error', message: 'Sign-in state did not match. Start the sign-in again from this page.' };
  }

  try {
    const res = await fetch(config.exchangeUrl, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify({ code, redirect_uri: config.redirectUri ?? location.origin + location.pathname }),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok || !data.access_token) {
      scrub();
      return { status: 'error', message: data.error_description || data.error || `Token exchange failed (HTTP ${res.status}).` };
    }
    store.set(data.access_token);
    scrub();
    return { status: 'signed-in' };
  } catch (e) {
    scrub();
    return { status: 'error', message: `Token exchange unreachable — ${e.message}` };
  }
}

// THE LOCAL-DEVELOPMENT CREDENTIAL, and the only one that does not come from a
// sign-in. A locally-served checkout has no registered app behind it and no business
// registering one, so a dev server may hand the page a token from the developer's own
// environment in the config it synthesizes — a key the SITE BUILD cannot emit, because
// it writes a fixed list of keys and `devToken` is not on it.
//
// Session-scoped whatever `Remember me` says: a credential the page was handed rather
// than asked for is not one a viewer chose to keep, and a dev token outliving the tab
// in localStorage would then be spent by whatever else that origin later serves.
export function adoptDevCredential(config) {
  const token = String(config?.devToken ?? '').trim();
  if (!token || store.get()) return false;
  writeTo(sessionStorage, TOKEN_KEY, token);
  return true;
}
