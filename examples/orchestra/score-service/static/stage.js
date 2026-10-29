// The stage: draws the chairs, the score strip and the podium once, then
// follows GET /api/state by long-poll and colours each chair by what the
// score service says it did. Between two answers the playhead moves on at
// the tempo the service gave, and snaps back to the service at each answer.
"use strict";

const NS = "http://www.w3.org/2000/svg";
const params = new URLSearchParams(location.search);
if (params.get("hud") === "0") document.body.classList.add("nohud");
if (params.get("theme")) document.documentElement.dataset.theme = params.get("theme");

// Where each section sits: an arc of the stage around the podium, its rows
// at radii from the podium, front row first. Angles in degrees, 0 to the right.
const PODIUM = { x: 500, y: 935 };
const SEATING = {
  vn1: { from: 178, to: 142, rows: [260, 345], per: [5, 5] },
  vn2: { from: 128, to: 104, rows: [260, 345], per: [4, 4] },
  va: { from: 76, to: 52, rows: [260, 345], per: [3, 3] },
  vc: { from: 38, to: 4, rows: [260, 345], per: [3, 2] },
  cb: { from: 36, to: 10, rows: [440], per: [3] },
  ob: { from: 106, to: 96, rows: [445], per: [2] },
  hn: { from: 84, to: 74, rows: [445], per: [2] },
};
const STRIP = { x: 150, y: 36, w: 820, lane: 30, window: 32, before: 6 };

let score = null, seats = {}, counts = {}, lanes = {}, last = null, lastAt = 0;

function el(name, attrs, parent) {
  const e = document.createElementNS(NS, name);
  for (const [k, v] of Object.entries(attrs || {})) e.setAttribute(k, v);
  if (parent) parent.appendChild(e);
  return e;
}

function polar(r, deg) {
  const a = (deg * Math.PI) / 180;
  return { x: PODIUM.x + r * Math.cos(a), y: PODIUM.y - r * Math.sin(a) };
}

function drawSeats(state) {
  const g = document.getElementById("seats");
  g.textContent = "";
  for (const section of state.sections) {
    const plan = SEATING[section.id];
    if (!plan) continue;
    let i = 0;
    plan.rows.forEach((r, row) => {
      const n = plan.per[row];
      for (let k = 0; k < n && i < section.chairs.length; k++, i++) {
        const deg = n === 1 ? (plan.from + plan.to) / 2 : plan.from + ((plan.to - plan.from) * k) / (n - 1);
        const p = polar(r, deg);
        const chair = section.chairs[i];
        const seat = el("g", { class: "seat waiting", transform: `translate(${p.x.toFixed(1)} ${p.y.toFixed(1)})` }, g);
        el("title", {}, seat).textContent = `${section.name} ${i + 1}: bot ${chair.id}`;
        el("circle", { r: 17 }, seat);
        el("text", {}, seat).textContent = String(i + 1);
        seats[chair.id] = seat;
      }
    });
    const outer = Math.max(...plan.rows) + 52;
    const mid = polar(outer, (plan.from + plan.to) / 2);
    el("text", { class: "section-name", x: mid.x, y: mid.y - 9 }, g).textContent = section.name;
    counts[section.id] = el("text", { class: "section-count", x: mid.x, y: mid.y + 10 }, g);
  }
  const podium = el("g", { class: "podium" }, document.getElementById("podium"));
  el("circle", { cx: PODIUM.x, cy: PODIUM.y, r: 9 }, podium);
  el("text", { x: PODIUM.x, y: PODIUM.y + 30 }, podium).textContent = "conductor";
}

function drawStrip() {
  const g = document.getElementById("strip");
  g.textContent = "";
  score.sections.forEach((s, i) => {
    const y = STRIP.y + i * STRIP.lane;
    const lane = el("g", { class: "lane" }, g);
    el("text", { class: "lane-name", x: STRIP.x - 8, y: y + STRIP.lane / 2 }, lane).textContent = s.name;
    lanes[s.id] = { g: lane, cells: el("g", {}, lane), y };
  });
  g.appendChild(el("line", { id: "playhead", class: "playhead", y1: STRIP.y - 6, y2: STRIP.y + score.sections.length * STRIP.lane + 4 }));
  el("g", { id: "barnos" }, g);
}

