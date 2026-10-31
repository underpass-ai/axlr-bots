// A mirror lake under the aurora: the conductor and the orchestra stand on
// still water at night. Above, one sky: a deep gradient, stars and a faint
// milky way, a low moon, mountain ridges in haze, and the aurora, three
// curtains of light ahead and to the left that wave slowly and burn brighter
// the more of the orchestra plays, pulsing on the beat. Below, the same sky
// again: the water is the sky function evaluated on the reflected ray, with
// ripples that grow with the music, the moon's glitter path, and under each
// lantern its reflection, a streak of its own colour and brightness, with a
// flame at the lantern's top. A cue from the conductor runs across the water
// as a ring; stop.all stills it and dims the aurora at once.
//
// Everything is one GLSL function (SKY) shared by the sky dome and the water,
// so the reflection is exact and free: no second render, no shadows, no
// post-processing, no textures but two small canvases. Five draw calls for
// the world (sky, water, streaks, flames, motes). Colours are linear and go
// through the renderer's ACES and sRGB (the lanterns' pipeline) at a lower
// exposure, so the lanterns keep their copper.
//
// Same contract as meadow.js and zen.js: (scene, {hall}) → {group, frame(dt)},
// group.userData.fog and .exposure for the page. With the hall it follows the
// orchestra: its yaw (the aurora stays ahead-left of the podium), the lanterns
// (their reflections), the state (how many play, the beat, a cut).
import { THREE } from "/static/room.js";

const V = (x, y, z) => new THREE.Vector3(x, y, z);

// --- the sky, one function -------------------------------------------------

// Directions are in the hall's frame: -z ahead of the conductor, +x to their
// right, y up. The moon sits ahead-right, the aurora ahead-left.
const AURORA_AT = -2.0;  // azimuth (atan(z, x)): -π/2 is ahead, -π is left
const MOON = { az: -0.9, el: 0.21, r: 0.034 };

