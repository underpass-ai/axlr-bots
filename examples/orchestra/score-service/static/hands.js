// The conductor's page: WebXR hand input → postures → POST /api/hands; the
// orchestra in the room (room.js), lit by the service's state; and one
// floating panel that says what each hand holds, what Jev read and what the
// service sent. See hands.html.
import { measure, signature, Steady, Beats, flatHand, pointing } from "/static/posture.js";
import { Hall, THREE } from "/static/room.js";
import { meadow, drawnHands } from "/static/meadow.js";
import { zen } from "/static/zen.js";
import { aurora } from "/static/aurora.js";
import { rave } from "/static/rave.js";

const $ = (id) => document.getElementById(id);
const panel = $("panel"), ctx = panel.getContext("2d");
const HOLD_MS = 300, SIDES = ["left", "right"];
const GROUP = { left: "strings", right: "winds" };
const steady = new Steady(HOLD_MS);
let live = { left: null, right: null }, last = null, orchestra = null, xr = null, dirty = true;
let soundOn = false, soundAt = -1, soundState = "off", heightAt = 0;
const SECTIONS = { strings: ["vn1", "vn2", "va", "vc", "cb"], winds: ["ob", "hn"] };
let gains = { strings: 1, winds: 1 }, instruments = [];
// The instruments and their slices of the arc, as the service has them.
fetch("/api/hands").then((r) => r.json()).then((v) => {
  instruments = v.instruments || [];
  for (const i of instruments) { SECTIONS[i.id] = i.sections; gains[i.id] = 1; }
}).catch(() => {});
const instrumentAt = (aim) => (aim == null ? null : instruments.find((i) => i.from <= aim && aim < i.to) || null);
const params = new URLSearchParams(location.search), preview = params.get("view") === "1";
if (params.get("sim") === "1") document.body.classList.add("simulate");
if (params.get("world")) $("world").value = params.get("world");  // ?world=zen|meadow|room picks what is around the orchestra

// --- the panel ------------------------------------------------------------

