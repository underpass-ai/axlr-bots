// The orchestra in the conductor's room: the hall of the podium view
// (stage3d-3.js, one-shot #3) rebuilt around the headset. The podium is where
// the conductor stands; the chairs sit on arcs ahead of them, on the room's
// floor, scaled down to fit a room. Each lantern is lit only by what GET
// /api/state says its bot did. A gesture the service sent runs across the
// floor as a ring at once; an event from anyone else when the service hears it.
import * as THREE from "/static/stage3d-3.three.module.min.js";

export { THREE };

// The dark theme's palette (stage.css): it reads over a passthrough room.
const HEX = { text: "#F4F4F2", muted: "#A3A39E", accent: "#E08A5A", warn: "#E8B45A", bad: "#F07B6B", idle: "#A3A39E", line: "#5A5A5E" };
const C = Object.fromEntries(Object.entries(HEX).map(([k, v]) => [k, new THREE.Color(v)]));

// Where each section sits, in metres before the room's scale: 0° to the
// conductor's right, 90° straight ahead, rows at radii from the podium.
const SEATING = {
  vn1: { from: 166, to: 134, rows: [2.5, 3.3], per: [5, 5], label: [152, 4.15] },
  vn2: { from: 123, to: 103, rows: [2.5, 3.3], per: [4, 4], label: [118, 4.7] },
  va: { from: 77, to: 57, rows: [2.5, 3.3], per: [3, 3], label: [62, 4.7] },
  vc: { from: 50, to: 22, rows: [2.5, 3.3], per: [3, 2], label: [42, 4.25] },
  cb: { from: 30, to: 8, rows: [4.15], per: [3], label: [22, 4.95] },
  ob: { from: 105, to: 95, rows: [4.2], per: [2], label: [100, 5.0] },
  hn: { from: 85, to: 75, rows: [4.2], per: [2], label: [80, 5.0] },
};
const polar = (r, deg) => { const a = (deg * Math.PI) / 180; return new THREE.Vector3(r * Math.cos(a), 0, -r * Math.sin(a)); };

// What a lantern looks like per state: height, how bright, which colour.
const LOOK = {
  waiting: { h: 0.12, glow: 0.0, body: 0.12, color: C.idle, label: 0.6 },
  scheduled: { h: 0.22, glow: 0.35, body: 0.9, color: C.warn, label: 1.0 },
  playing: { h: 0.7, glow: 1.0, body: 1.0, color: C.accent, label: 0.9 },
  stopped: { h: 0.07, glow: 0.0, body: 0.05, color: C.muted, label: 0.45 },
};
const DYN = { p: 0.55, mf: 0.78, f: 1.0 };

function radialTexture(stops) {
  const c = document.createElement("canvas");
  c.width = c.height = 128;
  const g = c.getContext("2d");
  const grad = g.createRadialGradient(64, 64, 0, 64, 64, 64);
  for (const [at, col] of stops) grad.addColorStop(at, col);
  g.fillStyle = grad;
  g.fillRect(0, 0, 128, 128);
  const t = new THREE.CanvasTexture(c);
  t.colorSpace = THREE.SRGBColorSpace;
  return t;
}
const POOL_TEX = radialTexture([[0, "rgba(255,255,255,0.9)"], [0.35, "rgba(255,255,255,0.35)"], [1, "rgba(255,255,255,0)"]]);
const GLASS = (() => {
  const c = document.createElement("canvas");
  c.width = 1; c.height = 64;
  const g = c.getContext("2d");
  const grad = g.createLinearGradient(0, 64, 0, 0);
  grad.addColorStop(0, "#6a6a6a"); grad.addColorStop(0.55, "#d0d0d0"); grad.addColorStop(1, "#ffffff");
  g.fillStyle = grad; g.fillRect(0, 0, 1, 64);
  const t = new THREE.CanvasTexture(c); t.colorSpace = THREE.SRGBColorSpace; return t;
})();

// A text label: the bot's id, a section's name. Cached by text and style.
const labelCache = new Map();
function labelTexture(text, size, color, sub) {
  const k = `${text}|${size}|${color}|${sub || ""}`;
  if (labelCache.has(k)) return labelCache.get(k);
  const c = document.createElement("canvas");
  c.width = 256; c.height = sub ? 120 : 72;
  const g = c.getContext("2d");
  g.textAlign = "center";
  g.textBaseline = "middle";
  g.fillStyle = color;
  g.font = `${size}px DotGothic16, monospace`;
  g.fillText(text, c.width / 2, sub ? 34 : c.height / 2);
  if (sub) {
    g.font = `600 ${size * 0.72}px Lexend, sans-serif`;
    g.fillText(sub, c.width / 2, 86);
  }
  const t = new THREE.CanvasTexture(c);
  t.colorSpace = THREE.SRGBColorSpace;
  t.minFilter = THREE.LinearFilter;
  labelCache.set(k, t);
  return t;
}
function makeLabel(text, size, color, sub, worldW) {
  const tex = labelTexture(text, size, color, sub);
  const s = new THREE.Sprite(new THREE.SpriteMaterial({ map: tex, transparent: true, depthWrite: false }));
  s.scale.set(worldW, worldW * tex.image.height / tex.image.width, 1);
  return s;
}

