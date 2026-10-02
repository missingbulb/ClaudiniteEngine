// @claudinite/sdk: the one module a pack's task script imports. Pure
// helpers run here; anything that needs the engine's state (git, the
// repository's GitHub API under the job's token, a pack's config, the
// declared packs) is a call back to the cn binary that started this
// process, over the pipe its runner holds. Only JSON values cross it.
//
// GitHub calls are named actions, never URLs: `github.openPr({...})`,
// `github.createComment({...})` and the rest. A pack declares the actions
// it takes in its pack.json (`githubActions`), the member's settings may
// narrow that grant, and the binary logs every call with the pack's id; an
// action outside the grant is refused and the refusal is the call's error.

const PIPE = Symbol.for('claudinite.sdk.pipe');

const pipe = () => {
  const p = globalThis[PIPE];
  if (!p) throw new Error('@claudinite/sdk: no engine on the other end — a task script runs under cn, which starts it');
  return p;
};

const call = (method, args) => pipe().call(method, args ?? {});

// The parameters bag the worker was called with.
export const params = () => pipe().params;

// One line on the run's log, prefixed with the task and its item.
export const log = (line) => pipe().params.log(line);

// The engine's version and the calls it answers, as its handshake said.
export const engine = () => ({ version: pipe().engine, methods: [...pipe().methods] });

// Fail the run with the worker's own diagnosis: the kind names what the
// person must do (action, decision, approval, failure).
export function fail(kind, message) {
  const e = new Error(message);
  e.triage = kind;
  throw e;
}

// The verdict that brings the item back at an instant: the run happened
// and found its subject not yet there.
export const requeue = (until, reason = null) => ({
  requeue: { until: new Date(until).toISOString(), ...(reason ? { reason } : {}) },
});

// The verdict that hands the item to its agent, naming what this run
// created ({ delivered: { branch, pr, merged, issue }, reason: { code, detail } }).
export const requestAgent = (payload = true) => ({ requestAgent: payload });

// The two trailers every commit a task writes carries: which task wrote
// it, and what that task authorizes to land unreviewed.
export function trailers() {
  const p = pipe().params;
  const lines = [];
  if (p.pack && p.task) lines.push(`Claudinite-Task: ${p.pack}/${p.task}`);
  if (p.automerge) lines.push(`Claudinite-Automerge-Policy: ${p.automerge}`);
  return lines.join('\n');
}

// A commit message: the subject, an optional body, and the trailers.
export const commitMessage = (subject, body = '') => [subject, body, trailers()].filter(Boolean).join('\n\n');

// git in the repository root, as the engine runs it: bounded, with the
// job's token reaching only the commands that talk to the remote. Resolves
// to { code, stdout, stderr }; a non-zero exit is not a throw.
export const git = (...args) => call('git', { args: args.flat().map(String) });

// The repository's GitHub API, as named actions.
export const github = new Proxy(Object.freeze({}), {
  get: (_, name) => (typeof name === 'string' ? (args) => call(`github.${name}`, args) : undefined),
});

// A declared pack's config from the member's settings, {} when it has none.
export const config = (packId) => call('config', { pack: packId });

// The declared packs: [{ id, version, kind }].
export const packs = () => call('packs', {});
