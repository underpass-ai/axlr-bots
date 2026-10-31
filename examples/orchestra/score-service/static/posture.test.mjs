// node --test posture.test.mjs: the posture math on hands built by hand.
import assert from "node:assert/strict";
import test from "node:test";
import { bend, Beats, FINGERS, flatHand, measure, palmNormal, pointing, signature, Steady } from "./posture.js";

// A right hand in front of the head, fingers up (+y), palm away from the
// conductor (-z), thumb on the left (-x). curl bends every finger joint by
// that many degrees, toward the palm.
function hand(handedness, curl = 0, palmDown = false) {
  const mirror = handedness === "left" ? -1 : 1;
  const at = 0.25 * mirror;  // the right hand right of the head, the left hand left
  const joints = { wrist: [at, -0.3, -0.4] };
  const xs = { thumb: -0.04, index: -0.025, middle: -0.005, ring: 0.015, little: 0.03 };
  for (const [name, list] of Object.entries(FINGERS)) {
    let p = [at + mirror * xs[name], -0.3, -0.4], dir = [0, 1, 0];
    list.forEach((joint, i) => {
      joints[joint] = p.slice();
      if (i >= 1 && name !== "thumb") {
        const a = (curl * Math.PI) / 180;  // bend toward the palm: from +y toward -z
        dir = [0, dir[1] * Math.cos(a) + dir[2] * Math.sin(a), -dir[1] * Math.sin(a) + dir[2] * Math.cos(a)];
      }
      p = [p[0] + dir[0] * 0.03, p[1] + dir[1] * 0.03, p[2] + dir[2] * 0.03];
    });
  }
  if (palmDown) {  // turn the hand about x by -90° at the wrist: (x, y, z) → (x, z, -y), fingers forward, palm to the floor
    for (const k of Object.keys(joints)) {
      const [x, y, z] = joints[k].map((v, i) => v - [0, -0.3, -0.4][i]);
      joints[k] = [x, z - 0.3, -y - 0.4];
    }
  }
  return joints;
}
const HEAD = { position: [0, 0, 0], forward: [0, 0, -1] };

test("a straight finger bends 0°, a curled one far more", () => {
  assert.equal(Math.round(bend([[0, 0, 0], [0, 1, 0], [0, 2, 0]])), 0);
  assert.equal(Math.round(bend([[0, 0, 0], [0, 1, 0], [1, 1, 0]])), 90);
  const fist = measure(hand("right", 70), "right", HEAD);
  assert.ok(fist.fingers.index >= 150, `index ${fist.fingers.index}`);
  const open = measure(hand("right", 0), "right", HEAD);
  assert.ok(open.fingers.index < 20);
});

test("the open right hand's palm faces away, and so does the left's", () => {
  for (const side of ["right", "left"]) {
    const m = measure(hand(side), side, HEAD);
    assert.ok(m.palm.away > 0.9, `${side} away ${m.palm.away}`);
    assert.equal(signature(m), "sssss|away");
  }
  assert.deepEqual(palmNormal(hand("right"), "right").map(Math.round), [0, 0, -1]);
});

test("a palm turned down faces the floor", () => {
  const m = measure(hand("right", 0, true), "right", HEAD);
  assert.ok(m.palm.floor > 0.9, `floor ${m.palm.floor}`);
  assert.equal(signature(m).split("|")[1], "floor");
});

test("position: left of the head, below the eyes, in front", () => {
  const m = measure(hand("left"), "left", HEAD);
  assert.ok(m.position.side < 0 && m.position.height < 0 && m.position.ahead > 0.3);
});

test("a posture is sent once it holds still, once", () => {
  const s = new Steady(300);
  assert.equal(s.update("sssss|away", 0), false);
  assert.equal(s.update("sssss|away", 200), false);
  assert.equal(s.update("sssss|away", 320), true);
  assert.equal(s.update("sssss|away", 900), false);
  assert.equal(s.update("ccccc|x", 1000), false);
  assert.equal(s.update("ccccc|x", 1400), true);
  assert.equal(s.update("-|-", 1500), false);
  assert.equal(s.update("-|-", 2500), false);
});

test("a flat hand has its four fingers straight; a pointing hand only the index", () => {
  const open = hand("right"), fist = hand("right", 70);
  assert.ok(flatHand(measure(open, "right", HEAD)));
  assert.ok(!flatHand(measure(fist, "right", HEAD)));
  const baton = { ...fist };
  for (const joint of FINGERS.index) baton[joint] = open[joint];
  const m = measure(baton, "right", HEAD);
  assert.ok(pointing(m), signature(m));
  assert.ok(!flatHand(m));
  assert.ok(!pointing(measure(open, "right", HEAD)));
  assert.ok(!flatHand(null) && !pointing(null));
});