const lanternGeo = new THREE.CapsuleGeometry(0.1, 1, 6, 16);
lanternGeo.translate(0, 0.5 + 0.1, 0); // its foot at y=0, 1.18 tall; scaled per state
const baseGeo = new THREE.CylinderGeometry(0.2, 0.22, 0.04, 28);
const waveGeo = new THREE.RingGeometry(0.9, 1.0, 128, 1, 0, Math.PI);

export class Hall {
  constructor(scene) {
    this.root = new THREE.Group();
    this.root.visible = false;
    scene.add(this.root);
    scene.add(new THREE.HemisphereLight(0x8a8a92, 0x0f0f10, 0.9));
    const key = new THREE.DirectionalLight(0xfff1e4, 0.9);
    key.position.set(-3, 7, 4);
    scene.add(key);
    this.lanterns = {};
    this.sections = {};
    this.waves = [];
    this.pulse = null;
    this.state = null;
    this.stateAt = 0;
    this.score = null;
    this.snap = 0;
    this.lastCutAt = null;
    // the rows as faint arcs on the floor, from 10° to 170°
    const arc = new THREE.MeshBasicMaterial({ color: C.line, transparent: true, opacity: 0.55, side: THREE.DoubleSide, depthWrite: false });
    for (const r of [2.5, 3.3, 4.15]) {
      const ring = new THREE.Mesh(new THREE.RingGeometry(r - 0.012, r + 0.012, 160, 1, Math.PI / 18, Math.PI * 16 / 18), arc);
      ring.rotation.x = -Math.PI / 2;
      ring.position.y = 0.002;
      this.root.add(ring);
    }
    this.ring = new THREE.Mesh(new THREE.RingGeometry(0.5, 0.56, 64), new THREE.MeshBasicMaterial({ color: C.accent, transparent: true, opacity: 0,
      blending: THREE.AdditiveBlending, depthWrite: false, side: THREE.DoubleSide }));
    this.ring.rotation.x = -Math.PI / 2;
    this.ring.position.y = 0.012;
    this.root.add(this.ring);
  }

  // The podium at the conductor's feet, the orchestra ahead of where they look.
  place(position, forward, scale) {
    this.root.position.set(position.x, 0, position.z);
    this.root.rotation.y = Math.atan2(-forward.x, -forward.z);
    this.root.scale.setScalar(scale);
    this.root.visible = true;
  }

