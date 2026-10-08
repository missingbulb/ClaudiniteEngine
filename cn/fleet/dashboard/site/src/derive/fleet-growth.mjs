// What the corpus is doing ACROSS the fleet, from the members' own usage files. Pure:
// no clock of its own beyond the one it is handed, no I/O, no DOM.
//
// This is the panel the fleet page could not have before. Every other figure it shows
// comes from a live read — issues, runs, a head commit — and none of those can answer
// the question a corpus is actually judged on: how often its checks caught something.
// That lives in each member's `sessions-and-elements.json`, which the page now reads anyway,
// so the panel costs nothing beyond what the sweep already spent.
//
// THE RULES THE FLEET PAGE'S OWN NUMBERS FOLLOW APPLY HERE TOO:
//
//   NO VANITY TOTAL. Every figure is a window against the window before it. A count
//   that only ever grows says nothing about this week.
//
//   NOTHING INVENTED. No estimate of hours saved, no score. Each figure is a sum of
//   things that individually happened and are individually checkable in a member's own
//   file.
//
//   AN UNREAD MEMBER IS NOT A ZERO. A member with no usage file is counted as NOT
//   FOLDING and named, never averaged in as a repo where nothing happens. That census
//   is the same fact the retired fleet aggregate carried as `coverage.absent`, derived
//   live instead of stored.

import { growthSeries } from '../read/usage.mjs';

const DAY_MS = 86400e3;

// One fleet-wide day series, plus the census of who could answer for it.
//
// `reads` is the fleet loader's raw per-member reads; each carries `usage`, which is
// null for a member that does not fold, could not be read, or predates the file.
export function fleetGrowth(reads, { now, days = 30, windowDays = 7 } = {}) {
  const readable = (reads ?? []).filter((r) => r && !r.error && r.declaration);
  const folding = readable.filter((r) => r.usage);
  const absent = readable.filter((r) => !r.usage).map((r) => r.repo).sort();

  const series = folding.map((r) => ({ repo: r.repo, growth: growthSeries(r.usage, { now, days }) }));
  const ladder = series[0]?.growth.days.map((d) => d.day)
    ?? Array.from({ length: days }, (_, i) => new Date(Math.floor(now / DAY_MS) * DAY_MS - (days - 1 - i) * DAY_MS).toISOString().slice(0, 10));

  // Summed across members, per day. A day no member had an opinion on stays null —
  // the fleet did not have a quiet Tuesday, nobody folded one.
  const rows = ladder.map((day, i) => {
    const cells = series.map((s) => s.growth.days[i]).filter(Boolean);
    const sum = (field) => {
      const known = cells.map((c) => c[field]).filter((n) => n !== null && n !== undefined);
      return known.length ? known.reduce((a, b) => a + b, 0) : null;
    };
    return {
      day,
      sessions: sum('sessions'),
      checkRuns: sum('checkRuns'),
      checkFailures: sum('checkFailures'),
      findings: sum('findings'),
      // A day is `source: 'none'` for the chart when nothing answered for it, which is
      // what leaves the column blank rather than drawing it at the floor.
      source: cells.some((c) => !c.missing) ? 'folded' : 'none',
    };
  });

  const windowOf = (from, to) => {
    const slice = rows.filter((r) => r.day >= from && r.day < to);
    const sum = (field) => {
      const known = slice.map((r) => r[field]).filter((n) => n !== null);
      return known.length ? known.reduce((a, b) => a + b, 0) : null;
    };
    return { checkRuns: sum('checkRuns'), checkFailures: sum('checkFailures'), sessions: sum('sessions') };
  };

  const dayKey = (t) => new Date(t).toISOString().slice(0, 10);
  const current = windowOf(dayKey(now - (windowDays - 1) * DAY_MS), dayKey(now + DAY_MS));
  const previous = windowOf(dayKey(now - (2 * windowDays - 1) * DAY_MS), dayKey(now - (windowDays - 1) * DAY_MS));

  return {
    days: rows,
    from: ladder[0],
    to: ladder[ladder.length - 1],
    windowDays,
    current,
    previous,
    // The coverage census, derived rather than stored: a member the page could read but
    // that carries no usage file IS the absent row, and naming them is what keeps every
    // rate above honest about its own denominator.
    folding: folding.length,
    members: readable.length,
    absent,
  };
}

