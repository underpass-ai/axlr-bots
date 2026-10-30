// A zen garden at dusk around the orchestra: raked gravel with rings around
// the stones, a still pond with the moon's glint, a torii standing in the
// water, bamboo that sways, a cherry tree shedding petals, stone lanterns,
// fireflies, mist, stars. Every shape is made here from three.js primitives
// (no assets), instanced where there are many, no shadows and one point light:
// it runs on a Quest 3S. The ground stays flat and bare where the orchestra
// and the conductor stand (the hall's floor is drawn on it); it is dark on
// purpose: the chairs' lanterns are the light. Same contract as meadow.js.
import { THREE } from "/static/room.js";

function seeded(seed) {
  return () => { seed = (seed * 16807) % 2147483647; return (seed - 1) / 2147483646; };
}
const V = (x, y, z) => new THREE.Vector3(x, y, z);
const std = (color, extra = {}) => new THREE.MeshStandardMaterial({ color, roughness: 0.85, metalness: 0.0, ...extra });

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
const GLOW = radialTexture([[0, "rgba(255,255,255,1)"], [0.3, "rgba(255,255,255,0.45)"], [1, "rgba(255,255,255,0)"]]);

// A rock: an icosahedron pushed in and out by a hash of each vertex, so the
// shared vertices agree and the faces stay flat.
function rockGeometry(seed) {
  const g = new THREE.IcosahedronGeometry(1, 2);
  const p = g.attributes.position;
  for (let i = 0; i < p.count; i++) {
    const x = p.getX(i), y = p.getY(i), z = p.getZ(i);
    const h = Math.sin(Math.round(x * 97) * 12.9898 + Math.round(y * 97) * 78.233 + Math.round(z * 97) * 37.719 + seed) * 43758.5453;
    const k = 0.82 + 0.36 * (h - Math.floor(h));
    p.setXYZ(i, x * k, y * k * 0.75, z * k);
  }
  g.computeVertexNormals();
  return g;
}

// The ground is one plane and one shader: fine gravel raked in rings around
// the stones and in straight lines elsewhere, a band of moss at the shore,
// then water with slow ripples and the moon's glint. The fog is three's.
function groundMaterial(stones, garden, moonDir) {
  return new THREE.ShaderMaterial({
    fog: true,
    uniforms: THREE.UniformsUtils.merge([THREE.UniformsLib.fog, {
      time: { value: 0 }, garden: { value: garden }, moon: { value: new THREE.Vector2(moonDir.x, moonDir.z).normalize() },
      stones: { value: stones.map((s) => new THREE.Vector3(s.x, s.z, s.ring)) },
    }]),
    vertexShader: `
      varying vec2 vXZ;
      #include <fog_pars_vertex>
      void main() {
        vec4 mvPosition = modelViewMatrix * vec4(position, 1.0);
        vXZ = position.xz;
        gl_Position = projectionMatrix * mvPosition;
        #include <fog_vertex>
      }`,
    fragmentShader: `
      uniform float time; uniform float garden; uniform vec2 moon; uniform vec3 stones[${stones.length}];
      varying vec2 vXZ;
      #include <fog_pars_fragment>
      float hash(vec2 p) { return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453); }
      void main() {
        float r = length(vXZ);
        // gravel: 12 cm furrows, rings near a stone, lines elsewhere, grain
        float lines = sin(vXZ.x * 52.0);
        float ring = 0.0, w = 0.0;
        for (int i = 0; i < ${stones.length}; i++) {
          float d = distance(vXZ, stones[i].xy);
          float k = smoothstep(stones[i].z, stones[i].z * 0.35, d);
          ring += k * sin(d * 52.0);
          w += k;
        }
        float rake = mix(lines, ring / max(w, 1.0), clamp(w, 0.0, 1.0));
        float grain = (hash(floor(vXZ * 80.0)) - 0.5) * 0.10;
        vec3 gravel = vec3(0.33, 0.30, 0.26) * (1.0 + 0.06 * rake + grain);
        // the moss at the shore, the water beyond
        vec3 moss = vec3(0.13, 0.20, 0.11) * (1.0 + 0.2 * (hash(floor(vXZ * 14.0)) - 0.5));
        vec3 water = vec3(0.025, 0.05, 0.085);
        float ripple = 0.5 * sin(r * 6.0 - time * 0.7) + 0.5 * sin((vXZ.x + vXZ.y) * 2.6 + time * 0.45);
        vec2 dir = vXZ / max(r, 0.001);
        float toward = dot(dir, moon);
        float lane = smoothstep(0.975, 0.999, toward) * smoothstep(9.0, 24.0, r) * (1.0 - smoothstep(40.0, 85.0, r));
        float glint = lane * (0.35 + 0.65 * pow(0.5 + 0.5 * sin(r * 7.0 - time * 1.5 + vXZ.x * 1.7), 3.0));
        vec3 pond = water * (1.0 + 0.10 * ripple) + vec3(0.62, 0.66, 0.78) * glint * 0.85;
        vec3 col = mix(gravel, moss, smoothstep(garden - 0.25, garden + 0.15, r));
        col = mix(col, pond, smoothstep(garden + 0.55, garden + 0.95, r));
        gl_FragColor = vec4(col, 1.0);
        #include <fog_fragment>
      }`,
  });
}