  build(state) {
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
          const group = new THREE.Group();
          group.position.copy(p);
          const base = new THREE.Mesh(baseGeo, new THREE.MeshStandardMaterial({ color: 0x232326, roughness: 0.7, metalness: 0.2 }));
          base.position.y = 0.02;
          const body = new THREE.Mesh(lanternGeo, new THREE.MeshStandardMaterial({ color: C.idle.clone(), emissive: C.idle.clone(), emissiveMap: GLASS,
            emissiveIntensity: 0.1, roughness: 0.4, metalness: 0.1 }));
          body.position.y = 0.04;
          body.scale.y = LOOK.waiting.h;
          const glow = new THREE.Sprite(new THREE.SpriteMaterial({ map: POOL_TEX, color: C.accent, transparent: true, opacity: 0,
            blending: THREE.AdditiveBlending, depthWrite: false }));
          glow.position.y = 0.6;
          glow.scale.set(1.6, 1.6, 1);
          const pool = new THREE.Mesh(new THREE.PlaneGeometry(1.8, 1.8), new THREE.MeshBasicMaterial({ map: POOL_TEX, color: C.accent, transparent: true,
            opacity: 0, blending: THREE.AdditiveBlending, depthWrite: false }));
          pool.rotation.x = -Math.PI / 2;
          pool.position.y = 0.005;
          const idTex = labelTexture(chair.id, 62, HEX.muted, null);
          const label = new THREE.Mesh(new THREE.PlaneGeometry(0.6, 0.6 * idTex.image.height / idTex.image.width),
            new THREE.MeshBasicMaterial({ map: idTex, transparent: true, opacity: LOOK.waiting.label, depthWrite: false }));
          label.rotation.x = -Math.PI / 2;
          const toward = p.clone().setY(0).normalize().multiplyScalar(-0.36); // towards the podium
          label.position.set(toward.x, 0.006, toward.z + 0.02);
          const tag = makeLabel(chair.id, 40, HEX.warn, "enters", 1.3);
          tag.position.y = 0.6;
          tag.material.opacity = 0;
          group.add(base, body, glow, pool, label, tag);
          this.root.add(group);
          this.lanterns[chair.id] = { group, body, glow, pool, label, tag, sub: null, idCol: HEX.muted, state: "waiting",
            now: { h: LOOK.waiting.h, glow: 0, body: 0.1, color: C.idle.clone(), label: LOOK.waiting.label, tag: 0 },
            target: { h: LOOK.waiting.h, glow: 0, body: 0.1, color: C.idle.clone(), label: LOOK.waiting.label, tag: 0 } };
        }
      });
      const mid = polar(plan.label[1], plan.label[0]);
      const s = makeLabel(section.name, 40, HEX.text, `${section.playing}/${section.chairs.length}`, 1.6);
      s.position.set(mid.x, 0.85, mid.z);
      this.root.add(s);
      this.sections[section.id] = { sprite: s, name: section.name, total: section.chairs.length, playing: -1, by: null };
    }
  }

  // Each lantern's target from the service's state, as the podium view does.
  apply(state) {
    if (!Object.keys(this.lanterns).length) this.build(state);
    this.state = state;
    this.stateAt = performance.now();
    for (const section of state.sections) {
      for (const chair of section.chairs) {
        const L = this.lanterns[chair.id];
        if (!L) continue;
        const look = LOOK[chair.state] || LOOK.waiting;
        const dyn = DYN[chair.dynamics] || 0.78;
        const ended = state.status === "ended" ? 0.35 : 1;
        L.state = chair.state;
        const gain = chair.gain ?? 1;  // the conductor's hand: the lantern fades with the sound
        L.target.h = (chair.state === "playing" ? 0.3 + 0.4 * dyn : look.h) * (0.25 + 0.75 * gain);
        L.target.glow = look.glow * (chair.state === "playing" ? dyn : 1) * (ended === 1 ? 1 : 0) * gain;
        L.target.body = look.body * (chair.state === "playing" ? 0.6 + 0.4 * dyn : 1) * ended * (0.2 + 0.8 * gain);
        L.target.color = look.color;
        L.target.label = chair.held ? 1 : look.label;
        const sub = chair.state === "scheduled" && chair.enteredBar ? `enters bar ${chair.enteredBar}` : null;
        L.target.tag = sub ? 1 : 0;
        if (sub && sub !== L.sub) {
          L.sub = sub;
          L.tag.material.map = labelTexture(chair.id, 40, HEX.warn, sub);
          L.tag.material.needsUpdate = true;
        }
        // the id on the floor: red while the conductor holds the chair's section
        const idCol = chair.held ? HEX.bad : chair.state === "playing" ? HEX.text : chair.state === "scheduled" ? HEX.warn : HEX.muted;
        if (idCol !== L.idCol) {
          L.idCol = idCol;
          L.label.material.map = labelTexture(chair.id, 62, idCol, null);
          L.label.material.needsUpdate = true;
        }
      }
      const S = this.sections[section.id];
      if (S && S.playing !== section.playing) {
        S.playing = section.playing;
        S.subText = this.sub(section.id, S);
        S.sprite.material.map = labelTexture(S.name, 40, S.by || (section.playing ? HEX.text : HEX.muted), S.subText);
        S.sprite.material.needsUpdate = true;
      }
    }
    if (state.cut && state.cut.cutAt !== this.lastCutAt) { this.lastCutAt = state.cut.cutAt; this.snap = performance.now(); }
  }

  // The sections a hand points at: their names lit, the left hand's copper, the right's amber.
  aim(side, sections) {
    this.aimed = this.aimed || { left: [], right: [] };
    this.aimed[side] = sections;
    for (const [id, S] of Object.entries(this.sections)) {
      const by = this.aimed.left.includes(id) ? HEX.accent : this.aimed.right.includes(id) ? HEX.warn : null;
      const sub = this.sub(id, S);
      if (by === S.by && sub === S.subText) continue;
      S.by = by;
      S.subText = sub;
      S.sprite.material.map = labelTexture(S.name, 40, by || (S.playing ? HEX.text : HEX.muted), sub);
      S.sprite.material.needsUpdate = true;
      if (by !== S.lit) { S.sprite.scale.multiplyScalar(by ? 1.35 : 1 / 1.35); S.lit = by; }
    }
  }

  // The sections the conductor conducts: the ones they look at, in copper;
  // locked with a pinch, marked.
  select(sections, locked) {
    this.locked = !!locked;
    this.aim("left", sections);
  }

  // Under a section's name: how many play, and a mark while the conductor holds it locked.
  sub(id, S) {
    const held = this.locked && this.aimed && this.aimed.left.includes(id);
    return `${Math.max(S.playing, 0)}/${S.total}${held ? " ●" : ""}`;
  }

  // An event leaves the podium and runs across the floor to the chairs.
  wave(stop) {
    const color = stop ? C.bad : C.accent;
    const m = new THREE.Mesh(waveGeo, new THREE.MeshBasicMaterial({ color, transparent: true, opacity: 0.9, blending: THREE.AdditiveBlending,
      depthWrite: false, side: THREE.DoubleSide }));
    m.rotation.x = -Math.PI / 2;
    m.position.y = 0.01;
    this.root.add(m);
    this.waves.push({ m, t0: performance.now(), strong: stop, hits: {} });
    this.pulse = { t0: performance.now(), color };
  }

  // The playhead, as the 2D stage computes it.
  position() {
    const s = this.state, score = this.score;
    if (!s || !score) return 0;
    let pos = s.bar - 1 + s.beat / score.beatsPerBar;
    if (s.status === "playing") pos += ((performance.now() - this.stateAt) / 1000) * (s.bpm / 60) / score.beatsPerBar;
    return Math.min(pos, score.bars);
  }

  frame(dt) {
    const now = performance.now();
    const beat = (this.position() * (this.score ? this.score.beatsPerBar : 4)) % 1;
    const breathe = this.state && this.state.status === "playing" ? Math.pow(Math.max(0, Math.cos(beat * Math.PI * 2)), 3) : 0;
    const falling = this.snap && now - this.snap < 600; // a stop.all drops the lanterns at once
    for (const id in this.lanterns) {
      const L = this.lanterns[id], n = L.now, t = L.target;
      const k = falling && L.state === "stopped" ? 1 - Math.exp(-dt * 18) : 1 - Math.exp(-dt * (L.state === "playing" ? 5 : 4));
      n.h += (t.h - n.h) * k;
      n.glow += (t.glow - n.glow) * k;
      n.body += (t.body - n.body) * k;
      n.label += (t.label - n.label) * k;
      n.tag += (t.tag - n.tag) * k;
      n.color.lerp(t.color, k);
      const since = now - this.hitAt(id);
      const wave = since < 450 ? 1 - since / 450 : 0;
      const h = n.h * (1 + 0.05 * breathe * (L.state === "playing" ? 1 : 0));
      L.body.scale.y = h;
      L.body.material.color.copy(n.color).multiplyScalar(0.45);
      L.body.material.emissive.copy(n.color);
      L.body.material.emissiveIntensity = n.body * (1 + 0.12 * breathe) + wave * 0.5;
      L.glow.material.color.copy(n.color);
      L.glow.material.opacity = n.glow * 0.28 * (1 + 0.15 * breathe) + wave * 0.2;
      L.glow.position.y = 0.04 + 1.18 * h * 0.55;
      L.glow.scale.setScalar(1.0 + 1.2 * h);
      L.pool.material.color.copy(n.color);
      L.pool.material.opacity = n.glow * 0.3 + wave * 0.2;
      L.label.material.opacity = n.label;
      L.tag.material.opacity = n.tag;
      L.tag.position.y = 0.04 + 1.18 * h + 0.3 + 0.05 * Math.sin(now / 400);
    }
    for (let i = this.waves.length - 1; i >= 0; i--) {
      const w = this.waves[i], age = (now - w.t0) / 1000, r = 0.4 + age * 4.2;
      w.m.scale.set(r, r, 1);
      w.m.material.opacity = Math.max(0, (w.strong ? 1.0 : 0.7) * (1 - age / 1.3));
      for (const id in this.lanterns) {
        if (!(id in w.hits) && this.lanterns[id].group.position.length() <= r) w.hits[id] = now;
      }
      if (age > 1.3) { this.root.remove(w.m); w.m.material.dispose(); this.waves.splice(i, 1); }
    }
    const age = this.pulse ? (now - this.pulse.t0) / 1000 : 9;
    this.ring.material.opacity = Math.max(0, 1 - age / 1.6) * 0.9;
    if (this.pulse) this.ring.material.color.copy(this.pulse.color);
  }

  // When a ring reached a chair (ms), for the flash as the event passes.
  hitAt(id) { let best = -Infinity; for (const w of this.waves) if (w.hits[id] != null) best = Math.max(best, w.hits[id]); return best; }
}
