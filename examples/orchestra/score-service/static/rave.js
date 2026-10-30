// A rave in an abandoned factory, 2000s style: the conductor is the DJ, the
// orchestra the band, in a concrete hall with columns, trusses and holes in
// the roof where the moon comes through. Over the stage a truss of moving
// heads throws cones of coloured light through the haze; from the back,
// green laser sheets and a cage of thin beams sweep over the crowd; a VJ
// screen behind the orchestra plays a tunnel of squares with the section
// bars as an equalizer and the tempo in big digits; two speaker stacks pump
// on the beat; a strobe fires on the first beat of each bar; the floor is wet
// concrete whose puddles mirror the screen; the crowd behind the DJ is dark
// silhouettes that bob on the beat with glow sticks in the air. stop.all is
// a blackout: lights, screen and lasers die in a second, the crowd stands
// still, only the exit signs stay.
//
// The whole world is built in the hall's frame (-z: the stage, +z: the
// crowd) and the group follows the hall, so the stage is ahead of wherever
// the conductor entered looking. One LED/VJ function is shared by the screen
// and the floor's puddles (an exact reflection, no second render). No
// shadows, no post-processing; the textures are three small canvases. The
// models (ravers, trusses, cabinets, fixtures, columns, windows, lamps) are
// primitives merged into one geometry each and instanced. About twenty-five
// draw calls. Same contract as aurora.js: (scene, {hall,
// camera}) → {group, frame(dt)}, group.userData.fog and .exposure.
//
// A strobe is a strobe: it flashes once a bar (every 1.6 s at 152 bpm), at
// a third of white, never faster.
import { THREE } from "/static/room.js";

const V = (x, y, z) => new THREE.Vector3(x, y, z);
const Y = V(0, 1, 0);

// --- shared GLSL ---------------------------------------------------------------

const NOISE = `
  float hash12(vec2 p) { vec3 q = fract(vec3(p.xyx) * 0.1031); q += dot(q, q.yzx + 33.33); return fract((q.x + q.y) * q.z); }
  float vnoise(vec2 p) {
    vec2 i = floor(p), f = fract(p);
    f = f * f * (3.0 - 2.0 * f);
    float a = hash12(i), b = hash12(i + vec2(1.0, 0.0)), c = hash12(i + vec2(0.0, 1.0)), d = hash12(i + vec2(1.0, 1.0));
    return mix(mix(a, b, f.x), mix(c, d, f.x), f.y);
  }
  float fbm3(vec2 p) { return 0.5 * vnoise(p) + 0.25 * vnoise(p * 2.03 + 7.7) + 0.125 * vnoise(p * 4.11 + 3.1); }
  vec3 hsv(float h, float s, float v) { vec3 c = clamp(abs(mod(h * 6.0 + vec3(0.0, 4.0, 2.0), 6.0) - 3.0) - 1.0, 0.0, 1.0); return v * mix(vec3(1.0), c, s); }`;

// The show's state, shared by every shader.
const SHOW = `
  uniform float time;
  uniform float beat;      // a pulse on each beat
  uniform float bar;       // the bar's phase, 0..1
  uniform float flash;     // the strobe, 1 at the flash, gone in 80 ms
  uniform float music;     // 0..1: how much of the orchestra plays
  uniform float hush;      // 1 right after stop.all, back to 0 in a second
  uniform float bars[7];   // the sections, 0..1
  uniform sampler2D digits; // the tempo and the bar, drawn in DotGothic16
`;

// The VJ screen: a tunnel of squares that turns and pulses, hue drifting over
// the bars; the seven sections as an equalizer along the bottom; the digits
// across the top; a projector's pixel grid over it all. uv in 0..1.
const VJ = `
  vec3 vj(vec2 uv) {
    float live = 1.0 - hush;
    vec2 p = (uv - 0.5) * vec2(14.0 / 6.0, 1.0);
    float rot = time * 0.25 + 0.4 * sin(time * 0.11);
    p = mat2(cos(rot), -sin(rot), sin(rot), cos(rot)) * p;
    float d = max(abs(p.x), abs(p.y));
    float ld = log(d * 9.0 + 1.0);                                     // perspective: the squares crowd towards the far end
    float ring = fract(ld * 2.4 - time * 1.4 - beat * 0.25);
    float edge = smoothstep(0.0, 0.06, ring) * smoothstep(0.30, 0.06, ring);
    float hue = fract(bar * 0.25 + time * 0.02 + ld * 0.12);
    vec3 tunnel = hsv(hue, 0.95, 1.0) * edge * (0.7 + 0.7 * beat) * smoothstep(0.0, 0.06, d);
    tunnel += hsv(hue + 0.5, 0.8, 1.0) * smoothstep(0.05, 0.0, d) * (0.6 + beat);  // the bright far end
    // the equalizer: seven bars across the bottom, lit to each section's level
    float col = floor(uv.x * 7.0);
    float inBar = step(0.08, fract(uv.x * 7.0)) * step(fract(uv.x * 7.0), 0.92);
    float level = bars[int(clamp(col, 0.0, 6.0))];
    float h = uv.y / 0.22;
    float cells = step(0.15, fract(h * 8.0));
    vec3 eq = mix(vec3(0.0, 1.0, 0.5), vec3(1.0, 0.1, 0.4), h) * step(h, level) * step(uv.y, 0.22) * inBar * cells * (0.8 + 0.6 * beat);
    // the digits across the top
    vec4 t = texture2D(digits, vec2(uv.x, (uv.y - 0.86) / 0.13));
    vec3 digit = vec3(0.7, 1.0, 0.2) * t.a * step(0.86, uv.y) * (0.8 + 0.4 * beat);
    float eqBand = step(uv.y, 0.23), topBand = step(0.85, uv.y);
    vec3 c = tunnel * (1.0 - 0.85 * eqBand) * (1.0 - 0.9 * topBand) + eq + digit;
    // the projector's grid
    vec2 g = fract(uv * vec2(140.0, 60.0));
    c *= 0.75 + 0.25 * smoothstep(0.5, 0.2, length(g - 0.5));
    c *= live * (0.6 + 0.4 * music) + 0.0;
    c += vec3(0.25, 0.0, 0.0) * hush * step(0.5, fract(time * 1.0)) * step(abs(uv.y - 0.5), 0.02);  // standby: a red line, blinking
    c += vec3(1.0) * flash * 0.35;
    return c;
  }`;

// --- small textures -----------------------------------------------------------

function soft(w, h, draw) {
  const c = document.createElement("canvas");
  c.width = w; c.height = h;
  draw(c.getContext("2d"), w, h);
  const t = new THREE.CanvasTexture(c);
  t.colorSpace = THREE.SRGBColorSpace;
  return t;
}
const smokeTexture = () => soft(128, 128, (g, w, h) => {
  let seed = 31;
  const rnd = () => { seed = (seed * 16807) % 2147483647; return (seed - 1) / 2147483646; };
  for (let i = 0; i < 14; i++) {
    const x = 24 + rnd() * 80, y = 24 + rnd() * 80, r = 18 + rnd() * 30;
    const grad = g.createRadialGradient(x, y, 0, x, y, r);
    grad.addColorStop(0, "rgba(255,255,255,0.35)");
    grad.addColorStop(1, "rgba(255,255,255,0)");
    g.fillStyle = grad;
    g.fillRect(0, 0, w, h);
  }
});
const beamTexture = () => soft(4, 128, (g, w, h) => {  // bright at the base (v = 1), gone at the far end
  const grad = g.createLinearGradient(0, 0, 0, h);
  grad.addColorStop(0, "rgba(255,255,255,0)");
  grad.addColorStop(0.5, "rgba(255,255,255,0.45)");
  grad.addColorStop(1, "rgba(255,255,255,1)");
  g.fillStyle = grad;
  g.fillRect(0, 0, w, h);
});

