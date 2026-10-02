// The task runner: the Node half of a task step, which cn starts as
// `node --import <register.mjs> runner.mjs` with the task directory as its
// working directory and talks to over this process's stdin and stdout, one
// JSON object per line. First a handshake, {"proto", "engine", "methods"},
// answered with the same proto; then one request:
//
//   {"op":"work","module":"worker.mjs","secrets":[...],"automerge":"..."}
//   {"op":"terms","file":"preconditions.mjs","refs":[...],"signals":{...},
//    "config":{...},"item":{...},"windowDays":N,"now":"..."}
//
// While a request is answered, @claudinite/sdk calls back with
// {"sdk":METHOD,"id":N,"args":{...}} and reads {"id":N,"result":...} or
// {"id":N,"error":"..."}, one call in flight at a time. The answer to the
// request is the last line this process writes. Everything a worker prints
// goes to stderr: stdout carries the pipe alone.
//
// A worker module exports `worker(params)`. What it returns is its verdict,
// which the runner writes in the queue's own protocol: a `triage` becomes
// the claudinite-needs-human marker (and a failed run), a `requeue` the
// claudinite-requeue marker, a `requestAgent` the request file the executor
// handed in as CLAUDINITE_REQUEST_AGENT. A throw is the `<pack>/<task>
// failed` line plus the stack, and an error carrying `.triage` also prints
// the marker. The process ends by exitCode, never exit(), so nothing queued
// on a pipe is lost.

import { createInterface } from 'node:readline';
import { format } from 'node:util';
import { pathToFileURL } from 'node:url';
import { resolve } from 'node:path';
import { writeFileSync } from 'node:fs';

const PROTO = 'claudinite-tasks-v1';
const PIPE = Symbol.for('claudinite.sdk.pipe');

const write = (v) => process.stdout.write(`${JSON.stringify(v)}\n`);
for (const name of ['log', 'info', 'debug']) {
  console[name] = (...args) => { process.stderr.write(`${format(...args)}\n`); };
}

const requests = [];
const waiting = [];
const answers = new Map();
let closed = false;

const nextRequest = () => new Promise((ok, fail) => {
  if (requests.length) { ok(requests.shift()); return; }
  if (closed) { fail(new Error('the engine closed the pipe')); return; }
  waiting.push({ ok, fail });
});

createInterface({ input: process.stdin, crlfDelay: Infinity })
  .on('line', (line) => {
    let msg;
    try { msg = JSON.parse(line); } catch { return; }
    if (msg && typeof msg.id === 'number' && msg.op === undefined && msg.proto === undefined) {
      const pending = answers.get(msg.id);
      if (pending) { answers.delete(msg.id); pending(msg); }
      return;
    }
    const w = waiting.shift();
    if (w) w.ok(msg); else requests.push(msg);
  })
  .on('close', () => {
    closed = true;
    for (const w of waiting.splice(0)) w.fail(new Error('the engine closed the pipe'));
    for (const [id, pending] of answers) { answers.delete(id); pending({ id, error: 'the engine closed the pipe before answering' }); }
  });

let nextId = 0;
let inFlight = Promise.resolve();

function call(method, args, methods) {
  if (!methods.includes(method)) {
    return Promise.reject(new Error(`the engine does not answer ${method} (it offers ${methods.join(', ') || 'nothing'})`));
  }
  const run = () => new Promise((ok, fail) => {
    nextId += 1;
    const id = nextId;
    answers.set(id, (msg) => (msg.error != null ? fail(new Error(msg.error)) : ok(msg.result ?? null)));
    write({ sdk: method, id, args: args ?? {} });
  });
  const result = inFlight.then(run, run);
  inFlight = result.catch(() => {});
  return result;
}