// The window of bars the strip shows: from a few bars before the playhead.
let windowStart = -1;
function paintWindow(first) {
  if (first === windowStart) return;
  windowStart = first;
  const cw = STRIP.w / STRIP.window;
  for (const s of score.sections) {
    const lane = lanes[s.id];
    lane.cells.textContent = "";
    for (let b = 0; b < STRIP.window; b++) {
      const d = s.density[first - 1 + b];
      if (!d) continue;
      el("rect", { class: "cell", x: STRIP.x + b * cw + 1, y: lane.y + 3, width: cw - 2, height: STRIP.lane - 6, opacity: (0.12 + 0.6 * d).toFixed(2) }, lane.cells);
    }
  }
  const nos = document.getElementById("barnos");
  nos.textContent = "";
  for (let b = 0; b < STRIP.window; b += 4) {
    el("text", { class: "barno", x: STRIP.x + b * cw + cw / 2, y: STRIP.y + score.sections.length * STRIP.lane + 16 }, nos).textContent = first + b;
  }
}

function position() {
  // bars from the start, as a fraction, moved on since the last answer at its tempo
  if (!last) return 0;
  let pos = last.bar - 1 + last.beat / score.beatsPerBar;
  if (last.status === "playing") pos += ((performance.now() - lastAt) / 1000) * (last.bpm / 60) / score.beatsPerBar;
  return Math.min(pos, score.bars);
}

function frame() {
  if (score && last) {
    const pos = position();
    const first = Math.max(1, Math.floor(pos) + 1 - STRIP.before);
    paintWindow(first);
    const x = STRIP.x + (pos - (first - 1)) * (STRIP.w / STRIP.window);
    const ph = document.getElementById("playhead");
    ph.setAttribute("x1", x.toFixed(1));
    ph.setAttribute("x2", x.toFixed(1));
    document.getElementById("bar").textContent = String(Math.floor(pos) + 1);
  }
  requestAnimationFrame(frame);
}

function apply(state) {
  last = state;
  lastAt = performance.now();
  if (!Object.keys(seats).length) drawSeats(state);
  let playing = 0;
  for (const section of state.sections) {
    for (const chair of section.chairs) {
      const seat = seats[chair.id];
      if (!seat) continue;
      seat.setAttribute("class", `seat ${chair.state} ${chair.dynamics}`);
      if (chair.state === "playing") playing++;
    }
    if (counts[section.id]) counts[section.id].textContent = `${section.playing}/${section.chairs.length}`;
    if (lanes[section.id]) lanes[section.id].g.setAttribute("class", section.playing ? "lane live" : "lane");
  }
  document.getElementById("take").textContent = state.take || "–";
  document.getElementById("bars").textContent = ` / ${state.bars}`;
  document.getElementById("bpm").textContent = Math.round(state.bpm);
  document.getElementById("playing").textContent = playing;
  const status = document.getElementById("status");
  status.textContent = state.status;
  status.className = `pill ${state.status}`;
  document.getElementById("label").textContent = state.label || "";
  const bus = state.lastBus;
  if (bus) {
    const body = { name: bus.name, message: bus.message };
    if (bus.data) body.data = bus.data;
    document.getElementById("event").innerHTML = "";
    const b = document.createElement("b");
    b.textContent = "operator → bus  ";
    document.getElementById("event").append(b, JSON.stringify(body));
  }
  const cut = document.getElementById("cut");
  if (state.cut && state.cut.afterEventSeconds != null) {
    cut.innerHTML = "";
    const n = document.createElement("b");
    n.textContent = `${state.cut.afterEventSeconds.toFixed(2)} s`;
    cut.append("silence ", n, ` after ${state.cut.why}, at bar ${state.cut.bar} (the sound; the bots stop on their own clock)`);
  } else {
    cut.textContent = "";
  }
}

async function follow() {
  let version = null;
  for (;;) {
    try {
      const url = version === null ? "/api/state" : `/api/state?version=${version}&wait=20`;
      const r = await fetch(url, { cache: "no-store" });
      const state = await r.json();
      version = state.version;
      apply(state);
    } catch (e) {
      await new Promise((ok) => setTimeout(ok, 2000));
    }
  }
}

fetch("/api/score").then((r) => r.json()).then((s) => {
  score = s;
  drawStrip();
  follow();
  requestAnimationFrame(frame);
});
