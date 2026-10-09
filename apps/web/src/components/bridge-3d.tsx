'use client';

import { useEffect, useRef } from 'react';

/*
 * The hero's 3D suspension bridge, drawn as a cloud of glowing particles.
 *
 * The model is built here to real proportions rather than loaded from a file:
 * a main span with two side spans of about a quarter each, towers about a
 * sixth of the main span tall with portal crossbeams, main cables sagging
 * about a ninth of the span, hangers every few metres and a stiffening truss
 * under the deck.
 *
 * It is alive: particles fly in and assemble on load; messages cross the deck
 * and light it up as they pass; the cursor pushes particles aside; a click
 * sends a shock wave out from where it lands that throws the bridge apart, and
 * it puts itself back together; scrolling past lets it drift away. All of the
 * motion runs in the vertex shader, so the CPU only updates a few uniforms a
 * frame. Under reduced motion it renders one still frame. Three.js loads only
 * in the browser, after the page.
 */

type V3 = [number, number, number];

// Dimensions in tens of metres, close to a classic long-span suspension bridge.
const MAIN = 128; // main span between towers
const SIDE = 34; // each side span
const DECK_Y = 6.7; // deck height above the water
const DECK_W = 2.8; // deck width
const TRUSS = 0.8; // stiffening truss depth
const TOWER_TOP = 22.7;
const SAG = 14.2; // main cable sag
const CABLE_Z = DECK_W / 2 + 0.15;
const HALF = MAIN / 2;
const END = HALF + SIDE;

const PULSES = 6; // messages on the deck at once
const TRAIL = 8; // points in each message's tail
const BLASTS = 3; // shock waves that can overlap
const PULSE_SPEED = 11;

/** Main cable height at x: parabolic in the main span, flatter in the side spans. */
function cableY(x: number): number {
  const ax = Math.abs(x);
  if (ax <= HALF) {
    const t = (x + HALF) / MAIN;
    return TOWER_TOP - 4 * SAG * t * (1 - t);
  }
  const t = (ax - HALF) / SIDE; // 0 at the tower, 1 at the anchorage
  const anchorY = DECK_Y + 0.6;
  const sideSag = 2.4;
  return TOWER_TOP + (anchorY - TOWER_TOP) * t - 4 * sideSag * t * (1 - t);
}

type Kind = 'cable' | 'hanger' | 'tower' | 'deck' | 'water';
type Part = { points: V3[]; kind: Kind; size: number };

/** What the shader does with each kind: 0 structure, 1 deck (lights up), 2 water (waves). */
const KIND_ID: Record<Kind, number> = { cable: 0, hanger: 0, tower: 0, deck: 1, water: 2 };

function seeded(seed: number) {
  let s = seed;
  return () => {
    s = (s * 16807) % 2147483647;
    return (s - 1) / 2147483646;
  };
}

