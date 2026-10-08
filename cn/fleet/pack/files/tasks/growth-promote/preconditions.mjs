// growth-promote's own precondition term: which members changed the local packs
// this run would read. The fleet aggregate is a canon-only signal over a wider
// credential, and the participation rule (declare claudinite-growth, and do not
// opt out of promotion) is this task's alone — so both live beside its
// declaration rather than in the shared vocabulary.

export const terms = {
  'fleet-local-packs-changed': {
    signals: ['fleet'],
    holds(signals) {
      const fleet = signals.fleet;
      // NOT a decline. A missing credential or a failed enumeration is a read this
      // run could not make, and promoting nothing on unproven fleet state looks
      // exactly like a fleet with nothing to promote — permanently, and silently.
      if (!fleet) return { error: 'no fleet signal — FLEET_GITHUB_TOKEN is unset, or this repo is not a canon home' };
      if (fleet.error) return { error: `fleet enumeration failed — ${fleet.error}` };

      // A participant declares claudinite-growth; the growth entry's
      // `{ config: { promote: false } }` opts a member out of promotion while it
      // keeps extracting and deduping locally (absent or true = participate).
      const changed = (fleet.members ?? [])
        .filter((m) => m.activePacks.includes('claudinite-growth'))
        .filter((m) => m.packConfigs?.['claudinite-growth']?.promote !== false)
        .filter((m) => m.localPacksChanged);
      if (!changed.length) return { holds: false, reason: 'no participating member changed its local packs in the window' };
      const repos = changed.map((m) => m.repo);
      return {
        holds: true,
        reason: `${changed.length} participating member(s) changed their local packs in the window`,
        context: [
          `Target members (local packs changed in the window): ${repos.join(', ')}.`,
          'Read ONLY these members\' local packs — do not enumerate the fleet yourself or widen past this list.',
        ],
      };
    },
  },
};