function draw() {
  const w = panel.width, h = panel.height, ink = "#F4F4F2", muted = "#A5A5A0", copper = "#E08A5A", red = "#F07B6B";
  ctx.fillStyle = "rgba(15,15,16,0.88)";
  ctx.fillRect(0, 0, w, h);
  ctx.textBaseline = "top";
  ctx.font = "600 22px Lexend, sans-serif";
  ctx.fillStyle = red;
  ctx.fillText("SIMULATION", 24, 18);
  ctx.fillStyle = muted;
  const o = orchestra;
  ctx.fillText((o ? `${o.status}${o.status === "playing" ? ` · bar ${o.bar}${o.countIn ? ` · count-in ${o.countIn}` : ""} · ${o.playing}/36 playing` : ""}` : "orchestra: …") + ` · sound ${soundState} · ${link}`, 210, 18);
  SIDES.forEach((side, i) => {
    const x = 24 + i * 500, read = last?.read?.[side], jev = last?.jev?.answers?.[side], m = live[side];
    ctx.fillStyle = muted;
    ctx.font = "600 24px Lexend, sans-serif";
    const tgt = target(), group = tgt ? tgt.id : GROUP[side];
    ctx.fillText(`${side.toUpperCase()} HAND · ${tgt ? tgt.name : GROUP[side]} · ${Math.round(100 * (gains[group] ?? 1))}%`, x, 70);
    ctx.font = "600 64px Lexend, sans-serif";
    ctx.fillStyle = read && read !== "nothing" ? copper : ink;
    ctx.fillText(read ? read.toUpperCase() : "—", x, 104);
    ctx.font = "24px Lexend, sans-serif";
    ctx.fillStyle = ink;
    if (jev) ctx.fillText(`Jev: ${jev.choice} ${jev.probabilities[jev.choice].toFixed(2)}`, x, 184);
    ctx.fillStyle = muted;
    if (m) {
      const f = m.fingers;
      ctx.fillText(`now: T${f.thumb} I${f.index} M${f.middle} R${f.ring} L${f.little}  ${signature(m).split("|")[1]}`, x, 222);
      ctx.fillText(`palm n ${m.normal.join(" ")}`, x, 256);
    } else ctx.fillText("not tracked", x, 222);
  });
  ctx.font = "600 26px Lexend, sans-serif";
  const tgt = target();
  ctx.fillStyle = tgt ? copper : muted;
  ctx.fillText(tgt ? `CONDUCTING ${tgt.name.toUpperCase()}${sel.locked ? " · LOCKED (pinch to free)" : " · as you look (pinch to lock)"}` : "CONDUCTING strings + winds · look at a section", 24, 290);
  ctx.font = "26px Lexend, sans-serif";
  let y = 326;
  if (last?.applied) {
    ctx.fillStyle = last.applied.startsWith("fading") ? red : copper;
    ctx.fillText(`done: ${last.applied}`, 24, y);
    y += 36;
  }
  for (const e of last?.sent || []) {
    if (y > h - 130) break;
    ctx.fillStyle = copper;
    ctx.fillText(`sent ${e.name}${e.data?.dynamics ? " " + e.data.dynamics : ""}${e.to ? ` → ${e.to.length} chairs` : ""}${e.note ? " (dry run)" : ""}${e.error ? " FAILED" : ""}`, 24, y);
    y += 36;
  }
  for (const e of last?.notSent || []) {
    if (y > h - 130) break;
    ctx.fillStyle = muted;
    ctx.fillText(`not sent: ${e.why}`, 24, y);
    y += 36;
  }
  ctx.fillStyle = muted;
  ctx.font = "20px Lexend, sans-serif";
  const pinch = SIDES.filter((s) => quest.pinch[s]).map((s) => s[0].toUpperCase()).join("") || "–";
  const seen = quest.events.map((e) => `${e.what} ${((performance.now() - e.at) / 1000).toFixed(1)} s`).join(" · ");
  ctx.fillText(`headset${quest.browser ? ` Browser ${quest.browser}` : ""}: ${quest.profiles === null ? "no hand yet" : `${quest.profiles[0] || "?"} · ${quest.buttons} buttons${quest.buttons >= 10 ? " · microgestures" : ""}`} · pinch ${pinch} · looking at ${quest.gaze ? quest.gaze.name : "–"}`, 24, h - 92);
  ctx.fillStyle = copper;
  ctx.fillText(seen ? `headset recognised: ${seen}` : "headset recognised: nothing yet (pinch, thumb tap, thumb swipes: shown, not acted on)", 24, h - 64);
  ctx.fillStyle = muted;
  if (last?.jev?.ms) ctx.fillText(`Jev ${last.jev.ms} ms${roundTrip !== null ? ` · round trip ${roundTrip} ms` : ""}${last.simulated ? " · simulated posture" : ""}`, 24, h - 36);
  dirty = true;
}

// --- the service ------------------------------------------------------------

// The link to the service: one WebSocket, postures up, the state and Jev's
// readings down as soon as they change. While it is down: POST and long poll.
let sock = null, link = "http", roundTrip = null, nextId = 1;
const sentAt = {};
function connect() {
  const s = new WebSocket(`${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/ws/hands`);
  s.onopen = () => { sock = s; link = "websocket"; draw(); };
  s.onmessage = (m) => {
    const msg = JSON.parse(m.data);
    if (msg.type === "state") apply(msg.state);
    if (msg.type === "hands" || msg.type === "native") {
      if (sentAt[msg.id]) { roundTrip = Math.round(performance.now() - sentAt[msg.id]); delete sentAt[msg.id]; }
      heard(msg.record);
    }
  };
  s.onclose = () => { if (sock === s) { sock = null; link = "http"; draw(); } setTimeout(connect, 1000); };
}

async function send(hands, heldMs, simulated = false) {
  if (sock) {
    const id = nextId++;
    sentAt[id] = performance.now();
    sock.send(JSON.stringify({ type: "posture", id, hands, heldMs, simulated, target: sel.id, locked: sel.locked }));
    return;
  }
  const t0 = performance.now();
  try {
    const r = await fetch("/api/hands", { method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ hands, heldMs, simulated, target: sel.id, locked: sel.locked }) });
    const record = await r.json();
    roundTrip = Math.round(performance.now() - t0);
    heard(record);
  } catch (e) {
    last = { notSent: [{ why: `the service did not answer: ${e}` }] };
    draw();
  }
}

