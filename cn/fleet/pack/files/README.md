# fleet

The engine's own pack for a fleet manager: the repo whose settings carry a `fleet` block. `cn`
writes it under `.claudinite/temp/packs/fleet/` on every run there and nowhere else, so no member
holds a copy.

- **Rules** — reading what a sweep reports, running the manual levers, the `FLEET_GITHUB_TOKEN`
  grant, and authoring and policing the packs on a canon's shelf.
- **Skills** — `configuring-the-fleet` (the `fleet` block), `extract-packs-from-a-project`,
  `writing-claudinite-skills`.
- **Tasks** — the `cn fleet` sweeps (`fleet-roster`, `fleet-update`, `fleet-add-missing-packs`,
  `fleet-pack-seeds`) and the canon-curation and growth work over a shelf. Each sweep's README
  says what it reads and writes; every `cn fleet` call is license-checked by the manager repo's
  owner.
