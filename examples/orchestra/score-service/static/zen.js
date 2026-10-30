// A zen world around the orchestra: one giant tree whose canopy fills half
// the sky, a flat meadow that glows yellow-green near the podium and runs
// dark to a hazy horizon, a night sky of stars with a few bright ones, soft
// washes of cloud, a line of white birds crossing slowly, a few fireflies.
// Nothing else: the orchestra's lanterns are the only other light, the
// conductor the only figure. Painted, not lit: the sky, the meadow, the
// canopy and the trunk are unlit shaders with a paper grain in world space,
// so there are no shadows and one point light, a Quest 3S budget. Same
// contract as meadow.js: (scene, {flat}) → {group, frame(dt)}, and
// group.userData.fog and .exposure for the page.
import { THREE } from "/static/room.js";

function seeded(seed) {
  return () => { seed = (seed * 16807) % 2147483647; return (seed - 1) / 2147483646; };
}
const V = (x, y, z) => new THREE.Vector3(x, y, z);
const col = (hex) => new THREE.Color(hex);

function radialTexture(stops) {
  const c = document.createElement("canvas");
  c.width = c.height = 128;
  const g = c.getContext("2d");
  const grad = g.createRadialGradient(64, 64, 0, 64, 64, 64);
  for (const [at, color] of stops) grad.addColorStop(at, color);
  g.fillStyle = grad;
  g.fillRect(0, 0, 128, 128);
  const t = new THREE.CanvasTexture(c);
  t.colorSpace = THREE.SRGBColorSpace;
  return t;
}
const GLOW = radialTexture([[0, "rgba(255,255,255,1)"], [0.25, "rgba(255,255,255,0.5)"], [1, "rgba(255,255,255,0)"]]);
const WASH = radialTexture([[0, "rgba(255,255,255,0.7)"], [0.5, "rgba(255,255,255,0.3)"], [1, "rgba(255,255,255,0)"]]);

// The paper grain every painted surface shares: a hash of the world position.
const GRAIN = `
  float hash(vec3 p) { return fract(sin(dot(p, vec3(127.1, 311.7, 74.7))) * 43758.5453); }
  float grain(vec3 p, float cell) { return (hash(floor(p * cell)) - 0.5) + 0.5 * (hash(floor(p * cell * 3.1)) - 0.5); }`;

// An unlit, fogged, grained material: the fragment body gets vW (world
// position), vN (world normal) and writes `c`.
function painted(uniforms, body, extra = {}) {
  return new THREE.ShaderMaterial({
    fog: true, ...extra,
    uniforms: THREE.UniformsUtils.merge([THREE.UniformsLib.fog, uniforms]),
    vertexShader: `
      varying vec3 vW; varying vec3 vN;
      #include <fog_pars_vertex>
      void main() {
        vec4 w = modelMatrix * vec4(position, 1.0);
        vW = w.xyz;
        vN = normalize(mat3(modelMatrix) * normal);
        vec4 mvPosition = viewMatrix * w;
        gl_Position = projectionMatrix * mvPosition;
        #include <fog_vertex>
      }`,
    fragmentShader: `
      varying vec3 vW; varying vec3 vN;
      #include <fog_pars_fragment>
      ${GRAIN}
      ${Object.keys(uniforms).map((k) => `uniform ${uniforms[k].value.isColor ? "vec3" : "float"} ${k};`).join("\n")}
      void main() {
        vec3 c;
        ${body}
        gl_FragColor = vec4(c, 1.0);
        #include <fog_fragment>
      }`,
  });
}