const SKY = `
  #define PI 3.14159265
  #define TAU 6.28318531
  uniform float time;
  uniform float music;     // 0..1: how much of the orchestra plays
  uniform float beat;      // a pulse on each beat while it plays
  uniform float hush;      // 1 right after stop.all, back to 0 in a second
  uniform vec3 moonDir;    // unit, in the hall's frame
  uniform vec3 moonU;      // the moon's disc: two axes across it
  uniform vec3 moonV;
  uniform vec3 moonTint;
  uniform float gain;      // the world's light, raised as the exposure is lowered for the lanterns

  const vec3 HORIZON = vec3(0.024, 0.042, 0.080);
  const vec3 MID = vec3(0.009, 0.020, 0.052);
  const vec3 ZENITH = vec3(0.001, 0.002, 0.006);
  const vec3 FAR_MTN = vec3(0.014, 0.022, 0.040);
  const vec3 NEAR_MTN = vec3(0.0025, 0.0035, 0.007);
  const vec3 GREEN = vec3(0.10, 1.00, 0.38);
  const vec3 PINK = vec3(1.00, 0.36, 0.55);
  const vec3 VIOLET = vec3(0.42, 0.18, 1.00);
  const vec3 MW_NORMAL = vec3(-0.2, 0.45, -0.87);

  float hash12(vec2 p) { vec3 q = fract(vec3(p.xyx) * 0.1031); q += dot(q, q.yzx + 33.33); return fract((q.x + q.y) * q.z); }
  float hash13(vec3 p) { p = fract(p * 0.1031); p += dot(p, p.zyx + 31.32); return fract((p.x + p.y) * p.z); }
  float vnoise(vec2 p) {
    vec2 i = floor(p), f = fract(p);
    f = f * f * (3.0 - 2.0 * f);
    float a = hash12(i), b = hash12(i + vec2(1.0, 0.0)), c = hash12(i + vec2(0.0, 1.0)), d = hash12(i + vec2(1.0, 1.0));
    return mix(mix(a, b, f.x), mix(c, d, f.x), f.y);
  }
  float fbm3(vec2 p) { return 0.5 * vnoise(p) + 0.25 * vnoise(p * 2.03 + 7.7) + 0.125 * vnoise(p * 4.11 + 3.1); }
  float wrapPi(float a) { return mod(a + PI, TAU) - PI; }

  // One curtain: its lower edge waves along the sky with the azimuth and the
  // time; above the edge it fades out over h. Pink at the edge, green, violet
  // at the top. k is how far up the curtain the point is.
  vec3 curtain(float u, float el, float base, float h, float seed, float gain) {
    float lo = base + 0.11 * (vnoise(vec2(u * 1.4 + seed, time * 0.045 + seed)) - 0.5)
                    + 0.04 * (vnoise(vec2(u * 4.3 - seed, time * 0.08)) - 0.5);
    float k = (el - lo) / h;
    float I = smoothstep(0.0, 0.05, k) * exp(-k * 2.2) * gain;
    vec3 col = mix(mix(PINK, GREEN, smoothstep(0.0, 0.24, k)), VIOLET, smoothstep(0.35, 1.3, k));
    return col * I;
  }

  vec3 aurora(float u, float el) {
    float win = smoothstep(1.75, 0.7, abs(wrapPi(u - ${AURORA_AT.toFixed(3)})));
    if (win < 0.002 || el < 0.03) return vec3(0.0);
    float r1 = vnoise(vec2(u * 34.0 + time * 0.22, el * 1.2 + time * 0.03));
    float rays = 0.25 + 0.75 * r1 * r1 * (0.6 + 0.8 * vnoise(vec2(u * 9.0 - time * 0.1, el * 0.7)));
    float drift = 0.55 + 0.45 * vnoise(vec2(u * 3.5 + time * 0.1, el * 2.0 - time * 0.035));
    float A = 1.5 * (0.30 + 0.70 * music) * (1.0 + 0.35 * beat) * (1.0 - 0.85 * hush);
    vec3 c = curtain(u, el, 0.10, 0.13, 1.3, 1.00)
           + curtain(u, el, 0.17, 0.18, 7.1, 0.55)
           + curtain(u, el, 0.30, 0.30, 3.7, 0.32);
    return c * win * rays * drift * A;
  }

  // The whole sky in a unit direction d (the hall's frame).
  vec3 sky(vec3 d) {
    float el = d.y;
    float az = atan(d.z, d.x);
    vec2 ring = vec2(cos(az), sin(az));
    vec3 c = mix(HORIZON, MID, smoothstep(-0.02, 0.15, el));
    c = mix(c, ZENITH, smoothstep(0.18, 0.7, el));
    // the milky way, a faint tilted band
    float mw = dot(d, MW_NORMAL);
    c += vec3(0.05, 0.06, 0.09) * exp(-mw * mw * 28.0) * (0.4 + 0.6 * vnoise(d.xz * 6.0 + d.y * 3.0)) * smoothstep(0.0, 0.2, el);
    // stars: one in a few of the cells of a grid over the directions, soft, twinkling
    vec3 cell = floor(d * 110.0), f = fract(d * 110.0) - 0.5;
    float h = hash13(cell);
    vec3 jitter = (vec3(hash13(cell + 1.0), hash13(cell + 2.0), hash13(cell + 3.0)) - 0.5) * 0.4;
    float star = step(0.974, h) * (1.0 - smoothstep(0.0, 0.33, length(f - jitter)));
    float tw = 0.65 + 0.35 * sin(time * (1.5 + 2.0 * h) + h * 50.0);
    c += vec3(0.8, 0.85, 1.0) * star * tw * (0.5 + 1.4 * fract(h * 7.0)) * smoothstep(0.0, 0.12, el);
    // the moon, low ahead-right, with its maria and a halo
    float cosAng = clamp(dot(d, moonDir), -1.0, 1.0);
    float ang = acos(cosAng);
    float disc = 1.0 - smoothstep(${(MOON.r * 0.9).toFixed(4)}, ${MOON.r.toFixed(4)}, ang);
    vec2 m = vec2(dot(d, moonU), dot(d, moonV)) / ${MOON.r.toFixed(4)};
    float face = 0.72 + 0.28 * fbm3(m * 2.5 + 5.0);
    c += moonTint * (1.7 * disc * face + 0.30 * exp(-ang * 18.0) + 0.06 * exp(-ang * 4.0));
    // the aurora, behind the mountains
    c += aurora(az, el);
    // two ridges of mountains, the far one in haze, the near one almost black
    float farRidge = 0.025 + 0.11 * fbm3(ring * 3.4 + 3.0) + 0.02 * vnoise(ring * 11.0);
    float nearRidge = 0.05 * fbm3(ring * 3.9 + 11.0) + 0.01 * vnoise(ring * 17.0);
    float moonside = 0.5 + 0.5 * dot(normalize(vec3(d.x, 0.0, d.z)), normalize(vec3(moonDir.x, 0.0, moonDir.z)));
    vec3 farCol = mix(FAR_MTN, HORIZON, 0.22) + 0.02 * moonside * moonTint;
    c = mix(c, farCol, 1.0 - smoothstep(farRidge - 0.003, farRidge + 0.003, el));
    c = mix(c, NEAR_MTN, 1.0 - smoothstep(nearRidge - 0.002, nearRidge + 0.002, el));
    return c;
  }

  // World → the hall's frame (its yaw undone).
  uniform float cy;
  uniform float sy;
  vec3 toHall(vec3 d) { return vec3(d.x * cy - d.z * sy, d.y, d.x * sy + d.z * cy); }
`;

