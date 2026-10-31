// The posture of one hand from its WebXR joints: how far each finger bends,
// where the palm faces, where the hand is from the head. Plain math, no
// browser: hands.js feeds it the joints of each frame, and node tests it
// (node --test posture.test.mjs).

export const FINGERS = {
  thumb: ["thumb-metacarpal", "thumb-phalanx-proximal", "thumb-phalanx-distal", "thumb-tip"],
  index: ["index-finger-metacarpal", "index-finger-phalanx-proximal", "index-finger-phalanx-intermediate", "index-finger-phalanx-distal", "index-finger-tip"],
  middle: ["middle-finger-metacarpal", "middle-finger-phalanx-proximal", "middle-finger-phalanx-intermediate", "middle-finger-phalanx-distal", "middle-finger-tip"],
  ring: ["ring-finger-metacarpal", "ring-finger-phalanx-proximal", "ring-finger-phalanx-intermediate", "ring-finger-phalanx-distal", "ring-finger-tip"],
  little: ["pinky-finger-metacarpal", "pinky-finger-phalanx-proximal", "pinky-finger-phalanx-intermediate", "pinky-finger-phalanx-distal", "pinky-finger-tip"],
};

const sub = (a, b) => [a[0] - b[0], a[1] - b[1], a[2] - b[2]];
const dot = (a, b) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2];
const cross = (a, b) => [a[1] * b[2] - a[2] * b[1], a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0]];
const norm = (a) => { const l = Math.hypot(...a) || 1; return [a[0] / l, a[1] / l, a[2] / l]; };

// The sum of the angles at a finger's joints, in degrees: 0 straight, 150 and
// more curled into the palm.
export function bend(points) {
  let total = 0;
  for (let i = 1; i < points.length - 1; i++) {
    const a = norm(sub(points[i], points[i - 1])), b = norm(sub(points[i + 1], points[i]));
    total += Math.acos(Math.max(-1, Math.min(1, dot(a, b)))) * 180 / Math.PI;
  }
  return total;
}

// The palm's normal, out of the palm. With the right hand up, palm away from
// you, the index is left of the little finger: index × little points away.
// The left hand is the mirror.
export function palmNormal(joints, handedness) {
  const wrist = joints["wrist"];
  const a = sub(joints["index-finger-phalanx-proximal"], wrist), b = sub(joints["pinky-finger-phalanx-proximal"], wrist);
  return norm(handedness === "left" ? cross(b, a) : cross(a, b));
}

// The head's horizontal frame: forward, right, up (world up is +y).
export function headFrame(head) {
  const f = norm([head.forward[0], 0, head.forward[2]]);
  return { forward: f, right: norm(cross(f, [0, 1, 0])), up: [0, 1, 0], position: head.position };
}

// One hand's measurements, as the service and Jev read them.
export function measure(joints, handedness, head) {
  const frame = headFrame(head);
  const fingers = {};
  for (const [name, list] of Object.entries(FINGERS)) fingers[name] = Math.round(bend(list.map((j) => joints[j])));
  const n = palmNormal(joints, handedness);
  const away = dot(n, frame.forward), floor = -n[1];
  const palm = { away: r2(away), toward: r2(-away), floor: r2(floor), up: r2(-floor), side: r2(Math.abs(dot(n, frame.right))) };
  const rel = sub(joints["middle-finger-metacarpal"], frame.position);
  const position = { side: r2(dot(rel, frame.right)), height: r2(rel[1]), ahead: r2(dot(rel, frame.forward)) };
  const spread = r2(Math.hypot(...sub(joints["index-finger-tip"], joints["pinky-finger-tip"])));
  return { fingers, palm, position, spread, normal: n.map(r2) };
}

const r2 = (x) => Math.round(x * 100) / 100;

// A coarse name for a posture, so that only a change is sent: each finger
// straight, half or curled, and where the palm mostly faces.
export function signature(m) {
  if (!m) return "-";
  const f = Object.values(m.fingers).map((d) => (d < 60 ? "s" : d < 150 ? "h" : "c")).join("");
  const top = Object.entries(m.palm).filter(([k]) => k !== "side").sort((a, b) => b[1] - a[1])[0];
  return f + "|" + (top[1] > 0.6 ? top[0] : "x");
}