function build(budget: number): Part[] {
  const rand = seeded(7);
  const k = budget / 30000; // density scale for smaller screens
  const line = (a: V3, b: V3, perUnit: number): V3[] => {
    const len = Math.hypot(b[0] - a[0], b[1] - a[1], b[2] - a[2]);
    const n = Math.max(2, Math.round(len * perUnit * k));
    return Array.from({ length: n }, (_, i) => {
      const t = i / (n - 1);
      return [a[0] + (b[0] - a[0]) * t, a[1] + (b[1] - a[1]) * t, a[2] + (b[2] - a[2]) * t];
    });
  };
  const box = (c: V3, s: V3, n: number): V3[] =>
    Array.from({ length: Math.round(n * k) }, () => {
      // A random point on the box's surface.
      const p: V3 = [(rand() - 0.5) * s[0], (rand() - 0.5) * s[1], (rand() - 0.5) * s[2]];
      const axis = Math.floor(rand() * 3);
      p[axis] = (rand() < 0.5 ? -0.5 : 0.5) * (s[axis] ?? 0);
      return [c[0] + p[0], c[1] + p[1], c[2] + p[2]];
    });

  const cables: V3[] = [];
  const hangers: V3[] = [];
  for (const z of [-CABLE_Z, CABLE_Z]) {
    for (let x = -END; x < END; x += 0.5) {
      cables.push(...line([x, cableY(x), z], [x + 0.5, cableY(x + 0.5), z], 6));
    }
    for (let x = -END + 2; x < END - 1; x += 1.8) {
      if (Math.abs(Math.abs(x) - HALF) < 1.2) continue;
      hangers.push(...line([x, cableY(x) - 0.15, z], [x, DECK_Y + TRUSS / 2, z], 1.6));
    }
  }

  const towers: V3[] = [];
  for (const tx of [-HALF, HALF]) {
    for (const z of [-CABLE_Z, CABLE_Z]) {
      // Legs taper slightly toward the top.
      for (let y = 0; y < TOWER_TOP; y += 1) {
        const w = 1.3 - (y / TOWER_TOP) * 0.45;
        towers.push(...box([tx, y + 0.5, z], [w, 1, w], 26));
      }
    }
    for (const y of [DECK_Y - 2.2, 11.5, 15.6, 19.4, TOWER_TOP - 0.35]) {
      towers.push(...box([tx, y, 0], [0.9, 0.7, CABLE_Z * 2], 140));
    }
  }

  const deck: V3[] = [];
  for (const z of [-DECK_W / 2, DECK_W / 2]) {
    // Top and bottom chords, and the truss diagonals between them.
    deck.push(...line([-END, DECK_Y + TRUSS / 2, z], [END, DECK_Y + TRUSS / 2, z], 3));
    deck.push(...line([-END, DECK_Y - TRUSS / 2, z], [END, DECK_Y - TRUSS / 2, z], 3));
    for (let x = -END; x < END; x += 1.6) {
      deck.push(...line([x, DECK_Y - TRUSS / 2, z], [x + 0.8, DECK_Y + TRUSS / 2, z], 3));
      deck.push(...line([x + 0.8, DECK_Y + TRUSS / 2, z], [x + 1.6, DECK_Y - TRUSS / 2, z], 3));
    }
  }
  for (let i = 0; i < 2600 * k; i++) {
    deck.push([(rand() - 0.5) * END * 2, DECK_Y + TRUSS / 2, (rand() - 0.5) * DECK_W]);
  }
  // Approach viaducts and anchorages at both ends.
  for (const s of [-1, 1]) {
    deck.push(...box([s * (END + 1.2), DECK_Y / 2, 0], [2.4, DECK_Y, DECK_W + 0.6], 260));
  }

  const water: V3[] = [];
  for (let i = 0; i < 4500 * k; i++) {
    water.push([(rand() - 0.5) * END * 2.6, 0, (rand() - 0.5) * 70]);
  }

  return [
    { points: cables, kind: 'cable', size: 2.1 },
    { points: hangers, kind: 'hanger', size: 1.5 },
    { points: towers, kind: 'tower', size: 1.8 },
    { points: deck, kind: 'deck', size: 1.6 },
    { points: water, kind: 'water', size: 1.2 },
  ];
}

