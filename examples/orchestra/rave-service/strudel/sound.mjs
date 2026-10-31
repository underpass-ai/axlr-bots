// The rave's sound: superdough, Strudel's synthesizer, headless. One process;
// an AudioContext with no sound card (sink "none", which runs at the speed of
// the wall clock); a tap at its output that writes every block of audio to
// stdout as 16-bit stereo PCM, so the service streams it and the frames it
// has read are its clock; hits to play come in on stdin, one JSON a line,
// at a time on this context's clock. Nothing but PCM goes to stdout after
// the READY line; the log goes to stderr.
//
//   node --import ./register.mjs sound.mjs [--rate 44100] [--block 2048] [--master 0.6] [--cache DIR] [--no-network]
//
// stdout:  READY {"rate","block","channels","header":8,"sounds":[…],"samples":"loaded"|"none"}\n  then, per block,
//          8 bytes (float64 LE: the context's currentTime when the block was written) and the PCM
// stdin:   {"hit": {s, note, gain, …}, "at": seconds, "dur": seconds, "cps": 0.53, "cycle": 12}
//          {"mute": true|false}   the output gain to 0 (or back to 1) in 5 ms: stop.all, and the next take
//          {"master": 0.8}        the output gain
//          {"quit": true}
//
// The drum samples are Strudel's defaults (github:tidalcycles/dirt-samples):
// their index and the samples used are fetched once and kept in --cache, so
// the second run needs no network; without network and without a cache the
// synthesized sounds (sawtooth, supersaw, sbd, white…) still play and the
// samples are reported as "none".

