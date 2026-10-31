// The rave's pattern evaluator: one process, one batch of requests on stdin,
// one JSON answer on stdout, and it exits. A bot's pattern is Strudel code
// (mini-notation in double quotes, the controls note/s/gain/lpf…, the pattern
// methods fast/slow/every/…); it is transpiled as the Strudel REPL does, run
// in a vm context that holds Strudel and nothing else (no process, no
// require, no fetch), under a wall-clock timeout, and queried for the cycles
// asked for: the events (haps) with an onset in them, their value as Strudel
// built it. The service does the rest: it knows which sounds exist and how
// many events a bar may hold, and it kills this process if it does not
// answer in time (a pattern like "bd*1e9" dies here, not in the service).
//
// A vm context is not a security boundary; the bots are the orchestra's own.
// The source is also refused when it names what a pattern never needs.
//
//   echo '{"requests":[{"id":"k","code":"s(\"bd*4\")","from":0,"to":1}]}' | node --import ./register.mjs evaluate.mjs
//
// Each result: {id, ok, haps: [{b, e, v}], sounds, perCycle} or {id, ok: false, error}.
// b and e are the event's whole span in cycles (a cycle is one bar); v is its
// value with note names already turned into MIDI numbers (noteMidi).

import vm from "node:vm";

// Strudel greets on import (console.log, console.warn); stdout is the answer and nothing else.
const say = { log: console.log, warn: console.warn };
console.log = console.warn = () => {};
const core = await import("@strudel/core");
const { mini } = await import("@strudel/mini");
const { transpiler } = await import("@strudel/transpiler");
console.log = say.log;
console.warn = say.warn;

const FORBIDDEN = /\b(import|require|process|globalThis|global|window|this|eval|Function|constructor|prototype|__proto__|while|for|do|new|class|async|await|yield|with|throw|try|catch|fetch|setTimeout|setInterval|console)\b|`|\[\s*["']/;
const MAX_CODE = 2000;
const TIMEOUT_MS = 1500;
// The transpiler turns every "…" into m('…', offset): the offset is for the REPL's highlighting, never a second pattern.
const m = (code, _offset) => mini(code);
const scope = Object.assign(Object.create(null), core, { mini, m });
delete scope.evalScope;
delete scope.evaluate;

function plain(value) {
  // Strudel values may hold Fractions and functions: numbers and plain data only.
  if (value === null || value === undefined) return value;
  if (typeof value === "function") return undefined;
  if (typeof value === "object") {
    if (typeof value.valueOf === "function" && typeof value.valueOf() === "number") return value.valueOf();
    if (Array.isArray(value)) return value.map(plain);
    const out = {};
    for (const [k, v] of Object.entries(value)) {
      const p = plain(v);
      if (p !== undefined) out[k] = p;
    }
    return out;
  }
  return value;
}

function valueOf(hap) {
  let v = hap.value;
  if (typeof v === "string") v = { s: v };
  else if (typeof v === "number") v = { note: v };
  else if (typeof v !== "object" || v === null) v = {};
  v = plain(v);
  if (typeof v.note === "string") {
    const midi = core.noteToMidi(v.note);
    if (Number.isFinite(midi)) v.noteMidi = midi;
  } else if (typeof v.note === "number") {
    v.noteMidi = v.note;
  }
  return v;
}

export function evaluate(request) {
  const { id, code, from = 0, to = 1, maxHaps = 4096 } = request;
  if (typeof code !== "string" || !code.trim()) return { id, ok: false, error: "no pattern" };
  if (code.length > MAX_CODE) return { id, ok: false, error: `pattern longer than ${MAX_CODE} characters` };
  const hit = FORBIDDEN.exec(code);
  if (hit) return { id, ok: false, error: `a pattern never needs ${JSON.stringify(hit[0])}` };
  let body;
  try {
    body = transpiler(code, { wrapAsync: false, addReturn: true, emitMiniLocations: false }).output;
  } catch (e) {
    return { id, ok: false, error: `does not parse: ${String(e.message || e).split("\n")[0]}` };
  }
  const context = vm.createContext(Object.assign(Object.create(null), scope, { __from: from, __to: to }));
  const program = `(() => { const __p = (() => { ${body}\n })(); if (!__p || typeof __p.queryArc !== "function") throw new Error("not a pattern"); return __p.queryArc(__from, __to); })()`;
  let haps;
  try {
    haps = vm.runInContext(program, context, { timeout: TIMEOUT_MS });
  } catch (e) {
    const msg = String(e && e.message || e).split("\n")[0];
    return { id, ok: false, error: /timed out/i.test(msg) ? `took longer than ${TIMEOUT_MS} ms` : msg };
  }
  if (!Array.isArray(haps)) return { id, ok: false, error: "not a pattern" };
  const out = [];
  for (const h of haps) {
    if (!h.whole || typeof h.hasOnset !== "function" || !h.hasOnset()) continue;
    out.push({ b: h.whole.begin.valueOf(), e: h.whole.end.valueOf(), v: valueOf(h) });
    if (out.length > maxHaps) return { id, ok: false, error: `more than ${maxHaps} events in ${to - from} bars` };
  }
  out.sort((a, b) => a.b - b.b);
  const sounds = [...new Set(out.map((h) => h.v.s).filter((s) => typeof s === "string"))];
  return { id, ok: true, haps: out, sounds, perCycle: to > from ? out.length / (to - from) : out.length };
}

async function main() {
  const chunks = [];
  for await (const c of process.stdin) chunks.push(c);
  let input;
  try {
    input = JSON.parse(Buffer.concat(chunks).toString("utf8"));
  } catch (e) {
    process.stdout.write(JSON.stringify({ error: "stdin is not JSON" }));
    process.exitCode = 2;
    return;
  }
  const requests = Array.isArray(input) ? input : Array.isArray(input.requests) ? input.requests : [input];
  process.stdout.write(JSON.stringify({ results: requests.map(evaluate) }));
}

if (process.argv[1] && import.meta.url.endsWith(process.argv[1].split("/").pop())) {
  await main();
}