export function zen(scene, { flat = 7 } = {}) {
  const group = new THREE.Group();
  const rnd = seeded(2026);
  const garden = Math.max(flat, 8);  // the gravel's radius; the orchestra sits well inside
  const fog = new THREE.FogExp2(0x191a36, 0.013);
  group.userData.fog = fog;
  group.userData.exposure = 1.0;
  scene.fog = fog;
  const moonPos = V(-12, 27, -80);

  // the sky: a dome from a plum horizon to deep indigo, brighter near the moon; the moon; the stars
  const sky = new THREE.Mesh(new THREE.SphereGeometry(96, 32, 16), new THREE.ShaderMaterial({
    side: THREE.BackSide, depthWrite: false, fog: false,
    uniforms: { top: { value: new THREE.Color(0x070b22) }, mid: { value: new THREE.Color(0x1d1c48) }, horizon: { value: new THREE.Color(0x5a3650) },
      moon: { value: moonPos.clone().normalize() } },
    vertexShader: "varying vec3 d; void main() { d = normalize(position); gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0); }",
    fragmentShader: `uniform vec3 top; uniform vec3 mid; uniform vec3 horizon; uniform vec3 moon; varying vec3 d;
      void main() {
        vec3 c = mix(horizon, mid, smoothstep(-0.05, 0.22, d.y));
        c = mix(c, top, smoothstep(0.2, 0.7, d.y));
        c += vec3(0.42, 0.44, 0.6) * pow(max(dot(d, moon), 0.0), 60.0) * 0.5;
        gl_FragColor = vec4(c, 1.0);
      }`,
  }));
  sky.renderOrder = -100;
  const moon = new THREE.Mesh(new THREE.CircleGeometry(4.2, 48), new THREE.MeshBasicMaterial({ color: 0xf6f1df, fog: false }));
  moon.position.copy(moonPos);
  moon.lookAt(0, 0, 0);
  const halo = new THREE.Sprite(new THREE.SpriteMaterial({ map: GLOW, color: 0x8d93c4, transparent: true, opacity: 0.55, fog: false,
    blending: THREE.AdditiveBlending, depthWrite: false }));
  halo.position.copy(moonPos);
  halo.scale.set(34, 34, 1);
  const stars = (() => {
    const n = 900, pos = [], phase = [], size = [];
    for (let i = 0; i < n; i++) {
      const a = rnd() * Math.PI * 2, e = Math.asin(0.06 + rnd() * 0.94), r = 93;
      pos.push(r * Math.cos(e) * Math.cos(a), r * Math.sin(e), r * Math.cos(e) * Math.sin(a));
      phase.push(rnd() * Math.PI * 2);
      size.push(1.2 + rnd() * rnd() * 2.6);
    }
    const g = new THREE.BufferGeometry();
    g.setAttribute("position", new THREE.Float32BufferAttribute(pos, 3));
    g.setAttribute("phase", new THREE.Float32BufferAttribute(phase, 1));
    g.setAttribute("size", new THREE.Float32BufferAttribute(size, 1));
    const m = new THREE.ShaderMaterial({ transparent: true, depthWrite: false, fog: false, uniforms: { time: { value: 0 } },
      vertexShader: `attribute float phase; attribute float size; uniform float time; varying float a;
        void main() { a = 0.55 + 0.45 * sin(time * 0.9 + phase); gl_PointSize = size; gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0); }`,
      fragmentShader: `varying float a; void main() { float d = length(gl_PointCoord - 0.5) * 2.0; gl_FragColor = vec4(0.9, 0.93, 1.0, a * smoothstep(1.0, 0.3, d)); }` });
    return new THREE.Points(g, m);
  })();
  group.add(sky, moon, halo, stars);

  // the light: the moon, a dim sky, one warm lantern by the podium for the conductor's hands
  const moonlight = new THREE.DirectionalLight(0xc9d3ff, 0.75);
  moonlight.position.copy(moonPos).normalize().multiplyScalar(30);
  const podium = new THREE.PointLight(0xffb36a, 5, 5, 2);
  podium.position.set(0.5, 1.1, 0.9);
  group.add(moonlight, new THREE.HemisphereLight(0x4a5590, 0x14120f, 0.55), podium);

  // the stones, and the ground raked around them
  const stones = [
    { x: -5.6, z: -4.9, s: 1.0, ring: 3.2 }, { x: 6.1, z: -4.2, s: 0.8, ring: 2.8 }, { x: 0.6, z: -7.2, s: 0.6, ring: 2.2 },
    { x: 3.4, z: 5.2, s: 0.7, ring: 2.6 }, { x: -4.3, z: 5.8, s: 0.5, ring: 2.0 },
  ];
  const groundMat = groundMaterial(stones, garden, moonPos);
  const ground = new THREE.Mesh(new THREE.PlaneGeometry(190, 190, 1, 1), groundMat);
  ground.rotation.x = -Math.PI / 2;
  ground.position.y = -0.015;
  group.add(ground);
  stones.forEach((st, i) => {
    const rock = new THREE.Mesh(rockGeometry(i * 7.3), std(0x3a3c40, { flatShading: true, roughness: 0.95 }));
    rock.position.set(st.x, -0.15 * st.s, st.z);
    rock.scale.set(st.s * (1.1 + 0.5 * rnd()), st.s, st.s * (0.9 + 0.5 * rnd()));
    rock.rotation.y = rnd() * Math.PI;
    group.add(rock);
    if (st.s >= 0.7) {  // a little moss on the big ones
      const moss = new THREE.Mesh(new THREE.SphereGeometry(0.55 * st.s, 12, 8), std(0x2f5a2a, { roughness: 1 }));
      moss.scale.y = 0.25;
      moss.position.set(st.x + 0.2 * st.s, 0.55 * st.s, st.z - 0.1 * st.s);
      group.add(moss);
    }
  });

  // the far shore: mountains in the mist, a torii standing in the water, straight ahead under the moon
  const mountains = new THREE.InstancedMesh(new THREE.SphereGeometry(1, 20, 10), std(0x121733, { roughness: 1 }), 9);
  const m = new THREE.Matrix4(), q = new THREE.Quaternion(), s = new THREE.Vector3(), p = new THREE.Vector3();
  for (let i = 0; i < 9; i++) {
    const a = Math.PI * 0.55 + (i / 9) * Math.PI * 1.9 + rnd() * 0.3, r = 64 + rnd() * 16, k = 16 + rnd() * 12;
    m.compose(p.set(Math.cos(a) * r, -k * 0.3, Math.sin(a) * r), q.identity(), s.set(k * (1.2 + rnd() * 0.8), k * (0.45 + rnd() * 0.3), k));
    mountains.setMatrixAt(i, m);
  }
  group.add(mountains);
  const torii = new THREE.Group();
  const vermilion = std(0xa8301c, { emissive: 0x2a0a05, roughness: 0.7 });
  for (const x of [-2.3, 2.3]) {
    const pillar = new THREE.Mesh(new THREE.CylinderGeometry(0.2, 0.26, 5.2, 10), vermilion);
    pillar.position.set(x, 2.3, 0);
    torii.add(pillar);
  }
  const kasagi = new THREE.Mesh(new THREE.BoxGeometry(7.6, 0.36, 0.42), std(0x1a1512, { roughness: 0.8 }));
  kasagi.position.y = 5.05;
  const shimaki = new THREE.Mesh(new THREE.BoxGeometry(7.0, 0.22, 0.36), vermilion);
  shimaki.position.y = 4.76;
  const nuki = new THREE.Mesh(new THREE.BoxGeometry(6.2, 0.26, 0.3), vermilion);
  nuki.position.y = 4.0;
  const gakuzuka = new THREE.Mesh(new THREE.BoxGeometry(0.34, 0.65, 0.24), vermilion);
  gakuzuka.position.y = 4.38;
  torii.add(kasagi, shimaki, nuki, gakuzuka);
  torii.position.set(-7, -0.4, -26);  // beside the panel, on the moon's lane
  group.add(torii);

  // four stone lanterns at the shore, each a warm glow and a pool of light on the gravel
  const lanternAt = [[garden - 0.6, 122], [garden - 0.6, 58], [garden - 0.4, 232], [garden - 0.4, 308]];
  const stone = std(0x4b4a48, { roughness: 0.9 });
  const parts = [
    [new THREE.CylinderGeometry(0.32, 0.38, 0.18, 6), 0.09, stone], [new THREE.CylinderGeometry(0.11, 0.14, 0.9, 6), 0.63, stone],
    [new THREE.CylinderGeometry(0.34, 0.26, 0.12, 6), 1.14, stone], [new THREE.CylinderGeometry(0.15, 0.15, 0.3, 8), 1.37, std(0xffb45c, { emissive: 0xffa040, emissiveIntensity: 2.2 })],
    [new THREE.ConeGeometry(0.52, 0.3, 6), 1.69, stone], [new THREE.SphereGeometry(0.07, 10, 8), 1.9, stone],
  ];
  for (let k = 0; k < 4; k++) parts.push([new THREE.BoxGeometry(0.05, 0.34, 0.05), 1.37, stone, k]);
  for (const [geo, y, mat, post] of parts) {
    const mesh = new THREE.InstancedMesh(geo, mat, lanternAt.length);
    lanternAt.forEach(([r, deg], i) => {
      const a = (deg * Math.PI) / 180, x = r * Math.cos(a), z = -r * Math.sin(a);
      const ox = post == null ? 0 : 0.19 * Math.cos((post * Math.PI) / 2 + Math.PI / 4), oz = post == null ? 0 : 0.19 * Math.sin((post * Math.PI) / 2 + Math.PI / 4);
      m.compose(p.set(x + ox, y, z + oz), q.setFromAxisAngle(s.set(0, 1, 0), Math.PI / 6), s.set(1, 1, 1));
      mesh.setMatrixAt(i, m);
    });
    group.add(mesh);
  }
  for (const [r, deg] of lanternAt) {
    const a = (deg * Math.PI) / 180, x = r * Math.cos(a), z = -r * Math.sin(a);
    const glow = new THREE.Sprite(new THREE.SpriteMaterial({ map: GLOW, color: 0xffa050, transparent: true, opacity: 0.5, blending: THREE.AdditiveBlending, depthWrite: false }));
    glow.position.set(x, 1.37, z);
    glow.scale.set(1.6, 1.6, 1);
    const pool = new THREE.Mesh(new THREE.PlaneGeometry(3.2, 3.2), new THREE.MeshBasicMaterial({ map: GLOW, color: 0xff9a40, transparent: true, opacity: 0.22,
      blending: THREE.AdditiveBlending, depthWrite: false }));
    pool.rotation.x = -Math.PI / 2;
    pool.position.set(x, 0.004, z);
    group.add(glow, pool);
  }

  // bamboo in three clumps at the garden's edge, swaying; each stalk its nodes and a few leaves
  const clumps = [[10.5, 142, 14], [10.5, 38, 14], [9.5, 205, 16], [9.5, 335, 16], [10.5, 270, 12]];
  const stalks = [];
  for (const [r0, deg0, n] of clumps) {
    for (let i = 0; i < n; i++) {
      const a = ((deg0 + (rnd() - 0.5) * 28) * Math.PI) / 180, r = r0 + (rnd() - 0.5) * 2.4;
      stalks.push({ x: r * Math.cos(a), z: -r * Math.sin(a), h: 3.6 + rnd() * 2.6, phase: rnd() * Math.PI * 2, lean: (rnd() - 0.5) * 0.08, tilt: rnd() * Math.PI * 2 });
    }
  }
  const NODES = 5, LEAVES = 7;
  const stalkMesh = new THREE.InstancedMesh(new THREE.CylinderGeometry(0.035, 0.045, 1, 7), std(0x4d7a3f, { roughness: 0.7 }), stalks.length);
  const nodeMesh = new THREE.InstancedMesh(new THREE.CylinderGeometry(0.05, 0.05, 0.03, 7), std(0x2f4f28), stalks.length * NODES);
  const leafGeo = new THREE.PlaneGeometry(0.07, 0.42);
  leafGeo.translate(0, 0.2, 0);
  const leafMesh = new THREE.InstancedMesh(leafGeo, std(0x5f9a4a, { side: THREE.DoubleSide, roughness: 0.8 }), stalks.length * LEAVES);
  const leaves = stalks.map(() => Array.from({ length: LEAVES }, () => ({ y: 0.55 + rnd() * 0.45, a: rnd() * Math.PI * 2, t: 0.9 + rnd() * 0.8 })));
  const up = V(0, 1, 0), axis = V(0, 0, 0), tip = V(0, 0, 0);
  function bamboo(t) {
    stalks.forEach((st, i) => {
      const sway = st.lean + 0.035 * Math.sin(t * 0.7 + st.phase) + 0.012 * Math.sin(t * 2.1 + st.phase * 3);
      axis.set(Math.cos(st.tilt), 0, Math.sin(st.tilt));
      q.setFromAxisAngle(axis, sway);
      tip.set(0, st.h, 0).applyQuaternion(q);
      m.compose(p.set(st.x + tip.x / 2, tip.y / 2, st.z + tip.z / 2), q, s.set(1, st.h, 1));
      stalkMesh.setMatrixAt(i, m);
      for (let k = 0; k < NODES; k++) {
        const f = (k + 1) / (NODES + 1);
        m.compose(p.set(st.x + tip.x * f, tip.y * f, st.z + tip.z * f), q, s.set(1, 1, 1));
        nodeMesh.setMatrixAt(i * NODES + k, m);
      }
      leaves[i].forEach((lf, k) => {
        const f = lf.y, droop = new THREE.Quaternion().setFromAxisAngle(axis.set(Math.cos(lf.a), 0, Math.sin(lf.a)), lf.t + 0.1 * Math.sin(t * 1.3 + lf.a));
        m.compose(p.set(st.x + tip.x * f, tip.y * f, st.z + tip.z * f), droop.premultiply(q), s.set(1, 1, 1));
        leafMesh.setMatrixAt(i * LEAVES + k, m);
      });
    });
    stalkMesh.instanceMatrix.needsUpdate = nodeMesh.instanceMatrix.needsUpdate = leafMesh.instanceMatrix.needsUpdate = true;
  }
  group.add(stalkMesh, nodeMesh, leafMesh);

  // a cherry tree by the big stone on the left and a maple on the right: a trunk, a few branches, clouds of blossom
  function tree(x, z, crownColor, emissive, scale) {
    const t = new THREE.Group();
    const bark = std(0x3b2a22, { roughness: 0.95 });
    const trunk = new THREE.Mesh(new THREE.CylinderGeometry(0.16, 0.28, 2.4, 9), bark);
    trunk.position.y = 1.2;
    t.add(trunk);
    const branches = new THREE.InstancedMesh(new THREE.CylinderGeometry(0.06, 0.11, 1.6, 7), bark, 5);
    for (let i = 0; i < 5; i++) {
      const a = (i / 5) * Math.PI * 2 + rnd() * 0.5, lean = 0.6 + rnd() * 0.5;
      q.setFromAxisAngle(axis.set(Math.cos(a), 0, Math.sin(a)), lean);
      tip.set(0, 0.8, 0).applyQuaternion(q);
      m.compose(p.set(tip.x, 2.2 + tip.y, tip.z), q, s.set(1, 1, 1));
      branches.setMatrixAt(i, m);
    }
    const puffs = new THREE.InstancedMesh(new THREE.SphereGeometry(0.55, 12, 9), std(crownColor, { emissive, roughness: 1 }), 42);
    for (let i = 0; i < 42; i++) {
      const a = rnd() * Math.PI * 2, r = Math.sqrt(rnd()) * 1.9, y = 3.0 + (rnd() - 0.5) * 1.6 - r * 0.25, k = 0.7 + rnd() * 0.7;
      m.compose(p.set(Math.cos(a) * r, y, Math.sin(a) * r), q.identity(), s.set(k, k * 0.8, k));
      puffs.setMatrixAt(i, m);
    }
    t.add(branches, puffs);
    t.position.set(x, 0, z);
    t.scale.setScalar(scale);
    group.add(t);
    return { x, z, top: 3.1 * scale, spread: 1.9 * scale };
  }
  const sakura = tree(-6.4, -6.2, 0xf2a7bd, 0x3a1522, 1.15);
  tree(6.6, -6.0, 0xb8412a, 0x2a0a05, 0.95);

  // petals from the cherry tree: they drift down, turning, rest on the gravel a while and are back in the crown
  const PETALS = 320;
  const petalMesh = new THREE.InstancedMesh(new THREE.PlaneGeometry(0.055, 0.04), std(0xf7c6d4, { emissive: 0x2a0d16, side: THREE.DoubleSide, roughness: 1 }), PETALS);
  petalMesh.frustumCulled = false;
  const petals = [];
  const spawn = (pt, fresh) => {
    const a = rnd() * Math.PI * 2, r = Math.sqrt(rnd()) * sakura.spread;
    pt.x = sakura.x + Math.cos(a) * r; pt.z = sakura.z + Math.sin(a) * r;
    pt.y = fresh ? rnd() * sakura.top : sakura.top - 0.9 + rnd() * 1.4;
    pt.vy = 0.22 + rnd() * 0.25; pt.phase = rnd() * Math.PI * 2; pt.spin = (rnd() - 0.5) * 4;
    pt.ax = axis.set(rnd() - 0.5, rnd() - 0.5, rnd() - 0.5).normalize().clone(); pt.rest = 0;
  };
  for (let i = 0; i < PETALS; i++) { const pt = {}; spawn(pt, true); petals.push(pt); }
  const lying = new THREE.Quaternion().setFromAxisAngle(V(1, 0, 0), -Math.PI / 2);
  function fall(t, dt) {
    petals.forEach((pt, i) => {
      if (pt.rest > 0) {
        pt.rest -= dt;
        if (pt.rest <= 0) spawn(pt, false);
      } else {
        pt.y -= pt.vy * dt;
        pt.x += (0.35 * Math.sin(t * 1.1 + pt.phase) + 0.08) * dt;
        pt.z += 0.3 * Math.cos(t * 0.8 + pt.phase) * dt;
        if (pt.y <= 0.012) { pt.y = 0.012; pt.rest = 6 + rnd() * 10; }
      }
      if (pt.rest > 0) q.copy(lying); else q.setFromAxisAngle(pt.ax, t * pt.spin + pt.phase);
      m.compose(p.set(pt.x, pt.y, pt.z), q, s.set(1, 1, 1));
      petalMesh.setMatrixAt(i, m);
    });
    petalMesh.instanceMatrix.needsUpdate = true;
  }
  group.add(petalMesh);

  // fireflies over the garden and the shore, each with its own blink
  const FLIES = 110;
  const flyPos = new Float32Array(FLIES * 3), flyPhase = new Float32Array(FLIES);
  const flies = Array.from({ length: FLIES }, (_, i) => {
    const a = rnd() * Math.PI * 2, r = 3.5 + rnd() * 9;
    flyPhase[i] = rnd() * Math.PI * 2;
    return { x: Math.cos(a) * r, y: 0.3 + rnd() * 1.6, z: Math.sin(a) * r, a: rnd() * Math.PI * 2, w: 0.3 + rnd() * 0.5 };
  });
  const flyGeo = new THREE.BufferGeometry();
  flyGeo.setAttribute("position", new THREE.BufferAttribute(flyPos, 3));
  flyGeo.setAttribute("phase", new THREE.BufferAttribute(flyPhase, 1));
  const flyMesh = new THREE.Points(flyGeo, new THREE.ShaderMaterial({ transparent: true, depthWrite: false, blending: THREE.AdditiveBlending, uniforms: { time: { value: 0 } },
    vertexShader: `attribute float phase; uniform float time; varying float a;
      void main() { vec4 mv = modelViewMatrix * vec4(position, 1.0); float tw = 0.5 + 0.5 * sin(time * 1.6 + phase); a = tw * tw * tw;
        gl_PointSize = (3.0 + 9.0 * a) * 18.0 / max(-mv.z, 0.5); gl_Position = projectionMatrix * mv; }`,
    fragmentShader: `varying float a; void main() { float d = length(gl_PointCoord - 0.5) * 2.0; gl_FragColor = vec4(0.8, 1.0, 0.45, a * smoothstep(1.0, 0.15, d)); }` }));
  flyMesh.frustumCulled = false;
  function drift(t, dt) {
    flies.forEach((f, i) => {
      f.a += (Math.sin(t * 0.5 + f.w * 7) * 0.8) * dt;
      f.x += Math.cos(f.a) * f.w * dt; f.z += Math.sin(f.a) * f.w * dt;
      const r = Math.hypot(f.x, f.z);
      if (r < 2.2 || r > 14) f.a += Math.PI;  // not over the orchestra, not out on the water
      f.y = 0.9 + 0.7 * Math.sin(t * 0.4 + f.w * 11) * Math.sin(t * 0.23 + i);
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
      groundMat.uniforms.time.value = t;
      stars.material.uniforms.time.value = t;
      flyMesh.material.uniforms.time.value = t;
      bamboo(t);
      fall(t, dt);
      drift(t, dt);
    },
  };
}
