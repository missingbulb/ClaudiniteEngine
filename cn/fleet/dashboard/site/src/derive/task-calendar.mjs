// The scheduling calendar as the dashboard reads it: which instant a task's window
// last opened at or opens next, how long a cadence's period is, and which cadence
// term a declaration states. THE DASHBOARD'S OWN COPY of the engine's arithmetic:
// the page renders in a browser, so it carries the arithmetic it renders with.
//
// All times are UTC. `now` is always injected, so every answer is deterministic.

const HOUR_MS = 3600e3;
const DAY_MS = 24 * HOUR_MS;

// Sunday opens the week, matching Date#getUTCDay's own 0.
const SUNDAY = 0;

// When `frequency`'s current period opened, as a Date, or null for `manual`, which has
// no period at all. THE PERIOD IS THE UTC CALENDAR and nothing a repo configures
// (#1995): a day opens at midnight UTC, a week on the Sunday that opened it, a month
// on its 1st.
export function mostRecentAnchor(frequency, now) {
  const at = new Date(now);
  if (frequency === 'manual') return null;
  const midnight = Date.UTC(at.getUTCFullYear(), at.getUTCMonth(), at.getUTCDate());
  if (frequency === 'daily') return new Date(midnight);
  if (frequency === 'weekly') return new Date(midnight - ((at.getUTCDay() - SUNDAY + 7) % 7) * DAY_MS);
  if (frequency === 'monthly') return new Date(Date.UTC(at.getUTCFullYear(), at.getUTCMonth(), 1));
  throw new Error(`unknown frequency "${frequency}"`);
}

// --- the cadence terms ----------------------------------------------------------
// How a task states WHEN it runs, inside its own `preconditions`: the queue keeps no
// calendar of its own, so the cadence is one of the task's conditions.
//
//   schedule:at-most-<daily|weekly|monthly>   no run created or closed since this
//                                             UTC period opened
//
// Whether a task is asked at all is its `trigger`, not the shape of this list: a
// task nothing asks may still state conditions, which are judged when somebody
// creates an item for it (the retired `frequency: manual` is `trigger: 'request'`).
//
export const CADENCES = ['daily', 'weekly', 'monthly'];
export const ALTERNATIVE_SEPARATOR = '||';
export const SCHEDULE_TERM = 'schedule';
export const AT_MOST_PREFIX = 'at-most-';
export const NOT_FAILED_TERM = 'last-run-not-failed';
export const NOT_PARKED_TERM = 'last-run-not-parked';

export const scheduleTermFor = (cadence) => `${SCHEDULE_TERM}:${AT_MOST_PREFIX}${cadence}`;
export function cadenceOfScheduleArg(arg) {
  const text = String(arg ?? '');
  if (!text.startsWith(AT_MOST_PREFIX)) return null;
  const cadence = text.slice(AT_MOST_PREFIX.length);
  return CADENCES.includes(cadence) ? cadence : null;
}

// The spelling the cadence term was introduced with, permanently accepted because a
// task declaration is member-owned data no vendoring pass rewrites. The page reads
// declarations as text straight out of GitHub, so it meets the old spelling on any
// member that has not converged.
export const DUE_TERM = 'due';

// Rewrite every `due:<cadence>` in an expression to the current spelling, in place,
// leaving everything else byte-identical. The contract's own door does the same
// (`normalizeCadenceTerms` in calendar.mjs), so a declaration reads one way wherever
// it is lifted from.
export function normalizeCadenceTerms(preconditions) {
  if (!Array.isArray(preconditions)) return preconditions;
  return preconditions.map((entry) => (typeof entry === 'string'
    ? entry.split(ALTERNATIVE_SEPARATOR)
      .map((alt) => {
        const t = alt.trim();
        const cadence = t.startsWith(`${DUE_TERM}:`) ? t.slice(DUE_TERM.length + 1) : null;
        return cadence !== null && CADENCES.includes(cadence) ? alt.replace(t, scheduleTermFor(cadence)) : alt;
      })
      .join(ALTERNATIVE_SEPARATOR)
    : entry));
}