// The service answers at once, before the events reach the bus: what it sent
// runs across the floor now; a second later the answer is read again for the
// events' ids or a failure.
const waved = {};
function heard(record) {
  last = record;
  for (const e of last.sent || []) { hall.wave(e.name.startsWith("stop.")); waved[e.name] = performance.now(); }
  if ((last.applied || "").startsWith("fading")) hall.wave(true);  // a fade out is the service's: no event, the ring still runs
  if ((last.sent || []).length) setTimeout(() => recheck(last.at), 1500);
  draw();
}

async function recheck(at) {
  try {
    const v = await (await fetch("/api/hands")).json();
    const again = (v.history || []).find((r) => r.at === at);
    if (again && last && last.at === at) { last = again; draw(); }
  } catch (e) { /* the next answer will say */ }
}

// One state source for the panel and the lanterns: GET /api/state, long-polled.
// An event on the bus runs across the floor too, unless it is one this page
// sent and already showed.
let busAt = null;
function apply(s) {
  orchestra = { status: s.status, bar: s.bar, countIn: s.countIn, playing: s.sections.reduce((n, x) => n + x.playing, 0) };
  for (const [group, ids] of Object.entries(SECTIONS)) {  // a group's gain: its chairs', as the service set them
    const chairs = s.sections.filter((x) => ids.includes(x.id)).flatMap((x) => x.chairs);
    if (chairs.length) gains[group] = chairs.reduce((n, c) => n + (c.gain ?? 1), 0) / chairs.length;
  }
  $("label").textContent = s.label;
  const bus = s.lastBus;
  if (bus && bus.at !== busAt) {
    const mine = waved[bus.name] && performance.now() - waved[bus.name] < 8000;
    if (busAt !== null && !mine) hall.wave(bus.name.startsWith("stop."));
    busAt = bus.at;
  }
  hall.apply(s);
  draw();
}

async function follow() {
  let version = null;
  for (;;) {
    if (sock) {  // the WebSocket brings the state
      version = null;
      await new Promise((ok) => setTimeout(ok, 1000));
      continue;
    }
    try {
      const r = await fetch(version === null ? "/api/state" : `/api/state?version=${version}&wait=20`, { cache: "no-store" });
      if (!r.ok) throw new Error(`state ${r.status}`);
      const s = await r.json();
      version = s.version;
      apply(s);
    } catch (e) {
      orchestra = null;
      draw();
      await new Promise((ok) => setTimeout(ok, 2000));
    }
  }
}

// --- made-up postures, to try the path without a headset ----------------------

const MADE_UP = {
  open: { fingers: { thumb: 25, index: 8, middle: 6, ring: 10, little: 12 }, palm: { away: 0.1, toward: -0.1, floor: -0.93, up: 0.93, side: 0.05 }, spread: 0.1 },
  down: { fingers: { thumb: 25, index: 10, middle: 8, ring: 9, little: 12 }, palm: { away: 0.1, toward: -0.1, floor: 0.93, up: -0.93, side: 0.05 }, spread: 0.1 },
  fist: { fingers: { thumb: 90, index: 195, middle: 210, ring: 205, little: 200 }, palm: { away: 0.3, toward: -0.3, floor: 0.2, up: -0.2, side: 0.85 }, spread: 0.04 },
  rest: { fingers: { thumb: 40, index: 70, middle: 80, ring: 85, little: 90 }, palm: { away: 0.3, toward: -0.3, floor: 0.2, up: -0.2, side: 0.85 }, spread: 0.05 },
};
for (const b of document.querySelectorAll("[data-sim]")) {
  b.addEventListener("click", () => {
    const [side, g] = b.dataset.sim.split(":");
    const at = (s) => ({ ...MADE_UP[g], position: { side: s === "left" ? -0.25 : 0.25, height: -0.25, ahead: 0.4 } });
    send(side === "both" ? { left: at("left"), right: at("right") } : { [side]: at(side) }, HOLD_MS, true);
  });
}

// --- the orchestra's sound in the headset -------------------------------------