const VERTEX = `
  varying vec3 vW;
  void main() {
    vec4 w = modelMatrix * vec4(position, 1.0);
    vW = w.xyz;
    gl_Position = projectionMatrix * viewMatrix * w;
  }`;

const SKY_FRAGMENT = `
  ${SKY}
  varying vec3 vW;
  void main() {
    vec3 d = toHall(normalize(vW - cameraPosition));
    gl_FragColor = vec4(sky(d) * gain, 1.0);
    #include <tonemapping_fragment>
    #include <colorspace_fragment>
  }`;

// The water: the sky on the reflected ray. Ripples are three wave trains
// (analytic normals) plus the conductor's ring; they fade with distance so
// the far water is a true mirror and never shimmers.
const WATER_FRAGMENT = `
  ${SKY}
  uniform float ripple;    // 0..1: how much the water moves
  uniform vec3 waveC;      // the cue's ring: centre, radius, strength
  uniform float waveR;
  uniform float waveA;
  varying vec3 vW;
  const vec3 DEEP = vec3(0.0015, 0.003, 0.007);

  vec3 waterNormal(vec2 p, float dist) {
    float a = 0.0012 + 0.0045 * ripple;
    vec2 d1 = vec2(0.80, 0.60), d2 = vec2(-0.50, 0.87), d3 = vec2(0.20, -0.98);
    vec2 g = a * 1.6 * cos(dot(p, d1) * 1.6 + time * 0.5) * d1
           + a * 2.7 * 0.6 * cos(dot(p, d2) * 2.7 - time * 0.8 + 2.0) * d2
           + a * 4.5 * 0.35 * cos(dot(p, d3) * 4.5 + time * 1.2 + 5.0) * d3;
    vec2 toC = p - waveC.xz;
    float dc = length(toC);
    float q = (dc - waveR) / 0.25;
    float env = waveA * 0.02 * exp(-q * q);
    float ph = (dc - waveR) * 25.0;
    g += env * (-8.0 * q * cos(ph) - 25.0 * sin(ph)) * toC / max(dc, 0.001);
    g *= 1.0 - smoothstep(5.0, 40.0, dist);
    return normalize(vec3(-g.x, 1.0, -g.y));
  }

  void main() {
    vec3 toP = vW - cameraPosition;
    float dist = length(toP);
    vec3 Vd = toP / dist;
    vec3 n = waterNormal(vW.xz, dist);
    vec3 R = reflect(Vd, n);
    R.y = max(R.y, 0.004);
    vec3 Rl = toHall(normalize(R));
    vec3 refl = sky(Rl);
    float cosT = max(dot(-Vd, n), 0.0);
    float F = mix(0.22, 1.0, pow(1.0 - cosT, 3.5));
    vec3 c = mix(DEEP, refl, F);
    float glint = max(dot(Rl, moonDir), 0.0);
    c += moonTint * pow(glint, 900.0) * 2.0 * (0.2 + 0.8 * ripple) * (1.0 - smoothstep(5.0, 40.0, dist));
    gl_FragColor = vec4(c * gain, 1.0);
    #include <tonemapping_fragment>
    #include <colorspace_fragment>
  }`;

// --- small textures -----------------------------------------------------------