// --- what the corpus is doing, in detail ------------------------------------------

// The inspiration was a one-off page folded by hand off every member's logs branch:
// the two check scopes side by side, the rules that actually fire, the skills that
// load and the ones mounted everywhere that never do, and one row per member. Every
// one of those is in the usage file the sweep already reads, so the panel is a
// derivation and not a read.
//
// Two windows, deliberately. The WORKLOAD tiles are this week against last, like the
// benefits block above them, because they are read as a report card. The scope,
// rule, skill and member tables are the whole folded range (`days`, which is the
// file's own retention) with no comparison: a rule that fired twice this week and
// once last is not a trend, and "never loaded" is only a claim worth making over as
// long a range as the file holds. Each states the range it covers.

const SCOPES = ['work', 'world'];
const CHECK_FIELDS = ['runs', 'failures', 'errors', 'blocking', 'advisory', 'ciRuns', 'ciFailures'];

const emptyScope = () => Object.fromEntries(CHECK_FIELDS.map((f) => [f, 0]));

const addInto = (target, row) => {
  for (const f of CHECK_FIELDS) if (typeof row?.[f] === 'number') target[f] += row[f];
};

// The skills a member's tree mounts, for the packs it declares. A skill's home is
// `packs/<pack>/skills/<name>/SKILL.md` under whichever root the mount uses — the
// vendored `.claudinite/shared/`, the member-owned `.claudinite/local/` for a
// `local/<pack>` declaration, or the repo root in the canon itself — and a pack that
// is present but undeclared mounts nothing, so its skills are not counted as mounted.
export function mountedSkills(paths, declaration) {
  const ids = (declaration?.packs ?? []).map((p) => (typeof p === 'string' ? p : p?.id)).filter(Boolean);
  const out = new Set();
  for (const id of ids) {
    const local = id.startsWith('local/');
    const pack = local ? id.slice('local/'.length) : id;
    const root = local ? String.raw`\.claudinite/local/` : String.raw`(?:\.claudinite/shared/)?`;
    const re = new RegExp(`^${root}packs/${pack.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}/skills/([^/]+)/SKILL\\.md$`);
    for (const p of paths ?? []) {
      const m = re.exec(p);
      if (m) out.add(m[1]);
    }
  }
  return out;
}