// /audio.wav is an open-ended stream: it may drop (the headset's link to gx10
// changes, the browser stalls) and it may fall behind. Every 2 s: no progress
// means connect again; more than a second buffered means jump to the live edge.
function startSound() {
  const audio = $("audio");
  audio.src = `/audio.wav?t=${Date.now()}`;
  audio.play().catch(() => {});
  soundOn = true;
  soundAt = -1;
}
setInterval(() => {
  const audio = $("audio");
  if (!soundOn || !$("sound").checked) { soundState = "off"; if (soundOn && !audio.paused) audio.pause(); return; }
  if (audio.paused || audio.error || audio.ended || audio.currentTime === soundAt) { soundState = "reconnecting"; startSound(); return; }
  const end = audio.buffered.length ? audio.buffered.end(audio.buffered.length - 1) : audio.currentTime;
  if (end - audio.currentTime > 1.0) audio.currentTime = end - 0.25;
  soundAt = audio.currentTime;
  soundState = "on";
}, 2000);

// --- WebXR: the orchestra in the room, the panel above it ----------------------

// three.js owns the session; the hands are read in its reference space
// (local-floor: y = 0 is the room's floor), so the joints, the head, the
// lanterns and the panel share one frame.
const renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true });
renderer.xr.enabled = true;
renderer.xr.setReferenceSpaceType("local-floor");
renderer.toneMapping = THREE.ACESFilmicToneMapping;
renderer.toneMappingExposure = 1.15;
renderer.outputColorSpace = THREE.SRGBColorSpace;
const scene = new THREE.Scene();
const camera = new THREE.PerspectiveCamera(70, 2, 0.05, 220);  // far enough for the meadow's sky
const hall = new Hall(scene);
const panelTex = new THREE.CanvasTexture(panel);
panelTex.colorSpace = THREE.SRGBColorSpace;
const panelMesh = new THREE.Mesh(new THREE.PlaneGeometry(1, 1),
  new THREE.MeshBasicMaterial({ map: panelTex, transparent: true, depthTest: false, toneMapped: false }));
panelMesh.renderOrder = 100;
panelMesh.matrixAutoUpdate = false;
panelMesh.visible = false;
scene.add(panelMesh);
const SCALE = Number(params.get("scale")) || 0.6; // the hall's 2.5–4.2 m rows, brought into a room
// The worlds around the orchestra, made when first wanted: the room itself
// (passthrough), the mirror lake under the aurora (the default), the rave in
// an abandoned factory, the cartoon meadow, the zen night; and the drawn
// hands, needed anywhere but the room.
// Each world brings its fog and exposure, and gets the hall and the camera:
// what the orchestra does reaches the world (the lake mirrors the lanterns,
// the aurora burns with the music).
const WORLDS = { aurora, rave, meadow, zen };
const worlds = {};
let world = null, worldName = "room", drawn = null;
function setWorld(name) {
  worldName = WORLDS[name] ? name : "room";
  if (WORLDS[worldName] && !worlds[worldName]) worlds[worldName] = WORLDS[worldName](scene, { hall, camera });
  if (worldName !== "room" && !drawn) drawn = drawnHands(scene);
  world = worlds[worldName] || null;
  for (const [n, w] of Object.entries(worlds)) w.group.visible = n === worldName;
  scene.fog = world ? world.group.userData.fog || null : null;
  renderer.toneMappingExposure = world ? world.group.userData.exposure || 1.15 : 1.15;
  if (drawn) drawn.mesh.visible = !!world;
}

// --- what the headset itself recognises (shown, not acted on) -----------------