// --- built geometry: primitives merged into one, so each kind is one instanced draw ----

// Merge geometries (each with an optional matrix) into one non-indexed geometry.
function merge(parts) {
  const pos = [], nor = [], uv = [];
  for (const [geo, m] of parts) {
    const g = (geo.index ? geo.toNonIndexed() : geo).clone();
    if (m) g.applyMatrix4(m);
    pos.push(...g.attributes.position.array);
    nor.push(...g.attributes.normal.array);
    const u = g.attributes.uv ? g.attributes.uv.array : new Float32Array((g.attributes.position.count) * 2);
    uv.push(...u);
  }
  const out = new THREE.BufferGeometry();
  out.setAttribute("position", new THREE.Float32BufferAttribute(pos, 3));
  out.setAttribute("normal", new THREE.Float32BufferAttribute(nor, 3));
  out.setAttribute("uv", new THREE.Float32BufferAttribute(uv, 2));
  return out;
}
const at = (x, y, z, rx = 0, ry = 0, rz = 0, sx = 1, sy = 1, sz = 1) =>
  new THREE.Matrix4().compose(V(x, y, z), new THREE.Quaternion().setFromEuler(new THREE.Euler(rx, ry, rz)), V(sx, sy, sz));
const box = (w, h, d) => new THREE.BoxGeometry(w, h, d);
const cyl = (r, h, n = 8, r2 = r) => new THREE.CylinderGeometry(r, r2, h, n);

// A raver, 1.75 m: legs, a torso, a head and two arms, in three poses. hands: where the glow sticks go.
function raver(pose) {
  const parts = [
    [cyl(0.075, 0.82, 7, 0.06), at(-0.11, 0.41, 0)], [cyl(0.075, 0.82, 7, 0.06), at(0.11, 0.41, 0)],
    [new THREE.SphereGeometry(0.2, 10, 8), at(0, 1.1, 0, 0, 0, 0, 1.0, 1.55, 0.65)],   // the torso
    [box(0.44, 0.1, 0.22), at(0, 1.4, 0)],                                               // the shoulders
    [new THREE.SphereGeometry(0.11, 10, 8), at(0, 1.62, 0)],                              // the head
  ];
  const hands = [];
  const arm = (side, up) => {
    if (up) { parts.push([cyl(0.045, 0.62, 6), at(side * 0.3, 1.7, 0.02, 0, 0, -side * 0.3)]); hands.push([side * 0.39, 2.0, 0.03]); }
    else parts.push([cyl(0.045, 0.6, 6), at(side * 0.26, 1.1, 0.02, 0, 0, side * 0.08)]);
  };
  arm(-1, pose !== "down"); arm(1, pose === "up");
  return { geo: merge(parts), hands };
}
// A 2 m box-truss segment along +x, 0.4 m square: four chords, diagonals on each face, end posts.
function trussSegment() {
  const parts = [];
  for (const y of [-0.2, 0.2]) for (const z of [-0.2, 0.2]) parts.push([cyl(0.025, 2.0, 6), at(0, y, z, 0, 0, Math.PI / 2)]);
  const L = Math.hypot(2.0, 0.4), a = Math.atan2(0.4, 2.0);
  for (const z of [-0.2, 0.2]) parts.push([cyl(0.014, L, 5), at(0, 0, z, 0, 0, Math.PI / 2 - a)], [cyl(0.014, L, 5), at(0, 0, z, 0, 0, Math.PI / 2 + a)]);
  for (const y of [-0.2, 0.2]) parts.push([cyl(0.014, L, 5), at(0, y, 0, a, 0, Math.PI / 2)], [cyl(0.014, L, 5), at(0, y, 0, -a, 0, Math.PI / 2)]);
  for (const z of [-0.2, 0.2]) parts.push([cyl(0.014, 0.4, 5), at(-1.0, 0, z)], [cyl(0.014, 0.4, 5), at(1.0, 0, z)]);
  return merge(parts);
}
// An I-beam segment along +z, 8 m: two flanges and a web.
const iBeam = () => merge([[box(0.32, 0.03, 8), at(0, 0.2, 0)], [box(0.32, 0.03, 8), at(0, -0.2, 0)], [box(0.03, 0.37, 8), at(0, 0, 0)]]);
// A concrete column: plinth, shaft, capital, with a steel collar.
const column = () => merge([[box(1.4, 0.5, 1.4), at(0, 0.25, 0)], [box(0.9, 12, 0.9), at(0, 6, 0)], [box(1.3, 0.45, 1.3), at(0, 10.75, 0)], [box(1.0, 0.08, 1.0), at(0, 4.0, 0)]]);
// A moving head hung from the truss: the clamp and yoke on top, the head along -y (the beam), a lens at its end.
const fixture = () => merge([
  [box(0.32, 0.07, 0.32), at(0, -0.035, 0)],
  [box(0.05, 0.42, 0.14), at(-0.17, -0.3, 0)], [box(0.05, 0.42, 0.14), at(0.17, -0.3, 0)],
  [cyl(0.11, 0.34, 12), at(0, -0.42, 0)], [cyl(0.13, 0.08, 12, 0.11), at(0, -0.62, 0)],
]);
// A sub cabinet (1.6 × 1.1 × 1.3): the box with corner caps and handles; its two drivers (with dust caps) are a second geometry, in grey.
const subCabinet = () => merge([
  [box(1.6, 1.1, 1.3), at(0, 0, 0)],
  ...[-1, 1].flatMap((x) => [-1, 1].flatMap((y) => [-1, 1].map((z) => [box(0.12, 0.12, 0.12), at(x * 0.78, y * 0.53, z * 0.63)]))),
  [box(0.05, 0.14, 0.3), at(-0.82, 0.15, 0)], [box(0.05, 0.14, 0.3), at(0.82, 0.15, 0)],
]);
const subDrivers = () => merge([
  [cyl(0.32, 0.05, 20, 0.30), at(-0.4, 0, 0.66, Math.PI / 2)], [cyl(0.32, 0.05, 20, 0.30), at(0.4, 0, 0.66, Math.PI / 2)],   // the rims
  [cyl(0.29, 0.08, 20, 0.14), at(-0.4, 0, 0.62, -Math.PI / 2)], [cyl(0.29, 0.08, 20, 0.14), at(0.4, 0, 0.62, -Math.PI / 2)],  // the cones, dished in
  [new THREE.SphereGeometry(0.09, 10, 8), at(-0.4, 0, 0.62)], [new THREE.SphereGeometry(0.09, 10, 8), at(0.4, 0, 0.62)],
]);
// A top cabinet (1.2 × 1.3 × 1.0): the box; its horn (a flared square mouth) and driver are a second geometry, in grey.
const topCabinet = () => box(1.2, 1.3, 1.0);
const topDrivers = () => merge([
  [new THREE.ConeGeometry(0.46, 0.5, 4, 1, true), at(0, 0.3, 0.5, Math.PI / 2, 0, Math.PI / 4)],
  [cyl(0.24, 0.05, 16, 0.22), at(0, -0.35, 0.51, Math.PI / 2)], [cyl(0.21, 0.07, 16, 0.1), at(0, -0.35, 0.49, -Math.PI / 2)], [new THREE.SphereGeometry(0.06, 8, 6), at(0, -0.35, 0.5)],
]);
// A factory lamp hanging from the trusses: cord, shade, bulb.
const hangingLamp = () => merge([[cyl(0.012, 1.6, 4), at(0, -0.8, 0)], [new THREE.ConeGeometry(0.42, 0.34, 14, 1, true), at(0, -1.75, 0, Math.PI, 0, 0)], [new THREE.SphereGeometry(0.07, 8, 6), at(0, -1.7, 0)]]);
// A window frame (3 × 4): the border and the mullions.
const windowFrame = () => merge([
  [box(0.1, 4.0, 0.1), at(-1.5, 0, 0)], [box(0.1, 4.0, 0.1), at(1.5, 0, 0)], [box(3.0, 0.1, 0.1), at(0, 2.0, 0)], [box(3.0, 0.1, 0.1), at(0, -2.0, 0)],
  [box(0.06, 4.0, 0.06), at(-0.5, 0, 0)], [box(0.06, 4.0, 0.06), at(0.5, 0, 0)],
  [box(3.0, 0.06, 0.06), at(0, 1.0, 0)], [box(3.0, 0.06, 0.06), at(0, 0, 0)], [box(3.0, 0.06, 0.06), at(0, -1.0, 0)],
]);