import { mkdirSync, existsSync, readFileSync, writeFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { createInterface } from "node:readline";
import os from "node:os";
import path from "node:path";

const args = Object.fromEntries(process.argv.slice(2).map((a, i, all) => a.startsWith("--") ? [a.slice(2), all[i + 1] && !all[i + 1].startsWith("--") ? all[i + 1] : true] : []).filter(Boolean));
const RATE = Number(args.rate || 44100), BLOCK = Number(args.block || 2048);
const CACHE = args.cache || path.join(os.homedir(), ".cache", "rave-service", "samples");
const NETWORK = !args["no-network"];
const log = (...a) => process.stderr.write(a.join(" ") + "\n");

// Strudel's drum machines and the dirt samples the rave uses; the synths superdough makes itself.
export const SAMPLES = ["bd", "sd", "hh", "cp", "cr", "rs", "cb", "click", "808bd", "808sd", "808hc", "808oh", "808cy", "909",
  "techno", "tech", "perc", "lt", "mt", "ht", "industrial", "hoover", "hardkick", "clubkick", "house", "noise"];
export const SYNTHS = ["sawtooth", "square", "triangle", "sine", "supersaw", "pulse", "white", "pink", "brown", "sbd", "saw", "sqr", "tri", "sin"];

// --- the sample cache: fetch goes to disk first ---------------------------------
const realFetch = globalThis.fetch;
mkdirSync(CACHE, { recursive: true });
let fetched = 0, cached = 0;
globalThis.fetch = async (url, ...rest) => {
  const file = path.join(CACHE, createHash("sha1").update(String(url)).digest("hex"));
  if (existsSync(file)) { cached++; return new Response(readFileSync(file)); }
  if (!NETWORK) throw new Error(`no network, not cached: ${url}`);
  const r = await realFetch(url, ...rest);
  if (!r.ok) throw new Error(`${r.status} for ${url}`);
  const body = Buffer.from(await r.arrayBuffer());
  writeFileSync(file, body);
  fetched++;
  return new Response(body);
};

// --- superdough on a WebAudio polyfill, in this order (see the spike notes in the README) -----
await import("node-web-audio-api/polyfill.js");
const { AudioContext } = await import("node-web-audio-api");
globalThis.window.addEventListener ??= () => {};
globalThis.window.postMessage ??= () => {};
const sayLog = console.log;
console.log = (...a) => log(...a);  // superdough talks on console.log; stdout is the sound
const sd = await import("superdough");

const ctx = new AudioContext({ sinkId: { type: "none" }, latencyHint: "playback", sampleRate: RATE });
sd.setAudioContext(ctx);
await sd.initAudio();
sd.registerSynthSounds();
let samples = "none";
try {
  await sd.samples("github:tidalcycles/dirt-samples");
  samples = "loaded";
} catch (e) {
  log(`samples: ${e.message}; synthesized sounds only`);
}

// The output: superdough's bus → the rave's gain (mute) → the tap → the silent sink.
const ctrl = sd.getSuperdoughAudioController();
const out = ctx.createGain();
out.gain.value = 1;
const tap = ctx.createScriptProcessor(BLOCK, 2, 2);
ctrl.output.destinationGain.connect(out);
out.connect(tap);
const pcm = new Int16Array(BLOCK * 2);
let ready = false;  // the graph pulls the tap as soon as it is wired; nothing goes out before READY
tap.onaudioprocess = (e) => {
  if (!ready) return;
  const L = e.inputBuffer.getChannelData(0), R = e.inputBuffer.getChannelData(1);
  for (let i = 0; i < BLOCK; i++) {
    pcm[2 * i] = Math.max(-32768, Math.min(32767, Math.round(L[i] * 32767)));
    pcm[2 * i + 1] = Math.max(-32768, Math.min(32767, Math.round(R[i] * 32767)));
  }
  const out = Buffer.alloc(8 + pcm.byteLength);
  out.writeDoubleLE(ctx.currentTime, 0);  // the service schedules on this clock: the header says where it is
  Buffer.from(pcm.buffer).copy(out, 8);
  process.stdout.write(out);
};

// Warm the samples the rave names (their first variant) while the output is muted, so the
// first hit of a take is not late (a sample is fetched and decoded on its first use).
if (samples === "loaded") {
  out.gain.value = 0;
  for (const s of SAMPLES) {
    try { await sd.superdough({ s, gain: 0.01 }, ctx.currentTime + 0.3, 0.01); } catch (e) { log(`warm ${s}: ${e.message}`); }
  }
  await new Promise((r) => setTimeout(r, 400));
}
out.gain.value = 0;  // silent until the service's first take: {"mute": false} opens the output
log(`sound: ${RATE} Hz, blocks of ${BLOCK}, samples ${samples} (${fetched} fetched, ${cached} from the cache at ${CACHE})`);
// READY first, then the PCM.
process.stdout.write(`READY ${JSON.stringify({ rate: ctx.sampleRate, block: BLOCK, channels: 2, header: 8, sounds: [...(samples === "loaded" ? SAMPLES : []), ...SYNTHS], samples })}\n`);
tap.connect(ctx.destination);
ready = true;

// --- the hits -------------------------------------------------------------------
let late = 0, lateBy = 0, hits = 0, margin = Infinity, master = Number(args.master || 1);
const lines = createInterface({ input: process.stdin });
lines.on("line", async (line) => {
  let msg;
  try { msg = JSON.parse(line); } catch (e) { return log(`not JSON: ${line.slice(0, 80)}`); }
  if (msg.quit) { tap.disconnect(); await ctx.close(); process.exit(0); }
  if ("mute" in msg) {
    const now = ctx.currentTime;
    out.gain.cancelScheduledValues(now);
    out.gain.setValueAtTime(out.gain.value, now);
    out.gain.linearRampToValueAtTime(msg.mute ? 0 : master, now + 0.005);
    return;
  }
  if ("master" in msg) { master = Number(msg.master); out.gain.setTargetAtTime(master, ctx.currentTime, 0.02); return; }
  if (msg.hit) {
    let at = Number(msg.at);
    const floor = ctx.currentTime + 0.01;
    if (!(at >= floor)) { late++; lateBy += floor - at; at = floor; }
    hits++;
    margin = Math.min(margin, at - ctx.currentTime);
    try {
      await sd.superdough(msg.hit, at, Number(msg.dur) || 0.25, Number(msg.cps) || 0.5, Number(msg.cycle) || 0);
    } catch (e) {
      log(`hit ${JSON.stringify(msg.hit).slice(0, 80)}: ${e.message}`);
    }
    if (hits % 500 === 0) { log(`${hits} hits, ${late} late (by ${late ? (lateBy / late).toFixed(3) : 0} s on average), the tightest margin ${margin.toFixed(3)} s`); margin = Infinity; }
  }
});
lines.on("close", async () => { tap.disconnect(); await ctx.close(); process.exit(0); });