// The term references an expression carries: each entry split on `||`, each
// reference `{ name, arg }` with the argument after the first colon.
const alternativesOf = (entry) => String(entry ?? '').split(ALTERNATIVE_SEPARATOR).map((t) => t.trim()).filter(Boolean)
  .map((t) => { const c = t.indexOf(':'); return c === -1 ? { name: t, arg: null } : { name: t.slice(0, c).trim(), arg: t.slice(c + 1).trim() }; });
const entriesOf = (preconditions) => (Array.isArray(preconditions) ? preconditions : []).map(alternativesOf);

// The cadence a declaration states, as `{ kind: 'period', cadence }`, or null for a
// task with no cadence term (asked at every tick while it states any condition, it
// runs whenever those hold). The first cadence term wins; both spellings are read.
export function cadenceOf(preconditions) {
  for (const ref of entriesOf(preconditions).flat()) {
    if (ref.name === SCHEDULE_TERM) {
      const cadence = cadenceOfScheduleArg(ref.arg);
      if (cadence) return { kind: 'period', cadence };
    }
    if (ref.name === DUE_TERM && CADENCES.includes(ref.arg)) return { kind: 'period', cadence: ref.arg };
  }
  return null;
}

// Whether the declaration states any condition at all. Not a scheduling answer on
// its own — `trigger` is that. An entry carrying only separators states nothing,
// which is why this is not a length test.
export const statesConditions = (preconditions) => entriesOf(preconditions).some((alts) => alts.length > 0);

// A term gates when it is a whole conjunct of the expression — `['x', …]` gates,
// `['due:daily || x']` merely widens.
const gatesOn = (preconditions, term) =>
  entriesOf(preconditions).some((alts) => alts.length === 1 && alts[0].name === term && alts[0].arg === null);

// A task stops past its own failure park only when it says so: nothing holds a
// task's lane but its own word — `last-run-not-failed` for that park alone, or
// `last-run-not-parked`, which holds behind all four and so answers this too.
export const holdsOnFailure = (preconditions) =>
  gatesOn(preconditions, NOT_FAILED_TERM) || gatesOn(preconditions, NOT_PARKED_TERM);

// Whether the declaration holds its lane behind EVERY park, the three a person's
// inbox owns included. A reader showing what happens next needs the wider answer
// as well: a task held behind an approval park is not being asked on schedule,
// and an anchor shown there promises a run the task declines.
export const holdsOnAnyPark = (preconditions) => gatesOn(preconditions, NOT_PARKED_TERM);

// What the retired `frequency` field always meant, as the term that now says it —
// or null for `manual`, which meant no schedule at all and so adds no term.
export const cadenceTermFor = (frequency) =>
  (frequency === 'manual' ? null : scheduleTermFor(frequency));

// --- the anchors ---------------------------------------------------------------

// One period of a cadence word (`daily`, `weekly`, `monthly`; `manual` has none), in
// ms — the coarse step `nextAnchor` walks.
export function periodMs(frequency) {
  const freq = frequency;
  if (freq === 'weekly') return 7 * DAY_MS;
  if (freq === 'monthly') return 31 * DAY_MS;
  if (freq === 'manual') return null;
  return DAY_MS;
}

// The period a TASK keeps, read off the cadence term its declaration states, and null
// for a task with no cadence term (asked at every tick, it runs on movement or when woken).
export function taskPeriodMs(decl) {
  const cadence = cadenceOf(decl?.preconditions);
  return cadence === null ? null : periodMs(cadence.cadence);
}

// When the next period opens, strictly after `now`, which is what a rolled item is
// stamped with. Derived by walking `mostRecentAnchor` forward rather than by adding a
// period, because months are not a fixed distance apart. The coarse step is under one
// period, so the loop advances by at most two steps and never overshoots.
export function nextAnchor(frequency, now) {
  if (frequency === 'manual') return null;
  const from = mostRecentAnchor(frequency, now).getTime();
  // An unreadable instant makes every comparison below false, so the walk would never
  // terminate. There is no next period of a moment that is not one.
  if (!Number.isFinite(from)) return null;
  const step = frequency === 'monthly' ? 28 * DAY_MS : periodMs(frequency);
  for (let t = from + step; ; t += step) {
    const candidate = mostRecentAnchor(frequency, new Date(t));
    if (candidate.getTime() > from) return candidate;
  }
}