// The parameters bag, from the CLAUDINITE_* environment the executor set.
// Absence stays absent: a target with no pull request reads null, never 0.
function paramsOf(env, request) {
  const text = (name) => (env[name] === undefined || env[name] === '' ? null : env[name]);
  const number = (name) => (text(name) === null ? null : Number(text(name)));
  const pack = text('CLAUDINITE_PACK');
  const task = text('CLAUDINITE_TASK');
  const item = { number: number('CLAUDINITE_ITEM') };
  const secrets = {};
  for (const name of request.secrets ?? []) {
    if (env[name] !== undefined) secrets[name] = env[name];
  }
  return Object.freeze({
    root: text('CLAUDINITE_REPO_ROOT'),
    repo: text('CLAUDINITE_REPO') ?? text('GITHUB_REPOSITORY'),
    defaultBranch: text('CLAUDINITE_DEFAULT_BRANCH'),
    pack,
    task,
    item,
    context: text('CLAUDINITE_CONTEXT') === null ? [] : text('CLAUDINITE_CONTEXT').split('\n'),
    target: {
      mode: text('CLAUDINITE_TARGET_MODE'),
      branch: text('CLAUDINITE_TARGET_BRANCH'),
      pr: number('CLAUDINITE_TARGET_PR'),
    },
    automerge: request.automerge ?? null,
    stepSummary: text('GITHUB_STEP_SUMMARY'),
    secrets,
    log: (s) => console.log(`${task ?? 'task'}${item.number ? ` [#${item.number}]` : ''}: ${s}`),
  });
}

function emitVerdict(verdict, env) {
  const out = verdict ?? {};
  if (out.triage) {
    const detail = out.triage.detail ? ` - ${out.triage.detail}` : '';
    console.log(`claudinite-needs-human: ${out.triage.kind}${detail}`);
  }
  if (out.requeue) {
    const reason = out.requeue.reason ? ` - ${out.requeue.reason}` : '';
    console.log(`claudinite-requeue: ${out.requeue.until}${reason}`);
  }
  if (out.requestAgent) {
    const path = env.CLAUDINITE_REQUEST_AGENT;
    if (!path) throw new Error('the worker requested the agent, but no CLAUDINITE_REQUEST_AGENT path was handed in');
    const { delivered = undefined, reason = undefined } = out.requestAgent === true ? {} : out.requestAgent;
    writeFileSync(path, JSON.stringify({ ...(delivered ? { delivered } : {}), ...(reason ? { reason } : {}) }));
  }
}

async function work(request) {
  const env = process.env;
  const id = `${env.CLAUDINITE_PACK ?? '?'}/${env.CLAUDINITE_TASK ?? '?'}`;
  const started = Date.now();
  const took = () => ((Date.now() - started) / 1000).toFixed(1);
  try {
    const mod = await import(pathToFileURL(resolve(process.cwd(), request.module)).href);
    if (typeof mod.worker !== 'function') {
      throw new Error(`${request.module} exports no \`worker\` function - the runner calls that export with the parameters bag`);
    }
    const verdict = await mod.worker(globalThis[PIPE].params);
    emitVerdict(verdict, env);
    console.log(`${id}: worker done in ${took()}s`);
    return { ok: !verdict?.triage };
  } catch (e) {
    if (e?.triage) console.error(`claudinite-needs-human: ${e.triage} - ${e.message}`);
    console.error(`${id} failed after ${took()}s: ${e?.message ?? e}`);
    if (e?.stack) console.error(e.stack);
    return { ok: false };
  }
}

// A task-local preconditions.mjs's terms, every reference the expression
// names answered in one call. A term that throws answers its own error;
// a module that does not load fails the whole request.
async function terms(request) {
  const mod = await import(pathToFileURL(resolve(process.cwd(), request.file)).href);
  const table = mod.terms ?? {};
  const outcomes = {};
  for (const ref of request.refs ?? []) {
    const term = Object.hasOwn(table, ref.name) ? table[ref.name] : null;
    if (!term || typeof term.holds !== 'function') {
      outcomes[ref.text] = { error: `${request.file} exports no term "${ref.name}"` };
      continue;
    }
    try {
      const out = await term.holds(request.signals ?? {}, {
        arg: ref.arg ?? undefined,
        config: request.config ?? {},
        item: request.item ?? null,
        windowDays: request.windowDays,
        now: request.now ? new Date(request.now) : null,
      }) ?? {};
      outcomes[ref.text] = {
        holds: out.holds === true,
        ...(out.reason ? { reason: String(out.reason) } : {}),
        ...(Array.isArray(out.context) ? { context: out.context.map(String) } : {}),
        ...(out.error ? { error: String(out.error) } : {}),
      };
    } catch (e) {
      outcomes[ref.text] = { error: `threw: ${e?.message ?? e}` };
    }
  }
  return { outcomes };
}

async function main() {
  const hello = await nextRequest();
  if (hello?.proto !== PROTO) {
    write({ error: `the runner speaks ${PROTO}, not ${hello?.proto}` });
    return 1;
  }
  const methods = Array.isArray(hello.methods) ? hello.methods.map(String) : [];
  write({ proto: PROTO });
  const request = await nextRequest();
  globalThis[PIPE] = Object.freeze({
    engine: String(hello.engine ?? ''),
    methods,
    params: paramsOf(process.env, request ?? {}),
    call: (method, args) => call(method, args, methods),
  });
  if (request?.op === 'work') {
    const answer = await work(request);
    write(answer);
    return answer.ok ? 0 : 1;
  }
  if (request?.op === 'terms') {
    try {
      write(await terms(request));
    } catch (e) {
      write({ error: `${request.file} did not load: ${e?.message ?? e}` });
    }
    return 0;
  }
  write({ error: `unknown op ${JSON.stringify(request?.op ?? null)}` });
  return 1;
}

main().then((code) => { process.exitCode = code; }, (e) => {
  console.error(e?.stack ?? e);
  process.exitCode = 1;
}).finally(() => { process.stdin.destroy(); });
