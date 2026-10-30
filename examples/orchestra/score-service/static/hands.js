// The conductor's page: WebXR hand input → postures → POST /api/hands, and
// one floating panel that says what each hand holds, what Jev read and what
// the service sent. See hands.html.
import { measure, signature, Steady } from "/static/posture.js";

const $ = (id) => document.getElementById(id);
const panel = $("panel"), ctx = panel.getContext("2d");
const HOLD_MS = 300, SIDES = ["left", "right"];
const GROUP = { left: "strings", right: "winds" };
const steady = new Steady(HOLD_MS);
let live = { left: null, right: null }, last = null, orchestra = null, xr = null, dirty = true;
if (new URLSearchParams(location.search).get("sim") === "1") document.body.classList.add("simulate");

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
  ctx.fillText(o ? `${o.status}${o.status === "playing" ? ` · bar ${o.bar}${o.countIn ? ` · count-in ${o.countIn}` : ""} · ${o.playing}/36 playing` : ""}` : "orchestra: …", 210, 18);
  SIDES.forEach((side, i) => {
    const x = 24 + i * 500, read = last?.read?.[side], jev = last?.jev?.answers?.[side], m = live[side];
    ctx.fillStyle = muted;
    ctx.font = "600 24px Lexend, sans-serif";
    ctx.fillText(`${side.toUpperCase()} HAND · ${GROUP[side]}`, x, 70);
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
  ctx.font = "26px Lexend, sans-serif";
  let y = 312;
  for (const e of last?.sent || []) {
    ctx.fillStyle = copper;
    ctx.fillText(`sent ${e.name}${e.data?.dynamics ? " " + e.data.dynamics : ""}${e.to ? ` → ${e.to.length} chairs` : ""}${e.note ? " (dry run)" : ""}${e.error ? " FAILED" : ""}`, 24, y);
    y += 36;
  }
  for (const e of last?.notSent || []) {
    if (y > h - 70) break;
    ctx.fillStyle = muted;
    ctx.fillText(`not sent: ${e.why}`, 24, y);
    y += 36;
  }
  ctx.fillStyle = muted;
  ctx.font = "20px Lexend, sans-serif";
  if (last?.jev?.ms) ctx.fillText(`Jev ${last.jev.ms} ms${last.simulated ? " · simulated posture" : ""}`, 24, h - 36);
  dirty = true;
}

// --- the service ------------------------------------------------------------

async function send(hands, heldMs, simulated = false) {
  try {
    const r = await fetch("/api/hands", { method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ hands, heldMs, simulated }) });
    last = await r.json();
  } catch (e) {
    last = { notSent: [{ why: `the service did not answer: ${e}` }] };
  }
  draw();
}

async function poll() {
  try {
    const s = await (await fetch("/api/state")).json();
    orchestra = { status: s.status, bar: s.bar, countIn: s.countIn, playing: s.sections.reduce((n, x) => n + x.playing, 0) };
    $("label").textContent = s.label;
  } catch (e) {
    orchestra = null;
  }
  draw();
}
setInterval(poll, 1000);
poll();

// --- made-up postures, to try the path without a headset ----------------------

const MADE_UP = {
  open: { fingers: { thumb: 30, index: 8, middle: 6, ring: 10, little: 12 }, palm: { away: 0.88, toward: -0.88, floor: 0.15, up: -0.15, side: 0.1 }, spread: 0.12 },
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

// --- WebXR ------------------------------------------------------------------

const VERT = `#version 300 es
in vec2 p; uniform mat4 P, V, M; out vec2 uv;
void main() { uv = p + 0.5; gl_Position = P * V * M * vec4(p, 0.0, 1.0); }`;
const FRAG = `#version 300 es
precision mediump float; in vec2 uv; uniform sampler2D t; out vec4 c;
void main() { c = texture(t, vec2(uv.x, 1.0 - uv.y)); }`;

function glSetup(gl) {
  const sh = (type, src) => { const s = gl.createShader(type); gl.shaderSource(s, src); gl.compileShader(s); return s; };
  const prog = gl.createProgram();
  gl.attachShader(prog, sh(gl.VERTEX_SHADER, VERT));
  gl.attachShader(prog, sh(gl.FRAGMENT_SHADER, FRAG));
  gl.linkProgram(prog);
  const buf = gl.createBuffer();
  gl.bindBuffer(gl.ARRAY_BUFFER, buf);
  gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-0.5, -0.5, 0.5, -0.5, -0.5, 0.5, 0.5, 0.5]), gl.STATIC_DRAW);
  const tex = gl.createTexture();
  gl.bindTexture(gl.TEXTURE_2D, tex);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
  return { prog, buf, tex, loc: (n) => gl.getUniformLocation(prog, n), attr: gl.getAttribLocation(prog, "p") };
}