// Sends a posture once it has held still for holdMs and differs from the last
// one sent. No hand tracked is never sent: there is nothing to read.
export class Steady {
  constructor(holdMs) { this.holdMs = holdMs; this.current = null; this.since = 0; this.sent = null; }
  update(sig, now) {
    if (sig !== this.current) { this.current = sig; this.since = now; return false; }
    if (sig === this.sent || sig === "-|-" || now - this.since < this.holdMs) return false;
    this.sent = sig;
    return true;
  }
  heldMs(now) { return Math.round(now - this.since); }
}

// The fingers of a signature: thumb, index, middle, ring, little; s straight,
// h half, c curled.
const fingersOf = (sig) => sig.split("|")[0];

// A flat hand: the four fingers straight (the thumb is free). Only such a
// hand's height conducts a group's gain.
export function flatHand(m) { return !!m && fingersOf(signature(m)).slice(1) === "ssss"; }

// A hand whose four fingers are not curled: straight or half bent. Its height
// is sent while it conducts a level, so a hand that relaxes a little does not
// count as gone; a fist does.
export function uncurled(m) { return !!m && !fingersOf(signature(m)).slice(1).includes("c"); }

// A pointing hand, a baton: the index straight, the other three not. It
// beats time; it gives no signal by itself.
export function pointing(m) {
  if (!m) return false;
  const f = fingersOf(signature(m));
  return f[1] === "s" && f[2] !== "s" && f[3] !== "s" && f[4] !== "s";
}

// The beats of a hand that beats time: an ictus is the hand's lowest point
// after a fast fall. Three even intervals in a row make a tempo; it is
// answered once, and again when it changes by three beats a minute or more.
// Heights in metres, times in seconds.
export class Beats {
  constructor({ fall = 0.6, minGap = 0.25, maxGap = 1.2, even = 0.15, low = 60, high = 200 } = {}) {
    Object.assign(this, { fall, minGap, maxGap, even, low, high });
    this.reset();
  }
  reset() { this.prev = null; this.falling = false; this.beats = []; this.bpm = null; }
  update(height, now) {
    let out = null;
    if (this.prev && now > this.prev.t) {
      const v = (height - this.prev.h) / (now - this.prev.t);
      if (v < -this.fall) this.falling = true;
      else if (this.falling && v > 0) {  // the lowest point is behind
        this.falling = false;
        const last = this.beats[this.beats.length - 1];
        if (last == null || now - last >= this.minGap) {
          if (last != null && now - last > this.maxGap) this.beats = [];
          this.beats.push(now);
          if (this.beats.length > 5) this.beats.shift();
          out = this.tempo();
        }
      }
    }
    this.prev = { h: height, t: now };
    return out;
  }
  tempo() {
    if (this.beats.length < 4) return null;
    const gaps = this.beats.slice(1).map((b, i) => b - this.beats[i]);
    const mean = gaps.reduce((a, b) => a + b, 0) / gaps.length;
    if (gaps.some((g) => Math.abs(g - mean) > this.even * mean)) return null;
    const bpm = Math.round(60 / mean);
    if (bpm < this.low || bpm > this.high) return null;
    if (this.bpm !== null && Math.abs(bpm - this.bpm) < 3) return null;
    this.bpm = bpm;
    return bpm;
  }
}