export function rave(scene, { hall = null, camera = null } = {}) {
  const group = new THREE.Group();
  const fog = new THREE.FogExp2(0x04040a, 0.026);
  group.userData.fog = fog;
  group.userData.exposure = 0.55;
  scene.fog = fog;
  let seed = 4242;
  const rnd = () => { seed = (seed * 16807) % 2147483647; return (seed - 1) / 2147483646; };

  // the digits: the tempo and the bar, redrawn when they change
  const digitsCanvas = document.createElement("canvas");
  digitsCanvas.width = 1024; digitsCanvas.height = 96;
  const digitsTex = new THREE.CanvasTexture(digitsCanvas);
  digitsTex.colorSpace = THREE.SRGBColorSpace;
  let digitsText = null;
  function drawDigits(text) {
    if (text === digitsText) return;
    digitsText = text;
    const g = digitsCanvas.getContext("2d");
    g.clearRect(0, 0, 1024, 96);
    g.fillStyle = "#fff";
    g.font = "64px DotGothic16, monospace";
    g.textAlign = "center";
    g.textBaseline = "middle";
    g.fillText(text, 512, 50);
    digitsTex.needsUpdate = true;
  }
  drawDigits("· · ·");

  const show = {
    time: { value: 0 }, beat: { value: 0 }, bar: { value: 0 }, flash: { value: 0 }, music: { value: 0 }, hush: { value: 0 },
    bars: { value: new Float32Array(7) }, digits: { value: digitsTex },
  };
  const HEADS = 8, SHAFTS = 3, JETS = 2;
  const spots = Array.from({ length: HEADS }, () => new THREE.Vector4(0, 0, 0, 0));  // where each head's beam hits the floor: x, z, radius, on
  const headColors = [0xff9a1f, 0xffffff, 0x2a7dff, 0xff2bd6, 0xffffff, 0x2a7dff, 0xff9a1f, 0xff2bd6].map((h) => new THREE.Color(h));
  const camL = { value: V(0, 1.6, 0) };

  // --- concrete: the floor with its puddles, the shell, the columns --------------
  const concreteVertex = `
    varying vec3 vL; varying vec3 vN;
    #include <fog_pars_vertex>
    void main() {
      vec4 local = vec4(position, 1.0);
      vec3 n = normal;
      #ifdef USE_INSTANCING
        local = instanceMatrix * local;
        n = mat3(instanceMatrix) * n;
      #endif
      vL = local.xyz;
      vN = normalize(n);
      vec4 mvPosition = modelViewMatrix * local;
      gl_Position = projectionMatrix * mvPosition;
      #include <fog_vertex>
    }`;
  const floorMat = new THREE.ShaderMaterial({
    fog: true,
    uniforms: THREE.UniformsUtils.merge([THREE.UniformsLib.fog, {}]),
    vertexShader: concreteVertex,
    fragmentShader: `
      ${NOISE} ${SHOW} ${VJ}
      uniform vec3 camL;
      uniform vec4 spots[${HEADS}];
      uniform vec3 spotColors[${HEADS}];
      varying vec3 vL; varying vec3 vN;
      #include <fog_pars_fragment>
      void main() {
        vec2 p = vL.xz;
        float grain = 0.8 + 0.4 * vnoise(p * 9.0) + 0.2 * vnoise(p * 37.0);
        float patches = 0.7 + 0.5 * fbm3(p * 0.25 + 3.0);
        vec3 c = vec3(0.011, 0.011, 0.013) * grain * patches;
        // the puddles: where the floor is wet it mirrors the screen
        float wet = smoothstep(0.52, 0.62, fbm3(p * 0.33 + 11.0));
        vec3 Vd = normalize(vL - camL);
        vec3 R = vec3(Vd.x, -Vd.y, Vd.z);
        vec3 refl = vec3(0.0);
        if (R.z < -0.001) {
          float t = (-8.5 - vL.z) / R.z;
          vec3 hit = vL + R * t;
          vec2 uv = vec2((hit.x + 7.0) / 14.0, (hit.y - 1.2) / 6.0);
          if (uv.x > 0.0 && uv.x < 1.0 && uv.y > 0.0 && uv.y < 1.0) refl = vj(uv);
        }
        float fres = mix(0.25, 1.0, pow(1.0 - max(-Vd.y, 0.0), 3.0));
        c = mix(c, c * 0.4 + refl * fres * 1.2, wet);
        // the beams' pools on the floor, and their glints in the wet
        for (int i = 0; i < ${HEADS}; i++) {
          float d = distance(p, spots[i].xy);
          float pool = smoothstep(spots[i].z, spots[i].z * 0.2, d) * spots[i].w;
          c += spotColors[i] * pool * (0.10 + 0.35 * wet);
        }
        c += vec3(1.0) * flash * 0.12 * (0.5 + wet);
        c *= 1.0 - 0.6 * hush;
        gl_FragColor = vec4(c, 1.0);
        #include <fog_fragment>
        #include <tonemapping_fragment>
        #include <colorspace_fragment>
      }`,
  });
  Object.assign(floorMat.uniforms, show, { camL, spots: { value: spots }, spotColors: { value: headColors } });
  const floor = new THREE.Mesh(new THREE.PlaneGeometry(44, 60), floorMat);
  floor.rotation.x = -Math.PI / 2;
  floor.position.set(0, -0.015, 4);
  group.add(floor);

  const concreteMat = new THREE.ShaderMaterial({
    fog: true, side: THREE.DoubleSide,
    uniforms: THREE.UniformsUtils.merge([THREE.UniformsLib.fog, {}]),
    vertexShader: concreteVertex,
    fragmentShader: `
      ${NOISE} ${SHOW}
      uniform float glow;
      varying vec3 vL; varying vec3 vN;
      #include <fog_pars_fragment>
      void main() {
        vec2 p = abs(vN.y) > 0.5 ? vL.xz : (abs(vN.x) > 0.5 ? vL.zy : vL.xy);
        float grain = 0.75 + 0.5 * vnoise(p * 6.0) + 0.25 * vnoise(p * 23.0);
        float streaks = 0.6 + 0.6 * vnoise(vec2(p.x * 7.0, p.y * 0.4));          // rust runs down the walls
        float rust = smoothstep(0.55, 0.8, fbm3(p * 0.7 + 5.0)) * (1.0 - abs(vN.y));
        vec3 c = mix(vec3(0.014, 0.014, 0.017) * grain * streaks, vec3(0.05, 0.022, 0.009) * grain, rust * 0.6);
        c *= 0.6 + 0.4 * smoothstep(12.0, 0.0, vL.y);                              // darker up high
        // the screen's light on the back wall and the columns near it
        float near = smoothstep(14.0, 1.0, length(vL - vec3(0.0, 4.0, -8.5)));
        c += vec3(0.05, 0.03, 0.08) * near * near * (0.4 + 0.6 * beat) * (1.0 - hush) * glow;
        float wash = smoothstep(9.0, 0.0, vL.y) * smoothstep(-14.0, -24.0, vL.z);   // uplights along the back wall
        c += hsv(fract(bar * 0.25 + time * 0.02 + 0.6), 0.9, 1.0) * wash * 0.07 * (0.5 + 0.5 * beat) * (1.0 - hush) * glow;
        c += vec3(1.0) * flash * 0.10;
        c *= 1.0 - 0.5 * hush;
        gl_FragColor = vec4(c, 1.0);
        #include <fog_fragment>
        #include <tonemapping_fragment>
        #include <colorspace_fragment>
      }`,
  });
  Object.assign(concreteMat.uniforms, show, { glow: { value: 1 } });
  const shell = new THREE.Mesh(new THREE.BoxGeometry(44, 12, 60), concreteMat);
  shell.position.set(0, 6, 4);
  group.add(shell);
  // the columns with their plinths and capitals, I-beam girders between them, lattice trusses under the roof, dead lamps
  const cols = [];
  for (const x of [-15, -7.5, 7.5, 15]) for (const z of [-18, -10, -2, 6, 14, 22]) cols.push([x, z]);
  const columns = new THREE.InstancedMesh(column(), concreteMat, cols.length);
  const m4 = new THREE.Matrix4(), q = new THREE.Quaternion(), s3 = V(1, 1, 1), p3 = V(0, 0, 0);
  cols.forEach(([x, z], i) => { m4.compose(p3.set(x, 0, z), q.identity(), s3.set(1, 1, 1)); columns.setMatrixAt(i, m4); });
  const steel = new THREE.MeshStandardMaterial({ color: 0x24262c, roughness: 0.55, metalness: 0.85 });
  const girderList = [];
  for (const x of [-15, -7.5, 7.5, 15]) for (let z = -22; z < 26; z += 8) girderList.push([x, 10.75, z + 4]);
  const girders = new THREE.InstancedMesh(iBeam(), steel, girderList.length);
  girderList.forEach(([x, y, z], i) => { m4.compose(p3.set(x, y, z), q.identity(), s3.set(1, 1, 1)); girders.setMatrixAt(i, m4); });
  const segs = [];
  for (const z of [-22, -14, -6, 2, 10, 18, 26]) for (let x = -21; x < 22; x += 2) segs.push([x, 11.4, z, 0]);            // across
  for (const x of [-11, 0, 11]) for (let z = -25; z < 34; z += 2) segs.push([x, 11.0, z, Math.PI / 2]);                     // along
  for (let x = -7; x < 8; x += 2) segs.push([x, 7.0, -3.2, 0]);                                                                 // the lighting truss over the stage
  const trusses = new THREE.InstancedMesh(trussSegment(), steel, segs.length);
  segs.forEach(([x, y, z, ry], i) => { m4.compose(p3.set(x, y, z), q.setFromAxisAngle(Y, ry), s3.set(1, 1, 1)); trusses.setMatrixAt(i, m4); });
  const lampList = [];
  for (const x of [-11, 0, 11]) for (const z of [-20, -12, -4, 4, 12, 20, 28]) lampList.push([x + 0.6, 10.8, z]);
  const lamps = new THREE.InstancedMesh(hangingLamp(), steel, lampList.length);
  lampList.forEach(([x, y, z], i) => { m4.compose(p3.set(x, y, z), q.setFromAxisAngle(Y, (i * 0.7) % 6.28), s3.set(1, 1, 1)); lamps.setMatrixAt(i, m4); });
  const housings = new THREE.InstancedMesh(box(0.5, 0.3, 0.3), steel, 2);
  [-7.6, 7.6].forEach((x, i) => { m4.compose(p3.set(x, 6.9, -3.2), q.identity(), s3.set(1, 1, 1)); housings.setMatrixAt(i, m4); });
  group.add(columns, girders, trusses, lamps, housings);
  // tall windows high on the long walls: frames, and panes of which some are broken and let the moon in
  const winList = [];
  for (const side of [-1, 1]) for (let z = -21; z < 32; z += 6) winList.push([side * 21.9, 8.0, z, side]);
  const frames = new THREE.InstancedMesh(windowFrame(), steel, winList.length);
  const paneMat = new THREE.ShaderMaterial({
    fog: true, side: THREE.DoubleSide, uniforms: THREE.UniformsUtils.merge([THREE.UniformsLib.fog, {}]),
    vertexShader: `varying vec2 vUv; varying float vSeed;
      #include <fog_pars_vertex>
      void main() { vUv = uv; vec4 mvPosition = modelViewMatrix * instanceMatrix * vec4(position, 1.0); vSeed = instanceMatrix[3].z * 0.37 + instanceMatrix[3].x; gl_Position = projectionMatrix * mvPosition;
        #include <fog_vertex>
      }`,
    fragmentShader: `${NOISE}
      varying vec2 vUv; varying float vSeed;
      #include <fog_pars_fragment>
      void main() {
        vec2 cell = floor(vUv * vec2(3.0, 4.0));
        float h = hash12(cell + vSeed);
        float broken = step(0.62, h);                                           // this pane is gone: the night sky through it
        vec2 f = fract(vUv * vec2(3.0, 4.0));
        float shard = broken * step(0.3 + 0.5 * hash12(cell + 9.0 + vSeed), vnoise(f * 3.0 + cell));  // a jagged edge of glass left
        vec3 sky = vec3(0.10, 0.14, 0.26) * (0.8 + 0.4 * vnoise(vUv * 5.0 + vSeed));
        vec3 glass = vec3(0.012, 0.014, 0.02) * (0.8 + 0.5 * vnoise(vUv * 40.0 + vSeed)) + vec3(0.03, 0.035, 0.05) * smoothstep(0.9, 1.0, 1.0 - abs(f.x - 0.5) * 2.0);
        vec3 c = mix(glass, sky, broken * (1.0 - shard));
        gl_FragColor = vec4(c, 1.0);
        #include <fog_fragment>
        #include <tonemapping_fragment>
        #include <colorspace_fragment>
      }`,
  });
  const panes = new THREE.InstancedMesh(new THREE.PlaneGeometry(3, 4), paneMat, winList.length);
  winList.forEach(([x, y, z, side], i) => {
    m4.compose(p3.set(x, y, z), q.setFromAxisAngle(Y, side > 0 ? -Math.PI / 2 : Math.PI / 2), s3.set(1, 1, 1));
    frames.setMatrixAt(i, m4);
    m4.compose(p3.set(x - side * 0.03, y, z), q, s3.set(1, 1, 1));
    panes.setMatrixAt(i, m4);
  });
  group.add(frames, panes);
  // the exit signs, the only light a blackout leaves
  const exitMat = new THREE.MeshBasicMaterial({ color: 0x1aff5a, fog: false });
  for (const x of [-21.6, 21.6]) { const e = new THREE.Mesh(new THREE.BoxGeometry(0.1, 0.22, 0.6), exitMat); e.position.set(x, 2.6, 22); group.add(e); }

  // --- the stage: the screen, the speaker stacks, the strobes ----------------------
  const screenMat = new THREE.ShaderMaterial({
    fog: false, uniforms: show,
    vertexShader: "varying vec2 vUv; void main() { vUv = uv; gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0); }",
    fragmentShader: `${NOISE} ${SHOW} ${VJ}
      varying vec2 vUv;
      void main() { gl_FragColor = vec4(vj(vUv), 1.0);
        #include <tonemapping_fragment>
        #include <colorspace_fragment> }`,
  });
  const screen = new THREE.Mesh(new THREE.PlaneGeometry(14, 6), screenMat);
  screen.position.set(0, 4.2, -8.5);
  group.add(screen);
  const frame_ = new THREE.Mesh(new THREE.BoxGeometry(14.6, 6.6, 0.3), steel);
  frame_.position.set(0, 4.2, -8.7);
  group.add(frame_);
  // two stacks a side of the stage: three subs and two tops, black cabinets with their drivers and horns; the subs pump on the beat
  const cabinetMat = new THREE.MeshStandardMaterial({ color: 0x0b0b0d, roughness: 0.85, metalness: 0.1 });
  const subs = [], tops = [];
  for (const side of [-1, 1]) {
    for (let i = 0; i < 3; i++) subs.push({ x: side * 5.8, y: 0.55 + i * 1.1, z: -5.0, ry: -side * 0.18 });
    tops.push({ x: side * 5.8, y: 3.95, z: -5.0, ry: -side * 0.18 }, { x: side * 5.8, y: 5.2, z: -5.0, ry: -side * 0.18 });
  }
  const driverMat = new THREE.MeshStandardMaterial({ color: 0x4a4a52, roughness: 0.45, metalness: 0.6 });
  const subMesh = new THREE.InstancedMesh(subCabinet(), cabinetMat, subs.length), subDriverMesh = new THREE.InstancedMesh(subDrivers(), driverMat, subs.length);
  const topMesh = new THREE.InstancedMesh(topCabinet(), cabinetMat, tops.length), topDriverMesh = new THREE.InstancedMesh(topDrivers(), driverMat, tops.length);
  tops.forEach((c, i) => { m4.compose(p3.set(c.x, c.y, c.z), q.setFromAxisAngle(Y, c.ry), s3.set(1, 1, 1)); topMesh.setMatrixAt(i, m4); topDriverMesh.setMatrixAt(i, m4); });
  const ledMat = new THREE.MeshBasicMaterial({ color: 0x1aff5a, fog: false });
  const leds = new THREE.InstancedMesh(box(0.05, 0.05, 0.03), ledMat, subs.length + tops.length);
  function pump(beat) {
    subs.forEach((c, i) => {
      const k = 1 + 0.025 * beat;
      m4.compose(p3.set(c.x, c.y, c.z), q.setFromAxisAngle(Y, c.ry), s3.set(k, 1, k));
      subMesh.setMatrixAt(i, m4);
      subDriverMesh.setMatrixAt(i, m4);
    });
    subMesh.instanceMatrix.needsUpdate = subDriverMesh.instanceMatrix.needsUpdate = true;
    [...subs, ...tops].forEach((c, i) => { m4.compose(p3.set(c.x + Math.sign(c.x) * -0.62, c.y + 0.4, c.z + 0.66), q.setFromAxisAngle(Y, c.ry), s3.set(1, 1, 1)); leds.setMatrixAt(i, m4); });
    leds.instanceMatrix.needsUpdate = true;
  }
  pump(0);
  group.add(subMesh, subDriverMesh, topMesh, topDriverMesh, leds);
  const strobeMat = new THREE.MeshBasicMaterial({ color: 0xffffff, fog: false, transparent: true, opacity: 0 });
  const strobes = [];
  for (const x of [-7.6, 7.6]) { const s = new THREE.Mesh(new THREE.PlaneGeometry(0.5, 0.3), strobeMat); s.position.set(x, 6.9, -3.0); strobes.push(s); group.add(s); }

  // --- the light: cones from the heads, shafts of moon, laser sheets and beams -------
  const coneGeo = new THREE.ConeGeometry(1, 1, 24, 1, true);
  coneGeo.translate(0, -0.5, 0);  // the apex at the origin, the cone down -y, scaled per instance
  const coneMat = new THREE.ShaderMaterial({
    transparent: true, depthWrite: false, blending: THREE.AdditiveBlending, side: THREE.DoubleSide, fog: false,
    uniforms: show,
    vertexShader: `varying vec2 vUv; varying vec3 vN; varying vec3 vCol; varying vec3 vW;
      void main() { vUv = uv; vCol = instanceColor; vec4 w = modelMatrix * instanceMatrix * vec4(position, 1.0); vW = w.xyz;
        vN = normalize(mat3(modelMatrix * instanceMatrix) * normal); gl_Position = projectionMatrix * viewMatrix * w; }`,
    fragmentShader: `${NOISE} ${SHOW}
      varying vec2 vUv; varying vec3 vN; varying vec3 vCol; varying vec3 vW;
      void main() {
        vec3 Vd = normalize(vW - cameraPosition);
        float body = smoothstep(0.0, 0.7, abs(dot(vN, Vd)));                 // thick through the middle, soft at the rim
        float along = pow(vUv.y, 1.3);                                        // bright at the head, thinning to the floor
        float haze = 0.75 + 0.35 * vnoise(vec2(vUv.x * 6.0, vUv.y * 9.0 - time * 1.5));
        gl_FragColor = vec4(vCol * body * along * haze * 0.6, 1.0);
        #include <tonemapping_fragment>
        #include <colorspace_fragment> }`,
  });
  const cones = new THREE.InstancedMesh(coneGeo, coneMat, HEADS + SHAFTS + JETS);
  cones.frustumCulled = false;
  const fixtures = new THREE.InstancedMesh(fixture(), steel, HEADS);
  const lensMat = new THREE.MeshBasicMaterial({ color: 0xffffff, fog: false });
  const lenses = new THREE.InstancedMesh(new THREE.CircleGeometry(0.1, 16), lensMat, HEADS);
  group.add(fixtures, lenses);
  const headAt = Array.from({ length: HEADS }, (_, i) => V(-6.3 + i * 1.8, 6.4, -3.2));
  const shaftFrom = [V(-12, 12, 10), V(9, 12, -16), V(16, 12, 20)];
  const dir = V(0, -1, 0), target = V(0, 0, 0), moon = new THREE.Color(0x4a66aa).multiplyScalar(0.35);
  const tmpColor = new THREE.Color();
  const jetAt = [V(-3.6, 0.2, -1.2), V(3.6, 0.2, -1.2)], white = new THREE.Color(0xdfe8ff);
  function aimCones(t, beat, on, co2 = 0) {
    for (let i = 0; i < HEADS; i++) {
      const ph = i * 0.9, bank = i % 2 ? 1 : -1;
      // each head draws its own slow figure over the crowd and the band, and snaps a little on the beat
      target.set(headAt[i].x * 0.6 + 7.0 * Math.sin(t * 0.37 + ph) * bank, 0, 3.0 + 9.0 * (0.5 + 0.5 * Math.sin(t * 0.23 + ph * 1.7)) - 10.0 * (i % 3 === 0 ? 1 : 0) * (0.5 + 0.5 * Math.cos(t * 0.19 + ph)));
      dir.copy(target).sub(headAt[i]);
      const len = dir.length();
      dir.normalize();
      q.setFromUnitVectors(V(0, -1, 0), dir);
      const r = 0.9 + 0.5 * Math.sin(t * 0.5 + ph);
      m4.compose(headAt[i], q, s3.set(r, len, r));
      cones.setMatrixAt(i, m4);
      const strength = on * (0.7 + 0.5 * beat) * (0.5 + 0.5 * Math.max(0, Math.sin(t * 0.8 + i * 1.3)) + 0.25);
      cones.setColorAt(i, tmpColor.copy(headColors[i]).multiplyScalar(strength));
      spots[i].set(target.x, target.z, r * 1.1, strength);
      m4.compose(headAt[i], q, s3.set(1, 1, 1));                       // the fixture turns with its beam
      fixtures.setMatrixAt(i, m4);
      m4.compose(p3.copy(headAt[i]).addScaledVector(dir, 0.67), q.multiply(new THREE.Quaternion().setFromAxisAngle(V(1, 0, 0), Math.PI / 2)), s3.set(1, 1, 1));
      lenses.setMatrixAt(i, m4);
      lenses.setColorAt(i, tmpColor.copy(headColors[i]).multiplyScalar(0.3 + 2.5 * strength));
    }
    for (let i = 0; i < SHAFTS; i++) {
      q.setFromUnitVectors(V(0, -1, 0), dir.set(0.15, -1, 0.1).normalize());
      m4.compose(shaftFrom[i], q, s3.set(1.4, 13, 1.4));
      cones.setMatrixAt(HEADS + i, m4);
      cones.setColorAt(HEADS + i, moon);
    }
    for (let i = 0; i < JETS; i++) {  // the CO2: a white plume straight up, gone in a second
      q.setFromUnitVectors(V(0, -1, 0), dir.set(i ? 0.12 : -0.12, 1, 0.05).normalize());
      m4.compose(jetAt[i], q, s3.set(0.5 + 1.2 * (1 - co2), 3.0 + 5.0 * (1 - co2 * 0.5), 0.5 + 1.2 * (1 - co2)));
      cones.setMatrixAt(HEADS + SHAFTS + i, m4);
      cones.setColorAt(HEADS + SHAFTS + i, tmpColor.copy(white).multiplyScalar(2.2 * co2 * co2));
    }
    cones.instanceMatrix.needsUpdate = true;
    cones.instanceColor.needsUpdate = true;
    fixtures.instanceMatrix.needsUpdate = lenses.instanceMatrix.needsUpdate = true;
    if (lenses.instanceColor) lenses.instanceColor.needsUpdate = true;
  }
  aimCones(0, 0, 1);
  group.add(cones);

  // laser sheets: three fans of green (and one red) light from the back of the stage over the crowd, waving
  const SHEETS = 4;
  const sheetGeo = (() => {
    const g = new THREE.BufferGeometry();
    g.setAttribute("position", new THREE.Float32BufferAttribute([0, 0, 0, -11, 0, 36, 11, 0, 36], 3));  // the apex, two far corners; along +z
    g.setAttribute("uv", new THREE.Float32BufferAttribute([0.5, 0, 0, 1, 1, 1], 2));
    g.computeVertexNormals();
    return g;
  })();
  const sheetMat = new THREE.ShaderMaterial({
    transparent: true, depthWrite: false, blending: THREE.AdditiveBlending, side: THREE.DoubleSide, fog: false, uniforms: show,
    vertexShader: `varying vec2 vUv; varying vec3 vCol; varying vec3 vW;
      void main() { vUv = uv; vCol = instanceColor; vec4 w = modelMatrix * instanceMatrix * vec4(position, 1.0); vW = w.xyz; gl_Position = projectionMatrix * viewMatrix * w; }`,
    fragmentShader: `${NOISE} ${SHOW}
      varying vec2 vUv; varying vec3 vCol; varying vec3 vW;
      void main() {
        float along = smoothstep(0.0, 0.05, vUv.y) * pow(1.0 - vUv.y, 0.9);
        float smoke = 0.45 + 0.75 * fbm3(vec2(vUv.x * 9.0 + time * 0.3, vUv.y * 14.0 - time * 0.9));
        float lines = 0.6 + 0.4 * smoothstep(0.3, 0.9, vnoise(vec2(vUv.x * 60.0, time * 0.4)));  // the scanner's own lines in the fan
        gl_FragColor = vec4(vCol * along * smoke * lines * 0.22, 1.0);
        #include <tonemapping_fragment>
        #include <colorspace_fragment> }`,
  });
  const sheets = new THREE.InstancedMesh(sheetGeo, sheetMat, SHEETS);
  sheets.frustumCulled = false;
  const sheetFrom = [V(-6.5, 6.6, -8.0), V(6.5, 6.6, -8.0), V(0, 7.2, -8.0), V(0, 6.2, -8.0)];
  const sheetColors = [0x39ff14, 0x39ff14, 0x39ff14, 0xff2020].map((h) => new THREE.Color(h));
  const e = new THREE.Euler();
  function waveSheets(t, beat, on) {
    for (let i = 0; i < SHEETS; i++) {
      const ph = i * 1.9;
      const pitch = -0.10 + 0.14 * Math.sin(t * 0.45 + ph) + 0.03 * beat;        // up and down over the heads
      const yaw = 0.25 * Math.sin(t * 0.31 + ph * 0.7) * (i === 2 ? 0 : 1);
      const roll = i === 3 ? 0.6 * Math.sin(t * 0.7) : 0.08 * Math.sin(t * 0.9 + ph);
      q.setFromEuler(e.set(pitch, yaw, roll, "YXZ"));
      m4.compose(sheetFrom[i], q, s3.set(1, 1, 1));
      sheets.setMatrixAt(i, m4);
      const gate = i === 3 ? (Math.floor(t / 6.4) % 2 ? 1 : 0) : 1;  // the red one comes and goes
      sheets.setColorAt(i, tmpColor.copy(sheetColors[i]).multiplyScalar(on * gate * (0.6 + 0.6 * beat)));
    }
    sheets.instanceMatrix.needsUpdate = true;
    sheets.instanceColor.needsUpdate = true;
  }
  waveSheets(0, 0, 1);
  group.add(sheets);
  // the cage: thin beams from two laser heads, fanned and scanning
  const BEAMS = 26;  // two fans of eight, a tunnel of ten
  const beamGeo = new THREE.CylinderGeometry(0.018, 0.018, 1, 6, 1, true);
  beamGeo.translate(0, 0.5, 0);  // base at the origin, along +y
  const beams = new THREE.InstancedMesh(beamGeo, new THREE.MeshBasicMaterial({ map: beamTexture(), color: 0xffffff, transparent: true, blending: THREE.AdditiveBlending,
    depthWrite: false, fog: false, side: THREE.DoubleSide }), BEAMS);
  beams.frustumCulled = false;
  const laserFrom = [V(-5.5, 6.9, -8.2), V(5.5, 6.9, -8.2), V(0, 7.3, -8.2)];
  function scanBeams(t, beat, on) {
    for (let i = 0; i < BEAMS; i++) {
      if (i < 16) {  // the two fans
        const head = i < 8 ? 0 : 1, k = (i % 8) / 7 - 0.5;
        const spread = 0.55 + 0.35 * Math.sin(t * 0.6 + head * 2.0);
        const yaw = k * spread + 0.3 * Math.sin(t * 0.8 + head * 1.5) + (head ? -0.25 : 0.25);
        const pitch = -0.08 + 0.1 * Math.sin(t * 1.1 + k * 3.0 + head) + 0.04 * beat;
        dir.set(Math.sin(yaw) * Math.cos(pitch), Math.sin(pitch), Math.cos(yaw) * Math.cos(pitch));
        m4.compose(laserFrom[head], q.setFromUnitVectors(Y, dir), s3.set(1, 38, 1));
        const steps = Math.floor(t * 2.0 + i * 0.37) % 5 === 0 ? 0.2 : 1.0;  // a beam drops out now and then
        beams.setColorAt(i, tmpColor.set(head ? 0x39ff14 : 0x2ad8ff).multiplyScalar(on * steps * (0.9 + 0.6 * beat)));
      } else {  // the tunnel: ten beams in a ring round the crowd, breathing on the beat, slowly turning
        const a = ((i - 16) / 10) * Math.PI * 2 + t * 0.35;
        const r = 0.16 + 0.05 * beat + 0.04 * Math.sin(t * 0.7);
        dir.set(Math.cos(a) * r, Math.sin(a) * r * 0.6 - 0.02, 1).normalize();
        m4.compose(laserFrom[2], q.setFromUnitVectors(Y, dir), s3.set(1, 40, 1));
        beams.setColorAt(i, tmpColor.set(Math.floor(t / 3.2) % 2 ? 0xff2020 : 0x39ff14).multiplyScalar(on * (0.8 + 0.6 * beat)));
      }
      beams.setMatrixAt(i, m4);
    }
    beams.instanceMatrix.needsUpdate = true;
    beams.instanceColor.needsUpdate = true;
  }
  scanBeams(0, 0, 1);
  group.add(beams);

  // --- haze: a few drifting smoke billboards --------------------------------------
  const SMOKE = 9;
  const smoke = new THREE.InstancedMesh(new THREE.PlaneGeometry(1, 1), new THREE.MeshBasicMaterial({ map: smokeTexture(), color: 0x3a4458, transparent: true,
    opacity: 0.6, depthWrite: false, blending: THREE.AdditiveBlending, fog: false }), SMOKE);
  smoke.frustumCulled = false;
  const puffs = Array.from({ length: SMOKE }, (_, i) => ({ x: (rnd() - 0.5) * 22, y: 1.0 + rnd() * 3.5, z: -4 + rnd() * 22, w: 5 + rnd() * 4, h: 3 + rnd() * 2, vx: (rnd() - 0.5) * 0.25, rot: rnd() * 6.28, vr: (rnd() - 0.5) * 0.04 }));
  const face = new THREE.Quaternion(), spin = new THREE.Quaternion();
  function driftSmoke(dt) {
    if (camera) camera.getWorldQuaternion(face);
    for (let i = 0; i < SMOKE; i++) {
      const s = puffs[i];
      s.x += s.vx * dt; s.rot += s.vr * dt;
      if (s.x > 13) s.x = -13; else if (s.x < -13) s.x = 13;
      spin.setFromAxisAngle(V(0, 0, 1), s.rot);
      // the billboard faces the eye, in the world; the group's yaw is undone so the quad still faces the eye
      m4.compose(p3.set(s.x, s.y, s.z), q.copy(groupInv).multiply(face).multiply(spin), s3.set(s.w, s.h, 1));
      smoke.setMatrixAt(i, m4);
    }
    smoke.instanceMatrix.needsUpdate = true;
  }
  const groupInv = new THREE.Quaternion();
  group.add(smoke);

  // --- the crowd: figures that bob on the beat, glow sticks in their raised hands -------
  const CROWD = 150;
  const people = [];
  while (people.length < CROWD) {
    const x = (rnd() - 0.5) * 26, z = 1.5 + rnd() * rnd() * 22;
    if (Math.hypot(x, z) < 2.6) continue;
    if (cols.some(([cx, cz]) => Math.hypot(x - cx, z - cz) < 1.0)) continue;
    const r = rnd();
    people.push({ x, z, h: 0.9 + rnd() * 0.2, phase: rnd() * 6.28, yaw: (rnd() - 0.5) * 0.6, pose: r < 0.3 ? "up" : r < 0.65 ? "one" : "down",
      stick: [0x39ff14, 0xff4fd8, 0xffa21f, 0x2ad8ff, 0xe8ff2f][Math.floor(rnd() * 5)] });
  }
  const skin = new THREE.MeshLambertMaterial({ color: 0x15151c });
  const POSES = ["up", "one", "down"];
  const figures = Object.fromEntries(POSES.map((pose) => {
    const { geo, hands } = raver(pose), list = people.filter((p) => p.pose === pose);
    return [pose, { mesh: new THREE.InstancedMesh(geo, skin, list.length), hands, list }];
  }));
  const nSticks = people.reduce((n, p) => n + figures[p.pose].hands.length, 0);
  const stickPos = new Float32Array(nSticks * 3), stickCol = new Float32Array(nSticks * 3);
  const stickGeo = new THREE.BufferGeometry();
  stickGeo.setAttribute("position", new THREE.BufferAttribute(stickPos, 3));
  stickGeo.setAttribute("color", new THREE.BufferAttribute(stickCol, 3));
  const sticks = new THREE.Points(stickGeo, new THREE.ShaderMaterial({ transparent: true, depthWrite: false, blending: THREE.AdditiveBlending, vertexColors: true,
    uniforms: { hush: show.hush },
    vertexShader: `varying vec3 vC; void main() { vC = color; vec4 mv = modelViewMatrix * vec4(position, 1.0); gl_PointSize = 40.0 / max(-mv.z, 0.5); gl_Position = projectionMatrix * mv; }`,
    fragmentShader: `varying vec3 vC; uniform float hush; void main() { float d = length(gl_PointCoord - 0.5) * 2.0;
      gl_FragColor = vec4(vC * (1.6 - 0.9 * hush), smoothstep(1.0, 0.1, d));
      #include <tonemapping_fragment>
      #include <colorspace_fragment> }` }));
  sticks.frustumCulled = false;
  const hand = V(0, 0, 0);
  function dance(t, phase, live) {
    let k = 0;
    for (const pose of POSES) {
      const { mesh, hands, list } = figures[pose];
      list.forEach((p, i) => {
        const bob = live * 0.09 * Math.abs(Math.sin(Math.PI * phase + p.phase)) + 0.01 * Math.sin(t * 1.3 + p.phase);
        const sway = live * 0.05 * Math.sin(Math.PI * phase * 2.0 + p.phase);
        const turn = p.yaw + live * 0.12 * Math.sin(Math.PI * phase + p.phase * 0.5);
        q.setFromAxisAngle(Y, Math.PI + turn);                               // facing the stage
        m4.compose(p3.set(p.x + sway, bob, p.z), q, s3.set(p.h, p.h, p.h));
        mesh.setMatrixAt(i, m4);
        for (const [hx, hy, hz] of hands) {
          const wave = live * 0.18 * Math.sin(Math.PI * phase + p.phase);
          hand.set(hx + wave, hy, hz).multiplyScalar(p.h).applyQuaternion(q);
          stickPos[k * 3] = p.x + sway + hand.x; stickPos[k * 3 + 1] = bob + hand.y; stickPos[k * 3 + 2] = p.z + hand.z;
          tmpColor.set(p.stick);
          stickCol[k * 3] = tmpColor.r; stickCol[k * 3 + 1] = tmpColor.g; stickCol[k * 3 + 2] = tmpColor.b;
          k++;
        }
      });
      mesh.instanceMatrix.needsUpdate = true;
    }
    stickGeo.attributes.position.needsUpdate = stickGeo.attributes.color.needsUpdate = true;
  }
  dance(0, 0, 1);
  group.add(...POSES.map((pose) => figures[pose].mesh), sticks);

  // --- the lights that reach the lanterns, the hands and the crowd -----------------
  const screenLight = new THREE.PointLight(0x9a40ff, 90, 45, 2);
  screenLight.position.set(0, 4.5, -7.5);
  const stageFront = new THREE.PointLight(0xffe0c0, 14, 14, 2);  // a dim warm wash over the stage front: the cabinets, the band
  stageFront.position.set(0, 5.5, -0.5);
  group.add(stageFront);
  const lamp = new THREE.PointLight(0xffd090, 1.2, 1.2, 2);
  lamp.position.set(0, 1.35, 0.15);
  group.add(new THREE.HemisphereLight(0x303a55, 0x05050a, 0.25), screenLight, lamp);
  // an LED ring round the DJ's podium
  const ring = new THREE.Mesh(new THREE.RingGeometry(0.72, 0.78, 64), new THREE.MeshBasicMaterial({ color: 0x39ff14, transparent: true, opacity: 0.8, fog: false, side: THREE.DoubleSide, depthWrite: false }));
  ring.rotation.x = -Math.PI / 2;
  ring.position.y = 0.004;
  group.add(ring);

  scene.add(group);

  // --- what the music does to the world -------------------------------------------
  let t = 0, music = 0, lastBeatIndex = -1, flashAt = -9;
  const camWorld = V(0, 1.6, 0);
  return {
    group,
    frame(dt) {
      t += dt;
      const st = hall ? hall.state : null, playing = st && st.status === "playing";
      const bpb = hall && hall.score ? hall.score.beatsPerBar : 4;
      const pos = hall ? hall.position() : 0, beatsIn = pos * bpb, phase = beatsIn % 1, barPhase = (pos % 1 + 1) % 1;
      const beat = playing ? Math.pow(Math.max(0, Math.cos(phase * Math.PI * 2)), 3) : 0;
      const count = st ? st.sections.reduce((n, x) => n + x.playing, 0) : 0;
      const total = st ? st.sections.reduce((n, x) => n + x.chairs.length, 0) || 36 : 36;
      music += ((playing ? 0.3 + 0.7 * count / total : 0) - music) * (1 - Math.exp(-dt * 1.5));
      const since = hall && hall.snap ? (performance.now() - hall.snap) / 1000 : 9;
      const hush = Math.max(0, 1 - since / 1.2);
      const live = playing ? 1 - hush : 0;
      // the strobe: once a bar, on its first beat
      const beatIndex = Math.floor(beatsIn);
      if (playing && beatIndex !== lastBeatIndex) { lastBeatIndex = beatIndex; if (beatIndex % bpb === 0 && hush === 0) flashAt = t; }
      const flash = Math.max(0, 1 - (t - flashAt) / 0.08) * 0.35;
      show.time.value = t; show.beat.value = beat; show.bar.value = barPhase; show.flash.value = flash; show.music.value = music; show.hush.value = hush;
      if (st) st.sections.forEach((s, i) => { if (i < 7) { const g = s.chairs.length ? s.chairs.reduce((n, c) => n + (c.gain ?? 1), 0) / s.chairs.length : 1; show.bars.value[i] = (s.playing / Math.max(1, s.chairs.length)) * g; } });
      drawDigits(st && st.status === "playing" ? `${Math.round(st.bpm)} BPM · BAR ${st.bar}` : st && st.status === "ended" ? "END" : "· · ·");
      strobeMat.opacity = flash > 0 ? 1 : 0;
      screenLight.color.setHSL((barPhase * 0.25 + t * 0.02) % 1, 0.8, 0.5);
      screenLight.intensity = (20 + 70 * (0.5 + 0.5 * beat)) * (1 - hush) * (0.5 + 0.5 * music);
      ring.material.color.setHSL((t * 0.1) % 1, 1, 0.55);
      ring.material.opacity = (0.5 + 0.5 * beat) * (1 - hush);
      // the world follows the hall: the stage ahead of where the conductor entered looking
      if (hall) { group.position.set(hall.root.position.x, 0, hall.root.position.z); group.rotation.y = hall.root.rotation.y; }
      group.updateMatrixWorld();
      groupInv.setFromEuler(e.set(0, -group.rotation.y, 0));
      if (camera) { camera.getWorldPosition(camWorld); camL.value.copy(group.worldToLocal(camWorld.clone())); }
      const on = (0.25 + 0.75 * music) * (1 - hush);
      const phraseBars = hall && hall.score ? hall.score.phraseBars || 8 : 8;
      const co2 = live * Math.max(0, 1 - (pos % phraseBars) / 0.9);  // the jets fire as each phrase begins
      aimCones(t, beat, on, co2);
      waveSheets(t, beat, live * (0.4 + 0.6 * music));
      scanBeams(t, beat, live * (0.4 + 0.6 * music));
      pump(beat);
      driftSmoke(dt);
      dance(t, phase, live);
    },
  };
}
