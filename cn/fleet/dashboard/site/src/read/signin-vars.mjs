// The two repository variables a deployment's sign-in is configured in.
//
// Here, in browser-safe code, because BOTH SIDES of the configuration name them and
// neither may guess: the build reads them to bake the pair into the page's config,
// and the gate names them on the screen a viewer meets when they are unset — which is
// the one screen where the person who can fix it is being told what to set. A second
// spelling on the page would send an owner to set a variable the build does not read;
// the engine's site tests hold the build's spelling to this one.
//
// Namespaced, because a variable's name is repo-global and the dashboard does not own
// the word `CLIENT_ID`. Keyed by the config name each one fills.
export const SIGN_IN_VARS = {
  clientId: 'CLAUDINITE_DASHBOARD_CLIENT_ID',
  exchangeUrl: 'CLAUDINITE_DASHBOARD_EXCHANGE_URL',
};
