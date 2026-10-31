// A cartoon meadow around the orchestra: a sky, rolling grass, round trees,
// flowers and drifting clouds, toon-shaded and made here from simple shapes
// (no assets). The ground stays flat where the orchestra and the conductor
// stand. Instead of the room: the page then asks for an immersive-vr session
// and draws the conductor's hands, which the headset no longer shows.
import { THREE } from "/static/room.js";

// Three steps of light, the cartoon look.
const TOON = (() => {
  const t = new THREE.DataTexture(new Uint8Array([90, 170, 255]), 3, 1, THREE.RedFormat);
  t.minFilter = t.magFilter = THREE.NearestFilter;
  t.needsUpdate = true;
  return t;
})();
const toon = (color, extra = {}) => new THREE.MeshToonMaterial({ color, gradientMap: TOON, ...extra });

// A repeatable random, so the meadow is the same every time.
function seeded(seed) {
  return () => { seed = (seed * 16807) % 2147483647; return (seed - 1) / 2147483646; };
}

// Gentle hills, flat within `flat` metres of the middle.
function heightAt(x, z, flat) {
  const r = Math.hypot(x, z);
  const rise = Math.min(1, Math.max(0, (r - flat) / 12));
  return rise * (0.9 * Math.sin(x * 0.11) * Math.cos(z * 0.09) + 0.5 * Math.sin(x * 0.23 + z * 0.17) + 0.6);
}