// The Quest Browser gives each hand an emulated gamepad (profile oculus-hand):
// button 0 is the pinch (the session's select events too), and since Browser
// 38.1 buttons 5–9 are the thumb's microgestures on the side of the index
// finger: swipe left, right, forward, backward, and a tap. Button 4 on the
// left hand is the system's menu. A palm pinch (pinching while looking at the
// palm) is the system's on both hands: on the left it ends the session.
//
// What they conduct: the section the conductor looks at is the one their
// gestures act on; a short pinch locks it (and frees it); a pinch held and
// moved up or down is that section's gain; a thumb tap is its entrance; a
// swipe forward or backward steps the tempo; a swipe left or right moves
// the selection to the next section that way. The selection is the page's;
// the service does what each native gesture means and answers a record.
const NATIVE = { 0: "pinch", 4: "menu", 5: "swipe-left", 6: "swipe-right", 7: "swipe-forward", 8: "swipe-backward", 9: "tap-thumb" };
const TAP_MS = 350, TAP_M = 0.03;  // a pinch shorter and stiller than this is a tap
const quest = { profiles: null, buttons: null, pressed: { left: {}, right: {} }, pinch: { left: false, right: false }, events: [], gaze: null,
  browser: (navigator.userAgent.match(/OculusBrowser\/([\d.]+)/) || [])[1] || null };
const pinches = { left: null, right: null }, beats = { left: new Beats(), right: new Beats() };
function native(what) {
  quest.events.unshift({ what, at: performance.now() });
  quest.events.length = Math.min(quest.events.length, 4);
  draw();
}
function sendNative(gesture, side, extra = {}) {
  native(`${gesture} ${side || ""}`.trim());
  if (!sock) return;
  const id = nextId++;
  sentAt[id] = performance.now();
  sock.send(JSON.stringify({ type: "native", id, gesture, side, target: sel.id, locked: sel.locked, ...extra }));
}
function readNative(source, side, m, t) {
  if (quest.profiles === null) { quest.profiles = source.profiles || []; quest.buttons = source.gamepad ? source.gamepad.buttons.length : 0; }
  const g = source.gamepad;
  if (!g) return;
  const h = m ? m.position.height : null;
  g.buttons.forEach((b, i) => {
    const was = quest.pressed[side][i] || false;
    quest.pressed[side][i] = b.pressed;
    if (i === 0) {
      quest.pinch[side] = b.pressed;
      if (b.pressed && !was) { pinches[side] = { t0: t, h0: h, moving: false, sentAt: 0 }; native(`pinch ${side}`); }
      else if (b.pressed && pinches[side]) {
        const p = pinches[side];
        if (!p.moving && h !== null && p.h0 !== null && (t - p.t0 > TAP_MS || Math.abs(h - p.h0) > TAP_M)) { p.moving = true; sendNative("pinch-start", side, { height: p.h0 }); }
        if (p.moving && h !== null && t - p.sentAt > 100) { p.sentAt = t; sendNative("pinch-move", side, { height: h }); }
      } else if (!b.pressed && was && pinches[side]) {
        if (pinches[side].moving) sendNative("pinch-end", side);
        else toggleLock();
        pinches[side] = null;
      }
      return;
    }
    if (!(b.pressed && !was) || !NATIVE[i]) return;
    if (i === 5 || i === 6) { native(`${NATIVE[i]} ${side}`); step(i === 5 ? 1 : -1); }
    else if (i === 7 || i === 8 || i === 9) sendNative(NATIVE[i], side);
    else native(`${NATIVE[i]} ${side}`);
  });
}

// --- the section the conductor conducts -----------------------------------------

// The one they look at, after 300 ms on it; a short pinch locks it, and
// another frees it; a swipe left or right moves to the next section that
// way, locked. Looking at nothing (the sky, the panel) keeps the last one.
const SEL_MS = 300;
const sel = { gaze: null, gazeSince: 0, id: null, locked: false };
function target() { return sel.id ? instruments.find((i) => i.id === sel.id) || null : null; }
function gazeAt(instr, t) {
  const id = instr ? instr.id : null;
  if (id !== sel.gaze) { sel.gaze = id; sel.gazeSince = t; }
  if (!sel.locked && id && t - sel.gazeSince > SEL_MS && sel.id !== id) { sel.id = id; shown(); }
}
function toggleLock() {
  if (sel.locked && (!sel.gaze || sel.gaze === sel.id)) { sel.locked = false; if (sel.gaze) sel.id = sel.gaze; native("free"); }
  else if (sel.gaze || sel.id) { sel.id = sel.gaze || sel.id; sel.locked = true; native(`locked ${sel.id}`); }
  shown();
}
function step(dir) {  // +1: the next section to the conductor's left (a higher angle), -1: to the right
  const order = [...instruments].sort((a, b) => a.from - b.from);
  if (!order.length) return;
  const at = order.findIndex((i) => i.id === sel.id);
  const next = order[((at < 0 ? (dir > 0 ? -1 : 0) : at) + dir + order.length) % order.length];
  sel.id = next.id;
  sel.locked = true;
  shown();
}
function shown() {
  const tgt = target();
  hall.select(tgt ? tgt.sections : [], sel.locked);
  draw();
}

