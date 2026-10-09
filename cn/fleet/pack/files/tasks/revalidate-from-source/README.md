# revalidate-from-source

## Why the declaration reads as it does

Carried over from the declaration's comments when it became `task.json`.

revalidate-from-source — the shelf's own currency. A pack teaches a technology, and
the technology moves: an advisory publishes, a best current practice is
revised, an API the skills tell people to call is deprecated. None of that
touches this repo's history, so nothing repo-side can signal it — the watch is
a MONTHLY floor, deliberately ungated, exactly like the tasks/ watchers it
replaces.

KEEPING A PACK CURRENT IS THE CANON'S DUTY, NOT THE PACK'S. A pack's tasks are
work a MEMBER repo runs, so a pack watching its own technology from tasks/ put
the duty on every consumer and made it unrepeatable — each pack would author
its own watcher. One task here covers the whole shelf instead, and a pack opts
in by saying where its technology publishes: an `## Upstream` section in its
README.md, one line per source carrying what to watch, the URL, and the state
the pack's content was last reconciled against. No section, no watch.

No shelf-side gate. Which packs declare an upstream source is standing state,
not movement, so gating on it would only ask "is the shelf still the shelf?" —
and the run must read the sections anyway to know what to fetch. The month is
the trigger, and a run whose sources all moved nothing says so.
Canon content every member reads, and the reconciliation anchors that decide
what the NEXT run re-reads — both owner-approved, never auto-merged. A clean
month is still a PR: advancing the anchors is what windows the next run.

A month whose PR nobody reviewed is not a month to stack a second PR beside.
The run recomputes the whole answer from the sources and the anchors on the
base, so last month's unreviewed PR holds nothing this month's does not, and
`supersede_existing_pr` retires it once the successor exists — or lands it
first, where it had concluded green and was simply never merged.