// Where the panel floats: 1.4 m ahead of where the head first looked, a
// little above the eyes, facing the conductor, 0.9 m wide.
function panelMatrix(head) {
  const q = head.orientation, v = [0, 0, -1];
  const fx = 2 * (q.x * q.z + q.w * q.y) * v[2], fz = (1 - 2 * (q.x * q.x + q.y * q.y)) * v[2];
  const l = Math.hypot(fx, fz) || 1, f = [fx / l, 0, fz / l], r = [-f[2], 0, f[0]];
  const c = [head.position.x + f[0] * 1.4, head.position.y + 0.25, head.position.z + f[2] * 1.4];
  const W = 0.9, H = 0.45;
  return new Float32Array([r[0] * W, 0, r[2] * W, 0, 0, H, 0, 0, -f[0], 0, -f[2], 0, c[0], c[1], c[2], 1]);
}

function forwardOf(q) {  // the head's -z, rotated by its orientation
  return [-(2 * (q.x * q.z + q.w * q.y)), -(2 * (q.y * q.z - q.w * q.x)), -(1 - 2 * (q.x * q.x + q.y * q.y))];
}

async function enter() {
  if ($("sound").checked) {
    const audio = $("audio");
    audio.src = "/audio.wav";
    audio.play().catch(() => {});
  }
  const mode = xr.ar ? "immersive-ar" : "immersive-vr";
  const session = await navigator.xr.requestSession(mode, { requiredFeatures: ["hand-tracking"] });
  const canvas = document.createElement("canvas");
  const gl = canvas.getContext("webgl2", { xrCompatible: true, alpha: true });
  session.updateRenderState({ baseLayer: new XRWebGLLayer(session, gl) });
  const space = await session.requestReferenceSpace("local");
  const g = glSetup(gl);
  let model = null, drawnAt = 0;
  session.addEventListener("end", () => { $("enter").textContent = "Enter again"; });

  session.requestAnimationFrame(function frame(t, xrFrame) {
    session.requestAnimationFrame(frame);
    const pose = xrFrame.getViewerPose(space);
    if (!pose) return;
    const head = { position: [pose.transform.position.x, pose.transform.position.y, pose.transform.position.z], forward: forwardOf(pose.transform.orientation) };
    if (!model) model = panelMatrix(pose.transform);
    live = { left: null, right: null };
    for (const source of session.inputSources) {
      if (!source.hand || !SIDES.includes(source.handedness)) continue;
      const joints = {};
      let all = true;
      for (const [name, jointSpace] of source.hand) {
        const p = xrFrame.getJointPose(jointSpace, space);
        if (!p) { all = false; break; }
        joints[name] = [p.transform.position.x, p.transform.position.y, p.transform.position.z];
      }
      if (all) live[source.handedness] = measure(joints, source.handedness, head);
    }
    const sig = live.left || live.right ? `${signature(live.left)}/${signature(live.right)}` : "-|-";
    if (steady.update(sig, t)) {
      const hands = {};
      for (const side of SIDES) if (live[side]) hands[side] = live[side];
      send(hands, steady.heldMs(t));
    }
    if (t - drawnAt > 200) { drawnAt = t; draw(); }

    const layer = session.renderState.baseLayer;
    gl.bindFramebuffer(gl.FRAMEBUFFER, layer.framebuffer);
    gl.clearColor(0, 0, 0, 0);
    gl.clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT);
    gl.useProgram(g.prog);
    gl.bindTexture(gl.TEXTURE_2D, g.tex);
    if (dirty) {
      gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, panel);
      dirty = false;
    }
    gl.enable(gl.BLEND);
    gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA);
    gl.bindBuffer(gl.ARRAY_BUFFER, g.buf);
    gl.enableVertexAttribArray(g.attr);
    gl.vertexAttribPointer(g.attr, 2, gl.FLOAT, false, 0, 0);
    gl.uniformMatrix4fv(g.loc("M"), false, model);
    for (const view of pose.views) {
      const vp = layer.getViewport(view);
      gl.viewport(vp.x, vp.y, vp.width, vp.height);
      gl.uniformMatrix4fv(g.loc("P"), false, view.projectionMatrix);
      gl.uniformMatrix4fv(g.loc("V"), false, view.transform.inverse.matrix);
      gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
    }
  });
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