// Where the panel floats: 1.4 m ahead of where the head first looked, a
// little above the eyes, facing the conductor, 0.9 m wide.
function panelMatrix(head) {
  const f = flatForward(head.orientation), r = [-f.z, 0, f.x];
  const c = [head.position.x + f.x * 1.4, head.position.y + 0.3, head.position.z + f.z * 1.4];
  const W = 0.9, H = 0.45;
  return [r[0] * W, 0, r[2] * W, 0, 0, H, 0, 0, -f.x, 0, -f.z, 0, c[0], c[1], c[2], 1];
}

function flatForward(q) {  // where the head looks, on the floor's plane
  const fx = -(2 * (q.x * q.z + q.w * q.y)), fz = -(1 - 2 * (q.x * q.x + q.y * q.y));
  const l = Math.hypot(fx, fz) || 1;
  return { x: fx / l, z: fz / l };
}

// Where a hand points, on the hall's floor plan: the direction from the eyes
// to the hand, in degrees, 0 to the conductor's right, 90 ahead, 180 left.
function aimOf(point, eyes) {
  const dx = point[0] - eyes[0], dz = point[2] - eyes[2], t = hall.root.rotation.y;
  const x = dx * Math.cos(t) - dz * Math.sin(t), z = dx * Math.sin(t) + dz * Math.cos(t);
  return Math.round((Math.atan2(-z, x) * 180) / Math.PI);
}

function forwardOf(q) {  // the head's -z, rotated by its orientation
  return [-(2 * (q.x * q.z + q.w * q.y)), -(2 * (q.y * q.z - q.w * q.x)), -(1 - 2 * (q.x * q.x + q.y * q.y))];
}

// The first frame puts the podium at the conductor's feet, the orchestra
// ahead of where they look and the panel above it.
function placeAround(head) {
  hall.place(head.position, flatForward(head.orientation), SCALE);
  panelMesh.matrix.fromArray(panelMatrix(head));
  panelMesh.visible = true;
}

let placed = false, drawnAt = 0;
const clock = new THREE.Clock();
renderer.setAnimationLoop((t, xrFrame) => {
  const dt = Math.min(clock.getDelta(), 0.1);
  if (xrFrame) readHands(t, xrFrame);
  else if (!preview) return;
  if (dirty) { panelTex.needsUpdate = true; dirty = false; }
  if (world) world.frame(dt);
  hall.frame(dt);
  renderer.render(scene, camera);
});

