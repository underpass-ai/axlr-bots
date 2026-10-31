import { test } from "node:test";
import assert from "node:assert/strict";
import { evaluate } from "./evaluate.mjs";

test("a stack in one string: kick and hats together", () => {
  const r = evaluate({ id: "s", code: 's("bd*4, [~ hh]*4")', from: 0, to: 1 });
  assert.equal(r.ok, true);
  assert.deepEqual(r.sounds.sort(), ["bd", "hh"]);
  assert.deepEqual(r.haps.filter((h) => h.v.s === "bd").map((h) => h.b), [0, 0.25, 0.5, 0.75]);
  assert.deepEqual(r.haps.filter((h) => h.v.s === "hh").map((h) => h.b), [0.125, 0.375, 0.625, 0.875]);
});

test("a mini-notation kick: four onsets a bar, bd", () => {
  const r = evaluate({ id: "k", code: 's("bd*4")', from: 0, to: 2 });
  assert.equal(r.ok, true);
  assert.equal(r.haps.length, 8);
  assert.deepEqual(r.sounds, ["bd"]);
  assert.equal(r.perCycle, 4);
  assert.deepEqual(r.haps[1], { b: 0.25, e: 0.5, v: { s: "bd" } });
});

test("notes become MIDI numbers; lpf is Strudel's cutoff", () => {
  const r = evaluate({ id: "b", code: 'note("c2 [~ c2] g1 bb1").s("sawtooth").lpf(600).gain(0.9)', from: 0, to: 1 });
  assert.equal(r.ok, true);
  assert.equal(r.haps.length, 4);
  assert.equal(r.haps[0].v.noteMidi, 36);
  assert.equal(r.haps[0].v.cutoff, 600);
  assert.equal(r.haps[3].v.note, "bb1");
  assert.equal(r.haps[3].v.noteMidi, 34);
});

test("alternation follows the absolute cycle, so a swap keeps <a b> in step", () => {
  const r = evaluate({ id: "a", code: 's("<bd sd>")', from: 7, to: 9 });
  assert.deepEqual(r.haps.map((h) => [h.b, h.v.s]), [[7, "sd"], [8, "bd"]]);
});

test("every and arrow functions work; a pattern that is not a pattern is refused", () => {
  const e = evaluate({ id: "e", code: 's("hh*8").every(2, x => x.fast(2))', from: 0, to: 2 });
  assert.equal(e.ok, true);
  assert.ok(e.haps.length > 16, String(e.haps.length));
  assert.match(evaluate({ id: "n", code: "1 + 1" }).error, /not a pattern/);
  assert.match(evaluate({ id: "p", code: 's("bd*4"' }).error, /does not parse/);
  assert.match(evaluate({ id: "z", code: "" }).error, /no pattern/);
});

test("what a pattern never needs is refused before it runs", () => {
  for (const code of ['process.exit(1)', 'require("fs")', 'while(true){}', 'globalThis.x', '`bd`', 'this.constructor', 's("bd")["constructor"]']) {
    const r = evaluate({ id: "x", code });
    assert.equal(r.ok, false, code);
    assert.match(r.error, /never needs/, code);
  }
});

test("too many events is refused, with the count", () => {
  const r = evaluate({ id: "m", code: 's("bd*2000")', from: 0, to: 1, maxHaps: 256 });
  assert.equal(r.ok, false);
  assert.match(r.error, /more than 256 events/);
});

test("the vm holds Strudel and nothing of Node", () => {
  const r = evaluate({ id: "v", code: "s(typeof setImmediate)" });
  // typeof an unknown name is "undefined": the pattern is the string "undefined"
  assert.equal(r.ok, true);
  assert.deepEqual(r.sounds, ["undefined"]);
});