// The headset's pinch on one hand, frame by frame: a tap or a hold. A pinch
// is a hold only once it has lasted tapMs, however the hand moved before
// (the tracking jumps as the fingers meet); released earlier it is a tap.
// The hold starts from the hand's height when it begins, then moves every
// moveMs; a move more than glitchM from the last one sent is the tracking's,
// skipped, unless the next sample, moveMs later, agrees with it. update() answers null,
// "tap", "end", { start: height } or { move: height }. Times in ms, heights
// in metres (null: the hand is not tracked this frame).
export class Pinch {
  constructor({ tapMs = 350, moveMs = 100, glitchM = 0.15 } = {}) {
    Object.assign(this, { tapMs, moveMs, glitchM });
    this.p = null;
  }
  update(pressed, h, t) {
    const p = this.p;
    if (pressed && !p) { this.p = { t0: t, holding: false, sentAt: 0, last: null, skipped: null }; return null; }
    if (!pressed) {
      this.p = null;
      return !p ? null : p.holding ? "end" : t - p.t0 < this.tapMs ? "tap" : null;
    }
    if (h === null) return null;
    if (!p.holding) {
      if (t - p.t0 < this.tapMs) return null;
      Object.assign(p, { holding: true, sentAt: t, last: h });
      return { start: h };
    }
    if (t - p.sentAt <= this.moveMs) return null;
    if (Math.abs(h - p.last) > this.glitchM && (p.skipped === null || Math.abs(h - p.skipped) > this.glitchM)) { Object.assign(p, { sentAt: t, skipped: h }); return null; }
    Object.assign(p, { sentAt: t, last: h, skipped: null });
    return { move: h };
  }
}

// The instrument the head looks at, sticky: once one is looked at, another
// takes over only when the aim (degrees) sits margin degrees inside its slice
// of the arc, so a narrow slice does not flicker at its edges. No instrument
// there (the sky, behind) is null.
export function gazeOn(instruments, aim, current, margin = 4) {
  if (aim == null) return null;
  const at = instruments.find((i) => i.from <= aim && aim < i.to) || null;
  if (!at || !current || at.id === current) return at;
  return aim >= at.from + margin && aim < at.to - margin ? at : instruments.find((i) => i.id === current) || at;
}

// How a hand moves over the last seconds, summed up for Jev to read the
// conductor's expression (dolce, cantabile, brillante, appassionato): its
// speed, how far it rose and swept, how often it turned from rising to
// falling, how high it was, how open. Positions are the hand's from the head
// (side, height, ahead, metres), times in seconds; open is 0 (fingers curled)
// to 1 (straight). Nothing to do with what the hand holds: a baton, a flat
// hand and a fist move alike.
export class Motion {
  constructor({ window = 2.5, still = 0.05 } = {}) { this.window = window; this.still = still; this.samples = []; }
  reset() { this.samples = []; }
  update(position, open, now) {
    this.samples.push({ p: [position.side, position.height, position.ahead], open, t: now });
    while (this.samples.length && now - this.samples[0].t > this.window) this.samples.shift();
  }
  // null until the window holds 0.8 s of motion; moving says whether the hand moved at all.
  summary() {
    const s = this.samples;
    if (s.length < 4 || s[s.length - 1].t - s[0].t < 0.8) return null;
    const seconds = s[s.length - 1].t - s[0].t;
    let path = 0, peak = 0, turns = 0, wasRising = null;
    for (let i = 1; i < s.length; i++) {
      const dt = s[i].t - s[i - 1].t;
      if (dt <= 0) continue;
      const d = Math.hypot(...sub(s[i].p, s[i - 1].p));
      path += d;
      peak = Math.max(peak, d / dt);
      const vy = (s[i].p[1] - s[i - 1].p[1]) / dt;
      if (Math.abs(vy) > 0.15) {  // a clear rise or fall; a turn is a change between the two
        const rising = vy > 0;
        if (wasRising !== null && rising !== wasRising) turns++;
        wasRising = rising;
      }
    }
    const heights = s.map((x) => x.p[1]), sides = s.map((x) => x.p[0]);
    const speed = path / seconds;
    return {
      seconds: r2(seconds), speed: r2(speed), peak: r2(peak),
      rise: r2(Math.max(...heights) - Math.min(...heights)), sweep: r2(Math.max(...sides) - Math.min(...sides)),
      turns: r2(turns / seconds), height: r2(heights.reduce((a, b) => a + b, 0) / heights.length),
      open: r2(s.reduce((a, x) => a + x.open, 0) / s.length), moving: speed > this.still,
    };
  }
}

// How open a measured hand is: the four fingers' bend, 0° straight to 180° curled, as 1 to 0.
export function openness(m) {
  const f = m.fingers;
  return r2(Math.max(0, Math.min(1, 1 - (f.index + f.middle + f.ring + f.little) / 4 / 180)));
}
