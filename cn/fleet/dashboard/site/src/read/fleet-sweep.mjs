// The order a fleet sweep reads in. Pure: no clock, no I/O, no DOM — the callers
// supply what each pass does, this decides who it happens to and when.
//
// A fleet page is read on ONE viewer's rate limit, and that budget is small enough to
// run out mid-sweep (`budget.mjs`). So the order the reads happen in is not a
// performance detail — it decides WHICH FACTS the page ends up holding when the money
// stops.
//
// Read a member end to end before starting the next one, and the budget is spent
// DEPTH-FIRST: the first members get their commit graphs and pack cards before the
// last members have been looked at at all. The page that comes out of that says
// "nothing is on fire" over a fleet it never finished reading, and the members it
// never reached are the ones a viewer cannot tell from healthy.
//
// So a sweep here is HORIZONTAL. Every member is taken through one pass before any
// member starts the next, cheapest and most load-bearing pass first: what a member IS,
// then what needs a person's attention there, then the depth behind those numbers,
// then decoration. Under pressure the page then loses the same panel everywhere
// rather than losing whole members off the end of the list — a fleet missing its
// graphs still answers "where do I need to look", and a fleet missing its tail does
// not.
//
// The passes are also the natural paint boundaries: after pass two every row on the
// page is ranked and every tile counts the whole roster, which is the moment the page
// becomes the thing the viewer opened it for.

// Members are read concurrently, but not all at once: a dozen members at several
// calls each is enough parallel load to trip secondary rate limiting, and the page is
// not in a hurry. Small and steady beats a burst that gets throttled.
export const CONCURRENCY = 4;

export async function pool(items, worker, limit = CONCURRENCY) {
  const out = new Array(items.length);
  let next = 0;
  const runners = Array.from({ length: Math.min(limit, items.length) }, async () => {
    for (;;) {
      const i = next; next += 1;
      if (i >= items.length) return;
      out[i] = await worker(items[i], i);
    }
  });
  await Promise.all(runners);
  return out;
}

// Run `phases` over `members`, one whole pass at a time.
//
// A phase is `{ id, label, run(member), appliesTo?(member) }`. `appliesTo` is how a
// pass skips a member it has nothing to ask about — a repo that does not run
// Claudinite, or one whose first read failed — and skipping is not the same as
// failing: the member simply is not in that pass's population, and the pass's own
// progress counts say so.
//
// `run` is called for its effect on the member; a phase that throws for one member
// must not take the sweep down with it, so throwing is contained and reported through
// `onAdvance` as that member's `error`. The pass continues, and so do the passes after
// it — one member's unreadable tree is one row's missing column.
export async function sweepPhases({ members, phases, limit = CONCURRENCY, onAdvance = null } = {}) {
  for (const [index, phase] of phases.entries()) {
    const targets = members.filter((m) => (phase.appliesTo ? phase.appliesTo(m) : true));
    const total = targets.length;
    let done = 0;
    // A pass with nobody in it still announces itself: "no member had a tree to read"
    // and "the tree pass has not started" are different states, and a progress line
    // that skips silently makes them look identical.
    if (!total) {
      onAdvance?.({ phase: phase.id, label: phase.label, index, done: 0, total: 0, member: null, error: null });
      continue;
    }
    await pool(targets, async (member) => {
      let error = null;
      try {
        await phase.run(member);
      } catch (e) {
        error = e;
      }
      done += 1;
      onAdvance?.({ phase: phase.id, label: phase.label, index, done, total, member, error });
    }, limit);
  }
  return members;
}