test("beats: three even falls and rises make a tempo; a still hand none; a change of three or more is answered again", () => {
  const beats = new Beats();
  const at = (bpm, seconds, from = 0) => {  // the hand bobbing: lowest at each beat
    const out = [];
    for (let t = from; t < from + seconds; t += 1 / 72) {
      const bpmNow = beats.update(-0.3 + 0.12 * Math.cos((t * bpm / 60) * Math.PI * 2 + Math.PI), t);
      if (bpmNow) out.push(bpmNow);
    }
    return out;
  };
  const found = at(150, 3);
  assert.equal(found.length, 1, `answered ${found}`);
  assert.ok(Math.abs(found[0] - 150) <= 2, `bpm ${found[0]}`);
  assert.deepEqual(at(150, 3, 3), []);  // the same tempo again is not news
  const faster = at(170, 4, 6);
  assert.ok(faster.length >= 1 && Math.abs(faster[faster.length - 1] - 170) <= 3, `bpm ${faster}`);
  const still = new Beats();
  for (let t = 0; t < 3; t += 1 / 72) assert.equal(still.update(-0.3 + 0.002 * Math.sin(t * 9), t), null);
  const slow = new Beats();  // 40 bpm is below the range
  let answered = null;
  for (let t = 0; t < 8; t += 1 / 72) answered = slow.update(-0.3 + 0.2 * Math.cos((t * 40 / 60) * Math.PI * 2 + Math.PI), t) || answered;
  assert.equal(answered, null);
});

// --- the motion sums for the expression ---------------------------------------
import { Motion, openness } from "./posture.js";

// A hand moving for seconds: height a sine of amplitude a at f Hz, drifting sideways; sampled at 72 Hz like the headset.
function moved(motion, { a, f, side = 0, open = 0.9, seconds = 2.4, h0 = -0.2 }) {
  for (let t = 0; t <= seconds; t += 1 / 72) motion.update({ side: side * Math.sin(2 * Math.PI * f * t / 2), height: h0 + a * Math.sin(2 * Math.PI * f * t), ahead: 0.4 }, open, t);
  return motion.summary();
}

test("nothing to sum before 0.8 s of samples", () => {
  const m = new Motion();
  for (let t = 0; t < 0.5; t += 1 / 72) m.update({ side: 0, height: -0.2, ahead: 0.4 }, 0.9, t);
  assert.equal(m.summary(), null);
});

test("a slow small sweep and a fast big beat sum up apart", () => {
  const slow = moved(new Motion(), { a: 0.04, f: 0.4, side: 0.05 });
  const fast = moved(new Motion(), { a: 0.25, f: 2.0, side: 0.2, open: 0.3, h0: 0.1 });
  assert.ok(slow.speed < 0.2 && fast.speed > 1.0, `speeds ${slow.speed} ${fast.speed}`);
  assert.ok(slow.rise < 0.1 && fast.rise > 0.45, `rises ${slow.rise} ${fast.rise}`);
  assert.ok(slow.turns < 1 && fast.turns >= 3, `turns ${slow.turns} ${fast.turns}`);
  assert.ok(slow.height < 0 && fast.height > 0, `heights ${slow.height} ${fast.height}`);
  assert.ok(slow.open > fast.open);
  assert.ok(slow.moving && fast.moving);
  assert.ok(Math.abs(slow.seconds - 2.4) < 0.05);
});

test("a hand at rest is not moving, and the window forgets what is older than it", () => {
  const m = new Motion({ window: 1.0 });
  moved(m, { a: 0.25, f: 2.0, seconds: 2.0 });
  for (let t = 2.0; t <= 3.5; t += 1 / 72) m.update({ side: 0, height: -0.2, ahead: 0.4 }, 0.9, t);
  const sum = m.summary();
  assert.equal(sum.moving, false);
  assert.ok(sum.speed < 0.05 && sum.rise < 0.01, `still: ${JSON.stringify(sum)}`);
});

test("openness: straight fingers 1, a fist about 0", () => {
  assert.equal(openness({ fingers: { thumb: 30, index: 0, middle: 0, ring: 0, little: 0 } }), 1);
  assert.ok(openness({ fingers: { thumb: 90, index: 195, middle: 210, ring: 205, little: 200 } }) < 0.05);
});