export function fleetCorpus(reads, { now, days = 30, windowDays = 7 } = {}) {
  const readable = (reads ?? []).filter((r) => r && !r.error && r.declaration);
  const folding = readable.filter((r) => r.usage);
  const absent = readable.filter((r) => !r.usage).map((r) => r.repo).sort();

  const dayKey = (t) => new Date(t).toISOString().slice(0, 10);
  const from = dayKey(now - (days - 1) * DAY_MS);
  const to = dayKey(now + DAY_MS);
  const inRange = (day, lo, hi) => day >= lo && day < hi;

  // --- workload, this week against last ------------------------------------------
  const WORKLOAD = ['sessions', 'captures', 'merges', 'userMessages', 'userCommands'];
  const workloadWindow = (lo, hi) => {
    const out = Object.fromEntries(WORKLOAD.map((f) => [f, null]));
    for (const r of folding) {
      for (const [day, row] of Object.entries(r.usage.days ?? {})) {
        if (!inRange(day, lo, hi)) continue;
        for (const f of WORKLOAD) if (typeof row[f] === 'number') out[f] = (out[f] ?? 0) + row[f];
      }
    }
    return out;
  };
  const current = workloadWindow(dayKey(now - (windowDays - 1) * DAY_MS), to);
  const previous = workloadWindow(dayKey(now - (2 * windowDays - 1) * DAY_MS), dayKey(now - (windowDays - 1) * DAY_MS));

  // --- scopes, rules, skills and members over the folded range ----------------------
  const scopes = Object.fromEntries(SCOPES.map((s) => [s, emptyScope()]));
  const scopeSeen = Object.fromEntries(SCOPES.map((s) => [s, false]));
  const rules = {};
  const loads = {};
  const members = [];

  for (const r of folding) {
    const own = { work: emptyScope(), world: emptyScope() };
    let sessions = null; let turns = null; let commands = null; let skillLoads = null;
    let blocking = 0; let advisory = 0;
    let first = null; let last = null;
    const bump = (cur, n) => (typeof n === 'number' ? (cur ?? 0) + n : cur);

    for (const [day, row] of Object.entries(r.usage.days ?? {})) {
      if (!inRange(day, from, to)) continue;
      first = first === null || day < first ? day : first;
      last = last === null || day > last ? day : last;
      sessions = bump(sessions, row.sessions);
      turns = bump(turns, row.userMessages);
      commands = bump(commands, row.userCommands);
      for (const [scope, counts] of Object.entries(row.checks ?? {})) {
        if (!SCOPES.includes(scope)) continue;
        scopeSeen[scope] = true;
        addInto(scopes[scope], counts);
        addInto(own[scope], counts);
      }
      for (const [rule, f] of Object.entries(row.checkFindings ?? {})) {
        const entry = (rules[rule] ??= { rule, blocking: 0, advisory: 0, members: new Set() });
        entry.blocking += f.blocking ?? 0;
        entry.advisory += f.advisory ?? 0;
        entry.members.add(r.repo);
        blocking += f.blocking ?? 0;
        advisory += f.advisory ?? 0;
      }
      for (const [skill, n] of Object.entries(row.skillLoads ?? {})) {
        const entry = (loads[skill] ??= { skill, loads: 0, members: new Set() });
        entry.loads += n;
        entry.members.add(r.repo);
        skillLoads = bump(skillLoads, n);
      }
    }

    members.push({
      repo: r.repo,
      folding: true,
      sessions, turns, commands,
      skillLoads,
      work: own.work, world: own.world,
      blocking, advisory,
      foldedThrough: r.usage.foldedThrough ?? null,
      generated: r.usage.generated ?? null,
      span: first ? { from: first, to: last } : null,
    });
  }
  for (const repo of absent) members.push({ repo, folding: false });

  // The skills mounted across the fleet, each with who mounts it, so a skill that
  // never loaded can be told apart by how widely it is mounted: one that never loads
  // in one repo may not be that repo's subject; one that never loads anywhere is
  // mis-described or should not be gated at all.
  const mounted = {};
  for (const r of folding) {
    for (const skill of mountedSkills(r.paths, r.declaration)) (mounted[skill] ??= new Set()).add(r.repo);
  }

  const rateOf = (s) => (s.runs ? s.failures / s.runs : null);
  const scopeRows = Object.fromEntries(SCOPES.map((s) => [s, {
    ...scopes[s],
    seen: scopeSeen[s],
    catchRate: rateOf(scopes[s]),
  }]));

  const ruleRows = Object.values(rules)
    .map((e) => ({ ...e, members: e.members.size, total: e.blocking + e.advisory }))
    .sort((a, b) => b.total - a.total || a.rule.localeCompare(b.rule));

  const loaded = Object.values(loads)
    .map((e) => ({ ...e, members: e.members.size, mountedIn: mounted[e.skill]?.size ?? null }))
    .sort((a, b) => b.loads - a.loads || a.skill.localeCompare(b.skill));

  const neverLoaded = Object.entries(mounted)
    .filter(([skill]) => !loads[skill])
    .map(([skill, set]) => ({ skill, mountedIn: set.size }))
    .sort((a, b) => b.mountedIn - a.mountedIn || a.skill.localeCompare(b.skill));

  // Members with a tree listing, which is the denominator "mounted in N" is read
  // against: a member whose tree was not read mounts nothing the page can see.
  const treesRead = folding.filter((r) => Array.isArray(r.paths)).length;

  members.sort((a, b) => Number(b.folding) - Number(a.folding) || (b.sessions ?? -1) - (a.sessions ?? -1) || a.repo.localeCompare(b.repo));

  return {
    from, to: dayKey(now), days, windowDays,
    workload: { current, previous },
    scopes: scopeRows,
    rules: ruleRows,
    findings: { blocking: ruleRows.reduce((n, e) => n + e.blocking, 0), advisory: ruleRows.reduce((n, e) => n + e.advisory, 0) },
    skills: { loaded, neverLoaded, mountedDistinct: Object.keys(mounted).length, treesRead },
    members,
    folding: folding.length,
    readable: readable.length,
    absent,
  };
}