export function meadow(scene, { flat = 7 } = {}) {
  const group = new THREE.Group();
  const rnd = seeded(1830);
  group.userData.fog = new THREE.Fog(0xcfeeff, 30, 90);
  scene.fog = group.userData.fog;

  // the sky: a gradient dome, a round sun
  const sky = new THREE.Mesh(new THREE.SphereGeometry(95, 32, 16), new THREE.ShaderMaterial({
    side: THREE.BackSide, depthWrite: false, fog: false,
    uniforms: { top: { value: new THREE.Color(0x4fb3ff) }, horizon: { value: new THREE.Color(0xdff6ff) } },
    vertexShader: "varying float h; void main() { h = normalize(position).y; gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0); }",
    fragmentShader: "uniform vec3 top; uniform vec3 horizon; varying float h; void main() { gl_FragColor = vec4(mix(horizon, top, smoothstep(0.0, 0.55, h)), 1.0); }",
  }));
  sky.renderOrder = -100;
  const sun = new THREE.Mesh(new THREE.CircleGeometry(5, 40), new THREE.MeshBasicMaterial({ color: 0xfff3a0, fog: false }));
  sun.position.set(-35, 45, -60);
  sun.lookAt(0, 0, 0);
  group.add(sky, sun);

  // the light: a warm sun and a sky-blue fill
  const light = new THREE.DirectionalLight(0xfff4d6, 1.6);
  light.position.set(-20, 30, 10);
  group.add(light, new THREE.HemisphereLight(0xbfe8ff, 0x5b9a3c, 1.1));

  // the ground: rolling grass, two greens in patches
  const ground = new THREE.PlaneGeometry(190, 190, 96, 96);
  ground.rotateX(-Math.PI / 2);
  const pos = ground.attributes.position, colors = [];
  const light_ = new THREE.Color(0x8fd968), dark = new THREE.Color(0x6cbf4f), c = new THREE.Color();
  for (let i = 0; i < pos.count; i++) {
    const x = pos.getX(i), z = pos.getZ(i);
    pos.setY(i, heightAt(x, z, flat) - 0.02);
    c.copy(dark).lerp(light_, 0.5 + 0.5 * Math.sin(x * 0.31) * Math.cos(z * 0.27));
    colors.push(c.r, c.g, c.b);
  }
  ground.setAttribute("color", new THREE.Float32BufferAttribute(colors, 3));
  ground.computeVertexNormals();
  group.add(new THREE.Mesh(ground, toon(0xffffff, { vertexColors: true })));

  // far hills on the horizon
  for (let i = 0; i < 9; i++) {
    const a = (i / 9) * Math.PI * 2 + rnd() * 0.4, r = 62 + rnd() * 18;
    const hill = new THREE.Mesh(new THREE.SphereGeometry(14 + rnd() * 10, 20, 12), toon(rnd() > 0.5 ? 0x74c95a : 0x5fb04a));
    hill.scale.y = 0.35 + rnd() * 0.2;
    hill.position.set(Math.cos(a) * r, -2, Math.sin(a) * r);
    group.add(hill);
  }

  // round trees, not in front of the orchestra
  const where = (rMin, rMax, keepFront) => {
    for (;;) {
      const a = rnd() * Math.PI * 2, r = rMin + rnd() * (rMax - rMin);
      const x = Math.cos(a) * r, z = Math.sin(a) * r;
      if (keepFront && z < 0 && Math.abs(x) < r * 0.75 && r < 22) continue; // the view to the orchestra stays open
      return [x, heightAt(x, z, flat), z];
    }
  };
  const trees = 34, m = new THREE.Matrix4(), q = new THREE.Quaternion(), s = new THREE.Vector3(), p = new THREE.Vector3(), axis = new THREE.Vector3();
  const trunks = new THREE.InstancedMesh(new THREE.CylinderGeometry(0.22, 0.32, 2.2, 8), toon(0x8a5a36), trees);
  const crowns = new THREE.InstancedMesh(new THREE.IcosahedronGeometry(1.5, 1), toon(0xffffff), trees * 3);
  const greens = [0x4caf50, 0x66bb44, 0x3f9d3f, 0x7bc657].map((g) => new THREE.Color(g));
  for (let i = 0; i < trees; i++) {
    const [x, y, z] = where(10, 45, true), k = 0.8 + rnd() * 0.7;
    m.compose(p.set(x, y + 1.1 * k, z), q.identity(), s.set(k, k, k));
    trunks.setMatrixAt(i, m);
    for (let j = 0; j < 3; j++) {
      const dx = (rnd() - 0.5) * 1.6 * k, dz = (rnd() - 0.5) * 1.6 * k, kk = k * (0.8 + rnd() * 0.5);
      m.compose(p.set(x + dx, y + (2.6 + j * 0.7) * k, z + dz), q.identity(), s.set(kk, kk, kk));
      crowns.setMatrixAt(i * 3 + j, m);
      crowns.setColorAt(i * 3 + j, greens[Math.floor(rnd() * greens.length)]);
    }
  }
  group.add(trunks, crowns);

  // grass tufts and flowers
  const tufts = 700, flowers = 450;
  const tuft = new THREE.InstancedMesh(new THREE.ConeGeometry(0.05, 0.28, 4), toon(0x4e9e3a), tufts);
  for (let i = 0; i < tufts; i++) {
    const [x, y, z] = where(1.5, 32, false);
    q.setFromAxisAngle(axis.set(rnd() - 0.5, 0, rnd() - 0.5).normalize(), rnd() * 0.3);
    m.compose(p.set(x, y + 0.12, z), q, s.setScalar(0.7 + rnd() * 0.8));
    tuft.setMatrixAt(i, m);
  }
  const petals = [0xffffff, 0xffe14d, 0xff8fc8, 0xc59bff, 0xff7a6b].map((f) => new THREE.Color(f));
  const flower = new THREE.InstancedMesh(new THREE.SphereGeometry(0.05, 8, 6), toon(0xffffff), flowers);
  for (let i = 0; i < flowers; i++) {
    const [x, y, z] = where(2, 30, false);
    m.compose(p.set(x, y + 0.12, z), q.identity(), s.setScalar(0.8 + rnd() * 0.7));
    flower.setMatrixAt(i, m);
    flower.setColorAt(i, petals[Math.floor(rnd() * petals.length)]);
  }
  group.add(tuft, flower);

  // clouds that drift
  const clouds = [];
  for (let i = 0; i < 8; i++) {
    const cloud = new THREE.Group();
    for (let j = 0; j < 5; j++) {
      const puff = new THREE.Mesh(new THREE.SphereGeometry(2 + rnd() * 1.6, 14, 10), toon(0xffffff, { fog: false }));
      puff.position.set((j - 2) * 2.2 + rnd(), rnd() * 1.2, rnd() * 1.5);
      cloud.add(puff);
    }
    const a = rnd() * Math.PI * 2, r = 30 + rnd() * 30;
    cloud.position.set(Math.cos(a) * r, 18 + rnd() * 10, Math.sin(a) * r);
    clouds.push({ cloud, speed: 0.15 + rnd() * 0.25 });
    group.add(cloud);
  }

  scene.add(group);
  return {
    group,
    frame(dt) {
      for (const c of clouds) {
        c.cloud.position.x += c.speed * dt;
        if (c.cloud.position.x > 70) c.cloud.position.x = -70;
      }
    },
  };
}

// The conductor's hands, drawn: the headset shows no hands in a meadow.
export function drawnHands(scene) {
  const joints = 25;
  const mesh = new THREE.InstancedMesh(new THREE.SphereGeometry(0.009, 10, 8), toon(0xffe3c8), joints * 2);
  mesh.count = 0;
  mesh.frustumCulled = false;
  scene.add(mesh);
  const m = new THREE.Matrix4(), q = new THREE.Quaternion(), s = new THREE.Vector3(), p = new THREE.Vector3();
  return {
    mesh,
    // each hand's joints as [name, [x, y, z], radius]
    update(hands) {
      let n = 0;
      for (const list of hands) for (const [, at, radius] of list) {
        m.compose(p.set(at[0], at[1], at[2]), q.identity(), s.setScalar(Math.max(radius || 0.008, 0.006) / 0.009));
        mesh.setMatrixAt(n++, m);
      }
      mesh.count = n;
      mesh.instanceMatrix.needsUpdate = true;
    },
  };
}