const VERTEX = /* glsl */ `
  attribute vec3 aStart;
  attribute vec3 aColor;
  attribute vec3 aDir;
  attribute float aSize;
  attribute float aRand;
  attribute float aKind;
  uniform float uProgress;
  uniform float uTime;
  uniform float uPixelRatio;
  uniform float uScale;
  uniform float uScroll;
  uniform float uSpan;
  uniform float uSpeed;
  uniform float uOffsets[${PULSES}];
  uniform vec4 uBlasts[${BLASTS}];
  uniform vec3 uMouse;
  uniform float uHover;
  uniform vec3 uGlow;
  varying vec3 vColor;
  varying float vAlpha;

  // A shock wave's push over time: out fast, a beat in the air, then home.
  float envelope(float t) {
    if (t <= 0.0) return 0.0;
    float up = 1.0 - pow(1.0 - clamp(t / 0.5, 0.0, 1.0), 4.0);
    float back = clamp((t - 0.85) / 1.5, 0.0, 1.0);
    back = back < 0.5 ? 4.0 * back * back * back : 1.0 - pow(-2.0 * back + 2.0, 3.0) / 2.0;
    return up * (1.0 - back);
  }

  void main() {
    float t = clamp(uProgress * 1.5 - aRand * 0.5, 0.0, 1.0);
    t = 1.0 - pow(1.0 - t, 3.0);

    vec3 home = position;
    if (aKind > 1.5) {
      home.y += sin(home.x * 0.12 + uTime * 0.9) * 0.35 + sin(home.z * 0.2 + uTime * 0.6) * 0.25;
    } else {
      home.y += sin(uTime * 0.8 + aRand * 40.0) * 0.04;
    }
    vec3 p = mix(aStart, home, t);

    // Clicks: each shock wave runs out from where it landed.
    float spark = 0.0;
    for (int i = 0; i < ${BLASTS}; i++) {
      vec4 b = uBlasts[i];
      vec3 away = home - b.xyz;
      float d = length(away);
      float e = envelope(uTime - b.w - d * 0.006);
      if (e > 0.0) {
        vec3 dir = normalize(away / max(d, 0.001) + aDir * 0.9 + vec3(0.0, 0.6, 0.0));
        // Throw the pieces up and out into the open hero, not down off its edge.
        dir.y = mix(dir.y, abs(dir.y), 0.8);
        float reach = (14.0 + aRand * 52.0) * (0.45 + 0.55 * exp(-d * 0.02));
        p += dir * reach * e;
        spark = max(spark, e);
      }
    }

    // The cursor parts the particles around it.
    vec3 m = home - uMouse;
    float md = length(m);
    p += normalize(m + aDir * 0.4) * uHover * smoothstep(12.0, 0.0, md) * 4.0;

    // Scrolling past lets the bridge drift apart.
    p += vec3(aDir.x, abs(aDir.y), aDir.z) * uScroll * (18.0 + aRand * 36.0);
    p.y += uScroll * uScroll * 12.0 * aRand;

    // Messages light up the deck under them as they cross.
    float glow = 0.0;
    if (aKind > 0.5 && aKind < 1.5) {
      for (int i = 0; i < ${PULSES}; i++) {
        float px = mod(uOffsets[i] + uTime * uSpeed, uSpan * 2.0) - uSpan;
        float dx = home.x - px;
        glow += exp(-dx * dx * 0.08) + (dx < 0.0 ? exp(dx * 0.35) * 0.35 : 0.0);
      }
      glow = min(glow, 1.0) * t;
    }

    vec4 mv = modelViewMatrix * vec4(p, 1.0);
    gl_Position = projectionMatrix * mv;
    gl_PointSize = aSize * uPixelRatio * uScale * (180.0 / -mv.z) * (1.0 + glow * 0.9 + spark * 0.5);
    vColor = mix(aColor, uGlow, max(glow, spark * 0.7));
    vAlpha = mix(0.15, 1.0, t) * (1.0 - uScroll * 0.9);
  }
`;

const FRAGMENT = /* glsl */ `
  varying vec3 vColor;
  varying float vAlpha;
  void main() {
    float d = length(gl_PointCoord - 0.5);
    float a = smoothstep(0.5, 0.0, d);
    gl_FragColor = vec4(vColor, a * vAlpha);
  }
`;