function readHands(t, xrFrame) {
  const space = renderer.xr.getReferenceSpace();
  const pose = xrFrame.getViewerPose(space);
  if (!pose) return;
  if (!placed) { placeAround(pose.transform); placed = true; }
  const head = { position: [pose.transform.position.x, pose.transform.position.y, pose.transform.position.z], forward: forwardOf(pose.transform.orientation) };
  quest.gaze = instrumentAt(aimOf([head.position[0] + head.forward[0], head.position[1], head.position[2] + head.forward[2]], head.position));
  live = { left: null, right: null };
  const seen = [];
  for (const source of xrFrame.session.inputSources) {
    if (!source.hand || !SIDES.includes(source.handedness)) continue;
    const joints = {}, drawnJoints = [];
    let all = true;
    for (const [name, jointSpace] of source.hand) {
      const p = xrFrame.getJointPose(jointSpace, space);
      if (!p) { all = false; break; }
      joints[name] = [p.transform.position.x, p.transform.position.y, p.transform.position.z];
      drawnJoints.push([name, joints[name], p.radius]);
    }
    if (all) seen.push(drawnJoints);
    let m = null;
    if (all) {
      m = measure(joints, source.handedness, head);
      m.aim = aimOf(joints["middle-finger-metacarpal"], head.position);
      live[source.handedness] = m;
    }
    readNative(source, source.handedness, m, t);
  }
  if (drawn && world) drawn.update(seen);
  gazeAt(quest.gaze, t);
  // Each flat hand's height, ten times a second: the service makes it its
  // group's gain while Jev's last reading of that hand is a palm up or a palm
  // down. A hand that pinches, points or closes moves nothing this way.
  if (sock && t - heightAt > 100) {
    heightAt = t;
    const heights = {};
    for (const side of SIDES) if (live[side] && flatHand(live[side]) && !quest.pinch[side]) heights[side] = { height: live[side].position.height };
    if (Object.keys(heights).length) sock.send(JSON.stringify({ type: "height", hands: heights }));
  }
  // A pointing hand beats time, a baton: its beats set the tempo.
  for (const side of SIDES) {
    if (live[side] && pointing(live[side])) {
      const bpm = beats[side].update(live[side].position.height, t / 1000);
      if (bpm) sendNative("beat", side, { bpm });
    } else beats[side].reset();
  }
  // a posture with another section selected is a new posture
  const sig = live.left || live.right ? `${signature(live.left)}/${signature(live.right)}~${sel.id}` : "-|-";
  if (steady.update(sig, t)) {
    const hands = {};
    for (const side of SIDES) if (live[side]) hands[side] = live[side];
    send(hands, steady.heldMs(t));
  }
  if (t - drawnAt > 200) { drawnAt = t; draw(); }
}

async function enter() {
  if ($("sound").checked) startSound();
  setWorld($("world").value);
  const mode = xr.ar && worldName === "room" ? "immersive-ar" : "immersive-vr";
  const session = await navigator.xr.requestSession(mode, { requiredFeatures: ["hand-tracking", "local-floor"] });
  for (const kind of ["selectstart", "selectend"]) session.addEventListener(kind, (e) => native(`${kind} ${e.inputSource.handedness}`));
  session.addEventListener("end", () => { placed = false; $("enter").textContent = "Enter again"; });
  await renderer.xr.setSession(session);
}

// ?view=1: the same hall and panel on a flat screen, seen from behind the
// conductor, to check the page without a headset.
if (preview) {
  const view = renderer.domElement;
  view.id = "view";
  $("panel").after(view);
  const fit = () => { const w = view.clientWidth || 1024; renderer.setSize(w, w / 2, false); camera.aspect = 2; camera.updateProjectionMatrix(); };
  addEventListener("resize", () => { if (!renderer.xr.isPresenting) fit(); });
  fit();
  camera.position.set(0, 1.75, 0.7);
  const yaw = ((Number(params.get("yaw")) || 0) * Math.PI) / 180;  // ?yaw= turns the preview's eyes (180: the conductor looks back); ?tilt= raises them
  camera.lookAt(-Math.sin(yaw) * 2.3, 0.9 + (Number(params.get("tilt")) || 0), 0.7 - Math.cos(yaw) * 2.3);
  placeAround({ position: { x: 0, y: 1.6, z: 0 }, orientation: { x: 0, y: 0, z: 0, w: 1 } });
  setWorld($("world").value);
  $("world").addEventListener("change", () => setWorld($("world").value));
}

async function check() {
  const button = $("enter");
  if (!navigator.xr) { button.textContent = "No WebXR here (open this page in the Quest Browser)"; return; }
  const ar = await navigator.xr.isSessionSupported("immersive-ar").catch(() => false);
  const vr = ar || await navigator.xr.isSessionSupported("immersive-vr").catch(() => false);
  if (!vr) { button.textContent = "No immersive WebXR here"; return; }
  xr = { ar };
  button.disabled = false;
  button.textContent = ar ? "Enter (your room around you)" : "Enter (VR)";
  button.addEventListener("click", () => enter().catch((e) => { button.textContent = `Could not enter: ${e.message}`; }));
}
check();
draw();
Promise.all([document.fonts.load("30px DotGothic16"), document.fonts.load("30px Lexend"), fetch("/api/score").then((r) => r.json())])
  .then(([, , score]) => { hall.score = score; })
  .catch(() => {})
  .finally(() => { connect(); follow(); });
