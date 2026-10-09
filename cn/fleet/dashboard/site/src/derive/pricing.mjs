// What a window of token usage COST, and — just as often — what it cannot be said to
// have cost.
//
// The rates are a deployment's own fact, not the fold's: the same usage file priced by
// two different contracts gives two different answers, and the fold has no business
// guessing either. So they live in the deployment's config (`rates`), and everything
// here is a reduction over the fold's `tokensByModel` rows against that table.
//
// THREE STATES, KEPT APART. A dollar figure here is one of:
//   - a number, when every model in the window has a rate;
//   - *unpriced*, when the deployment set no `rates` at all — a configuration gap,
//     which the page states by naming the key rather than showing $0;
//   - a number PLUS an unpriced remainder, when some models are rated and others are
//     not. The remainder travels as tokens, never folded into the sum, because a model
//     priced at nothing is the one error this whole module exists to avoid.
// A fourth state belongs to the fold, not here: a day whose rows carry no
// `tokensByModel` at all is *not recorded*, and `priceWindow` reports it as such.

// The config key the whole thing rests on, named once so the page's own "unpriced"
// note and this module cannot disagree about what a reader has to set.
export const RATES_KEY = 'rates';

// USD per MILLION tokens, per counter. `cacheWrite` is optional and falls back to
// `in`: a cache-creation token is an input token the provider chose to charge
// differently, and a deployment that has not said so is better served by the input
// rate than by a zero.
const PER_MILLION = 1e6;

export function priceModel(counters, rate) {
  if (!rate || typeof rate !== 'object') return null;
  const n = (v) => (typeof v === 'number' && Number.isFinite(v) ? v : 0);
  const r = (v) => (typeof v === 'number' && Number.isFinite(v) ? v : null);
  const cacheWrite = r(rate.cacheWrite) ?? r(rate.in);
  const priced = (tokens, perMillion) => (perMillion === null ? null : (tokens * perMillion) / PER_MILLION);
  const parts = [
    priced(n(counters?.input), r(rate.in)),
    priced(n(counters?.cacheRead), r(rate.cacheRead)),
    priced(n(counters?.cacheCreate), cacheWrite),
    priced(n(counters?.output), r(rate.out)),
  ];
  // A rate table naming a model but not every counter it was billed for prices
  // nothing for that model: a partial sum reads as a total and understates it silently.
  return parts.some((p) => p === null) ? null : parts.reduce((a, b) => a + b, 0);
}

// The tokens one model's counters add up to, for the unpriced remainder and the
// top-model share — both of which are questions about volume, not money, and are
// answerable for a model with no rate.
export const modelTokens = (counters) =>
  ['input', 'cacheRead', 'cacheCreate', 'output']
    .reduce((n, f) => n + (typeof counters?.[f] === 'number' ? counters[f] : 0), 0);

// Sum a window's `tokensByModel` rows into one `{ model: counters }` map. `rows` is
// whatever the caller's window selected — day rows, week rows, one member's or a
// fleet's. A row with no `tokensByModel` key contributes nothing rather than a zero,
// which is what keeps `recorded` honest.
export function tokensByModelOver(rows) {
  const out = {};
  let recorded = false;
  for (const row of rows ?? []) {
    const byModel = row?.tokensByModel;
    if (!byModel || !Object.keys(byModel).length) continue;
    recorded = true;
    for (const [model, counters] of Object.entries(byModel)) {
      const into = (out[model] ??= { input: 0, cacheRead: 0, cacheCreate: 0, output: 0 });
      for (const field of Object.keys(into)) {
        if (typeof counters?.[field] === 'number') into[field] += counters[field];
      }
    }
  }
  return { models: out, recorded };
}

// The whole reduction, from the window's rows and the deployment's table.
//
//   recorded        — did any row in the window carry a per-model split at all
//   usd             — the priced sum, or null when nothing could be priced
//   unpricedTokens  — tokens belonging to models with no usable rate
//   unpricedModels  — which ones, so the page can name what to add to the table
//   tokens          — the window's total, priced and unpriced together
//   top             — the model with the most tokens, and its share of them
//   ratesSet        — false when the deployment configured no table at all, which is
//                     a different sentence from "one model is missing"
export function priceWindow(rows, rates) {
  const { models, recorded } = tokensByModelOver(rows);
  const ratesSet = Boolean(rates && typeof rates === 'object' && Object.keys(rates).length);

  let usd = null;
  let unpricedTokens = 0;
  let tokens = 0;
  const unpricedModels = [];
  const byModel = [];

  for (const [model, counters] of Object.entries(models)) {
    const own = modelTokens(counters);
    tokens += own;
    const cost = ratesSet ? priceModel(counters, rates[model]) : null;
    if (cost === null) {
      unpricedTokens += own;
      unpricedModels.push(model);
    } else {
      usd = (usd ?? 0) + cost;
    }
    byModel.push({ model, tokens: own, usd: cost });
  }

  byModel.sort((a, b) => b.tokens - a.tokens || a.model.localeCompare(b.model));
  unpricedModels.sort();

  return {
    recorded,
    ratesSet,
    usd,
    tokens,
    unpricedTokens,
    unpricedModels,
    byModel,
    // The share the heaviest model took, which is the figure that says whether a
    // deployment is running on one model or several. Null on an empty window rather
    // than a zero share of nothing.
    top: byModel.length && tokens > 0 ? { model: byModel[0].model, share: byModel[0].tokens / tokens } : null,
  };
}