// A soft streak: a lantern's reflection on the water, long along y.
function streakTexture() {
  const c = document.createElement("canvas");
  c.width = 64; c.height = 256;
  const g = c.getContext("2d");
  g.translate(32, 128);
  g.scale(32, 128);
  // the canvas's top is the lantern's foot, its bottom the mirrored top of the lantern: bright there, a tail towards the foot
  const grad = g.createRadialGradient(0, 0.45, 0, 0, 0.45, 1.0);
  grad.addColorStop(0, "rgba(255,255,255,1)");
  grad.addColorStop(0.25, "rgba(255,255,255,0.6)");
  grad.addColorStop(0.6, "rgba(255,255,255,0.15)");
  grad.addColorStop(1, "rgba(255,255,255,0)");
  g.fillStyle = grad;
  g.fillRect(-1, -1, 2, 2);
  const tail = g.createLinearGradient(0, -1, 0, 0.45);
  tail.addColorStop(0, "rgba(255,255,255,0)");
  tail.addColorStop(1, "rgba(255,255,255,0.35)");
  g.globalCompositeOperation = "lighter";
  g.fillStyle = tail;
  g.fillRect(-0.35, -1, 0.7, 1.45);
  const t = new THREE.CanvasTexture(c);
  t.colorSpace = THREE.SRGBColorSpace;
  return t;
}

// A round glow: the flame at a lantern's top.
function flameTexture() {
  const c = document.createElement("canvas");
  c.width = c.height = 64;
  const g = c.getContext("2d");
  const grad = g.createRadialGradient(32, 32, 0, 32, 32, 32);
  grad.addColorStop(0, "rgba(255,255,255,1)");
  grad.addColorStop(0.12, "rgba(255,255,255,0.9)");
  grad.addColorStop(0.3, "rgba(255,255,255,0.35)");
  grad.addColorStop(0.6, "rgba(255,255,255,0.08)");
  grad.addColorStop(1, "rgba(255,255,255,0)");
  g.fillStyle = grad;
  g.fillRect(0, 0, 64, 64);
  const t = new THREE.CanvasTexture(c);
  t.colorSpace = THREE.SRGBColorSpace;
  return t;
}