const PULSE_VERTEX = /* glsl */ `
  attribute float aOffset;
  attribute float aLane;
  attribute float aTrail;
  uniform float uTime;
  uniform float uPixelRatio;
  uniform float uScale;
  uniform float uSpan;
  uniform float uSpeed;
  uniform float uDeck;
  uniform float uVisible;
  varying float vFade;
  void main() {
    float x = mod(aOffset + uTime * uSpeed, uSpan * 2.0) - uSpan - aTrail * 5.0;
    vec4 mv = modelViewMatrix * vec4(x, uDeck, aLane, 1.0);
    gl_Position = projectionMatrix * mv;
    gl_PointSize = 9.0 * (1.0 - aTrail * 0.7) * uPixelRatio * uScale * (180.0 / -mv.z) * uVisible;
    vFade = smoothstep(uSpan, uSpan * 0.85, abs(x)) * (1.0 - aTrail);
  }
`;

const PULSE_FRAGMENT = /* glsl */ `
  uniform vec3 uColor;
  varying float vFade;
  void main() {
    float d = length(gl_PointCoord - 0.5);
    float core = smoothstep(0.18, 0.0, d);
    float glow = smoothstep(0.5, 0.0, d) * 0.5;
    gl_FragColor = vec4(uColor, (core + glow) * vFade);
  }
`;

/**
 * Fills its positioned parent (the hero) with the canvas, and frames the bridge
 * into the parent's `[data-bridge-stage]` box, so the copy sits on top and the
 * pieces can fly across the whole hero when it comes apart.
 */