export function zen(scene, { flat = 7 } = {}) {
  const group = new THREE.Group();
  const rnd = seeded(1791);
  const haze = col(0x4e6a65);
  const fog = new THREE.FogExp2(haze.getHex(), 0.0085);
  group.userData.fog = fog;
  group.userData.exposure = 1.0;
  scene.fog = fog;

  // the sky: a hazy horizon, deep teal, near-black green at the zenith; stars; a few bright ones; washes of cloud
  const sky = new THREE.Mesh(new THREE.SphereGeometry(100, 32, 16), new THREE.ShaderMaterial({
    side: THREE.BackSide, depthWrite: false, fog: false,
    uniforms: { horizon: { value: haze }, mid: { value: col(0x0e2b28) }, top: { value: col(0x020d0b) } },
    vertexShader: "varying vec3 d; void main() { d = normalize(position); gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0); }",
    fragmentShader: `uniform vec3 horizon; uniform vec3 mid; uniform vec3 top; varying vec3 d; ${GRAIN}
      void main() {
        vec3 c = mix(horizon, mid, smoothstep(-0.02, 0.28, d.y));
        c = mix(c, top, smoothstep(0.25, 0.8, d.y));
        c *= 1.0 + 0.07 * grain(d * 60.0, 1.0);
        gl_FragColor = vec4(c, 1.0);
      }`,
  }));
  sky.renderOrder = -100;
  group.add(sky);
  const stars = (() => {
    const n = 1100, pos = [], phase = [], size = [];
    for (let i = 0; i < n; i++) {
      const a = rnd() * Math.PI * 2, e = Math.asin(0.08 + rnd() * 0.92), r = 97;
      pos.push(r * Math.cos(e) * Math.cos(a), r * Math.sin(e), r * Math.cos(e) * Math.sin(a));
      phase.push(rnd() * Math.PI * 2);
      size.push(1.0 + rnd() * rnd() * 2.8);
    }
    const g = new THREE.BufferGeometry();
    g.setAttribute("position", new THREE.Float32BufferAttribute(pos, 3));
    g.setAttribute("phase", new THREE.Float32BufferAttribute(phase, 1));
    g.setAttribute("size", new THREE.Float32BufferAttribute(size, 1));
    return new THREE.Points(g, new THREE.ShaderMaterial({ transparent: true, depthWrite: false, fog: false, uniforms: { time: { value: 0 } },
      vertexShader: `attribute float phase; attribute float size; uniform float time; varying float a;
        void main() { a = 0.5 + 0.5 * sin(time * 0.8 + phase); gl_PointSize = size; gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0); }`,
      fragmentShader: `varying float a; void main() { float d = length(gl_PointCoord - 0.5) * 2.0; gl_FragColor = vec4(0.92, 0.95, 1.0, (0.4 + 0.6 * a) * smoothstep(1.0, 0.3, d)); }` }));
  })();
  group.add(stars);
  for (let i = 0; i < 9; i++) {  // the bright ones, with halos, where the canopy leaves the sky open
    const a = Math.PI * (0.15 + rnd() * 0.7) + (rnd() < 0.5 ? 0 : Math.PI), e = 0.25 + rnd() * 0.9, r = 94;
    const s = new THREE.Sprite(new THREE.SpriteMaterial({ map: GLOW, color: 0xffe9a8, transparent: true, opacity: 0.85, fog: false, blending: THREE.AdditiveBlending, depthWrite: false }));
    s.position.set(r * Math.cos(e) * Math.cos(a), r * Math.sin(e), r * Math.cos(e) * Math.sin(a));
    s.scale.setScalar(5 + rnd() * 6);
    group.add(s);
  }
  const washes = [];
  for (let i = 0; i < 7; i++) {
    const a = rnd() * Math.PI * 2, e = 0.12 + rnd() * 0.45, r = 88;
    const w = new THREE.Sprite(new THREE.SpriteMaterial({ map: WASH, color: 0x8fb0b8, transparent: true, opacity: 0.16 + rnd() * 0.1, fog: false, depthWrite: false }));
    w.position.set(r * Math.cos(e) * Math.cos(a), r * Math.sin(e), r * Math.cos(e) * Math.sin(a));
    w.scale.set(40 + rnd() * 40, 14 + rnd() * 14, 1);
    washes.push({ w, a, e, r, speed: 0.0015 + rnd() * 0.002 });
    group.add(w);
  }

  // the meadow: luminous near the podium, deep green far away, grass grain
  const ground = new THREE.Mesh(new THREE.PlaneGeometry(240, 240, 1, 1), painted(
    { near: { value: col(0x9ccb44) }, mid: { value: col(0x3a7a2c) }, far: { value: col(0x14341b) } }, `
      float r = length(vW.xz);
      c = mix(near, mid, smoothstep(1.5, 16.0, r));
      c = mix(c, far, smoothstep(14.0, 70.0, r));
      c *= 1.0 + 0.07 * grain(vW + 13.7, 23.0) + 0.05 * grain(vW, 6.3);`));
  ground.rotation.x = -Math.PI / 2;
  ground.position.y = -0.015;
  group.add(ground);

  // the tree: a canopy wider than the orchestra's whole field, lit from the meadow below; a trunk ahead and to the right
  // a cap (the top 55 % of a sphere, so its rim curves under) over a shallow
  // inverted cone: the underside, dark by the trunk, glowing at the rim where
  // the meadow's light reaches it
  const treeAt = V(8, 0, -18), capR = V(34, 24, 34), capY = 14, cut = 0.55 * Math.PI;
  const rimY = capY + capR.y * Math.cos(cut), rimR = capR.x * Math.sin(cut);
  const leaf = { dark: { value: col(0x0b2a1f) }, light: { value: col(0x6fa634) }, ax: { value: treeAt.x }, az: { value: treeAt.z }, rim: { value: rimR } };
  const cap = new THREE.Mesh(new THREE.SphereGeometry(1, 64, 32, 0, Math.PI * 2, 0, cut), painted(leaf, `
      float d = distance(vW.xz, vec2(ax, az)) / rim;
      c = mix(dark, light, 0.35 * smoothstep(0.75, 1.0, d) * clamp(-vN.y + 0.3, 0.0, 1.0));
      c *= 1.0 + 0.08 * grain(vW, 2.5) + 0.05 * grain(vW + 7.1, 11.0);`));
  cap.position.set(treeAt.x, capY, treeAt.z);
  cap.scale.copy(capR);
  const underside = new THREE.Mesh(new THREE.ConeGeometry(rimR, 4.5, 64, 1, true), painted(leaf, `
      float d = distance(vW.xz, vec2(ax, az)) / rim;
      c = mix(dark, light, 0.13 + 0.72 * pow(smoothstep(0.15, 1.0, d), 1.7));  // never void: the grain reads even by the trunk
      c *= 1.0 + 0.10 * grain(vW, 2.5) + 0.06 * grain(vW + 7.1, 11.0);`, { side: THREE.DoubleSide }));
  underside.position.set(treeAt.x, rimY + 2.25, treeAt.z);
  const canopy = new THREE.Group();
  canopy.add(cap, underside);
  const bark = painted({ dark: { value: col(0x0a1511) }, edge: { value: col(0x1e3a2a) } }, `
      c = mix(dark, edge, 0.5 * smoothstep(0.6, 1.0, abs(vN.x)) + 0.25 * smoothstep(2.0, 11.0, vW.y));
      c *= 1.0 + 0.12 * grain(vW, 3.0);`);
  const trunk = new THREE.Mesh(new THREE.CylinderGeometry(2.0, 3.2, rimY + 3, 18, 1, true), bark);
  trunk.position.set(treeAt.x, (rimY + 3) / 2, treeAt.z);
  group.add(canopy, trunk);
  for (let i = 0; i < 7; i++) {  // the roots, flaring out
    const a = (i / 7) * Math.PI * 2 + rnd() * 0.4, len = 3.5 + rnd() * 2.5;
    const root = new THREE.Mesh(new THREE.ConeGeometry(1.1, len, 7), bark);
    root.position.set(treeAt.x + Math.cos(a) * len * 0.42, 0.25, treeAt.z + Math.sin(a) * len * 0.42);
    root.rotation.z = -Math.PI / 2 + 0.18;
    root.rotation.y = -a;
    root.scale.set(0.55, 1, 1);
    group.add(root);
  }

  // the light that reaches the lanterns and the hands: a dim green sky, a warm lamp at the podium
  const podium = new THREE.PointLight(0xffd090, 4, 5, 2);
  podium.position.set(0.4, 1.0, 0.8);
  group.add(new THREE.HemisphereLight(0x5a8a7a, 0x2a4a1a, 0.5), podium);

  // birds: a loose line of seven, white, crossing far ahead and back again round the field
  const BIRDS = 7, wingGeo = new THREE.PlaneGeometry(0.7, 0.2);
  wingGeo.translate(0.35, 0, 0);
  const wingMat = new THREE.MeshBasicMaterial({ color: 0xf2f4ee, side: THREE.DoubleSide });
  const leftWings = new THREE.InstancedMesh(wingGeo, wingMat, BIRDS), rightWings = new THREE.InstancedMesh(wingGeo, wingMat, BIRDS);
  leftWings.frustumCulled = rightWings.frustumCulled = false;
  const birds = Array.from({ length: BIRDS }, (_, i) => ({ along: i * 1.9 + rnd() * 0.6, phase: rnd() * Math.PI * 2, lift: (rnd() - 0.5) * 0.6, out: (rnd() - 0.5) * 1.4 }));
  const m = new THREE.Matrix4(), q = new THREE.Quaternion(), s = V(1, 1, 1), p = V(0, 0, 0), heading = new THREE.Quaternion(), flap = new THREE.Quaternion();
  const RING = 17, HEIGHT = 4.2;
  function fly(t) {
    const along = t * 0.045;  // radians per second round the ring: 1.1 m/s
    birds.forEach((b, i) => {
      const a = along + b.along / RING, x = Math.cos(a) * (RING + b.out), z = -Math.sin(a) * (RING + b.out), y = HEIGHT + b.lift + 0.25 * Math.sin(t * 0.7 + b.phase);
      heading.setFromAxisAngle(V(0, 1, 0), a + Math.PI / 2);  // along the ring, counter-clockwise seen from above
      const beat = 0.6 * Math.sin(t * 5.5 + b.phase);
      flap.setFromAxisAngle(V(0, 0, 1), beat);
      m.compose(p.set(x, y, z), q.copy(heading).multiply(flap), s.set(-1, 1, 1));
      leftWings.setMatrixAt(i, m);
      flap.setFromAxisAngle(V(0, 0, 1), -beat);
      m.compose(p.set(x, y, z), q.copy(heading).multiply(flap), s.set(1, 1, 1));
      rightWings.setMatrixAt(i, m);
    });
    leftWings.instanceMatrix.needsUpdate = rightWings.instanceMatrix.needsUpdate = true;
  }
  group.add(leftWings, rightWings);

  // fireflies, few, under the canopy
  const FLIES = 70;
  const flyPos = new Float32Array(FLIES * 3), flyPhase = new Float32Array(FLIES);
  const flies = Array.from({ length: FLIES }, (_, i) => {
    const a = rnd() * Math.PI * 2, r = 3 + rnd() * 16;
    flyPhase[i] = rnd() * Math.PI * 2;
    return { x: Math.cos(a) * r, y: 0.4 + rnd() * 3, z: Math.sin(a) * r, a: rnd() * Math.PI * 2, w: 0.2 + rnd() * 0.4 };
  });
  const flyGeo = new THREE.BufferGeometry();
  flyGeo.setAttribute("position", new THREE.BufferAttribute(flyPos, 3));
  flyGeo.setAttribute("phase", new THREE.BufferAttribute(flyPhase, 1));
  const flyMesh = new THREE.Points(flyGeo, new THREE.ShaderMaterial({ transparent: true, depthWrite: false, blending: THREE.AdditiveBlending, uniforms: { time: { value: 0 } },
    vertexShader: `attribute float phase; uniform float time; varying float a;
      void main() { vec4 mv = modelViewMatrix * vec4(position, 1.0); float tw = 0.5 + 0.5 * sin(time * 1.4 + phase); a = tw * tw * tw;
        gl_PointSize = (1.5 + 5.0 * a) * 9.0 / max(-mv.z, 0.5); gl_Position = projectionMatrix * mv; }`,
    fragmentShader: `varying float a; void main() { float d = length(gl_PointCoord - 0.5) * 2.0; gl_FragColor = vec4(0.9, 1.0, 0.55, a * smoothstep(1.0, 0.15, d)); }` }));
  flyMesh.frustumCulled = false;
  function drift(t, dt) {
    flies.forEach((f, i) => {
      f.a += Math.sin(t * 0.4 + f.w * 9) * 0.7 * dt;
      f.x += Math.cos(f.a) * f.w * dt; f.z += Math.sin(f.a) * f.w * dt;
      const r = Math.hypot(f.x, f.z);
      if (r < 2.5 || r > 22) f.a += Math.PI;
      f.y = 1.4 + 1.2 * Math.sin(t * 0.3 + f.w * 11) * Math.sin(t * 0.21 + i);
      flyPos[i * 3] = f.x; flyPos[i * 3 + 1] = f.y; flyPos[i * 3 + 2] = f.z;
    });
    flyGeo.attributes.position.needsUpdate = true;
  }
  group.add(flyMesh);

  scene.add(group);
  let t = 0;
  return {
    group,
    frame(dt) {
      t += dt;
      stars.material.uniforms.time.value = t;
      flyMesh.material.uniforms.time.value = t;
      for (const w of washes) { w.a += w.speed * dt; w.w.position.set(w.r * Math.cos(w.e) * Math.cos(w.a), w.r * Math.sin(w.e), w.r * Math.cos(w.e) * Math.sin(w.a)); }
      fly(t);
      drift(t, dt);
    },
  };
}