export function aurora(scene, { hall = null, camera = null } = {}) {
  const group = new THREE.Group();
  group.userData.fog = null;  // the sky function carries its own haze; fog would grey the mirror
  group.userData.exposure = 0.6;
  scene.fog = null;

  // the moon's place and the disc's axes, in the hall's frame
  const moonDir = V(Math.cos(MOON.el) * Math.cos(MOON.az), Math.sin(MOON.el), Math.cos(MOON.el) * Math.sin(MOON.az)).normalize();
  const moonU = V(0, 1, 0).cross(moonDir).normalize(), moonV = moonDir.clone().cross(moonU).normalize();
  const shared = {
    time: { value: 0 }, music: { value: 0 }, beat: { value: 0 }, hush: { value: 0 },
    moonDir: { value: moonDir }, moonU: { value: moonU }, moonV: { value: moonV }, moonTint: { value: new THREE.Color(1.0, 0.93, 0.80) },
    cy: { value: 1 }, sy: { value: 0 }, gain: { value: 1.4 },
  };

  // the sky dome
  const sky = new THREE.Mesh(new THREE.SphereGeometry(150, 48, 24), new THREE.ShaderMaterial({
    side: THREE.BackSide, depthWrite: false, fog: false, uniforms: shared, vertexShader: VERTEX, fragmentShader: SKY_FRAGMENT,
  }));
  sky.renderOrder = -100;
  sky.frustumCulled = false;
  group.add(sky);

  // the lake: a disc out to the far plane, just under the hall's floor marks
  const water = new THREE.Mesh(new THREE.CircleGeometry(200, 96), new THREE.ShaderMaterial({
    fog: false, vertexShader: VERTEX, fragmentShader: WATER_FRAGMENT,
    uniforms: { ...shared, ripple: { value: 0 }, waveC: { value: V(0, 0, 0) }, waveR: { value: -10 }, waveA: { value: 0 } },
  }));
  water.rotation.x = -Math.PI / 2;
  water.position.y = -0.015;
  water.frustumCulled = false;
  group.add(water);

  // the light that reaches the lanterns' bodies and the drawn hands: the sky's blue, a warm lamp at the podium
  const podium = new THREE.PointLight(0xffd090, 1.2, 1.2, 2);
  podium.position.set(0, 1.35, 0.15);
  group.add(new THREE.HemisphereLight(0x4a6a9a, 0x0a0c12, 0.3), podium);

  // each lantern's reflection: one streak on the water, from its foot towards the eye
  const CHAIRS = 40;
  const streakGeo = new THREE.PlaneGeometry(1, 1);
  streakGeo.rotateX(-Math.PI / 2);  // flat; its long axis is local z
  const streaks = new THREE.InstancedMesh(streakGeo, new THREE.MeshBasicMaterial({ map: streakTexture(), color: 0xffffff, transparent: true,
    blending: THREE.AdditiveBlending, depthWrite: false }), CHAIRS);
  streaks.frustumCulled = false;
  streaks.count = 0;
  streaks.position.y = -0.012;
  group.add(streaks);
  // and its flame: a bright core at the top of each lantern that plays, so the lanterns read as lamps on the lake
  const flames = new THREE.InstancedMesh(new THREE.PlaneGeometry(1, 1), new THREE.MeshBasicMaterial({ map: flameTexture(), color: 0xffffff, transparent: true,
    blending: THREE.AdditiveBlending, depthWrite: false }), CHAIRS);
  flames.frustumCulled = false;
  flames.count = 0;
  group.add(flames);
  const m4 = new THREE.Matrix4(), q = new THREE.Quaternion(), face = new THREE.Quaternion(), s3 = V(1, 1, 1), p3 = V(0, 0, 0), eye = V(0, 1.6, 0), foot = V(0, 0, 0), tint = new THREE.Color();
  const UP = V(0, 1, 0);
  function reflectLanterns() {
    if (!hall || !Object.keys(hall.lanterns).length) { streaks.count = flames.count = 0; return; }
    if (camera) { camera.getWorldPosition(eye); camera.getWorldQuaternion(face); } else { eye.set(0, 1.6, 0); face.identity(); }
    const scale = hall.root.scale.x;
    let n = 0;
    for (const id in hall.lanterns) {
      if (n >= CHAIRS) break;
      const L = hall.lanterns[id], now = L.now;
      const b = now.glow * 0.9 + now.body * 0.25;
      if (b < 0.02) continue;
      L.group.getWorldPosition(foot);
      const dx = eye.x - foot.x, dz = eye.z - foot.z, D = Math.hypot(dx, dz) || 0.001;
      const H = (0.04 + 1.18 * L.body.scale.y) * scale;          // the lantern's height, as drawn
      const len = Math.max(0.05, D * H / (Math.max(eye.y, 0.2) + H)) * 1.9;  // where its mirror image falls on the water
      const w = (0.12 + 0.16 * L.body.scale.y) * scale;
      q.setFromAxisAngle(UP, Math.atan2(dx, dz));
      m4.compose(p3.set(foot.x + (dx / D) * len * 0.5, 0, foot.z + (dz / D) * len * 0.5), q, s3.set(w, 1, len));
      streaks.setMatrixAt(n, m4);
      streaks.setColorAt(n, tint.copy(now.color).multiplyScalar(b * 0.9));
      const r = (0.28 + 0.26 * L.body.scale.y) * scale * (0.6 + 0.4 * now.glow);
      m4.compose(p3.set(foot.x, H + 0.02 * scale, foot.z), face, s3.set(r, r, 1));
      flames.setMatrixAt(n, m4);
      flames.setColorAt(n, tint.copy(now.color).multiplyScalar(now.glow * 1.5 + now.body * 0.25));
      n++;
    }
    streaks.count = flames.count = n;
    streaks.instanceMatrix.needsUpdate = flames.instanceMatrix.needsUpdate = true;
    if (streaks.instanceColor) streaks.instanceColor.needsUpdate = true;
    if (flames.instanceColor) flames.instanceColor.needsUpdate = true;
  }

  // motes: a little frost in the air round the podium, catching the lanterns' light
  const MOTES = 110;
  const motePos = new Float32Array(MOTES * 3), motePhase = new Float32Array(MOTES);
  const motes = [];
  let seed = 977;
  const rnd = () => { seed = (seed * 16807) % 2147483647; return (seed - 1) / 2147483646; };
  for (let i = 0; i < MOTES; i++) {
    const a = rnd() * Math.PI * 2, r = 0.8 + rnd() * 6.0;
    motes.push({ x: Math.cos(a) * r, y: 0.1 + rnd() * 1.9, z: Math.sin(a) * r, vx: (rnd() - 0.5) * 0.06, vz: (rnd() - 0.5) * 0.06, w: 0.3 + rnd() * 0.7 });
    motePhase[i] = rnd() * Math.PI * 2;
  }
  const moteGeo = new THREE.BufferGeometry();
  moteGeo.setAttribute("position", new THREE.BufferAttribute(motePos, 3));
  moteGeo.setAttribute("phase", new THREE.BufferAttribute(motePhase, 1));
  const moteMesh = new THREE.Points(moteGeo, new THREE.ShaderMaterial({ transparent: true, depthWrite: false, blending: THREE.AdditiveBlending,
    uniforms: { time: shared.time, music: shared.music },
    vertexShader: `attribute float phase; uniform float time; varying float a;
      void main() { vec4 mv = modelViewMatrix * vec4(position, 1.0); float tw = 0.5 + 0.5 * sin(time * 0.9 + phase); a = tw * tw;
        gl_PointSize = (1.0 + 1.6 * a) * 3.2 / max(-mv.z, 0.4); gl_Position = projectionMatrix * mv; }`,
    fragmentShader: `varying float a; uniform float music; void main() { float d = length(gl_PointCoord - 0.5) * 2.0;
        gl_FragColor = vec4(vec3(1.0, 0.9, 0.75) * (0.35 + 0.65 * a) * (0.5 + 0.5 * music) * 1.4, smoothstep(1.0, 0.2, d) * 0.45);
        #include <tonemapping_fragment>
        #include <colorspace_fragment> }` }));
  moteMesh.frustumCulled = false;
  group.add(moteMesh);
  function drift(t, dt) {
    for (let i = 0; i < MOTES; i++) {
      const f = motes[i];
      f.x += f.vx * dt; f.z += f.vz * dt;
      const r = Math.hypot(f.x, f.z);
      if (r < 0.6 || r > 7.0) { f.vx = -f.vx; f.vz = -f.vz; }
      motePos[i * 3] = f.x;
      motePos[i * 3 + 1] = f.y + 0.12 * Math.sin(t * 0.4 * f.w + i);
      motePos[i * 3 + 2] = f.z;
    }
    moteGeo.attributes.position.needsUpdate = true;
  }

  scene.add(group);

  // --- what the music does to the world ----------------------------------------
  let t = 0, music = 0, ripple = 0;
  return {
    group,
    frame(dt) {
      t += dt;
      shared.time.value = t;
      const st = hall ? hall.state : null, playing = st && st.status === "playing";
      const count = st ? st.sections.reduce((n, x) => n + x.playing, 0) : 0;
      const total = st ? st.sections.reduce((n, x) => n + x.chairs.length, 0) || 36 : 36;
      const wantMusic = playing ? 0.25 + 0.75 * count / total : 0;
      music += (wantMusic - music) * (1 - Math.exp(-dt * 1.2));
      const bpb = hall && hall.score ? hall.score.beatsPerBar : 4;
      const phase = hall ? (hall.position() * bpb) % 1 : 0;
      const beat = playing ? Math.pow(Math.max(0, Math.cos(phase * Math.PI * 2)), 3) : 0;
      const since = hall && hall.snap ? (performance.now() - hall.snap) / 1000 : 9;
      const hush = Math.max(0, 1 - since / 1.4);
      shared.music.value = music;
      shared.beat.value = beat;
      shared.hush.value = hush;
      const wantRipple = (0.15 + 0.85 * music) * (1 - hush);
      ripple += (wantRipple - ripple) * (1 - Math.exp(-dt * (hush > 0 ? 8 : 1.5)));
      water.material.uniforms.ripple.value = ripple;
      if (hall) {
        shared.cy.value = Math.cos(hall.root.rotation.y);
        shared.sy.value = Math.sin(hall.root.rotation.y);
        podium.position.x = hall.root.position.x; podium.position.z = hall.root.position.z + 0.15;  // the podium is where the conductor stands, not the room's centre
        moteMesh.position.x = hall.root.position.x; moteMesh.position.z = hall.root.position.z;
        const w = hall.waves[hall.waves.length - 1];
        if (w) {
          const age = (performance.now() - w.t0) / 1000;
          water.material.uniforms.waveC.value.copy(hall.root.position);
          water.material.uniforms.waveR.value = (0.4 + age * 4.2) * hall.root.scale.x;
          water.material.uniforms.waveA.value = Math.max(0, 1 - age / 1.3) * (w.strong ? 1.3 : 0.8);
        } else water.material.uniforms.waveA.value = 0;
      }
      reflectLanterns();
      drift(t, dt);
    },
  };
}