export function Bridge3D({ className }: { className?: string }) {
  const host = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = host.current;
    if (!el) return;
    let disposed = false;
    let cleanup = () => {};
    const stage = el.closest('section')?.querySelector<HTMLElement>('[data-bridge-stage]') ?? el;

    // Three.js, the model and the shaders are real work for the main thread. Doing it while
    // the headline animates makes the words stall and jump, so the bridge waits until the
    // page has loaded, the headline has settled and the browser is idle.
    const mounted = performance.now();
    const settled = new Promise<void>((resolve) => {
      const idle = () => {
        const wait = Math.max(0, 1100 - (performance.now() - mounted));
        setTimeout(() => {
          if ('requestIdleCallback' in window)
            requestIdleCallback(() => resolve(), { timeout: 700 });
          else setTimeout(resolve, 50);
        }, wait);
      };
      if (document.readyState === 'complete') idle();
      else window.addEventListener('load', idle, { once: true });
    });

    // Hand the main thread back between the heavy steps, so none of them is one long task.
    const breathe = () =>
      new Promise<void>((resolve) => {
        if ('requestIdleCallback' in window) requestIdleCallback(() => resolve(), { timeout: 200 });
        else setTimeout(resolve, 0);
      });

    settled
      .then(() => (disposed ? null : import('three')))
      .then(async (THREE) => {
        if (!THREE || disposed) return;
        await breathe();
        if (disposed) return;
        const reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
        const small = window.innerWidth < 768;
        const primary = new THREE.Color();
        const palette: Record<Kind, InstanceType<typeof THREE.Color>> = {
          cable: new THREE.Color(),
          hanger: new THREE.Color(),
          tower: new THREE.Color(),
          deck: new THREE.Color(),
          water: new THREE.Color(),
        };
        let dark = true;
        // Colours follow the theme's tokens, and change with it.
        const readPalette = () => {
          const css = getComputedStyle(document.documentElement);
          const color = (name: string) =>
            new THREE.Color(css.getPropertyValue(name).trim() || '#3eebc0');
          const fg = color('--foreground');
          dark = document.documentElement.classList.contains('dark');
          primary.copy(color('--primary'));
          if (dark) {
            palette.cable.copy(primary);
            palette.hanger.copy(primary).lerp(fg, 0.15);
            palette.tower.copy(fg);
            palette.deck.copy(fg).lerp(primary, 0.25);
            palette.water.copy(primary).multiplyScalar(0.55);
          } else {
            // On a light page the bridge is inked in the brand green, darkest where it is solid.
            const bg = color('--background');
            palette.cable.copy(primary);
            palette.hanger.copy(primary).lerp(bg, 0.35);
            palette.tower.copy(primary).lerp(fg, 0.45);
            palette.deck.copy(primary).lerp(fg, 0.3);
            palette.water.copy(primary).lerp(bg, 0.55);
          }
        };
        readPalette();

        const renderer = new THREE.WebGLRenderer({
          antialias: false,
          alpha: true,
          powerPreference: 'high-performance',
        });
        const ratio = Math.min(window.devicePixelRatio, small ? 1.5 : 1.75);
        renderer.setPixelRatio(ratio);
        // Hidden until its first frame is drawn, then faded in.
        renderer.domElement.style.cssText =
          'display:block;width:100%;height:100%;opacity:0;transition:opacity .7s ease';
        el.appendChild(renderer.domElement);
        renderer.domElement.setAttribute('aria-hidden', 'true');

        const scene = new THREE.Scene();
        const camera = new THREE.PerspectiveCamera(small ? 38 : 24, 1, 1, 2000);
        const target = new THREE.Vector3(0, TOWER_TOP * 0.45, 0);
        // The resting view: nearly straight on and a little above the deck, so the
        // bridge reads level and symmetric, with just enough angle to feel 3D.
        const AZ = -0.1;
        const TILT = 0.2;

        // Particles: target positions, scattered starts, colours, sizes and a
        // random direction each one flies off in when the bridge comes apart.
        const parts = build(small ? 14000 : 30000);
        await breathe();
        if (disposed) {
          renderer.dispose();
          renderer.domElement.remove();
          return;
        }
        const count = parts.reduce((n, p) => n + p.points.length, 0);
        const pos = new Float32Array(count * 3);
        const start = new Float32Array(count * 3);
        const dir = new Float32Array(count * 3);
        const col = new Float32Array(count * 3);
        const size = new Float32Array(count);
        const rnd = new Float32Array(count);
        const kind = new Float32Array(count);
        let i = 0;
        const r = seeded(11);
        for (const part of parts) {
          for (const p of part.points) {
            pos.set(p, i * 3);
            const a = r() * Math.PI * 2;
            const b = (r() - 0.5) * Math.PI;
            const dist = 60 + r() * 110;
            start.set(
              [
                Math.cos(a) * Math.cos(b) * dist,
                12 + Math.sin(b) * dist * 0.6,
                Math.sin(a) * Math.cos(b) * dist,
              ],
              i * 3,
            );
            const u = r() * 2 - 1;
            const phi = r() * Math.PI * 2;
            const s = Math.sqrt(1 - u * u);
            dir.set([s * Math.cos(phi), u, s * Math.sin(phi)], i * 3);
            size[i] = part.size;
            rnd[i] = part.kind === 'water' ? 0.95 * r() : r();
            kind[i] = KIND_ID[part.kind];
            i++;
          }
        }
        const paint = () => {
          let j = 0;
          for (const part of parts) {
            const c = palette[part.kind];
            for (let n = 0; n < part.points.length; n++, j++) col.set([c.r, c.g, c.b], j * 3);
          }
        };
        paint();
        await breathe();
        if (disposed) {
          renderer.dispose();
          renderer.domElement.remove();
          return;
        }
        const geo = new THREE.BufferGeometry();
        geo.setAttribute('position', new THREE.BufferAttribute(pos, 3));
        geo.setAttribute('aStart', new THREE.BufferAttribute(start, 3));
        geo.setAttribute('aDir', new THREE.BufferAttribute(dir, 3));
        geo.setAttribute('aColor', new THREE.BufferAttribute(col, 3));
        geo.setAttribute('aSize', new THREE.BufferAttribute(size, 1));
        geo.setAttribute('aRand', new THREE.BufferAttribute(rnd, 1));
        geo.setAttribute('aKind', new THREE.BufferAttribute(kind, 1));
        // The model never leaves the frame enough to cull, and it moves in the shader.
        const offsets = Array.from({ length: PULSES }, (_, n) => (n * END * 2) / PULSES + n * 7);
        const uniforms = {
          uProgress: { value: reduce ? 1 : 0 },
          uTime: { value: 0 },
          uPixelRatio: { value: ratio },
          uScale: { value: 1 },
          uScroll: { value: 0 },
          uSpan: { value: END },
          uSpeed: { value: PULSE_SPEED },
          uOffsets: { value: offsets },
          uBlasts: {
            value: Array.from({ length: BLASTS }, () => new THREE.Vector4(0, 0, 0, -1000)),
          },
          uMouse: { value: new THREE.Vector3(0, -1000, 0) },
          uHover: { value: 0 },
          uGlow: { value: primary },
        };
        const mat = new THREE.ShaderMaterial({
          vertexShader: VERTEX,
          fragmentShader: FRAGMENT,
          uniforms,
          transparent: true,
          depthWrite: false,
        });
        const cloud = new THREE.Points(geo, mat);
        cloud.frustumCulled = false;
        scene.add(cloud);

        // Messages crossing the deck, each with a fading tail.
        const pg = new THREE.BufferGeometry();
        const n = PULSES * TRAIL;
        pg.setAttribute('position', new THREE.BufferAttribute(new Float32Array(n * 3), 3));
        pg.setAttribute(
          'aOffset',
          new THREE.BufferAttribute(
            Float32Array.from({ length: n }, (_, q) => offsets[Math.floor(q / TRAIL)] ?? 0),
            1,
          ),
        );
        pg.setAttribute(
          'aLane',
          new THREE.BufferAttribute(
            Float32Array.from({ length: n }, (_, q) => (Math.floor(q / TRAIL) % 2 ? 0.6 : -0.6)),
            1,
          ),
        );
        pg.setAttribute(
          'aTrail',
          new THREE.BufferAttribute(
            Float32Array.from({ length: n }, (_, q) => (q % TRAIL) / TRAIL),
            1,
          ),
        );
        const pulseUniforms = {
          uTime: uniforms.uTime,
          uPixelRatio: uniforms.uPixelRatio,
          uScale: uniforms.uScale,
          uSpan: uniforms.uSpan,
          uSpeed: uniforms.uSpeed,
          uDeck: { value: DECK_Y + TRUSS / 2 + 0.35 },
          uVisible: { value: 0 },
          uColor: { value: primary },
        };
        const pulseMat = new THREE.ShaderMaterial({
          vertexShader: PULSE_VERTEX,
          fragmentShader: PULSE_FRAGMENT,
          uniforms: pulseUniforms,
          transparent: true,
          depthWrite: false,
          blending: THREE.AdditiveBlending,
        });
        const pulses = new THREE.Points(pg, pulseMat);
        pulses.frustumCulled = false;
        scene.add(pulses);

        // Light adds up on a dark page; on a light page, plain blending keeps it crisp.
        const blend = () => {
          mat.blending = dark ? THREE.AdditiveBlending : THREE.NormalBlending;
          pulseMat.blending = dark ? THREE.AdditiveBlending : THREE.NormalBlending;
          mat.needsUpdate = true;
          pulseMat.needsUpdate = true;
        };
        blend();

        // The pointer: the view leans toward it, and particles near it part.
        const look = { x: 0, y: 0, tx: 0, ty: 0 };
        const ndc = new THREE.Vector2();
        let inside = false;
        const onPointer = (e: PointerEvent) => {
          look.tx = (e.clientX / window.innerWidth - 0.5) * 2;
          look.ty = (e.clientY / window.innerHeight - 0.5) * 2;
          const rect = el.getBoundingClientRect();
          inside =
            e.pointerType === 'mouse' &&
            e.clientX >= rect.left &&
            e.clientX <= rect.right &&
            e.clientY >= rect.top &&
            e.clientY <= rect.bottom;
          if (inside) {
            ndc.set(
              ((e.clientX - rect.left) / rect.width) * 2 - 1,
              -((e.clientY - rect.top) / rect.height) * 2 + 1,
            );
          }
        };
        window.addEventListener('pointermove', onPointer, { passive: true });

        // Where a screen point lands on the bridge: the plane through its middle.
        const ray = new THREE.Raycaster();
        const plane = new THREE.Plane(new THREE.Vector3(0, 0, 1), 0);
        const hit = new THREE.Vector3();
        const toBridge = (at: InstanceType<typeof THREE.Vector2>) => {
          ray.setFromCamera(at, camera);
          if (!ray.ray.intersectPlane(plane, hit)) hit.copy(target);
          hit.x = Math.max(-END, Math.min(END, hit.x));
          hit.y = Math.max(0, Math.min(TOWER_TOP + 4, hit.y));
          return hit;
        };

        // A click or tap throws the bridge apart from where it lands.
        let slot = 0;
        const onPress = (e: PointerEvent) => {
          if (reduce || uniforms.uProgress.value < 0.9) return;
          const rect = el.getBoundingClientRect();
          const at = new THREE.Vector2(
            ((e.clientX - rect.left) / rect.width) * 2 - 1,
            -((e.clientY - rect.top) / rect.height) * 2 + 1,
          );
          const p = toBridge(at);
          uniforms.uBlasts.value[slot]?.set(p.x, p.y, p.z, uniforms.uTime.value);
          slot = (slot + 1) % BLASTS;
        };
        el.addEventListener('pointerdown', onPress);

        // How far the hero has scrolled out of view, as 0 to 1.
        let scrollTarget = 0;
        const onScroll = () => {
          const rect = stage.getBoundingClientRect();
          const vh = window.innerHeight;
          scrollTarget = Math.max(0, Math.min(1, (vh * 0.1 - rect.top) / (rect.height * 1.3)));
        };
        window.addEventListener('scroll', onScroll, { passive: true });
        onScroll();

        // Back the camera off until the whole bridge, ends and tower tops included,
        // sits inside the stage box, whatever its shape.
        const towers = [-1, 1].flatMap((sx) =>
          [-1, 1].flatMap((sz) => [
            new THREE.Vector3(sx * HALF, TOWER_TOP, sz * CABLE_Z),
            new THREE.Vector3(sx * HALF, 0, sz * CABLE_Z),
          ]),
        );
        const ends = [-1, 1].flatMap((sx) =>
          [-1, 1].map((sz) => new THREE.Vector3(sx * (END + 2.4), DECK_Y, sz * DECK_W)),
        );
        const probe = new THREE.Vector3();
        let top = 1; // where the tower tops land in the stage, in clip space
        const fit = () => {
          let r = 150;
          for (let k = 0; k < 6; k++) {
            camera.position.set(
              Math.sin(AZ) * r * Math.cos(TILT),
              target.y + Math.sin(TILT) * r,
              Math.cos(AZ) * r * Math.cos(TILT),
            );
            camera.lookAt(target);
            camera.updateMatrixWorld();
            camera.updateProjectionMatrix();
            // Towers well inside the frame, the approach spans running to its edges.
            let tx = 0;
            let ty = 0;
            top = -1;
            let ex = 0;
            for (const c of towers) {
              probe.copy(c).project(camera);
              tx = Math.max(tx, Math.abs(probe.x));
              ty = Math.max(ty, Math.abs(probe.y));
              if (c.y > 0) top = Math.max(top, probe.y);
            }
            for (const c of ends) {
              probe.copy(c).project(camera);
              ex = Math.max(ex, Math.abs(probe.x));
            }
            r *= Math.max(tx / (small ? 0.92 : 0.8), ex / 2.2, ty / 0.94);
          }
          return r;
        };

        let radius = 180;
        const resize = () => {
          const w = el.clientWidth;
          const h = el.clientHeight;
          renderer.setSize(w, h, false);
          // Frame the bridge for the stage box, then widen the view to the whole canvas.
          const box = stage.getBoundingClientRect();
          const own = el.getBoundingClientRect();
          const sw = Math.max(1, box.width);
          const sh = Math.max(1, box.height);
          camera.aspect = sw / sh;
          camera.clearViewOffset();
          radius = fit();
          uniforms.uScale.value = radius / 128;
          // Lift the bridge so its towers stand just inside the top of the stage.
          const lift = Math.max(0, ((1 - top) / 2) * sh - 28);
          camera.setViewOffset(sw, sh, own.left - box.left, own.top - box.top + lift, w, h);
          if (reduce) {
            place(0);
            renderer.render(scene, camera);
          }
        };
        const ro = new ResizeObserver(resize);
        ro.observe(el);
        if (stage !== el) ro.observe(stage);

        const themes = new MutationObserver(() => {
          readPalette();
          paint();
          blend();
          geo.getAttribute('aColor').needsUpdate = true;
          if (reduce) renderer.render(scene, camera);
        });
        themes.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] });

        let visible = true;
        const io = new IntersectionObserver(([e]) => {
          visible = e?.isIntersecting ?? true;
        });
        io.observe(el);

        // A slow drift plus the pointer.
        const place = (time: number) => {
          look.x += (look.tx - look.x) * 0.04;
          look.y += (look.ty - look.y) * 0.04;
          const az = AZ + Math.sin(time * 0.06) * 0.06 + look.x * 0.1;
          const tilt = TILT + look.y * -0.04;
          camera.position.set(
            Math.sin(az) * radius * Math.cos(tilt),
            target.y + Math.sin(tilt) * radius,
            Math.cos(az) * radius * Math.cos(tilt),
          );
          camera.lookAt(target);
        };

        let begin = 0;
        let frame = 0;
        const tick = (now: number) => {
          frame = requestAnimationFrame(tick);
          if (!visible || document.hidden) return;
          if (!begin) begin = now;
          const t = (now - begin) / 1000;
          uniforms.uTime.value = t;
          uniforms.uProgress.value = Math.min(1, t / 2.6);
          pulseUniforms.uVisible.value = Math.min(1, Math.max(0, (t - 2.4) / 0.8));
          uniforms.uScroll.value += (scrollTarget - uniforms.uScroll.value) * 0.12;
          place(t);
          if (inside) uniforms.uMouse.value.copy(toBridge(ndc));
          uniforms.uHover.value += ((inside ? 1 : 0) - uniforms.uHover.value) * 0.08;
          renderer.render(scene, camera);
        };
        resize();
        place(0);
        // Compile the shaders off the main thread where the browser can, then draw.
        renderer
          .compileAsync(scene, camera)
          .catch(() => undefined)
          .then(() => {
            if (disposed) return;
            if (reduce) renderer.render(scene, camera);
            else frame = requestAnimationFrame(tick);
            requestAnimationFrame(() => {
              renderer.domElement.style.opacity = '1';
            });
          });

        cleanup = () => {
          cancelAnimationFrame(frame);
          window.removeEventListener('pointermove', onPointer);
          window.removeEventListener('scroll', onScroll);
          el.removeEventListener('pointerdown', onPress);
          ro.disconnect();
          io.disconnect();
          themes.disconnect();
          geo.dispose();
          mat.dispose();
          pg.dispose();
          pulseMat.dispose();
          renderer.dispose();
          renderer.domElement.remove();
        };
      });

    return () => {
      disposed = true;
      cleanup();
    };
  }, []);

  return (
    <div className={`bridge-layer absolute inset-0 ${className ?? ''}`}>
      <div
        ref={host}
        className="absolute inset-0 touch-pan-y"
        role="img"
        aria-label="A suspension bridge drawn in points of light, with messages crossing its deck"
      />
    </div>
  );
}
