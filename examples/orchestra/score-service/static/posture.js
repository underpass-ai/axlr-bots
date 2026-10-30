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
